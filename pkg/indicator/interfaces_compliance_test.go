// pkg/indicator（indicators）接口合规测试。
//
// K0 切片 2 初版；K3 切片 1 随契约变更同步（Update 标量化 + Save/Load）。
//
// 护栏目标：
//  1. Indicator 的方法集合（名字+数量）被反射精确断言为 **7 个**——
//     Name / Update / Value / Warmup / Reset / SaveState / LoadState；
//  2. 方法签名精确（Update 收 float64；SaveState/LoadState 的状态字节）；
//  3. OperatorSpec 的字段集合精确等于 ADR-028 §4 的 **7 项**（signature /
//     lookback / causal / state / warmup / init / nan_policy）——少一项
//     就不能注册，多一项就是契约外字段；
//  4. 字段名与 ADR-028 §4 的英文项名逐字对齐。
package indicator_test

import (
	"reflect"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// TestIndicatorInterfaceMethods 断言 Indicator 的方法集合精确等于
// {Name, Update, Value, Warmup, Reset, SaveState, LoadState}（7 个）。
func TestIndicatorInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*indicator.Indicator)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Indicator 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{
		"Name":      true,
		"Update":    true,
		"Value":     true,
		"Warmup":    true,
		"Reset":     true,
		"SaveState": true,
		"LoadState": true,
	}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Indicator.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("Indicator 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Indicator 缺失冻结方法 %q", name)
		}
	}
}

// TestIndicatorMethodSignatures 钉住方法签名：
//   - Update 收一个标量 float64（K3 切片 1 标量化，ADR-028 §7 的 Step(x)）；
//   - Value 返回 (float64, error)——warmup 未完成必须返回 error，不返回半成品；
//   - Warmup 返回 int（可静态推导）；
//   - SaveState 返回 ([]byte, error)、LoadState 收 []byte 返回 error
//     （状态序列化，三路一致性的第三路，ADR-028 §7）。
func TestIndicatorMethodSignatures(t *testing.T) {
	iface := reflect.TypeOf((*indicator.Indicator)(nil)).Elem()

	update, ok := iface.MethodByName("Update")
	if !ok {
		t.Fatal("Indicator 缺 Update")
	}
	wantUpdate := reflect.TypeOf(func(float64) error { return nil })
	if update.Type != wantUpdate {
		t.Errorf("Indicator.Update 签名 = %v, want %v", update.Type, wantUpdate)
	}

	value, ok := iface.MethodByName("Value")
	if !ok {
		t.Fatal("Indicator 缺 Value")
	}
	wantValue := reflect.TypeOf(func() (float64, error) { return 0, nil })
	if value.Type != wantValue {
		t.Errorf("Indicator.Value 签名 = %v, want %v", value.Type, wantValue)
	}

	warmup, ok := iface.MethodByName("Warmup")
	if !ok {
		t.Fatal("Indicator 缺 Warmup")
	}
	wantWarmup := reflect.TypeOf(func() int { return 0 })
	if warmup.Type != wantWarmup {
		t.Errorf("Indicator.Warmup 签名 = %v, want %v", warmup.Type, wantWarmup)
	}

	save, ok := iface.MethodByName("SaveState")
	if !ok {
		t.Fatal("Indicator 缺 SaveState")
	}
	wantSave := reflect.TypeOf(func() ([]byte, error) { return nil, nil })
	if save.Type != wantSave {
		t.Errorf("Indicator.SaveState 签名 = %v, want %v", save.Type, wantSave)
	}

	load, ok := iface.MethodByName("LoadState")
	if !ok {
		t.Fatal("Indicator 缺 LoadState")
	}
	wantLoad := reflect.TypeOf(func([]byte) error { return nil })
	if load.Type != wantLoad {
		t.Errorf("Indicator.LoadState 签名 = %v, want %v", load.Type, wantLoad)
	}
}

// TestOperatorSpecHasADR028SevenFields 冻结算子声明契约的 7 项
// （ADR-028 §4：signature / lookback / causal / state / warmup /
// init / nan_policy），字段名与项名逐字对齐，数量精确为 7。
func TestOperatorSpecHasADR028SevenFields(t *testing.T) {
	typ := reflect.TypeOf(indicator.OperatorSpec{})
	want := map[string]bool{
		"Name":      true, // 算子名（ADR-028 的算子标识，如 "ts_ewma"）
		"Signature": true, // signature：类型签名
		"Lookback":  true, // lookback：最大回看窗口
		"Causal":    true, // causal：是否只看过去（false 一律拒绝注册）
		"State":     true, // state：是否有状态（true=必须提供 Batch+Step 双实现）
		"Warmup":    true, // warmup：预热期（可为参数的函数）
		"Init":      true, // init：初始化规则
		"NaNPolicy": true, // nan_policy：NaN / unknown 传播规则
	}
	if got := typ.NumField(); got != len(want) {
		t.Errorf("OperatorSpec 字段数 = %d, want %d（ADR-028 §4 的 7 项 + Name）", got, len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !want[typ.Field(i).Name] {
			t.Errorf("OperatorSpec 出现契约外字段 %q（ADR-028 §4 冻结字段集: %v）", typ.Field(i).Name, want)
		}
	}
	for name := range want {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("OperatorSpec 缺失 ADR-028 §4 冻结字段 %q", name)
		}
	}
	assertFieldType(t, typ, "Causal", reflect.TypeOf(true))
	assertFieldType(t, typ, "State", reflect.TypeOf(true))
	assertFieldType(t, typ, "Lookback", reflect.TypeOf(int(0)))
	assertFieldType(t, typ, "Warmup", reflect.TypeOf(int(0)))
	assertFieldType(t, typ, "Init", reflect.TypeOf(float64(0)))
}

// TestThreeOperatorsSatisfyIndicator 是编译期守卫的反射版：三个算子的
// 具体类型都必须完整实现 Indicator 的 7 个方法（防止将来某算子漏改签名）。
func TestThreeOperatorsSatisfyIndicator(t *testing.T) {
	iface := reflect.TypeOf((*indicator.Indicator)(nil)).Elem()
	for _, tc := range []struct {
		name string
		v    any
	}{
		{"RMA", &indicator.RMA{}},
		{"EWMA", &indicator.EWMA{}},
		{"Kalman", &indicator.Kalman{}},
	} {
		typ := reflect.TypeOf(tc.v)
		if !typ.Implements(iface) {
			t.Errorf("%s 未实现 Indicator 接口", tc.name)
			continue
		}
		if got := typ.NumMethod(); got != iface.NumMethod() {
			t.Errorf("%s 方法数 = %d, want %d", tc.name, got, iface.NumMethod())
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
