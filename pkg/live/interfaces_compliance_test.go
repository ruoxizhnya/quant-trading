// K0 切片 2：pkg/live（exec-engine）接口合规测试。
//
// 护栏目标：
//  1. ExecEngine / Reconciler / OrderLifecycle 三个新增接口的方法集合
//     （名字+数量）被反射精确断言；
//  2. 既有 Broker / OrderStore 被「冻结引用」——它们定义在别的文件里
//     （engine.go / order_store.go），本测试断言其方法集不漂移；
//  3. ReconReport 的字段集合被冻结（对应 quant.recon_report 的列）；
//  4. OrderIntent 必须与 pkg/risk 的同名类型是**同一个类型**（别名，
//     不是复制体）——防止两处定义漂移。
package live_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
)

// TestExecEngineInterfaceMethods 断言 ExecEngine 的方法集合精确等于
// {Submit, Cancel, Reconcile, Broker}。
func TestExecEngineInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*live.ExecEngine)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("ExecEngine 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Submit": true, "Cancel": true, "Reconcile": true, "Broker": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("ExecEngine.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("ExecEngine 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("ExecEngine 缺失冻结方法 %q", name)
		}
	}
}

// TestExecEngineMethodSignatures 钉住 ExecEngine 四方法的签名。
func TestExecEngineMethodSignatures(t *testing.T) {
	iface := reflect.TypeOf((*live.ExecEngine)(nil)).Elem()

	submit, _ := iface.MethodByName("Submit")
	wantSubmit := reflect.TypeOf(func(context.Context, risk.OrderIntent) (string, error) { return "", nil })
	if submit.Type != wantSubmit {
		t.Errorf("ExecEngine.Submit 签名 = %v, want %v", submit.Type, wantSubmit)
	}

	cancel, _ := iface.MethodByName("Cancel")
	wantCancel := reflect.TypeOf(func(context.Context, string) error { return nil })
	if cancel.Type != wantCancel {
		t.Errorf("ExecEngine.Cancel 签名 = %v, want %v", cancel.Type, wantCancel)
	}

	reconcile, _ := iface.MethodByName("Reconcile")
	wantReconcile := reflect.TypeOf(func(context.Context, time.Time) (*live.ReconReport, error) {
		return nil, nil
	})
	if reconcile.Type != wantReconcile {
		t.Errorf("ExecEngine.Reconcile 签名 = %v, want %v", reconcile.Type, wantReconcile)
	}

	broker, _ := iface.MethodByName("Broker")
	wantBroker := reflect.TypeOf(func() live.Broker { return nil })
	if broker.Type != wantBroker {
		t.Errorf("ExecEngine.Broker 签名 = %v, want %v", broker.Type, wantBroker)
	}
}

// TestOrderIntentIsRiskOrderIntent 冻结「exec-engine 用的 OrderIntent
// 就是 risk-engine 定义的那个类型」——别名，不是复制体。若哪天有人在本
// 包另起一个 struct，风控与执行看到的意图会静默分叉（编译能过、语义已
// 漂移），本测试拦下。
func TestOrderIntentIsRiskOrderIntent(t *testing.T) {
	if reflect.TypeOf(live.OrderIntent{}) != reflect.TypeOf(risk.OrderIntent{}) {
		t.Fatalf("live.OrderIntent 与 risk.OrderIntent 不是同一类型——必须是别名引用，禁止复制定义")
	}
}

// TestReconcilerInterfaceMethods 断言 Reconciler 的方法集合精确等于
// {Reconcile}。
func TestReconcilerInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*live.Reconciler)(nil)).Elem()
	want := map[string]bool{"Reconcile": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Reconciler.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Reconciler 缺失冻结方法 %q", name)
		}
	}
}

// TestReconReportFields 冻结 ReconReport 的字段集合精确等于
// {RunID, AsOf, DiffCount, Detail, CreatedAt}（quant.recon_report 的行）。
func TestReconReportFields(t *testing.T) {
	typ := reflect.TypeOf(live.ReconReport{})
	want := map[string]bool{"RunID": true, "AsOf": true, "DiffCount": true, "Detail": true, "CreatedAt": true}
	if got := typ.NumField(); got != len(want) {
		t.Errorf("ReconReport 字段数 = %d, want %d", got, len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !want[typ.Field(i).Name] {
			t.Errorf("ReconReport 出现契约外字段 %q（冻结字段集: %v）", typ.Field(i).Name, want)
		}
	}
	for name := range want {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("ReconReport 缺失冻结字段 %q", name)
		}
	}
	assertFieldType(t, typ, "AsOf", reflect.TypeOf(time.Time{}))
	assertFieldType(t, typ, "DiffCount", reflect.TypeOf(int(0)))
	assertFieldType(t, typ, "Detail", reflect.TypeOf(json.RawMessage{}))
	assertFieldType(t, typ, "CreatedAt", reflect.TypeOf(time.Time{}))
}

// TestOrderLifecycleMethods 冻结 OrderLifecycle 的 7 个方法——它们逐字
// 来自既有 *OrderManager struct（order_manager.go）。既有 struct 满足
// 本接口由 interfaces.go 的编译期检查钉住；这里再断言方法集本身。
func TestOrderLifecycleMethods(t *testing.T) {
	iface := reflect.TypeOf((*live.OrderLifecycle)(nil)).Elem()
	want := map[string]bool{
		"SubmitOrder": true, "CancelOrder": true, "GetOrder": true,
		"GetOrders": true, "GetPendingOrders": true,
		"UpdateOrderStatus": true, "GetTrades": true,
	}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("OrderLifecycle.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("OrderLifecycle 出现契约外方法 %q", name)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("OrderLifecycle 缺失冻结方法 %q", name)
		}
	}
	assertMethodType(t, iface, "SubmitOrder",
		reflect.TypeOf(func(domain.Order) (string, error) { return "", nil }))
	assertMethodType(t, iface, "GetOrder",
		reflect.TypeOf(func(string) (domain.Order, bool) { return domain.Order{}, false }))
	assertMethodType(t, iface, "GetTrades", reflect.TypeOf(func() []domain.Trade { return nil }))
}

// TestBrokerFrozenMethods 冻结既有 Broker（pkg/live/engine.go）的
// 7 个方法——它是「broker 契约入口」，被 ExecEngine.Broker() 引用。
func TestBrokerFrozenMethods(t *testing.T) {
	iface := reflect.TypeOf((*live.Broker)(nil)).Elem()
	want := map[string]bool{
		"Connect": true, "Disconnect": true, "SubmitOrder": true,
		"CancelOrder": true, "GetOrderStatus": true,
		"GetPositions": true, "GetAccountBalance": true,
	}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Broker.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("Broker 出现契约外方法 %q", name)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Broker 缺失冻结方法 %q", name)
		}
	}
}

// TestOrderStoreFrozenMethods 冻结既有 OrderStore
// （pkg/live/order_store.go）的 5 个方法。
func TestOrderStoreFrozenMethods(t *testing.T) {
	iface := reflect.TypeOf((*live.OrderStore)(nil)).Elem()
	want := map[string]bool{"Save": true, "Get": true, "List": true, "Update": true, "Delete": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("OrderStore.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("OrderStore 出现契约外方法 %q", name)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("OrderStore 缺失冻结方法 %q", name)
		}
	}
}

// ─── 断言小工具 ────────────────────────────────────────────────────

// assertFieldType 断言字段类型精确等于 want。
func assertFieldType(t *testing.T, typ reflect.Type, field string, want reflect.Type) {
	t.Helper()
	f, ok := typ.FieldByName(field)
	if !ok {
		t.Errorf("%s 缺字段 %q", typ.Name(), field)
		return
	}
	if f.Type != want {
		t.Errorf("%s.%s 类型 = %v, want %v", typ.Name(), field, f.Type, want)
	}
}

// assertMethodType 断言接口方法签名（不含 receiver）精确等于 want。
func assertMethodType(t *testing.T, iface reflect.Type, method string, want reflect.Type) {
	t.Helper()
	m, ok := iface.MethodByName(method)
	if !ok {
		t.Errorf("%s 缺方法 %q", iface.Name(), method)
		return
	}
	if m.Type != want {
		t.Errorf("%s.%s 签名 = %v, want %v", iface.Name(), method, m.Type, want)
	}
}

// ifaceMethodNames 返回接口方法集的全部方法名（诊断输出用）。
func ifaceMethodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	return names
}
