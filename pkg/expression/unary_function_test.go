// 一元算子的**函数形式**回归测试。
//
// P2-12 接 PE/PB 时发现的潜伏 bug：`neg(x)` 解析得过（parser 认它是函数
// 调用），但 evaluateFunction 无条件把它丢给 applyTimeSeriesOp，于是报错
// "unknown time-series operator: neg"。
//
// 受影响的不只是新写的估值表达式 —— multi_factor 的默认表达式
// `cs_rank(neg(ts_std(close, 20)))` 从落地那天起就是坏的：它只被断言过
// "能解析"，从没被真正求值过。
package expression

import (
	"math"
	"testing"
)

func TestNegFunctionForm(t *testing.T) {
	p := &mockProvider{
		symbols: []string{"A", "B"},
		data: map[string]map[string][]float64{
			"A": {"close": {10, 20, 30}},
			"B": {"close": {-5, 0, 7}},
		},
	}
	ev := NewEvaluator(p)

	got, err := ev.Evaluate(mustParse(t, "neg(close)"), 0)
	if err != nil {
		t.Fatalf("neg(close): %v", err)
	}
	assertSeries(t, got, "A", []float64{-10, -20, -30})
	assertSeries(t, got, "B", []float64{5, -0.0, -7})
}

// cs_rank(neg(...)) 是「越低越好」类因子（估值、波动）的标准写法，
// multi_factor 和 value 的默认表达式都靠它。
func TestNegInsideCrossSectionalRank(t *testing.T) {
	p := &mockProvider{
		symbols: []string{"CHEAP", "MID", "PRICY"},
		data: map[string]map[string][]float64{
			"CHEAP": {"pe": {8}},
			"MID":   {"pe": {20}},
			"PRICY": {"pe": {60}},
		},
	}
	ev := NewEvaluator(p)

	got, err := ev.Evaluate(mustParse(t, "cs_rank(neg(pe))"), 0)
	if err != nil {
		t.Fatalf("cs_rank(neg(pe)): %v", err)
	}
	// neg 之后 CHEAP 变成最大（-8 > -20 > -60），排名 1.0。
	assertSeries(t, got, "CHEAP", []float64{1.0})
	assertSeries(t, got, "MID", []float64{0.5})
	assertSeries(t, got, "PRICY", []float64{0.0})
}

func TestUnaryOpsOtherThanNeg(t *testing.T) {
	p := &mockProvider{
		symbols: []string{"A"},
		data:    map[string]map[string][]float64{"A": {"x": {-4, 0, 9}}},
	}
	ev := NewEvaluator(p)

	cases := []struct {
		expr string
		want []float64
	}{
		{"abs(x)", []float64{4, 0, 9}},
		{"sqrt(x)", []float64{math.NaN(), 0, 3}}, // 负数开方 = NaN，不报错
		{"sign(x)", []float64{-1, 0, 1}},
	}

	for _, tc := range cases {
		got, err := ev.Evaluate(mustParse(t, tc.expr), 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.expr, err)
		}
		assertSeries(t, got, "A", tc.want)
	}
}

// 时序算子不受影响：ts_mean(close, 2) 仍然走 applyTimeSeriesOp。
func TestTimeSeriesOpsStillWork(t *testing.T) {
	p := &mockProvider{
		symbols: []string{"A"},
		data:    map[string]map[string][]float64{"A": {"close": {10, 20, 30}}},
	}
	ev := NewEvaluator(p)

	got, err := ev.Evaluate(mustParse(t, "ts_mean(close, 2)"), 0)
	if err != nil {
		t.Fatalf("ts_mean(close, 2): %v", err)
	}
	// tsMean 输出长度与输入一致，前 window-1 个是 NaN。
	assertSeries(t, got, "A", []float64{math.NaN(), 15, 25})
}

// ─── helpers ───────────────────────────────────────────────────────────

func mustParse(t *testing.T, expr string) Node {
	t.Helper()
	parsed, err := NewParser().Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	if parsed == nil || parsed.AST == nil {
		t.Fatalf("parse %q: nil AST", expr)
	}
	return parsed.AST
}

func assertSeries(t *testing.T, got map[string][]float64, symbol string, want []float64) {
	t.Helper()
	vals, ok := got[symbol]
	if !ok {
		t.Fatalf("no result for symbol %s (got keys %v)", symbol, keysOf(got))
	}
	if len(vals) != len(want) {
		t.Fatalf("%s: got %d values %v, want %d %v", symbol, len(vals), vals, len(want), want)
	}
	for i := range want {
		if math.IsNaN(want[i]) {
			if !math.IsNaN(vals[i]) {
				t.Errorf("%s[%d] = %v, want NaN", symbol, i, vals[i])
			}
			continue
		}
		if math.Abs(vals[i]-want[i]) > 1e-9 {
			t.Errorf("%s[%d] = %v, want %v", symbol, i, vals[i], want[i])
		}
	}
}

func keysOf(m map[string][]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
