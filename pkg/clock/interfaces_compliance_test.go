// K0 切片 1：pkg/clock 接口合规测试。
//
// 护栏目标：接口方法集合（名字+数量）被反射精确断言——从 Clock
// 接口删掉任一方法，编译期由 interfaces.go 的方法表达式守卫拦下
// （go build 失败），运行期由本文件拦下（方法集断言失败）。
// 双保险防「删方法后测试仍绿」。
package clock_test

import (
	"reflect"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
)

// TestClockInterfaceMethods 断言 Clock 接口的方法集合精确等于
// {Now, Advance, Mode}——数量与名字都不得增减。改方法集 = 改契约，
// 须走变更评审。
func TestClockInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*clock.Clock)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Clock 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Now": true, "Advance": true, "Mode": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Clock.NumMethod() = %d, want %d（方法集: %v）", got, len(want), methodNames(iface))
	}
	for _, name := range methodNames(iface) {
		if !want[name] {
			t.Errorf("Clock 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Clock 缺失冻结方法 %q", name)
		}
	}
}

// TestModeConstants 冻结 Mode 取值顺序：ModeBacktest=0、ModeLive=1。
// iota 顺序是契约（有代码按 0 值初始化零态），禁止重排。
func TestModeConstants(t *testing.T) {
	if clock.ModeBacktest != 0 {
		t.Errorf("ModeBacktest = %d, want 0（iota 顺序冻结）", clock.ModeBacktest)
	}
	if clock.ModeLive != 1 {
		t.Errorf("ModeLive = %d, want 1（iota 顺序冻结）", clock.ModeLive)
	}
	if clock.ModeBacktest == clock.ModeLive {
		t.Error("ModeBacktest 与 ModeLive 取值相同")
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
