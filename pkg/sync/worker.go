// Package sync provides a task queue and scheduler for data synchronization jobs.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
)

// JobExecutor is the interface that must be implemented to execute a specific job type.
type JobExecutor interface {
	// Execute runs the job and returns a result and/or error.
	// The worker will update job progress periodically via the progress callback.
	Execute(ctx context.Context, job *Job, progress ProgressReporter) (any, error)
	// JobType returns the type of job this executor handles.
	JobType() JobType
}

// ProgressReporter allows executors to report progress during job execution.
type ProgressReporter interface {
	ReportProgress(processed, total, failed int)
	ReportError(errMsg string)
}

// jobProgressReporter implements ProgressReporter and updates the job in the queue.
type jobProgressReporter struct {
	queue      *Queue
	job        *Job
	ctx        context.Context
	mu         sync.Mutex
	lastReport time.Time
	// lostRow fires once when the row stops being `running` — i.e. when a
	// cancellation landed. Reported once so the log says why a job stopped
	// mid-way instead of leaving a gap between "processing job" and silence.
	lostRow sync.Once
}

func (r *jobProgressReporter) ReportProgress(processed, total, failed int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Throttle updates to once per second to avoid excessive DB writes
	if time.Since(r.lastReport) < time.Second {
		return
	}
	r.lastReport = time.Now()

	r.job.UpdateProgress(processed, total, failed)
	r.persist()
}

func (r *jobProgressReporter) ReportError(errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.job.ErrorMessage = errMsg
	r.job.FailedItems++
	r.persist()
}

// persist writes the in-memory job back, but only while the row is still
// `running` (AUD-49). The in-memory copy carries `status` too, so an
// unconditional write here would put `running` back over a `cancelled` row on
// the next tick — the cancel endpoint would answer 200 and the job would carry
// on regardless.
func (r *jobProgressReporter) persist() {
	applied, err := r.queue.UpdateRunningJob(r.ctx, r.job)
	if err != nil {
		logging.Logger.Warn().Err(err).Str("job_id", r.job.ID).Msg("Failed to update job progress")
		return
	}
	if !applied {
		r.lostRow.Do(func() {
			logging.Logger.Info().
				Str("job_id", r.job.ID).
				Msg("Job row is no longer 'running'; stopping progress reports (cancelled elsewhere?)")
		})
	}
}

// runningJob is the handle to one in-flight execution. It is a pointer so the
// pool can tell "still mine" from "already replaced" by identity.
type runningJob struct {
	cancel context.CancelFunc
}

// WorkerPool manages a pool of goroutines that process sync jobs.
type WorkerPool struct {
	queue      *Queue
	executors  map[JobType]JobExecutor
	logger     zerolog.Logger
	mu         sync.RWMutex
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	numWorkers int

	// running maps job ID -> handle of the execution currently in flight in
	// this process. It exists so CancelJob can actually stop an executor
	// (AUD-49): the row alone cannot reach a goroutine, and the executor will
	// happily keep fetching data for hours after its row says `cancelled`.
	runningMu sync.RWMutex
	running   map[string]*runningJob
}

// NewWorkerPool creates a new worker pool with the specified number of workers.
func NewWorkerPool(queue *Queue, numWorkers int) *WorkerPool {
	if numWorkers <= 0 {
		numWorkers = 3
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		queue:      queue,
		executors:  make(map[JobType]JobExecutor),
		logger:     logging.WithContext(map[string]any{"component": "sync_worker_pool"}),
		ctx:        ctx,
		cancel:     cancel,
		numWorkers: numWorkers,
		running:    make(map[string]*runningJob),
	}
}

// Cancel interrupts the in-flight execution of jobID if this pool owns it.
// Returns false when no execution of that job is running here — which is the
// normal answer for a `pending` job (nothing has started yet) and for a job
// left `running` by a process that already died.
//
// Safe to call from any goroutine, including an HTTP handler.
func (wp *WorkerPool) Cancel(jobID string) bool {
	wp.runningMu.RLock()
	handle, ok := wp.running[jobID]
	wp.runningMu.RUnlock()
	if !ok {
		return false
	}
	handle.cancel()
	return true
}

// RunningCount returns how many executions are in flight in this pool.
func (wp *WorkerPool) RunningCount() int {
	wp.runningMu.RLock()
	defer wp.runningMu.RUnlock()
	return len(wp.running)
}

// registerRunning records the handle for a job about to execute.
func (wp *WorkerPool) registerRunning(jobID string, handle *runningJob) {
	wp.runningMu.Lock()
	defer wp.runningMu.Unlock()
	wp.running[jobID] = handle
}

// unregisterRunning removes the handle, but only if it is still the one we
// registered — a job ID that has since been re-dequeued (retry) must not have
// its live handle deleted by the previous run's cleanup.
func (wp *WorkerPool) unregisterRunning(jobID string, handle *runningJob) {
	wp.runningMu.Lock()
	defer wp.runningMu.Unlock()
	if current, ok := wp.running[jobID]; ok && current == handle {
		delete(wp.running, jobID)
	}
}

// RegisterExecutor registers a job executor for a specific job type.
func (wp *WorkerPool) RegisterExecutor(executor JobExecutor) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	wp.executors[executor.JobType()] = executor
	wp.logger.Info().Str("job_type", string(executor.JobType())).Msg("Job executor registered")
}

// Start begins processing jobs with the worker pool.
//
// AUD-50: before any worker comes up, repair rows left in `running` by a
// previous process. The pool only ever dequeues `pending`, so without this an
// interrupted job stays `running` forever and every worker idles in WaitForJob
// while the job still looks alive. Doing it here — rather than asking each
// entrypoint to remember — is what makes the recovery structural: it cannot be
// skipped by wiring a new caller, and it is guaranteed to run before any worker
// can claim a job.
//
// internal/repoguard pins this call site to pkg/sync/worker.go:Start, so moving
// it breaks a structural guard rather than silently dropping the recovery.
func (wp *WorkerPool) Start() {
	if n, err := wp.queue.CleanupStaleRunning(context.Background()); err != nil {
		// Do not abort startup: a failed repair is strictly better than a
		// service that refuses to come up. But say so loudly, because the
		// consequence is a job stuck at `running` until the next restart.
		wp.logger.Error().Err(err).
			Msg("Stale-running cleanup failed; an interrupted job may stay 'running' until the next restart")
	} else if n > 0 {
		wp.logger.Warn().Int("recovered", n).
			Msg("Recovered jobs left 'running' by a previous process")
	}

	wp.logger.Info().Int("workers", wp.numWorkers).Msg("Starting sync worker pool")
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.workerLoop(i)
	}
}

// Stop gracefully shuts down the worker pool.
func (wp *WorkerPool) Stop() {
	wp.logger.Info().Msg("Stopping sync worker pool")
	wp.cancel()
	wp.wg.Wait()
	wp.logger.Info().Msg("Sync worker pool stopped")
}

// workerLoop is the main loop for each worker goroutine.
func (wp *WorkerPool) workerLoop(workerID int) {
	defer wp.wg.Done()
	logger := wp.logger.With().Int("worker_id", workerID).Logger()

	for {
		select {
		case <-wp.ctx.Done():
			logger.Info().Msg("Worker shutting down")
			return
		default:
		}

		// Try to dequeue a job.
		//
		// AUD-49: the per-job context is derived from the pool's context and
		// registered under the job ID, so it can be cancelled from outside
		// (CancelJob -> WorkerPool.Cancel). It used to be context.Background(),
		// which meant no handle existed at all and cancelling was impossible in
		// principle, no matter what the database said. Deriving from wp.ctx
		// also means Stop() reaches in-flight executors instead of waiting on
		// them; an aborted job is left `running` and is reaped by
		// CleanupStaleRunning (AUD-50) on the next start, so it stays
		// recoverable.
		jobCtx, cancelJob := context.WithCancel(wp.ctx)
		job, err := wp.queue.Dequeue(jobCtx)
		if err != nil {
			cancelJob()
			logger.Error().Err(err).Msg("Failed to dequeue job")
			time.Sleep(5 * time.Second)
			continue
		}
		if job == nil {
			cancelJob()
			// No jobs available, wait for notification or timeout
			if !wp.queue.WaitForJob(wp.ctx) {
				// Context cancelled
				return
			}
			continue
		}

		// Process the job
		job.WorkerID = fmt.Sprintf("worker-%d", workerID)
		handle := &runningJob{cancel: cancelJob}
		wp.registerRunning(job.ID, handle)
		wp.processJob(jobCtx, job, logger)
		wp.unregisterRunning(job.ID, handle)
		cancelJob()
	}
}

// processJob executes a single job using the appropriate executor.
//
// One rule governs every terminal write below: a job whose context is already
// done writes nothing. Its row was settled by whoever cancelled it, or it is
// left `running` and reaped by CleanupStaleRunning (AUD-50) on the next start.
// Without that rule a cancelled job would immediately try to write `failed` —
// or worse, `pending` via retry — over the `cancelled` row, using a context
// that is already dead. The conditional SQL would refuse the write anyway; the
// rule exists so the log says why instead of showing a confusing error.
func (wp *WorkerPool) processJob(ctx context.Context, job *Job, logger zerolog.Logger) {
	// Recover from panics to prevent worker crash
	defer func() {
		if r := recover(); r != nil {
			logger.Error().
				Interface("panic", r).
				Str("job_id", job.ID).
				Msg("Executor panicked, recovering worker")
			if ctxErr := ctx.Err(); ctxErr != nil {
				logger.Warn().Str("ctx_err", ctxErr.Error()).
					Msg("Job context already done; leaving the row to the stale-running reaper")
				return
			}
			if _, err := wp.queue.FailJob(ctx, job, fmt.Sprintf("executor panic: %v", r)); err != nil {
				logger.Error().Err(err).Msg("Failed to mark job as failed after panic")
			}
		}
	}()

	logger = logger.With().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Logger()

	logger.Info().Msg("Processing job")

	// Find the executor for this job type
	wp.mu.RLock()
	executor, ok := wp.executors[job.JobType]
	wp.mu.RUnlock()

	if !ok {
		errMsg := fmt.Sprintf("no executor registered for job type: %s", job.JobType)
		logger.Error().Msg(errMsg)
		if _, err := wp.queue.FailJob(ctx, job, errMsg); err != nil {
			logger.Error().Err(err).Msg("Failed to mark job as failed")
		}
		return
	}

	// Create progress reporter
	reporter := &jobProgressReporter{
		queue: wp.queue,
		job:   job,
		ctx:   ctx,
	}

	// Execute the job
	result, err := executor.Execute(ctx, job, reporter)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// Cancelled, or the pool is shutting down. Deliberately no status
			// write: marking it `failed` would blame the data source for a stop
			// the user asked for, and requeueing it (the retry branch below)
			// would make the worker run it a second time.
			logger.Info().Str("ctx_err", ctxErr.Error()).
				Msg("Job aborted: context cancelled, leaving the row as it is")
			return
		}
		// Check if we should retry
		if job.RetryCount < job.MaxRetries {
			if _, retryErr := wp.queue.RetryLater(ctx, job, err.Error()); retryErr != nil {
				logger.Error().Err(retryErr).Msg("Failed to schedule retry")
			}
		} else {
			if _, failErr := wp.queue.FailJob(ctx, job, err.Error()); failErr != nil {
				logger.Error().Err(failErr).Msg("Failed to mark job as failed")
			}
		}
		return
	}

	// Marshal result if present
	var resultJSON []byte
	if result != nil {
		var marshalErr error
		resultJSON, marshalErr = json.Marshal(result)
		if marshalErr != nil {
			logger.Error().Err(marshalErr).Msg("Failed to marshal job result")
			if _, failErr := wp.queue.FailJob(ctx, job, fmt.Sprintf("failed to marshal result: %v", marshalErr)); failErr != nil {
				logger.Error().Err(failErr).Msg("Failed to mark job as failed")
			}
			return
		}
	}

	// Mark job as completed
	applied, completeErr := wp.queue.CompleteJob(ctx, job, resultJSON)
	if completeErr != nil {
		logger.Error().Err(completeErr).Msg("Failed to mark job as completed")
		return
	}
	if !applied {
		// The row was settled while the executor was finishing. Almost always a
		// cancellation; either way this run no longer owns the row, so claiming
		// success would be false.
		logger.Warn().Msg("Job finished but its row was already settled; not reporting success")
		return
	}

	logger.Info().Msg("Job processed successfully")
}

// GetExecutor returns the executor for a given job type.
func (wp *WorkerPool) GetExecutor(jobType JobType) (JobExecutor, bool) {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	executor, ok := wp.executors[jobType]
	return executor, ok
}

// HasExecutor returns true if an executor is registered for the given job type.
func (wp *WorkerPool) HasExecutor(jobType JobType) bool {
	_, ok := wp.GetExecutor(jobType)
	return ok
}

// WorkerStats holds statistics about the worker pool.
type WorkerStats struct {
	NumWorkers      int      `json:"num_workers"`
	RegisteredTypes []string `json:"registered_types"`
	IsRunning       bool     `json:"is_running"`
}

// Stats returns current worker pool statistics.
func (wp *WorkerPool) Stats() WorkerStats {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	types := make([]string, 0, len(wp.executors))
	for t := range wp.executors {
		types = append(types, string(t))
	}

	return WorkerStats{
		NumWorkers:      wp.numWorkers,
		RegisteredTypes: types,
		IsRunning:       wp.ctx.Err() == nil,
	}
}
