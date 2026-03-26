package backtest

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// WalkForwardEngine runs walk-forward validation for a strategy.
type WalkForwardEngine struct {
	engine *Engine
	store  *storage.PostgresStore
}

// NewWalkForwardEngine creates a new WalkForwardEngine.
func NewWalkForwardEngine(engine *Engine, store *storage.PostgresStore) *WalkForwardEngine {
	return &WalkForwardEngine{
		engine: engine,
		store:  store,
	}
}

// RunWalkForward runs walk-forward validation for a strategy.
// 1. Get all trading days in the full date range
// 2. Iterate: train on [train_start, train_end], test on [test_start, test_end]
// 3. For each window: run backtest on train period, then on test period
// 4. Compute OOS metrics and degradation
// 5. Return full report
func (wf *WalkForwardEngine) RunWalkForward(ctx context.Context, strategyID, universe string, params domain.WalkForwardParams, fullStart, fullEnd string) (*domain.WalkForwardReport, error) {
	// Validate params
	if params.TrainDays <= 0 {
		return nil, fmt.Errorf("train_days must be positive")
	}
	if params.TestDays <= 0 {
		return nil, fmt.Errorf("test_days must be positive")
	}
	if params.StepDays <= 0 {
		return nil, fmt.Errorf("step_days must be positive")
	}
	if params.MinTrainDays <= 0 {
		params.MinTrainDays = params.TrainDays / 2
	}

	// Parse dates
	startDate, err := time.Parse("2006-01-02", fullStart)
	if err != nil {
		return nil, fmt.Errorf("invalid fullStart format: %w", err)
	}
	endDate, err := time.Parse("2006-01-02", fullEnd)
	if err != nil {
		return nil, fmt.Errorf("invalid fullEnd format: %w", err)
	}

	// Get trading days from the store
	tradingDays, err := wf.store.GetTradingDates(ctx, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get trading days: %w", err)
	}

	if len(tradingDays) < params.TrainDays+params.TestDays {
		return nil, fmt.Errorf("insufficient trading days: need at least %d, got %d", params.TrainDays+params.TestDays, len(tradingDays))
	}

	// Build walk-forward windows
	windows := wf.buildWindows(tradingDays, params)
	if len(windows) == 0 {
		return nil, fmt.Errorf("no walk-forward windows could be generated")
	}

	report := &domain.WalkForwardReport{
		StrategyID: strategyID,
		Universe:   universe,
		Windows:    make([]*domain.WalkForwardResult, 0, len(windows)),
	}

	for i, win := range windows {
		wf.engine.logger.Info().
			Int("window", i+1).
			Str("train", win.trainStart.Format("2006-01-02")+" to "+win.trainEnd.Format("2006-01-02")).
			Str("test", win.testStart.Format("2006-01-02")+" to "+win.testEnd.Format("2006-01-02")).
			Msg("Running walk-forward window")

		// Run backtest on training period
		trainResult, err := wf.engine.RunBacktest(ctx, BacktestRequest{
			Strategy:  strategyID,
			StockPool: []string{universe}, // universe is used as stock pool for now
			StartDate: win.trainStart.Format("2006-01-02"),
			EndDate:   win.trainEnd.Format("2006-01-02"),
		})
		if err != nil {
			wf.engine.logger.Warn().
				Err(err).
				Int("window", i+1).
				Msg("Train backtest failed, skipping window")
			continue
		}

		// Run backtest on test period
		testResult, err := wf.engine.RunBacktest(ctx, BacktestRequest{
			Strategy:  strategyID,
			StockPool: []string{universe},
			StartDate: win.testStart.Format("2006-01-02"),
			EndDate:   win.testEnd.Format("2006-01-02"),
		})
		if err != nil {
			wf.engine.logger.Warn().
				Err(err).
				Int("window", i+1).
				Msg("Test backtest failed, skipping window")
			continue
		}

		// Convert BacktestResponse to BacktestResult
		trainBR := wf.toBacktestResult(trainResult)
		testBR := wf.toBacktestResult(testResult)

		// Compute degradation
		trainSharpe := trainBR.SharpeRatio
		testSharpe := testBR.SharpeRatio
		var oosVsTrain float64
		if trainSharpe > 0 {
			oosVsTrain = testSharpe / trainSharpe
		}

		wfResult := &domain.WalkForwardResult{
			WindowIndex:     i + 1,
			TrainStart:      win.trainStart.Format("2006-01-02"),
			TrainEnd:        win.trainEnd.Format("2006-01-02"),
			TestStart:       win.testStart.Format("2006-01-02"),
			TestEnd:         win.testEnd.Format("2006-01-02"),
			TrainResult:     trainBR,
			TestResult:      testBR,
			TrainSharpe:     trainSharpe,
			TestSharpe:      testSharpe,
			TestReturn:      testBR.AnnualReturn,
			TestMaxDrawdown: testBR.MaxDrawdown,
			OOSvsTrain:      oosVsTrain,
		}

		report.Windows = append(report.Windows, wfResult)
	}

	// Compute aggregate metrics
	if len(report.Windows) == 0 {
		return nil, fmt.Errorf("all walk-forward windows failed")
	}

	var totalTestSharpe, totalTestReturn, totalTestMaxDD, totalDegradation float64
	positiveSharpeCount := 0

	for _, w := range report.Windows {
		totalTestSharpe += w.TestSharpe
		totalTestReturn += w.TestReturn
		totalTestMaxDD += w.TestMaxDrawdown
		totalDegradation += w.OOSvsTrain
		if w.TestSharpe > 0 {
			positiveSharpeCount++
		}
	}

	n := float64(len(report.Windows))
	report.AvgTestSharpe = totalTestSharpe / n
	report.AvgTestReturn = totalTestReturn / n
	report.AvgTestMaxDD = totalTestMaxDD / n
	report.AvgDegradation = totalDegradation / n
	report.PassRate = float64(positiveSharpeCount) / n

	// Overall pass: avg_test_sharpe > 0.5 AND avg_degradation < 0.7
	report.OverallPass = report.AvgTestSharpe > 0.5 && report.AvgDegradation < 0.7

	// Save to database
	if err := wf.store.SaveWalkForwardReport(ctx, report); err != nil {
		wf.engine.logger.Warn().Err(err).Msg("Failed to save walk-forward report to DB")
	}

	return report, nil
}

// wfWindow represents a single walk-forward window.
type wfWindow struct {
	trainStart, trainEnd time.Time
	testStart, testEnd    time.Time
}

// buildWindows generates walk-forward windows from trading days.
// It creates rolling windows with:
//   - Train period: TrainDays trading days
//   - Test period: TestDays trading days
//   - Step: StepDays trading days forward
func (wf *WalkForwardEngine) buildWindows(days []time.Time, params domain.WalkForwardParams) []wfWindow {
	var windows []wfWindow

	// Window starts at index 0, train on [0, TrainDays-1], test on [TrainDays, TrainDays+TestDays-1]
	// Then step forward by StepDays
	// Continue while we have enough days for both train and test

	for startIdx := 0; startIdx+params.TrainDays+params.TestDays <= len(days); startIdx += params.StepDays {
		trainEndIdx := startIdx + params.TrainDays - 1
		testEndIdx := trainEndIdx + params.TestDays

		// Ensure test period doesn't exceed available days
		if testEndIdx >= len(days) {
			// Trim test end to available days, but ensure test period still has enough data
			testEndIdx = len(days) - 1
			if testEndIdx-trainEndIdx < params.TestDays/2 {
				// Not enough test data, skip this window
				continue
			}
		}

		window := wfWindow{
			trainStart: days[startIdx],
			trainEnd:   days[trainEndIdx],
			testStart:  days[trainEndIdx+1],
			testEnd:    days[testEndIdx],
		}

		// Validate minimum training days
		trainDays := trainEndIdx - startIdx + 1
		if trainDays < params.MinTrainDays {
			continue
		}

		windows = append(windows, window)
	}

	return windows
}

// toBacktestResult converts a BacktestResponse to a BacktestResult.
func (wf *WalkForwardEngine) toBacktestResult(r *BacktestResponse) *domain.BacktestResult {
	if r == nil {
		return nil
	}
	var maxDDDate time.Time
	if r.MaxDrawdownDate != "" {
		maxDDDate, _ = time.Parse("2006-01-02", r.MaxDrawdownDate)
	}
	var completedAt time.Time
	if r.CompletedAt != "" {
		completedAt, _ = time.Parse(time.RFC3339, r.CompletedAt)
	}
	var startedAt time.Time
	if r.StartedAt != "" {
		startedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}

	return &domain.BacktestResult{
		StartDate:        startedAt,
		EndDate:         completedAt,
		TotalReturn:     r.TotalReturn,
		AnnualReturn:    r.AnnualReturn,
		SharpeRatio:     r.SharpeRatio,
		SortinoRatio:    r.SortinoRatio,
		MaxDrawdown:     r.MaxDrawdown,
		MaxDrawdownDate: maxDDDate,
		WinRate:         r.WinRate,
		TotalTrades:     r.TotalTrades,
		WinTrades:       r.WinTrades,
		LoseTrades:      r.LoseTrades,
		AvgHoldingDays:  r.AvgHoldingDays,
		CalmarRatio:     r.CalmarRatio,
		PortfolioValues: r.PortfolioValues,
		Trades:          r.Trades,
	}
}
