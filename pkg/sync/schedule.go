package sync

import (
	"context"
	"encoding/json"
	"time"
)

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

// ScheduleStore defines the interface for schedule persistence.
type ScheduleStore interface {
	CreateSyncSchedule(ctx context.Context, schedule *Schedule) error
	GetSyncSchedule(ctx context.Context, id int) (*Schedule, error)
	GetSyncScheduleByName(ctx context.Context, name string) (*Schedule, error)
	UpdateSyncSchedule(ctx context.Context, schedule *Schedule) error
	DeleteSyncSchedule(ctx context.Context, id int) error
	ListSyncSchedules(ctx context.Context, activeOnly bool) ([]*Schedule, error)
}
