// L2 递推算子经 DSL 求值的一致性测试（K3 切片 2）。
//
// 核心断言：DSL 求值结果 == indicator 包的 Batch 实现（容差 1e-12）。
// 这同时证明求值**复用**了 indicator 的 Batch（不是 expression 里的第二份
// 递推），且是「整条一次算完」而非「每根 bar 重算」。
package expression

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// l2Series 是一段确定性的小序列（非单调、含不同量级）。
var l2Series = []float64{
	10, 11, 9, 13, 12, 15, 14, 16, 15, 18,
	17, 20, 19, 22, 21, 24, 23, 26, 25, 28,
}

func l2Provider() *mockProvider {
	return &mockProvider{
		symbols: []string{"A"},
		data:    map[string]map[string][]float64{"A": {"close": l2Series}},
	}
}

// assertSeriesCloseTo 逐点比较（NaN 视同相等），容差 tol。
func assertSeriesCloseTo(t *testing.T, got, want []float64, tol float64, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: 长度 %d != %d", label, len(got), len(want))
	}
	for i := range want {
		if math.IsNaN(want[i]) && math.IsNaN(got[i]) {
			continue
		}
		if d := math.Abs(got[i] - want[i]); d > tol {
			t.Errorf("%s[%d]: DSL=%.17g vs indicator=%.17g（diff=%.3g）", label, i, got[i], want[i], d)
		}
	}
}

func TestL2Eval_RMA_MatchesIndicatorBatch(t *testing.T) {
	got, err := NewEvaluator(l2Provider()).Evaluate(mustParse(t, "ts_rma(close, 14)"), 0)
	if err != nil {
		t.Fatalf("evaluate ts_rma: %v", err)
	}
	want, err := indicator.RMABatch(l2Series, 14)
	if err != nil {
		t.Fatalf("RMABatch: %v", err)
	}
	assertSeriesCloseTo(t, got["A"], want, 1e-12, "ts_rma(close,14)")
}

func TestL2Eval_EWMA_MatchesIndicatorBatch(t *testing.T) {
	const alpha = 0.3
	got, err := NewEvaluator(l2Provider()).Evaluate(mustParse(t, "ts_ewma(close, 0.3)"), 0)
	if err != nil {
		t.Fatalf("evaluate ts_ewma: %v", err)
	}
	want, err := indicator.EWMABatch(l2Series, alpha)
	if err != nil {
		t.Fatalf("EWMABatch: %v", err)
	}
	assertSeriesCloseTo(t, got["A"], want, 1e-12, "ts_ewma(close,0.3)")
}

func TestL2Eval_Kalman_MatchesIndicatorBatch(t *testing.T) {
	const q, r = 0.1, 0.5
	got, err := NewEvaluator(l2Provider()).Evaluate(mustParse(t, "ts_kalman(close, 0.1, 0.5)"), 0)
	if err != nil {
		t.Fatalf("evaluate ts_kalman: %v", err)
	}
	want, err := indicator.KalmanBatch(l2Series, q, r)
	if err != nil {
		t.Fatalf("KalmanBatch: %v", err)
	}
	assertSeriesCloseTo(t, got["A"], want, 1e-12, "ts_kalman(close,0.1,0.5)")
}

// L2 算子可作为子表达式嵌套（x 为任意子表达式，不只是裸字段）。
func TestL2Eval_SubExpressionArgument(t *testing.T) {
	got, err := NewEvaluator(l2Provider()).Evaluate(mustParse(t, "ts_rma(close * 2, 14)"), 0)
	if err != nil {
		t.Fatalf("evaluate ts_rma(close*2,14): %v", err)
	}
	scaled := make([]float64, len(l2Series))
	for i, v := range l2Series {
		scaled[i] = v * 2
	}
	want, err := indicator.RMABatch(scaled, 14)
	if err != nil {
		t.Fatalf("RMABatch: %v", err)
	}
	assertSeriesCloseTo(t, got["A"], want, 1e-12, "ts_rma(close*2,14)")
}
