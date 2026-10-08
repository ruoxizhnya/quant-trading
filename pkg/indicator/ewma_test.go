// pkg/indicator EWMA 测试（K3 切片 1）。
package indicator_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// TestEWMA_ConstructorValidation 参数非法必须 fail-loud。
func TestEWMA_ConstructorValidation(t *testing.T) {
	for _, a := range []float64{0, -0.1, 1.0001, 2, math.NaN(), math.Inf(1)} {
		if _, err := indicator.NewEWMA(a); err == nil {
			t.Errorf("NewEWMA(%v) 应报错，却成功", a)
		}
	}
	for _, a := range []float64{1, 0.5, 0.9, 1e-6} {
		if _, err := indicator.NewEWMA(a); err != nil {
			t.Errorf("NewEWMA(%v) 应成功: %v", a, err)
		}
	}
}

// TestEWMA_HandComputedFixture 手算数值 fixture（抓公式错）。
//
// EWMA(α=0.9) 对 xs = [10,20,...,90]，递推 prev = 0.9·x + 0.1·prev，首值 = x[0]：
//
//	idx0 = 10
//	idx1 = 0.9·20 + 0.1·10   = 19
//	idx2 = 0.9·30 + 0.1·19   = 28.9
//	idx3 = 0.9·40 + 0.1·28.9 = 38.89
//	idx4 = 0.9·50 + 0.1·38.89 = 48.889
//	idx5 = 0.9·60 + 0.1·48.889 = 58.8889
//	idx6 = 0.9·70 + 0.1·58.8889 = 68.88889
//	idx7 = 0.9·80 + 0.1·68.88889 = 78.888889
//	idx8 = 0.9·90 + 0.1·78.888889 = 88.8888889
//
// warmup(α=0.9) = ceil(ln(1e-6)/ln(0.1)) = 6 → 可观测 idx5..8。
// 期望值为手写常量（不调用被测代码推算）。
func TestEWMA_HandComputedFixture(t *testing.T) {
	xs := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90}
	warmup := 6
	want := []float64{
		math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(),
		58.8889, 68.888890000000004, 78.888889000000006, 88.888888899999998,
	}

	got, err := indicator.EWMABatch(xs, 0.9)
	if err != nil {
		t.Fatalf("EWMABatch: %v", err)
	}
	for i := range want {
		if !closeEnough(got[i], want[i]) {
			t.Errorf("EWMABatch[%d] = %.17g, want %.17g", i, got[i], want[i])
		}
	}

	ind, err := indicator.NewEWMA(0.9)
	if err != nil {
		t.Fatalf("NewEWMA: %v", err)
	}
	if w := ind.Warmup(); w != warmup {
		t.Fatalf("EWMA(0.9).Warmup() = %d, want %d", w, warmup)
	}
	for i, x := range xs {
		if err := ind.Update(x); err != nil {
			t.Fatalf("Update t=%d: %v", i, err)
		}
		v, verr := ind.Value()
		if i < warmup-1 {
			if verr == nil {
				t.Errorf("t=%d warmup 未完成却返回值 %v", i, v)
			}
			continue
		}
		if verr != nil {
			t.Fatalf("Value t=%d: %v", i, verr)
		}
		if !closeEnough(v, want[i]) {
			t.Errorf("Step Value[%d] = %.17g, want %.17g", i, v, want[i])
		}
	}
}

// TestEWMA_WarmupDerivationFromFormula 钉住 warmup 是参数的函数，不是常数
// （ADR-028 §4 明确「不能硬编码成一个常数」）。
func TestEWMA_WarmupDerivationFromFormula(t *testing.T) {
	cases := []struct {
		alpha float64
		want  int
	}{
		{1, 1},    // 特判：恒等
		{0.5, 20}, // ln(1e-6)/ln(0.5) = 19.93 → 20（§4 表内公式边界）
		{0.9, 6},  // (0.1)^6 = 1e-6
	}
	for _, c := range cases {
		ind, err := indicator.NewEWMA(c.alpha)
		if err != nil {
			t.Fatalf("NewEWMA(%v): %v", c.alpha, err)
		}
		if got := ind.Warmup(); got != c.want {
			t.Errorf("EWMA(α=%v).Warmup() = %d, want %d", c.alpha, got, c.want)
		}
	}
}

// TestEWMA_AlphaOneIdentity α=1 → 恒等，warmup=1。
func TestEWMA_AlphaOneIdentity(t *testing.T) {
	xs := []float64{3, -1.5, 0, 7.25, 100}
	got, err := indicator.EWMABatch(xs, 1)
	if err != nil {
		t.Fatalf("EWMABatch: %v", err)
	}
	for i := range xs {
		if got[i] != xs[i] {
			t.Errorf("EWMA(1)[%d] = %v, want %v（恒等）", i, got[i], xs[i])
		}
	}
	ind, _ := indicator.NewEWMA(1)
	if w := ind.Warmup(); w != 1 {
		t.Errorf("EWMA(1).Warmup() = %d, want 1", w)
	}
}

// TestEWMA_WarmupBoundary 喂 warmup-1 → error；喂第 warmup → 有值。
func TestEWMA_WarmupBoundary(t *testing.T) {
	ind, err := indicator.NewEWMA(0.5)
	if err != nil {
		t.Fatalf("NewEWMA: %v", err)
	}
	if w := ind.Warmup(); w != 20 {
		t.Fatalf("EWMA(0.5).Warmup() = %d, want 20", w)
	}
	assertWarmupBoundary(t, "EWMA(0.5)", ind, func(i int) float64 { return float64(i + 1) })
}

// TestEWMA_UpdateAtomicity NaN/±Inf → error 且状态逐位不变。
func TestEWMA_UpdateAtomicity(t *testing.T) {
	ind, _ := indicator.NewEWMA(0.9)
	for _, x := range []float64{10, 20, 30, 40, 50, 60, 70} {
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
		if vAfter, err := ind.Value(); err != nil || vAfter != vBefore {
			t.Errorf("Update(%v) 失败后 Value 变了: %v → %v", bad, vBefore, vAfter)
		}
	}
}

// TestEWMA_LoadStateFailLoud 垃圾/null/空/版本不符/缺字段 → error 且接收者原子不变。
func TestEWMA_LoadStateFailLoud(t *testing.T) {
	ind, _ := indicator.NewEWMA(0.9)
	for _, x := range []float64{1, 2, 3, 4, 5, 6} {
		_ = ind.Update(x)
	}
	before, _ := ind.SaveState()

	bad := [][]byte{
		nil,
		{},
		[]byte("null"),
		[]byte("garbage"),
		[]byte(`{"version":2,"count":1,"prev":1}`),
		[]byte(`{"count":1,"prev":1}`),    // 缺 version
		[]byte(`{"version":1,"prev":1}`),  // 缺 count
		[]byte(`{"version":1,"count":1}`), // 缺 prev
		[]byte(`{"version":1,"count":1,"prev":1,"extra":9}`),
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

// TestEWMA_BatchValidation Batch 参数校验与构造函数同规；空/nil 无错。
func TestEWMA_BatchValidation(t *testing.T) {
	if _, err := indicator.EWMABatch([]float64{1, 2}, 0); err == nil {
		t.Error("EWMABatch α=0 应报错")
	}
	if _, err := indicator.EWMABatch([]float64{1, 2}, 1.5); err == nil {
		t.Error("EWMABatch α=1.5 应报错")
	}
	if _, err := indicator.EWMABatch([]float64{1, math.NaN()}, 0.5); err == nil {
		t.Error("EWMABatch 含 NaN 应报错")
	}
	for _, in := range [][]float64{nil, {}} {
		got, err := indicator.EWMABatch(in, 0.5)
		if err != nil {
			t.Errorf("EWMABatch(空) 应无错: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("EWMABatch(空) 长度 = %d, want 0", len(got))
		}
	}
}
