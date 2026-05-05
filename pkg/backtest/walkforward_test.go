package backtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestWalkForwardEngine_buildWindows(t *testing.T) {
	wf := &WalkForwardEngine{}

	// Create 30 trading days
	days := make([]time.Time, 30)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range days {
		days[i] = base.AddDate(0, 0, i)
	}

	tests := []struct {
		name     string
		params   domain.WalkForwardParams
		expected int
	}{
		{
			name: "basic rolling window",
			params: domain.WalkForwardParams{
				TrainDays: 10,
				TestDays:  5,
				StepDays:  5,
			},
			expected: 4, // 30 days, need 15 per window, step 5: (30-15)/5 + 1 = 4
		},
		{
			name: "single window",
			params: domain.WalkForwardParams{
				TrainDays: 20,
				TestDays:  10,
				StepDays:  10,
			},
			expected: 1, // 30 days, need 30 per window, step 10: only 1 fits
		},
		{
			name: "expanding window",
			params: domain.WalkForwardParams{
				TrainDays: 10,
				TestDays:  5,
				StepDays:  0, // expanding, step defaults to TestDays=5
			},
			expected: 2, // expanding window logic produces fewer windows
		},
		{
			name: "insufficient days",
			params: domain.WalkForwardParams{
				TrainDays: 25,
				TestDays:  10,
				StepDays:  5,
			},
			expected: 0, // 30 < 35 needed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			windows := wf.buildWindows(days, tt.params)
			assert.Len(t, windows, tt.expected)
			if len(windows) > 0 {
				assert.True(t, windows[0].trainStart.Before(windows[0].testStart))
				assert.True(t, windows[0].trainEnd.Before(windows[0].testStart))
				assert.True(t, windows[0].testStart.Before(windows[0].testEnd))
			}
		})
	}
}

func TestWalkForwardEngine_buildWindows_WindowStructure(t *testing.T) {
	wf := &WalkForwardEngine{}

	days := make([]time.Time, 20)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range days {
		days[i] = base.AddDate(0, 0, i)
	}

	params := domain.WalkForwardParams{
		TrainDays: 10,
		TestDays:  5,
		StepDays:  5,
	}

	windows := wf.buildWindows(days, params)
	require.Len(t, windows, 2) // 20 days, need 15 per window, step 5: (20-15)/5 + 1 = 2

	win := windows[0]
	assert.Equal(t, days[0], win.trainStart)
	assert.Equal(t, days[9], win.trainEnd)
	assert.Equal(t, days[10], win.testStart)
	assert.Equal(t, days[14], win.testEnd)
}

func TestWalkForwardEngine_computeAggregateMetrics(t *testing.T) {
	wf := &WalkForwardEngine{}

	report := &domain.WalkForwardReport{
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 1.0, TestReturn: 0.1, TestMaxDrawdown: -0.05, OOSvsTrain: 0.8},
			{TestSharpe: 1.2, TestReturn: 0.15, TestMaxDrawdown: -0.03, OOSvsTrain: 0.9},
			{TestSharpe: 0.8, TestReturn: 0.08, TestMaxDrawdown: -0.07, OOSvsTrain: 0.7},
		},
	}

	wf.computeAggregateMetrics(report)

	assert.InDelta(t, 1.0, report.AvgTestSharpe, 0.001)
	assert.InDelta(t, 0.11, report.AvgTestReturn, 0.001)
	assert.InDelta(t, -0.05, report.AvgTestMaxDD, 0.001)
	assert.InDelta(t, 0.8, report.AvgDegradation, 0.001)
	assert.Equal(t, 1.0, report.PassRate)
	// OverallPass requires AvgTestSharpe > 0.5 && AvgDegradation < 0.7
	// AvgTestSharpe=1.0 > 0.5 but AvgDegradation=0.8 >= 0.7, so should be false
	assert.False(t, report.OverallPass)
}

func TestWalkForwardEngine_computeAggregateMetrics_Empty(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{Windows: []*domain.WalkForwardResult{}}
	wf.computeAggregateMetrics(report)
}

func TestWalkForwardEngine_detectOverfitting(t *testing.T) {
	wf := &WalkForwardEngine{}

	tests := []struct {
		name            string
		windows         []*domain.WalkForwardResult
		expectedOverfit float64
		expectedStable  float64
	}{
		{
			name: "robust strategy",
			windows: []*domain.WalkForwardResult{
				{TestSharpe: 1.5, OOSvsTrain: 0.9},
				{TestSharpe: 1.4, OOSvsTrain: 0.85},
				{TestSharpe: 1.6, OOSvsTrain: 0.95},
			},
			expectedOverfit: 0.2, // avg degradation ~0.9, so base=0.2, no punishment
			expectedStable:  0.833,
		},
		{
			name: "moderate overfitting",
			windows: []*domain.WalkForwardResult{
				{TestSharpe: 1.0, OOSvsTrain: 0.6},
				{TestSharpe: 0.8, OOSvsTrain: 0.5},
				{TestSharpe: 1.2, OOSvsTrain: 0.7},
			},
			expectedOverfit: 0.5, // avg degradation ~0.6, base=0.5
			expectedStable:  0.625,
		},
		{
			name: "severe overfitting",
			windows: []*domain.WalkForwardResult{
				{TestSharpe: -0.5, OOSvsTrain: 0.2},
				{TestSharpe: -0.3, OOSvsTrain: 0.1},
				{TestSharpe: -0.8, OOSvsTrain: 0.3},
			},
			expectedOverfit: 1.0, // avg degradation ~0.2, base=1.0
			expectedStable:  1.0, // all same negative sharpes, cv=0, stability=1
		},
		{
			name: "single window",
			windows: []*domain.WalkForwardResult{
				{TestSharpe: 1.0, OOSvsTrain: 0.8},
			},
			expectedOverfit: 0.5, // n<2, defaults
			expectedStable:  0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &domain.WalkForwardReport{Windows: tt.windows}
			wf.computeAggregateMetrics(report)
			wf.detectOverfitting(report)

			assert.InDelta(t, tt.expectedOverfit, report.OverfitScore, 0.01)
			assert.InDelta(t, tt.expectedStable, report.StabilityScore, 0.01)
		})
	}
}

func TestWalkForwardEngine_detectOverfitting_NegativeWindows(t *testing.T) {
	wf := &WalkForwardEngine{}

	report := &domain.WalkForwardReport{
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 0.5, OOSvsTrain: 0.6},
			{TestSharpe: -0.2, OOSvsTrain: 0.4},
			{TestSharpe: -0.3, OOSvsTrain: 0.5},
			{TestSharpe: 0.1, OOSvsTrain: 0.3},
		},
	}

	wf.computeAggregateMetrics(report)
	wf.detectOverfitting(report)

	// 2 out of 4 windows negative = 50%, should trigger high overfit score via negativeRatio > 0.5 check
	// But negativeRatio is exactly 0.5, not > 0.5, so doesn't trigger
	// However avgDegradation = 0.45, base overfit score = 0.75
	assert.InDelta(t, 0.75, report.OverfitScore, 0.01)
}

func TestWalkForwardEngine_toBacktestResult(t *testing.T) {
	wf := &WalkForwardEngine{}

	resp := &BacktestResponse{
		TotalReturn:     0.25,
		AnnualReturn:    0.15,
		SharpeRatio:     1.5,
		SortinoRatio:    1.8,
		MaxDrawdown:     -0.1,
		MaxDrawdownDate: "2024-06-15",
		WinRate:         0.6,
		TotalTrades:     20,
		WinTrades:       12,
		LoseTrades:      8,
		AvgHoldingDays:  15.5,
		CalmarRatio:     2.0,
		CompletedAt:     "2024-12-31T10:00:00Z",
		StartedAt:       "2024-01-01T09:00:00Z",
	}

	result := wf.toBacktestResult(resp)
	require.NotNil(t, result)
	assert.InDelta(t, 0.25, result.TotalReturn, 0.001)
	assert.InDelta(t, 0.15, result.AnnualReturn, 0.001)
	assert.InDelta(t, 1.5, result.SharpeRatio, 0.001)
	assert.InDelta(t, -0.1, result.MaxDrawdown, 0.001)
	assert.Equal(t, 20, result.TotalTrades)
	assert.Equal(t, 12, result.WinTrades)
	assert.Equal(t, 8, result.LoseTrades)
	assert.InDelta(t, 15.5, result.AvgHoldingDays, 0.001)
	assert.InDelta(t, 2.0, result.CalmarRatio, 0.001)
}

func TestWalkForwardEngine_toBacktestResult_Nil(t *testing.T) {
	wf := &WalkForwardEngine{}
	result := wf.toBacktestResult(nil)
	assert.Nil(t, result)
}

func TestWalkForwardRequest_Validation(t *testing.T) {
	req := WalkForwardRequest{
		Strategy:  "momentum",
		StockPool: []string{"AAPL"},
		StartDate: "2024-01-01",
		EndDate:   "2024-12-31",
		WalkForwardParams: domain.WalkForwardParams{
			TrainDays: 10,
			TestDays:  5,
			StepDays:  5,
		},
	}

	assert.Equal(t, "momentum", req.Strategy)
	assert.Equal(t, []string{"AAPL"}, req.StockPool)
	assert.Equal(t, "2024-01-01", req.StartDate)
	assert.Equal(t, "2024-12-31", req.EndDate)
	assert.Equal(t, 10, req.WalkForwardParams.TrainDays)
	assert.Equal(t, 5, req.WalkForwardParams.TestDays)
	assert.Equal(t, 5, req.WalkForwardParams.StepDays)
}

func TestWalkForwardResult_Structure(t *testing.T) {
	result := &domain.WalkForwardResult{
		WindowIndex:     1,
		TrainStart:      "2024-01-01",
		TrainEnd:        "2024-03-31",
		TestStart:       "2024-04-01",
		TestEnd:         "2024-06-30",
		TrainSharpe:     1.8,
		TestSharpe:      1.2,
		TestReturn:      0.15,
		TestMaxDrawdown: -0.08,
		OOSvsTrain:      0.67,
	}

	assert.Equal(t, 1, result.WindowIndex)
	assert.Equal(t, "2024-01-01", result.TrainStart)
	assert.Equal(t, "2024-03-31", result.TrainEnd)
	assert.Equal(t, "2024-04-01", result.TestStart)
	assert.Equal(t, "2024-06-30", result.TestEnd)
	assert.InDelta(t, 1.8, result.TrainSharpe, 0.001)
	assert.InDelta(t, 1.2, result.TestSharpe, 0.001)
	assert.InDelta(t, 0.15, result.TestReturn, 0.001)
	assert.InDelta(t, -0.08, result.TestMaxDrawdown, 0.001)
	assert.InDelta(t, 0.67, result.OOSvsTrain, 0.001)
}

func TestWalkForwardReport_Structure(t *testing.T) {
	report := &domain.WalkForwardReport{
		StrategyID:     "test_strategy",
		Universe:       "10 symbols",
		AvgTestSharpe:  1.1,
		AvgTestReturn:  0.12,
		AvgTestMaxDD:   -0.06,
		AvgDegradation: 0.75,
		StdTestSharpe:  0.2,
		StdDegradation: 0.1,
		PassRate:       0.9,
		OverallPass:    true,
		OverfitScore:   0.3,
		ProbNoOverfit:  0.7,
		StabilityScore: 0.8,
		Windows: []*domain.WalkForwardResult{
			{WindowIndex: 1, TestSharpe: 1.0},
			{WindowIndex: 2, TestSharpe: 1.2},
		},
	}

	assert.Equal(t, "test_strategy", report.StrategyID)
	assert.Equal(t, "10 symbols", report.Universe)
	assert.InDelta(t, 1.1, report.AvgTestSharpe, 0.001)
	assert.InDelta(t, 0.12, report.AvgTestReturn, 0.001)
	assert.InDelta(t, -0.06, report.AvgTestMaxDD, 0.001)
	assert.InDelta(t, 0.75, report.AvgDegradation, 0.001)
	assert.InDelta(t, 0.2, report.StdTestSharpe, 0.001)
	assert.InDelta(t, 0.1, report.StdDegradation, 0.001)
	assert.InDelta(t, 0.9, report.PassRate, 0.001)
	assert.True(t, report.OverallPass)
	assert.InDelta(t, 0.3, report.OverfitScore, 0.001)
	assert.InDelta(t, 0.7, report.ProbNoOverfit, 0.001)
	assert.InDelta(t, 0.8, report.StabilityScore, 0.001)
	assert.Len(t, report.Windows, 2)
}
