package backtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins" // registers momentum strategy via init()
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureData holds the loaded momentum-5stock-1yr.json fixture.
type fixtureData struct {
	Metadata struct {
		Name        string   `json:"name"`
		Symbols     []string `json:"symbols"`
		TradingDays int      `json:"trading_days"`
		StartDate   string   `json:"start_date"`
		EndDate     string   `json:"end_date"`
	} `json:"metadata"`
	OHLCV []struct {
		TradeDate string  `json:"trade_date"`
		Symbol    string  `json:"symbol"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Close     float64 `json:"close"`
		Volume    float64 `json:"volume"`
		PrevClose float64 `json:"prev_close"`
		LimitUp   bool    `json:"limit_up"`
		LimitDown bool    `json:"limit_down"`
	} `json:"ohlcv"`
}

// loadBenchmarkFixture loads the 5stock-1yr fixture.
func loadBenchmarkFixture() *fixtureData {
	paths := []string{
		"../../testdata/momentum-5stock-1yr.json",
		"/Users/ruoxi/longshaosWorld/quant-trading/testdata/momentum-5stock-1yr.json",
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			var fd fixtureData
			if err := json.Unmarshal(data, &fd); err == nil {
				return &fd
			}
		}
	}
	return nil
}

// ohlcvBar is the type served by the mock data service.
type ohlcvBar struct {
	TradeDate string  `json:"trade_date"`
	Symbol    string  `json:"symbol"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	PrevClose float64 `json:"prev_close"`
	LimitUp   bool    `json:"limit_up"`
	LimitDown bool    `json:"limit_down"`
}

// buildFixtureHandler returns an HTTP handler that serves fixture data.
func buildFixtureHandler(f *fixtureData) http.HandlerFunc {
	ohlcvBySymbol := make(map[string][]ohlcvBar)
	for _, b := range f.OHLCV {
		ohlcvBySymbol[b.Symbol] = append(ohlcvBySymbol[b.Symbol], ohlcvBar{
			TradeDate: b.TradeDate, Symbol: b.Symbol,
			Open: b.Open, High: b.High, Low: b.Low,
			Close: b.Close, Volume: b.Volume, PrevClose: b.PrevClose,
			LimitUp: b.LimitUp, LimitDown: b.LimitDown,
		})
	}
	dateSet := make(map[string]bool)
	for _, b := range f.OHLCV {
		dateSet[b.TradeDate] = true
	}
	var dates []string
	for d := range dateSet {
		dates = append(dates, d)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/trading/calendar":
			start := r.URL.Query().Get("start")
			end := r.URL.Query().Get("end")
			var filtered []string
			for _, d := range dates {
				if d >= start && d <= end {
					filtered = append(filtered, d)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"trading_days": filtered})

		case "/api/v1/cache/warm":
			w.WriteHeader(http.StatusOK)

		default:
			switch {
			case len(r.URL.Path) > 6 && r.URL.Path[:6] == "/ohlcv/":
				symbol := r.URL.Path[6:]
				if bars, ok := ohlcvBySymbol[symbol]; ok {
					_ = json.NewEncoder(w).Encode(map[string]any{"ohlcv": bars})
					return
				}
				http.NotFound(w, r)

			case len(r.URL.Path) > 18 && r.URL.Path[:18] == "/api/v1/stocks/":
				symbol := r.URL.Path[18:]
				_ = json.NewEncoder(w).Encode(map[string]any{"stock": domain.Stock{
					Symbol:   symbol,
					Name:     "测试股票",
					ListDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				}})

			default:
				http.NotFound(w, r)
			}
		}
	}
}

// BenchmarkBacktest measures the 5-stock, 1-year backtest performance.
//
// Per-backtest cost (baseline without in-memory cache):
//   - warmCache: 1 HTTP POST
//   - getTradingDays: 1 HTTP GET
//   - Per trading day (252 days):
//     a. getOHLCV × 5 stocks  (5 HTTP GETs per day → 1,260 total)
//     b. detectRegime × 1/day  (252 HTTP POSTs)
//     c. getSignals (local registry → 0 HTTP)
//     d. calculatePosition × M signals (M HTTP POSTs per day)
//     e. checkStopLosses × 1/day (252 HTTP POSTs)
//
// With in-memory cache + parallel workers: all getOHLCV calls are instant.
func BenchmarkBacktest(b *testing.B) {
	fixture := loadBenchmarkFixture()
	if fixture == nil {
		b.Fatal("failed to load momentum-5stock-1yr fixture")
	}
	b.Logf("Fixture: %s, symbols=%v, days=%d",
		fixture.Metadata.Name, fixture.Metadata.Symbols, fixture.Metadata.TradingDays)

	// Pre-load OHLCV into the engine's in-memory cache.
	// This makes getOHLCV serve from memory instead of making HTTP calls.
	inMemoryOHLCV := make(map[string][]domain.OHLCV, len(fixture.Metadata.Symbols))
	for _, b := range fixture.OHLCV {
		t, _ := time.Parse("2006-01-02", b.TradeDate)
		inMemoryOHLCV[b.Symbol] = append(inMemoryOHLCV[b.Symbol], domain.OHLCV{
			Symbol:    b.Symbol,
			Date:      t,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
			LimitUp:   b.LimitUp,
			LimitDown: b.LimitDown,
		})
	}

	dataServer := httptest.NewServer(buildFixtureHandler(fixture))
	defer dataServer.Close()

	stratServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"signals": []domain.Signal{}})
	}))
	defer stratServer.Close()

	riskServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/risk/regime":
			_ = json.NewEncoder(w).Encode(domain.MarketRegime{
				Trend: "sideways", Volatility: "medium", Sentiment: 0.0, Timestamp: time.Now(),
			})
		case "/calculate_position":
			_ = json.NewEncoder(w).Encode(domain.PositionSize{Size: 1000, Weight: 0.02})
		case "/api/v1/risk/stoploss":
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer riskServer.Close()

	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 42)
	v.Set("data_service.url", dataServer.URL)
	v.Set("strategy_service.url", stratServer.URL)
	v.Set("risk_service.url", riskServer.URL)

	logger := zerolog.New(zerolog.NewTestWriter(b))
	eng, err := NewEngine(v, logger)
	if err != nil {
		b.Fatalf("failed to create engine: %v", err)
	}

	// Populate in-memory cache: getOHLCV now returns instantly.
	eng.LoadOHLCVInMemory(inMemoryOHLCV)
	// 8 parallel workers for per-stock data fetching within each day.
	eng.SetParallelWorkers(8)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.RunBacktest(ctx, BacktestRequest{
			Strategy:       "momentum",
			StockPool:      fixture.Metadata.Symbols,
			StartDate:      fixture.Metadata.StartDate,
			EndDate:        fixture.Metadata.EndDate,
			InitialCapital: 1000000.0,
			RiskFreeRate:   0.03,
		})
		if err != nil {
			b.Fatalf("backtest failed: %v", err)
		}
	}
}

// TestBenchmarkSmoke verifies the benchmark setup produces a valid completed backtest.
func TestBenchmarkSmoke(t *testing.T) {
	fixture := loadBenchmarkFixture()
	require.NotNil(t, fixture, "fixture must load")
	require.NotEmpty(t, fixture.Metadata.Symbols)
	require.Greater(t, fixture.Metadata.TradingDays, 0)

	// Build in-memory OHLCV cache from fixture.
	inMemoryOHLCV := make(map[string][]domain.OHLCV, len(fixture.Metadata.Symbols))
	for _, b := range fixture.OHLCV {
		tm, _ := time.Parse("2006-01-02", b.TradeDate)
		inMemoryOHLCV[b.Symbol] = append(inMemoryOHLCV[b.Symbol], domain.OHLCV{
			Symbol: b.Symbol, Date: tm,
			Open: b.Open, High: b.High, Low: b.Low,
			Close: b.Close, Volume: b.Volume,
			LimitUp: b.LimitUp, LimitDown: b.LimitDown,
		})
	}

	dataServer := httptest.NewServer(buildFixtureHandler(fixture))
	defer dataServer.Close()
	stratServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"signals": []domain.Signal{}})
	}))
	defer stratServer.Close()
	riskServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/risk/regime":
			_ = json.NewEncoder(w).Encode(domain.MarketRegime{
				Trend: "sideways", Volatility: "medium", Sentiment: 0.0, Timestamp: time.Now(),
			})
		case "/calculate_position":
			_ = json.NewEncoder(w).Encode(domain.PositionSize{Size: 1000, Weight: 0.02})
		case "/api/v1/risk/stoploss":
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer riskServer.Close()

	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 42)
	v.Set("data_service.url", dataServer.URL)
	v.Set("strategy_service.url", stratServer.URL)
	v.Set("risk_service.url", riskServer.URL)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	eng, err := NewEngine(v, logger)
	require.NoError(t, err)
	eng.LoadOHLCVInMemory(inMemoryOHLCV)
	eng.SetParallelWorkers(4)

	ctx := context.Background()
	resp, err := eng.RunBacktest(ctx, BacktestRequest{
		Strategy:       "momentum",
		StockPool:      fixture.Metadata.Symbols,
		StartDate:      fixture.Metadata.StartDate,
		EndDate:        fixture.Metadata.EndDate,
		InitialCapital: 1000000.0,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", resp.Status)
	t.Logf("Backtest completed: total_return=%.4f, sharpe=%.2f, trades=%d",
		resp.TotalReturn, resp.SharpeRatio, resp.TotalTrades)
}
