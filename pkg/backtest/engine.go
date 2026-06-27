package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/ruoxizhnya/quant-trading/pkg/httpclient"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/spf13/viper"
)

// Config holds backtest engine configuration.
type Config struct {
	InitialCapital float64 `mapstructure:"initial_capital"`
	CommissionRate float64 `mapstructure:"commission_rate"`
	SlippageRate   float64 `mapstructure:"slippage_rate"`
	RiskFreeRate   float64 `mapstructure:"risk_free_rate"`
	Seed           int64   `mapstructure:"seed"` // Random seed for determinism; 0 = use time-based seed

	// Trading rules loaded from config
	Trading TradingConfig `mapstructure:"trading"`
}

// TradingConfig holds A-share trading rules
type TradingConfig struct {
	StampTaxRate    float64          `mapstructure:"stamp_tax_rate"`
	MinCommission   float64          `mapstructure:"min_commission"`
	TransferFeeRate float64          `mapstructure:"transfer_fee_rate"`
	PriceLimit      PriceLimitConfig `mapstructure:"price_limit"`
	NewStockDays    int              `mapstructure:"new_stock_days"`
}

// PriceLimitConfig holds price limit rules
type PriceLimitConfig struct {
	Normal float64 `mapstructure:"normal"`
	ST     float64 `mapstructure:"st"`
	New    float64 `mapstructure:"new"`
}

// Default trading constants (fallback if not configured)
func defaultTradingConfig() TradingConfig {
	return TradingConfig{
		StampTaxRate:    DefaultStampTaxRate,
		MinCommission:   DefaultMinCommission,
		TransferFeeRate: DefaultTransferFeeRate,
		PriceLimit: PriceLimitConfig{
			Normal: DefaultPriceLimitNormal,
			ST:     DefaultPriceLimitST,
			New:    DefaultPriceLimitNew,
		},
		NewStockDays: DefaultNewStockDays,
	}
}

// Engine is the backtesting engine that simulates trading strategies.
type Engine struct {
	mu sync.RWMutex

	// Configuration
	config Config

	// Market data provider (abstracted for testability)
	provider marketdata.Provider

	// External service URLs (for non-market-data calls: risk, strategy)
	strategyServiceURL string
	riskServiceURL     string

	// HTTP client for non-market-data service communication (with retry)
	httpClient *httpclient.Client

	// Active backtest states — P1-18 (ADR-020):
	// 旧字段 `backtests map[string]*BacktestState` + `btMu sync.RWMutex` 已
	// 删除。状态现在通过 `StateStore` 接口管理，LRUStateStore 默认
	// capacity = 1000，防止长跑场景下无界增长导致 OOM。外部代码
	// 通过 Engine.StateStore() 访问；旧 API (`GetBacktestResult` 等)
	// 行为完全保留,内部委托到 store.Get。
	//
	// 工作流程：
	//   - RunBacktest  : store.Put(id, state)
	//   - Get*         : store.Get(id)
	//   - 周期性 GC    : store.Evict(keepN)  (可由 caller 调用)
	//   - LRU 自动驱逐 : Put 超过 capacity 时旧条目自动丢弃
	stateStore StateStore

	// Logger
	logger zerolog.Logger

	// L1 in-memory OHLCV cache (per-backtest-lifecycle) — P1-16
	// (ADR-020) 拆分为 CacheManager 子组件。旧字段 inMemoryOHLCV /
	// inMemoryOHLCVAtomic 已删除，行为完全迁移到 cache；外部代码
	// 通过 Engine.CacheManager() 访问，旧 LoadOHLCVInMemory /
	// getOHLCV / warmCache 保留为 backward-compat shim (6 个月)。
	//
	// Key: symbol, Value: all OHLCV bars for that symbol (sorted by date).
	// Populated via CacheManager.Warm() or CacheManager.Load().
	// CacheManager.Get() checks this first — zero-latency hit, falls back
	// to provider on miss. Eliminates N×D HTTP calls (N=stocks, D=trading
	// days) during a backtest run.
	cache *CacheManager

	// L1 factor cache (per-backtest-lifecycle) — P1-16 (ADR-020) 拆分
	// 为 FactorCacheAccessor 子组件。旧字段 factorCache 已删除；
	// 旧 LoadFactorCache / GetFactorZScore / warmFactorCache 保留为
	// backward-compat shim。Engine.FactorCache() 返回 accessor。
	//
	// Structure: factorType -> tradeDate -> symbol -> zScore.
	// Populated via FactorCacheAccessor.Load() before a backtest run.
	// Get() reads from this map — zero-latency hit. Eliminates
	// per-symbol-per-day DB queries for multi-factor strategies.
	factor *FactorCacheAccessor

	// In-process risk manager — when set, position sizing, stop-loss, and
	// regime detection are computed locally without HTTP calls to risk-service.
	// Pass nil (default) to fall back to HTTP-based risk service calls.
	riskManager *risk.RiskManager

	// ParallelWorkers controls how many goroutines fetch data concurrently
	// inside each trading day. A value <= 0 means sequential (1 worker).
	parallelWorkers int

	// Optional DataAdapter for event-driven data pipeline with multi-source
	// fallback and runtime source switching. When set, provider reads are
	// routed through the adapter; when nil, the raw provider is used directly.
	dataAdapter *marketdata.DataAdapter

	// Optional PostgresStore for direct factor cache access.
	// When set, the engine pre-loads factor z-scores from DB before backtest,
	// eliminating per-symbol-per-day HTTP/DB queries for multi-factor strategies.
	store *storage.PostgresStore

	// liveBridge 桥接 backtest 信号到 live/paper trading (P1-17 ADR-020)。
	// 取代旧 `liveTrader live.LiveTrader` 字段；旧 SetLiveTrader /
	// GetLiveTrader / ExecuteSignalViaLiveTrader 等方法保留为
	// backward-compat shim (6 个月) 委托到 bridge。
	// live 包接口见 pkg/live/trader.go。
	liveBridge *LiveBridge

	// executionBridge 桥接 ExecutionService (slippage / commission /
	// 限价单)。取代旧 `executionService ExecutionService` 字段；
	// 旧 SetExecutionService / GetExecutionService 保留为 shim。
	// ExecutionService 接口见 execution.go。
	executionBridge *ExecutionBridge

	// rng is the per-engine *rand.Rand, initialized from config.Seed in
	// NewEngine. Always retained (never discarded) so that any code path
	// needing deterministic randomness — e.g. per-backtest trade IDs,
	// bootstrap sampling in walk-forward, or future stochastic order-filling
	// models — can call e.RNG() to get a stable, replayable stream.
	// When config.Seed is 0 the engine falls back to a time-based seed and
	// the stream is non-replayable (intentional, for production runs).
	rng *rand.Rand
}

// BacktestState is defined in state.go (P1-20) with internal locking
// (mu RWMutex) and a frozen flag. See state.go for the concurrency
// contract and accessor methods (GetStatus / SetStatus / Freeze /
// Snapshot / ...).

// BacktestRequest represents the API request to start a backtest.
type BacktestRequest struct {
	Strategy       string   `json:"strategy" binding:"required"`
	StockPool      []string `json:"stock_pool"`
	IndexCode      string   `json:"index_code"`
	StartDate      string   `json:"start_date" binding:"required"`
	EndDate        string   `json:"end_date" binding:"required"`
	InitialCapital float64  `json:"initial_capital"`
	RiskFreeRate   float64  `json:"risk_free_rate"`
}

// BacktestResponse represents the API response for a backtest run.
type BacktestResponse struct {
	ID              string                  `json:"id"`
	Status          string                  `json:"status"`
	Strategy        string                  `json:"strategy,omitempty"`
	StrategyGitHash string                  `json:"strategy_git_hash,omitempty"`
	StartDate       string                  `json:"start_date,omitempty"`
	EndDate         string                  `json:"end_date,omitempty"`
	TotalReturn     float64                 `json:"total_return,omitempty"`
	AnnualReturn    float64                 `json:"annual_return,omitempty"`
	SharpeRatio     float64                 `json:"sharpe_ratio,omitempty"`
	SortinoRatio    float64                 `json:"sortino_ratio,omitempty"`
	MaxDrawdown     float64                 `json:"max_drawdown,omitempty"`
	MaxDrawdownDate string                  `json:"max_drawdown_date,omitempty"`
	WinRate         float64                 `json:"win_rate,omitempty"`
	TotalTrades     int                     `json:"total_trades,omitempty"`
	WinTrades       int                     `json:"win_trades,omitempty"`
	LoseTrades      int                     `json:"lose_trades,omitempty"`
	AvgHoldingDays  float64                 `json:"avg_holding_days,omitempty"`
	CalmarRatio     float64                 `json:"calmar_ratio,omitempty"`
	StartedAt       string                  `json:"started_at,omitempty"`
	CompletedAt     string                  `json:"completed_at,omitempty"`
	Error           string                  `json:"error,omitempty"`
	PortfolioValues []domain.PortfolioValue `json:"portfolio_values,omitempty"`
	Trades          []domain.Trade          `json:"trades,omitempty"`
	StockPool       []string                `json:"stock_pool,omitempty"`
	InitialCapital  float64                 `json:"initial_capital,omitempty"`
}

// NewEngine creates a new backtest engine.
func NewEngine(v *viper.Viper, provider marketdata.Provider, logger zerolog.Logger) (*Engine, error) {
	config := Config{}
	if err := v.Sub("backtest").Unmarshal(&config); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInvalidInput, "failed to unmarshal backtest config", "NewEngine")
	}

	strategyServiceURL := v.GetString("strategy_service.url")
	if strategyServiceURL == "" {
		strategyServiceURL = "http://localhost:8082"
	}
	riskServiceURL := v.GetString("risk_service.url")
	if riskServiceURL == "" {
		riskServiceURL = "http://localhost:8083"
	}

	// Set defaults
	if config.InitialCapital == 0 {
		config.InitialCapital = DefaultInitialCapital
	}
	if config.CommissionRate == 0 {
		config.CommissionRate = DefaultCommissionRate
	}
	if config.SlippageRate == 0 {
		config.SlippageRate = DefaultSlippageRate
	}
	if config.RiskFreeRate == 0 {
		config.RiskFreeRate = DefaultRiskFreeRate
	}

	// Load trading rules from config, use defaults if not set
	if config.Trading.StampTaxRate == 0 {
		config.Trading = defaultTradingConfig()
	}

	// Initialize random seed for deterministic backtests.
	//
	// Sprint 6 P0-5 (ODR-013): the previous code called
	//     rand.New(rand.NewSource(config.Seed))
	// and threw the *rand.Rand return value away. The global state was
	// re-seeded but nothing in the engine held a handle to the source, so
	// any code that wanted to replay a backtest bit-for-bit had no way to
	// fork off a per-backtest RNG and no test could assert determinism.
	//
	// Now: the engine always owns a *rand.Rand (rng). When config.Seed is
	// non-zero, the engine is fully deterministic — any code that goes
	// through e.RNG() (or a future per-backtest fork) will produce a
	// reproducible sequence. When config.Seed is 0, we still initialize
	// rng but with a time-based seed so production runs remain
	// non-deterministic.
	var rng *rand.Rand
	if config.Seed != 0 {
		rng = rand.New(rand.NewSource(config.Seed))
	} else {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	// Initialize default execution service
	execConfig := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: config.CommissionRate,
		MinCommission:  config.Trading.MinCommission,
		InitialCapital: config.InitialCapital,
	}
	executionService := NewBacktestExecutionService(execConfig)
	componentLogger := logger.With().Str("component", "backtest_engine").Logger()

	executionBridge := NewExecutionBridge(componentLogger)
	executionBridge.Set(executionService)

	// P1-18 (ADR-020): 默认 LRU 1000 容量,防止长跑场景下无界增长 OOM。
	// 测试或生产可用 WithStateStore(NoopStateStore) 注入自定义实现。
	stateStore := NewLRUStateStore(DefaultStateStoreCapacity)

	eng := &Engine{
		config:             config,
		provider:           provider,
		strategyServiceURL: strategyServiceURL,
		riskServiceURL:     riskServiceURL,
		httpClient:         httpclient.New("", 30*time.Second, 3),
		logger:             componentLogger,
		cache:              NewCacheManager(componentLogger),
		factor:             NewFactorCacheAccessor(componentLogger),
		stateStore:         stateStore,
		liveBridge:         NewLiveBridge(componentLogger),
		executionBridge:    executionBridge,
		rng:                rng,
	}
	return eng, nil
}

// NewEngineWithOptions creates a new Engine from a parsed Config + Provider,
// then applies a list of EngineOption (functional injection).
//
// P1-19 (ADR-020): the new construction path. Preferred for new code:
//   eng, err := NewEngineWithOptions(cfg, provider,
//       WithRiskManager(rm),
//       WithDataAdapter(adapter),
//       WithLiveTrader(mockTrader),
//   )
//
// Backward compat: NewEngine(v, provider, logger) remains as a thin
// wrapper that parses config from viper and calls NewEngineWithOptions
// (no EngineOptions). Caller-controlled options should use this new path.
func NewEngineWithOptions(cfg Config, provider marketdata.Provider, opts ...EngineOption) (*Engine, error) {
	if provider == nil {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput, "provider is required").WithOperation("NewEngineWithOptions")
	}

	// Apply defaults identical to NewEngine so callers passing a partial
	// Config get the same behavior.
	if cfg.InitialCapital == 0 {
		cfg.InitialCapital = DefaultInitialCapital
	}
	if cfg.CommissionRate == 0 {
		cfg.CommissionRate = DefaultCommissionRate
	}
	if cfg.SlippageRate == 0 {
		cfg.SlippageRate = DefaultSlippageRate
	}
	if cfg.RiskFreeRate == 0 {
		cfg.RiskFreeRate = DefaultRiskFreeRate
	}
	if cfg.Trading.StampTaxRate == 0 {
		cfg.Trading = defaultTradingConfig()
	}

	// Initialize random seed (mirrors NewEngine; P0-5).
	var rng *rand.Rand
	if cfg.Seed != 0 {
		rng = rand.New(rand.NewSource(cfg.Seed))
	} else {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	componentLogger := zerolog.New(nil).With().Str("component", "backtest_engine").Logger()

	// Default ExecutionService (mirrors NewEngine).
	execConfig := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: cfg.CommissionRate,
		MinCommission:  cfg.Trading.MinCommission,
		InitialCapital: cfg.InitialCapital,
	}
	executionBridge := NewExecutionBridge(componentLogger)
	executionBridge.Set(NewBacktestExecutionService(execConfig))

	// P1-18 (ADR-020): 默认 LRU StateStore;通过 WithStateStore 可覆盖
	// (例如测试用 NoopStateStore,生产用 PersistentStateStore)。
	stateStore := NewLRUStateStore(DefaultStateStoreCapacity)

	eng := &Engine{
		config:          cfg,
		provider:        provider,
		httpClient:      httpclient.New("", 30*time.Second, 3),
		logger:          componentLogger,
		cache:           NewCacheManager(componentLogger),
		factor:          NewFactorCacheAccessor(componentLogger),
		stateStore:      stateStore,
		liveBridge:      NewLiveBridge(componentLogger),
		executionBridge: executionBridge,
		rng:             rng,
	}
	applyOptions(eng, opts)
	return eng, nil
}

func (e *Engine) SetDataAdapter(adapter *marketdata.DataAdapter) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dataAdapter = adapter
	if adapter != nil {
		e.logger.Info().Str("source", adapter.Primary()).Msg("DataAdapter attached to engine")
	}
}

// RNG returns the engine's *rand.Rand. The stream is fully deterministic
// when NewEngine was called with a non-zero Config.Seed; otherwise it is
// seeded from time.Now() and is non-replayable.
//
// Callers that need a stable, byte-equal stream across processes
// (regression tests, golden fixtures, property tests) MUST construct the
// engine with a fixed seed. Callers that need a per-backtest sub-stream
// should fork from this one with rand.New(rand.NewSource(e.RNG().Int63()))
// — that way the top-level seed is the only thing that needs to be
// recorded in test fixtures.
//
// Concurrency: *rand.Rand methods are safe for concurrent use (Go std
// documents this since 1.0), so callers may share e.RNG() across
// goroutines without extra synchronization.
func (e *Engine) RNG() *rand.Rand {
	return e.rng
}

func (e *Engine) SetStore(store *storage.PostgresStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = store
	if store != nil {
		e.logger.Info().Msg("PostgresStore attached to engine — factor cache warming enabled")
	}
}

func (e *Engine) DataAdapter() *marketdata.DataAdapter {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dataAdapter
}

func (e *Engine) SwitchDataSource(ctx context.Context, name string, p marketdata.Provider) error {
	e.mu.RLock()
	adapter := e.dataAdapter
	e.mu.RUnlock()

	if adapter == nil {
		return fmt.Errorf("no DataAdapter set — call SetDataAdapter first")
	}
	if err := adapter.SetPrimary(name, p); err != nil {
		return err
	}
	e.logger.Info().Str("new_source", name).Msg("Data source switched via engine")
	return nil
}

func (e *Engine) effectiveProvider() marketdata.Provider {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.dataAdapter != nil {
		return e.dataAdapter
	}
	return e.provider
}

// RunBacktest executes a backtest with the given parameters.
func (e *Engine) RunBacktest(ctx context.Context, req BacktestRequest) (*BacktestResponse, error) {
	startDate, endDate, err := e.parseBacktestDateRange(ctx, req)
	if err != nil {
		return nil, err
	}

	initialCapital := req.InitialCapital
	if initialCapital <= 0 {
		initialCapital = e.config.InitialCapital
	}
	riskFreeRate := req.RiskFreeRate
	if riskFreeRate <= 0 {
		riskFreeRate = e.config.RiskFreeRate
	}

	stockPool, err := e.resolveStockPool(ctx, req, startDate)
	if err != nil {
		return nil, err
	}

	backtestID := uuid.New().String()
	state := e.newBacktestState(backtestID, req, stockPool, startDate, endDate, initialCapital, riskFreeRate)
	e.stateStore.Put(backtestID, state)

	result, err := e.runBacktestInternal(ctx, state)
	if err != nil {
		state.SetStatus("failed")
		state.SetError(err)
		state.Freeze()
		return &BacktestResponse{
			ID:        backtestID,
			Status:    "failed",
			Error:     err.Error(),
			StartedAt: state.StartedAt.Format(time.RFC3339),
		}, err
	}

	state.SetResult(result)
	state.SetCompletedAt(time.Now())
	state.SetStatus("completed")
	state.Freeze()

	return e.buildBacktestResponse(backtestID, req, state, result, initialCapital), nil
}

// parseBacktestDateRange parses and validates the request date range and
// verifies the trading calendar has data for it.
func (e *Engine) parseBacktestDateRange(ctx context.Context, req BacktestRequest) (time.Time, time.Time, error) {
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return time.Time{}, time.Time{}, apperrors.Wrap(err, apperrors.ErrCodeInvalidInput, "invalid start_date format: "+req.StartDate, "RunBacktest")
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return time.Time{}, time.Time{}, apperrors.Wrap(err, apperrors.ErrCodeInvalidInput, "invalid end_date format: "+req.EndDate, "RunBacktest")
	}

	hasCalendar, err := e.checkCalendarExists(ctx, startDate, endDate)
	if err != nil {
		e.logger.Warn().Err(err).Msg("Calendar check error, proceeding anyway")
	} else if !hasCalendar {
		return time.Time{}, time.Time{}, apperrors.New(apperrors.ErrCodeInvalidInput, "trading calendar not synced, please run POST /sync/calendar first (with exchange 'SSE' or 'both')").WithOperation("RunBacktest")
	}
	return startDate, endDate, nil
}

// resolveStockPool returns the stock pool from the request, expanding index
// constituents when an IndexCode is supplied and StockPool is empty.
func (e *Engine) resolveStockPool(ctx context.Context, req BacktestRequest, startDate time.Time) ([]string, error) {
	stockPool := req.StockPool
	if len(stockPool) == 0 && req.IndexCode != "" {
		e.mu.RLock()
		st := e.store
		e.mu.RUnlock()
		if st != nil {
			constituents, err := st.GetIndexConstituentsByDate(ctx, req.IndexCode, startDate)
			if err != nil {
				e.logger.Warn().Err(err).Str("index_code", req.IndexCode).Msg("Failed to load index constituents")
			} else if len(constituents) > 0 {
				stockPool = constituents
				e.logger.Info().Str("index_code", req.IndexCode).Int("constituents", len(constituents)).Msg("Using index constituents as stock pool")
			}
		}
	}
	if len(stockPool) == 0 {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput, "stock_pool or index_code is required").WithOperation("RunBacktest")
	}
	return stockPool, nil
}

// newBacktestState constructs the initial BacktestState for a run.
func (e *Engine) newBacktestState(backtestID string, req BacktestRequest, stockPool []string, startDate, endDate time.Time, initialCapital, riskFreeRate float64) *BacktestState {
	return &BacktestState{
		ID:     backtestID,
		Status: "running",
		Params: domain.BacktestParams{
			StrategyName:   req.Strategy,
			StockPool:      stockPool,
			StartDate:      startDate,
			EndDate:        endDate,
			InitialCapital: initialCapital,
			RiskFreeRate:   riskFreeRate,
		},
		StartedAt: time.Now(),
		Tracker: NewTracker(
			initialCapital,
			e.config.CommissionRate,
			e.config.SlippageRate,
			e.config.Trading,
			e.logger,
		),
		targetPositions: make(map[string]*domain.TargetPosition),
	}
}

// buildBacktestResponse assembles the success BacktestResponse from the
// completed state and result.
func (e *Engine) buildBacktestResponse(backtestID string, req BacktestRequest, state *BacktestState, result *domain.BacktestResult, initialCapital float64) *BacktestResponse {
	return &BacktestResponse{
		ID:              backtestID,
		Status:          "completed",
		Strategy:        req.Strategy,
		StrategyGitHash: lookupStrategyGitHash(req.Strategy),
		StartDate:       req.StartDate,
		EndDate:         req.EndDate,
		TotalReturn:     result.TotalReturn,
		AnnualReturn:    result.AnnualReturn,
		SharpeRatio:     result.SharpeRatio,
		SortinoRatio:    result.SortinoRatio,
		MaxDrawdown:     result.MaxDrawdown,
		MaxDrawdownDate: result.MaxDrawdownDate.Format("2006-01-02"),
		WinRate:         result.WinRate,
		TotalTrades:     result.TotalTrades,
		WinTrades:       result.WinTrades,
		LoseTrades:      result.LoseTrades,
		AvgHoldingDays:  result.AvgHoldingDays,
		CalmarRatio:     result.CalmarRatio,
		StartedAt:       state.StartedAt.Format(time.RFC3339),
		CompletedAt:     state.GetCompletedAt().Format(time.RFC3339),
		PortfolioValues: result.PortfolioValues,
		Trades:          result.Trades,
		StockPool:       req.StockPool,
		InitialCapital:  initialCapital,
	}
}

// lookupStrategyGitHash returns the short git hash of the currently-loaded
// strategy plugin with the given name, or "" if the plugin loader is not
// initialized or the strategy is not loaded as a plugin (e.g. it is
// served by the external strategy-service).
//
// This supports the VISION.md requirement "Track which strategy version
// ran which backtest": the hash is stamped onto every BacktestResponse
// so that downstream tooling can correlate a backtest result with the
// exact source code that produced it.
func lookupStrategyGitHash(strategyName string) string {
	if strategy.GlobalPluginLoader == nil {
		return ""
	}
	info, err := strategy.GlobalPluginLoader.Get(strategyName)
	if err != nil || info == nil {
		return ""
	}
	return info.GitHash
}

// runBacktestInternal contains the core backtest loop.
func (e *Engine) runBacktestInternal(ctx context.Context, state *BacktestState) (*domain.BacktestResult, error) {
	params := state.Params
	logger := e.logger.With().
		Str("backtest_id", state.ID).
		Str("strategy", params.StrategyName).
		Time("start_date", params.StartDate).
		Time("end_date", params.EndDate).
		Logger()

	logger.Info().Msg("Starting backtest")

	warmCtx, warmCancel := context.WithTimeout(ctx, 2*time.Minute)
	if err := e.warmCache(warmCtx, params.StockPool, params.StartDate, params.EndDate); err != nil {
		warmCancel()
		logger.Warn().Err(err).Msg("Cache warm-up failed — continuing without pre-cached data")
	} else {
		warmCancel()
		logger.Info().Msg("Cache warm-up completed")
	}

	if err := e.warmFactorCache(ctx, params.StartDate, params.EndDate); err != nil {
		logger.Warn().Err(err).Msg("Factor cache warm-up failed — strategies will fall back to HTTP computation")
	} else if e.factor.Len() > 0 {
		logger.Info().Msg("Factor cache warm-up completed")
	}

	tradingDays, err := e.getTradingDays(ctx, params.StartDate, params.EndDate)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, "failed to get trading days", "RunBacktest")
	}

	if len(tradingDays) == 0 {
		return nil, apperrors.New(apperrors.ErrCodeDataQuality, "no trading days found in range").WithOperation("RunBacktest")
	}

	logger.Info().Int("trading_days", len(tradingDays)).Msg("Retrieved trading days")

	ca := e.loadCorporateActions(ctx, params.StartDate, params.EndDate, logger)
	prevCloseCache := make(map[string]float64)
	var pricesCache map[string]float64

	for i, date := range tradingDays {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if i%20 == 0 {
			logger.Debug().
				Int("day", i).
				Int("total", len(tradingDays)).
				Str("date", date.Format("2006-01-02")).
				Msg("Processing day")
		}

		marketDataCache, pricesCache, _, updatedPrevClose := e.fetchMarketDataForDay(
			ctx, params.StockPool, params, date, prevCloseCache, logger,
		)
		prevCloseCache = updatedPrevClose

		regime, err := e.detectRegime(ctx, marketDataCache)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to detect market regime, using default")
			regime = &domain.MarketRegime{
				Trend:      "sideways",
				Volatility: "medium",
				Sentiment:  0.0,
				Timestamp:  date,
			}
		}

		signals, err := e.getSignals(ctx, params.StrategyName, params.StockPool, marketDataCache, date, state.Tracker)
		if err != nil {
			logger.Warn().
				Time("date", date).
				Err(err).
				Msg("Failed to get signals, skipping day")
		}

		e.processSignalsAndExecuteTrades(ctx, state, signals, marketDataCache, pricesCache, regime, date, logger)

		e.processStopLosses(state, pricesCache, marketDataCache, date, logger)

		e.processCorporateActions(state, ca, date, logger)

		state.Tracker.RecordDailyValue(date, pricesCache)
		state.Tracker.AdvanceDay(date)

		for _, symbol := range params.StockPool {
			if ohlcvData, ok := marketDataCache[symbol]; ok && len(ohlcvData) > 0 {
				todayBar := ohlcvData[len(ohlcvData)-1]
				if todayBar.LimitUp {
					prevCloseCache[symbol] = todayBar.Close
				} else if todayBar.Close > 0 {
					prevCloseCache[symbol] = todayBar.Close
				}
			}
		}
	}

	lastTradingDay := tradingDays[len(tradingDays)-1]
	e.forceCloseAllPositions(state, pricesCache, lastTradingDay, logger)

	state.Tracker.RecordDailyValue(lastTradingDay, pricesCache)

	portfolioValues := state.Tracker.GetPortfolioValues()
	trades := state.Tracker.GetTrades()

	result := GenerateBacktestResult(
		portfolioValues,
		trades,
		params.RiskFreeRate,
		params.StartDate,
		params.EndDate,
		params.InitialCapital,
	)

	logger.Info().
		Float64("total_return", result.TotalReturn).
		Float64("sharpe_ratio", result.SharpeRatio).
		Float64("max_drawdown", result.MaxDrawdown).
		Int("total_trades", result.TotalTrades).
		Msg("Backtest completed")

	return &result, nil
}

// checkCalendarExists verifies the trading calendar has entries for the given range.
func (e *Engine) checkCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	e.mu.RLock()
	store := e.store
	e.mu.RUnlock()

	if store != nil {
		days, err := store.GetTradingDates(ctx, start, end)
		if err != nil {
			e.logger.Warn().Err(err).Msg("Store calendar check failed, falling back to provider")
		} else {
			return len(days) > 0, nil
		}
	}

	ok, err := e.effectiveProvider().CheckCalendarExists(ctx, start, end)
	if err != nil {
		e.logger.Warn().Err(err).Msg("Provider calendar check failed")
		return false, nil
	}
	return ok, nil
}

// warmCache pre-fetches all OHLCV data for the stock universe into the L1
// in-memory cache (e.inMemoryOHLCV) using the provider's bulk endpoint.
//
// If the L1 cache was already populated (via LoadOHLCVInMemory) and contains
// entries for the requested symbols, this function returns immediately — the
// caller has already done the work. This is the common path for in-memory and
// benchmark backtests.
//
// P1-16 (ADR-020): thin shim to cache.Warm(). The fast-path skip,
// bulk fetch, sort, and atomic publish are now in pkg/backtest/cache.go.
func (e *Engine) warmCache(ctx context.Context, symbols []string, start, end time.Time) error {
	return e.cache.Warm(ctx, symbols, start, end, e.effectiveProvider)
}

// P1-16 (ADR-020): thin shim to factor.Warm().
// Typed-nil guard: e.store is a *storage.PostgresStore (concrete pointer).
// Passing it directly to an interface param would create a non-nil
// interface (Go's typed-nil trap), causing factor.Warm to dereference
// a nil receiver. Check the underlying pointer first.
func (e *Engine) warmFactorCache(ctx context.Context, start, end time.Time) error {
	e.mu.RLock()
	store := e.store
	e.mu.RUnlock()
	if store == nil {
		return nil
	}
	return e.factor.Warm(ctx, start, end, store)
}

// getTradingDays retrieves trading days from data service.
func (e *Engine) getTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	days, err := e.effectiveProvider().GetTradingDays(ctx, start, end)
	if err != nil {
		return nil, err
	}

	sort.Slice(days, func(i, j int) bool {
		return days[i].Before(days[j])
	})

	return days, nil
}

// getOHLCV retrieves OHLCV data for a symbol.
// L1 cache hit (CacheManager) → zero-latency return with date-range filtering.
// L1 cache miss → fallback to provider (HTTP or InMemoryProvider).
//
// P1-16 (ADR-020): thin shim to cache.Get(). The atomic-pointer snapshot
// load, binary-search range filter, and provider fallback all live in
// pkg/backtest/cache.go. See CacheManager doc for hot-path invariants.
func (e *Engine) getOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	return e.cache.Get(ctx, symbol, start, end, e.effectiveProvider)
}

// dateRangeBounds was promoted to DateRangeBounds (exported) in
// pkg/backtest/cache.go (P1-16 ADR-020). The unexported alias below is
// kept for any in-package callers that still reference the old name.
func dateRangeBounds(bars []domain.OHLCV, start, end time.Time) (int, int) {
	return DateRangeBounds(bars, start, end)
}

// detectRegime detects market regime using risk service.
func (e *Engine) detectRegime(ctx context.Context, marketData map[string][]domain.OHLCV) (*domain.MarketRegime, error) {
	// Merge all OHLCV data for regime detection
	var allData []domain.OHLCV
	for _, data := range marketData {
		allData = append(allData, data...)
	}

	if len(allData) < 20 {
		return &domain.MarketRegime{
			Trend:      "sideways",
			Volatility: "medium",
			Sentiment:  0.0,
			Timestamp:  time.Now(),
		}, nil
	}

	e.mu.RLock()
	rm := e.riskManager
	e.mu.RUnlock()

	if rm != nil {
		regime, err := rm.DetectRegime(ctx, allData)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, "in-process regime detection failed", "detectRegime")
		}
		return regime, nil
	}

	url := fmt.Sprintf("%s/detect_regime", e.riskServiceURL)

	reqBody := struct {
		Data []domain.OHLCV `json:"data"`
	}{Data: allData}

	resp, err := e.httpClient.Post(ctx, url, reqBody)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, apperrors.Unavailable("risk", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var regime domain.MarketRegime
	if err := json.Unmarshal(resp.Body, &regime); err != nil {
		return nil, err
	}

	return &regime, nil
}

// getSignals retrieves trading signals from strategy service.
// It first tries the local strategy registry (plugins/ directory) and
// falls back to the external strategy service on miss.
func (e *Engine) getSignals(ctx context.Context, strategyName string, stockPool []string, marketData map[string][]domain.OHLCV, date time.Time, tracker *Tracker) ([]domain.Signal, error) {
	// Step 1: Try local strategy registry first (plugins/ directory)
	if strat, err := strategy.DefaultRegistry.Get(strategyName); err == nil {
		return e.getSignalsFromLocalStrategy(ctx, strat, strategyName, marketData, date, tracker)
	}
	// Step 2: Fall back to external strategy service
	return e.getSignalsFromStrategyService(ctx, strategyName, stockPool, marketData, date)
}

// getSignalsFromLocalStrategy generates signals via a locally-loaded strategy plugin.
func (e *Engine) getSignalsFromLocalStrategy(ctx context.Context, strat strategy.Strategy, strategyName string, marketData map[string][]domain.OHLCV, date time.Time, tracker *Tracker) ([]domain.Signal, error) {
	if fa, ok := strat.(strategy.FactorAware); ok {
		fa.SetFactorCache(e.GetFactorZScore)
	}

	prices := extractLatestPrices(marketData)
	portfolio := tracker.GetPortfolio(prices)

	signals, err := strat.GenerateSignals(ctx, marketData, portfolio)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, fmt.Sprintf("local strategy %s failed", strategyName), "getSignals")
	}

	domainSignals := convertStrategySignals(signals, date)

	e.logger.Debug().
		Str("strategy", strategyName).
		Int("signals", len(domainSignals)).
		Msg("Generated signals from local registry")
	return domainSignals, nil
}

// extractLatestPrices builds a symbol → latest close price map from market data.
func extractLatestPrices(marketData map[string][]domain.OHLCV) map[string]float64 {
	prices := make(map[string]float64)
	for sym, bars := range marketData {
		if len(bars) > 0 {
			prices[sym] = bars[len(bars)-1].Close
		}
	}
	return prices
}

// convertStrategySignals converts strategy plugin signals into domain Signals,
// filtering out hold actions and normalizing direction / order type / price.
func convertStrategySignals(signals []strategy.Signal, defaultDate time.Time) []domain.Signal {
	domainSignals := make([]domain.Signal, 0, len(signals))
	for _, s := range signals {
		if s.Action == "hold" {
			continue
		}
		dir := resolveDirection(s)
		if dir == "" {
			continue
		}

		sigDate := defaultDate
		if d, ok := s.Date.(time.Time); ok && !d.IsZero() {
			sigDate = d
		}

		factors := s.Factors
		if factors == nil {
			factors = make(map[string]float64)
		}
		metadata := s.Metadata
		if metadata == nil {
			metadata = make(map[string]interface{})
		}

		limitPrice := s.LimitPrice
		if limitPrice == 0 {
			limitPrice = s.Price
		}
		orderType := s.OrderType
		if orderType == "" {
			orderType = domain.OrderTypeMarket
		}

		domainSignals = append(domainSignals, domain.Signal{
			Symbol:         s.Symbol,
			Date:           sigDate,
			Direction:      dir,
			Strength:       s.Strength,
			Factors:        factors,
			Metadata:       metadata,
			LimitPrice:     limitPrice,
			OrderType:      orderType,
			CompositeScore: s.Strength,
		})
	}
	return domainSignals
}

// resolveDirection maps a strategy signal's Action/Direction to a domain Direction.
// Returns empty string when the signal should be skipped.
func resolveDirection(s strategy.Signal) domain.Direction {
	dir := s.Direction
	if dir != "" && dir != domain.DirectionHold {
		return dir
	}
	if s.Action == "buy" {
		return domain.DirectionLong
	}
	if s.Action == "sell" {
		return domain.DirectionClose
	}
	return ""
}

// getSignalsFromStrategyService calls the external strategy service to generate signals.
func (e *Engine) getSignalsFromStrategyService(ctx context.Context, strategyName string, stockPool []string, marketData map[string][]domain.OHLCV, date time.Time) ([]domain.Signal, error) {
	url := fmt.Sprintf("%s/strategies/%s/signals", e.strategyServiceURL, strategyName)

	stocks := make([]domain.Stock, len(stockPool))
	for i, sym := range stockPool {
		stocks[i] = domain.Stock{Symbol: sym}
	}

	reqBody := struct {
		StockPool   []string                        `json:"stock_pool"`
		Stocks      []domain.Stock                  `json:"stocks"`
		MarketData  map[string][]domain.OHLCV       `json:"market_data"`
		Fundamental map[string][]domain.Fundamental `json:"fundamental"`
		Date        string                          `json:"date"`
	}{
		StockPool:   stockPool,
		Stocks:      stocks,
		MarketData:  marketData,
		Fundamental: map[string][]domain.Fundamental{},
		Date:        date.Format("2006-01-02"),
	}

	resp, err := e.httpClient.Post(ctx, url, reqBody)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, apperrors.Unavailable("strategy", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var result struct {
		Signals []domain.Signal `json:"signals"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, err
	}

	return result.Signals, nil
}

// calculatePosition calculates position size using risk service.
// When an in-process RiskManager is set (via SetRiskManager), computes locally
// without HTTP calls. Otherwise falls back to the external risk-service.
func (e *Engine) calculatePosition(ctx context.Context, signal domain.Signal, portfolio *domain.Portfolio, regime *domain.MarketRegime, currentPrice float64) (domain.PositionSize, error) {
	e.mu.RLock()
	rm := e.riskManager
	e.mu.RUnlock()

	if rm != nil {
		// P1-16 (ADR-020): read snapshot via CacheManager
		var ohlcv []domain.OHLCV
		if snap := e.cache.inMemoryOHLCVAtomic.Load(); snap != nil {
			ohlcv = (*snap)[signal.Symbol]
		}
		pos, err := rm.CalculatePosition(ctx, signal, portfolio, regime, currentPrice, ohlcv)
		if err != nil {
			return domain.PositionSize{}, apperrors.Wrap(err, apperrors.ErrCodeInternal, "in-process position calculation failed", "calculatePosition")
		}
		return pos, nil
	}

	url := fmt.Sprintf("%s/calculate_position", e.riskServiceURL)

	reqBody := struct {
		Signal       domain.Signal       `json:"signal"`
		Portfolio    domain.Portfolio    `json:"portfolio"`
		Regime       domain.MarketRegime `json:"regime"`
		CurrentPrice float64             `json:"current_price"`
	}{
		Signal:       signal,
		Portfolio:    *portfolio,
		Regime:       *regime,
		CurrentPrice: currentPrice,
	}

	resp, err := e.httpClient.Post(ctx, url, reqBody)
	if err != nil {
		return domain.PositionSize{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return domain.PositionSize{}, apperrors.Unavailable("risk", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var positionSize domain.PositionSize
	if err := json.Unmarshal(resp.Body, &positionSize); err != nil {
		return domain.PositionSize{}, err
	}

	return positionSize, nil
}

// calculatePositionsBatch calculates position sizes for multiple signals at once.
// Uses the RiskManager's batch method when available for better performance.
func (e *Engine) calculatePositionsBatch(
	ctx context.Context,
	signals []domain.Signal,
	portfolio *domain.Portfolio,
	regime *domain.MarketRegime,
	pricesCache map[string]float64,
	marketDataCache map[string][]domain.OHLCV,
) (map[string]domain.PositionSize, error) {
	e.mu.RLock()
	rm := e.riskManager
	e.mu.RUnlock()

	if rm != nil {
		return rm.CalculatePositionsBatch(ctx, signals, portfolio, regime, pricesCache, marketDataCache)
	}

	results := make(map[string]domain.PositionSize, len(signals))
	for _, sig := range signals {
		ps, err := e.calculatePosition(ctx, sig, portfolio, regime, pricesCache[sig.Symbol])
		if err != nil {
			continue
		}
		results[sig.Symbol] = ps
	}
	return results, nil
}

// checkStopLosses checks and triggers stop losses using risk service.
// When an in-process RiskManager is set (via SetRiskManager), computes locally
// without HTTP calls. Otherwise falls back to the external risk-service.
func (e *Engine) checkStopLosses(ctx context.Context, tracker *Tracker, prices map[string]float64) ([]domain.StopLossEvent, error) {
	positions := tracker.GetAllPositions()
	if len(positions) == 0 {
		return nil, nil
	}

	e.mu.RLock()
	rm := e.riskManager
	e.mu.RUnlock()

	if rm != nil {
		var positionsList []domain.Position
		for _, pos := range positions {
			positionsList = append(positionsList, pos)
		}
		events, err := rm.CheckStopLoss(ctx, positionsList, prices)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, "in-process stop loss check failed", "checkStopLosses")
		}
		return events, nil
	}

	url := fmt.Sprintf("%s/check_stoploss", e.riskServiceURL)

	// Convert positions map to slice
	var positionsList []domain.Position
	for _, pos := range positions {
		positionsList = append(positionsList, pos)
	}

	reqBody := struct {
		Positions map[string]float64 `json:"prices"`
	}{
		Positions: prices,
	}

	resp, err := e.httpClient.Post(ctx, url, reqBody)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, apperrors.Unavailable("risk", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var events struct {
		Events []domain.StopLossEvent `json:"events"`
	}
	if err := json.Unmarshal(resp.Body, &events); err != nil {
		return nil, err
	}

	return events.Events, nil
}

// checkStopLossesWithATR checks stop losses using pre-computed ATR data.
// This avoids redundant ATR calculation inside the stop loss checker.
func (e *Engine) checkStopLossesWithATR(ctx context.Context, tracker *Tracker, prices map[string]float64, precomputedATR map[string]float64) ([]domain.StopLossEvent, error) {
	positions := tracker.GetAllPositions()
	if len(positions) == 0 {
		return nil, nil
	}

	e.mu.RLock()
	rm := e.riskManager
	e.mu.RUnlock()

	if rm != nil {
		var positionsList []domain.Position
		for _, pos := range positions {
			positionsList = append(positionsList, pos)
		}
		regime := risk.InferRegimeFromMarket(positionsList, prices)
		events, err := rm.GetStopLossChecker().CheckStopLossWithRegime(ctx, positionsList, prices, precomputedATR, regime)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, "in-process stop loss check with ATR failed", "checkStopLossesWithATR")
		}
		return events, nil
	}

	return e.checkStopLosses(ctx, tracker, prices)
}

// GetBacktestResult retrieves the result of a completed backtest.
func (e *Engine) GetBacktestResult(backtestID string) (*domain.BacktestResult, error) {
	state, ok := e.stateStore.Get(backtestID)
	if !ok || state == nil {
		return nil, apperrors.NotFound("backtest", backtestID)
	}

	status := state.GetStatus()
	if status != "completed" {
		return nil, apperrors.New(apperrors.ErrCodeConflict, fmt.Sprintf("backtest not completed: %s", status)).WithOperation("GetBacktestResult")
	}

	return state.GetResult(), nil
}

// GetBacktestTrades retrieves trades for a backtest.
func (e *Engine) GetBacktestTrades(backtestID string) ([]domain.Trade, error) {
	state, ok := e.stateStore.Get(backtestID)
	if !ok || state == nil {
		return nil, apperrors.NotFound("backtest", backtestID)
	}

	return state.Tracker.GetTrades(), nil
}

// GetBacktestEquity retrieves equity curve for a backtest.
func (e *Engine) GetBacktestEquity(backtestID string) ([]domain.PortfolioValue, error) {
	state, ok := e.stateStore.Get(backtestID)
	if !ok || state == nil {
		return nil, apperrors.NotFound("backtest", backtestID)
	}

	return state.Tracker.GetEquityCurve(), nil
}

// GetBacktestStatus returns the status of a backtest.
func (e *Engine) GetBacktestStatus(backtestID string) (string, error) {
	state, ok := e.stateStore.Get(backtestID)
	if !ok || state == nil {
		return "", apperrors.NotFound("backtest", backtestID)
	}

	return state.GetStatus(), nil
}

func (e *Engine) GetBacktestParams(backtestID string) (domain.BacktestParams, error) {
	state, ok := e.stateStore.Get(backtestID)
	if !ok || state == nil {
		return domain.BacktestParams{}, apperrors.NotFound("backtest", backtestID)
	}

	return state.Params, nil
}

// LoadOHLCVInMemory directly populates the L1 in-memory OHLCV cache.
// The map key is symbol; each slice is sorted by date ascending.
// After calling this, getOHLCV returns cached data instantly (L1 hit).
// Pass nil to clear the L1 cache (forces provider fallback on next getOHLCV).
//
// P1-16 (ADR-020): thin shim to cache.Load().
func (e *Engine) LoadOHLCVInMemory(data map[string][]domain.OHLCV) {
	e.cache.Load(data)
}

// CacheManager returns the underlying CacheManager sub-component (P1-16).
// Prefer this accessor in new code over the LoadOHLCVInMemory shim.
func (e *Engine) CacheManager() *CacheManager {
	return e.cache
}

// LoadFactorCache loads pre-computed factor z-scores into the L1 factor cache.
// The input is typically from storage.GetFactorCacheRange() or data.LoadFactorCacheIntoMap().
// After calling this, GetFactorZScore returns cached z-scores instantly (L1 hit).
// Pass nil to clear the factor cache.
//
// P1-16 (ADR-020): thin shim to factor.Load().
func (e *Engine) LoadFactorCache(data map[domain.FactorType]map[time.Time]map[string]float64) {
	e.factor.Load(data)
}

// FactorCache returns the underlying FactorCacheAccessor sub-component (P1-16).
// Prefer this accessor in new code over the LoadFactorCache shim.
func (e *Engine) FactorCache() *FactorCacheAccessor {
	return e.factor
}

// GetFactorZScore returns the pre-computed z-score for a given factor, date, and symbol.
// Returns (0, false) if the factor cache is not loaded or the entry doesn't exist.
//
// P1-16 (ADR-020): thin shim to factor.Get().
func (e *Engine) GetFactorZScore(factor domain.FactorType, date time.Time, symbol string) (float64, bool) {
	return e.factor.Get(factor, date, symbol)
}

// SetRiskManager injects an in-process risk manager.
// When set, calculatePosition/checkStopLosses/detectRegime use local computation
// (zero HTTP latency) instead of calling the external risk-service.
// Pass nil to clear and fall back to HTTP-based risk service calls.
func (e *Engine) SetRiskManager(rm *risk.RiskManager) {
	e.mu.Lock()
	e.riskManager = rm
	e.mu.Unlock()
}

// GetRiskManager returns the currently attached in-process RiskManager (P1-19).
// Returns nil if not set.
func (e *Engine) GetRiskManager() *risk.RiskManager {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.riskManager
}

// SetParallelWorkers sets the number of concurrent workers for per-stock data fetching.
// Must be called before RunBacktest. Values <= 0 mean sequential (no parallelism).
func (e *Engine) SetParallelWorkers(n int) {
	e.parallelWorkers = n
}

// getStock retrieves stock info (Name, ListDate) from the data service.
func (e *Engine) getStock(ctx context.Context, symbol string) (domain.Stock, error) {
	return e.effectiveProvider().GetStock(ctx, symbol)
}

// hasSTPrefix returns true if the stock name starts with an ST prefix (indicating special treatment).
// Handles edge cases: names shorter than 2 chars cannot be ST stocks.
func hasSTPrefix(name string) bool {
	if len(name) < 2 {
		return false
	}
	prefix := name[:2]
	return prefix == "ST" || prefix == "*ST" || prefix == "SST" || prefix == "S*ST"
}

// SetLiveTrader injects a LiveTrader for bridging backtest signals to live/paper trading.
// When set, the engine can optionally execute signals through the trader instead of
// only simulating internally via Tracker. This is the primary hook for transitioning
// from backtest → paper trading → live trading.
// Pass nil to disable live trading and use pure simulation.
//
// P1-17 (ADR-020): thin shim to liveBridge.Set(). Retained for
// backward compatibility (6 months); new code should use
// e.liveBridge().Set() or EngineOption WithLiveTrader (P1-19).
func (e *Engine) SetLiveTrader(trader live.LiveTrader) {
	e.liveBridge.Set(trader)
}

// SetExecutionService injects a custom ExecutionService for order execution.
// When set, the engine uses this service for all trade execution instead of
// Tracker's built-in logic. Pass nil to fall back to Tracker execution.
//
// P1-17 (ADR-020): thin shim to executionBridge.Set(). Retained for
// backward compatibility (6 months); new code should use
// e.executionBridge().Set() or EngineOption WithExecutionService (P1-19).
func (e *Engine) SetExecutionService(svc ExecutionService) {
	e.executionBridge.Set(svc)
}

// GetExecutionService returns the current ExecutionService.
//
// P1-17 (ADR-020): thin shim to executionBridge.Get(). Retained for
// backward compatibility.
func (e *Engine) GetExecutionService() ExecutionService {
	return e.executionBridge.Get()
}

// GetLiveTrader returns the currently attached LiveTrader, or nil if none.
//
// P1-17 (ADR-020): thin shim to liveBridge.Get(). Retained for
// backward compatibility.
func (e *Engine) GetLiveTrader() live.LiveTrader {
	return e.liveBridge.Get()
}

// LiveBridge returns the underlying LiveBridge sub-component (P1-17).
// Prefer this accessor in new code over the SetLiveTrader shim; the
// shim is kept for backward compat only.
func (e *Engine) LiveBridge() *LiveBridge {
	return e.liveBridge
}

// ExecutionBridge returns the underlying ExecutionBridge sub-component (P1-17).
// Prefer this accessor in new code over the SetExecutionService shim.
func (e *Engine) ExecutionBridge() *ExecutionBridge {
	return e.executionBridge
}

// StateStore returns the underlying StateStore (P1-18).
// The returned store is safe for concurrent use; callers may invoke
// Get / Put / Delete / Len / Evict directly. Typical use is for
// observability (e.g. "how many backtests are tracked?") or for
// out-of-band GC sweeps.
func (e *Engine) StateStore() StateStore {
	return e.stateStore
}

// EvictStates performs a bulk eviction of older backtest states, keeping
// at most keepN entries. keepN=0 evicts all. Returns the number of
// states removed. Useful for periodic GC under bursty batch load.
func (e *Engine) EvictStates(keepN int) int {
	return e.stateStore.Evict(keepN)
}

// ExecuteSignalViaLiveTrader executes a single trading signal through the attached LiveTrader.
// This is the bridge between backtest signal generation and live/paper order execution.
// Returns the order result or nil if no LiveTrader is attached.
// The signal's Direction determines buy (DirectionLong) or sell (DirectionClose).
//
// P1-17 (ADR-020): thin shim to liveBridge.ExecuteSignal(). All order
// type / price / quantity logic now lives in LiveBridge; behavior is
// byte-equivalent to the previous in-line implementation.
func (e *Engine) ExecuteSignalViaLiveTrader(ctx context.Context, signal domain.Signal, currentPrice float64) (*live.OrderResult, error) {
	return e.liveBridge.ExecuteSignal(ctx, signal, currentPrice)
}

// ExecuteSignalsViaLiveTrader executes multiple signals through the LiveTrader in batch.
// This is useful for daily rebalancing where multiple signals are generated at once.
// Returns a map of symbol → order result for successful executions.
//
// P1-17 (ADR-020): thin shim to liveBridge.ExecuteSignals().
func (e *Engine) ExecuteSignalsViaLiveTrader(ctx context.Context, signals []domain.Signal, prices map[string]float64) map[string]*live.OrderResult {
	return e.liveBridge.ExecuteSignals(ctx, signals, prices)
}

// HealthCheckLiveTrader checks the health of the attached LiveTrader.
// Returns nil if healthy or no trader attached, error otherwise.
//
// P1-17 (ADR-020): thin shim to liveBridge.HealthCheck().
func (e *Engine) HealthCheckLiveTrader(ctx context.Context) error {
	return e.liveBridge.HealthCheck(ctx)
}
