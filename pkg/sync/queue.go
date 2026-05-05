// Package sync provides a task queue and scheduler for data synchronization jobs.
package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/rs/zerolog"
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

	if err := q.store.UpdateSyncJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to mark job as running: %w", err)
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

// UpdateJob updates an existing job in the queue.
func (q *Queue) UpdateJob(ctx context.Context, job *Job) error {
	if err := q.store.UpdateSyncJob(ctx, job); err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}
	return nil
}

// CompleteJob marks a job as completed with optional result data.
func (q *Queue) CompleteJob(ctx context.Context, job *Job, result []byte) error {
	job.Status = JobStatusCompleted
	now := time.Now()
	job.CompletedAt = &now
	job.Result = result
	job.ProgressPercent = 100

	if err := q.store.UpdateSyncJob(ctx, job); err != nil {
		return fmt.Errorf("failed to complete job: %w", err)
	}

	q.logger.Info().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Int("processed", job.ProcessedItems).
		Int("failed", job.FailedItems).
		Msg("Job completed")

	return nil
}

// FailJob marks a job as failed with an error message.
func (q *Queue) FailJob(ctx context.Context, job *Job, errMsg string) error {
	job.Status = JobStatusFailed
	job.ErrorMessage = errMsg
	now := time.Now()
	job.CompletedAt = &now

	if err := q.store.UpdateSyncJob(ctx, job); err != nil {
		return fmt.Errorf("failed to mark job as failed: %w", err)
	}

	q.logger.Warn().
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Str("error", errMsg).
		Int("retry_count", job.RetryCount).
		Int("max_retries", job.MaxRetries).
		Msg("Job failed")

	return nil
}

// RetryLater requeues a failed job for later retry with exponential backoff.
func (q *Queue) RetryLater(ctx context.Context, job *Job, errMsg string) error {
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

	if err := q.store.UpdateSyncJob(ctx, job); err != nil {
		return fmt.Errorf("failed to requeue job for retry: %w", err)
	}

	q.logger.Info().
		Str("job_id", job.ID).
		Int("retry_count", job.RetryCount).
		Dur("backoff", backoff).
		Time("scheduled_at", scheduledAt).
		Msg("Job scheduled for retry")

	return nil
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
func (q *Queue) notify() {
	q.mu.RLock()
	notifiers := make([]chan struct{}, len(q.notifiers))
	copy(notifiers, q.notifiers)
	q.mu.RUnlock()

	for _, ch := range notifiers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
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
