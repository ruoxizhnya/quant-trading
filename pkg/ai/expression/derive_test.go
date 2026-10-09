// warmup / lookback 静态推导测试（ADR-028 §8 + 附录 A）。
//
// 只钉住推导函数本身，不涉及策略 / 引擎接线（属后续切片）。
package expression

import "testing"

func TestDeriveWarmup_AppendixA(t *testing.T) {
	// ADR-028 附录 A：ts_mean(close,20) → 19。
	if got := DeriveWarmup(mustParse(t, "ts_mean(close, 20)")); got != 19 {
		t.Errorf("warmup(ts_mean(close,20)) = %d, want 19", got)
	}
	// 两条并行链 → max(19,14) = 19。
	if got := DeriveWarmup(mustParse(t, "ts_mean(close, 20) + ts_rma(close, 14)")); got != 19 {
		t.Errorf("warmup(并行链) = %d, want max(19,14)=19", got)
	}
	// 单个 L2 算子 → 14（= N，Wilder）。
	if got := DeriveWarmup(mustParse(t, "ts_rma(close, 14)")); got != 14 {
		t.Errorf("warmup(ts_rma(close,14)) = %d, want 14", got)
	}
}

func TestDeriveWarmup_SerialAccumulate(t *testing.T) {
	// 串行累加：ts_rma(ts_mean(close,5),14) → 4 + 14 = 18。
	if got := DeriveWarmup(mustParse(t, "ts_rma(ts_mean(close, 5), 14)")); got != 18 {
		t.Errorf("warmup(串行链) = %d, want 4+14=18", got)
	}
	// 一元包裹不改变自身 warmup（self=0）。
	if got := DeriveWarmup(mustParse(t, "abs(ts_mean(close, 20))")); got != 19 {
		t.Errorf("warmup(abs(ts_mean(close,20))) = %d, want 19", got)
	}
}

func TestDeriveWarmup_Parameterized(t *testing.T) {
	cases := []struct {
		expr string
		want int
	}{
		{"ts_mean(close, 5)", 4},
		{"ts_delay(close, 1)", 1},
		{"ts_delta(close, 3)", 3},
		{"ts_pct_change(close, 20)", 20},
		{"ts_ewma(close, 0.5)", 20}, // ceil(ln(1e-6)/ln(0.5)) = 20
		{"ts_kalman(close, 0.1, 0.5)", 1},
		{"close", 0},
		{"close + open", 0},
	}
	for _, tc := range cases {
		if got := DeriveWarmup(mustParse(t, tc.expr)); got != tc.want {
			t.Errorf("warmup(%q) = %d, want %d", tc.expr, got, tc.want)
		}
	}
}

func TestDeriveLookback_Finite(t *testing.T) {
	cases := []struct {
		expr string
		want int
	}{
		{"close", 0},
		{"ts_mean(close, 20)", 19},
		{"ts_delay(close, 1)", 1},
		{"ts_delay(ts_mean(close, 20), 1)", 20}, // 串行累加 19 + 1
		{"cs_rank(ts_pct_change(close, 20))", 20},
	}
	for _, tc := range cases {
		bars, inf := DeriveLookback(mustParse(t, tc.expr))
		if inf {
			t.Errorf("lookback(%q): infinite=true, want false", tc.expr)
			continue
		}
		if bars != tc.want {
			t.Errorf("lookback(%q) = %d, want %d", tc.expr, bars, tc.want)
		}
	}
}

func TestDeriveLookback_InfiniteOnStateful(t *testing.T) {
	// 任一 State=true ⇒ infinite=true。
	infinite := []string{
		"ts_rma(close, 14)",
		"ts_ewma(close, 0.3)",
		"ts_kalman(close, 0.1, 0.5)",
		"ts_mean(close, 20) + ts_rma(close, 14)",
		"cs_rank(ts_rma(close, 14))",
	}
	for _, s := range infinite {
		if _, inf := DeriveLookback(mustParse(t, s)); !inf {
			t.Errorf("lookback(%q): infinite=false, want true（含 State=true 节点）", s)
		}
	}
}
