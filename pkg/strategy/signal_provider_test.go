// K8 · SignalProvider 契约测试。
//
// ─── 破坏验证（TASKS.md K8 ⑤）────────────────────────────────────────
//  腿①：LocalSignalProvider 人为放行 as_of > asOf 的信号 →
//        TestLocalSignalProviderRejectsFutureDatedOutput 变红
//        （守卫是 Signals 输出前的 RejectFutureSignals 调用）。
//  腿②：删 SignalProvider 接口任一方法 → signal_provider.go 的方法表达式
//        守卫编译期变红（测试文件无需改动，编译即验证）。
package strategy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/require"
)

// ─── 测试替身（自造自用，不共享 DefaultRegistry）────────────────────

// pvBatch 批式替身：记录注入与入参，信号由 gen 决定。
type pvBatch struct {
	name       string
	gen        func(bars map[string][]domain.OHLCV, p *domain.Portfolio) ([]Signal, error)
	gotBars    map[string][]domain.OHLCV
	gotPort    *domain.Portfolio
	gotFactor  FactorZScoreReader
	gotFund    map[string]FundamentalSeries
	fundCalls  int
	factorSeen bool
}

func (s *pvBatch) Name() string                   { return s.name }
func (s *pvBatch) Description() string            { return "" }
func (s *pvBatch) Parameters() []Parameter        { return nil }
func (s *pvBatch) Configure(map[string]any) error { return nil }
func (s *pvBatch) Weight(Signal, float64) float64 { return 1 }
func (s *pvBatch) Cleanup()                       {}

func (s *pvBatch) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, p *domain.Portfolio) ([]Signal, error) {
	s.gotBars, s.gotPort = bars, p
	if s.gen != nil {
		return s.gen(bars, p)
	}
	return nil, nil
}

func (s *pvBatch) SetFactorCache(r FactorZScoreReader) {
	s.gotFactor, s.factorSeen = r, true
}
func (s *pvBatch) SetFundamentals(m map[string]FundamentalSeries) {
	s.gotFund, s.fundCalls = m, s.fundCalls+1
}

// pvStream 流式替身（Strategy + BarHandler 双实现）：记录喂入的 bar 序列。
type pvStream struct {
	pvBatch
	fed []domain.OHLCV
	out []Signal
}

func (s *pvStream) OnBar(_ context.Context, bar domain.OHLCV) error {
	s.fed = append(s.fed, bar)
	return nil
}
func (s *pvStream) Warmup() int               { return 0 }
func (s *pvStream) SaveState() ([]byte, error) { return nil, nil }
func (s *pvStream) LoadState([]byte) error     { return nil }
func (s *pvStream) Signals() []Signal {
	got := s.out
	s.out = nil // 取走即清空
	return got
}

// pvBars 构造 n 根日线，最后一根落在 day。
func pvBars(symbol string, lastDay time.Time, n int) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, n)
	for i := n - 1; i >= 0; i-- {
		bars = append(bars, domain.OHLCV{
			Symbol: symbol,
			Date:   lastDay.AddDate(0, 0, -i),
			Close:  10,
		})
	}
	return bars
}

// ─── RejectFutureSignals（DayOf 日粒度）────────────────────────────

func TestRejectFutureSignalsDayGranularity(t *testing.T) {
	asOf := time.Date(2026, 1, 10, 9, 30, 0, 0, time.UTC)

	require.NoError(t, RejectFutureSignals(nil, asOf), "空信号集不该报错")
	require.NoError(t, RejectFutureSignals([]Signal{
		{Symbol: "A", Date: nil},                          // 无锚点 → 跳过
		{Symbol: "B", Date: "not-a-time"},                 // 非 time.Time → 跳过
		{Symbol: "C", Date: time.Time{}},                  // 零值 → 跳过
		{Symbol: "D", Date: asOf.AddDate(0, 0, -3)},       // 过去
		{Symbol: "E", Date: asOf.Add(6 * time.Hour)},      // 同日晚些时刻（日粒度 = 同日）
		{Symbol: "F", Date: asOf},                         // 正是 asOf
	}, asOf), "同日/过去/无锚点信号都必须放行")

	err := RejectFutureSignals([]Signal{{Symbol: "X", Date: asOf.AddDate(0, 0, 1)}}, asOf)
	require.Error(t, err, "未来信号必须 fail-loud（腿①的最小口径）")
	require.Contains(t, err.Error(), "防前视")
	require.Contains(t, err.Error(), "X", "拒绝要能定位到标的")
}

// ─── LocalSignalProvider ───────────────────────────────────────────

// TestLocalSignalProviderRejectsFutureDatedOutput 是破坏验证腿①：策略产出
// 未来日期信号时，Signals() 必须报错而不是放行。删掉 Signals 输出前的
// RejectFutureSignals 调用 → 本测试变红。
func TestLocalSignalProviderRejectsFutureDatedOutput(t *testing.T) {
	asOf := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	reg := NewRegistry()
	future := &pvBatch{name: "pv-future", gen: func(map[string][]domain.OHLCV, *domain.Portfolio) ([]Signal, error) {
		return []Signal{{Symbol: "X", Date: asOf.AddDate(0, 0, 2)}}, nil
	}}
	require.NoError(t, reg.Register(future))

	p := NewLocalSignalProvider(reg)
	_, err := p.Signals(context.Background(), Scope{Strategy: "pv-future"}, asOf)
	require.Error(t, err, "未来信号必须被契约拒绝（防前视），而不是静默放行")
}

// TestLocalSignalProviderBatchPassthrough 批式路径透传：信号原样返回，
// 调用入参（marketData / portfolio）原样到达策略。
func TestLocalSignalProviderBatchPassthrough(t *testing.T) {
	asOf := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	want := []Signal{
		{Symbol: "A", Direction: domain.DirectionLong, Strength: 0.5, Date: asOf.AddDate(0, 0, -1)},
		{Symbol: "B", Direction: domain.DirectionClose}, // Date 零值也必须透传
	}
	reg := NewRegistry()
	batch := &pvBatch{name: "pv-batch", gen: func(map[string][]domain.OHLCV, *domain.Portfolio) ([]Signal, error) {
		return want, nil
	}}
	require.NoError(t, reg.Register(batch))

	bars := map[string][]domain.OHLCV{"A": pvBars("A", asOf, 3)}
	port := &domain.Portfolio{Cash: 1000}
	got, err := NewLocalSignalProvider(reg).Signals(context.Background(),
		Scope{Strategy: "pv-batch", MarketData: bars, Portfolio: port}, asOf)
	require.NoError(t, err)
	require.Equal(t, want, got, "批式输出必须逐字段原样透传")
	require.Equal(t, bars, batch.gotBars, "marketData 必须原样到达策略")
	require.Same(t, port, batch.gotPort, "portfolio 必须原样到达策略")
}

// TestLocalSignalProviderStreamingFeedsOnlyTodayBars 流式路径：只喂「当日」
// bar（停牌标的的陈旧 bar 不喂），喂序按 symbol 字典序（喂序确定性）。
func TestLocalSignalProviderStreamingFeedsOnlyTodayBars(t *testing.T) {
	asOf := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	reg := NewRegistry()
	stream := &pvStream{pvBatch: pvBatch{name: "pv-stream"}}
	stream.out = []Signal{{Symbol: "AAA", Date: asOf}}
	require.NoError(t, reg.Register(stream))

	bars := map[string][]domain.OHLCV{
		"AAA": pvBars("AAA", asOf, 3),          // 当日有新 bar
		"BBB": pvBars("BBB", asOf.AddDate(0, 0, -2), 3), // 停牌：最后一根是前天
		"CCC": {},                              // 空历史
	}
	got, err := NewLocalSignalProvider(reg).Signals(context.Background(),
		Scope{Strategy: "pv-stream", MarketData: bars}, asOf)
	require.NoError(t, err)
	require.Len(t, stream.fed, 1, "只喂当日 bar：停牌/空历史标的不喂陈旧 bar")
	require.Equal(t, "AAA", stream.fed[0].Symbol)
	require.Equal(t, asOf, stream.fed[0].Date)
	require.Equal(t, []Signal{{Symbol: "AAA", Date: asOf}}, got, "Signals() 取走的信号必须原样返回")

	// 取走即清空：再次调用（无新信号）不重复吐出上一日的信号。
	got2, err := NewLocalSignalProvider(reg).Signals(context.Background(),
		Scope{Strategy: "pv-stream", MarketData: bars}, asOf)
	require.NoError(t, err)
	require.Empty(t, got2, "取走即清空，防重复消费")
}

// TestLocalSignalProviderRegistryMissFailLoud 注册表 miss 必须 fail-loud，
// 且错误消息与旧 getSignals 逐字节同源（OBS-11 口径）。
func TestLocalSignalProviderRegistryMissFailLoud(t *testing.T) {
	p := NewLocalSignalProvider(NewRegistry())
	_, err := p.Signals(context.Background(), Scope{Strategy: "no-such-strategy"}, time.Now())
	require.Error(t, err, "未注册策略必须报错")
	msg := err.Error()
	require.Contains(t, msg, "no-such-strategy", "错误必须点名策略")
	require.Contains(t, msg, "strategy \"no-such-strategy\" not registered (strategy-service signal fallback removed per OBS-11/ADR-012)",
		"错误消息必须与旧 getSignals 逐字节一致")
	require.NotContains(t, strings.ToLower(msg), "503")
	require.NotContains(t, strings.ToLower(msg), "http", "错误里不得有已删 HTTP fallback 的痕迹")
}

// TestLocalSignalProviderInjectsFactorAndFundamentals FactorAware /
// FundamentalAware 注入与旧实现同节奏（每次调用注入一次）。
func TestLocalSignalProviderInjectsFactorAndFundamentals(t *testing.T) {
	asOf := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	reg := NewRegistry()
	batch := &pvBatch{name: "pv-inject"}
	require.NoError(t, reg.Register(batch))

	reader := FactorZScoreReader(func(domain.FactorType, time.Time, string) (float64, bool) { return 0, false })
	fund := map[string]FundamentalSeries{"A": {}}

	p := NewLocalSignalProvider(reg)
	p.SetFactorReader(reader)
	p.SetFundamentalsProvider(func() map[string]FundamentalSeries { return fund })

	for i := 0; i < 2; i++ {
		_, err := p.Signals(context.Background(), Scope{Strategy: "pv-inject"}, asOf)
		require.NoError(t, err)
	}
	require.True(t, batch.factorSeen, "FactorAware 必须拿到 factorReader")
	require.Equal(t, 2, batch.fundCalls, "FundamentalAware 每次调用注入一次（同旧实现）")
	require.Equal(t, fund, batch.gotFund)
}
