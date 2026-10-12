package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
)

// JobService handles async backtest job lifecycle.
//
// K10：runner 不再装配期固定注入，改持 EngineRegistry —— 执行期按
// RunSpec.kernel 解析（Resolve）。理由：kernel 是**请求级**路由键，
// 同一个 JobService 要能为不同 job 选不同内核；未注册内核 fail-closed
// （job → failed + error_msg 点名内核，绝不回退 go，FR-1.3）。
type JobService struct {
	store    JobStore
	registry *contracts.EngineRegistry
	logger   zerolog.Logger

	// cancelFuncs holds cancellation functions for running jobs.
	// Key: jobID, Value: cancellation function.
	cancelFuncs sync.Map

	// Sprint 6 P0-8 (ODR-013): graceful shutdown coordination.
	//
	// shutdown is closed by Shutdown() to signal that the service is
	// draining. StartJob() checks this channel under no lock — close()
	// is atomic and reading from a closed channel is the standard
	// "is the service still alive" pattern in Go.
	shutdown chan struct{}

	// inflightWg tracks every in-flight StartJob goroutine. Shutdown()
	// waits on this WaitGroup (with the parent ctx as timeout) so that
	// "SIGTERM → all jobs settled" is testable and observable rather
	// than fire-and-forget.
	inflightWg sync.WaitGroup
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
	ID          string
	StrategyID  string
	Params      []byte // JSONB
	Universe    string
	StartDate   string
	EndDate     string
	Status      string
	Result      []byte // JSONB
	ErrorMsg    string
	CreatedAt   time.Time
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
//
// K10：registry 为必需依赖（构造期不注册任何内核 —— 装配方负责 Register，
// 通常至少注册 contracts.DefaultKernel）。nil registry = 装配错误，
// fail-fast（带着没有注册表的 JobService 对外服务，所有 job 都会在
// 执行期才以 nil-deref 崩溃，不如启动即炸）。
func NewJobService(store JobStore, registry *contracts.EngineRegistry, logger zerolog.Logger) *JobService {
	if registry == nil {
		panic("job: NewJobService requires a non-nil *contracts.EngineRegistry (K10 kernel routing)")
	}
	return &JobService{
		store:    store,
		registry: registry,
		logger:   logger.With().Str("component", "job_service").Logger(),
		shutdown: make(chan struct{}),
	}
}

// CreateJobRequest is the API request to create a backtest job.
//
// K10：新增 Kernel / EngineParams 两个内核路由字段（与同步路径的
// BacktestRequest 同构，K9 冻结的 RunSpec 语义）。Kernel 空值 = 默认
// go 内核（EffectiveKernel 语义，不写回字段）；EngineParams 的
// 未知名由 API 层的 ValidateRunSpec 闸门 400 拒绝（校验在 handler，
// 与同步路径同一口径）。
type CreateJobRequest struct {
	StrategyID     string         `json:"strategy_id" binding:"required"`
	Params         map[string]any `json:"params"`
	Universe       string         `json:"universe" binding:"required"`
	StartDate      string         `json:"start_date" binding:"required"`
	EndDate        string         `json:"end_date" binding:"required"`
	InitialCapital float64        `json:"initial_capital"`
	CommissionRate float64        `json:"commission_rate"`
	SlippageRate   float64        `json:"slippage_rate"`

	// Kernel 是内核路由键（K10）。落库位置：job 的 params JSONB 保留键
	// "kernel"（见 CreateJob）—— 不为路由键新增列，存量作业 params 不变。
	Kernel string `json:"kernel,omitempty"`

	// EngineParams 是引擎特有参数块（K10 透传）。落库位置：params JSONB
	// 保留键 "engine_params"（见 CreateJob）。
	EngineParams map[string]any `json:"engine_params,omitempty"`
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

	params := req.Params
	if params == nil {
		params = make(map[string]any)
	}
	if req.InitialCapital > 0 {
		params["initial_capital"] = req.InitialCapital
	}
	if req.CommissionRate > 0 {
		params["commission_rate"] = req.CommissionRate
	}
	if req.SlippageRate > 0 {
		params["slippage_rate"] = req.SlippageRate
	}
	// K10：内核路由键落库。位置是 params JSONB 的两个**保留键**
	// （服务端权威写入，StartJob 读回）—— 选 params 而不是新列的理由：
	// 路由键是 job 级元数据而非查询维度，加列要 migration + 改三处
	// store SQL，收益不抵风险；且只在非空时写入，存量作业的 params
	// 序列化逐字节不变（回归腿）。
	//
	// 已知边界（可接受）：调用方若自行在 params 里塞 "kernel" /
	// "engine_params" 而顶层字段为空，该值会被当作路由输入 —— 未知
	// 内核仍 fail-closed（job failed + error_msg 点名），go 内核则
	// 根本不读 EngineParams，无静默错配通道。
	if req.Kernel != "" {
		params["kernel"] = req.Kernel
	}
	if len(req.EngineParams) > 0 {
		params["engine_params"] = req.EngineParams
	}

	paramsJSON, err := json.Marshal(params)
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

	s.StartJob(context.Background(), jobID)

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
//   - Uses context.Background() as base to ensure the job continues running
//     even after the HTTP request that started it has completed.
//   - Monitors the parent context for cancellation signals to allow graceful shutdown.
//   - For explicit cancellation, use CancelJob() method instead.
//
// Sprint 6 P0-8 (ODR-013): if the JobService is currently draining
// (Shutdown has been called), StartJob refuses to start a new goroutine
// and immediately marks the job as failed with a clear reason. This
// keeps the DB clean: there are no "running" jobs that the process is
// no longer actually running.
func (s *JobService) StartJob(parentCtx context.Context, jobID string) {
	// Sprint 6 P0-8: refuse to start new jobs during shutdown.
	select {
	case <-s.shutdown:
		s.logger.Warn().
			Str("job_id", jobID).
			Msg("Rejecting StartJob: JobService is shutting down; marking as failed")
		// Use Background ctx for the DB write — the request ctx may
		// already be cancelled and we still want a persistent record
		// of the refusal.
		if err := s.store.UpdateJobFailed(context.Background(), jobID, "service is shutting down"); err != nil {
			s.logger.Error().Err(err).Str("job_id", jobID).Msg("Failed to mark refused job as failed")
		}
		return
	default:
	}

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

	// Sprint 6 P0-8: track this goroutine in the inflight WaitGroup
	// so Shutdown() can wait for all in-flight jobs to settle.
	s.inflightWg.Add(1)
	go func() {
		defer s.inflightWg.Done()
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

		// K10：内核路由键从 params 保留键读回（CreateJob 写入）。
		// 与 initial_capital 同一读取点 —— 作业执行所需的 job 级
		// 配置全部来自 record.Params 这一个 JSONB。
		var jobParams map[string]any
		if len(record.Params) > 0 {
			_ = json.Unmarshal(record.Params, &jobParams)
		}
		kernel, _ := jobParams["kernel"].(string)
		engineParams, _ := jobParams["engine_params"].(map[string]any)

		backtestReq := contracts.BacktestRequest{
			Strategy:     record.StrategyID,
			StockPool:    stockPool,
			StartDate:    record.StartDate,
			EndDate:      record.EndDate,
			Kernel:       kernel,
			EngineParams: engineParams,
		}
		if v, ok := jobParams["initial_capital"].(float64); ok && v > 0 {
			backtestReq.InitialCapital = v
		}

		// K10：经 EngineRegistry 按 kernel 路由执行器。未注册/不可用 →
		// job 标记 failed 且 error_msg 点名内核名，**不得回退 go**
		// （FR-1.3：防「以为在测 NT 其实在测 Go」）。解析失败发生在
		// 任何 runner 调用之前，库里不会留下 go 内核产物。
		runner, err := s.registry.Resolve(backtestReq.EffectiveKernel())
		if err != nil {
			s.logger.Error().
				Err(err).
				Str("job_id", jobID).
				Str("kernel", backtestReq.EffectiveKernel()).
				Msg("Kernel routing refused — marking job as failed (no fallback to default kernel)")
			if dbErr := s.store.UpdateJobFailed(jobCtx, jobID, fmt.Sprintf("kernel routing failed: %v", err)); dbErr != nil {
				s.logger.Error().Err(dbErr).Str("job_id", jobID).Msg("Failed to mark routing-refused job as failed")
			}
			s.cancelFuncs.Delete(jobID)
			return
		}

		s.logger.Info().
			Str("job_id", jobID).
			Str("kernel", backtestReq.EffectiveKernel()).
			Str("strategy", record.StrategyID).
			Str("universe", record.Universe).
			Int("stock_count", len(stockPool)).
			Str("start_date", record.StartDate).
			Str("end_date", record.EndDate).
			Msg("Executing backtest")

		result, err := runner.RunBacktest(jobCtx, backtestReq)
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

		// OBS-01：无效运行（空票池 / 0 成交 / 零值日期 / 垃圾指标）不得再报
		// 「Backtest completed successfully」，也不得持久化成 completed ——
		// 它走失败路径，原因 Code 可见，让作业调用方能判出「这次不算成功」。
		if len(result.InvalidReasons) > 0 {
			reason := "backtest produced an invalid run: " + strings.Join(result.InvalidReasons, ", ")
			s.logger.Error().
				Str("job_id", jobID).
				Dur("elapsed", elapsed).
				Int("total_trades", result.TotalTrades).
				Int("universe_max_size", derefIntOr(result.UniverseMaxSize, -1)).
				Strs("invalid_reasons", result.InvalidReasons).
				Msg("Backtest produced an invalid run — result marked Status=invalid")
			if dbErr := s.store.UpdateJobFailed(jobCtx, jobID, reason); dbErr != nil {
				s.logger.Error().Err(dbErr).Str("job_id", jobID).Msg("Failed to mark invalid run as failed")
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

// Sprint 6 P0-8 (ODR-013): graceful shutdown.
//
// Shutdown drains the JobService in three observable phases:
//
//  1. REJECT-NEW — close the shutdown channel so any in-flight
//     StartJob call short-circuits and refuses to spawn a new
//     goroutine. New HTTP requests that try to start a job will
//     be persisted as "failed" with a clear reason in the DB.
//
//  2. CANCEL-IN-FLIGHT — call every cancelFuncs entry. The running
//     backtest goroutines see ctx.Done() and unwind naturally. The
//     goroutines are responsible for marking their own row as
//     "completed" or "failed" — Shutdown itself does NOT touch the
//     DB for in-flight jobs because doing so would race with the
//     goroutine's own UpdateJobCompleted/Failed call.
//
//  3. WAIT — block on the inflight WaitGroup until either every
//     in-flight job has settled, or the parent ctx fires. If the
//     timeout wins, the caller MUST call CleanupStaleRunning next
//     to sweep any rows that are still "running" in the DB.
//
// Shutdown is idempotent: a second call after the first is a no-op.
//
// Returns nil on a clean drain, ctx.Err() if the wait timed out.
func (s *JobService) Shutdown(ctx context.Context) error {
	// Phase 1: REJECT-NEW. close() under no lock — startJob's select
	// will see the closed channel immediately.
	select {
	case <-s.shutdown:
		s.logger.Info().Msg("Shutdown already in progress; Shutdown is idempotent, returning")
		return nil
	default:
		close(s.shutdown)
	}
	s.logger.Info().Msg("JobService draining: rejecting new jobs and cancelling in-flight ones")

	// Phase 2: CANCEL-IN-FLIGHT.
	cancelledCount := 0
	s.cancelFuncs.Range(func(key, value any) bool {
		if cancel, ok := value.(context.CancelFunc); ok && cancel != nil {
			cancel()
			cancelledCount++
		}
		return true
	})
	s.logger.Info().Int("cancelled", cancelledCount).Msg("In-flight job contexts cancelled; waiting for goroutines to settle")

	// Phase 3: WAIT.
	done := make(chan struct{})
	go func() {
		s.inflightWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info().Msg("JobService drained cleanly: all in-flight jobs settled")
		return nil
	case <-ctx.Done():
		s.logger.Warn().Err(ctx.Err()).Msg("JobService drain deadline reached; some jobs may still be settling — call CleanupStaleRunning to ensure DB consistency")
		return ctx.Err()
	}
}

// CleanupStaleRunning scans the JobStore for rows whose status is
// still "running" and forces them to "failed" with a cancellation
// message. This is the safety net for Shutdown: if the wait
// deadline expired before all goroutines finished their own
// UpdateJob* call, those rows would otherwise be stuck as "running"
// forever, blocking CI gates / dashboards that look for
// status IN ('completed','failed').
//
// CleanupStaleRunning is also useful as a recovery tool after a
// hard process crash (kill -9, OOM, etc.) — call it on startup
// to repair stale rows from the previous run.
//
// Returns the number of rows that were transitioned from
// "running" to "failed".
func (s *JobService) CleanupStaleRunning(ctx context.Context) (int, error) {
	// ListBacktestJobs returns most-recent-first. The interface is
	// limit-based; for the cleanup pass we ask for a large window
	// (1000 rows = ~8 hours of typical 30s/job throughput) so any
	// genuinely stuck jobs are caught. For sites that have
	// legitimately thousands of jobs/day, a follow-up ODR may add a
	// dedicated "ListByStatus" to JobStore; for now the broad
	// scan is acceptable because the cost is one bounded query
	// and a small status check per row.
	const cleanupWindowLimit = 1000
	records, err := s.store.ListBacktestJobs(ctx, cleanupWindowLimit)
	if err != nil {
		return 0, fmt.Errorf("failed to list jobs for stale-running cleanup: %w", err)
	}

	transitioned := 0
	for _, r := range records {
		status, _ := r["status"].(string)
		if status != "running" {
			continue
		}
		id, _ := r["id"].(string)
		if id == "" {
			continue
		}
		// UpdateJobFailed is the existing API; it already sets
		// status='failed' and appends to error_msg. We use a fixed
		// prefix so downstream log/alert consumers can recognize
		// SIGTERM-killed rows.
		msg := "job cancelled by service shutdown (P0-8 stale-running cleanup)"
		if err := s.store.UpdateJobFailed(ctx, id, msg); err != nil {
			s.logger.Error().Err(err).Str("job_id", id).Msg("Failed to clean up stale 'running' job")
			continue
		}
		transitioned++
		s.logger.Info().Str("job_id", id).Msg("Cleaned up stale 'running' job → 'failed'")
	}
	s.logger.Info().Int("transitioned", transitioned).Int("scanned", len(records)).Msg("Stale-running cleanup complete")
	return transitioned, nil
}

// SaveSyncResult persists a completed synchronous backtest result to the DB.
func (s *JobService) SaveSyncResult(ctx context.Context, resp *contracts.BacktestResponse) error {
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
		EndDate:     r.EndDate,
		Status:      r.Status,
		Result:      r.Result,
		Error:       r.ErrorMsg,
		CreatedAt:   r.CreatedAt,
		StartedAt:   r.StartedAt,
		CompletedAt: r.CompletedAt,
	}
}

// derefIntOr 解引用 int 指针（nil 用 fallback 表示）—— OBS-01：票池规模是 *int，
// nil = 未评估，日志里用 -1 一眼能看出。
func derefIntOr(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}
