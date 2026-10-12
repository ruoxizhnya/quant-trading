package main

// K8 验收正证据：「POST /api/signals 写入 → SignalStrategy 消费下单」e2e。
//
// 全链路：外部模型经 HTTP 写入门批量写入信号（httptest 真路由、真 JSON）→
// SignalStore（内存 fake，落库语义已在 pkg/strategy 真库覆盖）→
// SignalStrategy（Actor）按 as_of 拉取转成 domain.Signal → 引擎
// processSignalsAndExecuteTrades 下单成交（resp.Trades）。
//
// 与 pkg/backtest/signal_inject_e2e_test.go 的分工：那条从内存注入起步，
// 这条补上 HTTP 写入门这一段 —— 验收要求的是「POST 写入 → 消费下单」闭环。
//
// 引擎 fixture 是 pkg/backtest/engine_synthetic_bench_test.go
// buildSyntheticEngine 的精简复制（测试夹具不可跨包复用）。

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// memorySignalStore 是 SignalStore 的内存实现（正确兑现 as_of <= upTo
// 契约）。真库 PGSignalStore 已在 pkg/strategy 覆盖，这里只为把
// 「HTTP 写入 → 消费下单」链路拆出来、不依赖 DB。
type memorySignalStore struct {
	signals []strategy.ExternalSignal
}

func (f *memorySignalStore) Save(_ context.Context, sig strategy.ExternalSignal) error {
	f.signals = append(f.signals, sig)
	return nil
}

func (f *memorySignalStore) ListFor(_ context.Context, modelID, symbol string, upTo time.Time) ([]strategy.ExternalSignal, error) {
	var out []strategy.ExternalSignal
	for _, s := range f.signals {
		if s.ModelID != modelID || s.Symbol != symbol {
			continue
		}
		if s.AsOf.After(upTo) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AsOf.Before(out[j].AsOf) })
	return out, nil
}

// buildSignalsE2EEngine 精简版合成引擎 fixture（3 股 × 30 日、确定性缓涨
// 价格、无涨停路径）。viper 键与 buildSyntheticEngine 一致。
func buildSignalsE2EEngine(t *testing.T, symbols []string, days int) *backtest.Engine {
	t.Helper()
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	data := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars := make([]domain.OHLCV, 0, days)
		price := 10.0
		for d := 0; d < days; d++ {
			open := price
			closePx := open * 1.005
			bars = append(bars, domain.OHLCV{
				Symbol:   sym,
				Date:     start.AddDate(0, 0, d),
				Open:     open,
				High:     closePx * 1.001,
				Low:      open * 0.999,
				Close:    closePx,
				Volume:   1_000_000,
				Turnover: (open + closePx) / 2 * 1_000_000,
			})
			price = closePx
		}
		data[sym] = bars
	}

	prov := marketdata.NewInMemoryProvider()
	for sym, bars := range data {
		prov.LoadOHLCV(sym, bars)
	}
	tradingDays := make([]time.Time, 0, days)
	for d := 0; d < days; d++ {
		tradingDays = append(tradingDays, start.AddDate(0, 0, d))
	}
	prov.SetTradingDays(tradingDays)
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
	v.Set("backtest.initial_capital", 1_000_000.0)
	v.Set("backtest.commission_rate", fees.DefaultCommissionRate)
	v.Set("backtest.slippage_rate", fees.DefaultSlippageRate)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 42)
	v.Set("strategy_service.url", "http://localhost:8082")

	eng, err := backtest.NewEngine(v, prov, logging.WithContext(map[string]any{}))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	eng.LoadOHLCVInMemory(data)

	// 风控必须装配：rm 缺省时 calculatePosition 回退外部 risk-service HTTP
	// 调用（测试环境无此服务 → 连接超时、0 成交 → OBS-01 zero_trades invalid）。
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
		t.Fatalf("risk.NewRiskManager: %v", err)
	}
	eng.SetRiskManager(rm)
	return eng
}

// TestSignalsE2E_PostDrivesOrders 是 K8 验收的核心正证据：外部模型经
// POST /api/signals 写入 → SignalStrategy 消费 → 引擎真的下单成交。
func TestSignalsE2E_PostDrivesOrders(t *testing.T) {
	symbols := []string{"600001.SH", "600002.SH", "600003.SH"}
	eng := buildSignalsE2EEngine(t, symbols, 30)

	// 同一内存 store 既是 HTTP 写入门的落点，也是 SignalStrategy 的读源。
	store := &memorySignalStore{}
	// gin 的 mode 由 TestMain 设一次（AUD-12）—— 这里不调 gin.SetMode。
	router := gin.New()
	NewSignalsHandler(store, zerolog.Nop()).RegisterRoutes(router)

	// 外部模型经 HTTP 批量写入：buy（窗口内）+ close（更晚）。
	body, err := json.Marshal(map[string]any{"signals": []map[string]any{
		{"model_id": "ml-model", "symbol": symbols[0], "direction": "long",
			"strength": 0.8, "as_of": "2024-01-10T00:00:00Z"},
		{"model_id": "ml-model", "symbol": symbols[0], "direction": "close",
			"strength": 0.5, "as_of": "2024-01-15T00:00:00Z"},
	}})
	if err != nil {
		t.Fatalf("请求体构造失败：%v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/signals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /api/signals 应 201，got %d body=%s", w.Code, w.Body.String())
	}
	if len(store.signals) != 2 {
		t.Fatalf("写入门落库条数漂移：%d, want 2", len(store.signals))
	}

	s := strategy.NewSignalStrategy("k8_http_e2e_probe", "ml-model", store)
	if err := strategy.GlobalRegister(s); err != nil {
		t.Fatalf("注册 SignalStrategy 失败：%v", err)
	}

	resp, err := eng.RunBacktest(context.Background(), backtest.BacktestRequest{
		Strategy:       "k8_http_e2e_probe",
		StockPool:      symbols,
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-31",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("回测失败：%v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("回测未完成：status=%s err=%s", resp.Status, resp.Error)
	}
	// 成交层正证据：HTTP 写入的信号真的驱动了下单。
	if resp.TotalTrades == 0 {
		t.Fatal("HTTP 写入的信号未驱动任何成交 —— 「写入 → 消费下单」链路断了")
	}
	for _, tr := range resp.Trades {
		if tr.Symbol != symbols[0] {
			t.Fatalf("成交 symbol 漂移：%s, want %s", tr.Symbol, symbols[0])
		}
	}
	t.Logf("HTTP 写入 → 消费下单闭环：%d 条信号落库，%d 笔成交，全部落在 %s",
		len(store.signals), resp.TotalTrades, symbols[0])
}
