// K0 切片 2：pkg/risk（risk-engine）接口合规测试。
//
// 护栏目标：
//  1. RiskEngine 的方法集合（名字+数量）被反射精确断言——其中 3 个是
//     冻结自 domain.RiskManager 的既有方法，1 个（CheckOrder）是新增的
//     订单前置风控挂点；
//  2. Verdict 的字段集合与 **fail-closed 语义**（零值 = 拒绝）被冻结；
//  3. OrderIntent 的字段集合被冻结。
package risk_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
)

// TestRiskEngineInterfaceMethods 断言 RiskEngine 的方法集合精确等于
// {CalculatePosition, DetectRegime, CheckStopLoss, CheckOrder}。
func TestRiskEngineInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*risk.RiskEngine)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("RiskEngine 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{
		"CalculatePosition": true,
		"DetectRegime":      true,
		"CheckStopLoss":     true,
		"CheckOrder":        true,
	}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("RiskEngine.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("RiskEngine 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("RiskEngine 缺失冻结方法 %q", name)
		}
	}
}

// TestRiskEngineMatchesDomainRiskManager 冻结「前三个方法签名与
// domain.RiskManager 逐字对齐」——本包既有 *RiskManager 已实现之，
// 若两者漂移，风控实现将不再满足冻结契约。
func TestRiskEngineMatchesDomainRiskManager(t *testing.T) {
	riskEngine := reflect.TypeOf((*risk.RiskEngine)(nil)).Elem()
	domainRM := reflect.TypeOf((*domain.RiskManager)(nil)).Elem()

	frozen := []string{"CalculatePosition", "DetectRegime", "CheckStopLoss"}
	for _, name := range frozen {
		got, ok := riskEngine.MethodByName(name)
		if !ok {
			t.Errorf("RiskEngine 缺冻结方法 %q", name)
			continue
		}
		want, ok := domainRM.MethodByName(name)
		if !ok {
			t.Errorf("domain.RiskManager 缺方法 %q（契约来源被改动）", name)
			continue
		}
		if got.Type != want.Type {
			t.Errorf("RiskEngine.%s 签名 = %v, want 与 domain.RiskManager 一致 %v", name, got.Type, want.Type)
		}
	}
	if domainRM.NumMethod() != len(frozen) {
		t.Errorf("domain.RiskManager.NumMethod() = %d, want %d——它多出/少了方法，RiskEngine 须同步评审",
			domainRM.NumMethod(), len(frozen))
	}
}

// TestCheckOrderSignature 钉住新增挂点 CheckOrder 的签名
// （ctx + OrderIntent → Verdict, error）。
func TestCheckOrderSignature(t *testing.T) {
	iface := reflect.TypeOf((*risk.RiskEngine)(nil)).Elem()
	m, ok := iface.MethodByName("CheckOrder")
	if !ok {
		t.Fatal("RiskEngine 缺 CheckOrder")
	}
	want := reflect.TypeOf(func(context.Context, risk.OrderIntent) (risk.Verdict, error) {
		return risk.Verdict{}, nil
	})
	if m.Type != want {
		t.Errorf("RiskEngine.CheckOrder 签名 = %v, want %v", m.Type, want)
	}
}

// TestVerdictFields 冻结 Verdict 的字段集合精确等于 {Allowed, Reason}。
func TestVerdictFields(t *testing.T) {
	typ := reflect.TypeOf(risk.Verdict{})
	want := map[string]bool{"Allowed": true, "Reason": true}
	if got := typ.NumField(); got != len(want) {
		t.Errorf("Verdict 字段数 = %d, want %d", got, len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !want[typ.Field(i).Name] {
			t.Errorf("Verdict 出现契约外字段 %q（冻结字段集: %v）", typ.Field(i).Name, want)
		}
	}
	if f, ok := typ.FieldByName("Allowed"); !ok || f.Type != reflect.TypeOf(true) {
		t.Errorf("Verdict.Allowed 缺失或不是 bool（fail-closed 语义依赖它）")
	}
}

// TestVerdictFailClosed 冻结 fail-closed 语义：零值 Verdict{} 的 Allowed
// 必须是 false——**未显式放行一律视为拒绝**。风控代码任何「忘了填
// Allowed」的分支都必须落在安全侧。这是本模块最要命的一条契约，
// 也是破坏验证腿 c 的目标（把 Verdict 默认值改成放行会红）。
func TestVerdictFailClosed(t *testing.T) {
	if (risk.Verdict{}).Allowed {
		t.Error("Verdict{} 的 Allowed = true, want false（fail-closed：未显式放行即拒绝）")
	}
	if (risk.Verdict{Reason: "some reason"}).Allowed {
		t.Error("只填 Reason 未填 Allowed 的 Verdict 放行——违反 fail-closed")
	}
}

// TestOrderIntentFields 冻结 OrderIntent 的字段集合（它是 risk-engine
// 与 exec-engine 共用的输入契约，pkg/live 以别名引用同一类型）。
func TestOrderIntentFields(t *testing.T) {
	typ := reflect.TypeOf(risk.OrderIntent{})
	want := map[string]bool{
		"OrderID": true, "RunID": true, "Symbol": true, "Side": true,
		"OrderType": true, "Qty": true, "Price": true, "Ts": true,
	}
	if got := typ.NumField(); got != len(want) {
		t.Errorf("OrderIntent 字段数 = %d, want %d", got, len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !want[typ.Field(i).Name] {
			t.Errorf("OrderIntent 出现契约外字段 %q（冻结字段集: %v）", typ.Field(i).Name, want)
		}
	}
	for name := range want {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("OrderIntent 缺失冻结字段 %q", name)
		}
	}
	assertFieldType(t, typ, "Side", reflect.TypeOf(domain.Direction("")))
	assertFieldType(t, typ, "OrderType", reflect.TypeOf(domain.OrderType("")))
	assertFieldType(t, typ, "Ts", reflect.TypeOf(time.Time{}))
}

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
