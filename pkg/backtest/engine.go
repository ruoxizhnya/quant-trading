package backtest

import (
	"bytes"
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
	"github.com/spf13/viper"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// Config holds backtest engine configuration.
type Config struct {
	InitialCapital float64 `mapstructure:"initial_capital"`
	CommissionRate float64 `mapstructure:"commission_rate"`
	SlippageRate   float64 `mapstructure:"slippage_rate"`
	RiskFreeRate   float64 `mapstructure:"risk_free_rate"`
	Seed           int64   `mapstructure:"seed"` // Random seed for determinism; 0 = use time-based seed
}

// Price limit constants for A-share stocks
const (
	priceLimitNormal   = 0.10  // ±10% for normal A-shares
	priceLimitST       = 0.05  // ±5% for ST stocks
	priceLimitNew      = 0.20  // ±20% for stocks listed < 60 trading days
	newStockTradeDays  = 60    // threshold for new stock price limit
)

// Engine is the backtesting engine that simulates trading strategies.
type Engine struct {
	mu sync.RWMutex

	// Configuration
	config Config

	// External service clients
	dataServiceURL    string
	strategyServiceURL string
	riskServiceURL    string

	// HTTP client for service communication
	httpClient *http.Client

	// Current backtest state
	currentBacktest *BacktestState

	// Logger
	logger zerolog.Logger

	// In-memory OHLCV cache for backtest speed.
	// Key: symbol, Value: all OHLCV bars for that symbol (sorted by date).
	// When populated via LoadOHLCVInMemory, getOHLCV returns from here
	// instead of making HTTP calls, eliminating per-stock-per-day latency.
	inMemoryOHLCV map[string][]domain.OHLCV

	// ParallelWorkers controls how many goroutines fetch data concurrently
	// inside each trading day. A value <= 0 means sequential (1 worker).
	parallelWorkers int

	// factorCache holds pre-computed factor z-scores loaded from the factor_cache table.
	// Structure: factorName -> tradeDate -> symbol -> zScore
	// Populated via LoadFactorCache before a backtest run.
	factorCache map[domain.FactorType]map[time.Time]map[string]float64
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
	Strategy      string   `json:"strategy" binding:"required"`
	StockPool     []string `json:"stock_pool" binding:"required"`
	StartDate     string   `json:"start_date" binding:"required"`
	EndDate       string   `json:"end_date" binding:"required"`
	InitialCapital float64 `json:"initial_capital"`
	RiskFreeRate  float64  `json:"risk_free_rate"`
}

// BacktestResponse represents the API response for a backtest run.
type BacktestResponse struct {
	ID              string                  `json:"id"`
	Status          string                  `json:"status"`
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
	Trades          []domain.Trade         `json:"trades,omitempty"`
	StockPool       []string               `json:"stock_pool,omitempty"`
	InitialCapital  float64                `json:"initial_capital,omitempty"`
}

// NewEngine creates a new backtest engine.
func NewEngine(v *viper.Viper, logger zerolog.Logger) (*Engine, error) {
	config := Config{}
	if err := v.Sub("backtest").Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backtest config: %w", err)
	}

	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
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
		config.InitialCapital = 1000000.0
	}
	if config.CommissionRate == 0 {
		config.CommissionRate = 0.0003
	}
	if config.SlippageRate == 0 {
		config.SlippageRate = 0.0001
	}
	if config.RiskFreeRate == 0 {
		config.RiskFreeRate = 0.03
	}

	// Initialize random seed for deterministic backtests.
	// If Seed is 0, math/rand is not used (no shuffle operations currently exist).
	// Document the seed value used in test fixtures for reproducibility.
	if config.Seed != 0 {
		rand.Seed(config.Seed)
	}

	return &Engine{
		config:            config,
		dataServiceURL:    dataServiceURL,
		strategyServiceURL: strategyServiceURL,
		riskServiceURL:    riskServiceURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger.With().Str("component", "backtest_engine").Logger(),
	}, nil
}

// RunBacktest executes a backtest with the given parameters.
func (e *Engine) RunBacktest(ctx context.Context, req BacktestRequest) (*BacktestResponse, error) {
	// Parse dates
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start_date format: %w", err)
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid end_date format: %w", err)
	}

	// Pre-check: verify trading calendar has data for the requested date range
	hasCalendar, err := e.checkCalendarExists(ctx, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to check trading calendar: %w", err)
	}
	if !hasCalendar {
		return nil, fmt.Errorf("trading calendar not synced, please run POST /sync/calendar first (with exchange 'SSE' or 'both')")
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
	backtestID := uuid.New().String()
	state := &BacktestState{
		ID:              backtestID,
		Status:          "running",
		Params:          domain.BacktestParams{
			StrategyName:   req.Strategy,
			StockPool:      req.StockPool,
			StartDate:      startDate,
			EndDate:        endDate,
			InitialCapital: initialCapital,
			RiskFreeRate:   riskFreeRate,
		},
		StartedAt:       time.Now(),
		Tracker: NewTracker(
			initialCapital,
			e.config.CommissionRate,
			e.config.SlippageRate,
			e.logger,
		),
		targetPositions: make(map[string]*domain.TargetPosition),
	}

	e.mu.Lock()
	e.currentBacktest = state
	e.mu.Unlock()

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
		TotalReturn:     result.TotalReturn,
		AnnualReturn:    result.AnnualReturn,
		SharpeRatio:    result.SharpeRatio,
		SortinoRatio:   result.SortinoRatio,
		MaxDrawdown:    result.MaxDrawdown,
		MaxDrawdownDate: result.MaxDrawdownDate.Format("2006-01-02"),
		WinRate:         result.WinRate,
		TotalTrades:     result.TotalTrades,
		WinTrades:       result.WinTrades,
		LoseTrades:      result.LoseTrades,
		AvgHoldingDays:  result.AvgHoldingDays,
		CalmarRatio:     result.CalmarRatio,
		StartedAt:       state.StartedAt.Format(time.RFC3339),
		CompletedAt:    state.CompletedAt.Format(time.RFC3339),
		PortfolioValues: result.PortfolioValues,
		Trades:          result.Trades,
		StockPool:       req.StockPool,
		InitialCapital:   initialCapital,
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

	// Warm the cache before running the backtest — pre-fetch all symbols into Redis
	// so the backtest loop hits Redis instead of PostgreSQL on every getOHLCV call.
	warmCtx, warmCancel := context.WithTimeout(ctx, 2*time.Minute)
	if err := e.warmCache(warmCtx, params.StockPool, params.StartDate, params.EndDate); err != nil {
		warmCancel()
		logger.Warn().Err(err).Msg("Cache warm-up failed — continuing without pre-cached data")
	} else {
		warmCancel()
		logger.Info().Msg("Cache warm-up completed")
	}

	// Get trading days
	tradingDays, err := e.getTradingDays(ctx, params.StartDate, params.EndDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get trading days: %w", err)
	}

	if len(tradingDays) == 0 {
		return nil, fmt.Errorf("no trading days found in range")
	}

	logger.Info().Int("trading_days", len(tradingDays)).Msg("Retrieved trading days")

	// Prepare market data cache
	marketDataCache := make(map[string][]domain.OHLCV)
	pricesCache := make(map[string]float64)         // Latest prices for each symbol
	stockCache := make(map[string]domain.Stock)      // Stock info cache (Name, ListDate)
	prevCloseCache := make(map[string]float64)      // Previous close per symbol (updated for limit-up)

	// Run backtest for each trading day
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

		// Step 1: Get market data for all stocks in pool.
		// Parallelism: when parallelWorkers > 1, each stock is fetched concurrently
		// via a pool of N goroutines (workers). Each worker processes a private copy
		// of the result maps; results are merged into the shared maps after all workers
		// complete. This avoids shared-map mutation races entirely.
		// When parallelWorkers <= 1, stocks are processed sequentially (original behavior).
		// Thread-safety: workers use only local variables; no shared mutable state.
		// Determinism: stocks are processed in a fixed order (sorted symbol list),
		// and within each stock the data fetch is deterministic (same input → same output).
		var stockWg sync.WaitGroup
		workers := e.parallelWorkers
		if workers <= 0 {
			workers = 1
		}

		// Partition stocks across workers using a channel.
		type stockJob struct {
			symbol    string
			prevClose float64 // Previous close for price limit detection (0 = use ohlcv fallback)
		}
		type stockResult struct {
			symbol    string
			stock     domain.Stock
			ohlcvData []domain.OHLCV
			price     float64
			prevClose float64 // Previous close to store for next day's computation
			limitUp   bool
			limitDown bool
			err       error
		}
		jobCh := make(chan stockJob, len(params.StockPool))
		resultCh := make(chan stockResult, len(params.StockPool))

		// Launch N workers. Workers process jobs from jobCh and send results to resultCh.
		// No shared mutable state in workers — each worker has only local variables.
		for w := 0; w < workers; w++ {
			stockWg.Add(1)
			go func() {
				defer stockWg.Done()
				for job := range jobCh {
					res := stockResult{symbol: job.symbol}

					// --- getStock ---
					if stock, err := e.getStock(ctx, job.symbol); err == nil {
						res.stock = stock
					}

					// --- getOHLCV ---
					ohlcvData, err := e.getOHLCV(ctx, job.symbol, params.StartDate, date)
					if err != nil {
						res.err = fmt.Errorf("symbol %s: %w", job.symbol, err)
						resultCh <- res
						continue
					}
					res.ohlcvData = ohlcvData

					// --- price limit detection (fully local; uses prevClose from job) ---
					limitUp, limitDown := false, false
					limitPrice := 0.0
					tradeDays := 0
					stockName := res.stock.Name
					if !res.stock.ListDate.IsZero() {
						tradeDays = int(date.Sub(res.stock.ListDate).Hours() / 24 / 7 * 5)
					}
					prevClose := job.prevClose
					if len(ohlcvData) >= 2 {
						if prevClose <= 0 {
							prevClose = ohlcvData[len(ohlcvData)-2].Close
						}
						if prevClose > 0 {
							limitRate := priceLimitNormal
							if tradeDays < newStockTradeDays {
								limitRate = priceLimitNew
							} else if hasSTPrefix(stockName) {
								limitRate = priceLimitST
							}
							todayBar := ohlcvData[len(ohlcvData)-1]
							upperLimit := prevClose * (1 + limitRate)
							lowerLimit := prevClose * (1 - limitRate)
							limitUp = todayBar.Close >= upperLimit
							limitDown = todayBar.Close <= lowerLimit
							if limitUp {
								limitPrice = upperLimit
							} else if limitDown {
								limitPrice = lowerLimit
							}
							todayBar.LimitUp = limitUp
							todayBar.LimitDown = limitDown
							ohlcvData[len(ohlcvData)-1] = todayBar
							res.ohlcvData = ohlcvData
							// The prevClose for next day is today's close (or limit price)
							if limitUp || limitDown {
								res.prevClose = todayBar.Close
							} else {
								res.prevClose = ohlcvData[len(ohlcvData)-1].Close
							}
						}
					}

					// --- prices ---
					if len(ohlcvData) > 0 {
						if limitPrice > 0 {
							res.price = limitPrice
						} else {
							res.price = ohlcvData[len(ohlcvData)-1].Close
						}
					}
					res.limitUp = limitUp
					res.limitDown = limitDown

					resultCh <- res
				}
			}()
		}
		// Feed jobs to workers (prevClose from prevCloseCache for each symbol).
		for _, s := range params.StockPool {
			jobCh <- stockJob{symbol: s, prevClose: prevCloseCache[s]}
		}
		close(jobCh)

		// Collect results — runs in the main goroutine sequentially.
		// prevCloseCache updates happen here only, avoiding any concurrent map access.
		for i := 0; i < len(params.StockPool); i++ {
			res := <-resultCh
			if res.err != nil {
				logger.Warn().Err(res.err).Msg("Failed to get stock data")
				continue
			}
			stockCache[res.symbol] = res.stock
			marketDataCache[res.symbol] = res.ohlcvData
			if res.price > 0 {
				pricesCache[res.symbol] = res.price
			}
			if res.prevClose > 0 {
				prevCloseCache[res.symbol] = res.prevClose
			}
		}
		stockWg.Wait()

		// Step 2: Detect market regime
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

		// Step 3: Get signals from strategy service
		signals, err := e.getSignals(ctx, params.StrategyName, params.StockPool, marketDataCache, date, state.Tracker)
		if err != nil {
			logger.Warn().
				Time("date", date).
				Err(err).
				Msg("Failed to get signals, skipping day")
			// Continue with existing positions only
		}

		// Step 4: Process signals and execute trades
		for _, signal := range signals {
			if signal.Date.Before(date) {
				// Signal generated before this day, skip
				continue
			}

			// Get today's OHLCV bar (used for limit checks and order execution)
			var todayBar *domain.OHLCV
			if ohlcvData, ok := marketDataCache[signal.Symbol]; ok && len(ohlcvData) > 0 {
				todayBar = &ohlcvData[len(ohlcvData)-1]
				// Enforce price limit: reject buys on limit-up days, sells on limit-down days
				if todayBar.LimitUp && (signal.Direction == domain.DirectionLong || signal.Direction == domain.DirectionShort) {
					logger.Info().
						Str("symbol", signal.Symbol).
						Str("direction", string(signal.Direction)).
						Time("date", date).
						Msg("Trade blocked: stock hit limit-up (涨停), cannot buy")
					continue
				}
				if todayBar.LimitDown && signal.Direction == domain.DirectionClose {
					logger.Info().
						Str("symbol", signal.Symbol).
						Time("date", date).
						Msg("Trade blocked: stock hit limit-down (跌停), cannot sell")
					continue
				}
			}

			// Build order execution options from signal
			execOpts := &OrderExecutionOpts{
				OrderType:  signal.OrderType,
				LimitPrice:  signal.LimitPrice,
				DayBar:      todayBar,
			}

			// Calculate position size
			portfolio := state.Tracker.GetPortfolio(pricesCache)
			currentPrice := pricesCache[signal.Symbol]
			positionSize, err := e.calculatePosition(ctx, signal, portfolio, regime, currentPrice)
			if err != nil {
				logger.Warn().
					Str("symbol", signal.Symbol).
					Err(err).
					Msg("Failed to calculate position size")
				continue
			}

			// Target/Actual Position Separation:
			// Compute effective target by netting pending qty from prior unfilled signals
			targetQty := positionSize.Size
			if targetQty <= 0 {
				continue
			}

			// Get or create target position record
			tp, exists := state.targetPositions[signal.Symbol]
			if !exists {
				tp = &domain.TargetPosition{
					Symbol:      signal.Symbol,
					TargetQty:   0,
					ActualQty:   0,
					PendingQty:  0,
					LastUpdated: date,
				}
				state.targetPositions[signal.Symbol] = tp
			}

			// Compute effective target: reduce by what we already own (ActualQty).
			// PendingQty represents unfilled shares from a PRIOR signal — it should NOT
			// reduce a NEW signal's target, as each day's signal is an independent decision.
			// e.g., Day1 target=1000, fill=300, pending=700. Day2 target=2000.
			// Correct: buy 2000-300=1700 (not 2000-700=1300, which would leave us 700 short
			// of the new target and compound the shortfall across days).
			effectiveTarget := targetQty
			if tp.PendingQty > 0 && (signal.Direction == domain.DirectionLong || signal.Direction == domain.DirectionShort) {
				// Reduce new target by what we already own (ActualQty), floor at 0
				if tp.ActualQty >= targetQty {
					effectiveTarget = 0
				} else {
					effectiveTarget = targetQty - tp.ActualQty
				}
				if effectiveTarget <= 0 {
					logger.Info().
						Str("symbol", signal.Symbol).
						Float64("actual_qty", tp.ActualQty).
						Float64("pending_qty", tp.PendingQty).
						Float64("new_target", targetQty).
						Time("date", date).
						Msg("Signal skipped: already at or above target")
					continue
				}
				if effectiveTarget < targetQty {
					logger.Info().
						Str("symbol", signal.Symbol).
						Float64("actual_qty", tp.ActualQty).
						Float64("pending_qty", tp.PendingQty).
						Float64("new_target", targetQty).
						Float64("effective_target", effectiveTarget).
						Time("date", date).
						Msg("Adjusted target: netting actual owned qty")
				}
			}

			// Update target position with new target
			tp.TargetQty = targetQty
			tp.LastUpdated = date

			// Execute trade if effective target > 0
			if effectiveTarget > 0 {
				price := pricesCache[signal.Symbol]
				if price <= 0 {
					continue
				}

				var trade *domain.Trade
				switch signal.Direction {
				case domain.DirectionLong:
					trade, err = state.Tracker.ExecuteTrade(
						signal.Symbol,
						domain.DirectionLong,
						effectiveTarget,
						price,
						date,
						execOpts,
					)
					if err != nil {
						logger.Warn().
							Str("symbol", signal.Symbol).
							Err(err).
							Msg("Failed to execute long trade")
						// Record unfilled qty as pending
						tp.PendingQty = targetQty - tp.ActualQty
						tp.LastUpdated = date
						continue
					}

				case domain.DirectionShort:
					trade, err = state.Tracker.ExecuteTrade(
						signal.Symbol,
						domain.DirectionShort,
						effectiveTarget,
						price,
						date,
						execOpts,
					)
					if err != nil {
						logger.Warn().
							Str("symbol", signal.Symbol).
							Err(err).
							Msg("Failed to execute short trade")
						tp.PendingQty = targetQty - tp.ActualQty
						tp.LastUpdated = date
						continue
					}

				case domain.DirectionClose:
					trade, err = state.Tracker.ClosePosition(signal.Symbol, price, date)
					if err != nil {
						logger.Warn().
							Str("symbol", signal.Symbol).
							Err(err).
							Msg("Failed to close position")
						// Don't modify tp on failure — leave it for retry or manual intervention
						continue
					}
					// Close succeeded: position is deleted from t.positions.
					// Clear tp fields and delete from state.targetPositions.
					tp.PendingQty = 0
					tp.TargetQty = 0
					tp.ActualQty = 0
					tp.LastUpdated = date
					trade.PendingQty = 0
					delete(state.targetPositions, signal.Symbol)
					continue

				case domain.DirectionHold:
					// No action needed
					continue
				}

				// Update target vs actual gap after trade execution
				if trade != nil {
					// Track how much of the target was actually filled
					if signal.Direction == domain.DirectionLong || signal.Direction == domain.DirectionShort {
						// For opening positions: actual is the qty that was filled
						tp.ActualQty += trade.Quantity
						// Pending = target - actual (what wasn't filled)
						tp.PendingQty = tp.TargetQty - tp.ActualQty
						// Record pending qty in the trade for visibility
						trade.PendingQty = tp.PendingQty
						if tp.PendingQty > 0 {
							logger.Info().
								Str("symbol", signal.Symbol).
								Float64("target_qty", tp.TargetQty).
								Float64("actual_qty", tp.ActualQty).
								Float64("pending_qty", tp.PendingQty).
								Time("date", date).
								Msg("Partial fill: target vs actual gap recorded")
						}
					} else if signal.Direction == domain.DirectionClose {
						// Closing resolves any pending buy qty — we're no longer trying to buy
						tp.PendingQty = 0
						tp.TargetQty = 0
						tp.ActualQty = 0
						trade.PendingQty = 0
					}
					tp.LastUpdated = date

					// If pending is resolved, clean up the target position
					if tp.PendingQty <= 0 && tp.TargetQty <= 0 {
						delete(state.targetPositions, signal.Symbol)
					}
				}
			}
		}

		// Step 5: Check stop losses
		stopLossEvents, err := e.checkStopLosses(ctx, state.Tracker, pricesCache)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to check stop losses")
		}

		// Execute stop loss trades
		for _, event := range stopLossEvents {
			if event.Type == "stop_loss" || event.Type == "take_profit" {
				_, err := state.Tracker.ExecuteTrade(
					event.Symbol,
					domain.DirectionClose,
					event.Quantity,
					event.Price,
					date,
					nil,
				)
				if err != nil {
					logger.Warn().
						Str("symbol", event.Symbol).
						Str("type", event.Type).
						Err(err).
						Msg("Failed to execute stop loss")
				}
			}
		}

		// Step 6: Record daily portfolio value
		state.Tracker.RecordDailyValue(date, pricesCache)

		// Step 7: Advance day for T+1 settlement (shift QuantityToday → QuantityYesterday)
		state.Tracker.AdvanceDay(date)

		// Step 8: Update prevCloseCache for price limit calculation on next trading day
		// If a stock hit limit-up today, the "previous close" for tomorrow is the limit price
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

	// Generate final results
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
	url := fmt.Sprintf("%s/api/v1/trading/calendar?start=%s&end=%s",
		e.dataServiceURL, start.Format("2006-01-02"), end.Format("2006-01-02"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		// Calendar not synced — no data at all
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("data service returned status %d", resp.StatusCode)
	}

	var result struct {
		TradingDays []string `json:"trading_days"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	return len(result.TradingDays) > 0, nil
}

// warmCache pre-fetches all OHLCV data for the stock universe into the engine's
// in-memory cache (e.inMemoryOHLCV) in a single bulk call. After this completes,
// all getOHLCV calls during the backtest run are served from memory — zero HTTP overhead.
//
// This replaces the previous approach of warming Redis only, which the engine did not
// directly read from.
func (e *Engine) warmCache(ctx context.Context, symbols []string, start, end time.Time) error {
	if len(symbols) == 0 {
		return nil
	}

	url := fmt.Sprintf("%s/api/v1/ohlcv/bulk", e.dataServiceURL)
	startStr := start.Format("20060102")
	endStr := end.Format("20060102")

	body := map[string]any{
		"symbols":    symbols,
		"start_date": startStr,
		"end_date":   endStr,
	}
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal bulk request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create bulk request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bulk OHLCV request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bulk endpoint returned status %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			Symbol string          `json:"symbol"`
			OHLCV  []domain.OHLCV `json:"ohlcv"`
			Error  string          `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode bulk response: %w", err)
	}

	// Populate in-memory cache from bulk result.
	// Subsequent getOHLCV calls will hit this map directly (zero HTTP).
	for _, r := range result.Results {
		if r.Error != "" {
			return fmt.Errorf("bulk fetch failed for %s: %s", r.Symbol, r.Error)
		}
		// Sort by date ascending for consistent iteration.
		sort.Slice(r.OHLCV, func(i, j int) bool {
			return r.OHLCV[i].Date.Before(r.OHLCV[j].Date)
		})
		e.inMemoryOHLCV[r.Symbol] = r.OHLCV
	}

	return nil
}

// getTradingDays retrieves trading days from data service.
func (e *Engine) getTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	url := fmt.Sprintf("%s/api/v1/trading/calendar?start=%s&end=%s",
		e.dataServiceURL, start.Format("2006-01-02"), end.Format("2006-01-02"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data service returned status %d", resp.StatusCode)
	}

	var result struct {
		TradingDays []string `json:"trading_days"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	days := make([]time.Time, 0, len(result.TradingDays))
	for _, d := range result.TradingDays {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		days = append(days, t)
	}

	// Sort deterministically: ascending by date.
	// This ensures the backtest loop processes days in a fixed order,
	// regardless of the order returned by the data service.
	sort.Slice(days, func(i, j int) bool {
		return days[i].Before(days[j])
	})

	return days, nil
}

// getOHLCV retrieves OHLCV data for a symbol.
// It first checks the in-memory cache (populated by LoadOHLCVInMemory),
// falling back to HTTP only if the cache is empty.
func (e *Engine) getOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	// Fast path: serve from in-memory cache (zero HTTP overhead).
	e.mu.RLock()
	cached, ok := e.inMemoryOHLCV[symbol]
	e.mu.RUnlock()
	if ok {
		// Slice the cached data to the requested date range.
		// The cached slice is sorted ascending by date.
		var filtered []domain.OHLCV
		for _, bar := range cached {
			if !bar.Date.Before(start) && !bar.Date.After(end) {
				filtered = append(filtered, bar)
			}
		}
		return filtered, nil
	}

	// Slow path: fetch via HTTP.
	url := fmt.Sprintf("%s/ohlcv/%s?start_date=%s&end_date=%s",
		e.dataServiceURL, symbol, start.Format("20060102"), end.Format("20060102"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data service returned status %d", resp.StatusCode)
	}

	var result struct {
		OHLCV []domain.OHLCV `json:"ohlcv"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Sort OHLCV data deterministically: by symbol, then by date.
	// This ensures consistent ordering regardless of data service response order.
	sort.Slice(result.OHLCV, func(i, j int) bool {
		if result.OHLCV[i].Symbol != result.OHLCV[j].Symbol {
			return result.OHLCV[i].Symbol < result.OHLCV[j].Symbol
		}
		return result.OHLCV[i].Date.Before(result.OHLCV[j].Date)
	})

	return result.OHLCV, nil
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

	url := fmt.Sprintf("%s/api/v1/risk/regime", e.riskServiceURL)

	reqBody := struct {
		Data []domain.OHLCV `json:"data"`
	}{Data: allData}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("risk service returned status %d", resp.StatusCode)
	}

	var regime domain.MarketRegime
	if err := json.NewDecoder(resp.Body).Decode(&regime); err != nil {
		return nil, err
	}

	return &regime, nil
}

// getSignals retrieves trading signals from strategy service.
func (e *Engine) getSignals(ctx context.Context, strategyName string, stockPool []string, marketData map[string][]domain.OHLCV, date time.Time, tracker *Tracker) ([]domain.Signal, error) {
	// Step 1: Try local strategy registry first (plugins/ directory)
	if strat, err := strategy.DefaultRegistry.Get(strategyName); err == nil {
		// Build current portfolio from tracker state
		prices := make(map[string]float64)
		for sym, bars := range marketData {
			if len(bars) > 0 {
				prices[sym] = bars[len(bars)-1].Close
			}
		}
		portfolio := tracker.GetPortfolio(prices)

		signals, err := strat.GenerateSignals(ctx, marketData, portfolio)
		if err != nil {
			return nil, fmt.Errorf("local strategy %s failed: %w", strategyName, err)
		}

		// Convert strategy.Signal to domain.Signal
		domainSignals := make([]domain.Signal, 0, len(signals))
		for _, s := range signals {
			if s.Action == "hold" {
				continue
			}
			var dir domain.Direction
			if s.Action == "buy" {
				dir = domain.DirectionLong
			} else if s.Action == "sell" {
				dir = domain.DirectionShort
			} else {
				continue
			}
			domainSignals = append(domainSignals, domain.Signal{
				Symbol:    s.Symbol,
				Direction: dir,
				Strength:  s.Strength,
				Date:      date,
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
		StockPool   []string                       `json:"stock_pool"`
		Stocks      []domain.Stock                 `json:"stocks"`
		MarketData  map[string][]domain.OHLCV      `json:"market_data"`
		Fundamental map[string][]domain.Fundamental `json:"fundamental"`
		Date        string                         `json:"date"`
	}{
		StockPool:   stockPool,
		Stocks:      stocks,
		MarketData:  marketData,
		Fundamental: map[string][]domain.Fundamental{},
		Date:        date.Format("2006-01-02"),
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("strategy service returned status %d", resp.StatusCode)
	}

	var result struct {
		Signals []domain.Signal `json:"signals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Signals, nil
}

// calculatePosition calculates position size using risk service.
func (e *Engine) calculatePosition(ctx context.Context, signal domain.Signal, portfolio *domain.Portfolio, regime *domain.MarketRegime, currentPrice float64) (domain.PositionSize, error) {
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

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return domain.PositionSize{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return domain.PositionSize{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return domain.PositionSize{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.PositionSize{}, fmt.Errorf("risk service returned status %d", resp.StatusCode)
	}

	var positionSize domain.PositionSize
	if err := json.NewDecoder(resp.Body).Decode(&positionSize); err != nil {
		return domain.PositionSize{}, err
	}

	return positionSize, nil
}

// checkStopLosses checks and triggers stop losses using risk service.
func (e *Engine) checkStopLosses(ctx context.Context, tracker *Tracker, prices map[string]float64) ([]domain.StopLossEvent, error) {
	positions := tracker.GetAllPositions()
	if len(positions) == 0 {
		return nil, nil
	}

	url := fmt.Sprintf("%s/api/v1/risk/stoploss", e.riskServiceURL)

	// Convert positions map to slice
	var positionsList []domain.Position
	for _, pos := range positions {
		positionsList = append(positionsList, pos)
	}

	reqBody := struct {
		Positions map[string]float64    `json:"prices"`
	}{
		Positions: prices,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("risk service returned status %d", resp.StatusCode)
	}

	var events struct {
		Events []domain.StopLossEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
	}

	return events.Events, nil
}

// GetBacktestResult retrieves the result of a completed backtest.
func (e *Engine) GetBacktestResult(backtestID string) (*domain.BacktestResult, error) {
	e.mu.RLock()
	state := e.currentBacktest
	e.mu.RUnlock()

	if state == nil || state.ID != backtestID {
		return nil, fmt.Errorf("backtest not found: %s", backtestID)
	}

	if state.Status != "completed" {
		return nil, fmt.Errorf("backtest not completed: %s", state.Status)
	}

	return state.Result, nil
}

// GetBacktestTrades retrieves trades for a backtest.
func (e *Engine) GetBacktestTrades(backtestID string) ([]domain.Trade, error) {
	e.mu.RLock()
	state := e.currentBacktest
	e.mu.RUnlock()

	if state == nil || state.ID != backtestID {
		return nil, fmt.Errorf("backtest not found: %s", backtestID)
	}

	return state.Tracker.GetTrades(), nil
}

// GetBacktestEquity retrieves equity curve for a backtest.
func (e *Engine) GetBacktestEquity(backtestID string) ([]domain.PortfolioValue, error) {
	e.mu.RLock()
	state := e.currentBacktest
	e.mu.RUnlock()

	if state == nil || state.ID != backtestID {
		return nil, fmt.Errorf("backtest not found: %s", backtestID)
	}

	return state.Tracker.GetEquityCurve(), nil
}

// GetBacktestStatus returns the status of a backtest.
func (e *Engine) GetBacktestStatus(backtestID string) (string, error) {
	e.mu.RLock()
	state := e.currentBacktest
	e.mu.RUnlock()

	if state == nil || state.ID != backtestID {
		return "", fmt.Errorf("backtest not found: %s", backtestID)
	}

	return state.Status, nil
}

// LoadOHLCVInMemory loads all OHLCV data into memory, bypassing HTTP for backtest speed.
// The map key is symbol; each slice is sorted by date ascending.
// After calling this, getOHLCV returns cached data instantly.
// Pass nil to clear the in-memory cache (forces HTTP mode again).
func (e *Engine) LoadOHLCVInMemory(data map[string][]domain.OHLCV) {
	e.mu.Lock()
	e.inMemoryOHLCV = data
	e.mu.Unlock()
}

// LoadFactorCache loads pre-computed factor z-scores into memory for the given factor and date range.
// The factor cache is stored as: factorName -> tradeDate -> symbol -> zScore
// This is called before a backtest run to enable fast factor-based stock ranking.
// Pass nil to clear the cache.
func (e *Engine) LoadFactorCache(factor domain.FactorType, startDate, endDate time.Time, entries []*domain.FactorCacheEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.factorCache == nil {
		e.factorCache = make(map[domain.FactorType]map[time.Time]map[string]float64)
	}

	// Initialize nested maps
	if e.factorCache[factor] == nil {
		e.factorCache[factor] = make(map[time.Time]map[string]float64)
	}

	// Group entries by trade date
	for _, entry := range entries {
		if entry.FactorName != factor {
			continue
		}
		dateKey := entry.TradeDate.Truncate(24 * time.Hour)
		if e.factorCache[factor][dateKey] == nil {
			e.factorCache[factor][dateKey] = make(map[string]float64)
		}
		e.factorCache[factor][dateKey][entry.Symbol] = entry.ZScore
	}

	e.logger.Info().
		Str("factor", string(factor)).
		Time("start_date", startDate).
		Time("end_date", endDate).
		Int("entries_loaded", len(entries)).
		Msg("Factor cache loaded into memory")
}

// GetFactorZScore returns the z-score for a given factor, date, and symbol from the in-memory cache.
// Returns 0 and false if the entry is not found.
func (e *Engine) GetFactorZScore(factor domain.FactorType, date time.Time, symbol string) (float64, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.factorCache == nil {
		return 0, false
	}
	dateKey := date.Truncate(24 * time.Hour)
	dateMap, ok := e.factorCache[factor]
	if !ok {
		return 0, false
	}
	symbolMap, ok := dateMap[dateKey]
	if !ok {
		return 0, false
	}
	zScore, ok := symbolMap[symbol]
	return zScore, ok
}

// SetParallelWorkers sets the number of concurrent workers for per-stock data fetching.
// Must be called before RunBacktest. Values <= 0 mean sequential (no parallelism).
func (e *Engine) SetParallelWorkers(n int) {
	e.parallelWorkers = n
}

// getStock retrieves stock info (Name, ListDate) from the data service.
func (e *Engine) getStock(ctx context.Context, symbol string) (domain.Stock, error) {
	url := fmt.Sprintf("%s/api/v1/stocks/%s", e.dataServiceURL, symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Stock{}, err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return domain.Stock{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return domain.Stock{Symbol: symbol}, nil // Return minimal stock if not found
	}
	if resp.StatusCode != http.StatusOK {
		return domain.Stock{}, fmt.Errorf("data service returned status %d", resp.StatusCode)
	}

	var result struct {
		Stock domain.Stock `json:"stock"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return domain.Stock{}, err
	}

	return result.Stock, nil
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
