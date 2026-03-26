package backtest

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// TestBuildWindows tests the walk-forward window generation logic.
func TestBuildWindows(t *testing.T) {
	// Create 300 trading days (mock)
	days := make([]time.Time, 300)
	for i := range days {
		days[i] = time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC)
	}

	tests := []struct {
		name        string
		params      domain.WalkForwardParams
		wantWindows int
	}{
		{
			name: "250 train + 60 test + 60 step → 0 windows (insufficient data)",
			params: domain.WalkForwardParams{
				TrainDays:    250,
				TestDays:     60,
				StepDays:     60,
				MinTrainDays: 125,
			},
			wantWindows: 0,
		},
		{
			name: "100 train + 20 test + 20 step → 10 windows",
			params: domain.WalkForwardParams{
				TrainDays:    100,
				TestDays:     20,
				StepDays:     20,
				MinTrainDays: 50,
			},
			wantWindows: 10,
		},
		{
			name: "50 train + 10 test + 10 step → 25 windows",
			params: domain.WalkForwardParams{
				TrainDays:    50,
				TestDays:     10,
				StepDays:     10,
				MinTrainDays: 25,
			},
			wantWindows: 25,
		},
		{
			name: "200 train + 50 test + 50 step → 2 windows",
			params: domain.WalkForwardParams{
				TrainDays:    200,
				TestDays:     50,
				StepDays:     50,
				MinTrainDays: 100,
			},
			wantWindows: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wf := &WalkForwardEngine{}
			got := wf.buildWindows(days, tc.params)
			if len(got) != tc.wantWindows {
				t.Errorf("buildWindows() = %d windows, want %d", len(got), tc.wantWindows)
			}
		})
	}
}

// TestBuildWindows_ExactDates tests that window boundaries are correct.
func TestBuildWindows_ExactDates(t *testing.T) {
	days := make([]time.Time, 100)
	for i := range days {
		days[i] = time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC)
	}

	params := domain.WalkForwardParams{
		TrainDays:    50,
		TestDays:     10,
		StepDays:     10,
		MinTrainDays: 25,
	}

	wf := &WalkForwardEngine{}
	windows := wf.buildWindows(days, params)

	if len(windows) != 5 {
		t.Fatalf("expected 5 windows, got %d", len(windows))
	}

	// Window 0: train [0,49], test [50,59]
	w0 := windows[0]
	if !w0.trainStart.Equal(days[0]) {
		t.Errorf("w0.trainStart = %v, want %v", w0.trainStart, days[0])
	}
	if !w0.trainEnd.Equal(days[49]) {
		t.Errorf("w0.trainEnd = %v, want %v", w0.trainEnd, days[49])
	}
	if !w0.testStart.Equal(days[50]) {
		t.Errorf("w0.testStart = %v, want %v", w0.testStart, days[50])
	}
	if !w0.testEnd.Equal(days[59]) {
		t.Errorf("w0.testEnd = %v, want %v", w0.testEnd, days[59])
	}

	// Window 1: train [10,59], test [60,69]
	w1 := windows[1]
	if !w1.trainStart.Equal(days[10]) {
		t.Errorf("w1.trainStart = %v, want %v", w1.trainStart, days[10])
	}
	if !w1.trainEnd.Equal(days[59]) {
		t.Errorf("w1.trainEnd = %v, want %v", w1.trainEnd, days[59])
	}
}

// TestBuildWindows_MinTrainDays tests the MinTrainDays constraint.
func TestBuildWindows_MinTrainDays(t *testing.T) {
	days := make([]time.Time, 100)
	for i := range days {
		days[i] = time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC)
	}

	params := domain.WalkForwardParams{
		TrainDays:    50,
		TestDays:     10,
		StepDays:     5, // small step creates many windows, some too small
		MinTrainDays: 30, // reject windows with < 30 train days
	}

	wf := &WalkForwardEngine{}
	windows := wf.buildWindows(days, params)

	// All windows should have at least 30 train days
	for i, w := range windows {
		trainDays := int(w.trainEnd.Sub(w.trainStart).Hours()/24) + 1
		if trainDays < 30 {
			t.Errorf("window %d train period = %d days, want >= 30", i, trainDays)
		}
	}
}

// TestToBacktestResult tests the BacktestResponse → BacktestResult conversion.
func TestToBacktestResult(t *testing.T) {
	wf := &WalkForwardEngine{}

	tests := []struct {
		name string
		resp *BacktestResponse
		want *domain.BacktestResult
	}{
		{
			name: "nil response",
			resp: nil,
			want: nil,
		},
		{
			name: "full response",
			resp: &BacktestResponse{
				ID:              "bt-123",
				Status:          "completed",
				TotalReturn:     0.35,
				AnnualReturn:    0.10,
				SharpeRatio:     1.5,
				SortinoRatio:    2.0,
				MaxDrawdown:     -0.15,
				MaxDrawdownDate: "2020-06-01",
				WinRate:         0.60,
				TotalTrades:     100,
				WinTrades:       60,
				LoseTrades:      40,
				AvgHoldingDays:   5.5,
				CalmarRatio:     0.67,
				StartedAt:       "2020-01-01T00:00:00Z",
				CompletedAt:     "2020-12-31T23:59:59Z",
			},
			want: &domain.BacktestResult{
				TotalReturn:   0.35,
				AnnualReturn:  0.10,
				SharpeRatio:  1.5,
				SortinoRatio: 2.0,
				MaxDrawdown:  -0.15,
				WinRate:      0.60,
				TotalTrades:  100,
				WinTrades:    60,
				LoseTrades:   40,
				AvgHoldingDays: 5.5,
				CalmarRatio:  0.67,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wf.toBacktestResult(tc.resp)
			if tc.want == nil {
				if got != nil {
					t.Errorf("toBacktestResult() = %v, want nil", got)
				}
				return
			}
			if got.TotalReturn != tc.want.TotalReturn {
				t.Errorf("TotalReturn = %v, want %v", got.TotalReturn, tc.want.TotalReturn)
			}
			if got.SharpeRatio != tc.want.SharpeRatio {
				t.Errorf("SharpeRatio = %v, want %v", got.SharpeRatio, tc.want.SharpeRatio)
			}
			if got.MaxDrawdown != tc.want.MaxDrawdown {
				t.Errorf("MaxDrawdown = %v, want %v", got.MaxDrawdown, tc.want.MaxDrawdown)
			}
			if got.TotalTrades != tc.want.TotalTrades {
				t.Errorf("TotalTrades = %v, want %v", got.TotalTrades, tc.want.TotalTrades)
			}
		})
	}
}

// TestBuildWindows_EdgeCases tests edge cases.
func TestBuildWindows_EdgeCases(t *testing.T) {
	wf := &WalkForwardEngine{}

	t.Run("empty days", func(t *testing.T) {
		days := []time.Time{}
		params := domain.WalkForwardParams{
			TrainDays: 100, TestDays: 20, StepDays: 20, MinTrainDays: 50,
		}
		windows := wf.buildWindows(days, params)
		if len(windows) != 0 {
			t.Errorf("buildWindows with empty days = %d, want 0", len(windows))
		}
	})

	t.Run("days shorter than train+test", func(t *testing.T) {
		days := make([]time.Time, 50)
		for i := range days {
			days[i] = time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC)
		}
		params := domain.WalkForwardParams{
			TrainDays: 100, TestDays: 20, StepDays: 20, MinTrainDays: 50,
		}
		windows := wf.buildWindows(days, params)
		if len(windows) != 0 {
			t.Errorf("buildWindows with short days = %d, want 0", len(windows))
		}
	})

	t.Run("exact fit 1 window", func(t *testing.T) {
		days := make([]time.Time, 120)
		for i := range days {
			days[i] = time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC)
		}
		params := domain.WalkForwardParams{
			TrainDays: 100, TestDays: 20, StepDays: 100, MinTrainDays: 50,
		}
		windows := wf.buildWindows(days, params)
		if len(windows) != 1 {
			t.Errorf("buildWindows exact fit = %d windows, want 1", len(windows))
		}
	})
}
