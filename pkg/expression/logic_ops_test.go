// OBS-07：逻辑与比较算子（>= <= AND OR NOT）测试。
//
// 三层覆盖：
//  1. 纯函数层（applyBinaryOp / applyUnaryOp）：0/1 语义 + **NaN 传播**；
//  2. AST 层：优先级结构（比较 > AND > OR；NOT 低于比较）；
//  3. 端到端：Parse → Evaluate 全链。
//
// 注册表自洽（样例/解析/闸门）已由 registry_test.go 的 sampleExpr 覆盖，
// 提示词派生（AvailableOperators 全集进提示词）由 agents 侧护栏覆盖。
package expression

import (
	"math"
	"testing"
)

// ─── 1. 纯函数层 ────────────────────────────────────────────────────

func TestLogicOps_ComparisonSemantics(t *testing.T) {
	cases := []struct {
		op   string
		a, b float64
		want float64
	}{
		{">=", 2, 2, 1}, {">=", 1, 2, 0}, {">=", 3, 2, 1},
		{"<=", 2, 2, 1}, {"<=", 3, 2, 0}, {"<=", 1, 2, 1},
	}
	for _, tc := range cases {
		if got := applyBinaryOp(tc.op, tc.a, tc.b); got != tc.want {
			t.Errorf("applyBinaryOp(%q, %v, %v) = %v, want %v", tc.op, tc.a, tc.b, got, tc.want)
		}
	}
}

// TestLogicOps_NaNPropagatesThroughLogic 是 OBS-07 的语义核心：Go 里
// NaN != 0 恒为 true，逻辑算子若不特判，「谓词 AND NaN」会组合出 1 ——
// 假信号。NaN 必须传播（与比较算子「NaN 参与比较得 false→0」不同：比较是
// 逐点产生谓词，NaN 参与即「不满足」；AND/OR 是组合谓词，操作数未知则结果
// 未知）。
func TestLogicOps_NaNPropagatesThroughLogic(t *testing.T) {
	nan := math.NaN()

	if got := applyBinaryOp("AND", nan, 1); !math.IsNaN(got) {
		t.Errorf("AND(NaN, 1) = %v, want NaN（不传播会组合出假信号）", got)
	}
	if got := applyBinaryOp("AND", 1, nan); !math.IsNaN(got) {
		t.Errorf("AND(1, NaN) = %v, want NaN", got)
	}
	if got := applyBinaryOp("OR", nan, 0); !math.IsNaN(got) {
		t.Errorf("OR(NaN, 0) = %v, want NaN", got)
	}
	if got := applyUnaryOp("NOT", nan); !math.IsNaN(got) {
		t.Errorf("NOT(NaN) = %v, want NaN", got)
	}
}

func TestLogicOps_BooleanSemantics(t *testing.T) {
	cases := []struct {
		op   string
		a, b float64
		want float64
	}{
		{"AND", 1, 1, 1}, {"AND", 1, 0, 0}, {"AND", 0, 0, 0},
		{"AND", -1, 2, 1}, // 非零即真（负数也是真）
		{"OR", 0, 0, 0}, {"OR", 1, 0, 1}, {"OR", 0, 3, 1},
	}
	for _, tc := range cases {
		if got := applyBinaryOp(tc.op, tc.a, tc.b); got != tc.want {
			t.Errorf("applyBinaryOp(%q, %v, %v) = %v, want %v", tc.op, tc.a, tc.b, got, tc.want)
		}
	}
	if got := applyUnaryOp("NOT", 0); got != 1 {
		t.Errorf("NOT(0) = %v, want 1", got)
	}
	if got := applyUnaryOp("NOT", 2); got != 0 {
		t.Errorf("NOT(2) = %v, want 0", got)
	}
}

// ─── 2. AST 层：优先级结构 ──────────────────────────────────────────

// astShape 把 AST 压成 S 表达式，便于断言优先级结构。
func astShape(n Node) string {
	switch v := n.(type) {
	case *LiteralNode:
		return strconvFormat(v.Value)
	case *IdentifierNode:
		return v.Name
	case *UnaryOpNode:
		return "(" + v.Op + " " + astShape(v.Expr) + ")"
	case *BinaryOpNode:
		return "(" + astShape(v.Left) + " " + v.Op + " " + astShape(v.Right) + ")"
	case *FunctionNode:
		return "(" + v.Name + " ...)"
	default:
		return "?"
	}
}

func strconvFormat(f float64) string {
	if f == math.Trunc(f) {
		return int64ToString(int64(f))
	}
	return "f"
}

func int64ToString(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func parseShape(t *testing.T, expr string) string {
	t.Helper()
	parsed, err := NewParser().Parse(expr)
	if err != nil {
		t.Fatalf("解析 %q 失败: %v", expr, err)
	}
	return astShape(parsed.AST)
}

// TestLogicOps_Precedence 优先级是这套算子最容易写错的地方，用结构断言钉死。
func TestLogicOps_Precedence(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		// 比较 > AND：两边各自成比较，再 AND。
		{"close > 1 AND close < 9", "((close > 1) AND (close < 9))"},
		// AND > OR：`a AND b OR c` = `(a AND b) OR c`。
		{"close > 1 AND close < 9 OR close == 2", "(((close > 1) AND (close < 9)) OR (close == 2))"},
		// NOT 低于比较：NOT(close > 1)，而不是 (NOT close) > 1。
		{"not close > 1", "(NOT (close > 1))"},
		// NOT 右结合可叠加。
		{"NOT NOT close > 1", "(NOT (NOT (close > 1)))"},
		// 算术高于比较：先算 close + 1 再比较。
		{"close + 1 > 2 AND close < 9", "(((close + 1) > 2) AND (close < 9))"},
		// OR 链左结合。
		{"close > 1 OR close > 2 OR close > 3", "(((close > 1) OR (close > 2)) OR (close > 3))"},
		// 括号重置优先级。
		{"close > 1 AND (close > 2 OR close > 3)", "((close > 1) AND ((close > 2) OR (close > 3)))"},
	}
	for _, tc := range cases {
		if got := parseShape(t, tc.expr); got != tc.want {
			t.Errorf("优先级结构不符：%s\n  got  %s\n  want %s", tc.expr, got, tc.want)
		}
	}
}

// TestLogicOps_CaseInsensitiveKeywords 关键字大小写不敏感（统一归一到大写
// 注册名），AI 无论写 and/And/AND 都能解析。
func TestLogicOps_CaseInsensitiveKeywords(t *testing.T) {
	cases := []struct {
		written   string // 书写形态
		canonical string // 归一后的注册名
	}{
		{"and", "AND"}, {"And", "AND"}, {"AND", "AND"},
		{"or", "OR"}, {"Or", "OR"}, {"OR", "OR"},
		{"not", "NOT"}, {"Not", "NOT"}, {"NOT", "NOT"},
	}
	for _, tc := range cases {
		var expr, want string
		switch tc.canonical {
		case "NOT":
			expr = tc.written + " close > 1"
			want = "(NOT (close > 1))"
		default: // AND / OR 二元
			expr = "close > 1 " + tc.written + " close < 9"
			want = "((close > 1) " + tc.canonical + " (close < 9))"
		}
		if got := parseShape(t, expr); got != want {
			t.Errorf("关键字 %q 归一不符：%s\n  got  %s\n  want %s", tc.written, expr, got, want)
		}
	}
}

// ─── 3. 端到端求值 ──────────────────────────────────────────────────

func logicOpsProvider() *mockProvider {
	return &mockProvider{
		symbols: []string{"S1"},
		data: map[string]map[string][]float64{
			"S1": {"close": {3, 1, 5}},
		},
	}
}

func evalExpr(t *testing.T, expr string) []float64 {
	t.Helper()
	parsed, err := NewParser().Parse(expr)
	if err != nil {
		t.Fatalf("解析 %q 失败: %v", expr, err)
	}
	result, err := NewEvaluator(logicOpsProvider()).Evaluate(parsed.AST, 3)
	if err != nil {
		t.Fatalf("求值 %q 失败: %v", expr, err)
	}
	vals, ok := result["S1"]
	if !ok || len(vals) != 3 {
		t.Fatalf("求值 %q 结果异常: %v", expr, result)
	}
	return vals
}

func TestLogicOps_EndToEndEvaluation(t *testing.T) {
	cases := []struct {
		expr string
		want []float64 // close = [3, 1, 5]
	}{
		{"close >= 3", []float64{1, 0, 1}},
		{"close <= 1", []float64{0, 1, 0}},
		{"close > 1 AND close < 5", []float64{1, 0, 0}},
		{"close > 4 OR close < 2", []float64{0, 1, 1}},
		{"not close > 2", []float64{0, 1, 0}},
		{"close >= 3 AND (close < 2 OR close > 4)", []float64{0, 0, 1}}, // 括号 + 全家福
	}
	for _, tc := range cases {
		got := evalExpr(t, tc.expr)
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%s 第 %d 点 = %v, want %v（close=%v）", tc.expr, i, got[i], tc.want[i], []float64{3, 1, 5})
			}
		}
	}
}

// TestLogicOps_GateAcceptsCombinations 组合表达式必须过闸门（含嵌套 ts_ 算子），
// 且报错信息里的可用算子清单应包含新关键字。
func TestLogicOps_GateAcceptsCombinations(t *testing.T) {
	expr := "cs_rank(close) > 0.8 AND ts_pct_change(close, 5) >= 0 OR not cs_rank(open) < 0.2"
	parsed, err := NewParser().Parse(expr)
	if err != nil {
		t.Fatalf("组合表达式解析失败: %v", err)
	}
	if err := parsed.Validate(); err != nil {
		t.Fatalf("组合表达式过不了闸门: %v", err)
	}

	// 未登记算子的报错信息里应列出 AND/OR/NOT（AI 自纠依赖可用清单）。
	// 注意：`NAND(close, close)` 在**语法层**是合法的函数调用（Parse 成功），
	// 拦截它的是 Validate 闸门（OBS-06 语义：解析成功 ≠ 合法）。
	bad, badErr := NewParser().Parse("NAND(close, close)")
	if badErr != nil {
		t.Fatalf("NAND 语法层应可解析（拦截在闸门）: %v", badErr)
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("NAND 未登记，闸门应拒绝")
	} else {
		hint := err.Error()
		for _, kw := range []string{"AND", "OR", "NOT"} {
			if !contains(hint, kw) {
				t.Errorf("闸门报错的可用算子清单缺 %q：%s", kw, hint)
			}
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
