// K4：VWAP 执行算法测试。
//
// 覆盖：按 profile 比例分配（容差 1e-9）、profile 退化 → 等分、
// Σ 精确、SubmitAt 等差、非法输入 error。
package execalgo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vwapParent(qty float64, slices int) execalgo.ParentOrder {
	start := time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC)
	return execalgo.ParentOrder{
		OrderID: "PO-V", RunID: "RUN-1", Symbol: "600000.SH",
		Side: domain.DirectionLong, Qty: qty,
		StartAt: start, EndAt: start.Add(time.Duration(slices) * time.Hour),
	}
}

// TestVWAP_ProportionalQty 断言各片 Qty 比例 == profile 比例（容差 1e-9），
// 且 Σ 精确 == 父单量。
func TestVWAP_ProportionalQty(t *testing.T) {
	profile := []float64{1, 2, 3, 4}
	qty := 1000.0
	a := execalgo.NewVWAP(len(profile), profile)
	plan, err := a.Schedule(context.Background(), vwapParent(qty, len(profile)))
	require.NoError(t, err)
	require.Len(t, plan, 4)
	assert.False(t, a.Degraded(), "合法 profile 不应退化")

	var sum float64
	var profileSum float64
	for _, v := range profile {
		profileSum += v
	}
	for i, c := range plan {
		sum += c.Qty
		want := profile[i] / profileSum
		got := c.Qty / qty
		assert.InDelta(t, want, got, 1e-9, "第 %d 片数量占比应等于 profile 占比", i)
	}
	assert.Equal(t, qty, sum, "Σ 子单 Qty 精确 == 父单量")
	t.Logf("profile=%v -> 各片 Qty=%v 占比=%v", profile, childQtys(plan), planRatios(plan, qty))
}

// TestVWAP_DegradeToEqual 断言 profile 为空 / 全 0 / 长度不匹配 / 含负值时
// 退化为等分（等价 TWAP），且 Degraded() 暴露退化事实。
func TestVWAP_DegradeToEqual(t *testing.T) {
	cases := []struct {
		name    string
		slices  int
		profile []float64
	}{
		{"empty profile", 4, nil},
		{"all-zero profile", 4, []float64{0, 0, 0, 0}},
		{"length mismatch", 4, []float64{1, 2}},
		{"negative weight", 4, []float64{1, -2, 3, 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qty := 1000.0
			a := execalgo.NewVWAP(tc.slices, tc.profile)
			plan, err := a.Schedule(context.Background(), vwapParent(qty, tc.slices))
			require.NoError(t, err)
			require.Len(t, plan, tc.slices)
			assert.True(t, a.Degraded(), "非法 profile 必须退化并置 Degraded=true")

			// 等分：每片 == qty/slices（末片补差）。
			even := qty / float64(tc.slices)
			var sum float64
			for i, c := range plan {
				sum += c.Qty
				if i < len(plan)-1 {
					assert.InDelta(t, even, c.Qty, 1e-9, "退化后第 %d 片应为等分", i)
				}
			}
			assert.Equal(t, qty, sum)
			t.Logf("%s -> 各片 Qty=%v（等分，Degraded=true）", tc.name, childQtys(plan))
		})
	}
}

// TestVWAP_SubmitAtUniform 复用 TWAP 的等差判据（VWAP 时间排程同构）。
func TestVWAP_SubmitAtUniform(t *testing.T) {
	a := execalgo.NewVWAP(0, []float64{1, 2, 3, 4})
	plan, err := a.Schedule(context.Background(), vwapParent(1000, 4))
	require.NoError(t, err)
	require.Len(t, plan, 4)
	step := plan[1].SubmitAt.Sub(plan[0].SubmitAt)
	for i := 1; i < len(plan); i++ {
		assert.Equal(t, step, plan[i].SubmitAt.Sub(plan[i-1].SubmitAt))
	}
}

// TestVWAP_IDIdempotent 断言子单号幂等。
func TestVWAP_IDIdempotent(t *testing.T) {
	profile := []float64{1, 2, 3, 4}
	parent := vwapParent(1000, 4)
	a1 := execalgo.NewVWAP(4, profile)
	p1, err := a1.Schedule(context.Background(), parent)
	require.NoError(t, err)

	// 同 TWAP：插延迟以暴露「时间/随机数派生 ID」——粗粒度时钟下背靠背调用
	// 会取到同一个值，护栏假绿（2026-10-09 审查加固）。
	time.Sleep(20 * time.Millisecond)

	a2 := execalgo.NewVWAP(4, profile)
	p2, err := a2.Schedule(context.Background(), parent)
	require.NoError(t, err)
	for i := range p1 {
		assert.Equal(t, p1[i].OrderID, p2[i].OrderID)
	}
}

// TestVWAP_InvalidInput 断言非法输入 error。
func TestVWAP_InvalidInput(t *testing.T) {
	a := execalgo.NewVWAP(4, []float64{1, 2, 3, 4})
	base := vwapParent(1000, 4)

	p := base
	p.Qty = -1
	_, err := a.Schedule(context.Background(), p)
	assert.Error(t, err)

	p = base
	p.EndAt = p.StartAt.Add(-time.Hour)
	_, err = a.Schedule(context.Background(), p)
	assert.Error(t, err)

	p = base
	p.Symbol = ""
	_, err = a.Schedule(context.Background(), p)
	assert.Error(t, err)
}

func planRatios(plan []execalgo.ChildOrder, qty float64) []float64 {
	out := make([]float64, len(plan))
	for i, c := range plan {
		out[i] = c.Qty / qty
	}
	return out
}
