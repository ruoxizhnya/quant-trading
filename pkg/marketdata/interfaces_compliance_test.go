// K0 切片 2：pkg/marketdata（data-engine）接口合规测试。
//
// 护栏目标：DataEngine / Provider 的方法集合（名字+数量）被反射精确
// 断言——删方法在编译期（interfaces.go 方法表达式守卫）与运行期
// （本文件）双重拦截；DataEngine 三个方法的签名被逐字钉住。
package marketdata_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// TestDataEngineInterfaceMethods 断言 DataEngine 的方法集合精确等于
// {Snapshot, Subscribe, Provider}。
func TestDataEngineInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*marketdata.DataEngine)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("DataEngine 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"Snapshot": true, "Subscribe": true, "Provider": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("DataEngine.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("DataEngine 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("DataEngine 缺失冻结方法 %q", name)
		}
	}
}

// TestDataEngineMethodSignatures 逐字钉住三个方法的签名——改签名即改
// 契约，必须走变更评审（Snapshot 的返回值形状是「物化边界」的核心）。
func TestDataEngineMethodSignatures(t *testing.T) {
	iface := reflect.TypeOf((*marketdata.DataEngine)(nil)).Elem()

	snapshot, ok := iface.MethodByName("Snapshot")
	if !ok {
		t.Fatal("DataEngine 缺 Snapshot")
	}
	wantSnapshot := reflect.TypeOf(func(context.Context, []string, time.Time, time.Time) (map[string][]domain.OHLCV, error) {
		return nil, nil
	})
	if got := snapshot.Type; got != wantSnapshot {
		t.Errorf("DataEngine.Snapshot 签名 = %v, want %v", got, wantSnapshot)
	}

	subscribe, ok := iface.MethodByName("Subscribe")
	if !ok {
		t.Fatal("DataEngine 缺 Subscribe")
	}
	wantSubscribe := reflect.TypeOf(func(context.Context, []string) error { return nil })
	if got := subscribe.Type; got != wantSubscribe {
		t.Errorf("DataEngine.Subscribe 签名 = %v, want %v", got, wantSubscribe)
	}

	provider, ok := iface.MethodByName("Provider")
	if !ok {
		t.Fatal("DataEngine 缺 Provider")
	}
	wantProvider := reflect.TypeOf(func() marketdata.Provider { return nil })
	if got := provider.Type; got != wantProvider {
		t.Errorf("DataEngine.Provider 签名 = %v, want %v", got, wantProvider)
	}
}

// TestProviderFrozenMethods 冻结既有 Provider（pkg/marketdata/provider.go）
// 的 11 个方法——它是「数据源契约入口」，被 DataEngine.Provider() 引用，
// 增删方法 = 改冻结契约。
func TestProviderFrozenMethods(t *testing.T) {
	iface := reflect.TypeOf((*marketdata.Provider)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Provider 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{
		"Name": true, "CheckConnectivity": true, "GetOHLCV": true,
		"GetFundamental": true, "GetStocks": true, "GetLatestPrice": true,
		"GetIndexConstituents": true, "GetTradingDays": true, "GetStock": true,
		"BulkLoadOHLCV": true, "CheckCalendarExists": true,
	}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("Provider.NumMethod() = %d, want %d（方法集: %v）", got, len(want), ifaceMethodNames(iface))
	}
	for _, name := range ifaceMethodNames(iface) {
		if !want[name] {
			t.Errorf("Provider 出现契约外方法 %q", name)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("Provider 缺失冻结方法 %q", name)
		}
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
