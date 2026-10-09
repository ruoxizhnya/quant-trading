// 三算子的实例 Spec() 契约测试（K3 切片 2）。
//
// 参数化算子的 OperatorSpec 由**实例**产出：Warmup 随构造参数变化，静态
// 注册表（expression 侧）装不下。本测试钉住「实例给出的 Warmup 与算子自身
// Warmup() 一致」以及 Init 词表取值。
package indicator_test

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// allowedInit 是 ADR-028 §4 init 的受控词表（K3 切片 2 裁决）。
var allowedInit = map[string]bool{
	"none":         true,
	"x[0]":         true,
	"sma(first,N)": true,
	"p0=r":         true,
	"zero":         true,
	"inf(never)":   true,
}

func TestSpec_RMA_WarmupEqualsN(t *testing.T) {
	for _, n := range []int{1, 2, 14, 50} {
		ind, err := indicator.NewRMA(n)
		if err != nil {
			t.Fatalf("NewRMA(%d): %v", n, err)
		}
		spec := ind.Spec()
		if spec.Name != "ts_rma" {
			t.Errorf("RMA(%d).Spec().Name = %q, want ts_rma", n, spec.Name)
		}
		if spec.Warmup != n {
			t.Errorf("RMA(%d).Spec().Warmup = %d, want %d", n, spec.Warmup, n)
		}
		if spec.Warmup != ind.Warmup() {
			t.Errorf("RMA(%d).Spec().Warmup(%d) != Warmup()(%d)", n, spec.Warmup, ind.Warmup())
		}
		if !spec.State || !spec.Causal {
			t.Errorf("RMA(%d).Spec(): State=%v Causal=%v, want true/true", n, spec.State, spec.Causal)
		}
		if spec.Lookback != 0 {
			t.Errorf("RMA(%d).Spec().Lookback = %d, want 0（∞ 约定）", n, spec.Lookback)
		}
		if !allowedInit[spec.Init] {
			t.Errorf("RMA(%d).Spec().Init = %q 不在受控词表内", n, spec.Init)
		}
	}
}

func TestSpec_EWMA_WarmupFormula(t *testing.T) {
	for _, alpha := range []float64{0.1, 0.3, 0.5, 1.0} {
		ind, err := indicator.NewEWMA(alpha)
		if err != nil {
			t.Fatalf("NewEWMA(%v): %v", alpha, err)
		}
		spec := ind.Spec()
		want := 1
		if alpha < 1 {
			want = int(math.Ceil(math.Log(1e-6) / math.Log(1-alpha)))
			if want < 1 {
				want = 1
			}
		}
		if spec.Warmup != want {
			t.Errorf("EWMA(%v).Spec().Warmup = %d, want %d", alpha, spec.Warmup, want)
		}
		if spec.Name != "ts_ewma" {
			t.Errorf("EWMA(%v).Spec().Name = %q, want ts_ewma", alpha, spec.Name)
		}
		if !allowedInit[spec.Init] {
			t.Errorf("EWMA(%v).Spec().Init = %q 不在受控词表内", alpha, spec.Init)
		}
	}
	// α=0.5 → 20 根（与 ADR-028 §4 订正记录一致）。
	if ind, _ := indicator.NewEWMA(0.5); ind.Spec().Warmup != 20 {
		t.Errorf("EWMA(0.5).Spec().Warmup = %d, want 20", ind.Spec().Warmup)
	}
}

func TestSpec_Kalman_WarmupOne(t *testing.T) {
	ind, err := indicator.NewKalman(0.1, 0.5)
	if err != nil {
		t.Fatalf("NewKalman: %v", err)
	}
	spec := ind.Spec()
	if spec.Name != "ts_kalman" {
		t.Errorf("Kalman.Spec().Name = %q, want ts_kalman", spec.Name)
	}
	if spec.Warmup != 1 {
		t.Errorf("Kalman.Spec().Warmup = %d, want 1", spec.Warmup)
	}
	if !spec.State || !spec.Causal || spec.Lookback != 0 {
		t.Errorf("Kalman.Spec(): State=%v Causal=%v Lookback=%d, want true/true/0",
			spec.State, spec.Causal, spec.Lookback)
	}
	if !allowedInit[spec.Init] {
		t.Errorf("Kalman.Spec().Init = %q 不在受控词表内", spec.Init)
	}
}
