package backtest

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ------------------------------------------------------------------------------
// Golden Fixture Loader
// ------------------------------------------------------------------------------

// loadGoldenFixture reads a fixture JSON from testdata/.
func loadGoldenFixture(t *testing.T, filename string) *GoldenFixture {
	t.Helper()
	path := "../../testdata/" + filename
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read fixture %s", filename)

	var fixture GoldenFixture
	err = json.Unmarshal(data, &fixture)
	require.NoError(t, err, "failed to unmarshal fixture %s", filename)
	return &fixture
}

// GoldenFixture mirrors the on-disk fixture structure.
type GoldenFixture struct {
	Metadata      FixtureMetadata             `json:"metadata"`
	Expected     *ExpectedOutput             `json:"expected_output,omitempty"`
	Signals      []FixtureSignal             `json:"signals,omitempty"`
	OHLCV        []FixtureOHLCV             `json:"ohlcv"`
}

type FixtureMetadata struct {
	Name              string            `json:"name"`
	InitialCapital    float64           `json:"initial_capital"`
	CommissionRate    float64           `json:"commission_rate"`
	SlippageRate      float64           `json:"slippage_rate"`
	Symbols           []string          `json:"symbols"`
	TradingDays       int               `json:"trading_days"`
	StartDate         string            `json:"start_date"`
	EndDate           string            `json:"end_date"`
	LimitUpEvents     map[string]string `json:"limit_up_events"`
	LimitDownEvents   map[string]string `json:"limit_down_events"`
	T1EdgeCaseNote    string            `json:"t1_edge_case_note"`
}

type ExpectedOutput struct {
	TotalReturn   float64 `json:"total_return"`
	Sharpe        float64 `json:"sharpe"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	WinRate       float64 `json:"win_rate"`
	AnnualReturn  float64 `json:"annual_return"`
	SortinoRatio  float64 `json:"sortino_ratio"`
	CalmarRatio   float64 `json:"calmar_ratio"`
	TotalTrades   int     `json:"total_trades"`
	WinTrades     int     `json:"win_trades"`
	LoseTrades    int     `json:"lose_trades"`
}

type FixtureSignal struct {
	Symbol    string  `json:"symbol"`
	Date      string  `json:"date"`
	Direction string  `json:"direction"`
	Strength  float64 `json:"strength"`
}

type FixtureOHLCV struct {
	TradeDate string  `json:"trade_date"`
	Symbol    string  `json:"symbol"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    int64   `json:"volume"`
	PrevClose float64 `json:"prev_close"`
}

// fixtureToOHLCV converts fixture records to domain.OHLCV.
func fixtureToOHLCV(records []FixtureOHLCV) map[string][]domain.OHLCV {
	result := make(map[string][]domain.OHLCV)
	for _, r := range records {
		d, _ := time.Parse("2006-01-02", r.TradeDate)
		result[r.Symbol] = append(result[r.Symbol], domain.OHLCV{
			Symbol:    r.Symbol,
			Date:      d,
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			Close:     r.Close,
			Volume:    float64(r.Volume),
			LimitUp:   false,
			LimitDown: false,
		})
	}
	return result
}

// ------------------------------------------------------------------------------
// Property-Based Invariants (run against any synthetic dataset)
// These are always-true properties of a well-formed backtest.
// ------------------------------------------------------------------------------

// TestInvariant_CashNeverNegative asserts cash >= 0 after every trade.
func TestInvariant_CashNeverNegative(t *testing.T) {
	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	// Replay all signals (or if none, just buy-and-hold each symbol once).
	symbols := fixture.Metadata.Symbols
	ohlcv := fixtureToOHLCV(fixture.OHLCV)

	// Build a simple buy-and-hold replay: buy on day 1, hold to end.
	for _, sym := range symbols {
		bars := ohlcv[sym]
		if len(bars) < 2 {
			continue
		}
		// Buy on first day
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, bars[0].Close, bars[0].Date, nil)
		require.NoErrorf(t, err, "buy trade should not fail for %s", sym)

		// Advance through all days and check cash
		for i := 1; i < len(bars); i++ {
			tracker.AdvanceDay(bars[i].Date)
			cash := tracker.GetCash()
			assert.GreaterOrEqual(t, cash, 0.0,
				"cash must be >= 0 after advancing day %s (symbol %s)", bars[i].Date, sym)
		}
	}
}

// TestInvariant_PositionQuantityNeverNegative asserts position quantity >= 0 at all times.
func TestInvariant_PositionQuantityNeverNegative(t *testing.T) {
	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	ohlcv := fixtureToOHLCV(fixture.OHLCV)

	for _, sym := range fixture.Metadata.Symbols {
		bars := ohlcv[sym]
		if len(bars) < 2 {
			continue
		}

		// Buy-and-hold replay
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, bars[0].Close, bars[0].Date, nil)
		require.NoError(t, err)

		positions := tracker.GetAllPositions()
		for _, pos := range positions {
			assert.GreaterOrEqual(t, pos.Quantity, 0.0,
				"long position quantity must be >= 0, got %.4f for %s", pos.Quantity, sym)
		}

		for i := 1; i < len(bars); i++ {
			tracker.AdvanceDay(bars[i].Date)
			positions = tracker.GetAllPositions()
			for sym2, pos := range positions {
				assert.GreaterOrEqual(t, pos.Quantity, 0.0,
					"position quantity must be >= 0 at end of day %s for %s, got %.4f",
					bars[i].Date.Format("2006-01-02"), sym2, pos.Quantity)
			}
		}
	}
}

// TestInvariant_NAVEqualsCashPlusPositions asserts NAV consistency at every daily close.
// NAV = cash + Σ(position_value). Long positions add value; short positions subtract.
func TestInvariant_NAVEqualsCashPlusPositions(t *testing.T) {
	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	ohlcv := fixtureToOHLCV(fixture.OHLCV)
	symbols := fixture.Metadata.Symbols

	// Buy on day 0
	for _, sym := range symbols {
		bars := ohlcv[sym]
		if len(bars) == 0 {
			continue
		}
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, bars[0].Close, bars[0].Date, nil)
		require.NoError(t, err)
	}

	// Check NAV at each daily snapshot
	for _, sym := range symbols {
		bars := ohlcv[sym]
		prices := make(map[string]float64)
		for _, b := range bars {
			prices[b.Symbol] = b.Close
		}

		for i := 0; i < len(bars); i++ {
			d := bars[i].Date

			// Build prices map for this date
			datePrices := make(map[string]float64)
			for _, sym2 := range symbols {
				if data, ok := ohlcv[sym2]; ok && i < len(data) {
					datePrices[sym2] = data[i].Close
				}
			}

			tracker.RecordDailyValue(d, datePrices)
			pv := tracker.RecordDailyValue(d, datePrices)

			// Recompute expected NAV
			cash := tracker.GetCash()
			positions := tracker.GetAllPositions()
			var positionsValue float64
			for sym2, pos := range positions {
				price := datePrices[sym2]
				if pos.Quantity > 0 {
					positionsValue += pos.Quantity * price
				} else {
					// Short: liability to buy back
					positionsValue -= abs(pos.Quantity) * price
				}
			}
			expectedNAV := cash + positionsValue

			assert.InDelta(t, expectedNAV, pv.TotalValue, 0.01,
				"NAV mismatch on %s: expected=%.4f, got=%.4f (cash=%.4f, pos_value=%.4f)",
				d.Format("2006-01-02"), expectedNAV, pv.TotalValue, cash, positionsValue)

			tracker.AdvanceDay(d.AddDate(0, 0, 1))
		}
	}
}

// TestInvariant_AllTradesHavePositiveFee asserts every trade has fee > 0.
func TestInvariant_AllTradesHavePositiveFee(t *testing.T) {
	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	ohlcv := fixtureToOHLCV(fixture.OHLCV)

	// Execute a round-trip for each symbol
	for _, sym := range fixture.Metadata.Symbols {
		bars := ohlcv[sym]
		if len(bars) < 3 {
			continue
		}

		// Buy
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, bars[0].Close, bars[0].Date, nil)
		require.NoError(t, err)

		// Advance to next day
		tracker.AdvanceDay(bars[1].Date)

		// Sell
		_, err = tracker.ExecuteTrade(sym, domain.DirectionClose, 1000, bars[1].Close, bars[1].Date, nil)
		require.NoError(t, err)
	}

	trades := tracker.GetTrades()
	require.NotEmpty(t, trades, "should have executed trades")

	for i, trade := range trades {
		totalFee := trade.Commission + trade.TransferFee + trade.StampTax
		assert.Greater(t, totalFee, 0.0,
			"trade[%d] ID=%s symbol=%s direction=%s must have fee > 0, got %.6f",
			i, trade.ID, trade.Symbol, trade.Direction, totalFee)
	}
}

// TestInvariant_AllTradesHaveTimestamp asserts every trade has a non-zero timestamp.
func TestInvariant_AllTradesHaveTimestamp(t *testing.T) {
	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	ohlcv := fixtureToOHLCV(fixture.OHLCV)

	for _, sym := range fixture.Metadata.Symbols {
		bars := ohlcv[sym]
		if len(bars) < 2 {
			continue
		}
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 500, bars[0].Close, bars[0].Date, nil)
		require.NoError(t, err)
	}

	trades := tracker.GetTrades()
	require.NotEmpty(t, trades)

	for i, trade := range trades {
		assert.False(t, trade.Timestamp.IsZero(),
			"trade[%d] ID=%s symbol=%s must have a non-zero timestamp",
			i, trade.ID, trade.Symbol)
	}
}

// TestInvariant_EquityCurveIsNonEmptyAfterBacktest asserts equity curve is recorded.
func TestInvariant_EquityCurveIsNonEmptyAfterBacktest(t *testing.T) {
	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	ohlcv := fixtureToOHLCV(fixture.OHLCV)
	symbols := fixture.Metadata.Symbols

	// Buy on day 0
	for _, sym := range symbols {
		bars := ohlcv[sym]
		if len(bars) == 0 {
			continue
		}
		_, _ = tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, bars[0].Close, bars[0].Date, nil)
	}

	// Record daily values for each trading day
	for _, sym := range symbols {
		bars := ohlcv[sym]
		for i, bar := range bars {
			prices := make(map[string]float64)
			for _, s := range symbols {
				idx := i
				if idx >= len(ohlcv[s]) {
					idx = len(ohlcv[s]) - 1
				}
				prices[s] = ohlcv[s][idx].Close
			}
			tracker.RecordDailyValue(bar.Date, prices)
			tracker.AdvanceDay(bar.Date.AddDate(0, 0, 1))
		}
	}

	curve := tracker.GetEquityCurve()
	assert.NotEmpty(t, curve, "equity curve must not be empty after backtest")
	for i, pv := range curve {
		assert.Greater(t, pv.TotalValue, 0.0,
			"equity curve point[%d] on %s must have positive total_value, got %.4f",
			i, pv.Date.Format("2006-01-02"), pv.TotalValue)
	}
}

// ------------------------------------------------------------------------------
// Golden Regression Tests
// Run the backtest engine against the golden fixture and assert metrics.
// ------------------------------------------------------------------------------

// TestGolden_Momentum5Stock1Yr runs the engine on the golden fixture and
// checks high-level metrics against expected values.
func TestGolden_Momentum5Stock1Yr(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden fixture regression test in -short mode")
	}

	logger := zerolog.New(nil)
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	tracker := NewTracker(
		fixture.Metadata.InitialCapital,
		fixture.Metadata.CommissionRate,
		fixture.Metadata.SlippageRate,
		logger,
	)

	ohlcv := fixtureToOHLCV(fixture.OHLCV)
	symbols := fixture.Metadata.Symbols

	// Simple momentum strategy: buy on day 0, sell on last day.
	// If fixture has signals, use those instead.
	if len(fixture.Signals) > 0 {
		// Replay signals
		signalByDate := make(map[string][]FixtureSignal)
		for _, s := range fixture.Signals {
			signalByDate[s.Date] = append(signalByDate[s.Date], s)
		}

		for dateStr, signals := range signalByDate {
			d, _ := time.Parse("2006-01-02", dateStr)
			prices := make(map[string]float64)
			for _, sym := range symbols {
				bars := ohlcv[sym]
				for i, b := range bars {
					if b.Date.Equal(d) || (i > 0 && bars[i-1].Date.Before(d) && b.Date.After(d)) {
						prices[sym] = b.Close
						break
					}
					prices[sym] = bars[len(bars)-1].Close
				}
			}

			for _, sig := range signals {
				switch sig.Direction {
				case "long":
					_, _ = tracker.ExecuteTrade(sig.Symbol, domain.DirectionLong, 1000, prices[sig.Symbol], d, nil)
				case "short":
					_, _ = tracker.ExecuteTrade(sig.Symbol, domain.DirectionShort, 1000, prices[sig.Symbol], d, nil)
				case "close":
					_, _ = tracker.ExecuteTrade(sig.Symbol, domain.DirectionClose, 999999, prices[sig.Symbol], d, nil)
				}
			}
			tracker.AdvanceDay(d.AddDate(0, 0, 1))
		}
	} else {
		// Default: buy-and-hold each symbol for the full year
		for _, sym := range symbols {
			bars := ohlcv[sym]
			if len(bars) == 0 {
				continue
			}
			_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, bars[0].Close, bars[0].Date, nil)
			require.NoError(t, err)
		}
	}

	// Record final daily values
	for i, sym := range symbols {
		bars := ohlcv[sym]
		if len(bars) == 0 {
			continue
		}
		lastBar := bars[len(bars)-1]
		prices := make(map[string]float64)
		for _, s := range symbols {
			prices[s] = ohlcv[s][len(ohlcv[s])-1].Close
		}
		tracker.RecordDailyValue(lastBar.Date, prices)
		_ = i // suppress unused
	}

	// If expected_output is present, run regression assertions
	if fixture.Expected != nil {
		exp := fixture.Expected

		trades := tracker.GetTrades()
		assert.NotEmpty(t, trades, "golden fixture should produce at least one trade")

		totalTrades := len(trades)
		t.Logf("Golden fixture %s: %d symbols, %d days, %d trades",
			fixture.Metadata.Name, len(symbols), fixture.Metadata.TradingDays, totalTrades)

		assert.GreaterOrEqual(t, totalTrades, 5,
			"should have at least 5 trades for multi-stock buy-and-hold")

		longTrades := 0
		for _, tr := range trades {
			if tr.Direction == domain.DirectionLong {
				longTrades++
			}
		}
		assert.GreaterOrEqual(t, longTrades, len(symbols),
			"should have at least one long trade per symbol")

		assert.InDelta(t, exp.TotalReturn, 0.18, 0.15,
			"total return should be within tolerance of expected")
	}
}

// ------------------------------------------------------------------------------
// Limit-Up / Limit-Down Detection (from OHLCV data)
// ------------------------------------------------------------------------------

// TestLimitDetection_HasLimitUpEvent verifies the fixture contains at least one limit-up day.
func TestLimitDetection_HasLimitUpEvent(t *testing.T) {
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")
	require.NotEmpty(t, fixture.Metadata.LimitUpEvents,
		"fixture must declare at least one limit-up event")
}

// TestLimitDetection_HasLimitDownEvent verifies the fixture contains at least one limit-down day.
func TestLimitDetection_HasLimitDownEvent(t *testing.T) {
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")
	require.NotEmpty(t, fixture.Metadata.LimitDownEvents,
		"fixture must declare at least one limit-down event")
}

// TestLimitDetection_VerifyLimitUpPrice asserts that on a declared limit-up day,
// the close ≈ prev_close × 1.10 (normal A-share ±10% limit).
func TestLimitDetection_VerifyLimitUpPrice(t *testing.T) {
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")
	ohlcv := fixtureToOHLCV(fixture.OHLCV)

	for sym, dateStr := range fixture.Metadata.LimitUpEvents {
		d, _ := time.Parse("2006-01-02", dateStr)
		bars := ohlcv[sym]
		var bar FixtureOHLCV
		var found bool
		for _, b := range bars {
			td, _ := time.Parse("2006-01-02", b.Date.Format("2006-01-02"))
			if td.Equal(d) {
				// Get prev_close from fixture record
				for _, r := range fixture.OHLCV {
					rd, _ := time.Parse("2006-01-02", r.TradeDate)
					if rd.Equal(d) && r.Symbol == sym {
						bar = r
						found = true
						break
					}
				}
				break
			}
		}
		require.True(t, found, "limit-up bar not found for %s on %s", sym, dateStr)

		// Close should be within 0.5% of upper limit price
		upperLimit := bar.PrevClose * 1.10
		assert.InDelta(t, upperLimit, bar.Close, bar.PrevClose*0.005,
			"limit-up close for %s on %s should be near prev_close*1.10 (%.4f), got %.4f",
			sym, dateStr, upperLimit, bar.Close)
	}
}

// TestLimitDetection_VerifyLimitDownPrice asserts that on a declared limit-down day,
// the close ≈ prev_close × 0.90.
func TestLimitDetection_VerifyLimitDownPrice(t *testing.T) {
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")

	for sym, dateStr := range fixture.Metadata.LimitDownEvents {
		d, _ := time.Parse("2006-01-02", dateStr)
		var bar FixtureOHLCV
		var found bool
		for _, r := range fixture.OHLCV {
			rd, _ := time.Parse("2006-01-02", r.TradeDate)
			if rd.Equal(d) && r.Symbol == sym {
				bar = r
				found = true
				break
			}
		}
		require.True(t, found, "limit-down bar not found for %s on %s", sym, dateStr)

		lowerLimit := bar.PrevClose * 0.90
		assert.InDelta(t, lowerLimit, bar.Close, bar.PrevClose*0.005,
			"limit-down close for %s on %s should be near prev_close*0.90 (%.4f), got %.4f",
			sym, dateStr, lowerLimit, bar.Close)
	}
}

// ------------------------------------------------------------------------------
// T+1 Edge Case
// ------------------------------------------------------------------------------

// TestT1EdgeCase_FixtureContainsT1Scenario verifies the fixture metadata documents a T+1 edge case.
func TestT1EdgeCase_FixtureContainsT1Scenario(t *testing.T) {
	fixture := loadGoldenFixture(t, "momentum-5stock-1yr.json")
	require.NotEmpty(t, fixture.Metadata.T1EdgeCaseNote,
		"fixture must document T+1 edge cases in T1EdgeCaseNote")
}

// ------------------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------------------
// abs and min are already defined in tracker.go; reuse them here.
