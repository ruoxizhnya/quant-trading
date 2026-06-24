package hkex

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Helpers for building stub data
// ============================================================================

// dailyFlowByDate builds a dailyFn that returns a NorthboundFlow with
// the given TotalNetBuy for the given date, and nil for any other date.
// This lets a test script exactly the trading days it cares about.
func dailyFlowByDate(values map[string]float64) func(ctx context.Context, date time.Time) (*NorthboundFlow, error) {
	return func(ctx context.Context, date time.Time) (*NorthboundFlow, error) {
		key := date.Format("2006-01-02")
		if v, ok := values[key]; ok {
			return &NorthboundFlow{Date: date, TotalNetBuy: v}, nil
		}
		return nil, nil
	}
}

// makeStockFlows builds an ascending-by-date StockFlow slice with the
// given net-buy and holding-ratio sequences. Dates are consecutive
// weekdays starting from 2024-01-15 (a Monday).
func makeStockFlows(netBuys, holdings []float64) []StockFlow {
	if len(netBuys) != len(holdings) {
		panic("makeStockFlows: netBuys and holdings must have equal length")
	}
	flows := make([]StockFlow, len(netBuys))
	d := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC) // Monday
	for i := range netBuys {
		// Skip weekends.
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, 1)
		}
		flows[i] = StockFlow{
			Symbol:       "600519.SH",
			Name:         "贵州茅台",
			NetBuy:       netBuys[i],
			HoldingRatio: holdings[i],
			Date:         d,
		}
		d = d.AddDate(0, 0, 1)
	}
	return flows
}

// ============================================================================
// mean / sampleStdDev helpers
// ============================================================================

func TestMean_Empty(t *testing.T) {
	assert.Equal(t, 0.0, mean(nil))
	assert.Equal(t, 0.0, mean([]float64{}))
}

func TestMean_Basic(t *testing.T) {
	assert.Equal(t, 5.0, mean([]float64{2, 4, 6, 8}))
}

func TestMean_Negative(t *testing.T) {
	assert.Equal(t, -1.0, mean([]float64{-3, 1}))
}

func TestSampleStdDev_ShortSlice(t *testing.T) {
	assert.Equal(t, 0.0, sampleStdDev(nil))
	assert.Equal(t, 0.0, sampleStdDev([]float64{5}))
}

func TestSampleStdDev_Basic(t *testing.T) {
	// Sample stddev of {2,4,4,4,5,5,7,9} = sqrt(32/7) ≈ 2.138
	got := sampleStdDev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	want := math.Sqrt(32.0 / 7.0)
	assert.InDelta(t, want, got, 1e-9)
}

func TestSampleStdDev_AllEqual(t *testing.T) {
	assert.Equal(t, 0.0, sampleStdDev([]float64{5, 5, 5, 5}))
}

// ============================================================================
// NetBuyMA
// ============================================================================

func TestNetBuyMA_Basic(t *testing.T) {
	// 5 trading days of net buy: 10, 20, 30, 40, 50 → MA = 30
	flows := map[string]float64{
		"2024-01-15": 10, "2024-01-16": 20, "2024-01-17": 30,
		"2024-01-18": 40, "2024-01-19": 50,
	}
	stub := &stubFetcher{dailyFn: dailyFlowByDate(flows)}
	f := NewNorthboundFactor(stub)

	ma, err := f.NetBuyMA(context.Background(), time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC), 5)
	require.NoError(t, err)
	assert.Equal(t, 30.0, ma)
}

func TestNetBuyMA_NilFetcher(t *testing.T) {
	f := NewNorthboundFactor(nil)
	_, err := f.NetBuyMA(context.Background(), time.Now(), 5)
	require.Error(t, err)
}

func TestNetBuyMA_NoData(t *testing.T) {
	stub := &stubFetcher{dailyFn: dailyFlowByDate(map[string]float64{})}
	f := NewNorthboundFactor(stub)
	_, err := f.NetBuyMA(context.Background(), time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data")
}

func TestNetBuyMA_DaysClampedToOne(t *testing.T) {
	flows := map[string]float64{"2024-01-15": 42}
	stub := &stubFetcher{dailyFn: dailyFlowByDate(flows)}
	f := NewNorthboundFactor(stub)
	ma, err := f.NetBuyMA(context.Background(), time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), 0)
	require.NoError(t, err)
	assert.Equal(t, 42.0, ma)
}

func TestNetBuyMA_SkipsFetchErrors(t *testing.T) {
	// Day 17 errors, others succeed. MA should still compute from the
	// remaining 4 days.
	flows := map[string]float64{
		"2024-01-15": 10, "2024-01-16": 20,
		"2024-01-18": 40, "2024-01-19": 50,
	}
	stub := &stubFetcher{
		dailyFn: func(ctx context.Context, date time.Time) (*NorthboundFlow, error) {
			if date.Format("2006-01-02") == "2024-01-17" {
				return nil, errors.New("upstream flake")
			}
			return dailyFlowByDate(flows)(ctx, date)
		},
	}
	f := NewNorthboundFactor(stub)
	ma, err := f.NetBuyMA(context.Background(), time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC), 5)
	require.NoError(t, err)
	assert.Equal(t, 30.0, ma, "MA should average the 4 successful days")
}

// ============================================================================
// NetBuyMomentum
// ============================================================================

func TestNetBuyMomentum_Positive(t *testing.T) {
	// MA of {10,20,30,40,50} = 30; current = 50 → (50-30)/30 = 0.667
	flows := map[string]float64{
		"2024-01-15": 10, "2024-01-16": 20, "2024-01-17": 30,
		"2024-01-18": 40, "2024-01-19": 50,
	}
	stub := &stubFetcher{dailyFn: dailyFlowByDate(flows)}
	f := NewNorthboundFactor(stub)

	mom, err := f.NetBuyMomentum(context.Background(), time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC), 5)
	require.NoError(t, err)
	assert.InDelta(t, (50.0-30.0)/30.0, mom, 1e-9)
}

func TestNetBuyMomentum_Negative(t *testing.T) {
	// MA of {50,40,30,20,10} = 30; current = 10 → (10-30)/30 = -0.667
	flows := map[string]float64{
		"2024-01-15": 50, "2024-01-16": 40, "2024-01-17": 30,
		"2024-01-18": 20, "2024-01-19": 10,
	}
	stub := &stubFetcher{dailyFn: dailyFlowByDate(flows)}
	f := NewNorthboundFactor(stub)

	mom, err := f.NetBuyMomentum(context.Background(), time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC), 5)
	require.NoError(t, err)
	assert.InDelta(t, -0.6666667, mom, 1e-6)
}

func TestNetBuyMomentum_ZeroMA_ReturnsZero(t *testing.T) {
	// All zeros → MA = 0 → momentum = 0 (guard against div-by-zero).
	flows := map[string]float64{
		"2024-01-15": 0, "2024-01-16": 0, "2024-01-17": 0,
	}
	stub := &stubFetcher{dailyFn: dailyFlowByDate(flows)}
	f := NewNorthboundFactor(stub)

	mom, err := f.NetBuyMomentum(context.Background(), time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC), 3)
	require.NoError(t, err)
	assert.Equal(t, 0.0, mom, "zero MA should yield zero momentum, not ±Inf")
}

func TestNetBuyMomentum_NoData(t *testing.T) {
	stub := &stubFetcher{dailyFn: dailyFlowByDate(map[string]float64{})}
	f := NewNorthboundFactor(stub)
	_, err := f.NetBuyMomentum(context.Background(), time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC), 3)
	require.Error(t, err)
}

// ============================================================================
// HoldingChange
// ============================================================================

func TestHoldingChange_Basic(t *testing.T) {
	flows := makeStockFlows(
		[]float64{100, 200, 300, 400, 500},
		[]float64{3.0, 3.1, 3.2, 3.3, 3.5},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	change, err := f.HoldingChange(context.Background(), "600519.SH", flows[4].Date, 5)
	require.NoError(t, err)
	assert.Equal(t, 0.5, change, "3.5 - 3.0 = 0.5")
}

func TestHoldingChange_Negative(t *testing.T) {
	flows := makeStockFlows(
		[]float64{100, 200, 300, 400, 500},
		[]float64{4.0, 3.8, 3.6, 3.4, 3.2},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	change, err := f.HoldingChange(context.Background(), "600519.SH", flows[4].Date, 5)
	require.NoError(t, err)
	assert.InDelta(t, -0.8, change, 1e-9)
}

func TestHoldingChange_InsufficientData(t *testing.T) {
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return []StockFlow{{Symbol: "600519.SH", HoldingRatio: 3.0}}, nil
		},
	}
	f := NewNorthboundFactor(stub)
	_, err := f.HoldingChange(context.Background(), "600519.SH", time.Now(), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestHoldingChange_FetchError(t *testing.T) {
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return nil, errors.New("upstream down")
		},
	}
	f := NewNorthboundFactor(stub)
	_, err := f.HoldingChange(context.Background(), "600519.SH", time.Now(), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upstream down")
}

// ============================================================================
// NetBuyRank
// ============================================================================

func TestNetBuyRank_SortedDescending(t *testing.T) {
	top := []StockFlow{
		{Symbol: "A", NetBuy: 100},
		{Symbol: "B", NetBuy: 300},
		{Symbol: "C", NetBuy: 200},
	}
	stub := &stubFetcher{
		topFn: func(ctx context.Context, date time.Time, limit int) ([]StockFlow, error) {
			return top, nil
		},
	}
	f := NewNorthboundFactor(stub)

	ranked, err := f.NetBuyRank(context.Background(), time.Now(), 3)
	require.NoError(t, err)
	require.Len(t, ranked, 3)
	assert.Equal(t, "B", ranked[0].Symbol, "highest net buy first")
	assert.Equal(t, "C", ranked[1].Symbol)
	assert.Equal(t, "A", ranked[2].Symbol)
}

func TestNetBuyRank_FetchError(t *testing.T) {
	stub := &stubFetcher{
		topFn: func(ctx context.Context, date time.Time, limit int) ([]StockFlow, error) {
			return nil, errors.New("rank endpoint down")
		},
	}
	f := NewNorthboundFactor(stub)
	_, err := f.NetBuyRank(context.Background(), time.Now(), 3)
	require.Error(t, err)
}

func TestNetBuyRank_NilFetcher(t *testing.T) {
	f := NewNorthboundFactor(nil)
	_, err := f.NetBuyRank(context.Background(), time.Now(), 3)
	require.Error(t, err)
}

// ============================================================================
// IsNetInflow
// ============================================================================

func TestIsNetInflow_True(t *testing.T) {
	flows := makeStockFlows(
		[]float64{10, 20, 30, 40, 50},
		[]float64{1, 1, 1, 1, 1},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	inflow, err := f.IsNetInflow(context.Background(), "600519.SH", flows[4].Date, 5)
	require.NoError(t, err)
	assert.True(t, inflow, "all-positive net buys should be a streak")
}

func TestIsNetInflow_FalseDueToOneNegativeDay(t *testing.T) {
	flows := makeStockFlows(
		[]float64{10, 20, -5, 40, 50},
		[]float64{1, 1, 1, 1, 1},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	inflow, err := f.IsNetInflow(context.Background(), "600519.SH", flows[4].Date, 5)
	require.NoError(t, err)
	assert.False(t, inflow, "one negative day breaks the streak")
}

func TestIsNetInflow_InsufficientData(t *testing.T) {
	flows := makeStockFlows(
		[]float64{10, 20},
		[]float64{1, 1},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	inflow, err := f.IsNetInflow(context.Background(), "600519.SH", flows[1].Date, 5)
	require.NoError(t, err)
	assert.False(t, inflow, "fewer rows than `days` → false, not error")
}

func TestIsNetInflow_InvalidDays(t *testing.T) {
	stub := &stubFetcher{}
	f := NewNorthboundFactor(stub)
	_, err := f.IsNetInflow(context.Background(), "600519.SH", time.Now(), 0)
	require.Error(t, err)
}

// ============================================================================
// FlowSignal
// ============================================================================

func TestFlowSignal_StrongInflow(t *testing.T) {
	// Build a series where the last value is far above the mean + 2σ
	// and holding ratio is increasing.
	//
	// Mathematical note: with n=5 and a single outlier, the outlier
	// inflates σ so much it can never exceed mean+2σ (n³-5n²-4n+4 < 0
	// for n=5). With n=10, 9 tightly-clustered values + 1 outlier gives
	// σ ≈ 28.5, so 100 > 19 + 2*28.5 = 75.9 ✓.
	// netBuys: 9×10 + 100 → mean=19, σ≈28.5, upper≈75.9, current=100.
	// holdings: strictly increasing 1→10.
	netBuys := []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 100}
	holdings := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0}
	flows := makeStockFlows(netBuys, holdings)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	sig, err := f.FlowSignal(context.Background(), "600519.SH", flows[9].Date, 10)
	require.NoError(t, err)
	assert.Equal(t, FlowSignalStrongInflow, sig)
}

func TestFlowSignal_StrongOutflow(t *testing.T) {
	// Last value far below mean - 2σ, holding ratio decreasing.
	// netBuys: 9×10 + (-100) → mean=-1, σ≈34.8, lower≈-70.6, current=-100.
	// holdings: strictly decreasing 10→1.
	netBuys := []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, -100}
	holdings := []float64{10.0, 9.0, 8.0, 7.0, 6.0, 5.0, 4.0, 3.0, 2.0, 1.0}
	flows := makeStockFlows(netBuys, holdings)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	sig, err := f.FlowSignal(context.Background(), "600519.SH", flows[9].Date, 10)
	require.NoError(t, err)
	assert.Equal(t, FlowSignalStrongOutflow, sig)
}

func TestFlowSignal_Neutral_WithinBand(t *testing.T) {
	// All values equal → σ = 0 → mean ± 2σ = mean → current == mean → neutral.
	flows := makeStockFlows(
		[]float64{50, 50, 50, 50, 50},
		[]float64{3.0, 3.0, 3.0, 3.0, 3.0},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	sig, err := f.FlowSignal(context.Background(), "600519.SH", flows[4].Date, 5)
	require.NoError(t, err)
	assert.Equal(t, FlowSignalNeutral, sig)
}

func TestFlowSignal_Neutral_HoldingRatioFlat(t *testing.T) {
	// current > mean + 2σ BUT holding ratio is flat (change == 0) →
	// the "increasing" condition fails → neutral.
	flows := makeStockFlows(
		[]float64{10, 10, 10, 10, 1000},
		[]float64{3.0, 3.0, 3.0, 3.0, 3.0},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	sig, err := f.FlowSignal(context.Background(), "600519.SH", flows[4].Date, 5)
	require.NoError(t, err)
	assert.Equal(t, FlowSignalNeutral, sig, "flat holding ratio → not strong inflow")
}

func TestFlowSignal_TooFewDays(t *testing.T) {
	stub := &stubFetcher{}
	f := NewNorthboundFactor(stub)
	_, err := f.FlowSignal(context.Background(), "600519.SH", time.Now(), 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "days >= 3")
}

func TestFlowSignal_InsufficientData(t *testing.T) {
	flows := makeStockFlows(
		[]float64{10, 20},
		[]float64{1, 2},
	)
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return flows, nil
		},
	}
	f := NewNorthboundFactor(stub)

	_, err := f.FlowSignal(context.Background(), "600519.SH", flows[1].Date, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestFlowSignal_FetchError(t *testing.T) {
	stub := &stubFetcher{
		stockFlowFn: func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
			return nil, errors.New("network down")
		},
	}
	f := NewNorthboundFactor(stub)

	_, err := f.FlowSignal(context.Background(), "600519.SH", time.Now(), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network down")
}

func TestFlowSignal_NilFetcher(t *testing.T) {
	f := NewNorthboundFactor(nil)
	sig, err := f.FlowSignal(context.Background(), "600519.SH", time.Now(), 5)
	require.Error(t, err)
	assert.Equal(t, FlowSignalNeutral, sig)
}

// ============================================================================
// NorthboundFactor.SetLogger
// ============================================================================

func TestNorthboundFactor_SetLogger(t *testing.T) {
	// SetLogger must not panic and must not change behavior.
	f := NewNorthboundFactor(&stubFetcher{})
	assert.NotPanics(t, func() { f.SetLogger(zerolog.Nop()) })
}
