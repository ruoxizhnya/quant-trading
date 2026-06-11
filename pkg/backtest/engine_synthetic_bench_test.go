package backtest

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/spf13/viper"
)

// synthOHLCV generates deterministic synthetic OHLCV data for benchmarking.
// Realistic GBM-like price walk + alternating limit-up/limit-down to exercise
// the limit-up, T+1, and regime-detection code paths.
func synthOHLCV(symbols []string, days int, start time.Time) map[string][]domain.OHLCV {
	rng := rand.New(rand.NewSource(42))
	out := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars := make([]domain.OHLCV, 0, days)
		price := 10.0 + rng.Float64()*90.0 // start in [10, 100]
		for d := 0; d < days; d++ {
			date := start.AddDate(0, 0, d)
			drift := (rng.Float64() - 0.49) * 0.04 // slight upward drift
			open := price
			close := open * (1 + drift)
			high := math.Max(open, close) * (1 + rng.Float64()*0.01)
			low := math.Min(open, close) * (1 - rng.Float64()*0.01)
			vol := float64(1_000_000 + rng.Int63n(5_000_000))
			turnover := (open + close) / 2 * vol

			// 5% chance of limit-up to exercise that code path
			limitUp := rng.Float64() < 0.05
			if limitUp {
				close = math.Round(open*1.10*100) / 100
				high = close
			}

			bars = append(bars, domain.OHLCV{
				Symbol: sym,
				Date:   date,
				Open:   open,
				High:   high,
				Low:    low,
				Close:  close,
				Volume: vol,
				Turnover: turnover,
				LimitUp: limitUp,
			})
			price = close
		}
		out[sym] = bars
	}
	return out
}

// buildSyntheticEngine creates an engine wired to an in-memory provider pre-loaded
// with synthetic data. This lets us benchmark engine computation cost in isolation
// (no DB, no HTTP). Use sizing parameters to model different scenarios.
func buildSyntheticEngine(b testing.TB, numSymbols, numDays int, workers int) (*Engine, marketdata.Provider, []string) {
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	symbols := make([]string, numSymbols)
	for i := 0; i < numSymbols; i++ {
		symbols[i] = fmt.Sprintf("%06d.SH", 600000+i)
	}

	data := synthOHLCV(symbols, numDays, start)

	prov := marketdata.NewInMemoryProvider()
	for sym, bars := range data {
		prov.LoadOHLCV(sym, bars)
	}
	// Trading days = one per day
	days := make([]time.Time, 0, numDays)
	for d := 0; d < numDays; d++ {
		days = append(days, start.AddDate(0, 0, d))
	}
	prov.SetTradingDays(days)
	// Stocks metadata (needed by some strategies for list_date)
	stocks := make([]domain.Stock, 0, len(symbols))
	for _, sym := range symbols {
		stocks = append(stocks, domain.Stock{
			Symbol:   sym,
			Name:     "STK-" + sym,
			ListDate: start.AddDate(-1, 0, 0),
		})
	}
	prov.LoadStocks(stocks)

	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	// Sprint 6 P1-22 (ODR-013): pull fee constants from the
	// unified pkg/fees instead of inlining 0.0003 / 0.0001
	// here. Keeps the bench fixture in lock-step with the
	// regulatory default after a CSRC change.
	v.Set("backtest.commission_rate", fees.DefaultCommissionRate)
	v.Set("backtest.slippage_rate", fees.DefaultSlippageRate)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 42)
	v.Set("strategy_service.url", "http://localhost:8082")

	logger := logging.WithContext(map[string]any{})
	eng, err := NewEngine(v, prov, logger)
	if err != nil {
		b.Fatalf("NewEngine: %v", err)
	}
	eng.LoadOHLCVInMemory(data)
	eng.SetParallelWorkers(workers)

	rm, err := risk.NewRiskManager(risk.RiskManagerConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
		ATRPeriod:           14,
		BaseMultiplier:      2.0,
		BullMultiplier:      1.5,
		BearMultiplier:      3.0,
		SidewaysMultiplier:  2.0,
		TakeProfitMult:      3.0,
		VolLookbackDays:     60,
		AnnualizationFactor: math.Sqrt(252),
		FastMAPeriod:        50,
		SlowMAPeriod:        200,
		RegimeVolLookback:   120,
	}, zerolog.Nop())
	if err != nil {
		b.Fatalf("risk.NewRiskManager: %v", err)
	}
	eng.SetRiskManager(rm)
	return eng, prov, symbols
}

// runSynthetic is a helper that executes a backtest and reports the per-run cost.
func runSynthetic(b *testing.B, numSymbols, numDays int, workers int) {
	ctx := context.Background()
	eng, _, symbols := buildSyntheticEngine(b, numSymbols, numDays, workers)

	req := BacktestRequest{
		Strategy:       "momentum",
		StockPool:      symbols,
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-01",
		InitialCapital: 1000000.0,
		RiskFreeRate:   0.03,
	}
	if numDays > 0 {
		req.EndDate = time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).
			AddDate(0, 0, numDays-1).Format("2006-01-02")
	}

	// Warm up
	resp, err := eng.RunBacktest(ctx, req)
	if err != nil {
		b.Skipf("warm-up skipped: %v", err)
	}
	b.Logf("Warm-up: ret=%.4f sharpe=%.2f trades=%d days=%d symbols=%d",
		resp.TotalReturn, resp.SharpeRatio, resp.TotalTrades, numDays, numSymbols)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err = eng.RunBacktest(ctx, req)
		if err != nil {
			b.Fatalf("backtest failed: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(numSymbols), "symbols")
	b.ReportMetric(float64(numDays), "days")
	b.ReportMetric(float64(numSymbols*numDays), "bars")
	b.ReportMetric(float64(workers), "workers")
	if b.N > 0 && resp != nil {
		b.ReportMetric(resp.TotalReturn, "total_return")
	}
}

// --- Benchmark matrix ---
// Sizes chosen to model real A-share backtests:
//   50 stocks × 240 days = 12,000 bars (1y daily)  ← typical single strategy
//   100 stocks × 480 days = 48,000 bars (2y daily) ← walk-forward window
//   200 stocks × 240 days = 48,000 bars (large universe)

func BenchmarkEngineSynthetic_50x240(b *testing.B)   { runSynthetic(b, 50, 240, 1) }
func BenchmarkEngineSynthetic_50x240_W8(b *testing.B) { runSynthetic(b, 50, 240, 8) }

func BenchmarkEngineSynthetic_100x480(b *testing.B)    { runSynthetic(b, 100, 480, 1) }
func BenchmarkEngineSynthetic_100x480_W8(b *testing.B) { runSynthetic(b, 100, 480, 8) }

func BenchmarkEngineSynthetic_200x240(b *testing.B)    { runSynthetic(b, 200, 240, 1) }
func BenchmarkEngineSynthetic_200x240_W8(b *testing.B) { runSynthetic(b, 200, 240, 8) }
