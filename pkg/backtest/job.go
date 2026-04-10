package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// JobService handles async backtest job lifecycle.
type JobService struct {
	store  JobStore
	engine *Engine
	logger zerolog.Logger

	// cancelFuncs holds cancellation functions for running jobs.
	// Key: jobID, Value: cancellation function.
	cancelFuncs sync.Map
}

// JobStore is the subset of storage operations needed by JobService.
// Uses plain types to avoid import cycles with pkg/storage.
type JobStore interface {
	CreateBacktestJob(ctx context.Context, job map[string]any) error
	UpdateJobStarted(ctx context.Context, jobID string) error
	UpdateJobCompleted(ctx context.Context, jobID string, result []byte) error
	UpdateJobFailed(ctx context.Context, jobID string, errMsg string) error
	GetBacktestJob(ctx context.Context, jobID string) (map[string]any, error)
	ListBacktestJobs(ctx context.Context, limit int) ([]map[string]any, error)
	DeleteBacktestJob(ctx context.Context, jobID string) error
}

// JobRecord holds the raw fields of a backtest job as stored in the DB.
// The Params and Result fields are stored as JSON bytes.
type JobRecord struct {
	ID         string
	StrategyID string
	Params     []byte // JSONB
	Universe   string
	StartDate  string
	EndDate    string
	Status     string
	Result     []byte // JSONB
	ErrorMsg   string
	CreatedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// jobRecordToMap converts a JobRecord to map[string]any for JobStore interface.
func jobRecordToMap(j *JobRecord) map[string]any {
	m := map[string]any{
		"id":          j.ID,
		"strategy_id": j.StrategyID,
		"params":      j.Params,
		"universe":    j.Universe,
		"start_date":  j.StartDate,
		"end_date":    j.EndDate,
		"status":      j.Status,
		"result":      j.Result,
		"error_msg":   j.ErrorMsg,
		"created_at":  j.CreatedAt,
	}
	if j.StartedAt != nil {
		m["started_at"] = *j.StartedAt
	}
	if j.CompletedAt != nil {
		m["completed_at"] = *j.CompletedAt
	}
	return m
}

// mapToJobRecord converts a map back to JobRecord.
func mapToJobRecord(m map[string]any) *JobRecord {
	j := &JobRecord{
		ID:         m["id"].(string),
		StrategyID: m["strategy_id"].(string),
		Universe:   m["universe"].(string),
		StartDate:  m["start_date"].(string),
		EndDate:    m["end_date"].(string),
		Status:     m["status"].(string),
	}
	if v, ok := m["params"].([]byte); ok {
		j.Params = v
	}
	if v, ok := m["result"].([]byte); ok {
		j.Result = v
	}
	if v, ok := m["error_msg"].(string); ok {
		j.ErrorMsg = v
	}
	if v, ok := m["created_at"].(time.Time); ok {
		j.CreatedAt = v
	}
	if v, ok := m["started_at"].(time.Time); ok {
		j.StartedAt = &v
	}
	if v, ok := m["completed_at"].(time.Time); ok {
		j.CompletedAt = &v
	}
	return j
}

// NewJobService creates a new JobService.
func NewJobService(store JobStore, engine *Engine) *JobService {
	return &JobService{
		store:  store,
		engine: engine,
		logger: engine.logger.With().Str("component", "job_service").Logger(),
	}
}

// CreateJobRequest is the API request to create a backtest job.
type CreateJobRequest struct {
	StrategyID string         `json:"strategy_id" binding:"required"`
	Params     map[string]any `json:"params"`
	Universe   string         `json:"universe" binding:"required"`
	StartDate  string         `json:"start_date" binding:"required"`
	EndDate    string         `json:"end_date" binding:"required"`
}

// Job is the public API response for a job.
type Job struct {
	ID          string          `json:"id"`
	StrategyID  string          `json:"strategy_id"`
	Params      json.RawMessage `json:"params"`
	Universe    string          `json:"universe"`
	StartDate   string          `json:"start_date"`
	EndDate     string          `json:"end_date"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// CreateJob creates a new backtest job record and starts it in a background goroutine.
func (s *JobService) CreateJob(ctx context.Context, req CreateJobRequest) (*Job, error) {
	jobID := uuid.New().String()

	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	record := &JobRecord{
		ID:         jobID,
		StrategyID: req.StrategyID,
		Params:     paramsJSON,
		Universe:   req.Universe,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
		Status:     "pending",
		CreatedAt:  time.Now(),
	}

	if err := s.store.CreateBacktestJob(ctx, jobRecordToMap(record)); err != nil {
		return nil, fmt.Errorf("failed to create job record: %w", err)
	}

	s.StartJob(ctx, jobID)

	return &Job{
		ID:         jobID,
		StrategyID: req.StrategyID,
		Params:     paramsJSON,
		Universe:   req.Universe,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
		Status:     "pending",
		CreatedAt:  record.CreatedAt,
	}, nil
}

// StartJob runs the backtest in a background goroutine.
// It updates job status to "running", executes the backtest, then
// marks it "completed" with the result or "failed" with the error.
//
// Context handling strategy:
// - Uses context.Background() as base to ensure the job continues running
//   even after the HTTP request that started it has completed.
// - Monitors the parent context for cancellation signals to allow graceful shutdown.
// - For explicit cancellation, use CancelJob() method instead.
func (s *JobService) StartJob(parentCtx context.Context, jobID string) {
	s.logger.Info().Str("job_id", jobID).Msg("Starting backtest job")

	// Use Background context so the goroutine isn't cancelled when HTTP request ends
	// This ensures long-running backtests complete even if client disconnects.
	jobCtx, jobCancel := context.WithCancel(context.Background())
	s.cancelFuncs.Store(jobID, jobCancel)

	// Monitor parent context for cancellation signal
	// This allows callers to propagate cancellation if needed (e.g., server shutdown)
	go func() {
		select {
		case <-parentCtx.Done():
			s.logger.Warn().
				Str("job_id", jobID).
				Err(parentCtx.Err()).
				Msg("Parent context cancelled, initiating graceful job shutdown")
			jobCancel()
		case <-jobCtx.Done():
			// Job already completed or cancelled via CancelJob()
		}
	}()

	go func() {
		defer jobCancel()
		startTime := time.Now()

		s.logger.Info().Str("job_id", jobID).Msg("Updating job status to 'running'")

		if err := s.store.UpdateJobStarted(jobCtx, jobID); err != nil {
			s.logger.Error().Err(err).Str("job_id", jobID).Msg("Failed to mark job as started")
			if dbErr := s.store.UpdateJobFailed(jobCtx, jobID, fmt.Sprintf("failed to mark started: %v", err)); dbErr != nil {
				s.logger.Error().Err(dbErr).Str("job_id", jobID).Msg("Failed to mark job as failed after start error")
			}
			s.cancelFuncs.Delete(jobID)
			return
		}

		s.logger.Info().Str("job_id", jobID).Msg("Loading job record from database")

		recordMap, err := s.store.GetBacktestJob(jobCtx, jobID)
		if err != nil || recordMap == nil {
			s.logger.Error().Err(err).Str("job_id", jobID).Msg("Job not found in database")
			if dbErr := s.store.UpdateJobFailed(jobCtx, jobID, fmt.Sprintf("job not found: %v", err)); dbErr != nil {
				s.logger.Error().Err(dbErr).Str("job_id", jobID).Msg("Failed to mark job as failed")
			}
			s.cancelFuncs.Delete(jobID)
			return
		}

		record := mapToJobRecord(recordMap)
		stockPool := parseUniverse(record.Universe)

		s.logger.Info().
			Str("job_id", jobID).
			Str("strategy", record.StrategyID).
			Str("universe", record.Universe).
			Int("stock_count", len(stockPool)).
			Str("start_date", record.StartDate).
			Str("end_date", record.EndDate).
			Msg("Executing backtest")

		backtestReq := BacktestRequest{
			Strategy:  record.StrategyID,
			StockPool: stockPool,
			StartDate: record.StartDate,
			EndDate:   record.EndDate,
		}

		result, err := s.engine.RunBacktest(jobCtx, backtestReq)
		elapsed := time.Since(startTime)

		if err != nil {
			s.logger.Error().
				Err(err).
				Str("job_id", jobID).
				Dur("elapsed", elapsed).
				Msg("Backtest execution failed")

			if dbErr := s.store.UpdateJobFailed(jobCtx, jobID, err.Error()); dbErr != nil {
				s.logger.Error().Err(dbErr).Str("job_id", jobID).Msg("Failed to update job status to failed")
			}
			s.cancelFuncs.Delete(jobID)
			return
		}

		s.logger.Info().
			Str("job_id", jobID).
			Dur("elapsed", elapsed).
			Float64("total_return", result.TotalReturn).
			Float64("sharpe_ratio", result.SharpeRatio).
			Float64("max_drawdown", result.MaxDrawdown).
			Int("trade_count", len(result.Trades)).
			Msg("Backtest completed successfully")

		resultJSON, err := json.Marshal(result)
		if err != nil {
			s.logger.Error().Err(err).Str("job_id", jobID).Msg("Failed to marshal backtest result")
			if dbErr := s.store.UpdateJobFailed(jobCtx, jobID, fmt.Sprintf("failed to marshal result: %v", err)); dbErr != nil {
				s.logger.Error().Err(dbErr).Str("job_id", jobID).Msg("Failed to mark job as failed")
			}
			s.cancelFuncs.Delete(jobID)
			return
		}

		if err := s.store.UpdateJobCompleted(jobCtx, jobID, resultJSON); err != nil {
			s.logger.Error().Err(err).Str("job_id", jobID).Msg("Failed to persist job result to database")
		} else {
			s.logger.Info().Str("job_id", jobID).Msg("Job result persisted successfully")
		}
		s.cancelFuncs.Delete(jobID)
	}()
}

// GetJob retrieves the current state of a job.
func (s *JobService) GetJob(ctx context.Context, jobID string) (*Job, error) {
	recordMap, err := s.store.GetBacktestJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}
	if recordMap == nil {
		return nil, nil
	}
	return recordToJob(mapToJobRecord(recordMap)), nil
}

// ListJobs returns the most recent jobs.
func (s *JobService) ListJobs(ctx context.Context, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 20
	}
	records, err := s.store.ListBacktestJobs(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	jobs := make([]*Job, len(records))
	for i, r := range records {
		jobs[i] = recordToJob(mapToJobRecord(r))
	}
	return jobs, nil
}

// CancelJob attempts to cancel a pending/running job.
func (s *JobService) CancelJob(ctx context.Context, jobID string) error {
	recordMap, err := s.store.GetBacktestJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}
	if recordMap == nil {
		return fmt.Errorf("job not found")
	}
	record := mapToJobRecord(recordMap)
	switch record.Status {
	case "pending":
		return s.store.DeleteBacktestJob(ctx, jobID)
	case "running":
		if cancel, ok := s.cancelFuncs.Load(jobID); ok {
			cancel.(context.CancelFunc)()
			s.cancelFuncs.Delete(jobID)
		}
		return nil
	default:
		return fmt.Errorf("job is already %s", record.Status)
	}
}

// SaveSyncResult persists a completed synchronous backtest result to the DB.
func (s *JobService) SaveSyncResult(ctx context.Context, resp *BacktestResponse) error {
	resultJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	universe := ""
	if len(resp.StockPool) > 0 {
		universe = strings.Join(resp.StockPool, ",")
	}

	job := map[string]any{
		"id":          resp.ID,
		"strategy_id": resp.Strategy,
		"params":      []byte("{}"),
		"universe":    universe,
		"start_date":  resp.StartDate,
		"end_date":    resp.EndDate,
		"status":      "pending",
	}

	if err := s.store.CreateBacktestJob(ctx, job); err != nil {
		return fmt.Errorf("failed to create job record: %w", err)
	}

	if err := s.store.UpdateJobStarted(ctx, resp.ID); err != nil {
		return fmt.Errorf("failed to mark job started: %w", err)
	}

	if err := s.store.UpdateJobCompleted(ctx, resp.ID, resultJSON); err != nil {
		return fmt.Errorf("failed to persist job result: %w", err)
	}

	return nil
}

// parseUniverse converts a universe string to a list of stock symbols.
func parseUniverse(universe string) []string {
	if universe == "" || universe == "all" {
		return nil
	}
	if strings.HasPrefix(universe, "universe:") {
		universe = strings.TrimPrefix(universe, "universe:")
	}
	parts := strings.Split(universe, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// recordToJob converts an internal JobRecord to a public Job.
func recordToJob(r *JobRecord) *Job {
	return &Job{
		ID:          r.ID,
		StrategyID:  r.StrategyID,
		Params:      r.Params,
		Universe:    r.Universe,
		StartDate:   r.StartDate,
		EndDate:    r.EndDate,
		Status:      r.Status,
		Result:      r.Result,
		Error:       r.ErrorMsg,
		CreatedAt:   r.CreatedAt,
		StartedAt:   r.StartedAt,
		CompletedAt: r.CompletedAt,
	}
}
