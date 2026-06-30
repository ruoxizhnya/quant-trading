package sync

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExecutor is a test executor that can simulate success, failure, or slow execution.
type mockExecutor struct {
	mu           sync.Mutex
	jobType      JobType
	executeFunc  func(ctx context.Context, job *Job, progress ProgressReporter) (any, error)
	executedJobs []*Job
}

func (e *mockExecutor) Execute(ctx context.Context, job *Job, progress ProgressReporter) (any, error) {
	e.mu.Lock()
	e.executedJobs = append(e.executedJobs, job)
	e.mu.Unlock()
	if e.executeFunc != nil {
		return e.executeFunc(ctx, job, progress)
	}
	return map[string]any{"status": "ok"}, nil
}

func (e *mockExecutor) JobType() JobType {
	return e.jobType
}

func TestWorkerPool_RegisterExecutor(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	pool := NewWorkerPool(queue, 2)

	exec := &mockExecutor{jobType: JobTypeStocks}
	pool.RegisterExecutor(exec)

	found, ok := pool.GetExecutor(JobTypeStocks)
	require.True(t, ok)
	assert.Equal(t, JobTypeStocks, found.JobType())

	assert.True(t, pool.HasExecutor(JobTypeStocks))
	assert.False(t, pool.HasExecutor(JobTypeOHLCV))
}

func TestWorkerPool_Stats(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	pool := NewWorkerPool(queue, 3)

	stats := pool.Stats()
	assert.Equal(t, 3, stats.NumWorkers)
	assert.Equal(t, 0, len(stats.RegisteredTypes))
	assert.True(t, stats.IsRunning)

	pool.RegisterExecutor(&mockExecutor{jobType: JobTypeStocks})
	pool.RegisterExecutor(&mockExecutor{jobType: JobTypeOHLCV})

	stats = pool.Stats()
	assert.Equal(t, 2, len(stats.RegisteredTypes))
}

func TestWorkerPool_StartStop(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	pool := NewWorkerPool(queue, 2)

	// Start the pool — Start() sets IsRunning synchronously before returning.
	pool.Start()

	// Should be running
	stats := pool.Stats()
	assert.True(t, stats.IsRunning)

	// Stop the pool
	pool.Stop()

	// Should be stopped
	stats = pool.Stats()
	assert.False(t, stats.IsRunning)
}

func TestWorkerPool_ProcessJob_Success(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	pool := NewWorkerPool(queue, 1)
	exec := &mockExecutor{jobType: JobTypeStocks}
	pool.RegisterExecutor(exec)

	// Create and enqueue a job
	job := &Job{ID: "job-1", JobType: JobTypeStocks, Status: JobStatusPending, MaxRetries: 3}
	queue.Enqueue(ctx, job)

	// Start pool, poll for completion, then stop.
	pool.Start()
	require.Eventually(t, func() bool {
		stored, _ := store.GetSyncJob(ctx, "job-1")
		return stored != nil && stored.Status == JobStatusCompleted
	}, 5*time.Second, 10*time.Millisecond, "job should reach Completed status")
	pool.Stop()

	// Verify job was executed
	require.Len(t, exec.executedJobs, 1)
	assert.Equal(t, "job-1", exec.executedJobs[0].ID)

	// Verify job was marked completed
	stored, _ := store.GetSyncJob(ctx, "job-1")
	require.NotNil(t, stored)
	assert.Equal(t, JobStatusCompleted, stored.Status)
	assert.Equal(t, 100, stored.ProgressPercent)
}

func TestWorkerPool_ProcessJob_NoExecutor(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	pool := NewWorkerPool(queue, 1)
	// Don't register any executor

	// Create and enqueue a job
	job := &Job{ID: "job-1", JobType: JobTypeStocks, Status: JobStatusPending, MaxRetries: 3}
	queue.Enqueue(ctx, job)

	// Start pool, poll for failure, then stop.
	pool.Start()
	require.Eventually(t, func() bool {
		stored, _ := store.GetSyncJob(ctx, "job-1")
		return stored != nil && stored.Status == JobStatusFailed
	}, 5*time.Second, 10*time.Millisecond, "job should reach Failed status")
	pool.Stop()

	// Verify job was marked failed
	stored, _ := store.GetSyncJob(ctx, "job-1")
	require.NotNil(t, stored)
	assert.Equal(t, JobStatusFailed, stored.Status)
	assert.Contains(t, stored.ErrorMessage, "no executor registered")
}

func TestWorkerPool_ProcessJob_ExecutorError(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	pool := NewWorkerPool(queue, 1)
	exec := &mockExecutor{
		jobType: JobTypeStocks,
		executeFunc: func(ctx context.Context, job *Job, progress ProgressReporter) (any, error) {
			return nil, assert.AnError
		},
	}
	pool.RegisterExecutor(exec)

	// Create and enqueue a job with MaxRetries=3, RetryCount=0
	// The worker will retry until max retries is exceeded
	job := &Job{ID: "job-1", JobType: JobTypeStocks, Status: JobStatusPending, MaxRetries: 3, RetryCount: 0}
	queue.Enqueue(ctx, job)

	// Start pool, poll for terminal failure after retries, then stop.
	pool.Start()
	require.Eventually(t, func() bool {
		stored, _ := store.GetSyncJob(ctx, "job-1")
		return stored != nil && stored.Status == JobStatusFailed
	}, 15*time.Second, 10*time.Millisecond, "job should reach Failed status after exhausting retries")
	pool.Stop()

	// Verify job eventually failed after exhausting retries
	stored, _ := store.GetSyncJob(ctx, "job-1")
	require.NotNil(t, stored)
	// After multiple retries, job should eventually be failed
	assert.Equal(t, JobStatusFailed, stored.Status)
	// Should have been retried multiple times (retry count incremented each time)
	assert.GreaterOrEqual(t, stored.RetryCount, 1)
}

func TestWorkerPool_ProcessJob_MaxRetriesExceeded(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	pool := NewWorkerPool(queue, 1)
	exec := &mockExecutor{
		jobType: JobTypeStocks,
		executeFunc: func(ctx context.Context, job *Job, progress ProgressReporter) (any, error) {
			return nil, assert.AnError
		},
	}
	pool.RegisterExecutor(exec)

	// Create and enqueue a job at max retries
	job := &Job{ID: "job-1", JobType: JobTypeStocks, Status: JobStatusPending, MaxRetries: 2, RetryCount: 2}
	queue.Enqueue(ctx, job)

	// Start pool, poll for failure, then stop.
	pool.Start()
	require.Eventually(t, func() bool {
		stored, _ := store.GetSyncJob(ctx, "job-1")
		return stored != nil && stored.Status == JobStatusFailed
	}, 5*time.Second, 10*time.Millisecond, "job should reach Failed status")
	pool.Stop()

	// Verify job was marked failed
	stored, _ := store.GetSyncJob(ctx, "job-1")
	require.NotNil(t, stored)
	assert.Equal(t, JobStatusFailed, stored.Status)
}

func TestWorkerPool_MultipleJobs(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	pool := NewWorkerPool(queue, 2)
	exec := &mockExecutor{jobType: JobTypeStocks}
	pool.RegisterExecutor(exec)

	// Enqueue multiple jobs
	for i := 0; i < 5; i++ {
		job := &Job{ID: fmt.Sprintf("job-%d", i), JobType: JobTypeStocks, Status: JobStatusPending, MaxRetries: 3}
		queue.Enqueue(ctx, job)
	}

	// Start pool, poll until all 5 jobs complete, then stop.
	pool.Start()
	require.Eventually(t, func() bool {
		for i := 0; i < 5; i++ {
			stored, _ := store.GetSyncJob(ctx, fmt.Sprintf("job-%d", i))
			if stored == nil || stored.Status != JobStatusCompleted {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond, "all 5 jobs should reach Completed status")
	pool.Stop()

	// Verify all jobs were executed at least once
	assert.GreaterOrEqual(t, len(exec.executedJobs), 5)

	// Verify all jobs were marked completed (not pending/failed)
	for i := 0; i < 5; i++ {
		stored, _ := store.GetSyncJob(ctx, fmt.Sprintf("job-%d", i))
		require.NotNil(t, stored)
		assert.Equal(t, JobStatusCompleted, stored.Status)
	}
}

func TestWorkerPool_ProgressReporting(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	pool := NewWorkerPool(queue, 1)
	exec := &mockExecutor{
		jobType: JobTypeStocks,
		executeFunc: func(ctx context.Context, job *Job, progress ProgressReporter) (any, error) {
			progress.ReportProgress(50, 100, 0)
			return map[string]any{"count": 100}, nil
		},
	}
	pool.RegisterExecutor(exec)

	job := &Job{ID: "job-1", JobType: JobTypeStocks, Status: JobStatusPending, MaxRetries: 3}
	queue.Enqueue(ctx, job)

	pool.Start()
	require.Eventually(t, func() bool {
		stored, _ := store.GetSyncJob(ctx, "job-1")
		return stored != nil && stored.Status == JobStatusCompleted
	}, 5*time.Second, 10*time.Millisecond, "job should reach Completed status")
	pool.Stop()

	// Job should be completed
	stored, _ := store.GetSyncJob(ctx, "job-1")
	require.NotNil(t, stored)
	assert.Equal(t, JobStatusCompleted, stored.Status)
}
