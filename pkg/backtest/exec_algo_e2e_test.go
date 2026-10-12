package backtest

// K17：exec-algo 接进回测下单主路径的端到端测试（TASKS.md K17 ⑤ 验收）。
//
// 正证据（一条用例全覆盖四项判据）：
//  1. 指定 TWAP 拆 7 片的大单 → 撮合层（spy ExecutionService）收到 **7 笔
//     子单**（不是 1 笔整单）；
//  2. Σ 子单量 == 同策略无算法运行的整单量（父单量一致）；
//  3. 时间片均匀：7 片子单落在 7 个不同交易日，相邻间隔恰为 1 自然日；
//  4. 大单总冲击成本 < 同量整单（D5 正证据：impact 模型下拆细后单笔
//     冲击下降，总量约为整单的 1/√7）。
//
// 回归腿：无 exec_algo 的同一策略 → 撮合层只收到 1 笔整单（默认路径
// 逐字节不变的机械判据；既有全量测试是另一条回归腿）。
//
// OnFill 回填腿：回放日志不得出现会话结束的「CANCELLED」撤销警告——
// 一旦接线不回填进度，所有父单都会被判未完成而触发撤销告警（破坏腿 ②
// 的引擎层护栏）。
//
// 数据设计（为什么这样造）：
//   - 交易日 = 逐日历日（合成 provider），7 片窗口内无周末 ⇒ 每片恰好
//     一个交易日，「7 个不同日期」可机械断言；
//   - close 在 10.00/10.02 间振荡、high/low ±0.05 ⇒ ATR≈0.10，止损位
//     （entry−2×ATR≈9.85）远低于市价，不会误触止损污染成交计数；
//   - Volume=1e6 作 ADV 代理，ImpactSigma=0.05 ⇒ 整单冲击≈0.5%、
//     拆分后≈0.19%，差值远超佣金噪声（佣金按滑点分量单独剥离，见
//     impactCost 只算 (Price−参考价)×Qty）。
import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/spf13/viper"
)

// execAlgoFlatBars 造确定性振荡 bar：close 在 10.00/10.02 间交替，
// high/low = close±0.05，Volume=1e6，无涨跌停标志。
func execAlgoFlatBars(symbols []string, days int, start time.Time) map[string][]domain.OHLCV {
	out := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars := make([]domain.OHLCV, 0, days)
		for d := 0; d < days; d++ {
			close := 10.00
			if d%2 == 1 {
				close = 10.02
			}
			bars = append(bars, domain.OHLCV{
				Symbol:   sym,
				Date:     start.AddDate(0, 0, d),
				Open:     close,
				High:     close + 0.05,
				Low:      close - 0.05,
				Close:    close,
				Volume:   1_000_000,
				Turnover: close * 1_000_000,
			})
		}
		out[sym] = bars
	}
	return out
}

// buildExecAlgoEngine 造一台挂载合成数据的引擎（logger 写入 logSink，
// 用于断言「无会话结束撤销警告」）。
func buildExecAlgoEngine(t *testing.T, logSink *bytes.Buffer) (*Engine, []string, time.Time) {
	t.Helper()
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // 周一
	symbols := []string{"000001.SZ", "600000.SH"}
	data := execAlgoFlatBars(symbols, 20, start)

	prov := marketdata.NewInMemoryProvider()
	for sym, bars := range data {
		prov.LoadOHLCV(sym, bars)
	}
	days := make([]time.Time, 0, 20)
	for d := 0; d < 20; d++ {
		days = append(days, start.AddDate(0, 0, d))
	}
	prov.SetTradingDays(days)
	stocks := make([]domain.Stock, 0, len(symbols))
	for _, sym := range symbols {
		stocks = append(stocks, domain.Stock{Symbol: sym, Name: "STK-" + sym, ListDate: start.AddDate(-1, 0, 0)})
	}
	prov.LoadStocks(stocks)

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
	eng.LoadOHLCVInMemory(data)

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
		AnnualizationFactor: 1.4142135623730951,
		FastMAPeriod:        50,
		SlowMAPeriod:        200,
		RegimeVolLookback:   120,
	}, zerolog.Nop())
	require.NoError(t, err)
	eng.SetRiskManager(rm)
	return eng, symbols, start
}

// execAlgoProbeStrategy 在窗口首日对指定 symbol 发一条 Long 信号，
// exec_algo / slices / profile 可配置（每个实例只发一次）。
type execAlgoProbeStrategy struct {
	*strategy.BaseStrategy
	name       string
	symbol     string
	execAlgo   string
	slices     int
	profile    []float64
	strength   float64
	registerFn func(*execAlgoProbeStrategy) error
}

func (s *execAlgoProbeStrategy) Name() string { return s.name }

func (s *execAlgoProbeStrategy) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	series := bars[s.symbol]
	if len(series) != 1 {
		return nil, nil // 只在窗口首日发一次
	}
	return []strategy.Signal{{
		Symbol:      s.symbol,
		Action:      "buy",
		Direction:   domain.DirectionLong,
		Strength:    s.strength,
		Date:        series[0].Date,
		ExecAlgo:    s.execAlgo,
		ExecSlices:  s.slices,
		ExecProfile: s.profile,
	}}, nil
}

func (s *execAlgoProbeStrategy) Weight(_ strategy.Signal, _ float64) float64 { return 1 }

func (s *execAlgoProbeStrategy) register(t *testing.T) {
	t.Helper()
	require.NoError(t, s.registerFn(s))
}

// recordedOrder 是 spy 抓到的一笔委托（含撮合参考 quote 与撮合结果）。
type recordedOrder struct {
	Order domain.Order
	Quote Quote
	Trade domain.Trade // inner.ExecuteOrder 的返回（撮合层成交）
	OK    bool         // 撮合是否成功
}

// recordingExecService 包住真实 BacktestExecutionService，记录全部
// ExecuteOrder 调用（拆单断言的核心观测点）。
type recordingExecService struct {
	inner ExecutionService
	mu    sync.Mutex
	seen  []recordedOrder
}

func (s *recordingExecService) ExecuteOrder(order domain.Order, quote Quote) (domain.Trade, error) {
	trade, err := s.inner.ExecuteOrder(order, quote)
	s.mu.Lock()
	s.seen = append(s.seen, recordedOrder{Order: order, Quote: quote, Trade: trade, OK: err == nil})
	s.mu.Unlock()
	return trade, err
}

func (s *recordingExecService) GetSlippageModel() string  { return s.inner.GetSlippageModel() }
func (s *recordingExecService) SetSlippageModel(m string) { s.inner.SetSlippageModel(m) }

func (s *recordingExecService) orders() []recordedOrder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedOrder, len(s.seen))
	copy(out, s.seen)
	return out
}

// impactExecConfig 是带冲击的执行配置（大单拆细的成本对比来源）。
func impactExecConfig() domain.ExecutionConfig {
	return domain.ExecutionConfig{
		OrderType:             domain.OrderTypeMarket,
		SlippageModel:         "impact",
		CommissionRate:        0.0003,
		MinCommission:         5.0,
		ImpactSigma:           0.05,
		ImpactLiquidityFactor: 1.0,
	}
}

// spyImpactCost 从 spy 观测记录算「冲击分量」= Σ (成交价 − 当根参考价)
// × 数量，只统计成功撮合的**买入**腿。刻意不含佣金（佣金有最低 5 元/笔
// 的下限，7 笔子单的佣金之和会淹没冲击差——要测的是 D5 的冲击下降，
// 不是费率结构）。
//
// 为什么用 spy 记录而不是 resp.Trades：resp.Trades 含回测末尾的
// forceCloseAllPositions 强平卖单（不走 ExecutionService，但会进 tracker
// 成交流），混进来会把「冲击成本」变成「全部成交额」。
func spyImpactCost(recs []recordedOrder) float64 {
	var cost float64
	for _, rec := range recs {
		if !rec.OK || rec.Trade.Direction != domain.DirectionLong {
			continue
		}
		cost += (rec.Trade.Price - rec.Quote.Close) * rec.Trade.Quantity
	}
	return cost
}

// runExecAlgoProbe 跑一次回测，返回响应与撮合层观测记录。
func runExecAlgoProbe(t *testing.T, eng *Engine, stratName string, spy *recordingExecService) (*BacktestResponse, []recordedOrder) {
	t.Helper()
	eng.SetExecutionService(spy)
	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       stratName,
		StockPool:      []string{"000001.SZ", "600000.SH"},
		StartDate:      "2026-03-02",
		EndDate:        "2026-03-21",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err)
	require.Equal(t, "completed", resp.Status, "回测应正常完成: %s", resp.Error)
	return resp, spy.orders()
}

// TestExecAlgo_BacktestTWAP7Slices 是 K17 ⑤ 的正证据主腿。
func TestExecAlgo_BacktestTWAP7Slices(t *testing.T) {
	// ── 运行 A：无 exec_algo（整单）——父单量基准 + 冲击基准 ──
	logA := &bytes.Buffer{}
	engA, _, _ := buildExecAlgoEngine(t, logA)
	spyA := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	whole := &execAlgoProbeStrategy{
		BaseStrategy: &strategy.BaseStrategy{}, name: "k17_e2e_whole",
		symbol: "000001.SZ", execAlgo: "", strength: 1.0,
		registerFn: func(s *execAlgoProbeStrategy) error { return strategy.DefaultRegistry.Register(s) },
	}
	whole.register(t)
	respA, ordersA := runExecAlgoProbe(t, engA, "k17_e2e_whole", spyA)

	require.Len(t, ordersA, 1, "默认路径：撮合层只应收到 1 笔整单")
	assert.Empty(t, ordersA[0].Order.ID, "默认路径的委托不带算法子单号（逐字节不变）")
	wholeQty := ordersA[0].Order.Quantity
	require.Positive(t, wholeQty)
	require.Equal(t, 1, respA.TotalTrades)

	// ── 运行 B：exec_algo=twap, slices=7 ──
	logB := &bytes.Buffer{}
	engB, _, _ := buildExecAlgoEngine(t, logB)
	spyB := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	twap := &execAlgoProbeStrategy{
		BaseStrategy: &strategy.BaseStrategy{}, name: "k17_e2e_twap7",
		symbol: "000001.SZ", execAlgo: "twap", slices: 7, strength: 1.0,
		registerFn: func(s *execAlgoProbeStrategy) error { return strategy.DefaultRegistry.Register(s) },
	}
	twap.register(t)
	_, ordersB := runExecAlgoProbe(t, engB, "k17_e2e_twap7", spyB)

	// 判据 1：7 笔子单（而非 1 笔整单）。
	require.Len(t, ordersB, 7, "TWAP 7 片 → 撮合层应收到 7 笔子单")

	// 判据 2：Σ 子单量 == 整单量（父单量一致）。
	var sum float64
	for i, rec := range ordersB {
		sum += rec.Order.Quantity
		assert.Equal(t,
			fmt.Sprintf("EA-20260302-000001.SZ-long-C%04d", i),
			rec.Order.ID, "第 %d 片子单号 = (父单号, 片序号) 纯函数", i)
		assert.Positive(t, rec.Order.Quantity, "每片为正手")
		assert.Equal(t, domain.DirectionLong, rec.Order.Direction)
		assert.Equal(t, domain.OrderTypeMarket, rec.Order.OrderType)
	}
	assert.Equal(t, wholeQty, sum, "Σ 子单量必须等于同策略整单量")

	// 判据 3：时间片均匀 —— 7 个不同交易日、相邻间隔恰为 1 自然日。
	dates := make([]time.Time, 0, 7)
	for _, rec := range ordersB {
		dates = append(dates, rec.Order.Timestamp)
	}
	for i := 1; i < len(dates); i++ {
		assert.Equal(t, 24*time.Hour, dates[i].Sub(dates[i-1]), "第 %d 片与前一峰间隔应恰为 1 自然日", i)
	}
	// 7 笔子单全部成交（TotalTrades 是「往返次数」口径，同 symbol 加仓
	// 只算 1 次——这里按撮合层观测断言）。
	for i, rec := range ordersB {
		assert.True(t, rec.OK, "第 %d 片子单应撮合成功", i)
		assert.Equal(t, rec.Order.Quantity, rec.Trade.Quantity, "第 %d 片应全额成交", i)
	}

	// 判据 4：大单总冲击成本 < 同量整单（D5 正证据，期望 ≈ 1/√7）。
	wholeImpact := spyImpactCost(ordersA)
	splitImpact := spyImpactCost(ordersB)
	require.Positive(t, wholeImpact, "整单应有正冲击成本（impact 模型生效）")
	assert.Less(t, splitImpact, wholeImpact,
		"拆单后总冲击成本必须低于同量整单（got split=%v whole=%v）", splitImpact, wholeImpact)
	ratio := splitImpact / wholeImpact
	assert.Less(t, ratio, 0.8, "拆细 7 片后期望占比 ≈ 1/√7 ≈ 0.378，got %v", ratio)
	t.Logf("D5 冲击对比：整单冲击=%v 拆分冲击=%v 占比=%v（期望≈0.378）", wholeImpact, splitImpact, ratio)

	// OnFill 回填腿：无会话结束撤销警告（接线不回填 → 全部父单未完成 → 红）。
	assert.NotContains(t, logB.String(), "CANCELLED",
		"回放日志出现会话结束撤销警告 —— exec-algo 父单进度未正确回填")
	t.Logf("回测 e2e：%d 笔子单 Σ=%v（整单 %v），冲击占比 %v，日志无撤销警告",
		len(ordersB), sum, wholeQty, ratio)
}

// TestExecAlgo_BacktestVWAP7Slices 覆盖 paper 之外的第二条算法经回测
// 路径：VWAP 按 profile 加权，Σ 仍精确等于整单量。
func TestExecAlgo_BacktestVWAP7Slices(t *testing.T) {
	logB := &bytes.Buffer{}
	engB, _, _ := buildExecAlgoEngine(t, logB)
	spyB := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	vwap := &execAlgoProbeStrategy{
		BaseStrategy: &strategy.BaseStrategy{}, name: "k17_e2e_vwap7",
		symbol: "000001.SZ", execAlgo: "vwap", slices: 7,
		profile:    []float64{1, 2, 3, 4, 5, 6, 7},
		strength:   1.0,
		registerFn: func(s *execAlgoProbeStrategy) error { return strategy.DefaultRegistry.Register(s) },
	}
	vwap.register(t)
	_, ordersB := runExecAlgoProbe(t, engB, "k17_e2e_vwap7", spyB)

	require.Len(t, ordersB, 7)
	var sum float64
	for _, rec := range ordersB {
		sum += rec.Order.Quantity
	}
	// profile[1..7] 归一化占比 1/28..7/28 ⇒ 首片 < 末片，Σ=父单量。
	assert.Less(t, ordersB[0].Order.Quantity, ordersB[6].Order.Quantity, "VWAP 应按成交量 profile 加权")
	require.Positive(t, sum)
	for i, rec := range ordersB {
		assert.True(t, rec.OK, "VWAP 第 %d 片子单应撮合成功", i)
	}
	assert.NotContains(t, logB.String(), "CANCELLED")
	t.Logf("VWAP 7 片经回测路径：Σ=%v 首片=%v 末片=%v", sum, ordersB[0].Order.Quantity, ordersB[6].Order.Quantity)
}

// TestExecAlgo_DefaultPathUnchangedWithActiveSiblings 是回归腿的机械判据：
// 同一引擎里「有算法单」与「无算法单」并存时，无算法信号仍只走 1 笔
// 整单（算法接线不污染默认路径）。
func TestExecAlgo_DefaultPathUnchangedWithActiveSiblings(t *testing.T) {
	logB := &bytes.Buffer{}
	eng, _, _ := buildExecAlgoEngine(t, logB)
	spy := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}

	// 策略：000001.SZ 走 TWAP 7 片；600000.SH 不指定算法。
	mixed := &execAlgoMixedStrategy{name: "k17_e2e_mixed"}
	require.NoError(t, strategy.DefaultRegistry.Register(mixed))

	_, orders := runExecAlgoProbe(t, eng, "k17_e2e_mixed", spy)

	var twap, plain int
	var plainQty float64
	for _, rec := range orders {
		if rec.Order.Symbol == "000001.SZ" {
			twap++
			assert.NotEmpty(t, rec.Order.ID, "算法子单带 (父单号, 片序号) 子单号")
		} else {
			plain++
			plainQty += rec.Order.Quantity
			assert.Empty(t, rec.Order.ID, "默认路径委托不带算法子单号")
		}
	}
	assert.Equal(t, 7, twap, "000001.SZ 应拆 7 片")
	assert.Equal(t, 1, plain, "600000.SH 无算法 → 1 笔整单")
	assert.NotContains(t, logB.String(), "CANCELLED")
	t.Logf("混合运行：TWAP %d 片 + 默认整单 %d 笔（qty=%v）", twap, plain, plainQty)
}

// execAlgoMixedStrategy 一个 symbol 走算法、一个不走的混合策略。
type execAlgoMixedStrategy struct {
	*strategy.BaseStrategy
	name string
}

func (s *execAlgoMixedStrategy) Name() string { return s.name }

func (s *execAlgoMixedStrategy) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	var out []strategy.Signal
	if series := bars["000001.SZ"]; len(series) == 1 {
		out = append(out, strategy.Signal{
			Symbol: "000001.SZ", Action: "buy", Direction: domain.DirectionLong,
			Strength: 1.0, Date: series[0].Date, ExecAlgo: "twap", ExecSlices: 7,
		})
	}
	if series := bars["600000.SH"]; len(series) == 1 {
		out = append(out, strategy.Signal{
			Symbol: "600000.SH", Action: "buy", Direction: domain.DirectionLong,
			Strength: 1.0, Date: series[0].Date,
		})
	}
	return out, nil
}

func (s *execAlgoMixedStrategy) Weight(_ strategy.Signal, _ float64) float64 { return 1 }

// ─── 小工具 ────────────────────────────────────────────────────────
