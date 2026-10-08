// pkg/indicator Kalman 测试（K3 切片 1）。
package indicator_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// TestKalman_ConstructorValidation 参数非法必须 fail-loud（q ≥ 0、r > 0）。
func TestKalman_ConstructorValidation(t *testing.T) {
	bad := []struct{ q, r float64 }{
		{-1, 1}, {1, 0}, {1, -0.5}, {math.NaN(), 1}, {1, math.NaN()},
		{math.Inf(1), 1}, {1, math.Inf(1)},
	}
	for _, c := range bad {
		if _, err := indicator.NewKalman(c.q, c.r); err == nil {
			t.Errorf("NewKalman(q=%v, r=%v) 应报错，却成功", c.q, c.r)
		}
	}
	for _, c := range []struct{ q, r float64 }{{0, 1}, {0.1, 0.5}, {5, 0.001}} {
		if _, err := indicator.NewKalman(c.q, c.r); err != nil {
			t.Errorf("NewKalman(q=%v, r=%v) 应成功: %v", c.q, c.r, err)
		}
	}
}

// TestKalman_HandComputedFixture 手算数值 fixture（抓公式错，核心）。
//
// Kalman(q=0.1, r=0.5) 对 z = [1, 2, 4]，p0 = r = 0.5：
//
//	idx0: 首观测 → x = z = 1; p = p0 = 0.5
//	idx1: p = 0.5 + 0.1 = 0.6
//	      k = 0.6/(0.6+0.5) = 6/11
//	      x = 1 + (6/11)(2-1) = 17/11 = 1.5454545454545454
//	      p = (1-6/11)·0.6 = (5/11)(3/5) = 3/11 = 0.27272727272727276
//	idx2: p = 3/11 + 1/10 = 41/110
//	      k = (41/110)/(41/110 + 1/2) = (41/110)/(96/110) = 41/96
//	      x = 17/11 + (41/96)(4-17/11) = 17/11 + (41/96)(27/11)
//	        = 544/352 + 1107/1056·... = 913/352 = 2.59375
//	      p = (1-41/96)·(41/110) = (55/96)(41/110) = 41/192 = 0.21354166666666669
//
// 期望值为手写常量（不调用被测代码推算）。warmup=1，故无 NaN 前导。
func TestKalman_HandComputedFixture(t *testing.T) {
	z := []float64{1, 2, 4}
	want := []float64{1, 1.5454545454545454, 2.59375}

	got, err := indicator.KalmanBatch(z, 0.1, 0.5)
	if err != nil {
		t.Fatalf("KalmanBatch: %v", err)
	}
	for i := range want {
		if !closeEnough(got[i], want[i]) {
			t.Errorf("KalmanBatch[%d] = %.17g, want %.17g", i, got[i], want[i])
		}
	}

	ind, err := indicator.NewKalman(0.1, 0.5)
	if err != nil {
		t.Fatalf("NewKalman: %v", err)
	}
	if w := ind.Warmup(); w != 1 {
		t.Fatalf("Kalman.Warmup() = %d, want 1", w)
	}
	for i, v := range z {
		if err := ind.Update(v); err != nil {
			t.Fatalf("Update t=%d: %v", i, err)
		}
		got, verr := ind.Value()
		if verr != nil {
			t.Fatalf("Value t=%d: %v", i, verr)
		}
		if !closeEnough(got, want[i]) {
			t.Errorf("Step Value[%d] = %.17g, want %.17g", i, got, want[i])
		}
	}
}

// TestKalman_CovarianceStateHandCheck 钉住协方差 p 的递推（防止 p 不更新
// 这类「只改核、fixture 才抓得住」的 bug）。
func TestKalman_CovarianceStateHandCheck(t *testing.T) {
	ind, _ := indicator.NewKalman(0.1, 0.5)
	for _, z := range []float64{1, 2, 4} {
		_ = ind.Update(z)
	}
	blob, err := ind.SaveState()
	if err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	// p 期望 = 41/192（见上），检查序列化里确实带上了这个内部量。
	if !bytes.Contains(blob, []byte("0.213541666666666")) {
		t.Errorf("SaveState 未包含期望的协方差 p≈41/192: %s", blob)
	}
}

// TestKalman_WarmupBoundary warmup=1：喂 0 根 → error；喂 1 根 → 有值。
func TestKalman_WarmupBoundary(t *testing.T) {
	ind, _ := indicator.NewKalman(0.1, 0.5)
	if w := ind.Warmup(); w != 1 {
		t.Fatalf("Kalman.Warmup() = %d, want 1", w)
	}
	if _, err := ind.Value(); err == nil {
		t.Error("未喂任何观测却返回值（warmup 未完成应 error）")
	}
	if err := ind.Update(3.14); err != nil {
		t.Fatalf("Update: %v", err)
	}
	v, err := ind.Value()
	if err != nil {
		t.Fatalf("喂 1 根后 Value 应成功: %v", err)
	}
	if v != 3.14 {
		t.Errorf("首观测后 Value = %v, want 3.14（x = z）", v)
	}
}

// TestKalman_UpdateAtomicity NaN/±Inf → error 且状态逐位不变。
func TestKalman_UpdateAtomicity(t *testing.T) {
	ind, _ := indicator.NewKalman(0.1, 0.5)
	for _, z := range []float64{1, 2, 4, 3, 5} {
		if err := ind.Update(z); err != nil {
			t.Fatalf("Update(%v): %v", z, err)
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

// TestKalman_LoadStateFailLoud 垃圾/null/空/版本不符/缺字段 → error 且接收者原子不变。
func TestKalman_LoadStateFailLoud(t *testing.T) {
	ind, _ := indicator.NewKalman(0.1, 0.5)
	for _, z := range []float64{1, 2, 4, 3} {
		_ = ind.Update(z)
	}
	before, _ := ind.SaveState()

	bad := [][]byte{
		nil,
		{},
		[]byte("null"),
		[]byte("garbage"),
		[]byte(`{"version":0,"count":1,"x":1,"p":1}`),
		[]byte(`{"count":1,"x":1,"p":1}`),       // 缺 version
		[]byte(`{"version":1,"x":1,"p":1}`),     // 缺 count
		[]byte(`{"version":1,"count":1,"p":1}`), // 缺 x
		[]byte(`{"version":1,"count":1,"x":1}`), // 缺 p
		[]byte(`{"version":1,"count":1,"x":1,"p":1,"zzz":0}`),
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

// TestKalman_BatchValidation Batch 参数校验与构造函数同规；空/nil 无错。
func TestKalman_BatchValidation(t *testing.T) {
	if _, err := indicator.KalmanBatch([]float64{1}, -1, 1); err == nil {
		t.Error("KalmanBatch q<0 应报错")
	}
	if _, err := indicator.KalmanBatch([]float64{1}, 1, 0); err == nil {
		t.Error("KalmanBatch r=0 应报错")
	}
	if _, err := indicator.KalmanBatch([]float64{math.NaN()}, 0.1, 0.5); err == nil {
		t.Error("KalmanBatch 含 NaN 应报错")
	}
	for _, in := range [][]float64{nil, {}} {
		got, err := indicator.KalmanBatch(in, 0.1, 0.5)
		if err != nil {
			t.Errorf("KalmanBatch(空) 应无错: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("KalmanBatch(空) 长度 = %d, want 0", len(got))
		}
	}
}
