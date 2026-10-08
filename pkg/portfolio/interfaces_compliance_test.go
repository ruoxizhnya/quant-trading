// K0 切片 2：pkg/portfolio 接口合规测试。
//
// 护栏目标：
//  1. Portfolio 接口方法集合（名字+数量）被反射精确断言；
//  2. Fill / Snapshot 两个契约结构体的**字段集合**被冻结（字段名+数量），
//     增字段 = 改契约（它们直接对应 quant.fills / quant.portfolio_snapshot
//     的列）；
//  3. 断言 Fill 不含费用字段（裁决：费用是 ApplyFill 的产出，不是输入）。
package portfolio_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
)

// TestPortfolioInterfaceMethods 断言 Portfolio 的方法集合精确等于
// {ApplyFill, Value, Positions, Snapshot}。
func TestPortfolioInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*portfolio.Portfolio)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Portfolio 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"ApplyFill": true, "Value": true, "Positions": true, "Snapshot": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Portfolio.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("Portfolio 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Portfolio 缺失冻结方法 %q", name)
		}
	}
}

// TestFillFields 冻结 Fill 的字段集合精确等于
// {OrderID, Symbol, Side, Qty, Price, Ts}。
func TestFillFields(t *testing.T) {
	want := []string{"OrderID", "Symbol", "Side", "Qty", "Price", "Ts"}
	assertStructFields(t, reflect.TypeOf(portfolio.Fill{}), "Fill", want)

	// 逐字段类型钉住（落库列的类型来自这里）。
	typ := reflect.TypeOf(portfolio.Fill{})
	assertFieldType(t, typ, "OrderID", reflect.TypeOf(""))
	assertFieldType(t, typ, "Symbol", reflect.TypeOf(""))
	assertFieldType(t, typ, "Side", reflect.TypeOf(domain.Direction("")))
	assertFieldType(t, typ, "Qty", reflect.TypeOf(float64(0)))
	assertFieldType(t, typ, "Price", reflect.TypeOf(float64(0)))
	assertFieldType(t, typ, "Ts", reflect.TypeOf(time.Time{}))
}

// TestFillCarriesNoFees 冻结「Fill 不含费用」的裁决：费用是 portfolio
// 按 pkg/fees 算出来的产出，若哪天把 Commission 之类加进 Fill，说明
// 因果被倒置（exec-engine 替 portfolio 定价），本测试拦下。
func TestFillCarriesNoFees(t *testing.T) {
	typ := reflect.TypeOf(portfolio.Fill{})
	for _, banned := range []string{"Commission", "TransferFee", "StampTax", "Fee"} {
		if _, ok := typ.FieldByName(banned); ok {
			t.Errorf("Fill 出现费用字段 %q——费用应由 ApplyFill 内部按 pkg/fees 计算，不属成交事件契约", banned)
		}
	}
}

// TestSnapshotFields 冻结 Snapshot 的字段集合精确等于
// {RunID, Ts, NAV, Cash, Positions}（quant.portfolio_snapshot 的行形状）。
func TestSnapshotFields(t *testing.T) {
	want := []string{"RunID", "Ts", "NAV", "Cash", "Positions"}
	assertStructFields(t, reflect.TypeOf(portfolio.Snapshot{}), "Snapshot", want)

	typ := reflect.TypeOf(portfolio.Snapshot{})
	assertFieldType(t, typ, "RunID", reflect.TypeOf(""))
	assertFieldType(t, typ, "NAV", reflect.TypeOf(float64(0)))
	assertFieldType(t, typ, "Cash", reflect.TypeOf(float64(0)))
	assertFieldType(t, typ, "Positions", reflect.TypeOf([]domain.Position{}))
}

// ─── 断言小工具 ────────────────────────────────────────────────────

// assertStructFields 断言结构体的导出字段集合精确等于 want（顺序无关）。
func assertStructFields(t *testing.T, typ reflect.Type, typeName string, want []string) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, n := range want {
		wantSet[n] = true
	}
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		got = append(got, name)
		if !wantSet[name] {
			t.Errorf("%s 出现契约外字段 %q（冻结字段集: %v）", typeName, name, want)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s 字段数 = %d, want %d（字段集: %v）", typeName, len(got), len(want), got)
	}
	for _, name := range want {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("%s 缺失冻结字段 %q", typeName, name)
		}
	}
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
