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
	if err := r.queue.UpdateJob(r.ctx, r.job); err != nil {
		logging.Logger.Warn().Err(err).Str("job_id", r.job.ID).Msg("Failed to update job progress")
	}
}

func (r *jobProgressReporter) ReportError(errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.job.ErrorMessage = errMsg
	r.job.FailedItems++
	if err := r.queue.UpdateJob(r.ctx, r.job); err != nil {
		logging.Logger.Warn().Err(err).Str("job_id", r.job.ID).Msg("Failed to update job error")
	}
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
func (wp *WorkerPool) Start() {
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

		// Try to dequeue a job
		jobCtx := context.Background()
		job, err := wp.queue.Dequeue(jobCtx)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to dequeue job")
			time.Sleep(5 * time.Second)
			continue
		}
		if job == nil {
			// No jobs available, wait for notification or timeout
			if !wp.queue.WaitForJob(wp.ctx) {
				// Context cancelled
				return
			}
			continue
		}

		// Process the job
		job.WorkerID = fmt.Sprintf("worker-%d", workerID)
		wp.processJob(jobCtx, job, logger)
	}
}

// processJob executes a single job using the appropriate executor.
func (wp *WorkerPool) processJob(ctx context.Context, job *Job, logger zerolog.Logger) {
	// Recover from panics to prevent worker crash
	defer func() {
		if r := recover(); r != nil {
			logger.Error().
				Interface("panic", r).
				Str("job_id", job.ID).
				Msg("Executor panicked, recovering worker")
			if err := wp.queue.FailJob(ctx, job, fmt.Sprintf("executor panic: %v", r)); err != nil {
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
		if err := wp.queue.FailJob(ctx, job, errMsg); err != nil {
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
		// Check if we should retry
		if job.RetryCount < job.MaxRetries {
			if retryErr := wp.queue.RetryLater(ctx, job, err.Error()); retryErr != nil {
				logger.Error().Err(retryErr).Msg("Failed to schedule retry")
			}
		} else {
			if failErr := wp.queue.FailJob(ctx, job, err.Error()); failErr != nil {
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
			if failErr := wp.queue.FailJob(ctx, job, fmt.Sprintf("failed to marshal result: %v", marshalErr)); failErr != nil {
				logger.Error().Err(failErr).Msg("Failed to mark job as failed")
			}
			return
		}
	}

	// Mark job as completed
	if completeErr := wp.queue.CompleteJob(ctx, job, resultJSON); completeErr != nil {
		logger.Error().Err(completeErr).Msg("Failed to mark job as completed")
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
