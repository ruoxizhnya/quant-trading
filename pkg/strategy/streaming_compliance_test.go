// K0 切片 2：pkg/strategy（strategy-runtime）流式接口合规测试。
//
// 落位裁决：本包**已有** interfaces_compliance_test.go（P1-24，批式
// Strategy 四子接口的合规测试）。按「只追加、不改现有测试逻辑」的约束，
// BarHandler 的合规测试落在本文件（streaming_compliance_test.go），
// 不并入也不改动既有文件。
//
// 护栏目标：
//  1. BarHandler 的方法集合（名字+数量）被反射精确断言，并与
//     docs/SPEC.md「Streaming Strategy Interface — BarHandler」节逐字对齐；
//  2. OnBar 的形参冻结为 domain.OHLCV（裁决：domain.Bar 不存在）；
//  3. 批式 SignalGenerator 与流式 BarHandler **并存**（谁都没被顶替）——
//     双模式是本模块的核心设计，若哪天只剩一个，本测试红。
package strategy_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// TestBarHandlerInterfaceMethods 断言 BarHandler 的方法集合精确等于
// {OnBar, Warmup, SaveState, LoadState}。
func TestBarHandlerInterfaceMethods(t *testing.T) {
	iface := reflect.TypeOf((*strategy.BarHandler)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("BarHandler 的类型是 %v, want interface", iface.Kind())
	}
	want := map[string]bool{"OnBar": true, "Warmup": true, "SaveState": true, "LoadState": true}
	if got := iface.NumMethod(); got != len(want) {
		t.Errorf("BarHandler.NumMethod() = %d, want %d（方法集: %v）", got, len(want), streamingMethodNames(iface))
	}
	for _, name := range streamingMethodNames(iface) {
		if !want[name] {
			t.Errorf("BarHandler 出现契约外方法 %q（冻结方法集: %v）", name, want)
		}
	}
	for name := range want {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("BarHandler 缺失冻结方法 %q", name)
		}
	}
}

// TestBarHandlerMethodSignatures 逐字钉住四方法的签名（与 SPEC 对齐）。
func TestBarHandlerMethodSignatures(t *testing.T) {
	iface := reflect.TypeOf((*strategy.BarHandler)(nil)).Elem()

	// 裁决：SPEC 写 domain.Bar，但该类型不存在（pkg/domain 与
	// pkg/domain/market 均无定义），故用 domain.OHLCV——本项目唯一的
	// bar 载体。改这里 = 改冻结契约。
	onBar, ok := iface.MethodByName("OnBar")
	if !ok {
		t.Fatal("BarHandler 缺 OnBar")
	}
	wantOnBar := reflect.TypeOf(func(context.Context, domain.OHLCV) error { return nil })
	if onBar.Type != wantOnBar {
		t.Errorf("BarHandler.OnBar 签名 = %v, want %v（形参必须是 domain.OHLCV）", onBar.Type, wantOnBar)
	}

	warmup, ok := iface.MethodByName("Warmup")
	if !ok {
		t.Fatal("BarHandler 缺 Warmup")
	}
	wantWarmup := reflect.TypeOf(func() int { return 0 })
	if warmup.Type != wantWarmup {
		t.Errorf("BarHandler.Warmup 签名 = %v, want %v", warmup.Type, wantWarmup)
	}

	saveState, _ := iface.MethodByName("SaveState")
	wantSave := reflect.TypeOf(func() ([]byte, error) { return nil, nil })
	if saveState.Type != wantSave {
		t.Errorf("BarHandler.SaveState 签名 = %v, want %v", saveState.Type, wantSave)
	}

	loadState, _ := iface.MethodByName("LoadState")
	wantLoad := reflect.TypeOf(func([]byte) error { return nil })
	if loadState.Type != wantLoad {
		t.Errorf("BarHandler.LoadState 签名 = %v, want %v", loadState.Type, wantLoad)
	}
}

// TestDualModeInterfacesCoexist 冻结「双模式并存」——批式 SignalGenerator
// 与流式 BarHandler 必须同时存在，引擎按策略实现的接口自动选执行模式。
// 删掉任一个模式（例如把 BarHandler 并回 Strategy）都会让本测试红。
func TestDualModeInterfacesCoexist(t *testing.T) {
	batch := reflect.TypeOf((*strategy.SignalGenerator)(nil)).Elem()
	stream := reflect.TypeOf((*strategy.BarHandler)(nil)).Elem()

	if batch.NumMethod() != 2 {
		t.Errorf("批式 SignalGenerator.NumMethod() = %d, want 2（GenerateSignals / Weight）", batch.NumMethod())
	}
	for _, name := range []string{"GenerateSignals", "Weight"} {
		if _, ok := batch.MethodByName(name); !ok {
			t.Errorf("批式 SignalGenerator 缺失方法 %q", name)
		}
	}
	if stream.NumMethod() != 4 {
		t.Errorf("流式 BarHandler.NumMethod() = %d, want 4", stream.NumMethod())
	}
	// 两模式正交：流式接口不得含批式的 GenerateSignals/Weight。
	for _, name := range []string{"GenerateSignals", "Weight"} {
		if _, ok := stream.MethodByName(name); ok {
			t.Errorf("流式 BarHandler 混入批式方法 %q——双模式必须正交", name)
		}
	}
}

// streamingMethodNames 返回接口方法集的全部方法名（诊断输出用）。
// 注意：本包既有的 strategy_test 包内已有一个 methodNames，此处另起名
// 避免同包重名。
func streamingMethodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	return names
}
