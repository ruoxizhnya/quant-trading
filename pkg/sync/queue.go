// Package sync provides a task queue and scheduler for data synchronization jobs.
package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
)

// Queue manages the lifecycle of sync jobs using a PostgreSQL-backed queue.
// It provides thread-safe operations for enqueueing, dequeuing, and updating jobs.
type Queue struct {
	store     JobStore
	logger    zerolog.Logger
	mu        sync.RWMutex
	notifiers []chan struct{} // channels to notify when new jobs are available
}

// NewQueue creates a new job queue.
func NewQueue(store JobStore) *Queue {
	return &Queue{
		store:  store,
		logger: logging.WithContext(map[string]any{"component": "sync_queue"}),
	}
}

// Enqueue adds a new job to the queue.
func (q *Queue) Enqueue(ctx context.Context, job *Job) error {
	if err := q.store.CreateSyncJob(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}
	q.logger.Info().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Msg("Job enqueued")
	q.notify()
	return nil
}

// Dequeue retrieves the oldest pending job and marks it as running.
//
// The claim is a conditional write (AUD-49). Listing and claiming are two
// separate statements, and between them the job can be cancelled, or claimed
// by another worker. Writing `running` unconditionally would silently undo a
// cancellation that landed in that window — and the worker would then run a
// job the user had already stopped. When the claim does not land we return
// (nil, nil): there is no job for this worker, which is exactly true.
func (q *Queue) Dequeue(ctx context.Context) (*Job, error) {
	jobs, err := q.store.ListSyncJobs(ctx, JobStatusPending, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to dequeue job: %w", err)
	}
	if len(jobs) == 0 {
		return nil, nil
	}

	job := jobs[0]
	job.Status = JobStatusRunning
	now := time.Now()
	job.StartedAt = &now

	claimed, err := q.store.UpdateSyncJobIfStatus(ctx, job, JobStatusPending)
	if err != nil {
		return nil, fmt.Errorf("failed to mark job as running: %w", err)
	}
	if !claimed {
		q.logger.Info().
			Str("job_id", job.ID).
			Str("job_type", string(job.JobType)).
			Msg("Job was no longer pending when claimed; skipping it")
		return nil, nil
	}

	q.logger.Info().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Msg("Job dequeued and marked running")

	return job, nil
}

// Peek returns the oldest pending job without changing its status.
func (q *Queue) Peek(ctx context.Context) (*Job, error) {
	jobs, err := q.store.ListSyncJobs(ctx, JobStatusPending, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to peek queue: %w", err)
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return jobs[0], nil
}

// UpdateRunningJob persists progress while — and only while — the row is still
// `running`. It returns false when the job has been settled elsewhere, which
// is the signal that this execution no longer owns the row.
//
// AUD-49. This replaced an unconditional UpdateJob. A progress report carries
// the worker's entire in-memory copy of the job, `status` included, so writing
// it unconditionally meant the next tick (throttled to one per second) would
// write `running` back over a `cancelled` row. That is the whole defect: the
// cancel endpoint worked, the progress reporter undid it. With the condition
// in SQL a settled row is final by construction, not by timing.
func (q *Queue) UpdateRunningJob(ctx context.Context, job *Job) (bool, error) {
	applied, err := q.store.UpdateSyncJobIfStatus(ctx, job, JobStatusRunning)
	if err != nil {
		return false, fmt.Errorf("failed to update job: %w", err)
	}
	return applied, nil
}

// CompleteJob marks a job as completed with optional result data.
//
// Returns false when the row was already settled (in practice: cancelled while
// the executor was finishing). The settled status is left alone — the
// cancellation was a deliberate act by the user, and overwriting it with
// `completed` would report success for work that was stopped.
func (q *Queue) CompleteJob(ctx context.Context, job *Job, result []byte) (bool, error) {
	job.Status = JobStatusCompleted
	now := time.Now()
	job.CompletedAt = &now
	job.Result = result
	job.ProgressPercent = 100

	applied, err := q.store.UpdateSyncJobIfStatus(ctx, job, JobStatusRunning)
	if err != nil {
		return false, fmt.Errorf("failed to complete job: %w", err)
	}
	if !applied {
		q.logger.Warn().
			Str("job_id", job.ID).
			Str("job_type", string(job.JobType)).
			Msg("Job finished but its row was already settled; leaving the settled status alone")
		return false, nil
	}

	q.logger.Info().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Int("processed", job.ProcessedItems).
		Int("failed", job.FailedItems).
		Msg("Job completed")

	return true, nil
}

// FailJob marks a job as failed with an error message.
//
// Returns false when the row was already settled. A cancelled job stays
// `cancelled`: recording it as `failed` would blame the data source for a stop
// the user asked for.
func (q *Queue) FailJob(ctx context.Context, job *Job, errMsg string) (bool, error) {
	job.Status = JobStatusFailed
	job.ErrorMessage = errMsg
	now := time.Now()
	job.CompletedAt = &now

	applied, err := q.store.UpdateSyncJobIfStatus(ctx, job, JobStatusRunning)
	if err != nil {
		return false, fmt.Errorf("failed to mark job as failed: %w", err)
	}
	if !applied {
		q.logger.Warn().
			Str("job_id", job.ID).
			Str("job_type", string(job.JobType)).
			Msg("Job failed but its row was already settled; leaving the settled status alone")
		return false, nil
	}

	q.logger.Warn().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Str("error", errMsg).
		Int("retry_count", job.RetryCount).
		Int("max_retries", job.MaxRetries).
		Msg("Job failed")

	return true, nil
}

// RetryLater requeues a failed job for later retry with exponential backoff.
//
// Returns false when the row was already settled — most importantly, when it
// was cancelled. Requeueing a cancelled job as `pending` would make the worker
// pick it up again and run it a second time, which is the same defect as
// AUD-49 wearing a different hat: a worker-side write undoing a decision made
// elsewhere.
func (q *Queue) RetryLater(ctx context.Context, job *Job, errMsg string) (bool, error) {
	job.RetryCount++
	if job.RetryCount > job.MaxRetries {
		return q.FailJob(ctx, job, fmt.Sprintf("max retries exceeded: %s", errMsg))
	}

	job.Status = JobStatusPending
	job.ErrorMessage = errMsg
	// Exponential backoff: 2^retry_count * 5 seconds
	backoff := time.Duration(1<<uint(job.RetryCount)) * 5 * time.Second
	scheduledAt := time.Now().Add(backoff)
	job.ScheduledAt = &scheduledAt

	applied, err := q.store.UpdateSyncJobIfStatus(ctx, job, JobStatusRunning)
	if err != nil {
		return false, fmt.Errorf("failed to requeue job for retry: %w", err)
	}
	if !applied {
		q.logger.Warn().
			Str("job_id", job.ID).
			Msg("Job was already settled; not requeueing it for retry")
		return false, nil
	}

	q.logger.Info().
		Str("job_id", job.ID).
		Int("retry_count", job.RetryCount).
		Dur("backoff", backoff).
		Time("scheduled_at", scheduledAt).
		Msg("Job scheduled for retry")

	return true, nil
}

// GetJob retrieves a job by ID.
func (q *Queue) GetJob(ctx context.Context, jobID string) (*Job, error) {
	return q.store.GetSyncJob(ctx, jobID)
}

// ListJobs lists jobs filtered by status.
func (q *Queue) ListJobs(ctx context.Context, status JobStatus, limit int) ([]*Job, error) {
	return q.store.ListSyncJobs(ctx, status, limit)
}

// CountPending returns the number of pending jobs.
func (q *Queue) CountPending(ctx context.Context) (int, error) {
	jobs, err := q.store.ListSyncJobs(ctx, JobStatusPending, 1000)
	if err != nil {
		return 0, err
	}
	return len(jobs), nil
}

// Subscribe returns a channel that receives a notification when a new job is enqueued.
// The caller should call Unsubscribe when done to avoid goroutine leaks.
func (q *Queue) Subscribe() chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch := make(chan struct{}, 1)
	q.notifiers = append(q.notifiers, ch)
	return ch
}

// Unsubscribe removes a notification channel.
func (q *Queue) Unsubscribe(ch chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, n := range q.notifiers {
		if n == ch {
			q.notifiers = append(q.notifiers[:i], q.notifiers[i+1:]...)
			close(ch)
			return
		}
	}
}

// notify sends a non-blocking notification to all subscribers.
//
// S7-P0-13 (ODR-043): This acquires the write lock (not RLock) and sends
// while holding it. The previous implementation copied the notifiers slice
// under RLock, released the lock, then sent — which raced with
// Unsubscribe()'s close(ch): the copy could include a channel that
// Unsubscribe closed after the copy but before the send, causing a
// "send on closed channel" panic and a -race report. Holding the write
// lock during the send serializes notify() against Unsubscribe()'s
// close(). The send is non-blocking (default branch), so the lock is
// held for a negligible duration.
func (q *Queue) notify() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, ch := range q.notifiers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// CleanupStaleRunning repairs jobs left in `running` by a process that died
// before it could settle them (SIGKILL, OOM, `docker restart`).
//
// AUD-50. Why this is needed at all: Dequeue queries the database on every
// iteration, but only for `pending`. A row left in `running` is therefore
// invisible to the workers forever — nothing else ever moves it back, and the
// pool sits idle in WaitForJob while the job still looks like it is making
// progress. The observed symptom is a job frozen at N/M with nothing in the log
// but health checks.
//
// The precedent is pkg/backtest/job's CleanupStaleRunning (P0-8). That one is
// wired, but only into gracefulShutdown (cmd/analysis setup.go) — while its own
// doc comment tells the reader to call it "on startup" after a hard crash. No
// startup caller ever existed, so the recommended path was never taken. This is
// the same repair, for the sync queue, and it is wired.
//
// It is invoked from WorkerPool.Start rather than by each caller because it is
// only safe before any worker is running: it decides purely from the database
// and cannot see in-flight jobs held in memory. Putting the call where workers
// come up makes "someone forgot to call it" structurally impossible instead of
// merely documented.
//
// Jobs are marked `failed`, not `cancelled`: nobody chose to stop them, they
// were interrupted, and that distinction matters to whoever reads the ledger
// later. `failed` also leaves them retryable (CanRetry accepts failed), so an
// interrupted bulk sync can be resumed by hand.
//
// Returns the number of rows transitioned from `running` to `failed`.
func (q *Queue) CleanupStaleRunning(ctx context.Context) (int, error) {
	// ListSyncJobs is limit-based; there is no ListByStatus on JobStore. Ask
	// for a wide window instead — a single-user lab accumulates jobs slowly,
	// so one bounded query covers any realistic backlog.
	const cleanupWindowLimit = 1000

	jobs, err := q.store.ListSyncJobs(ctx, JobStatusRunning, cleanupWindowLimit)
	if err != nil {
		return 0, fmt.Errorf("failed to list running jobs for cleanup: %w", err)
	}

	transitioned := 0
	for _, job := range jobs {
		now := time.Now()
		job.Status = JobStatusFailed
		job.CompletedAt = &now
		job.ErrorMessage = "interrupted by a service restart (AUD-50 stale-running cleanup); " +
			"the worker pool only dequeues `pending`, so this row would otherwise stay `running` forever"

		// Conditional on `running` for the same reason every other write here
		// is: between the list above and this write the row may have been
		// settled by whoever owns it, and reaping a live job would be worse
		// than leaving a stale one.
		applied, err := q.store.UpdateSyncJobIfStatus(ctx, job, JobStatusRunning)
		if err != nil {
			q.logger.Error().Err(err).Str("job_id", job.ID).
				Msg("Failed to clean up stale 'running' job")
			continue
		}
		if !applied {
			q.logger.Info().Str("job_id", job.ID).
				Msg("Stale-running candidate was settled by someone else; leaving it alone")
			continue
		}
		transitioned++
		q.logger.Warn().
			Str("job_id", job.ID).
			Str("job_type", string(job.JobType)).
			Int("processed", job.ProcessedItems).
			Int("total", job.TotalItems).
			Msg("Recovered interrupted job: 'running' -> 'failed'")
	}

	if len(jobs) > 0 {
		q.logger.Info().
			Int("transitioned", transitioned).
			Int("scanned", len(jobs)).
			Msg("Stale-running cleanup complete")
	}
	return transitioned, nil
}

// NotifyJobAvailable wakes idle workers blocked in WaitForJob.
//
// Any code path that transitions a job to pending WITHOUT going through
// Queue.Enqueue must call this — otherwise workers that went idle before
// the job existed sleep forever (e2e runtime forensics: JobService.CreateJob
// wrote pending rows to PostgreSQL while all three workers were blocked in
// WaitForJob, so jobs created over HTTP were never dequeued). JobService
// invokes this through the notifier registered via SetPendingNotifier.
func (q *Queue) NotifyJobAvailable() {
	q.notify()
}

// WaitForJob blocks until a new job is available or the context is cancelled.
// Returns true if a job may be available, false if the context was cancelled.
func (q *Queue) WaitForJob(ctx context.Context) bool {
	ch := q.Subscribe()
	defer q.Unsubscribe(ch)

	// Check immediately before waiting
	pending, _ := q.CountPending(ctx)
	if pending > 0 {
		return true
	}

	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}
