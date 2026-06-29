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
)

// JobStatus represents the current state of a sync job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
	JobStatusRetrying  JobStatus = "retrying"
)

// JobType represents the type of data synchronization job.
type JobType string

const (
	JobTypeStocks       JobType = "stocks"
	JobTypeOHLCV        JobType = "ohlcv"
	JobTypeOHLCVAll     JobType = "ohlcv_all"
	JobTypeFundamentals JobType = "fundamentals"
	JobTypeFundamental  JobType = "fundamental"
	JobTypeDividends    JobType = "dividends"
	JobTypeSplits       JobType = "splits"
	JobTypeCalendar     JobType = "calendar"
	JobTypeFactors      JobType = "factors"
	JobTypeFactor       JobType = "factor"
	JobTypeFactorAttr   JobType = "factor_attribution"
	JobTypeFactorIC     JobType = "factor_ic"
	JobTypeIndexConst   JobType = "index_constituents"
)

// Job represents a single data synchronization task.
type Job struct {
	ID              string          `json:"id"`
	JobType         JobType         `json:"job_type"`
	Status          JobStatus       `json:"status"`
	Params          json.RawMessage `json:"params"`
	ProgressPercent int             `json:"progress_percent"`
	TotalItems      int             `json:"total_items"`
	ProcessedItems  int             `json:"processed_items"`
	FailedItems     int             `json:"failed_items"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	RetryCount      int             `json:"retry_count"`
	MaxRetries      int             `json:"max_retries"`
	ScheduledAt     *time.Time      `json:"scheduled_at,omitempty"`
	WorkerID        string          `json:"worker_id,omitempty"`
}

// IsTerminal returns true if the job status is a terminal state.
func (j *Job) IsTerminal() bool {
	return j.Status == JobStatusCompleted || j.Status == JobStatusFailed || j.Status == JobStatusCancelled
}

// CanRetry returns true if the job can be retried.
func (j *Job) CanRetry() bool {
	return j.RetryCount < j.MaxRetries && (j.Status == JobStatusFailed || j.Status == JobStatusPending)
}

// UpdateProgress updates the job progress.
func (j *Job) UpdateProgress(processed, total, failed int) {
	j.ProcessedItems = processed
	j.TotalItems = total
	j.FailedItems = failed
	if total > 0 {
		j.ProgressPercent = (processed * 100) / total
	}
}

// Clone returns a deep copy of the Job.
func (j *Job) Clone() *Job {
	if j == nil {
		return nil
	}
	clone := &Job{
		ID:              j.ID,
		JobType:         j.JobType,
		Status:          j.Status,
		Params:          append(json.RawMessage(nil), j.Params...),
		ProgressPercent: j.ProgressPercent,
		TotalItems:      j.TotalItems,
		ProcessedItems:  j.ProcessedItems,
		FailedItems:     j.FailedItems,
		ErrorMessage:    j.ErrorMessage,
		Result:          append(json.RawMessage(nil), j.Result...),
		CreatedAt:       j.CreatedAt,
		RetryCount:      j.RetryCount,
		MaxRetries:      j.MaxRetries,
		WorkerID:        j.WorkerID,
	}
	if j.StartedAt != nil {
		t := *j.StartedAt
		clone.StartedAt = &t
	}
	if j.CompletedAt != nil {
		t := *j.CompletedAt
		clone.CompletedAt = &t
	}
	if j.ScheduledAt != nil {
		t := *j.ScheduledAt
		clone.ScheduledAt = &t
	}
	return clone
}

// JobStore defines the interface for job persistence.
type JobStore interface {
	CreateSyncJob(ctx context.Context, job *Job) error
	GetSyncJob(ctx context.Context, jobID string) (*Job, error)
	UpdateSyncJob(ctx context.Context, job *Job) error
	ListSyncJobs(ctx context.Context, status JobStatus, limit int) ([]*Job, error)
	ListSyncJobsByType(ctx context.Context, jobType JobType, limit int) ([]*Job, error)
	DeleteSyncJob(ctx context.Context, jobID string) error
}

// JobService handles sync job lifecycle management.
type JobService struct {
	store  JobStore
	logger zerolog.Logger
}

// NewJobService creates a new JobService.
func NewJobService(store JobStore) *JobService {
	return &JobService{
		store:  store,
		logger: logging.WithContext(map[string]any{"component": "sync_job_service"}),
	}
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

	job.Status = JobStatusCancelled
	now := time.Now()
	job.CompletedAt = &now

	if err := s.store.UpdateSyncJob(ctx, job); err != nil {
		return fmt.Errorf("failed to cancel job: %w", err)
	}

	s.logger.Info().Str("job_id", jobID).Msg("Sync job cancelled")
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

	job.Status = JobStatusPending
	job.RetryCount++
	job.ErrorMessage = ""
	job.ProgressPercent = 0
	job.ProcessedItems = 0
	job.FailedItems = 0
	job.WorkerID = ""

	if err := s.store.UpdateSyncJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to retry job: %w", err)
	}

	s.logger.Info().
		Str("job_id", jobID).
		Int("retry_count", job.RetryCount).
		Msg("Sync job queued for retry")

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
