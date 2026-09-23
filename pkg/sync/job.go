// Package sync provides a task queue and scheduler for data synchronization jobs.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/sync/types"
)

// S7-P1-2 (ODR-043): Job, JobStatus, JobType, and Schedule now live in
// pkg/sync/types (a leaf package with no heavy deps). These aliases
// preserve backward compatibility for all existing `sync.Job`,
// `sync.JobStatus`, etc. references. The canonical definitions and
// methods (IsTerminal, CanRetry, UpdateProgress, Clone) are in
// pkg/sync/types. This breaks the storage → sync reverse dependency:
// pkg/storage now imports pkg/sync/types directly.

// JobStatus is an alias for types.JobStatus.
type JobStatus = types.JobStatus

const (
	JobStatusPending   = types.JobStatusPending
	JobStatusRunning   = types.JobStatusRunning
	JobStatusCompleted = types.JobStatusCompleted
	JobStatusFailed    = types.JobStatusFailed
	JobStatusCancelled = types.JobStatusCancelled
	JobStatusRetrying  = types.JobStatusRetrying
)

// JobType is an alias for types.JobType.
type JobType = types.JobType

const (
	JobTypeStocks       = types.JobTypeStocks
	JobTypeOHLCV        = types.JobTypeOHLCV
	JobTypeOHLCVAll     = types.JobTypeOHLCVAll
	JobTypeFundamentals = types.JobTypeFundamentals
	JobTypeFundamental  = types.JobTypeFundamental
	JobTypeDividends    = types.JobTypeDividends
	JobTypeSplits       = types.JobTypeSplits
	JobTypeCalendar     = types.JobTypeCalendar
	JobTypeFactors      = types.JobTypeFactors
	JobTypeFactor       = types.JobTypeFactor
	JobTypeFactorAttr   = types.JobTypeFactorAttr
	JobTypeFactorIC     = types.JobTypeFactorIC
	JobTypeIndexConst   = types.JobTypeIndexConst
)

// Job is an alias for types.Job. Methods (IsTerminal, CanRetry,
// UpdateProgress, Clone) are defined on types.Job.
type Job = types.Job

// JobStore defines the interface for job persistence.
type JobStore interface {
	CreateSyncJob(ctx context.Context, job *Job) error
	GetSyncJob(ctx context.Context, jobID string) (*Job, error)
	// UpdateSyncJobIfStatus persists the job only when its *current* status is
	// one of `from`, reporting whether the write landed. There is deliberately
	// no unconditional update: every writer here is racing someone else
	// (progress reports vs. cancel vs. a second worker), and an unconditional
	// write silently wins that race with stale in-memory state. See AUD-49.
	UpdateSyncJobIfStatus(ctx context.Context, job *Job, from ...JobStatus) (bool, error)
	ListSyncJobs(ctx context.Context, status JobStatus, limit int) ([]*Job, error)
	ListSyncJobsByType(ctx context.Context, jobType JobType, limit int) ([]*Job, error)
	DeleteSyncJob(ctx context.Context, jobID string) error
}

// JobService handles sync job lifecycle management.
type JobService struct {
	store       JobStore
	logger      zerolog.Logger
	onPending   func()            // optional; invoked when a job transitions to pending
	onCancelRun func(string) bool // optional; interrupts a job executing in this process
}

// NewJobService creates a new JobService.
func NewJobService(store JobStore) *JobService {
	return &JobService{
		store:  store,
		logger: logging.WithContext(map[string]any{"component": "sync_job_service"}),
	}
}

// SetPendingNotifier registers a callback invoked after a job transitions
// to pending (CreateJob / RetryJob). The in-process Queue uses this to wake
// idle workers blocked in WaitForJob — without it, workers that went idle
// before the job was created never observe the new pending row.
// The callback must be set once during wiring, before concurrent use.
func (s *JobService) SetPendingNotifier(fn func()) {
	s.onPending = fn
}

// notifyPending invokes the pending notifier if one is registered.
func (s *JobService) notifyPending() {
	if s.onPending != nil {
		s.onPending()
	}
}

// SetRunningCanceller registers the callback CancelJob uses to interrupt a job
// that is executing *right now* in this process. It returns true when the job
// was found running here and its context was cancelled.
//
// Why this exists (AUD-49): marking the row `cancelled` only tells the
// database. The executor is a separate goroutine holding its own context and
// its own copy of the job, and nothing short of cancelling that context makes
// it stop — the endpoint would answer 200 while the sync kept hammering
// Tushare. A job that is `pending` has no in-flight context yet, so a false
// return is normal there, not an error.
//
// Must be set once during wiring, before concurrent use.
func (s *JobService) SetRunningCanceller(fn func(jobID string) bool) {
	s.onCancelRun = fn
}

// CreateJob creates a new sync job.
func (s *JobService) CreateJob(ctx context.Context, jobType JobType, params any) (*Job, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	job := &Job{
		ID:         uuid.New().String(),
		JobType:    jobType,
		Status:     JobStatusPending,
		Params:     paramsJSON,
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}

	if err := s.store.CreateSyncJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create sync job: %w", err)
	}

	s.logger.Info().
		Str("job_id", job.ID).
		Str("job_type", string(jobType)).
		Msg("Sync job created")
	s.notifyPending()

	return job, nil
}

// GetJob retrieves a sync job by ID.
func (s *JobService) GetJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := s.store.GetSyncJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sync job: %w", err)
	}
	return job, nil
}

// ListJobs lists sync jobs filtered by status.
func (s *JobService) ListJobs(ctx context.Context, status JobStatus, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.store.ListSyncJobs(ctx, status, limit)
}

// ListJobsByType lists sync jobs filtered by job type.
func (s *JobService) ListJobsByType(ctx context.Context, jobType JobType, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.store.ListSyncJobsByType(ctx, jobType, limit)
}

// CancelJob cancels a pending or running job.
//
// AUD-49. Two halves are needed and neither is sufficient alone:
//
//	① the row must become `cancelled` and *stay* that way — which is why the
//	   write is conditional (only from pending/running) and why every
//	   worker-side write is conditional on `running` too;
//	② the executor must actually stop — which is why the canceller is invoked.
//
// Order: settle the row first, then signal. Because every worker-side write is
// conditional on `running`, once the row says `cancelled` the in-flight worker
// can no longer overwrite it — not on its next progress report, not on its
// completion, not via a retry. Signalling first would open a window where the
// job completes legitimately and the cancel then reports a confusing failure.
func (s *JobService) CancelJob(ctx context.Context, jobID string) error {
	job, err := s.store.GetSyncJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job for cancellation: %w", err)
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}
	if job.IsTerminal() {
		return fmt.Errorf("job is already in terminal state: %s", job.Status)
	}

	wasRunning := job.Status == JobStatusRunning

	job.Status = JobStatusCancelled
	now := time.Now()
	job.CompletedAt = &now

	applied, err := s.store.UpdateSyncJobIfStatus(ctx, job, JobStatusPending, JobStatusRunning)
	if err != nil {
		return fmt.Errorf("failed to cancel job: %w", err)
	}
	if !applied {
		// The job moved between our read and our write — it finished, failed,
		// or another canceller got there first. Report what it actually is
		// instead of claiming a cancellation that did not happen.
		current, getErr := s.store.GetSyncJob(ctx, jobID)
		if getErr == nil && current != nil {
			return fmt.Errorf("job is already in terminal state: %s", current.Status)
		}
		return fmt.Errorf("failed to cancel job %s: status changed concurrently", jobID)
	}

	if wasRunning && s.onCancelRun != nil {
		if !s.onCancelRun(jobID) {
			// The row said `running` but no live execution owns it here. That
			// is the state a crashed process leaves behind, and AUD-50's
			// CleanupStaleRunning reaps it on the next start. Worth a warning:
			// the row is cancelled either way, but nothing was actually
			// interrupted.
			s.logger.Warn().
				Str("job_id", jobID).
				Msg("Job was marked cancelled but no in-flight execution was found to interrupt; " +
					"it was probably left 'running' by a previous process (see AUD-50)")
		}
	}

	s.logger.Info().Str("job_id", jobID).Bool("was_running", wasRunning).Msg("Sync job cancelled")
	return nil
}

// RetryJob retries a failed job.
func (s *JobService) RetryJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := s.store.GetSyncJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job for retry: %w", err)
	}
	if job == nil {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	if !job.CanRetry() {
		return nil, fmt.Errorf("job cannot be retried (status=%s, retries=%d/%d)", job.Status, job.RetryCount, job.MaxRetries)
	}

	// Capture the status we read before overwriting it: the conditional write
	// below has to name the states we are allowed to move out of.
	fromStatus := job.Status

	job.Status = JobStatusPending
	job.RetryCount++
	job.ErrorMessage = ""
	job.ProgressPercent = 0
	job.ProcessedItems = 0
	job.FailedItems = 0
	job.WorkerID = ""

	applied, err := s.store.UpdateSyncJobIfStatus(ctx, job, fromStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to retry job: %w", err)
	}
	if !applied {
		// Someone else moved the job (typically a worker already picked it up
		// after another retry). Resetting it to pending here would clobber
		// live progress and could run the same job twice.
		return nil, fmt.Errorf("failed to retry job %s: status changed concurrently", jobID)
	}

	s.logger.Info().
		Str("job_id", jobID).
		Int("retry_count", job.RetryCount).
		Msg("Sync job queued for retry")
	s.notifyPending()

	return job, nil
}

// JobParams helpers for different job types.

// StocksSyncParams parameters for stocks sync job.
type StocksSyncParams struct {
	Exchange   string `json:"exchange,omitempty"`
	ListStatus string `json:"list_status,omitempty"`
}

// OHLCVSyncParams parameters for OHLCV sync job.
type OHLCVSyncParams struct {
	Symbols      []string `json:"symbols,omitempty"`
	StartDate    string   `json:"start_date,omitempty"`
	EndDate      string   `json:"end_date,omitempty"`
	BatchSize    int      `json:"batch_size,omitempty"`
	SkipExisting bool     `json:"skip_existing,omitempty"`
}

// FundamentalSyncParams parameters for fundamental sync job.
type FundamentalSyncParams struct {
	Symbols []string `json:"symbols,omitempty"`
	Date    string   `json:"date,omitempty"`
}

// CalendarSyncParams parameters for calendar sync job.
type CalendarSyncParams struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Exchange  string `json:"exchange,omitempty"`
}

// FactorSyncParams parameters for factor sync job.
type FactorSyncParams struct {
	FactorName string `json:"factor_name,omitempty"`
	Date       string `json:"date,omitempty"`
}

// ParseJobParams parses job params into the appropriate struct based on job type.
func ParseJobParams(jobType JobType, params json.RawMessage) (any, error) {
	switch jobType {
	case JobTypeStocks:
		var p StocksSyncParams
		err := json.Unmarshal(params, &p)
		return &p, err
	case JobTypeOHLCV, JobTypeOHLCVAll:
		var p OHLCVSyncParams
		err := json.Unmarshal(params, &p)
		return &p, err
	case JobTypeFundamentals, JobTypeFundamental:
		var p FundamentalSyncParams
		err := json.Unmarshal(params, &p)
		return &p, err
	case JobTypeCalendar:
		var p CalendarSyncParams
		err := json.Unmarshal(params, &p)
		return &p, err
	case JobTypeFactors, JobTypeFactor:
		var p FactorSyncParams
		err := json.Unmarshal(params, &p)
		return &p, err
	default:
		var p map[string]any
		err := json.Unmarshal(params, &p)
		return p, err
	}
}
