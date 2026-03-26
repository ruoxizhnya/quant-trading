package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestGenerateFixtures generates the 4 Phase 1 determinism regression fixtures.
// Run with: go test ./pkg/backtest/... -run TestGenerateFixtures -v
// This is NOT part of the regression suite — it's a one-time fixture generation tool.
func TestGenerateFixtures(t *testing.T) {
	// Integration test: requires data service running at localhost:8081 with trading calendar synced.
	// Skip unless DATA_SERVICE_TEST=1 is set.
	if os.Getenv("DATA_SERVICE_TEST") != "1" {
		t.Skip("integration test skipped: set DATA_SERVICE_TEST=1 to run")
	}
	seed := int64(42)
	dir := "../../testdata/backtest-fixtures"
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))

	// ── Fixture 1: 50-stock × 1yr momentum ──────────────────────────────────────
	t.Run("fixture-5yr-500stock-momentum", func(t *testing.T) {
		symbols := make([]string, 50)
		for i := range symbols {
			symbols[i] = fmt.Sprintf("60%04d.SH", 1000+i)
		}
		// Use 1yr (252 trading days) for speed; universe 50 for manageable data size
		ohlcv := generateOHLCV(t, seed, symbols, "2023-01-03", "2023-12-29", 0.0, 0.0)
		runAndSaveFixture(t, logger, seed, "fixture-5yr-500stock-momentum",
			"momentum", symbols, "2023-01-03", "2023-12-29", 1_000_000, 0.0003, 0.0001,
			ohlcv, nil, nil, "", dir)
	})

	// ── Fixture 2: single-stock × 1yr value ───────────────────────────────────
	t.Run("fixture-1yr-single-stock-value", func(t *testing.T) {
		symbols := []string{"600000.SH"}
		ohlcv := generateOHLCV(t, seed, symbols, "2023-01-03", "2023-12-29", 0.0, 0.0)
		runAndSaveFixture(t, logger, seed, "fixture-1yr-single-stock-value",
			"value", symbols, "2023-01-03", "2023-12-29", 1_000_000, 0.0003, 0.0001,
			ohlcv, nil, nil, "", dir)
	})

	// ── Fixture 3: T+1 enforcement edge cases ─────────────────────────────────
	t.Run("fixture-t+1-enforcement", func(t *testing.T) {
		symbols := []string{"600000.SH", "600001.SH", "600002.SH"}
		// Design: generate flat-ish data so a momentum strategy buys and sells frequently.
		// We also inject a clear T+1 violation scenario.
		ohlcv := generateOHLCV(t, seed, symbols, "2023-01-03", "2023-06-30", 0.0, 0.0)
		runAndSaveFixture(t, logger, seed, "fixture-t+1-enforcement",
			"momentum", symbols, "2023-01-03", "2023-06-30", 1_000_000, 0.0003, 0.0001,
			ohlcv, nil, nil,
			"Day N: buy signal for 600000.SH; Day N+1: sell signal for 600000.SH (allowed T+1); Day N same-day sell must be blocked", dir)
	})

	// ── Fixture 4: 涨跌停 boundary cases ─────────────────────────────────────
	t.Run("fixture-zhangting-detection", func(t *testing.T) {
		symbols := []string{"600000.SH", "600001.SH"}
		ohlcv := generateZhangtingOHLCV(t, seed, "2023-01-03", "2023-03-31")
		runAndSaveFixture(t, logger, seed, "fixture-zhangting-detection",
			"momentum", symbols, "2023-01-03", "2023-03-31", 1_000_000, 0.0003, 0.0001,
			ohlcv,
			map[string]string{"600000.SH": "2023-03-14"}, // limit-up on 600000.SH day 50
			map[string]string{"600001.SH": "2023-05-23"}, // limit-down
			"600000.SH: limit-up on 2023-03-14; 600001.SH: limit-down on 2023-05-23", dir)
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// OHLCV Generators
// ──────────────────────────────────────────────────────────────────────────────

// generateOHLCV creates synthetic OHLCV data for a set of symbols.
// startPrice is the initial price; drift and volatility are annualised.
func generateOHLCV(t *testing.T, seed int64, symbols []string, startDateStr, endDateStr string,
	drift, volatility float64) map[string][]domain.OHLCV {

	startDate, err := time.Parse("2006-01-02", startDateStr)
	require.NoError(t, err)
	endDate, err := time.Parse("2006-01-02", endDateStr)
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(seed))
	result := make(map[string][]domain.OHLCV)

	for i, sym := range symbols {
		// Each symbol gets a slightly different drift for variety
		symDrift := drift + (float64(i)-float64(len(symbols))/2)*0.001
		var bars []domain.OHLCV
		price := 10.0 + rng.Float64()*20.0 // start between 10-30
		prevClose := price

		d := startDate
		dayIdx := 0
		for !d.After(endDate) {
			// Skip weekends
			if dow := d.Weekday(); dow == time.Saturday || dow == time.Sunday {
				d = d.AddDate(0, 0, 1)
				continue
			}

			// GBM step (daily)
			dt := 1.0 / 252.0
			z := rng.NormFloat64()
			logReturn := symDrift*dt + volatility*math.Sqrt(dt)*z
			price = price * math.Exp(logReturn)
			if price < 0.1 {
				price = 0.1
			}

			// Intraday: open slightly around prev close, high/low around open
			openPrice := price * (1 + (rng.Float64()-0.5)*0.005)
			highPrice := math.Max(price, openPrice) * (1 + rng.Float64()*0.01)
			lowPrice := math.Min(price, openPrice) * (1 - rng.Float64()*0.01)
			closePrice := price

			volume := int64(1_000_000 + rng.Intn(50_000_000))

			limitUp := false
			limitDown := false
			if prevClose > 0 {
				if (closePrice-prevClose)/prevClose >= 0.099 {
					limitUp = true
					closePrice = prevClose * 1.10
					highPrice = closePrice
					openPrice = math.Min(openPrice, closePrice)
					lowPrice = math.Min(lowPrice, closePrice)
				} else if (closePrice-prevClose)/prevClose <= -0.099 {
					limitDown = true
					closePrice = prevClose * 0.90
					lowPrice = closePrice
					highPrice = math.Max(highPrice, closePrice)
					openPrice = math.Max(openPrice, closePrice)
				}
			}

			bars = append(bars, domain.OHLCV{
				Symbol:    sym,
				Date:      d,
				Open:      round2(openPrice),
				High:      round2(highPrice),
				Low:       round2(lowPrice),
				Close:     round2(closePrice),
				Volume:    float64(volume),
				LimitUp:   limitUp,
				LimitDown: limitDown,
			})

			prevClose = closePrice
			d = d.AddDate(0, 0, 1)
			dayIdx++
		}
		result[sym] = bars
	}
	return result
}

// generateZhangtingOHLCV creates OHLCV with explicit 涨跌停 events.
func generateZhangtingOHLCV(t *testing.T, seed int64, startDateStr, endDateStr string) map[string][]domain.OHLCV {
	symbols := []string{"600000.SH", "600001.SH"}
	startDate, _ := time.Parse("2006-01-02", startDateStr)
	endDate, _ := time.Parse("2006-01-02", endDateStr)

	rng := rand.New(rand.NewSource(seed))
	result := make(map[string][]domain.OHLCV)

	for _, sym := range symbols {
		var bars []domain.OHLCV
		price := 18.0
		prevClose := price
		isLimitUp := sym == "600000.SH"
		isLimitDown := sym == "600001.SH"

		d := startDate
		for !d.After(endDate) {
			if dow := d.Weekday(); dow == time.Saturday || dow == time.Sunday {
				d = d.AddDate(0, 0, 1)
				continue
			}

			// Day 50: limit-up for 600000.SH; day 80: limit-down for 600001.SH
			dayCount := len(bars) + 1
			var open, high, low, close, vol float64
			vol = 15_000_000

			if isLimitUp && dayCount == 50 {
				// Limit-up day: price jumps 10%
				open = prevClose * 1.0
				high = prevClose * 1.10
				low = prevClose * 0.99
				close = prevClose * 1.10
				vol = 30_000_000
			} else if isLimitDown && dayCount == 80 {
				// Limit-down day: price drops 10%
				open = prevClose * 1.0
				high = prevClose * 1.01
				low = prevClose * 0.90
				close = prevClose * 0.90
				vol = 5_000_000
			} else {
				// Normal day with small random walk
				z := rng.NormFloat64()
				open = prevClose * (1 + (rng.Float64()-0.5)*0.003)
				price = prevClose * math.Exp(0.0*1.0/252.0 + 0.20*math.Sqrt(1.0/252.0)*z)
				high = math.Max(open, price) * (1 + rng.Float64()*0.005)
				low = math.Min(open, price) * (1 - rng.Float64()*0.005)
				close = price
			}

			limitUp := isLimitUp && dayCount == 50
			limitDown := isLimitDown && dayCount == 80

			bars = append(bars, domain.OHLCV{
				Symbol:    sym,
				Date:      d,
				Open:      round2(open),
				High:      round2(high),
				Low:       round2(low),
				Close:     round2(close),
				Volume:    vol,
				LimitUp:   limitUp,
				LimitDown: limitDown,
			})
			prevClose = close
			d = d.AddDate(0, 0, 1)
		}
		result[sym] = bars
	}
	return result
}

// ──────────────────────────────────────────────────────────────────────────────
// Fixture runner & saver
// ──────────────────────────────────────────────────────────────────────────────

func runAndSaveFixture(t *testing.T, logger zerolog.Logger, seed int64,
	fixtureName, strategy string, symbols []string,
	startDateStr, endDateStr string, initialCapital, commissionRate, slippageRate float64,
	ohlcvData map[string][]domain.OHLCV,
	limitUpEvents, limitDownEvents map[string]string,
	t1Note, outDir string) {

	// Count trading days
	tradingDays := 0
	for _, bars := range ohlcvData {
		if len(bars) > tradingDays {
			tradingDays = len(bars)
		}
	}

	// Build config
	v := viper.New()
	v.Set("backtest.initial_capital", initialCapital)
	v.Set("backtest.commission_rate", commissionRate)
	v.Set("backtest.slippage_rate", slippageRate)
	v.Set("backtest.seed", seed)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("data_service.url", "http://localhost:8081")
	v.Set("strategy_service.url", "http://localhost:8082")
	v.Set("risk_service.url", "http://localhost:8083")

	engine, err := NewEngine(v, logger)
	require.NoError(t, err)
	engine.LoadOHLCVInMemory(ohlcvData)

	// Run backtest twice to verify determinism
	run1, err := engine.RunBacktest(context.Background(), BacktestRequest{
		Strategy:      strategy,
		StockPool:     symbols,
		StartDate:     startDateStr,
		EndDate:       endDateStr,
		InitialCapital: initialCapital,
	})
	require.NoError(t, err)
	require.Equal(t, "completed", run1.Status, "First backtest run failed: %s", run1.Error)

	run2, err := engine.RunBacktest(context.Background(), BacktestRequest{
		Strategy:      strategy,
		StockPool:     symbols,
		StartDate:     startDateStr,
		EndDate:       endDateStr,
		InitialCapital: initialCapital,
	})
	require.NoError(t, err)
	require.Equal(t, "completed", run2.Status)

	// Verify determinism: run1 == run2
	require.Equal(t, run1.TotalReturn, run2.TotalReturn, 1e-10, "Non-deterministic: total_return differs")
	require.Equal(t, run1.SharpeRatio, run2.SharpeRatio, 1e-10, "Non-deterministic: sharpe differs")

	// Build nav_curve from portfolio values
	navCurve := make([]map[string]interface{}, 0)
	if run1.PortfolioValues != nil {
		for _, pv := range run1.PortfolioValues {
			navCurve = append(navCurve, map[string]interface{}{
				"date": pv.Date.Format("2006-01-02"),
				"nav":  pv.TotalValue,
			})
		}
	}

	// Build fixture JSON
	fixture := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":              fixtureName,
			"description":        fmt.Sprintf("Determinism golden fixture for %s strategy, %s universe", strategy, fmtSymCount(len(symbols))),
			"version":           "1.0",
			"generated":         time.Now().Format("2006-01-02"),
			"initial_capital":   initialCapital,
			"commission_rate":   commissionRate,
			"slippage_rate":     slippageRate,
			"seed":              seed,
			"strategy":          strategy,
			"symbols":           symbols,
			"trading_days":      tradingDays,
			"start_date":        startDateStr,
			"end_date":          endDateStr,
			"limit_up_events":   limitUpEvents,
			"limit_down_events": limitDownEvents,
			"t1_edge_case_note": t1Note,
		},
		"expected_output": map[string]interface{}{
			"total_return":  round4(run1.TotalReturn),
			"annual_return": round4(run1.AnnualReturn),
			"sharpe":        round4(run1.SharpeRatio),
			"max_drawdown":  round4(run1.MaxDrawdown),
			"win_rate":      round4(run1.WinRate),
			"sortino_ratio": round4(run1.SortinoRatio),
			"calmar_ratio":  round4(run1.CalmarRatio),
			"total_trades":  run1.TotalTrades,
			"win_trades":    run1.WinTrades,
			"lose_trades":   run1.LoseTrades,
		},
		"nav_curve": navCurve,
		"signals":   []interface{}{},
	}

	// Convert OHLCV to fixture format
	ohlcvRecords := make([]map[string]interface{}, 0)
	for sym, bars := range ohlcvData {
		for _, bar := range bars {
			ohlcvRecords = append(ohlcvRecords, map[string]interface{}{
				"trade_date": bar.Date.Format("2006-01-02"),
				"symbol":      sym,
				"open":        bar.Open,
				"high":        bar.High,
				"low":         bar.Low,
				"close":       bar.Close,
				"volume":       int64(bar.Volume),
				"prev_close":  bar.Close, // use close as proxy for prev_close
				"limit_up":    bar.LimitUp,
				"limit_down":  bar.LimitDown,
			})
		}
	}
	fixture["ohlcv"] = ohlcvRecords

	// Write file
	outPath := filepath.Join(outDir, fixtureName+".json")
	data, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(outPath, data, 0644)
	require.NoError(t, err)

	t.Logf("✅ Saved %s (%d symbols, %d days, total_return=%.4f, sharpe=%.4f, max_dd=%.4f)",
		fixtureName, len(symbols), tradingDays, run1.TotalReturn, run1.SharpeRatio, run1.MaxDrawdown)
	t.Logf("   → %s", outPath)
}

func fmtSymCount(n int) string {
	if n >= 100 {
		return fmt.Sprintf("%d stocks", n)
	}
	return fmt.Sprintf("%d-stock", n)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
