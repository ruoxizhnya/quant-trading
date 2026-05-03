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

	// Active backtest states (supports concurrent backtests)
	btMu      sync.RWMutex
	backtests map[string]*BacktestState

	// Logger
	logger zerolog.Logger

	// L1 in-memory OHLCV cache (per-backtest-lifecycle).
	// Key: symbol, Value: all OHLCV bars for that symbol (sorted by date).
	// Populated via warmCache() or LoadOHLCVInMemory().
	// getOHLCV() checks this first — zero-latency hit, falls back to provider on miss.
	// This is the primary speed optimization: eliminates N×D HTTP calls
	// (N=stocks, D=trading days) during a backtest run.
	inMemoryOHLCV map[string][]domain.OHLCV

	// L1 factor cache (per-backtest-lifecycle).
	// Structure: factorType -> tradeDate -> symbol -> zScore.
	// Populated via LoadFactorCache() before a backtest run.
	// GetFactorZScore() reads from this map — zero-latency hit.
	// Eliminates per-symbol-per-day DB queries for multi-factor strategies.
	factorCache map[domain.FactorType]map[time.Time]map[string]float64

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
}

// BacktestState holds the state of a backtest run.
type BacktestState struct {
	ID              string
	Status          string // "running", "completed", "failed"
	Params          domain.BacktestParams
	Result          *domain.BacktestResult
	Tracker         *Tracker
	StartedAt       time.Time
	CompletedAt     time.Time
	Error           error
	targetPositions map[string]*domain.TargetPosition // symbol -> target vs actual tracking
}

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
	// If Seed is 0, math/rand is not used (no shuffle operations currently exist).
	// Document the seed value used in test fixtures for reproducibility.
	if config.Seed != 0 {
		rand.New(rand.NewSource(config.Seed))
	}

	return &Engine{
		config:             config,
		provider:           provider,
		strategyServiceURL: strategyServiceURL,
		riskServiceURL:     riskServiceURL,
		httpClient:         httpclient.New("", 30*time.Second, 3),
		logger:             logger.With().Str("component", "backtest_engine").Logger(),
		inMemoryOHLCV:      make(map[string][]domain.OHLCV),
	}, nil
}

func (e *Engine) SetDataAdapter(adapter *marketdata.DataAdapter) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dataAdapter = adapter
	if adapter != nil {
		e.logger.Info().Str("source", adapter.Primary()).Msg("DataAdapter attached to engine")
	}
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
	// Parse dates
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInvalidInput, "invalid start_date format: "+req.StartDate, "RunBacktest")
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInvalidInput, "invalid end_date format: "+req.EndDate, "RunBacktest")
	}

	// Pre-check: verify trading calendar has data for the requested date range
	hasCalendar, err := e.checkCalendarExists(ctx, startDate, endDate)
	if err != nil {
		e.logger.Warn().Err(err).Msg("Calendar check error, proceeding anyway")
	} else if !hasCalendar {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput, "trading calendar not synced, please run POST /sync/calendar first (with exchange 'SSE' or 'both')").WithOperation("RunBacktest")
	}

	// Use default initial capital if not provided
	initialCapital := req.InitialCapital
	if initialCapital <= 0 {
		initialCapital = e.config.InitialCapital
	}

	riskFreeRate := req.RiskFreeRate
	if riskFreeRate <= 0 {
		riskFreeRate = e.config.RiskFreeRate
	}

	// Create backtest state
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

	backtestID := uuid.New().String()
	state := &BacktestState{
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

	e.btMu.Lock()
	if e.backtests == nil {
		e.backtests = make(map[string]*BacktestState)
	}
	e.backtests[backtestID] = state
	e.btMu.Unlock()

	// Run the backtest
	result, err := e.runBacktestInternal(ctx, state)
	if err != nil {
		state.Status = "failed"
		state.Error = err
		return &BacktestResponse{
			ID:        backtestID,
			Status:    "failed",
			Error:     err.Error(),
			StartedAt: state.StartedAt.Format(time.RFC3339),
		}, err
	}

	state.Status = "completed"
	state.Result = result
	state.CompletedAt = time.Now()

	return &BacktestResponse{
		ID:              backtestID,
		Status:          "completed",
		Strategy:        req.Strategy,
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
		CompletedAt:     state.CompletedAt.Format(time.RFC3339),
		PortfolioValues: result.PortfolioValues,
		Trades:          result.Trades,
		StockPool:       req.StockPool,
		InitialCapital:  initialCapital,
	}, nil
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
	} else if e.factorCache != nil {
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
func (e *Engine) warmCache(ctx context.Context, symbols []string, start, end time.Time) error {
	if len(symbols) == 0 {
		return nil
	}

	data, err := e.effectiveProvider().BulkLoadOHLCV(ctx, symbols, start, end)
	if err != nil {
		return apperrors.Wrap(err, apperrors.ErrCodeUnavailable, "bulk OHLCV request failed", "warmCache")
	}

	e.mu.Lock()
	for symbol, bars := range data {
		e.inMemoryOHLCV[symbol] = bars
	}
	e.mu.Unlock()

	return nil
}

func (e *Engine) warmFactorCache(ctx context.Context, start, end time.Time) error {
	e.mu.RLock()
	store := e.store
	e.mu.RUnlock()
	if store == nil {
		return nil
	}

	factors := []domain.FactorType{domain.FactorMomentum, domain.FactorValue, domain.FactorQuality}
	combined := make(map[domain.FactorType]map[time.Time]map[string]float64)

	for _, factor := range factors {
		entries, err := store.GetFactorCacheRange(ctx, factor, start, end)
		if err != nil {
			e.logger.Warn().Str("factor", string(factor)).Err(err).Msg("Failed to load factor cache from DB")
			continue
		}
		if len(entries) == 0 {
			e.logger.Info().Str("factor", string(factor)).Msg("No factor cache entries in DB — run sync/factors/all first")
			continue
		}
		for _, entry := range entries {
			if combined[entry.FactorName] == nil {
				combined[entry.FactorName] = make(map[time.Time]map[string]float64)
			}
			if combined[entry.FactorName][entry.TradeDate] == nil {
				combined[entry.FactorName][entry.TradeDate] = make(map[string]float64)
			}
			combined[entry.FactorName][entry.TradeDate][entry.Symbol] = entry.ZScore
		}
		e.logger.Info().
			Str("factor", string(factor)).
			Int("entries", len(entries)).
			Msg("Factor cache loaded from DB")
	}

	if len(combined) > 0 {
		e.mu.Lock()
		e.factorCache = combined
		e.mu.Unlock()
	}

	return nil
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
// L1 cache hit (inMemoryOHLCV) → zero-latency return with date-range filtering.
// L1 cache miss → fallback to provider (HTTP or InMemoryProvider).
func (e *Engine) getOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	e.mu.RLock()
	cached, ok := e.inMemoryOHLCV[symbol]
	e.mu.RUnlock()
	if ok {
		var filtered []domain.OHLCV
		for _, bar := range cached {
			if !bar.Date.Before(start) && !bar.Date.After(end) {
				filtered = append(filtered, bar)
			}
		}
		return filtered, nil
	}

	return e.effectiveProvider().GetOHLCV(ctx, symbol, start, end)
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
func (e *Engine) getSignals(ctx context.Context, strategyName string, stockPool []string, marketData map[string][]domain.OHLCV, date time.Time, tracker *Tracker) ([]domain.Signal, error) {
	// Step 1: Try local strategy registry first (plugins/ directory)
	if strat, err := strategy.DefaultRegistry.Get(strategyName); err == nil {
		if fa, ok := strat.(strategy.FactorAware); ok {
			fa.SetFactorCache(e.GetFactorZScore)
		}

		prices := make(map[string]float64)
		for sym, bars := range marketData {
			if len(bars) > 0 {
				prices[sym] = bars[len(bars)-1].Close
			}
		}
		portfolio := tracker.GetPortfolio(prices)

		signals, err := strat.GenerateSignals(ctx, marketData, portfolio)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, fmt.Sprintf("local strategy %s failed", strategyName), "getSignals")
		}

		domainSignals := make([]domain.Signal, 0, len(signals))
		for _, s := range signals {
			if s.Action == "hold" {
				continue
			}
			dir := s.Direction
			if dir == "" || dir == domain.DirectionHold {
				if s.Action == "buy" {
					dir = domain.DirectionLong
				} else if s.Action == "sell" {
					dir = domain.DirectionClose
				} else {
					continue
				}
			}

			sigDate := date
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

		e.logger.Debug().
			Str("strategy", strategyName).
			Int("signals", len(domainSignals)).
			Msg("Generated signals from local registry")
		return domainSignals, nil
	}

	// Step 2: Fall back to external strategy service
	url := fmt.Sprintf("%s/strategies/%s/signals", e.strategyServiceURL, strategyName)

	// Get stock info from market data keys (symbol only, no external data needed for momentum)
	stocks := make([]domain.Stock, len(stockPool))
	for i, sym := range stockPool {
		stocks[i] = domain.Stock{Symbol: sym}
	}

	// Convert market data to the format expected by strategy service
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
		var ohlcv []domain.OHLCV
		if e.inMemoryOHLCV != nil {
			ohlcv = e.inMemoryOHLCV[signal.Symbol]
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
	e.btMu.RLock()
	state := e.backtests[backtestID]
	e.btMu.RUnlock()

	if state == nil {
		return nil, apperrors.NotFound("backtest", backtestID)
	}

	if state.Status != "completed" {
		return nil, apperrors.New(apperrors.ErrCodeConflict, fmt.Sprintf("backtest not completed: %s", state.Status)).WithOperation("GetBacktestResult")
	}

	return state.Result, nil
}

// GetBacktestTrades retrieves trades for a backtest.
func (e *Engine) GetBacktestTrades(backtestID string) ([]domain.Trade, error) {
	e.btMu.RLock()
	state := e.backtests[backtestID]
	e.btMu.RUnlock()

	if state == nil {
		return nil, apperrors.NotFound("backtest", backtestID)
	}

	return state.Tracker.GetTrades(), nil
}

// GetBacktestEquity retrieves equity curve for a backtest.
func (e *Engine) GetBacktestEquity(backtestID string) ([]domain.PortfolioValue, error) {
	e.btMu.RLock()
	state := e.backtests[backtestID]
	e.btMu.RUnlock()

	if state == nil {
		return nil, apperrors.NotFound("backtest", backtestID)
	}

	return state.Tracker.GetEquityCurve(), nil
}

// GetBacktestStatus returns the status of a backtest.
func (e *Engine) GetBacktestStatus(backtestID string) (string, error) {
	e.btMu.RLock()
	state := e.backtests[backtestID]
	e.btMu.RUnlock()

	if state == nil {
		return "", apperrors.NotFound("backtest", backtestID)
	}

	return state.Status, nil
}

func (e *Engine) GetBacktestParams(backtestID string) (domain.BacktestParams, error) {
	e.btMu.RLock()
	state := e.backtests[backtestID]
	e.btMu.RUnlock()

	if state == nil {
		return domain.BacktestParams{}, apperrors.NotFound("backtest", backtestID)
	}

	return state.Params, nil
}

// LoadOHLCVInMemory directly populates the L1 in-memory OHLCV cache.
// The map key is symbol; each slice is sorted by date ascending.
// After calling this, getOHLCV returns cached data instantly (L1 hit).
// Pass nil to clear the L1 cache (forces provider fallback on next getOHLCV).
func (e *Engine) LoadOHLCVInMemory(data map[string][]domain.OHLCV) {
	e.mu.Lock()
	e.inMemoryOHLCV = data
	e.mu.Unlock()
}

// LoadFactorCache loads pre-computed factor z-scores into the L1 factor cache.
// The input is typically from storage.GetFactorCacheRange() or data.LoadFactorCacheIntoMap().
// After calling this, GetFactorZScore returns cached z-scores instantly (L1 hit).
// Pass nil to clear the factor cache.
func (e *Engine) LoadFactorCache(data map[domain.FactorType]map[time.Time]map[string]float64) {
	e.mu.Lock()
	e.factorCache = data
	e.mu.Unlock()
}

// GetFactorZScore returns the pre-computed z-score for a given factor, date, and symbol.
// Returns (0, false) if the factor cache is not loaded or the entry doesn't exist.
func (e *Engine) GetFactorZScore(factor domain.FactorType, date time.Time, symbol string) (float64, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.factorCache == nil {
		return 0, false
	}
	dateMap, ok := e.factorCache[factor]
	if !ok {
		return 0, false
	}
	symbolMap, ok := dateMap[date]
	if !ok {
		return 0, false
	}
	z, ok := symbolMap[symbol]
	return z, ok
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
