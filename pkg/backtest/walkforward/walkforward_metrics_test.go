package walkforward

import (
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWalkForwardEngine(t *testing.T) {
	wf := NewWalkForwardEngine(nil, nil, zerolog.Nop())
	assert.NotNil(t, wf)
}

func TestComputeAggregateMetrics(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 1.5, TestReturn: 0.2, TestMaxDrawdown: -0.1, OOSvsTrain: 0.8},
			{TestSharpe: 1.0, TestReturn: 0.15, TestMaxDrawdown: -0.15, OOSvsTrain: 0.7},
			{TestSharpe: 0.5, TestReturn: 0.05, TestMaxDrawdown: -0.2, OOSvsTrain: 0.6},
		},
	}

	wf.computeAggregateMetrics(report)

	assert.InDelta(t, 1.0, report.AvgTestSharpe, 0.01)
	assert.InDelta(t, 0.133, report.AvgTestReturn, 0.01)
	assert.InDelta(t, -0.15, report.AvgTestMaxDD, 0.01)
	assert.InDelta(t, 0.7, report.AvgDegradation, 0.01)
	assert.InDelta(t, 1.0, report.PassRate, 0.01)
	assert.Greater(t, report.StdTestSharpe, 0.0)
	assert.Greater(t, report.StdDegradation, 0.0)
	assert.False(t, report.OverallPass)
}

func TestComputeAggregateMetrics_Empty(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{Windows: []*domain.WalkForwardResult{}}

	wf.computeAggregateMetrics(report)
	assert.InDelta(t, 0.0, report.AvgTestSharpe, 0.01)
}

func TestComputeAggregateMetrics_AllNegativeSharpe(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: -0.5, TestReturn: -0.1, TestMaxDrawdown: -0.3, OOSvsTrain: 0.2},
			{TestSharpe: -1.0, TestReturn: -0.2, TestMaxDrawdown: -0.4, OOSvsTrain: 0.1},
		},
	}

	wf.computeAggregateMetrics(report)
	assert.Negative(t, report.AvgTestSharpe)
	assert.Equal(t, 0.0, report.PassRate)
	assert.False(t, report.OverallPass)
}

func TestComputeAggregateMetrics_NaNDeogradation(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 1.0, TestReturn: 0.1, TestMaxDrawdown: -0.1, OOSvsTrain: math.NaN()},
			{TestSharpe: 0.8, TestReturn: 0.08, TestMaxDrawdown: -0.12, OOSvsTrain: 0.9},
		},
	}

	wf.computeAggregateMetrics(report)
	assert.True(t, math.IsNaN(report.AvgDegradation))
	assert.InDelta(t, 0.0, report.StdDegradation, 0.01)
}

func TestBuildWindows_Rolling(t *testing.T) {
	wf := &WalkForwardEngine{}
	days := make([]time.Time, 300)
	base := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range days {
		days[i] = base.AddDate(0, 0, i)
	}

	params := domain.WalkForwardParams{
		TrainDays:    60,
		TestDays:     20,
		StepDays:     20,
		MinTrainDays: 30,
	}

	windows := wf.buildWindows(days, params)
	require.NotEmpty(t, windows)

	for i, w := range windows {
		assert.True(t, w.trainStart.Before(w.trainEnd), "window %d: trainStart < trainEnd", i)
		assert.True(t, w.testStart.Before(w.testEnd) || w.testStart.Equal(w.testEnd), "window %d: testStart <= testEnd", i)
		assert.True(t, w.trainEnd.Before(w.testStart), "window %d: trainEnd < testStart", i)
	}
}

func TestBuildWindows_Expanding(t *testing.T) {
	wf := &WalkForwardEngine{}
	days := make([]time.Time, 300)
	base := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range days {
		days[i] = base.AddDate(0, 0, i)
	}

	params := domain.WalkForwardParams{
		TrainDays:    60,
		TestDays:     20,
		StepDays:     0,
		MinTrainDays: 30,
	}

	windows := wf.buildWindows(days, params)
	require.NotEmpty(t, windows)

	assert.Equal(t, days[0], windows[0].trainStart)
}

func TestBuildWindows_InsufficientDays(t *testing.T) {
	wf := &WalkForwardEngine{}
	days := make([]time.Time, 50)
	base := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range days {
		days[i] = base.AddDate(0, 0, i)
	}

	params := domain.WalkForwardParams{
		TrainDays:    60,
		TestDays:     20,
		StepDays:     20,
		MinTrainDays: 30,
	}

	windows := wf.buildWindows(days, params)
	assert.Empty(t, windows)
}

func TestRunWalkForward_InvalidParams(t *testing.T) {
	wf := NewWalkForwardEngine(nil, nil, zerolog.Nop())

	tests := []struct {
		name string
		req  WalkForwardRequest
	}{
		{
			"zero train days",
			WalkForwardRequest{StartDate: "2020-01-01", EndDate: "2023-12-31", WalkForwardParams: domain.WalkForwardParams{TrainDays: 0, TestDays: 20, StepDays: 10}},
		},
		{
			"zero test days",
			WalkForwardRequest{StartDate: "2020-01-01", EndDate: "2023-12-31", WalkForwardParams: domain.WalkForwardParams{TrainDays: 60, TestDays: 0, StepDays: 10}},
		},
		{
			"zero step days",
			WalkForwardRequest{StartDate: "2020-01-01", EndDate: "2023-12-31", WalkForwardParams: domain.WalkForwardParams{TrainDays: 60, TestDays: 20, StepDays: 0}},
		},
		{
			"invalid start date",
			WalkForwardRequest{StartDate: "not-a-date", EndDate: "2023-12-31", WalkForwardParams: domain.WalkForwardParams{TrainDays: 60, TestDays: 20, StepDays: 10}},
		},
		{
			"invalid end date",
			WalkForwardRequest{StartDate: "2020-01-01", EndDate: "bad", WalkForwardParams: domain.WalkForwardParams{TrainDays: 60, TestDays: 20, StepDays: 10}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := wf.RunWalkForward(nil, tc.req)
			assert.Error(t, err)
		})
	}
}

func TestWalkForwardToBacktestResult(t *testing.T) {
	wf := &WalkForwardEngine{}
	resp := &contracts.BacktestResponse{
		TotalReturn:  0.15,
		SharpeRatio:  1.2,
		MaxDrawdown:  -0.1,
		WinRate:      0.6,
		TotalTrades:  30,
		AnnualReturn: 0.08,
	}

	result := wf.toBacktestResult(resp)
	require.NotNil(t, result)
	assert.InDelta(t, 0.15, result.TotalReturn, 0.001)
	assert.InDelta(t, 1.2, result.SharpeRatio, 0.001)
}

func TestWalkForwardToBacktestResult_Nil(t *testing.T) {
	wf := &WalkForwardEngine{}
	result := wf.toBacktestResult(nil)
	assert.Nil(t, result)
}
