package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string                                { return m.name }
func (m *mockProvider) CheckConnectivity(ctx context.Context) error { return nil }
func (m *mockProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	return nil, nil
}
func (m *mockProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	return nil, nil
}
func (m *mockProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	return nil, nil
}
func (m *mockProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *mockProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	return nil, nil
}
func (m *mockProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	return nil, nil
}
func (m *mockProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	return domain.Stock{}, nil
}
func (m *mockProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	return nil, nil
}
func (m *mockProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	return true, nil
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.trading.stamp_tax_rate", 0.001)
	v.Set("backtest.trading.min_commission", 5.0)
	v.Set("backtest.trading.transfer_fee_rate", 0.00001)
	v.Set("backtest.trading.price_limit.normal", 0.10)
	v.Set("backtest.trading.price_limit.st", 0.05)
	v.Set("backtest.trading.price_limit.new", 0.20)
	v.Set("backtest.trading.new_stock_days", 60)

	eng, err := NewEngine(v, nil, zerolog.Nop())
	require.NoError(t, err)
	return eng
}

func TestEngine_SetDataAdapter(t *testing.T) {
	eng := newTestEngine(t)
	assert.Nil(t, eng.DataAdapter())
	eng.SetDataAdapter(nil)
	assert.Nil(t, eng.DataAdapter())
}

func TestEngine_SetStore(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetStore(nil)
	assert.Nil(t, eng.store)
}

func TestEngine_SwitchDataSource_NoAdapter(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.SwitchDataSource(context.Background(), "test", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no DataAdapter set")
}

func TestEngine_SwitchDataSource_WithAdapter(t *testing.T) {
	eng := newTestEngine(t)

	// Create a mock provider for initial adapter
	initialProvider := &mockProvider{name: "postgres"}
	bus := marketdata.NewEventBus(1)
	adapter := marketdata.NewDataAdapter(bus, initialProvider, nil, zerolog.Nop())
	eng.SetDataAdapter(adapter)
	assert.NotNil(t, eng.DataAdapter())
	assert.Equal(t, "postgres", eng.DataAdapter().Primary())

	// Switch to a new provider
	newProvider := &mockProvider{name: "http"}
	err := eng.SwitchDataSource(context.Background(), "http", newProvider)
	assert.NoError(t, err)
	assert.Equal(t, "http", eng.DataAdapter().Primary())
}

func TestEngine_LoadFactorCache(t *testing.T) {
	eng := newTestEngine(t)

	cache := map[domain.FactorType]map[time.Time]map[string]float64{
		domain.FactorMomentum: {
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC): {
				"600000.SH": 1.5,
			},
		},
	}
	eng.LoadFactorCache(cache)

	z, ok := eng.GetFactorZScore(domain.FactorMomentum, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "600000.SH")
	assert.True(t, ok)
	assert.InDelta(t, 1.5, z, 0.001)
}

func TestEngine_GetFactorZScore_Miss(t *testing.T) {
	eng := newTestEngine(t)

	z, ok := eng.GetFactorZScore(domain.FactorMomentum, time.Now(), "NOTEXIST.SH")
	assert.False(t, ok)
	assert.Equal(t, 0.0, z)
}

func TestEngine_GetFactorZScore_NoCache(t *testing.T) {
	eng := newTestEngine(t)
	eng.LoadFactorCache(nil)

	z, ok := eng.GetFactorZScore(domain.FactorMomentum, time.Now(), "600000.SH")
	assert.False(t, ok)
	assert.Equal(t, 0.0, z)
}

func TestEngine_GetFactorZScore_WrongFactor(t *testing.T) {
	eng := newTestEngine(t)
	cache := map[domain.FactorType]map[time.Time]map[string]float64{
		domain.FactorMomentum: {
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC): {
				"600000.SH": 1.5,
			},
		},
	}
	eng.LoadFactorCache(cache)

	z, ok := eng.GetFactorZScore(domain.FactorValue, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "600000.SH")
	assert.False(t, ok)
	assert.Equal(t, 0.0, z)
}

func TestEngine_GetFactorZScore_WrongDate(t *testing.T) {
	eng := newTestEngine(t)
	cache := map[domain.FactorType]map[time.Time]map[string]float64{
		domain.FactorMomentum: {
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC): {
				"600000.SH": 1.5,
			},
		},
	}
	eng.LoadFactorCache(cache)

	z, ok := eng.GetFactorZScore(domain.FactorMomentum, time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), "600000.SH")
	assert.False(t, ok)
	assert.Equal(t, 0.0, z)
}

func TestEngine_SetRiskManager(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetRiskManager(nil)
	assert.Nil(t, eng.riskManager)
}

func TestEngine_GetBacktestResult_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.GetBacktestResult("nonexistent")
	assert.Error(t, err)
}

func TestEngine_GetBacktestResult_Completed(t *testing.T) {
	eng := newTestEngine(t)
	eng.stateStore.Put("test-123", &BacktestState{ID: "test-123", Status: "completed", Result: &domain.BacktestResult{TotalReturn: 0.15}})

	result, err := eng.GetBacktestResult("test-123")
	require.NoError(t, err)
	assert.InDelta(t, 0.15, result.TotalReturn, 0.001)
}

func TestEngine_GetBacktestResult_NotCompleted(t *testing.T) {
	eng := newTestEngine(t)
	eng.stateStore.Put("test-456", &BacktestState{ID: "test-456", Status: "running"})

	_, err := eng.GetBacktestResult("test-456")
	assert.Error(t, err)
}

func TestEngine_GetBacktestTrades_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.GetBacktestTrades("nonexistent")
	assert.Error(t, err)
}

func TestEngine_GetBacktestTrades_Found(t *testing.T) {
	eng := newTestEngine(t)
	tracker := NewTracker(1000000, 0.0003, 0.001, defaultTradingConfig(), zerolog.Nop())
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 100, 10.0, day, nil)

	eng.stateStore.Put("test-trades", &BacktestState{ID: "test-trades", Status: "completed", Tracker: tracker})

	trades, err := eng.GetBacktestTrades("test-trades")
	require.NoError(t, err)
	assert.Len(t, trades, 1)
}

func TestEngine_GetBacktestEquity_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.GetBacktestEquity("nonexistent")
	assert.Error(t, err)
}

func TestEngine_GetBacktestEquity_Found(t *testing.T) {
	eng := newTestEngine(t)
	tracker := NewTracker(1000000, 0.0003, 0.001, defaultTradingConfig(), zerolog.Nop())
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	tracker.RecordDailyValue(day, map[string]float64{})

	eng.stateStore.Put("test-equity", &BacktestState{ID: "test-equity", Status: "completed", Tracker: tracker})

	curve, err := eng.GetBacktestEquity("test-equity")
	require.NoError(t, err)
	assert.Len(t, curve, 1)
}

func TestEngine_GetBacktestStatus_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.GetBacktestStatus("nonexistent")
	assert.Error(t, err)
}

func TestEngine_GetBacktestStatus_Found(t *testing.T) {
	eng := newTestEngine(t)
	eng.stateStore.Put("test-status", &BacktestState{ID: "test-status", Status: "running"})

	status, err := eng.GetBacktestStatus("test-status")
	require.NoError(t, err)
	assert.Equal(t, "running", status)
}

func TestEngine_LoadOHLCVInMemory(t *testing.T) {
	eng := newTestEngine(t)
	data := map[string][]domain.OHLCV{
		"600000.SH": {
			{Symbol: "600000.SH", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Open: 10.0, Close: 10.5},
		},
	}
	eng.LoadOHLCVInMemory(data)
	// P1-16 (ADR-020): access via CacheManager sub-component.
	// S7-P2-1: use Snapshot() — direct field access to
	// inMemoryOHLCVAtomic is no longer possible from the parent package
	// after CacheManager moved to the cache/ subpackage.
	assert.Equal(t, data, eng.CacheManager().Snapshot())

	// Passing nil clears the cache. To keep the L1 atomic snapshot valid
	// (lock-free readers never observe a nil pointer), the cache is reset to
	// an empty map rather than left as nil. Symbol lookups still fall through
	// to the provider, matching the documented "L1 miss → provider fallback"
	// contract.
	eng.LoadOHLCVInMemory(nil)
	snap := eng.CacheManager().Snapshot()
	assert.NotNil(t, snap)
	assert.Empty(t, snap)
}

func TestEngine_hasSTPrefix(t *testing.T) {
	// AUD-08 (ODR-065 H4): this table previously asserted
	// {"*STXYZ.SH", false} — i.e. it encoded the name[:2] bug as the
	// specification. Fixing AUD-08 reddens that assertion, which is
	// exactly the trap the audit called out ("would be mistaken for a
	// regression and rolled back"). The expectations are corrected
	// here, in the same commit as the fix.
	//
	// Note the inputs are stock NAMES, not ts_codes — the production
	// call site passes stockName ("*ST某某"), and the old table's use
	// of symbol-shaped strings ("*STXYZ.SH") obscured that.
	tests := []struct {
		name string
		isST bool
	}{
		{"ST某某", true},
		{"*ST某某", true},  // was false — the bug
		{"SST某某", true},  // was absent — also never matched
		{"S*ST某某", true}, // was absent — also never matched
		{"平安银行", false},
		{"600000.SH", false}, // a ts_code is not a name; not ST
		{"stabc", false},     // lowercase is not the exchange convention
		{"ST", true},         // bare prefix
		{"S", false},         // too short
		{"", false},          // empty
		{" ST某某", true},      // leading whitespace tolerated
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.isST, hasSTPrefix(tc.name))
		})
	}
}

func TestDateRangeBounds(t *testing.T) {
	mkBars := func(dates ...string) []domain.OHLCV {
		out := make([]domain.OHLCV, 0, len(dates))
		for _, s := range dates {
			d, _ := time.Parse("2006-01-02", s)
			out = append(out, domain.OHLCV{Symbol: "X", Date: d, Close: 1.0})
		}
		return out
	}

	t.Run("empty slice", func(t *testing.T) {
		lo, hi := dateRangeBounds(nil, time.Now(), time.Now())
		assert.Equal(t, 0, lo)
		assert.Equal(t, -1, hi)
	})

	t.Run("single bar", func(t *testing.T) {
		bars := mkBars("2024-01-02")
		lo, hi := dateRangeBounds(bars,
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC))
		assert.Equal(t, 0, lo)
		assert.Equal(t, 0, hi)
	})

	t.Run("range fully inside", func(t *testing.T) {
		bars := mkBars("2024-01-02", "2024-01-03", "2024-01-04", "2024-01-05")
		lo, hi := dateRangeBounds(bars,
			time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC))
		assert.Equal(t, 1, lo)
		assert.Equal(t, 2, hi)
	})

	t.Run("range entirely before data (no match)", func(t *testing.T) {
		bars := mkBars("2024-01-10", "2024-01-11")
		lo, hi := dateRangeBounds(bars,
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC))
		// All bars are after `end`, so lo is the index of the first bar > end
		// and hi is one less. lo=0, hi=-1 → lo > hi → empty range.
		assert.Equal(t, 0, lo)
		assert.Equal(t, -1, hi)
		assert.True(t, lo > hi, "expected lo>hi for empty match: lo=%d hi=%d", lo, hi)
	})

	t.Run("range entirely after data (no match)", func(t *testing.T) {
		bars := mkBars("2024-01-02", "2024-01-03")
		lo, hi := dateRangeBounds(bars,
			time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC))
		// No bar >= start, lo returns n (==2) and hi is one before.
		assert.Equal(t, 2, lo)
		assert.True(t, hi < lo, "expected hi<lo when no bars match: lo=%d hi=%d", lo, hi)
	})

	t.Run("range covers all", func(t *testing.T) {
		bars := mkBars("2024-01-02", "2024-01-03", "2024-01-04")
		lo, hi := dateRangeBounds(bars,
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC))
		assert.Equal(t, 0, lo)
		assert.Equal(t, len(bars)-1, hi)
	})

	t.Run("end equals last bar date (inclusive)", func(t *testing.T) {
		bars := mkBars("2024-01-02", "2024-01-03", "2024-01-04")
		end := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)
		lo, hi := dateRangeBounds(bars, end, end)
		assert.Equal(t, 2, lo)
		assert.Equal(t, 2, hi)
	})
}
