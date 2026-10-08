// pkg/indicator RMA 测试（K3 切片 1）。
package indicator_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// TestRMA_ConstructorValidation 参数非法必须 fail-loud（不静默纠正）。
func TestRMA_ConstructorValidation(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		if _, err := indicator.NewRMA(n); err == nil {
			t.Errorf("NewRMA(%d) 应报错，却成功", n)
		}
	}
	if _, err := indicator.NewRMA(1); err != nil {
		t.Errorf("NewRMA(1) 应成功: %v", err)
	}
}

// TestRMA_HandComputedFixture 手算数值 fixture（抓公式错，核心）。
//
// RMA(3) 对 xs = [1,2,3,4,5]：
//
//	idx0: 未满 3 根 → 无值
//	idx1: 未满 3 根 → 无值
//	idx2: 前 3 根 SMA = (1+2+3)/3 = 2
//	idx3: prev += (x-prev)/N = 2 + (4-2)/3 = 2 + 2/3 = 2.6666666666666665
//	idx4: prev += (x-prev)/N = 2.6666666666666665 + (5-2.6666666666666665)/3
//	      = 2.6666666666666665 + 0.7777777777777778 = 3.4444444444444446
//
// 期望值全部为**手写常量**（不调用被测代码推算）。
func TestRMA_HandComputedFixture(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	want := []float64{math.NaN(), math.NaN(), 2, 2.6666666666666665, 3.4444444444444446}

	got, err := indicator.RMABatch(xs, 3)
	if err != nil {
		t.Fatalf("RMABatch: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("长度 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !closeEnough(got[i], want[i]) {
			t.Errorf("RMABatch[%d] = %.17g, want %.17g", i, got[i], want[i])
		}
	}

	// Step 路径逐点同值（前两位 Value 必须 error）。
	ind, err := indicator.NewRMA(3)
	if err != nil {
		t.Fatalf("NewRMA: %v", err)
	}
	for i, x := range xs {
		if err := ind.Update(x); err != nil {
			t.Fatalf("Update t=%d: %v", i, err)
		}
		v, err := ind.Value()
		if i < 2 {
			if err == nil {
				t.Errorf("t=%d warmup 未完成却返回值 %v", i, v)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Value t=%d: %v", i, err)
		}
		if !closeEnough(v, want[i]) {
			t.Errorf("Step Value[%d] = %.17g, want %.17g", i, v, want[i])
		}
	}
}

// TestRMA_N1Identity 钉住 n=1 退化为恒等。
func TestRMA_N1Identity(t *testing.T) {
	xs := []float64{3, -1.5, 0, 7.25, 100}
	got, err := indicator.RMABatch(xs, 1)
	if err != nil {
		t.Fatalf("RMABatch: %v", err)
	}
	for i := range xs {
		if got[i] != xs[i] {
			t.Errorf("RMA(1)[%d] = %v, want %v（应为恒等）", i, got[i], xs[i])
		}
	}
	if w := mustRMA(t, 1).Warmup(); w != 1 {
		t.Errorf("RMA(1).Warmup() = %d, want 1", w)
	}
}

// TestRMA_WarmupBoundary 喂 N-1 → Value error；喂第 N → 有值。
func TestRMA_WarmupBoundary(t *testing.T) {
	ind := mustRMA(t, 14)
	if w := ind.Warmup(); w != 14 {
		t.Fatalf("RMA(14).Warmup() = %d, want 14", w)
	}
	assertWarmupBoundary(t, "RMA(14)", ind, func(i int) float64 { return float64(i + 1) })
}

// TestRMA_UpdateAtomicity NaN/±Inf → error 且状态逐位不变。
func TestRMA_UpdateAtomicity(t *testing.T) {
	ind := mustRMA(t, 3)
	for _, x := range []float64{1, 2, 3, 4} {
		if err := ind.Update(x); err != nil {
			t.Fatalf("Update(%v): %v", x, err)
		}
	}
	before, _ := ind.SaveState()
	vBefore, _ := ind.Value()

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := ind.Update(bad); err == nil {
			t.Errorf("Update(%v) 应报错", bad)
		}
		after, _ := ind.SaveState()
		if !bytes.Equal(before, after) {
			t.Errorf("Update(%v) 失败后状态被改动:\n before=%s\n after =%s", bad, before, after)
		}
		vAfter, err := ind.Value()
		if err != nil {
			t.Fatalf("失败更新后 Value 报错: %v", err)
		}
		if vAfter != vBefore {
			t.Errorf("Update(%v) 失败后 Value 变了: %v → %v", bad, vBefore, vAfter)
		}
	}
}

// TestRMA_LoadStateFailLoud 垃圾/null/空/版本不符/缺字段 → error 且接收者原子不变。
func TestRMA_LoadStateFailLoud(t *testing.T) {
	ind := mustRMA(t, 3)
	for _, x := range []float64{1, 2, 3, 4, 5} {
		_ = ind.Update(x)
	}
	before, _ := ind.SaveState()

	bad := [][]byte{
		nil,
		{},
		[]byte("null"),
		[]byte("not json"),
		[]byte(`{"version":999,"count":1,"sum":1,"prev":1}`),
		[]byte(`{"count":1,"sum":1,"prev":1}`),                      // 缺 version
		[]byte(`{"version":1,"sum":1,"prev":1}`),                    // 缺 count
		[]byte(`{"version":1,"count":1,"sum":1}`),                   // 缺 prev
		[]byte(`{"version":1,"count":1,"sum":1,"prev":1,"oops":0}`), // 未知字段
	}
	for _, b := range bad {
		if err := ind.LoadState(b); err == nil {
			t.Errorf("LoadState(%q) 应报错，却成功", b)
		}
		after, _ := ind.SaveState()
		if !bytes.Equal(before, after) {
			t.Errorf("LoadState(%q) 失败后接收者被改动:\n before=%s\n after =%s", b, before, after)
		}
	}
}

// TestRMA_RoundTrip SaveState → LoadState 后继续跑与不中断一致。
func TestRMA_RoundTrip(t *testing.T) {
	xs := []float64{5, 3, 8, 1, 9, 2, 7, 4, 6, 10}
	a := mustRMA(t, 4)
	for _, x := range xs {
		_ = a.Update(x)
	}
	aVal, err := a.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	blob, err := a.SaveState()
	if err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	b := mustRMA(t, 4)
	if err := b.LoadState(blob); err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	bVal, err := b.Value()
	if err != nil {
		t.Fatalf("恢复后 Value: %v", err)
	}
	if !closeEnough(aVal, bVal) {
		t.Errorf("恢复后值 = %.17g, want %.17g", bVal, aVal)
	}
}

// TestRMA_Reset 重置换回初始态。
func TestRMA_Reset(t *testing.T) {
	ind := mustRMA(t, 3)
	_ = ind.Update(1)
	_ = ind.Update(2)
	ind.Reset()
	if _, err := ind.Value(); err == nil {
		t.Error("Reset 后应立即需要重新预热")
	}
	blob, _ := ind.SaveState()
	if err := ind.LoadState(blob); err != nil {
		t.Fatalf("空状态不可回灌: %v", err)
	}
}

// TestRMA_BatchValidation Batch 参数校验与构造函数同规；空/nil 无错。
func TestRMA_BatchValidation(t *testing.T) {
	if _, err := indicator.RMABatch([]float64{1, 2}, 0); err == nil {
		t.Error("RMABatch n=0 应报错")
	}
	if _, err := indicator.RMABatch([]float64{1, math.NaN()}, 3); err == nil {
		t.Error("RMABatch 含 NaN 应报错")
	}
	for _, in := range [][]float64{nil, {}} {
		got, err := indicator.RMABatch(in, 3)
		if err != nil {
			t.Errorf("RMABatch(空) 应无错: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("RMABatch(空) 长度 = %d, want 0", len(got))
		}
	}
}

func mustRMA(t *testing.T, n int) *indicator.RMA {
	t.Helper()
	ind, err := indicator.NewRMA(n)
	if err != nil {
		t.Fatalf("NewRMA(%d): %v", n, err)
	}
	return ind
}
