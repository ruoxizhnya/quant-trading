// K0 切片 2：pkg/execalgo（exec-algo）接口合规测试。
//
// 护栏目标：
//  1. ExecAlgorithm 的方法集合（名字+数量）被反射精确断言；
//  2. ParentOrder / ChildOrder 的字段集合被冻结，且 ChildOrder 必须带
//     ParentID 回链父订单（拆单链路的可追溯性靠它）；
//  3. Fill 必须与 pkg/portfolio 的同名类型是**同一个类型**（别名，
//     不是复制体）——父单成交后 exec-algo 与 portfolio 看到的是同一个
//     fill 事件，定义两处必然漂移。
package execalgo_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
)

// TestExecAlgorithmInterfaceMethods 断言 ExecAlgorithm 的方法集合精确
// 等于 {Name, OnBar, OnFill, Schedule}——与策略同构 trait
// （对照 nautilus crates/trading/src/algorithm/）。
func TestExecAlgorithmInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*execalgo.ExecAlgorithm)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("ExecAlgorithm 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Name": true, "OnBar": true, "OnFill": true, "Schedule": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("ExecAlgorithm.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("ExecAlgorithm 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("ExecAlgorithm 缺失冻结方法 %q", name)
		}
	}
}

// TestExecAlgorithmMethodSignatures 钉住四方法的签名。
func TestExecAlgorithmMethodSignatures(t *testing.T) {
	iface := reflect.TypeOf((*execalgo.ExecAlgorithm)(nil)).Elem()

	name, _ := iface.MethodByName("Name")
	wantName := reflect.TypeOf(func() string { return "" })
	if name.Type != wantName {
		t.Errorf("ExecAlgorithm.Name 签名 = %v, want %v", name.Type, wantName)
	}

	onBar, _ := iface.MethodByName("OnBar")
	wantOnBar := reflect.TypeOf(func(context.Context, domain.OHLCV) error { return nil })
	if onBar.Type != wantOnBar {
		t.Errorf("ExecAlgorithm.OnBar 签名 = %v, want %v", onBar.Type, wantOnBar)
	}

	onFill, _ := iface.MethodByName("OnFill")
	wantOnFill := reflect.TypeOf(func(context.Context, portfolio.Fill) error { return nil })
	if onFill.Type != wantOnFill {
		t.Errorf("ExecAlgorithm.OnFill 签名 = %v, want %v", onFill.Type, wantOnFill)
	}

	schedule, _ := iface.MethodByName("Schedule")
	wantSchedule := reflect.TypeOf(func(context.Context, execalgo.ParentOrder) ([]execalgo.ChildOrder, error) {
		return nil, nil
	})
	if schedule.Type != wantSchedule {
		t.Errorf("ExecAlgorithm.Schedule 签名 = %v, want %v", schedule.Type, wantSchedule)
	}
}

// TestFillIsPortfolioFill 冻结「exec-algo 用的 Fill 就是 portfolio 定义的
// 那个类型」——别名，不是复制体。
func TestFillIsPortfolioFill(t *testing.T) {
	if reflect.TypeOf(execalgo.Fill{}) != reflect.TypeOf(portfolio.Fill{}) {
		t.Fatalf("execalgo.Fill 与 portfolio.Fill 不是同一类型——必须是别名引用，禁止复制定义")
	}
}

// TestParentOrderFields 冻结 ParentOrder 的字段集合。
func TestParentOrderFields(t *testing.T) {
	typ := reflect.TypeOf(execalgo.ParentOrder{})
	want := map[string]bool{
		"OrderID": true, "RunID": true, "Symbol": true, "Side": true,
		"Qty": true, "LimitPrice": true, "Style": true,
		"StartAt": true, "EndAt": true,
	}
	if got := typ.NumField(); got != len(want) {
		t.Errorf("ParentOrder 字段数 = %d, want %d", got, len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !want[typ.Field(i).Name] {
			t.Errorf("ParentOrder 出现契约外字段 %q（冻结字段集: %v）", typ.Field(i).Name, want)
		}
	}
	for name := range want {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("ParentOrder 缺失冻结字段 %q", name)
		}
	}
	assertFieldType(t, typ, "Side", reflect.TypeOf(domain.Direction("")))
	assertFieldType(t, typ, "StartAt", reflect.TypeOf(time.Time{}))
	assertFieldType(t, typ, "EndAt", reflect.TypeOf(time.Time{}))
}

// TestChildOrderFields 冻结 ChildOrder 的字段集合，并断言它带 ParentID
// 回链父订单——没有回链就无法把子订单归并回父单进度（UC4 的进度更新
// 会失去锚点）。
func TestChildOrderFields(t *testing.T) {
	typ := reflect.TypeOf(execalgo.ChildOrder{})
	want := map[string]bool{
		"OrderID": true, "ParentID": true, "RunID": true, "Symbol": true,
		"Side": true, "Qty": true, "LimitPrice": true, "SubmitAt": true,
	}
	if got := typ.NumField(); got != len(want) {
		t.Errorf("ChildOrder 字段数 = %d, want %d", got, len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !want[typ.Field(i).Name] {
			t.Errorf("ChildOrder 出现契约外字段 %q（冻结字段集: %v）", typ.Field(i).Name, want)
		}
	}
	for name := range want {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("ChildOrder 缺失冻结字段 %q", name)
		}
	}
	assertFieldType(t, typ, "SubmitAt", reflect.TypeOf(time.Time{}))
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

// ifaceMethodNames 返回接口方法集的全部方法名（诊断输出用）。
func ifaceMethodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	return names
}
