// K2 切片 1：流式运行时基座（StreamRunner / BatchAdapter）测试 +
// ADR-028 §7「Batch ≡ Step ≡ Step-from-persisted」三路一致性落测。
//
// 落位裁决：本包既有的 strategy_test.go / loader_test.go / registry_test.go /
// copilot_test.go / db_test.go 都是 `package strategy`（内部测试），
// streaming_compliance_test.go / interfaces_compliance_test.go 是
// `package strategy_test`（外部）。本文件用内部包，直接引用 Signal /
// BatchAdapter / StreamRunner，不绕限定名；测试辅助类型一律加 rt 前缀
// 避免与既有测试的同包标识符重名。
package strategy

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/require"
)

// ─── 测试辅助 ──────────────────────────────────────────────────────

// rtBaseDate 是测试序列的时间原点（UTC）。
var rtBaseDate = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// rtMakeBars 造一串同一 symbol 的 bar，时间按小时升序递推。
func rtMakeBars(symbol string, closes []float64) []domain.OHLCV {
	bars := make([]domain.OHLCV, len(closes))
	for i, c := range closes {
		bars[i] = domain.OHLCV{
			Symbol: symbol,
			Date:   rtBaseDate.Add(time.Duration(i) * time.Hour),
			Close:  c,
		}
	}
	return bars
}

// rtCloses 抽取一串 bar 的 Close，做值比较（避免直接比 time.Time 的坑）。
func rtCloses(bars []domain.OHLCV) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

// rtCopyBars 深拷贝一串 bar（快照语义，防别名）。
func rtCopyBars(bars []domain.OHLCV) []domain.OHLCV {
	cp := make([]domain.OHLCV, len(bars))
	copy(cp, bars)
	return cp
}

// rtSignalSeq 把「每根 bar 一次的信号集」拍平成可比较的字符串序列。
// 只取结构性字段，绕开 map / interface{} 的 DeepEqual 噪音。
func rtSignalSeq(seq [][]Signal) []string {
	out := make([]string, 0)
	for _, set := range seq {
		for _, s := range set {
			out = append(out, s.Symbol+"|"+s.Action+"|"+
				strconv.FormatFloat(s.Strength, 'g', -1, 64)+"|"+
				strconv.FormatFloat(s.Price, 'g', -1, 64))
		}
	}
	return out
}

// ─── 测试 1：StreamRunner 基本驱动 + 可复现 ─────────────────────────

// rtTraceHandler 是带递推状态的 fake BarHandler：累计和 + 每根后的轨迹。
type rtTraceHandler struct {
	sum   float64
	n     int
	trace []float64
}

func (h *rtTraceHandler) OnBar(_ context.Context, bar domain.OHLCV) error {
	h.sum += bar.Close
	h.n++
	h.trace = append(h.trace, h.sum)
	return nil
}

func (h *rtTraceHandler) Warmup() int { return 0 }

func (h *rtTraceHandler) SaveState() ([]byte, error) {
	return json.Marshal(map[string]any{"sum": h.sum, "n": h.n})
}

func (h *rtTraceHandler) LoadState(b []byte) error {
	var s struct {
		Sum float64 `json:"sum"`
		N   int     `json:"n"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	h.sum, h.n = s.Sum, s.N
	return nil
}

// TestStreamRunnerDrivesBarHandlerAndIsReproducible 验证：
//   - 逐 bar 驱动后内部递推状态正确；
//   - 时钟被推进到最后一根 bar 的时间；
//   - 同一输入两次跑，喂给 OnBar 的轨迹逐点一致（可复现）。
func TestStreamRunnerDrivesBarHandlerAndIsReproducible(t *testing.T) {
	ctx := context.Background()
	bars := rtMakeBars("AAA", []float64{1, 2, 3, 4, 5})

	h1 := &rtTraceHandler{}
	clk1 := clock.NewVirtualClock(bars[0].Date)
	require.NoError(t, NewStreamRunner().Run(ctx, h1, bars, clk1))

	// 递推状态正确：sum = 1+2+3+4+5 = 15，轨迹是前缀和。
	require.Equal(t, 5, h1.n)
	require.Equal(t, 15.0, h1.sum)
	require.Equal(t, []float64{1, 3, 6, 10, 15}, h1.trace)

	// 时钟推进到最后一根 bar 的时间。
	require.True(t, clk1.Now().Equal(bars[len(bars)-1].Date),
		"时钟应停在最后一根 bar 时间, got %s want %s", clk1.Now(), bars[len(bars)-1].Date)

	// 可复现：第二次跑轨迹逐点一致。
	h2 := &rtTraceHandler{}
	clk2 := clock.NewVirtualClock(bars[0].Date)
	require.NoError(t, NewStreamRunner().Run(ctx, h2, bars, clk2))
	require.Equal(t, h1.trace, h2.trace, "同一输入两次跑的轨迹必须逐点一致")
	require.Equal(t, h1.sum, h2.sum)
}

// ─── 测试 2：时间倒退护栏（立即停，不喂后续 bar） ───────────────────

// TestStreamRunnerStopsImmediatelyOnTimeRewind 构造一个让 VirtualClock
// 倒退的 bars 序列，断言 Run 返回 error（且可 errors.Is 到
// clock.ErrTimeRewind），且**倒退那根及其后所有 bar 都未被喂**。
func TestStreamRunnerStopsImmediatelyOnTimeRewind(t *testing.T) {
	ctx := context.Background()
	base := rtBaseDate
	bars := []domain.OHLCV{
		{Symbol: "AAA", Date: base, Close: 1},
		{Symbol: "AAA", Date: base.Add(time.Hour), Close: 2},
		{Symbol: "AAA", Date: base.Add(-time.Hour), Close: 100}, // ← 倒退
		{Symbol: "AAA", Date: base.Add(2 * time.Hour), Close: 4},
	}

	h := &rtTraceHandler{}
	clk := clock.NewVirtualClock(bars[0].Date)
	err := NewStreamRunner().Run(ctx, h, bars, clk)

	require.Error(t, err, "时间倒退必须 fail-loud")
	require.ErrorIs(t, err, clock.ErrTimeRewind, "error 应可 Is 到 clock.ErrTimeRewind（说明就是倒退而非别的错）")

	// 立即停：只喂了前两根；倒退那根（close=100）没进去。
	require.Equal(t, 2, h.n, "倒退那根及其后 bar 不得被喂")
	require.Equal(t, 3.0, h.sum, "sum 只能是 1+2，不得含倒退那根的 100")
	require.Equal(t, []float64{1, 3}, h.trace)

	// 时钟停在第二根时间（Advance 失败不留痕迹）。
	require.True(t, clk.Now().Equal(bars[1].Date),
		"时钟应停在失败前最后一次成功推进的时间, got %s", clk.Now())
}

// ─── 测试 3：BatchAdapter 攒窗口（Warmup 前零调用） ─────────────────

// rtCountGen 是计数用的 fake SignalGenerator（记录调用次数与最后一次的窗口）。
type rtCountGen struct {
	calls         int
	lastBars      map[string][]domain.OHLCV
	lastPortfolio *domain.Portfolio
}

func (g *rtCountGen) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, p *domain.Portfolio) ([]Signal, error) {
	g.calls++
	cp := make(map[string][]domain.OHLCV, len(bars))
	for s, w := range bars {
		cp[s] = rtCopyBars(w)
	}
	g.lastBars = cp
	g.lastPortfolio = p
	return []Signal{{Symbol: "x"}}, nil
}

func (g *rtCountGen) Weight(Signal, float64) float64 { return 1 }

// TestBatchAdapterGatesOnWarmupWindow 验证：
//   - Warmup() == window；
//   - 窗口未满时 GenerateSignals **零调用**、LastSignals 为 nil；
//   - 凑满后每根都调，且传入窗口正是「最近 window 根」。
func TestBatchAdapterGatesOnWarmupWindow(t *testing.T) {
	ctx := context.Background()
	bars := rtMakeBars("AAA", []float64{10, 11, 12, 13, 14})
	const window = 3

	gen := &rtCountGen{}
	ad := NewBatchAdapter(gen, []string{"AAA"}, window)
	require.Equal(t, window, ad.Warmup())

	// 前 window-1 根：未满，不得触发。
	for i := 0; i < window-1; i++ {
		require.NoError(t, ad.OnBar(ctx, bars[i]))
		require.Equal(t, 0, gen.calls, "窗口未满不得调 GenerateSignals (i=%d)", i)
		require.Nil(t, ad.LastSignals(), "窗口未满前不得有信号 (i=%d)", i)
	}

	// 第 window 根：首次触发，窗口 = bars[0:3]。
	require.NoError(t, ad.OnBar(ctx, bars[window-1]))
	require.Equal(t, 1, gen.calls, "凑满 window 后应首次调用")
	require.Equal(t, []float64{10, 11, 12}, rtCloses(gen.lastBars["AAA"]),
		"传入窗口应是最近 window 根")
	require.NotNil(t, ad.LastSignals(), "首次触发后应有信号")

	// 后续每根都触发，窗口滚动到「最近 window 根」。
	for i := window; i < len(bars); i++ {
		require.NoError(t, ad.OnBar(ctx, bars[i]))
	}
	require.Equal(t, len(bars)-window+1, gen.calls, "每根 bar 触发一次")
	require.Equal(t, []float64{12, 13, 14}, rtCloses(gen.lastBars["AAA"]),
		"最后一窗应是最近 window 根 (bars[2:5])")
}

// TestBatchAdapterMultiSymbolRequiresAllWindowsFull 验证多 symbol 就绪门：
// 必须 universe 内**每个** symbol 都满窗口才触发（横截面对齐语义）。
func TestBatchAdapterMultiSymbolRequiresAllWindowsFull(t *testing.T) {
	ctx := context.Background()
	a := rtMakeBars("AAA", []float64{1, 2, 3})
	b := rtMakeBars("BBB", []float64{4, 5, 6})

	gen := &rtCountGen{}
	ad := NewBatchAdapter(gen, []string{"AAA", "BBB"}, 2)

	require.NoError(t, ad.OnBar(ctx, a[0]))
	require.NoError(t, ad.OnBar(ctx, a[1])) // AAA 满，BBB 空 → 不触发
	require.Equal(t, 0, gen.calls, "只有 AAA 满不得触发（需全 symbol 都满）")

	require.NoError(t, ad.OnBar(ctx, b[0])) // BBB 才 1 根 → 仍不触发
	require.Equal(t, 0, gen.calls)

	require.NoError(t, ad.OnBar(ctx, b[1])) // 两个都满 → 触发
	require.Equal(t, 1, gen.calls)
	require.Len(t, gen.lastBars, 2, "快照应含全部 universe symbol")
	require.Equal(t, []float64{1, 2}, rtCloses(gen.lastBars["AAA"]))
	require.Equal(t, []float64{4, 5}, rtCloses(gen.lastBars["BBB"]))
}

// TestBatchAdapterEmptyUniverseIsInert 验证：空 universe = inert（恒不触发）。
func TestBatchAdapterEmptyUniverseIsInert(t *testing.T) {
	ctx := context.Background()
	gen := &rtCountGen{}
	ad := NewBatchAdapter(gen, nil, 2)
	for _, bar := range rtMakeBars("AAA", []float64{1, 2, 3, 4}) {
		require.NoError(t, ad.OnBar(ctx, bar))
	}
	require.Equal(t, 0, gen.calls, "空 universe 应恒不触发（无横截面则无信号）")
	require.Nil(t, ad.LastSignals())
}

// TestStreamRunnerDrivesBatchAdapter 验证两个交付物协同：用 StreamRunner
// 逐 bar 驱动 BatchAdapter，调用次数应为 len-window+1（同构桥接进流式引擎）。
func TestStreamRunnerDrivesBatchAdapter(t *testing.T) {
	ctx := context.Background()
	bars := rtMakeBars("AAA", []float64{1, 2, 3, 4, 5, 6})

	gen := &rtCountGen{}
	ad := NewBatchAdapter(gen, []string{"AAA"}, 3)
	clk := clock.NewVirtualClock(bars[0].Date)

	require.NoError(t, NewStreamRunner().Run(ctx, ad, bars, clk))
	require.Equal(t, len(bars)-3+1, gen.calls)
}

// ─── 测试 4：三路一致性 Batch ≡ Step ≡ Step-from-persisted ───────────

// rtWindowGen 是确定性的批式策略：纯函数（只依赖传入的窗口），
// 每 symbol 产一根信号，Action 由「最后一根 vs 前一根」的收盘比较决定。
// 纯函数是「Batch ≡ Step」可实现的前提——同构桥的全部赌注就在这。
type rtWindowGen struct{ calls int }

func (g *rtWindowGen) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]Signal, error) {
	g.calls++
	syms := make([]string, 0, len(bars))
	for s := range bars {
		syms = append(syms, s)
	}
	sort.Strings(syms)

	var out []Signal
	for _, s := range syms {
		w := bars[s]
		if len(w) == 0 {
			continue
		}
		last := w[len(w)-1]
		prev := last
		if len(w) >= 2 {
			prev = w[len(w)-2]
		}
		action := "hold"
		switch {
		case last.Close > prev.Close:
			action = "buy"
		case last.Close < prev.Close:
			action = "sell"
		}
		out = append(out, Signal{
			Symbol:   s,
			Action:   action,
			Strength: last.Close,
			Price:    last.Close,
			Date:     last.Date,
		})
	}
	return out, nil
}

func (g *rtWindowGen) Weight(Signal, float64) float64 { return 1 }

// TestThreeWayConsistencyBatchStepStepFromPersisted 是 ADR-028 §7 的落测：
//
//	Batch(全序列)[t] ≡ Step 从零累积到 t ≡ Step 从持久化状态恢复到 t
//
// 三条路：
//
//	(a) Batch —— 回测式：直接从全序列按窗口切片调 GenerateSignals；
//	(b) Step  —— 实盘式：逐 bar 喂 BatchAdapter；
//	(c) Step-from-persisted —— 喂一半 → SaveState → 新实例 LoadState → 喂完。
//
// 断言：三路信号序列一致；且 (b) 与 (c) 的终态序列化字节相等。
func TestThreeWayConsistencyBatchStepStepFromPersisted(t *testing.T) {
	ctx := context.Background()
	const sym = "AAA"
	const window = 3
	const half = 5

	closes := []float64{10, 11, 10.5, 12, 11, 13, 12.5, 14} // 有涨有跌，覆盖 buy/sell/hold
	bars := rtMakeBars(sym, closes)
	require.Greater(t, half, window, "半程点必须晚于 warmup，否则第一段无信号")

	// ── (a) Batch：回测式，每 t 从全序列切出最近 window 根 ──
	genA := &rtWindowGen{}
	var seqA [][]Signal
	for i := 0; i < len(bars); i++ {
		if i < window-1 {
			continue // 未就绪，与适配器就绪门一致
		}
		w := rtCopyBars(bars[i-window+1 : i+1])
		sigs, err := genA.GenerateSignals(ctx, map[string][]domain.OHLCV{sym: w}, nil)
		require.NoError(t, err)
		seqA = append(seqA, sigs)
	}

	// ── (b) Step：逐 bar 喂适配器，每根后取 LastSignals ──
	genB := &rtWindowGen{}
	adB := NewBatchAdapter(genB, []string{sym}, window)
	var seqB [][]Signal
	for i := 0; i < len(bars); i++ {
		require.NoError(t, adB.OnBar(ctx, bars[i]))
		if s := adB.LastSignals(); s != nil {
			seqB = append(seqB, s)
		}
	}

	// ── (c) Step-from-persisted：喂一半 → SaveState → 新实例 LoadState → 喂完 ──
	genC := &rtWindowGen{}
	adC := NewBatchAdapter(genC, []string{sym}, window)
	var seqC [][]Signal
	for i := 0; i < half; i++ {
		require.NoError(t, adC.OnBar(ctx, bars[i]))
		if s := adC.LastSignals(); s != nil {
			seqC = append(seqC, s)
		}
	}
	blob, err := adC.SaveState()
	require.NoError(t, err)

	adC2 := NewBatchAdapter(genC, []string{sym}, window)
	require.NoError(t, adC2.LoadState(blob), "合法状态必须能加载")
	// 注意：不在 LoadState 后立刻记录（否则会重复半程最后一根的信号）；
	// 只记录 LoadState 之后喂入的 bar 所产生的信号。
	for i := half; i < len(bars); i++ {
		require.NoError(t, adC2.OnBar(ctx, bars[i]))
		seqC = append(seqC, adC2.LastSignals())
	}

	// 三路信号序列一致。
	require.Equal(t, len(bars)-window+1, len(seqA), "信号集数量 = len-window+1")
	require.NotEmpty(t, rtSignalSeq(seqA))
	require.Equal(t, rtSignalSeq(seqA), rtSignalSeq(seqB), "(a) Batch 与 (b) Step 信号序列必须一致")
	require.Equal(t, rtSignalSeq(seqA), rtSignalSeq(seqC), "(a) Batch 与 (c) Step-from-persisted 信号序列必须一致")

	// 终态一致：(b) 与 (c) 两条适配器的序列化状态字节相等。
	blobB, err := adB.SaveState()
	require.NoError(t, err)
	blobC, err := adC2.SaveState()
	require.NoError(t, err)
	require.Equal(t, string(blobB), string(blobC),
		"(b) 与 (c) 的终态必须逐字节一致（窗口 + 最近信号都对齐）")
}

// ─── 测试 5：LoadState fail-loud（不静默重置） ─────────────────────

// TestBatchAdapterLoadStateFailsLoud 验证：
//   - 垃圾字节 / 空字节 / null 字面量 / 版本不符 / window 非法 → 全部返回 error；
//   - 失败后适配器状态**一字不动**（不得被静默重置为初始态）；
//   - 合法状态可往返（新实例 LoadState → SaveState 字节稳定）。
func TestBatchAdapterLoadStateFailsLoud(t *testing.T) {
	ctx := context.Background()
	const sym = "AAA"
	bars := rtMakeBars(sym, []float64{1, 2, 3, 4})

	ad := NewBatchAdapter(&rtWindowGen{}, []string{sym}, 3)
	for i := 0; i < 3; i++ {
		require.NoError(t, ad.OnBar(ctx, bars[i]))
	}
	before, err := ad.SaveState()
	require.NoError(t, err)
	require.NotEmpty(t, before)

	cases := []struct {
		name string
		blob []byte
	}{
		{"garbage", []byte("{not-json")},
		{"empty", []byte{}},
		{"null-literal", []byte("null")},
		{"wrong-version", []byte(`{"version":999,"symbols":["AAA"],"window":3,"windows":{}}`)},
		{"bad-window", []byte(`{"version":1,"symbols":["AAA"],"window":0,"windows":{}}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, ad.LoadState(tc.blob), "非法状态必须 fail-loud")
			after, err := ad.SaveState()
			require.NoError(t, err)
			require.Equal(t, string(before), string(after),
				"LoadState 失败后状态必须一字不动（不得静默重置为初始态）")
		})
	}

	// 合法往返：新实例 LoadState 后 SaveState 字节稳定。
	fresh := NewBatchAdapter(&rtWindowGen{}, []string{sym}, 3)
	require.NoError(t, fresh.LoadState(before))
	roundtrip, err := fresh.SaveState()
	require.NoError(t, err)
	require.Equal(t, string(before), string(roundtrip), "合法状态必须可逆往返")
}
