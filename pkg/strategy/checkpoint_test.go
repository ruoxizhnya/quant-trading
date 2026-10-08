// K2 切片 3：pkg/strategy 检查点 / 断点续跑端到端测试（真库）。
//
// 核心正证据：用自写的**确定性** fake BarHandler（带递推状态：per-bar 累计 +
// EMA；SaveState=JSON，LoadState fail-loud；记录每根被处理的 bar）在真库上跑
// 「跑一半 → 中断 → 新实例续跑」，断言：
//   - 续跑实际处理的 bar 恰为「断点之后的那些」（既不重复已保存的，也不跳过
//     未保存的）；
//   - 续跑终态与「一次跑完」终态**逐字节一致**。
//
// 真库纪律同 state_store_test.go（独享 id、只按自己的 id 删、库不在就红不
// skip）。本文件用内部包以直接调用 skipPrefix / isDateBoundary 单测边界。
package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/require"
)

// ckptBaseDate 是测试序列的时间原点（UTC）。
var ckptBaseDate = time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

// ─── 确定性 fake BarHandler ────────────────────────────────────────

const ckptFakeStateVersion = 1

// ckptFakeState 是 fake 的序列化状态：**完整**捕获递推量（sum / n / ema）。
// 只有状态完整可逆，「续跑终态 == 一次跑完终态」才可能成立。
type ckptFakeState struct {
	Version int     `json:"version"`
	Sum     float64 `json:"sum"`
	N       int     `json:"n"`
	Ema     float64 `json:"ema"`
}

// ckptFakeHandler 是带递推状态的确定性 BarHandler：
//   - sum  = 已处理 bar 的 Close 累计；
//   - n    = 已成功处理的 bar 数；
//   - ema  = ema*0.5 + close*0.5（首根初始化）；
//   - processed 记录每根**成功处理**的 bar 身份（供精确序列断言）；
//   - failOn/failAt 注入故障：当已成功处理 n 根后，下一根 OnBar 返回 error。
type ckptFakeHandler struct {
	sum       float64
	n         int
	ema       float64
	loaded    bool
	processed []string

	failOn bool
	failAt int
}

func (h *ckptFakeHandler) OnBar(_ context.Context, bar domain.OHLCV) error {
	if h.failOn && h.n == h.failAt {
		return fmt.Errorf("ckptFakeHandler: 注入故障（已成功处理 %d 根）", h.n)
	}
	h.sum += bar.Close
	if h.n == 0 {
		h.ema = bar.Close
	} else {
		h.ema = h.ema*0.5 + bar.Close*0.5
	}
	h.n++
	h.processed = append(h.processed, ckptBarID(bar))
	return nil
}

func (h *ckptFakeHandler) Warmup() int       { return 0 }
func (h *ckptFakeHandler) Signals() []Signal { return nil }

func (h *ckptFakeHandler) SaveState() ([]byte, error) {
	return json.Marshal(ckptFakeState{Version: ckptFakeStateVersion, Sum: h.sum, N: h.n, Ema: h.ema})
}

// LoadState fail-loud（不静默重置）：任何不合法输入返回 error，且**不**改动
// 接收者（原子提交）。垃圾字节必须被拒——这是 #3（LoadState fail-loud）的
// 被测对象。
func (h *ckptFakeHandler) LoadState(b []byte) error {
	var st *ckptFakeState
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("ckptFakeHandler.LoadState 反序列化失败: %w", err)
	}
	if st == nil {
		return errors.New("ckptFakeHandler.LoadState 状态为空 (null)")
	}
	if st.Version != ckptFakeStateVersion {
		return fmt.Errorf("ckptFakeHandler.LoadState 版本不匹配 got=%d want=%d", st.Version, ckptFakeStateVersion)
	}
	h.sum, h.n, h.ema = st.Sum, st.N, st.Ema
	h.loaded = true
	return nil
}

// ckptBarID 给一根 bar 一个稳定身份字符串（symbol + 日期）。
func ckptBarID(bar domain.OHLCV) string {
	return bar.Symbol + "@" + bar.Date.UTC().Format(time.RFC3339Nano)
}

// ckptBarIDs 批量取身份（期望序列用）。
func ckptBarIDs(bars []domain.OHLCV) []string {
	out := make([]string, len(bars))
	for i, b := range bars {
		out[i] = ckptBarID(b)
	}
	return out
}

// ckptSeqBars 造 n 根**严格递增日期**的 bar（每根独占一天），Close = 1..n。
func ckptSeqBars(n int) []domain.OHLCV {
	bars := make([]domain.OHLCV, n)
	for i := 0; i < n; i++ {
		bars[i] = domain.OHLCV{
			Symbol: "AAA",
			Date:   ckptBaseDate.AddDate(0, 0, i),
			Close:  float64(i + 1),
		}
	}
	return bars
}

// ckptFlatBars 造 days × 每日多 symbol 的**平铺** bar 流：
// [d0sA, d0sB, d0sC, d1sA, ...]（同一日的多根相邻，跨日才换日期）。
func ckptFlatBars(days int, syms []string) []domain.OHLCV {
	var bars []domain.OHLCV
	close := 1.0
	for d := 0; d < days; d++ {
		for _, s := range syms {
			bars = append(bars, domain.OHLCV{
				Symbol: s,
				Date:   ckptBaseDate.AddDate(0, 0, d),
				Close:  close,
			})
			close++
		}
	}
	return bars
}

// ─── 1. 续跑等价性（核心正证据） ───────────────────────────────────

// TestRunCheckpointedResumeEquivalence 是核心正证据：20 根严格递增日期的 bar、
// Every=5。对若干「故障点」，断言：
//   - 故障后库内 ts == 断点前最后一根已处理 bar 的日期（保存点落在日期边界）；
//   - 新 handler + 新 runner 以 Resume=true 续跑同一批全量 bars，**实际处理的
//     bar 序列恰为断点之后那些**（不重复、不跳过）；
//   - 续跑终态字节与一次跑完的终态**完全一致**。
//
// 故障点 f 的期望（严格递增 → 每根都是日期边界）：
//   - 库内 ts = bars[f-1].Date；
//   - 续跑从 bars[f] 开始，处理 bars[f..19]。
//
// 其中 f=10（「跑到第 11 根时故障」）对应任务描述里的样例：库内 ts = 第 10 根、
// 续跑处理第 11..20 根。（任务原文写「第 12 根报错」，但按 ③「异常退出尽力保存
// 断点」，第 11 根的处理结果会被故障保存写进库，续跑自然从第 12 根起；f=10 才
// 精确复现「ts=第 10 根 / 续跑 11..20」这组样例数字。两种故障点本测试都覆盖。）
func TestRunCheckpointedResumeEquivalence(t *testing.T) {
	ctx := context.Background()
	bars := ckptSeqBars(20)
	require.Len(t, bars, 20)
	const every = 5

	// 参照：一次跑完的终态（真库 store，独立 id）。
	hFull := &ckptFakeHandler{}
	storeFull, _, sidFull, ridFull := stNewStore(t)
	ckFull := Checkpoint{Store: storeFull, StrategyID: sidFull, RunID: ridFull, Every: every}
	require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, hFull, bars, clock.NewVirtualClock(bars[0].Date), ckFull))
	wantBlob, err := hFull.SaveState()
	require.NoError(t, err)
	require.Equal(t, 20, hFull.n, "一次跑完应处理全部 20 根")

	for _, failAt := range []int{10, 11, 12} {
		failAt := failAt
		t.Run(fmt.Sprintf("failAtBar_%d", failAt), func(t *testing.T) {
			store, _, sid, rid := stNewStore(t)
			ck := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: every}

			// (b) 跑到第 failAt 根时 fake 报 error（模拟故障）。
			hBroken := &ckptFakeHandler{failOn: true, failAt: failAt}
			err := NewStreamRunner().RunCheckpointed(ctx, hBroken, bars, clock.NewVirtualClock(bars[0].Date), ck)
			require.Error(t, err, "注入故障必须让 RunCheckpointed fail-loud")
			require.Equal(t, failAt, hBroken.n, "故障前应恰好成功处理 failAt 根")

			// 断点落在日期边界：库内 ts == 最后成功处理那根的日期。
			_, ts, err := store.Load(ctx, sid, rid)
			require.NoError(t, err, "故障后应有断点落库")
			require.True(t, ts.Equal(bars[failAt-1].Date),
				"库内 ts = %s, want %s（第 %d 根，最后日期边界）", ts, bars[failAt-1].Date, failAt)

			// 续跑：新 handler + 新 runner，Resume=true，喂全量 bars。
			hResume := &ckptFakeHandler{}
			ckResume := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: every, Resume: true}
			require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, hResume, bars, clock.NewVirtualClock(bars[0].Date), ckResume))

			// 精确序列：恰好处理 bars[failAt:]，既无重复也无跳过。
			require.Equal(t, ckptBarIDs(bars[failAt:]), hResume.processed,
				"续跑处理序列必须恰为第 %d..20 根", failAt+1)

			// 终态逐字节一致。
			gotBlob, err := hResume.SaveState()
			require.NoError(t, err)
			require.Equal(t, string(wantBlob), string(gotBlob),
				"续跑终态字节必须与一次跑完的终态完全一致")
		})
	}
}

// ─── 2. 边界对齐（多 symbol 平铺流） ───────────────────────────────

// TestRunCheckpointedBoundaryAlignmentMultiSymbol 用一个平铺流
// [d1sA,d1sB,d1sC, d2sA,d2sB,d2sC, d3sA,d3sB,d3sC]、Every=1，在一个**日期块中间**
// （d2 块的 d2sB）制造故障，断言：
//   - 库内 ts 停在**上一个完整日期块**（d1）的日期——同日期块内绝不保存；
//   - 续跑跳过整个 d1 块（d1 三根都已处理并保存），从 d2sA 继续，**不丢**任何
//     未处理的 bar（尤其不丢 d1sC）。
//
// 若把「日期边界」门槛去掉（破坏验证 c），保存点会落到 d1sA（ts=d1）甚至 d2 块
// 中间，续跑会按 `Date <= ts` 误跳整个 d1 块（含未处理的 d1sC）→ 本测试红。
func TestRunCheckpointedBoundaryAlignmentMultiSymbol(t *testing.T) {
	ctx := context.Background()
	syms := []string{"AAA", "BBB", "CCC"}
	bars := ckptFlatBars(3, syms)
	require.Len(t, bars, 9)
	const every = 1

	// 参照终态。
	hFull := &ckptFakeHandler{}
	storeFull, _, sidFull, ridFull := stNewStore(t)
	ckFull := Checkpoint{Store: storeFull, StrategyID: sidFull, RunID: ridFull, Every: every}
	require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, hFull, bars, clock.NewVirtualClock(bars[0].Date), ckFull))
	wantBlob, err := hFull.SaveState()
	require.NoError(t, err)

	store, _, sid, rid := stNewStore(t)
	ck := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: every}

	// 故障点：d2sB（index 4）——处理完 d1 整块 + d2sA 后崩。
	const failAt = 4
	require.Equal(t, "BBB", bars[failAt].Symbol, "前置：index 4 应是 d2 的 BBB")
	hBroken := &ckptFakeHandler{failOn: true, failAt: failAt}
	require.Error(t, NewStreamRunner().RunCheckpointed(ctx, hBroken, bars, clock.NewVirtualClock(bars[0].Date), ck))

	// 库内 ts 停在 d1（上一个完整日期块），不是 d2sA 的日期。
	d1 := bars[0].Date
	_, ts, err := store.Load(ctx, sid, rid)
	require.NoError(t, err)
	require.True(t, ts.Equal(d1),
		"库内 ts = %s, want %s（保存点必须落在日期边界 d1，绝不能停在同一日期块中间）", ts, d1)

	// 续跑：跳过整个 d1 块（0,1,2），从 d2sA（3）继续。
	hResume := &ckptFakeHandler{}
	ckResume := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: every, Resume: true}
	require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, hResume, bars, clock.NewVirtualClock(bars[0].Date), ckResume))

	require.Equal(t, ckptBarIDs(bars[3:]), hResume.processed,
		"续跑应跳过整个 d1 块并从 d2sA 起，不丢任何 bar")

	gotBlob, err := hResume.SaveState()
	require.NoError(t, err)
	require.Equal(t, string(wantBlob), string(gotBlob), "续跑终态字节必须与一次跑完一致")
}

// ─── 3. LoadState fail-loud（不静默从头跑） ────────────────────────

// TestRunCheckpointedLoadStateFailsLoud 库里塞垃圾字节 → Resume=true → 立即
// error，且**一根 bar 都不处理**（证明没有静默从头跑）。
func TestRunCheckpointedLoadStateFailsLoud(t *testing.T) {
	ctx := context.Background()
	bars := ckptSeqBars(6)
	store, _, sid, rid := stNewStore(t)

	// 直接塞一段非法 JSON（Save 只拒空/零 ts，非空脏字节可落库——模拟损坏）。
	require.NoError(t, store.Save(ctx, sid, rid, stTestTS(0), []byte("{not-json")))

	h := &ckptFakeHandler{}
	ck := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: 1, Resume: true}
	err := NewStreamRunner().RunCheckpointed(ctx, h, bars, clock.NewVirtualClock(bars[0].Date), ck)
	require.Error(t, err, "损坏状态必须 fail-loud，不得静默从头跑")
	require.Empty(t, h.processed, "LoadState 失败后一根 bar 都不该被处理（证明没从头跑）")
	require.False(t, h.loaded, "LoadState 根本不该成功")
}

// ─── 4. Resume=true + 无状态 → 从头跑（不是 error） ────────────────

// TestRunCheckpointedResumeWithoutState 首次运行（库中无检查点）是正常情况：
// Resume=true 不报错，从头跑完全部 bar。
func TestRunCheckpointedResumeWithoutState(t *testing.T) {
	ctx := context.Background()
	bars := ckptSeqBars(6)
	store, _, sid, rid := stNewStore(t)

	hFull := &ckptFakeHandler{}
	require.NoError(t, NewStreamRunner().Run(ctx, hFull, bars, clock.NewVirtualClock(bars[0].Date)))
	wantBlob, err := hFull.SaveState()
	require.NoError(t, err)

	h := &ckptFakeHandler{}
	ck := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: 2, Resume: true}
	require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, h, bars, clock.NewVirtualClock(bars[0].Date), ck),
		"Resume=true 但库中无状态应从头上路，不是 error")
	require.Equal(t, ckptBarIDs(bars), h.processed, "应从头处理全部 bar")

	gotBlob, err := h.SaveState()
	require.NoError(t, err)
	require.Equal(t, string(wantBlob), string(gotBlob))
}

// TestRunCheckpointedResumeWithoutStore 配置矛盾必须 fail-loud。
func TestRunCheckpointedResumeWithoutStore(t *testing.T) {
	ctx := context.Background()
	bars := ckptSeqBars(3)
	h := &ckptFakeHandler{}
	err := NewStreamRunner().RunCheckpointed(ctx, h, bars, clock.NewVirtualClock(bars[0].Date),
		Checkpoint{Resume: true})
	require.Error(t, err, "Resume=true 但 Store=nil 是配置矛盾，必须 error")
	require.Empty(t, h.processed)
}

// ─── 5. Store=nil → 与 Run 行为相同（无 DB 依赖） ──────────────────

// TestRunCheckpointedNilStoreMatchesRun Store=nil（零值 Checkpoint）时，
// RunCheckpointed 与 Run 完全等价：处理全部 bar、终态一致、无任何 DB 依赖。
func TestRunCheckpointedNilStoreMatchesRun(t *testing.T) {
	ctx := context.Background()
	bars := ckptSeqBars(5)

	hRun := &ckptFakeHandler{}
	require.NoError(t, NewStreamRunner().Run(ctx, hRun, bars, clock.NewVirtualClock(bars[0].Date)))

	hCk := &ckptFakeHandler{}
	require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, hCk, bars, clock.NewVirtualClock(bars[0].Date), Checkpoint{}))

	require.Equal(t, hRun.processed, hCk.processed, "Store=nil 的 RunCheckpointed 应与 Run 处理同一序列")
	blobRun, err := hRun.SaveState()
	require.NoError(t, err)
	blobCk, err := hCk.SaveState()
	require.NoError(t, err)
	require.Equal(t, string(blobRun), string(blobCk))
}

// ─── 6. 无 bar 可跑 → 直接 nil、不保存 ─────────────────────────────

// TestRunCheckpointedNoBarsToRun 恢复后断点已覆盖全部输入 → 直接返回 nil，
// 不报错、不处理任何 bar、不产生新的保存（无状态推进）。
func TestRunCheckpointedNoBarsToRun(t *testing.T) {
	ctx := context.Background()
	bars := ckptSeqBars(4)
	store, _, sid, rid := stNewStore(t)

	// 预置一份「已跑完」的检查点（ts = 最后一根），状态合法可 LoadState。
	hDone := &ckptFakeHandler{}
	require.NoError(t, NewStreamRunner().Run(ctx, hDone, bars, clock.NewVirtualClock(bars[0].Date)))
	blob, err := hDone.SaveState()
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, sid, rid, bars[len(bars)-1].Date, blob))

	h := &ckptFakeHandler{}
	ck := Checkpoint{Store: store, StrategyID: sid, RunID: rid, Every: 1, Resume: true}
	require.NoError(t, NewStreamRunner().RunCheckpointed(ctx, h, bars, clock.NewVirtualClock(bars[0].Date), ck),
		"无 bar 可跑不是错误")
	require.Empty(t, h.processed, "断点已覆盖全部输入，不该再处理任何 bar")

	// 库内 ts 未被改写（仍是最初那根最后 bar 的日期）。
	_, ts, err := store.Load(ctx, sid, rid)
	require.NoError(t, err)
	require.True(t, ts.Equal(bars[len(bars)-1].Date), "无 bar 可跑不得改写断点")
}

// ─── 7. 跳过逻辑纯函数单测（边界） ─────────────────────────────────

// TestSkipPrefixBoundaries 单测 skipPrefix 的边界：等于 ts 必须跳过、首根 >
// ts 跳 0 根、全部 <= ts 跳完。
func TestSkipPrefixBoundaries(t *testing.T) {
	bars := ckptSeqBars(4) // dates: t0 < t1 < t2 < t3

	require.Equal(t, 0, skipPrefix(bars, bars[0].Date.AddDate(0, 0, -1)), "全部 > ts → 跳 0 根")
	require.Equal(t, 4, skipPrefix(bars, bars[3].Date), "全部 <= ts → 跳完（含等于 ts 的最后一根）")
	require.Equal(t, 2, skipPrefix(bars, bars[1].Date), "等于 bars[1] 的 ts 必须跳过它（共 2 根）")
	require.Equal(t, 4, skipPrefix(bars, bars[3].Date.AddDate(0, 0, 100)), "ts 超出末根 → 跳完")
	require.Equal(t, 0, skipPrefix(nil, bars[0].Date), "空 bars → 0")
	require.Equal(t, 1, skipPrefix([]domain.OHLCV{bars[0]}, bars[0].Date),
		"等于首根 ts → 跳过首根")
}

// TestIsDateBoundary 钉住边界定义：末根恒为边界；下一根同日不是、跨日是。
func TestIsDateBoundary(t *testing.T) {
	bars := ckptFlatBars(2, []string{"AAA", "BBB"}) // [d0A,d0B,d1A,d1B]
	require.False(t, isDateBoundary(bars, 0), "d0A 下一根同日 → 非边界")
	require.True(t, isDateBoundary(bars, 1), "d0B 下一根跨日 → 边界")
	require.False(t, isDateBoundary(bars, 2), "d1A 下一根同日 → 非边界")
	require.True(t, isDateBoundary(bars, 3), "末根恒为边界")
}
