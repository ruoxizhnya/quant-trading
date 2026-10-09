// K3a：闸门「标量参数位」校验测试。
//
// 病根（审查发现）：ADR-028 §4 的签名（`ts_mean : Series × Scalar → Series`）
// 此前只是 Spec.Signature 里的一句字符串，闸门**不校验参数位类型** ⇒
// `ts_mean(close, volume)` 参数个数与节点形态都合法、能过闸，但求值时
// firstScalar 取到序列首值（或 NaN）⇒ int(NaN) 未定义 ⇒ 窗口荒谬 ⇒
// **整条序列 NaN**。这是「假合法残留」（危害被 OBS-01 兜住，但浪费 AI 试验
// 且报错无指向性）。
//
// 本文件覆盖两条腿：
//  1. 病根必须被拦（序列/时序算子出现在标量位 → 拒绝）；
//  2. 反证腿：合法常量写法**不得误拒**（K3c 教训：误拒比假合法更危险）——
//     包括 `1/20` 这类常量表达式。
package expression

import (
	"strings"
	"testing"
)

// TestScalarParamGate_RejectsSeriesInScalarSlot 是 K3a 的核心正证据：把序列
// 塞进标量参数位必须被闸门拦在解析期，而不是留到求值期产出 NaN。
func TestScalarParamGate_RejectsSeriesInScalarSlot(t *testing.T) {
	cases := []struct {
		expr string
		why  string
	}{
		// ── 数据字段（序列）当窗口长度用：K3a 原始现场 ──
		{"ts_mean(close, volume)", "volume 是序列，不能当窗口 N"},
		{"ts_std(close, open)", "open 是序列，不能当窗口 N"},
		{"ts_delay(close, volume)", "volume 是序列，不能当延迟期数"},
		{"ts_rank(close, close)", "close 是序列，不能当窗口 N"},
		// ── 时序算子（产出序列）当标量用 ──
		{"ts_mean(close, ts_delay(volume, 1))", "ts_delay 产出序列，不是标量"},
		{"ts_ewma(close, ts_mean(close, 5))", "ts_mean 产出序列，不是标量"},
		// ── ts_corr 的标量位是第 3 个（args[2]）──
		{"ts_corr(close, open, volume)", "ts_corr 第 3 位应是窗口 N，不是序列"},
		// ── ts_kalman 的标量位是第 2、3 个（args[1], args[2]）──
		{"ts_kalman(close, volume, 0.1)", "ts_kalman 第 2 位应是标量 r"},
		{"ts_kalman(close, 0.1, close)", "ts_kalman 第 3 位应是标量 q"},
		// ── 横截面算子产出序列，同样不是标量 ──
		{"ts_mean(close, cs_rank(close))", "cs_rank 产出序列，不是标量"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			err := validateExpr(tc.expr)
			if err == nil {
				t.Fatalf("K3a 未修复：%q 被判合法（%s）", tc.expr, tc.why)
			}
			msg := err.Error()
			if !strings.Contains(msg, "标量") {
				t.Errorf("错误信息未点明「标量参数位」：%v", err)
			}
		})
	}
}

// TestScalarParamGate_AcceptsConstantExpressions 是反证腿：合法常量写法
// 不得被误拒。判定标准是「不引用序列数据」，而非「必须是裸字面量」。
func TestScalarParamGate_AcceptsConstantExpressions(t *testing.T) {
	valid := []string{
		"ts_mean(close, 5)",                    // 裸字面量
		"ts_ewma(close, 0.3)",                  // 浮点字面量
		"ts_ewma(close, 1/20)",                 // 常量表达式（衰减率的常见写法）
		"ts_delay(close, 1)",                   // 期数字面量
		"ts_corr(close, open, 20)",             // 标量位在 args[2]
		"ts_kalman(close, 0.1, 0.5)",           // 两个标量位
		"ts_kalman(close, 1/10, 1/2)",          // 两个标量位都是常量表达式
		"ts_rma(close, 14)",                    // L2 递推算子的标量位
		"ts_mean(close, 2+3)",                  // 常量折叠
		"cs_rank(close)",                       // 无标量位，不受影响
		"cs_neutralize(close, sector)",         // Series × Series，sector 不是标量位
		"ts_mean(close, 5) > ts_mean(open, 5)", // 嵌套合法用法

		// 真库存量表达式（2026-10-09 K3a 核对）：全库 factor_genes.formula /
		// strategies.params / strategy_genes.params / experiments.expression
		// 里唯一的真实表达式。钉在这里做永久回归护栏——新闸门不得误拒它。
		"cs_rank(ts_pct_change(close, 20)) > 0.8",
	}
	for _, s := range valid {
		if err := validateExpr(s); err != nil {
			t.Errorf("合法表达式被误拒（K3a 过度收紧）：%q → %v", s, err)
		}
	}
}

// TestScalarParamGate_ErrorNamesParameterSlot 报错要点名参数位，让 AI 能自纠
// （而不是抛一句「参数非法」让人去猜哪个位置错了）。
func TestScalarParamGate_ErrorNamesParameterSlot(t *testing.T) {
	err := validateExpr("ts_corr(close, open, volume)")
	if err == nil {
		t.Fatal("ts_corr(close, open, volume) 应被拒（第 3 位是序列）")
	}
	msg := err.Error()
	// 期望点名算子与参数位序号（ts_corr 的标量位是 2，即「第 2 个参数」
	// 按 0 起还是 1 起取决于实现措辞；这里只要求出现数字与算子名）。
	if !strings.Contains(msg, "ts_corr") {
		t.Errorf("错误未点名算子 ts_corr：%v", err)
	}
	if !strings.Contains(msg, "2") {
		t.Errorf("错误未点名参数位（期望含 2）：%v", err)
	}
}

// TestIsConstantExpr 是纯函数单测：判定标准 = 「不引用任何序列数据」。
func TestIsConstantExpr(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"5", true},                    // 字面量
		{"0.3", true},                  // 浮点字面量
		{"1/20", true},                 // 常量二元
		{"2+3", true},                  // 常量二元
		{"neg(5)", true},               // 一元逐元素算子作用在常量上
		{"abs(2-5)", true},             // 嵌套常量
		{"close", false},               // 数据字段 ⇒ 序列
		{"volume", false},              // 数据字段 ⇒ 序列
		{"ts_mean(close, 5)", false},   // 时序算子 ⇒ 序列
		{"ts_delay(volume, 1)", false}, // 时序算子 ⇒ 序列
		{"cs_rank(close)", false},      // 横截面算子 ⇒ 序列
		{"close / 5", false},           // 含字段 ⇒ 序列
		{"neg(close)", false},          // 一元算子作用在序列上 ⇒ 序列
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			expr, err := NewParser().Parse(tc.expr)
			if err != nil {
				t.Fatalf("解析 %q 失败（测试夹具问题，非被测行为）: %v", tc.expr, err)
			}
			if got := isConstantExpr(expr.AST); got != tc.want {
				t.Errorf("isConstantExpr(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestScalarParamsDeclarationsMatchArity 是注册表自洽护栏：ScalarParams 声明
// 的索引必须落在该算子的参数个数内（写错会在闸门里 fail-loud，但这里提前
// 在注册表层钉住，让错误离写错的地方更近）。
func TestScalarParamsDeclarationsMatchArity(t *testing.T) {
	for name, def := range operatorRegistry {
		for _, i := range def.ScalarParams {
			if i < 0 || i >= def.Arity {
				t.Errorf("算子 %q 的 ScalarParams 声明越界：索引 %d 超出 arity=%d", name, i, def.Arity)
			}
		}
	}
	// 时序算子的标量位声明必须非空（它们的签名都含 Scalar）—— 漏声明就等于
	// 把 K3a 的洞重新打开。
	mustHaveScalar := []string{
		"ts_mean", "ts_std", "ts_sum", "ts_max", "ts_min",
		"ts_delay", "ts_delta", "ts_pct_change", "ts_rank",
		"ts_rma", "ts_ewma", "ts_corr", "ts_kalman",
	}
	for _, name := range mustHaveScalar {
		def, ok := operatorRegistry[name]
		if !ok {
			t.Errorf("算子 %q 未在注册表（测试夹具过期）", name)
			continue
		}
		if len(def.ScalarParams) == 0 {
			t.Errorf("算子 %q 的签名含 Scalar 位，但 ScalarParams 为空 —— K3a 的洞被重新打开", name)
		}
	}
}
