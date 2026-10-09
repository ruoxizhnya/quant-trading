// K4：TWAP 执行算法测试。
//
// 覆盖验收判据：片数正确、SubmitAt 严格等差（均匀性）、Σ 子单 Qty 精确
// 等于父单量（含 1000/7 除不尽用例）、子单号幂等、非法输入 error、
// OnBar 到点产出 + DueChildOrders 取走即清空、OnFill 进度与超额不 panic。
package execalgo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twapParent(qty float64, slices int) execalgo.ParentOrder {
	start := time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC)
	return execalgo.ParentOrder{
		OrderID: "PO-1", RunID: "RUN-1", Symbol: "000001.SZ",
		Side: domain.DirectionLong, Qty: qty,
		StartAt: start, EndAt: start.Add(time.Duration(slices) * time.Hour),
	}
}

// TestTWAP_SliceCountAndQtySumExact 断言片数正确且 Σ Qty 精确 == 父单量，
// 含除不尽的 1000/7。
func TestTWAP_SliceCountAndQtySumExact(t *testing.T) {
	cases := []struct {
		name   string
		qty    float64
		slices int
	}{
		{"even 1000/4", 1000, 4},
		{"odd 1000/7", 1000, 7},
		{"prime 997/3", 997, 3},
		{"single slice", 1000, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := execalgo.NewTWAP(tc.slices)
			plan, err := a.Schedule(context.Background(), twapParent(tc.qty, tc.slices))
			require.NoError(t, err)
			require.Len(t, plan, tc.slices)

			var sum float64
			for _, c := range plan {
				require.Greater(t, c.Qty, 0.0, "每片都应为正手（未产出 0 手子单）")
				sum += c.Qty
			}
			assert.Equal(t, tc.qty, sum, "Σ 子单 Qty 必须精确等于父单量")
			t.Logf("qty=%v slices=%d -> Σ=%v 各片=%v", tc.qty, tc.slices, sum, childQtys(plan))
		})
	}
}

// TestTWAP_QtyUniform 断言可整除时各片等量（TWAP 的「等量」语义）。
// 与 TestTWAP_SubmitAtUniform（等时）共同构成 TWAP 的均匀性判据。
func TestTWAP_QtyUniform(t *testing.T) {
	a := execalgo.NewTWAP(4)
	plan, err := a.Schedule(context.Background(), twapParent(1000, 4))
	require.NoError(t, err)
	want := 1000.0 / 4
	for i, c := range plan {
		assert.InDelta(t, want, c.Qty, 1e-9, "第 %d 片应与均分量相等", i)
	}
}

// TestTWAP_SubmitAtUniform 是本切片的**均匀性正证据**：相邻 SubmitAt 间隔
// 全等，即时间分布严格等差。
func TestTWAP_SubmitAtUniform(t *testing.T) {
	a := execalgo.NewTWAP(7)
	parent := twapParent(1000, 7)
	plan, err := a.Schedule(context.Background(), parent)
	require.NoError(t, err)

	step := plan[1].SubmitAt.Sub(plan[0].SubmitAt)
	require.Greater(t, step, time.Duration(0))
	for i := 1; i < len(plan); i++ {
		got := plan[i].SubmitAt.Sub(plan[i-1].SubmitAt)
		assert.Equal(t, step, got, "第 %d 片与前一间隔应等于 step", i)
	}
	// 首片落在窗口起点。
	assert.Equal(t, parent.StartAt, plan[0].SubmitAt)
	t.Logf("SubmitAt 序列=%v 步长=%s（相邻间隔全等）", submitTimes(plan), step)
}

// TestTWAP_IDIdempotent 断言同一 (父单号, 片序号) 两次 Schedule 产出同一串 ID。
func TestTWAP_IDIdempotent(t *testing.T) {
	parent := twapParent(1000, 7)

	a1 := execalgo.NewTWAP(7)
	p1, err := a1.Schedule(context.Background(), parent)
	require.NoError(t, err)

	// 两次 Schedule 之间插一段延迟（2026-10-09 审查加固）：若 ID 由时间/随机数
	// 派生，背靠背两次调用在**粗粒度时钟**上会取到同一个值 —— 护栏会假绿。
	// 实测：把 childOrderID 改成带 time.Now().UnixNano() 后，本用例仍 PASS；
	// 加这段延迟后才变红。真实性判据是「ID 是 (父单号, 片序号) 的纯函数」，
	// 所以必须让两次调用**在时间上拉开**才能测出来。
	time.Sleep(20 * time.Millisecond)

	a2 := execalgo.NewTWAP(7)
	p2, err := a2.Schedule(context.Background(), parent)
	require.NoError(t, err)

	require.Len(t, p2, len(p1))
	for i := range p1 {
		assert.Equal(t, p1[i].OrderID, p2[i].OrderID, "第 %d 片子单号必须幂等", i)
		assert.Equal(t, "PO-1", p1[i].ParentID, "子单必须回链父单号")
	}
	t.Logf("ID 序列=%v", childIDs(p1))
}

// TestTWAP_InvalidInput 断言非法输入返回 error。
func TestTWAP_InvalidInput(t *testing.T) {
	base := twapParent(1000, 4)
	a := execalgo.NewTWAP(4)

	t.Run("qty<=0", func(t *testing.T) {
		p := base
		p.Qty = 0
		_, err := a.Schedule(context.Background(), p)
		assert.Error(t, err)
	})
	t.Run("end<=start", func(t *testing.T) {
		p := base
		p.EndAt = p.StartAt
		_, err := a.Schedule(context.Background(), p)
		assert.Error(t, err)
	})
	t.Run("empty symbol", func(t *testing.T) {
		p := base
		p.Symbol = ""
		_, err := a.Schedule(context.Background(), p)
		assert.Error(t, err)
	})
}

// TestTWAP_OnBarDueAndTakeAway 断言到点产出、取走即清空。
func TestTWAP_OnBarDueAndTakeAway(t *testing.T) {
	a := execalgo.NewTWAP(7)
	parent := twapParent(1000, 7) // 步长 1h
	plan, err := a.Schedule(context.Background(), parent)
	require.NoError(t, err)

	ctx := context.Background()

	// 未到点的 bar：无子单。
	require.NoError(t, a.OnBar(ctx, domain.OHLCV{Date: parent.StartAt.Add(-time.Minute)}))
	assert.Empty(t, a.DueChildOrders())

	// 恰在首片时刻：产出 1 片。
	require.NoError(t, a.OnBar(ctx, domain.OHLCV{Date: parent.StartAt}))
	due := a.DueChildOrders()
	require.Len(t, due, 1)
	assert.Equal(t, plan[0].OrderID, due[0].OrderID)

	// 取走即清空：再取为 nil/空。
	assert.Empty(t, a.DueChildOrders())

	// 跳到 2h30m：应补发第 1、2 片（SubmitAt 1h、2h 均 <= 2h30m）。
	require.NoError(t, a.OnBar(ctx, domain.OHLCV{Date: parent.StartAt.Add(2*time.Hour + 30*time.Minute)}))
	due = a.DueChildOrders()
	require.Len(t, due, 2)
	assert.Equal(t, plan[1].OrderID, due[0].OrderID)
	assert.Equal(t, plan[2].OrderID, due[1].OrderID)
}

// TestTWAP_OnBarBeforeSchedule 断言未 Schedule 即 OnBar 报错（fail-loud）。
func TestTWAP_OnBarBeforeSchedule(t *testing.T) {
	a := execalgo.NewTWAP(4)
	err := a.OnBar(context.Background(), domain.OHLCV{Date: time.Now()})
	assert.Error(t, err)
}

// TestTWAP_OnFillProgress covers progress monotonicity, completion, and the
// overfill / duplicate-delivery case (must not panic, filled clamped).
func TestTWAP_OnFillProgress(t *testing.T) {
	a := execalgo.NewTWAP(4)
	plan, err := a.Schedule(context.Background(), twapParent(1000, 4))
	require.NoError(t, err)

	f, total, done := a.Progress()
	assert.Equal(t, 0.0, f)
	assert.Equal(t, 1000.0, total)
	assert.False(t, done)

	ctx := context.Background()
	// 第一批：第 0、1 片。
	require.NoError(t, a.OnFill(ctx, portfolio.Fill{OrderID: plan[0].OrderID, Qty: plan[0].Qty}))
	require.NoError(t, a.OnFill(ctx, portfolio.Fill{OrderID: plan[1].OrderID, Qty: plan[1].Qty}))
	f1, _, done1 := a.Progress()
	assert.InDelta(t, 500.0, f1, 1e-9)
	assert.False(t, done1)

	// 第二批：第 2、3 片 → 全部成交。
	require.NoError(t, a.OnFill(ctx, portfolio.Fill{OrderID: plan[2].OrderID, Qty: plan[2].Qty}))
	require.NoError(t, a.OnFill(ctx, portfolio.Fill{OrderID: plan[3].OrderID, Qty: plan[3].Qty}))
	f2, _, done2 := a.Progress()
	assert.InDelta(t, 1000.0, f2, 1e-9)
	assert.True(t, done2, "全填后 done=true")
	assert.GreaterOrEqual(t, f2, f1, "进度单调不减")

	// 超额 fill（含重复投递）不 panic，filled 被钳制在 [0,total]。
	assert.NotPanics(t, func() {
		require.NoError(t, a.OnFill(ctx, portfolio.Fill{OrderID: plan[0].OrderID, Qty: 99999}))
	})
	f3, _, done3 := a.Progress()
	assert.Equal(t, 1000.0, f3, "超额 fill 后 filled 钳制为 total")
	assert.True(t, done3)

	// 非本计划的回报被忽略（不报错、不改进度）。
	require.NoError(t, a.OnFill(ctx, portfolio.Fill{OrderID: "OTHER-999", Qty: 5}))
	f4, _, _ := a.Progress()
	assert.Equal(t, 1000.0, f4)
}

// TestTWAP_DeriveSlices 断言未指定片数时按窗口时长 / 5min 推导。
func TestTWAP_DeriveSlices(t *testing.T) {
	a := execalgo.NewTWAP(0) // 自动推导
	start := time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC)
	parent := execalgo.ParentOrder{
		OrderID: "PO-2", Symbol: "000001.SZ", Side: domain.DirectionLong,
		Qty: 600, StartAt: start, EndAt: start.Add(30 * time.Minute), // 30min/5min = 6 片
	}
	plan, err := a.Schedule(context.Background(), parent)
	require.NoError(t, err)
	assert.Len(t, plan, 6)
	t.Logf("30min 窗口自动推导片数=%d", len(plan))
}

// ─── 测试小工具 ────────────────────────────────────────────────────

func childQtys(plan []execalgo.ChildOrder) []float64 {
	out := make([]float64, len(plan))
	for i, c := range plan {
		out[i] = c.Qty
	}
	return out
}

func childIDs(plan []execalgo.ChildOrder) []string {
	out := make([]string, len(plan))
	for i, c := range plan {
		out[i] = c.OrderID
	}
	return out
}

func submitTimes(plan []execalgo.ChildOrder) []string {
	out := make([]string, len(plan))
	for i, c := range plan {
		out[i] = c.SubmitAt.Format("15:04")
	}
	return out
}
