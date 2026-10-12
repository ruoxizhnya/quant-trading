package backtest

// K18：exec-algo 外部仓位变动取消活跃父单 —— 回测侧端到端测试
// （TASKS.md K18 ⑤ 验收）。
//
// 场景：TWAP 7 片执行窗口（日 0..6）中段（日 3）发生**引擎外改仓**——
// 止损平仓。验收判据：
//  1. 止损前的子单照报；**止损后剩余子单不再报送**（后续交易日无新子单）；
//  2. 止损后不再产生买入成交（不把刚平的仓位买回）；
//  3. 持仓与「整单 + 止损」对照路径一致 —— 两条路径止损后都归零，且
//     `TargetPosition` 的 ActualQty 与实际持仓一致（AUD-55 账目）；
//  4. 日志带 cancel-reason（`cancelled mid-window` / `cancel_reason`），
//     且**不再**出现会话结束的撤销告警（已取消父单不进 Unfinished）。
//
// 破坏腿（去取消钩子 → 断言红）：把 engine.go 日循环里 processStopLosses
// 的取消调用去掉，则日 4..6 的子单照报、止损后买入 > 0、ActualQty ≠ 0，
// 判据 1/2/3 全红。
//
// 数据设计：日 0..2 在 10.00/10.02 振荡（止损位远低于市价，不误触）；
// 日 3 起跌到 9.20（约 −7.8%，未触及 −10% 跌停）并维持 → 触发止损。
// ATR 在本窗口早期不可得（bar 数 < ATRPeriod=14），止损走「2% × AvgCost」
// 兜底 ATR ⇒ 1.5×2% = 3% 止损距离，日 3 的 −7.8% 必然触发。测试用较紧的
// 止损倍数只为让「取消钩子」成为被测对象，止损机制本身与生产同一套代码。
import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// k18Bars 造「窗口中段暴跌触发止损」的确定性 bar（20 个自然日交易日）。
func k18Bars(symbol string, days int, start time.Time) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, days)
	for d := 0; d < days; d++ {
		close := 10.00
		switch {
		case d == 1:
			close = 10.02
		case d >= 3:
			close = 9.20
		}
		bars = append(bars, domain.OHLCV{
			Symbol:   symbol,
			Date:     start.AddDate(0, 0, d),
			Open:     close,
			High:     close + 0.05,
			Low:      close - 0.05,
			Close:    close,
			Volume:   1_000_000,
			Turnover: close * 1_000_000,
		})
	}
	return bars
}

// buildK18Engine 造一台挂载合成数据的引擎（logger 写入 logSink），风险
// 管理器用较紧的止损倍数（见文件头「数据设计」）。
func buildK18Engine(t *testing.T, symbol string, logSink *bytes.Buffer) (*Engine, time.Time) {
	t.Helper()
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // 周一
	bars := k18Bars(symbol, 20, start)

	prov := marketdata.NewInMemoryProvider()
	prov.LoadOHLCV(symbol, bars)
	days := make([]time.Time, 0, 20)
	for d := 0; d < 20; d++ {
		days = append(days, start.AddDate(0, 0, d))
	}
	prov.SetTradingDays(days)
	prov.LoadStocks([]domain.Stock{{Symbol: symbol, Name: "STK-" + symbol, ListDate: start.AddDate(-1, 0, 0)}})

	v := viper.New()
	v.Set("backtest.initial_capital", 1_000_000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 42)
	v.Set("strategy_service.url", "http://localhost:8082")

	logger := zerolog.New(logSink)
	eng, err := NewEngine(v, prov, logger)
	require.NoError(t, err)
	eng.LoadOHLCVInMemory(map[string][]domain.OHLCV{symbol: bars})

	rm, err := risk.NewRiskManager(risk.RiskManagerConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
		ATRPeriod:           14,
		BaseMultiplier:      2.0,
		BullMultiplier:      1.5,
		BearMultiplier:      1.5,
		SidewaysMultiplier:  1.5,
		TakeProfitMult:      3.0,
		VolLookbackDays:     60,
		AnnualizationFactor: 1.4142135623730951,
		FastMAPeriod:        50,
		SlowMAPeriod:        200,
		RegimeVolLookback:   120,
	}, zerolog.Nop())
	require.NoError(t, err)
	eng.SetRiskManager(rm)
	return eng, start
}

// runK18 跑一次回测，返回响应与撮合层观测记录。
func runK18(t *testing.T, eng *Engine, symbol, stratName string, spy *recordingExecService) (*BacktestResponse, []recordedOrder) {
	t.Helper()
	eng.SetExecutionService(spy)
	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       stratName,
		StockPool:      []string{symbol},
		StartDate:      "2026-03-02",
		EndDate:        "2026-03-21",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err)
	require.Equal(t, "completed", resp.Status, "回测应正常完成: %s", resp.Error)
	return resp, spy.orders()
}

// TestExecAlgo_CancelOnStopLossMidWindow 是 K18 ⑤ 的正证据主腿。
func TestExecAlgo_CancelOnStopLossMidWindow(t *testing.T) {
	const sym = "000001.SZ"
	stopDay := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC) // 日 3

	// ── 运行 A：整单 + 止损（对照路径）──
	logA := &bytes.Buffer{}
	engA, _ := buildK18Engine(t, sym, logA)
	spyA := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	whole := &execAlgoProbeStrategy{
		BaseStrategy: &strategy.BaseStrategy{}, name: "k18_e2e_whole",
		symbol: sym, execAlgo: "", strength: 1.0,
		registerFn: func(s *execAlgoProbeStrategy) error { return strategy.DefaultRegistry.Register(s) },
	}
	whole.register(t)
	respA, ordersA := runK18(t, engA, sym, "k18_e2e_whole", spyA)

	require.Len(t, ordersA, 1, "整单路径撮合层只应收到 1 笔整单")
	require.Positive(t, ordersA[0].Order.Quantity)

	// 对照路径确认真被止损平仓（是「整单 + 止损」而非「整单」）。
	require.NotEmpty(t, respA.Trades)
	var closedA bool
	for _, tr := range respA.Trades {
		if tr.Symbol == sym && tr.Direction == domain.DirectionClose && tr.Timestamp.Equal(stopDay) {
			closedA = true
		}
	}
	require.True(t, closedA, "对照路径应在止损日发生平仓成交（否则本用例不成立）")

	// ── 运行 B：TWAP 7 片 + 同一止损 ──
	logB := &bytes.Buffer{}
	engB, _ := buildK18Engine(t, sym, logB)
	spyB := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	twap := &execAlgoProbeStrategy{
		BaseStrategy: &strategy.BaseStrategy{}, name: "k18_e2e_twap7",
		symbol: sym, execAlgo: "twap", slices: 7, strength: 1.0,
		registerFn: func(s *execAlgoProbeStrategy) error { return strategy.DefaultRegistry.Register(s) },
	}
	twap.register(t)
	respB, ordersB := runK18(t, engB, sym, "k18_e2e_twap7", spyB)

	stopLossHappened := false
	for _, tr := range respB.Trades {
		if tr.Symbol == sym && tr.Direction == domain.DirectionClose && tr.Timestamp.Equal(stopDay) {
			stopLossHappened = true
		}
	}
	require.True(t, stopLossHappened, "TWAP 路径应在止损日发生平仓成交（触发条件未成立则用例不成立）")

	// 判据 1：止损后的子单不再报送（当日到点片 vs 后续交易日）。
	require.NotEmpty(t, ordersB, "止损前应有子单被报送")
	assert.Less(t, len(ordersB), 7, "止损后剩余子单必须中止（不应报满 7 片）")
	for i, rec := range ordersB {
		assert.Falsef(t, rec.Order.Timestamp.After(stopDay),
			"第 %d 片子单（%s）落在止损日之后 —— 止损后仍在续买（K18 语义漏洞）",
			i, rec.Order.Timestamp.Format("2006-01-02"))
	}

	// 判据 2：止损后不再产生买入成交。
	var buyAfterStop int
	for _, tr := range respB.Trades {
		if tr.Symbol == sym && tr.Direction == domain.DirectionLong && tr.Timestamp.After(stopDay) {
			buyAfterStop++
		}
	}
	assert.Zero(t, buyAfterStop, "止损日之后不得再有买入成交（止损后应空仓）")

	// 判据 3：与「整单 + 止损」对照 —— 止损后 TargetPosition 账与实际持仓一致。
	stA, okA := engA.stateStore.Get(respA.ID)
	require.True(t, okA)
	stB, okB := engB.stateStore.Get(respB.ID)
	require.True(t, okB)
	heldA := engA.heldQty(stA, sym)
	heldB := engB.heldQty(stB, sym)
	assert.Zero(t, heldA, "对照路径止损后应空仓")
	assert.Zero(t, heldB, "TWAP 路径止损后应空仓（未续买）")
	tpB := stB.TargetPositions[sym]
	require.NotNil(t, tpB, "TWAP 路径应留有 TargetPosition 账")
	assert.InDelta(t, heldB, tpB.ActualQty, 1e-9,
		"TargetPosition.ActualQty 必须与实际持仓一致（AUD-55：引擎外改仓要对齐这份账）")
	assert.InDelta(t, tpB.TargetQty-tpB.ActualQty, tpB.PendingQty, 1e-9,
		"PendingQty 必须等于 TargetQty − ActualQty（账目自洽）")

	// 判据 4：取消带显式 cancel-reason；已取消父单不再触发会话结束撤销告警。
	assert.Contains(t, logB.String(), "cancelled mid-window", "止损取消应带显式 cancel-reason 日志")
	assert.Contains(t, logB.String(), "cancel_reason")
	assert.Contains(t, logB.String(), "stop_loss", "cancel-reason 应为 stop_loss")
	assert.NotContains(t, logB.String(), "unfinished at session end",
		"已取消父单不得再触发会话结束撤销告警（一次取消只记一条、判据单义）")
	// 对照：整单路径无算法单，不应出现算法取消日志。
	assert.NotContains(t, logA.String(), "cancelled mid-window")

	t.Logf("K18 e2e：整单 %d 笔 / 子单 %d 笔（止损日 %s），止损后买入 %d 笔",
		len(ordersA), len(ordersB), stopDay.Format("2006-01-02"), buyAfterStop)
}
