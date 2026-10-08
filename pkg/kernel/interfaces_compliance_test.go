// K0 切片 1：pkg/kernel 接口与 BootOrder 顺序契约合规测试。
//
// 护栏目标：
//  1. Module/Kernel 接口方法集合（名字+数量）被反射精确断言——
//     删方法在编译期（interfaces.go 方法表达式守卫）与运行期
//     （本文件）双重拦截。
//  2. TestBootOrderContract 把蓝图 §4.2 生命周期顺序契约可测试化：
//     eventstore 首位、msgbus 末位、全序精确匹配、无重复。
package kernel_test

import (
	"reflect"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/kernel"
)

// ─── 1. 接口方法集合断言 ─────────────────────────────────────────

// TestModuleInterfaceMethods 断言 Module 接口的方法集合精确等于
// {Name, Init, Start, Stop}。
func TestModuleInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*kernel.Module)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Module 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Name": true, "Init": true, "Start": true, "Stop": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Module.NumMethod() = %d, want %d（方法集: %v）", got, len(want), methodNames(iface))
	}
	for _, name := range methodNames(iface) {
		if !want[name] {
			t.Errorf("Module 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Module 缺失冻结方法 %q", name)
		}
	}
}

// TestKernelInterfaceMethods 断言 Kernel 接口的方法集合精确等于
// {Boot, Shutdown, Module, Clock, Bus, Store}。
func TestKernelInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*kernel.Kernel)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Kernel 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{
		"Boot": true, "Shutdown": true, "Module": true,
		"Clock": true, "Bus": true, "Store": true,
	}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Kernel.NumMethod() = %d, want %d（方法集: %v）", got, len(want), methodNames(iface))
	}
	for _, name := range methodNames(iface) {
		if !want[name] {
			t.Errorf("Kernel 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Kernel 缺失冻结方法 %q", name)
		}
	}
}

// ─── 2. BootOrder 顺序契约（蓝图 §4.2 可测试化） ────────────────────

// TestBootOrderContract 冻结 BootOrder 的三个不变量与全序。
//
// K0 裁决备注：任务书原文称 BootOrder「含全部 11 模块名 / 共 11 个
// 元素」，但其给出的顺序枚举（eventstore → clock → data-engine →
// portfolio → risk-engine → exec-engine → strategy-runtime →
// indicators → exec-algo → msgbus）恰为蓝图 §5 模块矩阵 11 模块中
// 除 kernel 外的全部 10 个——kernel 是装配者本身（Boot 的发起者），
// 不能被自己装配，不进入被装配序列。故本测试断言 10 个元素；
// 「11」为任务书计数笔误，首/末元素断言（eventstore / msgbus）
// 与枚举顺序为准。
func TestBootOrderContract(t *testing.T) {
	// 蓝图 §4.2 生命周期顺序 + §5 模块矩阵的全集（除 kernel）。
	want := []string{
		"eventstore",
		"clock",
		"data-engine",
		"portfolio",
		"risk-engine",
		"exec-engine",
		"strategy-runtime",
		"indicators",
		"exec-algo",
		"msgbus",
	}

	if len(kernel.BootOrder) != len(want) {
		t.Errorf("BootOrder 长度 = %d, want %d（%v）",
			len(kernel.BootOrder), len(want), kernel.BootOrder)
	}

	// 不变量 1：eventstore 必须首位（先开记录，否则启动期消息无审计）。
	if got := kernel.BootOrder[0]; got != "eventstore" {
		t.Errorf("BootOrder[0] = %q, want %q（eventstore 必须最先 Boot——蓝图 §4.2）", got, "eventstore")
	}

	// 不变量 2：msgbus 必须末位（最后开分发，此前消息只记录不派发）。
	if got := kernel.BootOrder[len(kernel.BootOrder)-1]; got != "msgbus" {
		t.Errorf("BootOrder[len-1] = %q, want %q（msgbus 必须最后开——蓝图 §4.2）", got, "msgbus")
	}

	// 不变量 3：无重复模块名。
	seen := map[string]bool{}
	for _, m := range kernel.BootOrder {
		if seen[m] {
			t.Errorf("BootOrder 存在重复模块名 %q", m)
		}
		seen[m] = true
	}

	// 全序精确匹配：顺序本身就是契约，不只是首末两个不变量。
	for i, m := range want {
		if i >= len(kernel.BootOrder) {
			break
		}
		if kernel.BootOrder[i] != m {
			t.Errorf("BootOrder[%d] = %q, want %q（全序冻结，改顺序须走变更评审）",
				i, kernel.BootOrder[i], m)
		}
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
