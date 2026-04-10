package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateBacktestJob inserts a new backtest job.
func (s *PostgresStore) CreateBacktestJob(ctx context.Context, job map[string]any) error {
	query := `
		INSERT INTO backtest_jobs (id, strategy_id, params, universe, start_date, end_date, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`
	_, err := s.pool.Exec(ctx, query,
		job["id"], job["strategy_id"], job["params"],
		job["universe"], job["start_date"], job["end_date"], job["status"],
	)
	if err != nil {
		return fmt.Errorf("failed to create backtest job: %w", err)
	}
	return nil
}

// UpdateJobStarted marks a job as running.
func (s *PostgresStore) UpdateJobStarted(ctx context.Context, jobID string) error {
	query := `UPDATE backtest_jobs SET status = 'running', started_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, jobID)
	if err != nil {
		return fmt.Errorf("failed to update job started: %w", err)
	}
	return nil
}

// UpdateJobCompleted marks a job as completed with a result.
func (s *PostgresStore) UpdateJobCompleted(ctx context.Context, jobID string, result []byte) error {
	query := `UPDATE backtest_jobs SET status = 'completed', result = $2, completed_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, jobID, result)
	if err != nil {
		return fmt.Errorf("failed to update job completed: %w", err)
	}
	return nil
}

// UpdateJobFailed marks a job as failed with an error message.
func (s *PostgresStore) UpdateJobFailed(ctx context.Context, jobID string, errMsg string) error {
	query := `UPDATE backtest_jobs SET status = 'failed', error_message = $2, completed_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, jobID, errMsg)
	if err != nil {
		return fmt.Errorf("failed to update job failed: %w", err)
	}
	return nil
}

// GetBacktestJob retrieves a single backtest job by ID as map[string]any.
func (s *PostgresStore) GetBacktestJob(ctx context.Context, jobID string) (map[string]any, error) {
	query := `
		SELECT id, strategy_id, params, universe,
		       start_date::text, end_date::text,
		       status, result, error_message, created_at, started_at, completed_at
		FROM backtest_jobs WHERE id = $1
	`
	var id, strategyID, universe, startDate, endDate, status string
	var params, result []byte
	var errorMsg *string
	var createdAt time.Time
	var startedAt, completedAt *time.Time

	err := s.pool.QueryRow(ctx, query, jobID).Scan(
		&id, &strategyID, &params, &universe, &startDate, &endDate,
		&status, &result, &errorMsg, &createdAt, &startedAt, &completedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get backtest job: %w", err)
	}

	m := map[string]any{
		"id":          id,
		"strategy_id": strategyID,
		"params":      params,
		"universe":    universe,
		"start_date":  startDate,
		"end_date":    endDate,
		"status":      status,
		"result":      result,
		"created_at":  createdAt,
	}
	if errorMsg != nil {
		m["error_msg"] = *errorMsg
	}
	if startedAt != nil {
		m["started_at"] = *startedAt
	}
	if completedAt != nil {
		m["completed_at"] = *completedAt
	}
	return m, nil
}

// ListBacktestJobs returns recent backtest jobs as []map[string]any.
func (s *PostgresStore) ListBacktestJobs(ctx context.Context, limit int) ([]map[string]any, error) {
	query := `
		SELECT id, strategy_id, params, universe,
		       start_date::text, end_date::text,
		       status, result, error_message, created_at, started_at, completed_at
		FROM backtest_jobs
		ORDER BY created_at DESC
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list backtest jobs: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, strategyID, universe, startDate, endDate, status string
		var params, result []byte
		var errorMsg *string
		var createdAt time.Time
		var startedAt, completedAt *time.Time
		if err := rows.Scan(
			&id, &strategyID, &params, &universe, &startDate, &endDate,
			&status, &result, &errorMsg, &createdAt, &startedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan backtest job row: %w", err)
		}
		m := map[string]any{
			"id":          id,
			"strategy_id": strategyID,
			"params":      params,
			"universe":    universe,
			"start_date":  startDate,
			"end_date":    endDate,
			"status":      status,
			"result":      result,
			"created_at":  createdAt,
		}
		if errorMsg != nil {
			m["error_msg"] = *errorMsg
		}
		if startedAt != nil {
			m["started_at"] = *startedAt
		}
		if completedAt != nil {
			m["completed_at"] = *completedAt
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// DeleteBacktestJob deletes a pending job by ID.
func (s *PostgresStore) DeleteBacktestJob(ctx context.Context, jobID string) error {
	query := `DELETE FROM backtest_jobs WHERE id = $1 AND status = 'pending'`
	result, err := s.pool.Exec(ctx, query, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete backtest job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found or not pending")
	}
	return nil
}
