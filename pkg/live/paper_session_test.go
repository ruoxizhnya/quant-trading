package live

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 假 Provider（只覆盖 PaperSession 用到的两个方法） ─────────────

type fakeProvider struct {
	bars map[string][]domain.OHLCV
	days []time.Time
}

func (p *fakeProvider) Name() string                            { return "fake" }
func (p *fakeProvider) CheckConnectivity(context.Context) error { return nil }
func (p *fakeProvider) GetFundamental(context.Context, string, time.Time) (*domain.Fundamental, error) {
	return nil, nil
}
func (p *fakeProvider) GetStocks(context.Context, string) ([]domain.Stock, error) { return nil, nil }
func (p *fakeProvider) GetLatestPrice(context.Context, string) (float64, error)   { return 0, nil }
func (p *fakeProvider) GetIndexConstituents(context.Context, string) ([]string, error) {
	return nil, nil
}
func (p *fakeProvider) GetStock(context.Context, string) (domain.Stock, error) {
	return domain.Stock{}, nil
}
func (p *fakeProvider) CheckCalendarExists(context.Context, time.Time, time.Time) (bool, error) {
	return true, nil
}

func (p *fakeProvider) GetOHLCV(_ context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	var out []domain.OHLCV
	for _, b := range p.bars[symbol] {
		if !b.Date.Before(start) && !b.Date.After(end) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (p *fakeProvider) GetTradingDays(_ context.Context, start, end time.Time) ([]time.Time, error) {
	var out []time.Time
	for _, d := range p.days {
		if !d.Before(start) && !d.After(end) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (p *fakeProvider) BulkLoadOHLCV(_ context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	out := map[string][]domain.OHLCV{}
	for _, s := range symbols {
		for _, b := range p.bars[s] {
			if !b.Date.Before(start) && !b.Date.After(end) {
				out[s] = append(out[s], b)
			}
		}
	}
	return out, nil
}

// ─── 探针策略 ─────────────────────────────────────────────────────

// probeBatch 是批式探针：记录每次 GenerateSignals 看到的**最大 bar 日期**，
// 并在每个交易日买 1000 股 buySymbol。防前视测试断言第 k 次看到的最大日期
// 永远不超过第 k 个交易日。
type probeBatch struct {
	*strategy.BaseStrategy
	buySymbol string
	seenMax   []time.Time
}

func newProbeBatch(sym string) *probeBatch {
	return &probeBatch{BaseStrategy: strategy.NewBaseStrategy("probe_batch", "K5 anti-lookahead probe"), buySymbol: sym}
}

func (p *probeBatch) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	var maxT time.Time
	for _, sym := range sortedKeys(bars) {
		for _, b := range bars[sym] {
			if b.Date.After(maxT) {
				maxT = b.Date
			}
		}
	}
	p.seenMax = append(p.seenMax, maxT)
	return []strategy.Signal{{
		Symbol: p.buySymbol, Action: "buy", Direction: domain.DirectionLong, Strength: 1.0,
	}}, nil
}

func (p *probeBatch) Weight(strategy.Signal, float64) float64 { return 1.0 }

// probeStream 是流式探针：记录每根 OnBar 的 (date, symbol)，据此断言喂序
// 与防前视；Signals() 每次返回一笔买 signal。
type probeStream struct {
	*strategy.BaseStrategy
	buySymbol string
	barsSeen  []domain.OHLCV
	pending   []strategy.Signal
}

func newProbeStream(sym string) *probeStream {
	return &probeStream{BaseStrategy: strategy.NewBaseStrategy("probe_stream", "K5 streaming probe"), buySymbol: sym}
}

func (p *probeStream) OnBar(_ context.Context, bar domain.OHLCV) error {
	p.barsSeen = append(p.barsSeen, bar)
	p.pending = append(p.pending, strategy.Signal{
		Symbol: p.buySymbol, Action: "buy", Direction: domain.DirectionLong, Strength: 1.0,
	})
	return nil
}
func (p *probeStream) Warmup() int                { return 0 }
func (p *probeStream) SaveState() ([]byte, error) { return nil, nil }
func (p *probeStream) LoadState([]byte) error     { return nil }
func (p *probeStream) Signals() []strategy.Signal { s := p.pending; p.pending = nil; return s }
func (p *probeStream) GenerateSignals(context.Context, map[string][]domain.OHLCV, *domain.Portfolio) ([]strategy.Signal, error) {
	return nil, nil
}
func (p *probeStream) Weight(strategy.Signal, float64) float64 { return 1.0 }

// ─── 测试数据 ─────────────────────────────────────────────────────

var (
	testDays = []time.Time{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC),
	}
	testSyms = []string{"000001.SZ", "600000.SH"}
)

func testProvider() *fakeProvider {
	p := &fakeProvider{days: testDays, bars: map[string][]domain.OHLCV{}}
	closes := map[string][]float64{
		"000001.SZ": {10, 11, 12},
		"600000.SH": {20, 21, 22},
	}
	for _, sym := range testSyms {
		for i, d := range testDays {
			c := closes[sym][i]
			p.bars[sym] = append(p.bars[sym], domain.OHLCV{
				Symbol: sym, Date: d, Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 100000,
			})
		}
	}
	return p
}

func newPaperTrader(t *testing.T) *MockTrader {
	t.Helper()
	return NewMockTrader(MockTraderConfig{
		InitialCash:   1_000_000,
		SlippageModel: "fixed", // 走共享成本核
	}, zerolog.Nop())
}

func newSession(t *testing.T, prov marketdata.Provider, trader LiveTrader) *PaperSession {
	t.Helper()
	s, err := NewPaperSession(PaperSessionConfig{
		Provider: prov,
		Clock:    clock.NewVirtualClock(testDays[0]),
		Trader:   trader,
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)
	return s
}

// TestPaperSession_NoLookahead_Batch 是防前视的**核心正证据（批式）**：
// 第 k 个交易日调用 GenerateSignals 时，窗口里看到的**最大日期必须不超过
// 第 k 日**。把窗口从「Date<=d」改成「全量」会让本用例立刻变红。
func TestPaperSession_NoLookahead_Batch(t *testing.T) {
	prov := testProvider()
	probe := newProbeBatch("000001.SZ")
	s := newSession(t, prov, newPaperTrader(t))

	fills, err := s.Run(context.Background(), testSyms, testDays[0], testDays[2], probe)
	require.NoError(t, err)
	require.Len(t, probe.seenMax, len(testDays), "每个交易日调用一次 GenerateSignals")

	for k, seen := range probe.seenMax {
		t.Logf("第 %d 日(%s): 窗口最大日期=%s", k+1, testDays[k].Format("2006-01-02"), seen.Format("2006-01-02"))
		assert.False(t, seen.After(testDays[k]),
			"第 %d 日不得看到 > 当日的 bar（防前视）", k+1)
		assert.Equal(t, testDays[k], seen, "窗口最大日期应恰为当日")
	}
	require.NotEmpty(t, fills)
}

// TestPaperSession_NoLookahead_Stream 是防前视的**核心正证据（流式）**：
// OnBar 收到的 bar 恰好按 (日升序 × symbol 字典序) 出现，且不越过当日。
func TestPaperSession_NoLookahead_Stream(t *testing.T) {
	prov := testProvider()
	probe := newProbeStream("000001.SZ")
	s := newSession(t, prov, newPaperTrader(t))

	_, err := s.Run(context.Background(), testSyms, testDays[0], testDays[2], probe)
	require.NoError(t, err)

	// 每日 2 票 → 6 次 OnBar，顺序为 每日内 symbol 字典序、日间升序。
	require.Len(t, probe.barsSeen, len(testDays)*len(testSyms))
	want := []struct {
		date time.Time
		sym  string
	}{
		{testDays[0], "000001.SZ"}, {testDays[0], "600000.SH"},
		{testDays[1], "000001.SZ"}, {testDays[1], "600000.SH"},
		{testDays[2], "000001.SZ"}, {testDays[2], "600000.SH"},
	}
	var lastDate time.Time
	for i, b := range probe.barsSeen {
		t.Logf("OnBar[%d] %s %s", i, b.Date.Format("2006-01-02"), b.Symbol)
		assert.Equal(t, want[i].date, b.Date, "喂序：日升序")
		assert.Equal(t, want[i].sym, b.Symbol, "喂序：symbol 字典序")
		assert.False(t, b.Date.Before(lastDate), "OnBar 日期必须非递减（防前视）")
		lastDate = b.Date
	}
}

// TestPaperSession_RunDeterministicAndSorted 冒烟 + 确定性：同一输入两次 Run
// 产出逐条一致的成交流，且按 (Date, Symbol) 有序。
func TestPaperSession_RunDeterministicAndSorted(t *testing.T) {
	run := func() []PaperFill {
		prov := testProvider()
		s := newSession(t, prov, newPaperTrader(t))
		fills, err := s.Run(context.Background(), testSyms, testDays[0], testDays[2], newProbeBatch("000001.SZ"))
		require.NoError(t, err)
		return fills
	}

	first := run()
	second := run()

	require.NotEmpty(t, first)
	assert.Equal(t, first, second, "同一输入两次 Run 的成交流必须逐条一致")

	for i, f := range first {
		t.Logf("fill[%d] %s %s side=%s qty=%.0f price=%.4f fee=%.4f",
			i, f.Date.Format("2006-01-02"), f.Symbol, f.Side, f.Qty, f.Price, f.Fee)
	}
	for i := 1; i < len(first); i++ {
		prev, cur := first[i-1], first[i]
		if cur.Date.Equal(prev.Date) {
			assert.LessOrEqual(t, prev.Symbol, cur.Symbol, "同日必须 symbol 升序")
		} else {
			assert.True(t, prev.Date.Before(cur.Date), "日期必须升序")
		}
	}
}

// TestPaperSession_RequiresDeps 依赖注入 fail-loud：缺 Provider/Clock/Trader
// 一律报错，不静默补默认（尤其不得静默用墙钟）。
func TestPaperSession_RequiresDeps(t *testing.T) {
	_, err := NewPaperSession(PaperSessionConfig{})
	require.Error(t, err)

	_, err = NewPaperSession(PaperSessionConfig{Provider: testProvider()})
	require.Error(t, err, "缺 Clock 必须报错（不得静默墙钟）")

	_, err = NewPaperSession(PaperSessionConfig{Provider: testProvider(), Clock: clock.NewVirtualClock(testDays[0])})
	require.Error(t, err, "缺 Trader 必须报错")
}
