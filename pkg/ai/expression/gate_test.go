// L1 语法 + 算子闸门测试（OBS-06）。
//
// 覆盖：
//   - 正证据（假合法现场）：CROSS(MA(...)) / ts_mean(close) / nosuchfield+1
//     必须 valid=false，且 error 指明名字与可用算子；
//   - 反证腿：既有合法表达式仍必须通过（不能把正常表达式拦掉）；
//   - K3 切片 2：三个 L2 算子（ts_rma/ts_ewma/ts_kalman）被闸门认可。
package expression

import (
	"strings"
	"testing"
)

// validateExpr 解析并跑闸门，返回闸门结果（解析失败即视为不通过）。
func validateExpr(s string) error {
	expr, err := NewParser().Parse(s)
	if err != nil {
		return err
	}
	return expr.Validate()
}

func TestValidateGate_AcceptsValidExpressions(t *testing.T) {
	// 反证腿：这些是既有 / 新增的合法表达式，闸门必须放行。
	valid := []string{
		"close",
		"close / open",
		"ts_rank(close, 20)",
		"ts_mean(close, 3)",
		"ts_std(volume, 10)",
		"(close - ts_mean(close, 20)) / ts_std(close, 20)",
		"cs_rank(ts_pct_change(close, 20)) > 0.8",
		"cs_rank(neg(pe))",
		"cs_neutralize(close, sector)",
		"ts_corr(close, open, 20)",
		"ts_delay(close, 1) < ts_delay(open, 1)",
		"exp(log(close) + sqrt(volume))",
		// K3 切片 2：L2 算子被闸门认可。
		"ts_rma(close, 14) > 0",
		"ts_ewma(close, 0.3)",
		"ts_kalman(close, 0.1, 0.5)",
	}
	for _, s := range valid {
		if err := validateExpr(s); err != nil {
			t.Errorf("合法表达式被拦：%q → %v", s, err)
		}
	}
}

func TestValidateGate_RejectsUnknownOperators_OBS06(t *testing.T) {
	// 这是 OBS-06 的「假合法」现场：两个算子都不存在，旧闸门判 valid=true。
	s := "CROSS(MA(close, 5), MA(close, 20))"
	err := validateExpr(s)
	if err == nil {
		t.Fatalf("OBS-06 未修复：%q 被判合法", s)
	}
	msg := err.Error()
	if !strings.Contains(msg, "CROSS") {
		t.Errorf("错误信息未指明未知算子名 CROSS：%v", err)
	}
	if !strings.Contains(msg, "unknown operator") {
		t.Errorf("错误信息未说明 unknown operator：%v", err)
	}
	// 报错要列出可用算子，便于 AI 自纠。
	if !strings.Contains(msg, "ts_rma") || !strings.Contains(msg, "cs_rank") {
		t.Errorf("错误信息未列出可用算子：%v", err)
	}
}

func TestValidateGate_RejectsArityMismatch(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"ts_mean(close)", "expects 2 argument"},
		{"ts_corr(close, open)", "expects 3 argument"},
		{"ts_kalman(close, 0.1)", "expects 3 argument"},
		{"neg(close, open)", "expects 1 argument"},
	}
	for _, tc := range cases {
		err := validateExpr(tc.expr)
		if err == nil {
			t.Errorf("%q 应因参数个数不符被拦，却通过", tc.expr)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q 错误信息 = %v，want 含 %q", tc.expr, err, tc.want)
		}
	}
}

func TestValidateGate_RejectsUnknownField(t *testing.T) {
	err := validateExpr("nosuchfield + 1")
	if err == nil {
		t.Fatal("nosuchfield + 1 应被判非法")
	}
	if !strings.Contains(err.Error(), "nosuchfield") {
		t.Errorf("错误信息未指明未知字段：%v", err)
	}
	if !strings.Contains(err.Error(), "unknown data field") {
		t.Errorf("错误信息未说明 unknown data field：%v", err)
	}
}

func TestValidateGate_RejectsUnknownUnaryAndBinary(t *testing.T) {
	// pow 未登记（^ 才是幂），round 未登记。
	for _, s := range []string{"pow(close, 2)", "round(close)"} {
		if err := validateExpr(s); err == nil {
			t.Errorf("%q 应被判非法（未登记算子）", s)
		}
	}
}

// NewOperatorsAccepted 是 3.4-1 对 L2 算子的显式断言（名字与可用集合）。
func TestValidateGate_NewOperatorsListed(t *testing.T) {
	available := AvailableOperators()
	set := make(map[string]bool, len(available))
	for _, n := range available {
		set[n] = true
	}
	for _, op := range []string{"ts_rma", "ts_ewma", "ts_kalman"} {
		if !set[op] {
			t.Errorf("AvailableOperators() 缺 %q", op)
		}
	}
}
