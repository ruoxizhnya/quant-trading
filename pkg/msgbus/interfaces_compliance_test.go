// K0 切片 1：pkg/msgbus 接口与 topic 注册表合规测试。
//
// 护栏目标：
//  1. Message/MsgBus 接口方法集合（名字+数量）被反射精确断言——
//     删方法在编译期（interfaces.go 方法表达式守卫）与运行期
//     （本文件）双重拦截。
//  2. TestTopicNamingConvention 强制命名规范 ^[a-z]+(\.[a-z_]+)+$
//     且无重复值。
//  3. topicRegistry 直接引用每个冻结常量——从 topics.go 删除任一
//     常量会导致本测试文件编译失败（K0 验收破坏验证腿 b 的护栏，
//     故意为之，勿改为反射/字符串间接引用）。
package msgbus_test

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// ─── 1. 接口方法集合断言 ─────────────────────────────────────────

// TestMessageInterfaceMethods 断言 Message 接口的方法集合精确等于
// {Topic, Ts, Payload}。
func TestMessageInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*msgbus.Message)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Message 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Topic": true, "Ts": true, "Payload": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Message.NumMethod() = %d, want %d（方法集: %v）", got, len(want), methodNames(iface))
	}
	for _, name := range methodNames(iface) {
		if !want[name] {
			t.Errorf("Message 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Message 缺失冻结方法 %q", name)
		}
	}
}

// TestMsgBusInterfaceMethods 断言 MsgBus 接口的方法集合精确等于
// {Publish, Subscribe}。
func TestMsgBusInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*msgbus.MsgBus)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("MsgBus 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Publish": true, "Subscribe": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("MsgBus.NumMethod() = %d, want %d（方法集: %v）", got, len(want), methodNames(iface))
	}
	for _, name := range methodNames(iface) {
		if !want[name] {
			t.Errorf("MsgBus 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("MsgBus 缺失冻结方法 %q", name)
		}
	}
}

// ─── 2. topic 注册表断言 ─────────────────────────────────────────

// topicRegistry 逐个直接引用全部冻结 topic 常量并冻结其值。
// 删除任一常量 → 本文件编译失败；改任一常量值 → TestTopicValues 红。
var topicRegistry = []struct {
	constant string // 常量的当前取值（编译期绑定）
	name     string // 常量的 Go 标识名（诊断用）
}{
	{msgbus.TopicDataBar, "TopicDataBar"},
	{msgbus.TopicExecOrderIntent, "TopicExecOrderIntent"},
	{msgbus.TopicExecFill, "TopicExecFill"},
	{msgbus.TopicPortfolioUpdated, "TopicPortfolioUpdated"},
	{msgbus.TopicRiskVerdict, "TopicRiskVerdict"},
	{msgbus.TopicRunStart, "TopicRunStart"},
	{msgbus.TopicRunDone, "TopicRunDone"},
	{msgbus.TopicKernelBoot, "TopicKernelBoot"},
	{msgbus.TopicKernelShutdown, "TopicKernelShutdown"},
	// ─── K0 切片 2 追加的 7 个 topic ───────────────────────────────
	{msgbus.TopicRiskOrderVerdict, "TopicRiskOrderVerdict"},
	{msgbus.TopicExecOrderRejected, "TopicExecOrderRejected"},
	{msgbus.TopicPortfolioFilled, "TopicPortfolioFilled"},
	{msgbus.TopicReconDiff, "TopicReconDiff"},
	{msgbus.TopicStrategyStateSaved, "TopicStrategyStateSaved"},
	{msgbus.TopicIndicatorWarmupDone, "TopicIndicatorWarmupDone"},
	{msgbus.TopicExecAlgoChildOrder, "TopicExecAlgoChildOrder"},
}

// frozenTopicValues 是 K0 切片 1 冻结的「常量名 → 值」映射。
var frozenTopicValues = map[string]string{
	"TopicDataBar":          "data.bar",
	"TopicExecOrderIntent":  "exec.order_intent",
	"TopicExecFill":         "exec.fill",
	"TopicPortfolioUpdated": "portfolio.updated",
	"TopicRiskVerdict":      "risk.verdict",
	"TopicRunStart":         "run.start",
	"TopicRunDone":          "run.done",
	"TopicKernelBoot":       "kernel.boot",
	"TopicKernelShutdown":   "kernel.shutdown",
	// ─── K0 切片 2 追加的 7 个 topic ───────────────────────────────
	"TopicRiskOrderVerdict":    "risk.order_verdict",
	"TopicExecOrderRejected":   "exec.order_rejected",
	"TopicPortfolioFilled":     "portfolio.filled",
	"TopicReconDiff":           "recon.diff",
	"TopicStrategyStateSaved":  "strategy.state_saved",
	"TopicIndicatorWarmupDone": "indicator.warmup_done",
	"TopicExecAlgoChildOrder":  "execalgo.child_order",
}

// TestTopicRegistryCount 冻结注册表规模：切片 1 的 9 个 + 切片 2 追加的
// 7 个 = 16。新增 topic 必须同步登记到 topicRegistry 与
// frozenTopicValues，否则本测试红（防「加了常量忘了登记」）。
func TestTopicRegistryCount(t *testing.T) {
	const want = 16
	if len(topicRegistry) != want {
		t.Errorf("topicRegistry 有 %d 项, want %d（切片 1 的 9 个 + 切片 2 的 7 个）",
			len(topicRegistry), want)
	}
}

// TestTopicNamingConvention 强制全部 topic 常量匹配
// ^[a-z]+(\.[a-z_]+)+$ 且值无重复。
func TestTopicNamingConvention(t *testing.T) {
	re := regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)
	seen := map[string]string{}
	for _, tc := range topicRegistry {
		if !re.MatchString(tc.constant) {
			t.Errorf("topic %s = %q 违反命名规范 ^[a-z]+(\\.[a-z_]+)+$", tc.name, tc.constant)
		}
		if prev, dup := seen[tc.constant]; dup {
			t.Errorf("topic 值重复：%s 与 %s 都取值 %q", prev, tc.name, tc.constant)
		}
		seen[tc.constant] = tc.name
	}
}

// TestTopicValues 冻结每个常量的字面值：改值 = 改契约，测试必须红。
func TestTopicValues(t *testing.T) {
	for _, tc := range topicRegistry {
		want, ok := frozenTopicValues[tc.name]
		if !ok {
			t.Errorf("常量 %s 未登记 frozenTopicValues（登记它以冻结其值）", tc.name)
			continue
		}
		if tc.constant != want {
			t.Errorf("topic %s = %q, want 冻结值 %q（改值须走变更评审）", tc.name, tc.constant, want)
		}
	}
	if len(topicRegistry) != len(frozenTopicValues) {
		t.Errorf("topicRegistry（%d 项）与 frozenTopicValues（%d 项）数量不一致",
			len(topicRegistry), len(frozenTopicValues))
	}
}

// methodNames 返回接口方法集的全部方法名（诊断输出用）。
func methodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	return names
}
