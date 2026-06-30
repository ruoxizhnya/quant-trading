// Package types — sync job and schedule data types (leaf package).
//
// S7-P1-2 (ODR-043): Job, Schedule, JobStatus, and JobType previously
// lived in pkg/sync and were imported by pkg/storage (sync_jobs.go),
// creating a reverse dependency (storage → sync). storage is the lower
// persistence layer; sync is the orchestration layer above it.
//
// This leaf package holds only the data structs and enums (no heavy
// deps like logging, context, or zerolog). Both pkg/sync (which adds
// business logic + workers) and pkg/storage (which persists these
// types) import from here, breaking the cycle. pkg/sync re-exports
// the symbols via aliases for backward compatibility.
package types

import (
	"encoding/json"
	"time"
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

// Schedule represents a scheduled sync job configuration.
type Schedule struct {
	ID             int             `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	JobType        string          `json:"job_type"`
	CronExpression string          `json:"cron_expression"`
	Params         json.RawMessage `json:"params"`
	IsActive       bool            `json:"is_active"`
	LastRunAt      *time.Time      `json:"last_run_at,omitempty"`
	LastRunStatus  *string         `json:"last_run_status,omitempty"`
	LastRunJobID   *string         `json:"last_run_job_id,omitempty"`
	NextRunAt      *time.Time      `json:"next_run_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CreatedBy      string          `json:"created_by,omitempty"`
}
