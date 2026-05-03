package backtest

import (
	"context"
	"fmt"
	"math"
	"sync"
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

// GetLatestReport returns the latest walk-forward report for a strategy.
func (wf *WalkForwardEngine) GetLatestReport(ctx context.Context, strategyID string) (*domain.WalkForwardReport, error) {
	return wf.store.GetLatestWalkForwardReport(ctx, strategyID)
}

// ListReports returns all walk-forward reports, limited to n most recent.
func (wf *WalkForwardEngine) ListReports(ctx context.Context, limit int) ([]*domain.WalkForwardReport, error) {
	return wf.store.ListAllWalkForwardReports(ctx, limit)
}

// WalkForwardRequest holds parameters for a walk-forward validation run.
type WalkForwardRequest struct {
	Strategy       string   `json:"strategy" binding:"required"`
	StockPool      []string `json:"stock_pool" binding:"required"`
	StartDate      string   `json:"start_date" binding:"required"`
	EndDate        string   `json:"end_date" binding:"required"`
	InitialCapital float64  `json:"initial_capital"`
	RiskFreeRate   float64  `json:"risk_free_rate"`

	WalkForwardParams domain.WalkForwardParams `json:"walk_forward_params"`
}

// RunWalkForward runs walk-forward validation for a strategy.
//
// Algorithm:
//  1. Get all trading days in [fullStart, fullEnd]
//  2. Build rolling windows: train=[train_start, train_end], test=[test_start, test_end]
//  3. For each window: run backtest on train period → optimize / calibrate,
//     then run on test period → measure OOS performance
//  4. Compute degradation = OOS_Sharpe / IS_Sharpe for each window
//  5. Aggregate: avg OOS Sharpe, avg degradation, pass rate, overfit score
func (wf *WalkForwardEngine) RunWalkForward(ctx context.Context, req WalkForwardRequest) (*domain.WalkForwardReport, error) {
	params := req.WalkForwardParams

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

	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start_date format: %w", err)
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid end_date format: %w", err)
	}

	tradingDays, err := wf.store.GetTradingDates(ctx, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get trading days: %w", err)
	}

	if len(tradingDays) < params.TrainDays+params.TestDays {
		return nil, fmt.Errorf("insufficient trading days: need at least %d, got %d",
			params.TrainDays+params.TestDays, len(tradingDays))
	}

	windows := wf.buildWindows(tradingDays, params)
	if len(windows) == 0 {
		return nil, fmt.Errorf("no walk-forward windows could be generated")
	}

	wf.engine.logger.Info().
		Int("windows", len(windows)).
		Int("symbols", len(req.StockPool)).
		Str("range", req.StartDate+" ~ "+req.EndDate).
		Msg("Starting walk-forward validation")

	report := &domain.WalkForwardReport{
		StrategyID: req.Strategy,
		Universe:   fmt.Sprintf("%d symbols", len(req.StockPool)),
		Windows:    make([]*domain.WalkForwardResult, 0, len(windows)),
	}

	results := wf.runWindowsParallel(ctx, req, windows)

	for _, r := range results {
		if r != nil {
			report.Windows = append(report.Windows, r)
		}
	}

	if len(report.Windows) == 0 {
		return nil, fmt.Errorf("all walk-forward windows failed")
	}

	wf.computeAggregateMetrics(report)
	wf.detectOverfitting(report)

	if err := wf.store.SaveWalkForwardReport(ctx, report); err != nil {
		wf.engine.logger.Warn().Err(err).Msg("Failed to save walk-forward report to DB")
	}

	return report, nil
}

// runWindowsParallel executes all walk-forward windows concurrently.
// Each window runs its train + test backtests sequentially (they share the same engine),
// but different windows run in parallel goroutines.
func (wf *WalkForwardEngine) runWindowsParallel(
	ctx context.Context,
	req WalkForwardRequest,
	windows []wfWindow,
) []*domain.WalkForwardResult {
	var mu sync.Mutex
	results := make([]*domain.WalkForwardResult, len(windows))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for i, win := range windows {
		wg.Add(1)
		go func(idx int, w wfWindow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := wf.runSingleWindow(ctx, req, idx, w)
			mu.Lock()
			results[idx] = r
			mu.Unlock()
		}(i, win)
	}
	wg.Wait()

	valid := make([]*domain.WalkForwardResult, 0, len(results))
	for _, r := range results {
		if r != nil {
			valid = append(valid, r)
		}
	}
	return valid
}

// runSingleWindow executes train + test backtests for one walk-forward window.
func (wf *WalkForwardEngine) runSingleWindow(
	ctx context.Context,
	req WalkForwardRequest,
	windowIdx int,
	win wfWindow,
) *domain.WalkForwardResult {
	wf.engine.logger.Info().
		Int("window", windowIdx+1).
		Int("total", windowIdx).
		Str("train", win.trainStart.Format("2006-01-02")+"~"+win.trainEnd.Format("2006-01-02")).
		Str("test", win.testStart.Format("2006-01-02")+"~"+win.testEnd.Format("2006-01-02")).
		Msg("Running walk-forward window")

	capital := req.InitialCapital
	if capital <= 0 {
		capital = 1000000.0
	}
	riskFree := req.RiskFreeRate

	trainReq := BacktestRequest{
		Strategy:       req.Strategy,
		StockPool:      req.StockPool,
		StartDate:      win.trainStart.Format("2006-01-02"),
		EndDate:        win.trainEnd.Format("2006-01-02"),
		InitialCapital: capital,
		RiskFreeRate:   riskFree,
	}

	testReq := BacktestRequest{
		Strategy:       req.Strategy,
		StockPool:      req.StockPool,
		StartDate:      win.testStart.Format("2006-01-02"),
		EndDate:        win.testEnd.Format("2006-01-02"),
		InitialCapital: capital,
		RiskFreeRate:   riskFree,
	}

	trainResp, err := wf.engine.RunBacktest(ctx, trainReq)
	if err != nil {
		wf.engine.logger.Warn().Err(err).Int("window", windowIdx+1).Msg("Train backtest failed")
		return nil
	}

	testResp, err := wf.engine.RunBacktest(ctx, testReq)
	if err != nil {
		wf.engine.logger.Warn().Err(err).Int("window", windowIdx+1).Msg("Test backtest failed")
		return nil
	}

	trainBR := wf.toBacktestResult(trainResp)
	testBR := wf.toBacktestResult(testResp)

	trainSharpe := trainBR.SharpeRatio
	testSharpe := testBR.SharpeRatio
	oosVsTrain := math.NaN()
	if trainSharpe > 1e-9 {
		oosVsTrain = testSharpe / trainSharpe
	}

	return &domain.WalkForwardResult{
		WindowIndex:     windowIdx + 1,
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
}

// computeAggregateMetrics calculates summary statistics from all windows.
func (wf *WalkForwardEngine) computeAggregateMetrics(report *domain.WalkForwardReport) {
	n := len(report.Windows)
	if n == 0 {
		return
	}

	var sumTestSharpe, sumTestReturn, sumTestMaxDD, sumDegradation float64
	var sharpes []float64
	degradations := make([]float64, 0, n)
	positiveCount := 0

	for _, w := range report.Windows {
		sumTestSharpe += w.TestSharpe
		sumTestReturn += w.TestReturn
		sumTestMaxDD += w.TestMaxDrawdown
		sumDegradation += w.OOSvsTrain
		sharpes = append(sharpes, w.TestSharpe)
		if !math.IsNaN(w.OOSvsTrain) {
			degradations = append(degradations, w.OOSvsTrain)
		}
		if w.TestSharpe > 0 {
			positiveCount++
		}
	}

	fn := float64(n)
	report.AvgTestSharpe = sumTestSharpe / fn
	report.AvgTestReturn = sumTestReturn / fn
	report.AvgTestMaxDD = sumTestMaxDD / fn
	report.AvgDegradation = sumDegradation / fn
	report.PassRate = float64(positiveCount) / fn

	report.StdTestSharpe = stdDev(sharpes)
	if len(degradations) > 0 {
		report.StdDegradation = stdDev(degradations)
	}

	report.OverallPass = report.AvgTestSharpe > 0.5 && report.AvgDegradation < 0.7
}

// detectOverfitting computes overfitting probability and robustness score.
//
// Metrics:
//   - OverfitScore: 0=robust, 1=severely overfit. Based on avg degradation and variance.
//   - ProbNoOverfit: P(strategy is NOT overfit) using Monte Carlo-style heuristic.
//   - StabilityScore: consistency of OOS Sharpe across windows (lower CV = higher stability).
func (wf *WalkForwardEngine) detectOverfitting(report *domain.WalkForwardReport) {
	n := len(report.Windows)
	if n < 2 {
		report.OverfitScore = 0.5
		report.ProbNoOverfit = 0.5
		report.StabilityScore = 0.5
		return
	}

	avgDegrade := report.AvgDegradation
	stdDegrade := report.StdDegradation
	avgOOS := report.AvgTestSharpe
	stdOOS := report.StdTestSharpe

	overfitScore := 0.0

	if avgDegrade >= 1.0 {
		overfitScore = 0.0
	} else if avgDegrade >= 0.7 {
		overfitScore = 0.2
	} else if avgDegrade >= 0.5 {
		overfitScore = 0.5
	} else if avgDegrade >= 0.3 {
		overfitScore = 0.75
	} else {
		overfitScore = 1.0
	}

	if stdDegrade > 0.3 && n > 2 {
		punishment := math.Min(stdDegrade*1.5, 0.25)
		overfitScore += punishment
	}
	overfitScore = math.Min(overfitScore, 1.0)

	negativeWindows := 0
	for _, w := range report.Windows {
		if w.TestSharpe < 0 {
			negativeWindows++
		}
	}
	negativeRatio := float64(negativeWindows) / float64(n)
	if negativeRatio > 0.5 {
		overfitScore = math.Max(overfitScore, 0.8)
	}

	report.OverfitScore = overfitScore

	probNoOverfit := 1.0 - overfitScore
	if avgOOS > 1.0 && stdOOS < avgOOS*0.5 {
		probNoOverfit = math.Min(probNoOverfit+0.15, 1.0)
	}
	if report.PassRate > 0.8 {
		probNoOverfit = math.Min(probNoOverfit+0.10, 1.0)
	}
	report.ProbNoOverfit = probNoOverfit

	cv := 0.0
	if avgOOS > 1e-9 {
		cv = stdOOS / avgOOS
	}
	stability := 1.0 / (1.0 + cv*3.0)
	if n >= 5 {
		halfN := float64(n / 2)
		firstHalf := report.Windows[:n/2]
		secondHalf := report.Windows[n/2:]

		var firstSum, secondSum float64
		for _, w := range firstHalf {
			firstSum += w.TestSharpe
		}
		for _, w := range secondHalf {
			secondSum += w.TestSharpe
		}
		firstAvg := firstSum / halfN
		secondAvg := secondSum / halfN

		drift := math.Abs(firstAvg - secondAvg)
		if avgOOS > 1e-9 {
			driftNorm := drift / avgOOS
			stability *= (1.0 - math.Min(driftNorm*0.5, 0.3))
		}
	}
	report.StabilityScore = math.Max(0.0, math.Min(stability, 1.0))
}

// wfWindow represents a single walk-forward window.
type wfWindow struct {
	trainStart, trainEnd time.Time
	testStart, testEnd    time.Time
}

// buildWindows generates walk-forward windows from trading days.
//
// Rolling window scheme:
//
//	Window k:  Train = [k*Step, k*Step+TrainDays-1]
//	          Test  = [k*Step+TrainDays, k*Step+TrainDays+TestDays-1]
//
// Expanding window variant (anchored): if StepDays == 0, train start is fixed at day 0
// and only the end expands. This is useful for strategies that need more data over time.
func (wf *WalkForwardEngine) buildWindows(days []time.Time, params domain.WalkForwardParams) []wfWindow {
	var windows []wfWindow
	totalNeed := params.TrainDays + params.TestDays

	isExpanding := params.StepDays == 0
	step := params.StepDays
	if step <= 0 {
		step = params.TestDays
	}

	for startIdx := 0; startIdx+totalNeed <= len(days); startIdx += step {
		trainEndIdx := startIdx + params.TrainDays - 1
		testEndIdx := trainEndIdx + params.TestDays

		if isExpanding {
			trainEndIdx = startIdx + params.TrainDays - 1 + (startIdx/params.TestDays)*params.TestDays
			testEndIdx = trainEndIdx + params.TestDays
		}

		if testEndIdx >= len(days) {
			testEndIdx = len(days) - 1
			if testEndIdx-trainEndIdx < params.TestDays/2 {
				continue
			}
		}

		window := wfWindow{
			trainStart: days[startIdx],
			trainEnd:   days[trainEndIdx],
			testStart:  days[trainEndIdx+1],
			testEnd:    days[testEndIdx],
		}

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

func stdDev(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0.0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(n)
	variance := 0.0
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	variance /= float64(n - 1)
	return math.Sqrt(variance)
}
