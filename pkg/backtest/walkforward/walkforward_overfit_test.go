package walkforward

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
	"github.com/stretchr/testify/assert"
)

func TestDetectOverfitting_LessThan2Windows(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 1.0},
		},
	}
	wf.detectOverfitting(report)
	assert.InDelta(t, 0.5, report.OverfitScore, 0.001)
	assert.InDelta(t, 0.5, report.ProbNoOverfit, 0.001)
	assert.InDelta(t, 0.5, report.StabilityScore, 0.001)
}

func TestDetectOverfitting_RobustStrategy(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		AvgDegradation: 0.9,
		StdDegradation: 0.1,
		AvgTestSharpe:  1.5,
		StdTestSharpe:  0.3,
		PassRate:       0.9,
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 1.4},
			{TestSharpe: 1.5},
			{TestSharpe: 1.6},
		},
	}
	wf.detectOverfitting(report)
	assert.Less(t, report.OverfitScore, 0.3)
	assert.Greater(t, report.ProbNoOverfit, 0.7)
	assert.Greater(t, report.StabilityScore, 0.5)
}

func TestDetectOverfitting_SevereOverfit(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		AvgDegradation: 0.1,
		StdDegradation: 0.5,
		AvgTestSharpe:  -0.5,
		StdTestSharpe:  1.0,
		PassRate:       0.2,
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: -1.0},
			{TestSharpe: -0.5},
			{TestSharpe: 0.2},
		},
	}
	wf.detectOverfitting(report)
	assert.Greater(t, report.OverfitScore, 0.5)
}

func TestDetectOverfitting_ModerateDegradation(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		AvgDegradation: 0.6,
		StdDegradation: 0.1,
		AvgTestSharpe:  0.8,
		StdTestSharpe:  0.2,
		PassRate:       0.6,
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 0.7},
			{TestSharpe: 0.8},
			{TestSharpe: 0.9},
		},
	}
	wf.detectOverfitting(report)
	assert.Greater(t, report.OverfitScore, 0.3)
	assert.Less(t, report.OverfitScore, 0.8)
}

func TestDetectOverfitting_HighVariancePunishment(t *testing.T) {
	wf := &WalkForwardEngine{}
	report := &domain.WalkForwardReport{
		AvgDegradation: 0.8,
		StdDegradation: 0.5,
		AvgTestSharpe:  0.5,
		StdTestSharpe:  0.8,
		PassRate:       0.5,
		Windows: []*domain.WalkForwardResult{
			{TestSharpe: 0.2},
			{TestSharpe: 1.2},
			{TestSharpe: -0.2},
		},
	}
	wf.detectOverfitting(report)
	assert.Greater(t, report.OverfitScore, 0.2)
}

func TestDetectOverfitting_ManyNegativeWindows(t *testing.T) {
	wf := &WalkForwardEngine{}
	windows := make([]*domain.WalkForwardResult, 6)
	for i := range windows {
		if i < 4 {
			windows[i] = &domain.WalkForwardResult{TestSharpe: -0.5}
		} else {
			windows[i] = &domain.WalkForwardResult{TestSharpe: 0.3}
		}
	}
	report := &domain.WalkForwardReport{
		AvgDegradation: 0.4,
		StdDegradation: 0.1,
		AvgTestSharpe:  -0.1,
		StdTestSharpe:  0.4,
		PassRate:       0.33,
		Windows:        windows,
	}
	wf.detectOverfitting(report)
	assert.GreaterOrEqual(t, report.OverfitScore, 0.8)
}

func TestDetectOverfitting_StabilityWithDrift(t *testing.T) {
	wf := &WalkForwardEngine{}
	windows := make([]*domain.WalkForwardResult, 10)
	for i := range windows {
		if i < 5 {
			windows[i] = &domain.WalkForwardResult{TestSharpe: 1.5}
		} else {
			windows[i] = &domain.WalkForwardResult{TestSharpe: 0.3}
		}
	}
	report := &domain.WalkForwardReport{
		AvgDegradation: 0.7,
		StdDegradation: 0.1,
		AvgTestSharpe:  0.9,
		StdTestSharpe:  0.5,
		PassRate:       0.7,
		Windows:        windows,
	}
	wf.detectOverfitting(report)
	assert.Less(t, report.StabilityScore, 0.8)
}

func TestStdDev(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   float64
	}{
		{"empty", nil, 0.0},
		{"single", []float64{1.0}, 0.0},
		{"two same", []float64{5.0, 5.0}, 0.0},
		{"two different", []float64{1.0, 3.0}, math.Sqrt(2.0)},
		{"three values", []float64{2.0, 4.0, 6.0}, math.Sqrt(8.0 / 2.0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := statistics.SampleStdDev(tc.values)
			assert.InDelta(t, tc.want, got, 0.001)
		})
	}
}
