// K0 切片 1：pkg/eventstore 接口合规测试。
//
// 护栏目标：EventStore 接口方法集合（名字+数量）被反射精确断言——
// 删方法在编译期（interfaces.go 方法表达式守卫）与运行期（本文件）
// 双重拦截，防「删方法后测试仍绿」。
package eventstore_test

import (
	"reflect"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
)

// TestEventStoreInterfaceMethods 断言 EventStore 接口的方法集合精确
// 等于 {Append, Replay, Verify}——数量与名字都不得增减。改方法集 =
// 改契约，须走变更评审。
func TestEventStoreInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*eventstore.EventStore)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("EventStore 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Append": true, "Replay": true, "Verify": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("EventStore.NumMethod() = %d, want %d（方法集: %v）", got, len(want), methodNames(iface))
	}
	for _, name := range methodNames(iface) {
		if !want[name] {
			t.Errorf("EventStore 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("EventStore 缺失冻结方法 %q", name)
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
