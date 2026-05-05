package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/sync"
)

// CreateSyncJob inserts a new sync job.
func (s *PostgresStore) CreateSyncJob(ctx context.Context, job *sync.Job) error {
	query := `
		INSERT INTO sync_jobs (
			id, job_type, status, params, progress_percent, total_items, processed_items, failed_items,
			error_message, result, created_at, started_at, completed_at, retry_count, max_retries, scheduled_at, worker_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`
	_, err := s.pool.Exec(ctx, query,
		job.ID, job.JobType, job.Status, job.Params, job.ProgressPercent, job.TotalItems,
		job.ProcessedItems, job.FailedItems, job.ErrorMessage, job.Result, job.CreatedAt,
		job.StartedAt, job.CompletedAt, job.RetryCount, job.MaxRetries, job.ScheduledAt, job.WorkerID,
	)
	if err != nil {
		return fmt.Errorf("failed to create sync job: %w", err)
	}
	return nil
}

// GetSyncJob retrieves a sync job by ID.
func (s *PostgresStore) GetSyncJob(ctx context.Context, jobID string) (*sync.Job, error) {
	query := `
		SELECT id, job_type, status, params, progress_percent, total_items, processed_items, failed_items,
			error_message, result, created_at, started_at, completed_at, retry_count, max_retries, scheduled_at, worker_id
		FROM sync_jobs WHERE id = $1
	`
	job := &sync.Job{}
	var errMsg, workerID *string
	var startedAt, completedAt, scheduledAt *time.Time
	var result []byte

	err := s.pool.QueryRow(ctx, query, jobID).Scan(
		&job.ID, &job.JobType, &job.Status, &job.Params, &job.ProgressPercent, &job.TotalItems,
		&job.ProcessedItems, &job.FailedItems, &errMsg, &result, &job.CreatedAt,
		&startedAt, &completedAt, &job.RetryCount, &job.MaxRetries, &scheduledAt, &workerID,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sync job: %w", err)
	}

	if errMsg != nil {
		job.ErrorMessage = *errMsg
	}
	if workerID != nil {
		job.WorkerID = *workerID
	}
	if startedAt != nil {
		job.StartedAt = startedAt
	}
	if completedAt != nil {
		job.CompletedAt = completedAt
	}
	if scheduledAt != nil {
		job.ScheduledAt = scheduledAt
	}
	if len(result) > 0 {
		job.Result = result
	}

	return job, nil
}

// UpdateSyncJob updates an existing sync job.
func (s *PostgresStore) UpdateSyncJob(ctx context.Context, job *sync.Job) error {
	query := `
		UPDATE sync_jobs SET
			status = $2, params = $3, progress_percent = $4, total_items = $5, processed_items = $6,
			failed_items = $7, error_message = $8, result = $9, started_at = $10, completed_at = $11,
			retry_count = $12, max_retries = $13, scheduled_at = $14, worker_id = $15
		WHERE id = $1
	`
	_, err := s.pool.Exec(ctx, query,
		job.ID, job.Status, job.Params, job.ProgressPercent, job.TotalItems,
		job.ProcessedItems, job.FailedItems, job.ErrorMessage, job.Result,
		job.StartedAt, job.CompletedAt, job.RetryCount, job.MaxRetries, job.ScheduledAt, job.WorkerID,
	)
	if err != nil {
		return fmt.Errorf("failed to update sync job: %w", err)
	}
	return nil
}

// ListSyncJobs returns sync jobs filtered by status.
func (s *PostgresStore) ListSyncJobs(ctx context.Context, status sync.JobStatus, limit int) ([]*sync.Job, error) {
	var query string
	var args []any

	if status == "" {
		query = `
			SELECT id, job_type, status, params, progress_percent, total_items, processed_items, failed_items,
				error_message, result, created_at, started_at, completed_at, retry_count, max_retries, scheduled_at, worker_id
			FROM sync_jobs ORDER BY created_at DESC LIMIT $1
		`
		args = append(args, limit)
	} else {
		query = `
			SELECT id, job_type, status, params, progress_percent, total_items, processed_items, failed_items,
				error_message, result, created_at, started_at, completed_at, retry_count, max_retries, scheduled_at, worker_id
			FROM sync_jobs WHERE status = $1 ORDER BY created_at DESC LIMIT $2
		`
		args = append(args, status, limit)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list sync jobs: %w", err)
	}
	defer rows.Close()

	return s.scanSyncJobs(rows)
}

// ListSyncJobsByType returns sync jobs filtered by job type.
func (s *PostgresStore) ListSyncJobsByType(ctx context.Context, jobType sync.JobType, limit int) ([]*sync.Job, error) {
	query := `
		SELECT id, job_type, status, params, progress_percent, total_items, processed_items, failed_items,
			error_message, result, created_at, started_at, completed_at, retry_count, max_retries, scheduled_at, worker_id
		FROM sync_jobs WHERE job_type = $1 ORDER BY created_at DESC LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, jobType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list sync jobs by type: %w", err)
	}
	defer rows.Close()

	return s.scanSyncJobs(rows)
}

// DeleteSyncJob deletes a sync job by ID.
func (s *PostgresStore) DeleteSyncJob(ctx context.Context, jobID string) error {
	query := `DELETE FROM sync_jobs WHERE id = $1`
	result, err := s.pool.Exec(ctx, query, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete sync job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("sync job not found: %s", jobID)
	}
	return nil
}

// scanSyncJobs scans rows into a slice of sync.Job.
func (s *PostgresStore) scanSyncJobs(rows pgx.Rows) ([]*sync.Job, error) {
	var jobs []*sync.Job
	for rows.Next() {
		job := &sync.Job{}
		var errMsg, workerID *string
		var startedAt, completedAt, scheduledAt *time.Time
		var result []byte

		if err := rows.Scan(
			&job.ID, &job.JobType, &job.Status, &job.Params, &job.ProgressPercent, &job.TotalItems,
			&job.ProcessedItems, &job.FailedItems, &errMsg, &result, &job.CreatedAt,
			&startedAt, &completedAt, &job.RetryCount, &job.MaxRetries, &scheduledAt, &workerID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan sync job row: %w", err)
		}

		if errMsg != nil {
			job.ErrorMessage = *errMsg
		}
		if workerID != nil {
			job.WorkerID = *workerID
		}
		if startedAt != nil {
			job.StartedAt = startedAt
		}
		if completedAt != nil {
			job.CompletedAt = completedAt
		}
		if scheduledAt != nil {
			job.ScheduledAt = scheduledAt
		}
		if len(result) > 0 {
			job.Result = result
		}

		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ---- Sync Schedule Store Methods ----

// CreateSyncSchedule inserts a new sync schedule.
func (s *PostgresStore) CreateSyncSchedule(ctx context.Context, schedule *sync.Schedule) error {
	query := `
		INSERT INTO sync_schedules (
			name, description, job_type, cron_expression, params, is_active, next_run_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`
	err := s.pool.QueryRow(ctx, query,
		schedule.Name, schedule.Description, schedule.JobType, schedule.CronExpression,
		schedule.Params, schedule.IsActive, schedule.NextRunAt, schedule.CreatedBy,
	).Scan(&schedule.ID, &schedule.CreatedAt, &schedule.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create sync schedule: %w", err)
	}
	return nil
}

// GetSyncSchedule retrieves a sync schedule by ID.
func (s *PostgresStore) GetSyncSchedule(ctx context.Context, id int) (*sync.Schedule, error) {
	query := `
		SELECT id, name, description, job_type, cron_expression, params, is_active,
			last_run_at, last_run_status, last_run_job_id, next_run_at, created_at, updated_at, created_by
		FROM sync_schedules WHERE id = $1
	`
	schedule := &sync.Schedule{}
	var lastRunAt, nextRunAt *time.Time
	var lastRunStatus, lastRunJobID *string

	err := s.pool.QueryRow(ctx, query, id).Scan(
		&schedule.ID, &schedule.Name, &schedule.Description, &schedule.JobType, &schedule.CronExpression,
		&schedule.Params, &schedule.IsActive, &lastRunAt, &lastRunStatus, &lastRunJobID,
		&nextRunAt, &schedule.CreatedAt, &schedule.UpdatedAt, &schedule.CreatedBy,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sync schedule: %w", err)
	}

	if lastRunAt != nil {
		schedule.LastRunAt = lastRunAt
	}
	if lastRunStatus != nil {
		schedule.LastRunStatus = lastRunStatus
	}
	if lastRunJobID != nil {
		schedule.LastRunJobID = lastRunJobID
	}
	if nextRunAt != nil {
		schedule.NextRunAt = nextRunAt
	}

	return schedule, nil
}

// GetSyncScheduleByName retrieves a sync schedule by name.
func (s *PostgresStore) GetSyncScheduleByName(ctx context.Context, name string) (*sync.Schedule, error) {
	query := `
		SELECT id, name, description, job_type, cron_expression, params, is_active,
			last_run_at, last_run_status, last_run_job_id, next_run_at, created_at, updated_at, created_by
		FROM sync_schedules WHERE name = $1
	`
	schedule := &sync.Schedule{}
	var lastRunAt, nextRunAt *time.Time
	var lastRunStatus, lastRunJobID *string

	err := s.pool.QueryRow(ctx, query, name).Scan(
		&schedule.ID, &schedule.Name, &schedule.Description, &schedule.JobType, &schedule.CronExpression,
		&schedule.Params, &schedule.IsActive, &lastRunAt, &lastRunStatus, &lastRunJobID,
		&nextRunAt, &schedule.CreatedAt, &schedule.UpdatedAt, &schedule.CreatedBy,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sync schedule by name: %w", err)
	}

	if lastRunAt != nil {
		schedule.LastRunAt = lastRunAt
	}
	if lastRunStatus != nil {
		schedule.LastRunStatus = lastRunStatus
	}
	if lastRunJobID != nil {
		schedule.LastRunJobID = lastRunJobID
	}
	if nextRunAt != nil {
		schedule.NextRunAt = nextRunAt
	}

	return schedule, nil
}

// UpdateSyncSchedule updates an existing sync schedule.
func (s *PostgresStore) UpdateSyncSchedule(ctx context.Context, schedule *sync.Schedule) error {
	query := `
		UPDATE sync_schedules SET
			name = $2, description = $3, job_type = $4, cron_expression = $5, params = $6,
			is_active = $7, last_run_at = $8, last_run_status = $9, last_run_job_id = $10,
			next_run_at = $11, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`
	err := s.pool.QueryRow(ctx, query,
		schedule.ID, schedule.Name, schedule.Description, schedule.JobType, schedule.CronExpression,
		schedule.Params, schedule.IsActive, schedule.LastRunAt, schedule.LastRunStatus,
		schedule.LastRunJobID, schedule.NextRunAt,
	).Scan(&schedule.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to update sync schedule: %w", err)
	}
	return nil
}

// DeleteSyncSchedule deletes a sync schedule by ID.
func (s *PostgresStore) DeleteSyncSchedule(ctx context.Context, id int) error {
	query := `DELETE FROM sync_schedules WHERE id = $1`
	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete sync schedule: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("sync schedule not found: %d", id)
	}
	return nil
}

// ListSyncSchedules returns all sync schedules.
func (s *PostgresStore) ListSyncSchedules(ctx context.Context, activeOnly bool) ([]*sync.Schedule, error) {
	var query string
	if activeOnly {
		query = `
			SELECT id, name, description, job_type, cron_expression, params, is_active,
				last_run_at, last_run_status, last_run_job_id, next_run_at, created_at, updated_at, created_by
			FROM sync_schedules WHERE is_active = TRUE ORDER BY created_at DESC
		`
	} else {
		query = `
			SELECT id, name, description, job_type, cron_expression, params, is_active,
				last_run_at, last_run_status, last_run_job_id, next_run_at, created_at, updated_at, created_by
			FROM sync_schedules ORDER BY created_at DESC
		`
	}

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list sync schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*sync.Schedule
	for rows.Next() {
		schedule := &sync.Schedule{}
		var lastRunAt, nextRunAt *time.Time
		var lastRunStatus, lastRunJobID *string

		if err := rows.Scan(
			&schedule.ID, &schedule.Name, &schedule.Description, &schedule.JobType, &schedule.CronExpression,
			&schedule.Params, &schedule.IsActive, &lastRunAt, &lastRunStatus, &lastRunJobID,
			&nextRunAt, &schedule.CreatedAt, &schedule.UpdatedAt, &schedule.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("failed to scan sync schedule row: %w", err)
		}

		if lastRunAt != nil {
			schedule.LastRunAt = lastRunAt
		}
		if lastRunStatus != nil {
			schedule.LastRunStatus = lastRunStatus
		}
		if lastRunJobID != nil {
			schedule.LastRunJobID = lastRunJobID
		}
		if nextRunAt != nil {
			schedule.NextRunAt = nextRunAt
		}

		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}
