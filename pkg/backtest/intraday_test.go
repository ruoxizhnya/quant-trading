package backtest

// D6 · 多频率进回测策略视野 —— 引擎侧测试。
//
// 覆盖验收六条：
//  ① 分钟 PIT 闸门：10:00 bar 在 10:00:30 不可见、10:01 可见
//     （判定原语在 pkg/domain/market 的 visibility_test.go 钉死，这里
//     补 provider 级的特征可用性翻转）；
//  ② 样例策略回测：「日频决策 + 引用当日内分钟特征」跑通 RunBacktest；
//  ③ 对拍口径：1m 与 15m（D1 聚合规则）两条输入算出同一特征值，
//     且等于手算样本；
//  ④ 回归：纯日线策略在 D6 接线在/不在两种装配下数值逐点相等；
//  ⑤ 预热三「不」：策略不要就不查、没有 Router 不阻断、失败不假装；
//  ⑥ 红线：provider 无运行时数据通道（结构 + 反射）。

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ─── 测试装置 ────────────────────────────────────────────────────────

// d6Day 是样例数据使用的交易日（UTC 零点形态，与引擎日循环一致）。
var d6Day = time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

// d6MinuteBar 构造一根 1m bar：stamp 为 Shanghai 时刻，VisibleAt 按
// 规范层口径（周期结束）自动填。
func d6MinuteBar(symbol string, h, m int, open, high, low, close float64, vol float64) domain.OHLCV {
	stamp := time.Date(2024, 1, 2, h, m, 0, 0, market.AsiaShanghai)
	return domain.OHLCV{
		Symbol:    symbol,
		Date:      stamp,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    vol,
		Frequency: market.Freq1m,
		VisibleAt: market.IntradayVisibleAt(stamp, 1),
	}
}

// d6OpenHalfHourBars 构造开盘半小时（09:30–09:59 共 30 根）的 1m bar。
//
// 手算样本（测试钉死用）：
//   - 09:30 首笔：open=10.00
//   - 09:59 末笔：close=10.50
//   - ⇒ open_momentum_30m = 10.50/10.00 - 1 = 0.05
//
// 中间 bar 的价格走向不影响特征值（只取首开/末收），但**根数**影响
// 窗口是否闭合的判断，故给满 30 根。
func d6OpenHalfHourBars(symbol string) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, 30)
	for i := 0; i < 30; i++ {
		m := 30 + i
		h := 9
		if m >= 60 {
			h, m = 10, m-60
		}
		// 价格从 10.00 线性抬到 10.50（末笔 close 恰为 10.50）。
		price := 10.00 + 0.50*float64(i)/29.0
		bars = append(bars, d6MinuteBar(symbol, h, m, price, price+0.02, price-0.02, price, 1000))
	}
	return bars
}

// d6DailyBar 构造一根日线 bar（VisibleAt = 当日 15:00 收盘）。
func d6DailyBar(symbol string, day time.Time, open, high, low, close float64) domain.OHLCV {
	return domain.OHLCV{
		Symbol:    symbol,
		Date:      day,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    1e6,
		Frequency: market.Freq1d,
		VisibleAt: market.DailyVisibleAt(day),
	}
}

// d6Engine 装配一台带合成日线 + RiskManager 的引擎（无 Router ——
// 日内数据走 SetIntradaySnapshot 直注，见各测试）。
//
// 交易日历 = [01-02, 01-03, 01-04]，与 daily 数据的三根 bar 对齐。
func d6Engine(t *testing.T, daily map[string][]domain.OHLCV) *Engine {
	t.Helper()

	prov := marketdata.NewInMemoryProvider()
	for sym, bars := range daily {
		prov.LoadOHLCV(sym, bars)
	}
	days := []time.Time{d6Day, d6Day.AddDate(0, 0, 1), d6Day.AddDate(0, 0, 2)}
	prov.SetTradingDays(days)
	prov.LoadStocks([]domain.Stock{
		{Symbol: "600519.SH", Name: "贵州茅台", ListDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Symbol: "000001.SZ", Name: "平安银行", ListDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
	})

	eng, err := NewEngine(viperD6(), prov, zerolog.Nop())
	require.NoError(t, err)
	eng.LoadOHLCVInMemory(daily)
	eng.SetRiskManager(d6RiskManager(t))
	eng.SetParallelWorkers(1)
	return eng
}

// viperD6 是与 newTestEngine 同款的引擎配置（费用/风控参数取同一组值，
// 保证回测数值判据不因配置漂移）。
func viperD6() *viper.Viper {
	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("trading.stamp_tax_rate", 0.001)
	v.Set("trading.min_commission", 5.0)
	v.Set("trading.transfer_fee_rate", 0.00001)
	v.Set("trading.price_limit.normal", 0.10)
	v.Set("trading.price_limit.st", 0.05)
	v.Set("trading.price_limit.new", 0.20)
	v.Set("trading.new_stock_days", 60)
	return v
}

// d6RiskManager 装配 in-process 风控（与 buildSyntheticEngine 同配）——
// 没有它，position sizing 会退到 HTTP 风控服务，测试环境必然失败。
func d6RiskManager(t *testing.T) *risk.RiskManager {
	t.Helper()
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
	require.NoError(t, err)
	return rm
}

// ─── ① 分钟粒度闸门：特征可用性在窗口闭合那一刻翻转 ──────────────────

func TestSnapshotIntradayProvider_OpenMomentum30m_HandComputed(t *testing.T) {
	sym := "600519.SH"
	snapshot := map[string][]domain.OHLCV{
		sym: d6OpenHalfHourBars(sym),
	}
	prov := &snapshotIntradayProvider{
		snapshot: snapshot,
		dayStart: time.Date(2024, 1, 2, 0, 0, 0, 0, market.AsiaShanghai),
		dayEnd:   time.Date(2024, 1, 3, 0, 0, 0, 0, market.AsiaShanghai),
	}

	// 能力声明：目录里必须有这个特征（策略靠它自查）。
	assert.Equal(t, []string{featureOpenMomentum30m}, prov.Features())

	cases := []struct {
		name    string
		asOf    time.Time
		wantOK  bool
		wantVal float64
	}{
		// 窗口未闭合：末 bar（09:59）VisibleAt=10:00，之前不得给
		// 「半窗口」的值 —— 那看起来像个完整动量，其实是残缺样本。
		{"before_window_close", time.Date(2024, 1, 2, 9, 59, 30, 0, market.AsiaShanghai), false, 0},
		{"window_close_minus_1ns", time.Date(2024, 1, 2, 9, 59, 59, 999999999, market.AsiaShanghai), false, 0},
		// 窗口闭合：10:00 整起可得（VisibleAt 边界 inclusive）。
		{"window_close_exact", time.Date(2024, 1, 2, 10, 0, 0, 0, market.AsiaShanghai), true, 0.05},
		// 日频决策时刻（收盘后）当然可得，值不变。
		{"daily_decision_time", market.DailyVisibleAt(d6Day), true, 0.05},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := prov.IntradayFeature(context.Background(), sym, featureOpenMomentum30m, tc.asOf)
			assert.Equal(t, tc.wantOK, ok, "asOf=%s", tc.asOf.In(market.AsiaShanghai))
			if tc.wantOK {
				assert.InDelta(t, tc.wantVal, got, 1e-12, "手算样本 10.50/10.00-1=0.05")
			} else {
				assert.Equal(t, 0.0, got, "不可得时值是 0 且 ok=false（缺失≠0 由 ok 承载）")
			}
		})
	}
}

// 10:00 整的 bar（周期 10:00–10:01，VisibleAt=10:01）在 10:00:30
// 不可见、10:01 可见 —— 蓝图 §4.3 原句，provider 消费侧复现。
// 该 bar 在窗口 [09:30,10:00) 之外，不进特征；这里单独钉它的闸门。
func TestSnapshotIntradayProvider_MinuteGateOnTenOClockBar(t *testing.T) {
	sym := "600519.SH"
	tenOClock := d6MinuteBar(sym, 10, 0, 10.50, 10.52, 10.48, 10.51, 1000)
	require.Equal(t,
		time.Date(2024, 1, 2, 10, 1, 0, 0, market.AsiaShanghai), tenOClock.VisibleAt,
		"10:00 的 1m bar 可见点必须是 10:01（周期结束）")

	assert.False(t, tenOClock.VisibleBy(time.Date(2024, 1, 2, 10, 0, 30, 0, market.AsiaShanghai)),
		"10:00:30 不得看到 10:00 bar")
	assert.True(t, tenOClock.VisibleBy(time.Date(2024, 1, 2, 10, 1, 0, 0, market.AsiaShanghai)),
		"10:01 应看到 10:00 bar")

	// 即便把 10:00 bar 塞进当日快照，特征也不该用它（窗口不含它）。
	prov := &snapshotIntradayProvider{
		snapshot: map[string][]domain.OHLCV{sym: append(d6OpenHalfHourBars(sym), tenOClock)},
		dayStart: time.Date(2024, 1, 2, 0, 0, 0, 0, market.AsiaShanghai),
		dayEnd:   time.Date(2024, 1, 3, 0, 0, 0, 0, market.AsiaShanghai),
	}
	got, ok := prov.IntradayFeature(context.Background(), sym, featureOpenMomentum30m,
		market.DailyVisibleAt(d6Day))
	require.True(t, ok)
	assert.InDelta(t, 0.05, got, 1e-12, "10:00 bar 的 close(10.51) 不得污染开盘半小时动量")
}

// 缺失≠0：当日无分钟数据 / 只有半窗 ⇒ ok=false，不是 0。
func TestSnapshotIntradayProvider_MissingIsNotZero(t *testing.T) {
	sym := "600519.SH"
	halfWindow := d6OpenHalfHourBars(sym)[:15] // 只有 09:30–09:44
	prov := &snapshotIntradayProvider{
		snapshot: map[string][]domain.OHLCV{sym: halfWindow},
		dayStart: time.Date(2024, 1, 2, 0, 0, 0, 0, market.AsiaShanghai),
		dayEnd:   time.Date(2024, 1, 3, 0, 0, 0, 0, market.AsiaShanghai),
	}
	got, ok := prov.IntradayFeature(context.Background(), sym, featureOpenMomentum30m,
		market.DailyVisibleAt(d6Day))
	assert.False(t, ok, "半窗不得给出特征")
	assert.Equal(t, 0.0, got)

	// 目录外特征名：不可得（不发明特征）。
	_, ok = prov.IntradayFeature(context.Background(), sym, "no_such_feature", market.DailyVisibleAt(d6Day))
	assert.False(t, ok)

	// 空快照 / 无此 symbol：不可得。
	empty := &snapshotIntradayProvider{dayStart: prov.dayStart, dayEnd: prov.dayEnd}
	_, ok = empty.IntradayFeature(context.Background(), sym, featureOpenMomentum30m, market.DailyVisibleAt(d6Day))
	assert.False(t, ok)
}

// 回放日边界：provider 只暴露**当日**的 bar。前一日 / 次日的分钟数据
// 物理上在快照里（启动前预加载全区间），但按日绑定后取不到 —— 这是
// 「启动前快照」与「不许偷看别的日子」两道红线的交点。
func TestSnapshotIntradayProvider_DayScopedNoOtherDays(t *testing.T) {
	sym := "600519.SH"
	day2 := d6OpenHalfHourBars(sym) // 2024-01-02：特征 0.05
	day3 := d6OpenHalfHourBars(sym) // 2024-01-03：同形态
	for i := range day3 {           // 改个开盘价拉开差异
		day3[i].Date = day3[i].Date.AddDate(0, 0, 1)
		day3[i].Open = 20.0
		day3[i].Close = 22.0
		day3[i].VisibleAt = day3[i].VisibleAt.AddDate(0, 0, 1)
	}
	day1 := d6OpenHalfHourBars(sym) // 2024-01-01
	for i := range day1 {
		day1[i].Date = day1[i].Date.AddDate(0, 0, -1)
		day1[i].VisibleAt = day1[i].VisibleAt.AddDate(0, 0, -1)
	}
	all := append(append(append([]domain.OHLCV{}, day1...), day2...), day3...)

	provDay2 := &snapshotIntradayProvider{
		snapshot: map[string][]domain.OHLCV{sym: all},
		dayStart: time.Date(2024, 1, 2, 0, 0, 0, 0, market.AsiaShanghai),
		dayEnd:   time.Date(2024, 1, 3, 0, 0, 0, 0, market.AsiaShanghai),
	}
	got, ok := provDay2.IntradayFeature(context.Background(), sym, featureOpenMomentum30m,
		market.DailyVisibleAt(d6Day))
	require.True(t, ok, "回放日 01-02 的特征必须可得")
	assert.InDelta(t, 0.05, got, 1e-12, "取值只能来自 01-02 当日（10.50/10.00），不得混入 01-03 的 22/20")

	// asOf 传到次日也不会拿到次日的特征：day3 的窗口末 bar VisibleAt
	// 是 01-03 10:00，但 dayBars 只切 01-02 段 ⇒ 半窗 ⇒ 不可得。
	got, ok = provDay2.IntradayFeature(context.Background(), sym, featureOpenMomentum30m,
		time.Date(2024, 1, 3, 15, 0, 0, 0, market.AsiaShanghai))
	assert.True(t, ok)
	assert.InDelta(t, 0.05, got, 1e-12, "asOf 未来不改变按日绑定：仍只有 01-02 的值")
}

// ─── ③ 对拍口径：1m 与 15m（D1 聚合规则）同值 ────────────────────────

func TestSnapshotIntradayProvider_FeatureParity_1m_vs_15m(t *testing.T) {
	sym := "600519.SH"
	minuteBars := d6OpenHalfHourBars(sym)

	// 15m 路径：经 D1 聚合规则（开=首笔开、收=末笔收）由 1m 聚合。
	agg15, err := marketdata.Aggregate(minuteBars, market.Freq15m)
	require.NoError(t, err)
	require.Len(t, agg15, 2, "09:30–10:00 应聚出 09:30 / 09:45 两个 bucket")

	prov1m := &snapshotIntradayProvider{
		snapshot: map[string][]domain.OHLCV{sym: minuteBars},
		dayStart: time.Date(2024, 1, 2, 0, 0, 0, 0, market.AsiaShanghai),
		dayEnd:   time.Date(2024, 1, 3, 0, 0, 0, 0, market.AsiaShanghai),
	}
	prov15m := &snapshotIntradayProvider{
		snapshot: map[string][]domain.OHLCV{sym: agg15},
		dayStart: prov1m.dayStart,
		dayEnd:   prov1m.dayEnd,
	}

	asOf := market.DailyVisibleAt(d6Day)
	v1m, ok1 := prov1m.IntradayFeature(context.Background(), sym, featureOpenMomentum30m, asOf)
	v15m, ok15 := prov15m.IntradayFeature(context.Background(), sym, featureOpenMomentum30m, asOf)
	require.True(t, ok1)
	require.True(t, ok15, "15m 聚合输入必须同样算出特征（双核翻译点口径一致）")
	assert.InDelta(t, v1m, v15m, 1e-12, "1m 与 15m 两条输入必须同值（D1 聚合规则对拍）")
	assert.InDelta(t, 0.05, v15m, 1e-12, "且都等于手算样本")

	// 15m 的可见性与 1m 同构：09:45 bucket 收 = 09:59 末笔收，
	// 其 VisibleAt 同为 10:00 ⇒ 窗口闭合时刻一致。
	require.Equal(t,
		time.Date(2024, 1, 2, 10, 0, 0, 0, market.AsiaShanghai), agg15[1].VisibleAt,
		"09:45 bucket 可见点 = 09:45+15m = 10:00")
}

// ─── ② 样例策略回测：日频决策 + 日内特征 ─────────────────────────────

// intradayMomentumStrategy 是 D6 样例策略：每个交易日读「开盘半小时
// 动量」日内特征，正的且过阈值的标的买入（strength = 特征值），
// 否则持有。**没有日内供给时明确失败**（不静默当 0）。
type intradayMomentumStrategy struct {
	*strategy.BaseStrategy
	threshold float64

	mu       sync.Mutex
	provider strategy.IntradayFeatureProvider
	// seen 按「日期|symbol」记录每次决策看到的特征值（测试断言用；
	// 同 symbol 多日决策会覆盖，故带日期键）。
	seen  map[string]float64
	calls int
}

func newIntradayMomentumStrategy(name string, threshold float64) *intradayMomentumStrategy {
	return &intradayMomentumStrategy{
		BaseStrategy: strategy.NewBaseStrategy(name, "D6 sample: daily decision + intraday open-30m momentum"),
		threshold:    threshold,
		seen:         map[string]float64{},
	}
}

func (s *intradayMomentumStrategy) SetIntradayFeatures(p strategy.IntradayFeatureProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = p
}

func (s *intradayMomentumStrategy) seenSnapshot() (map[string]float64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]float64, len(s.seen))
	for k, v := range s.seen {
		out[k] = v
	}
	return out, s.calls
}

func (s *intradayMomentumStrategy) GenerateSignals(
	ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio,
) ([]strategy.Signal, error) {
	s.mu.Lock()
	provider := s.provider
	s.mu.Unlock()
	if provider == nil {
		return nil, fmt.Errorf("intraday sample strategy: no IntradayFeatureProvider injected — " +
			"没有供给就没有特征，明确失败而不是当 0")
	}

	symbols := make([]string, 0, len(bars))
	for sym := range bars {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols) // 喂序确定性（P1-14）

	var signals []strategy.Signal
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()

	for _, sym := range symbols {
		data := bars[sym]
		if len(data) == 0 {
			continue
		}
		// 日频决策时刻 = 当日收盘（日线 bar 的可见点，规范层口径）。
		asOf := market.DailyVisibleAt(data[len(data)-1].Date)
		feat, ok := provider.IntradayFeature(ctx, sym, featureOpenMomentum30m, asOf)
		if !ok {
			continue // 当日无分钟数据 / 停牌：没有特征就没有信号
		}
		s.mu.Lock()
		s.seen[data[len(data)-1].Date.UTC().Format("2006-01-02")+"|"+sym] = feat
		s.mu.Unlock()
		if feat > s.threshold {
			signals = append(signals, strategy.Signal{
				Symbol:   sym,
				Action:   "buy",
				Strength: feat,
				Price:    data[len(data)-1].Close,
			})
		}
	}
	return signals, nil
}

func (s *intradayMomentumStrategy) Weight(sig strategy.Signal, portfolioValue float64) float64 {
	return sig.Strength
}

func TestEngine_IntradayAwareStrategy_Backtest(t *testing.T) {
	const sym = "600519.SH"
	// 三个交易日：01-02 特征 +0.05（>0.02 ⇒ 买），01-03 特征 -0.03
	// （<=阈值 ⇒ 不买，持有），01-04 无分钟数据（停牌 ⇒ 无信号）。
	daily := map[string][]domain.OHLCV{
		sym: {
			d6DailyBar(sym, d6Day, 10.00, 10.60, 9.98, 10.50),
			d6DailyBar(sym, d6Day.AddDate(0, 0, 1), 10.50, 10.55, 10.20, 10.22),
			d6DailyBar(sym, d6Day.AddDate(0, 0, 2), 10.22, 10.30, 10.10, 10.15),
		},
	}
	eng := d6Engine(t, daily)

	// 日内快照：01-02 开盘半小时 10.00 → 10.50（+0.05）；
	// 01-03 开盘半小时 20.00 → 19.40（-0.03）；01-04 无数据。
	day2Min := d6OpenHalfHourBars(sym)
	day3Min := d6OpenHalfHourBars(sym)
	for i := range day3Min {
		day3Min[i].Date = day3Min[i].Date.AddDate(0, 0, 1)
		day3Min[i].VisibleAt = day3Min[i].VisibleAt.AddDate(0, 0, 1)
		day3Min[i].Open = 20.0
		day3Min[i].Close = 19.40
	}
	eng.SetIntradaySnapshot(map[string][]domain.OHLCV{
		sym: append(append([]domain.OHLCV{}, day2Min...), day3Min...),
	})

	strat := newIntradayMomentumStrategy("d6_intraday_momentum", 0.02)
	require.NoError(t, strategy.DefaultRegistry.Register(strat))

	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       strat.Name(),
		StockPool:      []string{sym},
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-04",
		InitialCapital: 1000000,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, resp.Status, "invalid_reasons=%v", resp.InvalidReasons)

	// 策略确实读到了日内特征，且值 = 手算样本（按日键）。
	seen, calls := strat.seenSnapshot()
	assert.Equal(t, 3, calls, "三个交易日每天都该被调用")
	assert.InDelta(t, 0.05, seen["2024-01-02|"+sym], 1e-12, "01-02 特征 = 10.50/10.00-1")
	assert.InDelta(t, -0.03, seen["2024-01-03|"+sym], 1e-12, "01-03 特征 = 19.40/20.00-1")
	assert.Len(t, seen, 2, "01-04 无分钟数据，不得有特征记录（缺失≠0）")

	// 01-02 收盘 10.50 买入一笔；之后特征转负、停牌无数据 ⇒ 不再买；
	// 回测结束强平一笔 ⇒ fills 2 笔、往返 1 次。
	// 注意 TotalTrades 是**往返**口径（metrics.calculateTradeMetrics：
	// 一开一平配对计 1），逐笔看 len(resp.Trades)。
	require.Equal(t, 1, resp.TotalTrades, "一个开平往返")
	require.Len(t, resp.Trades, 2, "trades=%+v", resp.Trades)
	assert.Equal(t, domain.DirectionLong, resp.Trades[0].Direction)
	assert.Equal(t, sym, resp.Trades[0].Symbol)
	// 买入价 = 收盘 × (1 + 滑点) = 10.50 × 1.0001。
	assert.InDelta(t, 10.5105, resp.Trades[0].Price, 1e-9, "成交价 = 01-02 收盘含滑点（日频决策口径）")
	assert.Equal(t, d6Day, resp.Trades[0].Timestamp.UTC(), "首笔成交在 01-02")
	// 强平在回测末日（01-04），卖出价 = 10.15 × (1 - 滑点)。
	assert.Equal(t, domain.DirectionClose, resp.Trades[1].Direction)
	assert.Equal(t, d6Day.AddDate(0, 0, 2), resp.Trades[1].Timestamp.UTC(), "强平在 01-04")
	assert.InDelta(t, 10.148985, resp.Trades[1].Price, 1e-9)
}

// 没有供给时样例策略明确失败（不静默当 0）—— 契约注释的执行体。
func TestEngine_IntradayAwareStrategy_FailsLoudWithoutProvider(t *testing.T) {
	const sym = "600519.SH"
	daily := map[string][]domain.OHLCV{
		sym: {
			d6DailyBar(sym, d6Day, 10.00, 10.60, 9.98, 10.50),
			d6DailyBar(sym, d6Day.AddDate(0, 0, 1), 10.50, 10.55, 10.20, 10.22),
			d6DailyBar(sym, d6Day.AddDate(0, 0, 2), 10.22, 10.30, 10.10, 10.15),
		},
	}
	eng := d6Engine(t, daily)
	// 不注快照、不接 Router ⇒ 策略拿不到供给。

	strat := newIntradayMomentumStrategy("d6_intraday_no_provider", 0.02)
	require.NoError(t, strategy.DefaultRegistry.Register(strat))

	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       strat.Name(),
		StockPool:      []string{sym},
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-04",
		InitialCapital: 1000000,
		RiskFreeRate:   0.03,
	})
	// 引擎对 getSignals 失败是「跳过该日」（warn），不是整场失败 ——
	// 与既有语义一致。但结果必须是没有成交（策略明确失败 ⇒ 无信号），
	// 而不是假装有特征下了单。
	require.NoError(t, err)
	assert.Equal(t, 0, resp.TotalTrades, "没有供给就没有信号，不得凭空成交")
}

// ─── ④ 回归：纯日线策略对 D6 接线不敏感 ──────────────────────────────

func TestEngine_DailyOnlyBacktest_UnchangedWithIntradayWiring(t *testing.T) {
	const sym = "600519.SH"
	daily := map[string][]domain.OHLCV{
		sym: {
			d6DailyBar(sym, d6Day, 10.00, 10.60, 9.98, 10.50),
			d6DailyBar(sym, d6Day.AddDate(0, 0, 1), 10.50, 10.55, 10.20, 10.22),
			d6DailyBar(sym, d6Day.AddDate(0, 0, 2), 10.22, 10.30, 10.10, 10.15),
		},
	}
	req := BacktestRequest{
		Strategy:       "momentum",
		StockPool:      []string{sym},
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-04",
		InitialCapital: 1000000,
		RiskFreeRate:   0.03,
	}

	// A：无任何 D6 装配（基线形态）。
	engA := d6Engine(t, daily)
	respA, err := engA.RunBacktest(context.Background(), req)
	require.NoError(t, err)

	// B：D6 接线全在（日内快照 + 空 Router 均挂上），但策略是纯日线
	// 的 momentum（未实现 IntradayAware）—— 必须逐点相等。
	engB := d6Engine(t, daily)
	engB.SetIntradaySnapshot(map[string][]domain.OHLCV{
		sym: d6OpenHalfHourBars(sym),
	})
	engB.SetRouter(nil) // 显式挂 nil：接线在，预热不触发
	respB, err := engB.RunBacktest(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, respA.TotalReturn, respB.TotalReturn, "纯日线策略总收益不得因 D6 接线变化")
	require.Equal(t, respA.TotalTrades, respB.TotalTrades)
	require.Equal(t, len(respA.PortfolioValues), len(respB.PortfolioValues))
	for i := range respA.PortfolioValues {
		assert.Equal(t, respA.PortfolioValues[i].Date, respB.PortfolioValues[i].Date)
		assert.InDelta(t, respA.PortfolioValues[i].TotalValue, respB.PortfolioValues[i].TotalValue, 1e-9,
			"equity curve 第 %d 点漂移", i)
	}
}

// ─── ⑤ 预热三「不」 ──────────────────────────────────────────────────

func TestEngine_WarmIntraday_SkippedWhenStrategyNotAware(t *testing.T) {
	eng := d6Engine(t, nil)
	require.NoError(t, strategy.DefaultRegistry.Register(&plainStrategy{name: "d6_plain"}))

	err := eng.warmIntradayIfNeeded(context.Background(), "d6_plain",
		[]string{"600519.SH"}, d6Day, d6Day.AddDate(0, 0, 2))
	require.NoError(t, err)
	assert.Nil(t, eng.intradaySnapshot(), "纯日线策略不该触发日内预热")
}

func TestEngine_WarmIntraday_NoRouterIsNoop(t *testing.T) {
	eng := d6Engine(t, nil)
	strat := newIntradayMomentumStrategy("d6_warm_norouter", 0.02)
	require.NoError(t, strategy.DefaultRegistry.Register(strat))

	// IntradayAware 但没接 Router：不报错、不阻断（同财报无 store 语义）。
	err := eng.warmIntradayIfNeeded(context.Background(), strat.Name(),
		[]string{"600519.SH"}, d6Day, d6Day.AddDate(0, 0, 2))
	require.NoError(t, err)
	assert.Nil(t, eng.intradaySnapshot(), "无 Router 不预热；策略用到特征会明确失败")
}

func TestEngine_SetIntradaySnapshot_RoundTrip(t *testing.T) {
	eng := d6Engine(t, nil)
	sym := "600519.SH"
	want := map[string][]domain.OHLCV{sym: d6OpenHalfHourBars(sym)}
	eng.SetIntradaySnapshot(want)

	got := eng.intradaySnapshot()
	require.NotNil(t, got)
	require.Len(t, got[sym], 30)
	assert.Equal(t, market.Freq1m, got[sym][0].Frequency, "快照 bar 必须带规范层频率声明")
	assert.False(t, got[sym][0].VisibleAt.IsZero(), "快照 bar 必须带可见时间点（闸门靠它裁决）")

	// 按日 provider 从快照切当日段并算出特征。
	prov := eng.intradayFeatureProvider(d6Day)
	v, ok := prov.IntradayFeature(context.Background(), sym, featureOpenMomentum30m,
		market.DailyVisibleAt(d6Day))
	require.True(t, ok)
	assert.InDelta(t, 0.05, v, 1e-12)
}

// ─── ⑥ 红线：provider 无运行时数据通道 ───────────────────────────────

// snapshotIntradayProvider 的字段集合就是红线自证：快照 + 日期边界，
// 没有任何 provider / pool / http client 字段。加一个 = 本测试红
// （破坏验证腿②的第二只眼）。
func TestSnapshotIntradayProvider_NoRuntimeDataChannel(t *testing.T) {
	typ := reflect.TypeOf(snapshotIntradayProvider{}) // 值类型：NumField 才可用
	fields := make(map[string]string, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		fields[typ.Field(i).Name] = typ.Field(i).Type.String()
	}
	assert.NotContains(t, fields, "provider", "provider 实现不得持有 marketdata.Provider")
	assert.NotContains(t, fields, "router", "provider 实现不得持有 Router")
	assert.NotContains(t, fields, "pool", "provider 实现不得持有连接池")
	assert.Len(t, fields, 3, "字段集合 = snapshot + dayStart + dayEnd；新增字段须过本测试")
}

// ─── DB 集成：Router → warmIntradayIfNeeded 自灌自证 ─────────────────

// TestEngine_WarmIntraday_WithRouter_SelfSeed（D6 验收⑤ 机械判据的
// 生产路径版）：真 PG + 真 Router 取数 → 快照内容与 VisibleAt 口径。
// 写法遵循 D1 router_integration_test.go 的「自灌自证自清理」惯例，
// DB 不可达则 Skip 不 Fail。
func TestEngine_WarmIntraday_WithRouter_SelfSeed(t *testing.T) {
	const (
		testDSN    = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
		testSymbol = "D6TEST.SH"
	)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Skipf("skipping integration test: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: cannot connect to DB: %v", err)
	}
	_, _ = pool.Exec(ctx, "DELETE FROM ohlcv_minute WHERE symbol = $1", testSymbol)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, "DELETE FROM ohlcv_minute WHERE symbol = $1", testSymbol)
		pool.Close()
	})

	// 灌开盘半小时 30 根（09:30–09:59，close 10.00→10.50）+ 10:00 一根
	// （不进特征窗口，但其可见性 10:01 由快照断言）。
	seed := d6OpenHalfHourBars(testSymbol)
	seed = append(seed, d6MinuteBar(testSymbol, 10, 0, 10.50, 10.52, 10.48, 10.51, 1000))
	for _, b := range seed {
		_, err := pool.Exec(ctx, `
			INSERT INTO ohlcv_minute (symbol, ts, open, high, low, close, volume, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			b.Symbol, b.Date, b.Open, b.High, b.Low, b.Close, int64(b.Volume), b.Turnover)
		require.NoError(t, err)
	}

	eng := d6Engine(t, nil)
	eng.SetRouter(marketdata.NewRouter(pool))
	strat := newIntradayMomentumStrategy("d6_warm_router", 0.02)
	require.NoError(t, strategy.DefaultRegistry.Register(strat))

	require.NoError(t, eng.warmIntradayIfNeeded(ctx, strat.Name(),
		[]string{testSymbol}, d6Day, d6Day))

	snap := eng.intradaySnapshot()
	require.NotNil(t, snap)
	require.Len(t, snap[testSymbol], 31, "30 根半小时 + 1 根 10:00")

	// 规范层口径：频率声明 + VisibleAt 由 Router 填好。
	// 时刻相等用 time.Time.Equal（比 instant）而不是 assert.Equal
	// （比墙钟表述）：pgx 把 TIMESTAMPTZ 扫进本机 location（CET），
	// 同一个 09:31 CST 时刻会显示成 02:31 CET —— instant 一致即可。
	first := snap[testSymbol][0]
	assert.Equal(t, market.Freq1m, first.Frequency)
	assert.True(t, time.Date(2024, 1, 2, 9, 31, 0, 0, market.AsiaShanghai).Equal(first.VisibleAt),
		"09:30 bar 可见点 = 09:31 CST（周期结束），got %s", first.VisibleAt)
	last := snap[testSymbol][30]
	assert.True(t, time.Date(2024, 1, 2, 10, 1, 0, 0, market.AsiaShanghai).Equal(last.VisibleAt),
		"10:00 bar 可见点 = 10:01 CST（分钟粒度闸门的数据基础），got %s", last.VisibleAt)

	// 快照 → 特征：手算样本 0.05。
	prov := eng.intradayFeatureProvider(d6Day)
	v, ok := prov.IntradayFeature(ctx, testSymbol, featureOpenMomentum30m, market.DailyVisibleAt(d6Day))
	require.True(t, ok)
	assert.InDelta(t, 0.05, v, 1e-12)
}
