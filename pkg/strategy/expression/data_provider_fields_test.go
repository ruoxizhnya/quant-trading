// OBS-08 切片 1 —— 字段注册表 ↔ provider 能力 的跨包防漂移护栏。
//
// 为什么这个护栏在 pkg/strategy/expression/：
//
//	pkg/strategy/expression import pkg/expression（反过来不行，否则成环）。
//	所以「同时看到两侧」的断言只能放在这里。
//
// 它钉死一条不变式：
//
//	expr.AvailableDataFields()（注册表里来源非 group 的字段）
//	  ≡  OHLCVDataProvider.Fields()（provider 自报能供应的字段）
//
// **双向**：任一侧多一个或少一个都红，并点名是哪个字段。这样 OBS-08 那种
// 「闸门放行 provider 求不出的字段 / 拦掉 provider 支持的字段」就再也回不来。
package expression

import (
	"reflect"
	"sort"
	"testing"

	expr "github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// TestOHLCVDataProvider_FieldsMatchLanguageRegistry 双向对齐护栏。
func TestOHLCVDataProvider_FieldsMatchLanguageRegistry(t *testing.T) {
	providerFields := NewOHLCVDataProvider(nil).Fields()
	langFields := expr.AvailableDataFields()

	providerSet := make(map[string]bool, len(providerFields))
	for _, f := range providerFields {
		providerSet[f] = true
	}
	langSet := make(map[string]bool, len(langFields))
	for _, f := range langFields {
		langSet[f] = true
	}

	// provider 多出来的字段：provider 供得了、语言注册表（来源非 group）没有
	// —— 闸门会拦掉，正经用法被「误拒」。
	for _, f := range providerFields {
		if !langSet[f] {
			t.Errorf("provider.Fields() 多出字段 %q：语言注册表（来源非 group）里没有它 —— "+
				"provider 供得了、闸门却不认，正经用法会被误拒", f)
		}
	}
	// 语言多出来的字段：注册表登记了、provider 供不了 —— 「假合法」。
	for _, f := range langFields {
		if !providerSet[f] {
			t.Errorf("provider.Fields() 缺少字段 %q：语言注册表（来源非 group）登记了它 —— "+
				"闸门放行、provider 却求不出值，是「假合法」", f)
		}
	}

	// 集合层面再兜一道整体相等（逐条已点名，这里防遗漏）。
	if !reflect.DeepEqual(sortedCopy(providerFields), sortedCopy(langFields)) {
		t.Errorf("provider.Fields() 与 expr.AvailableDataFields() 不相等：\n  provider = %v\n  language = %v",
			providerFields, langFields)
	}
}

// TestOHLCVDataProvider_FieldsWellFormed 钉住 Fields() 的形态：升序、无重复、非空。
func TestOHLCVDataProvider_FieldsWellFormed(t *testing.T) {
	fields := NewOHLCVDataProvider(nil).Fields()
	if len(fields) == 0 {
		t.Fatal("OHLCVDataProvider.Fields() 为空 —— 能力声明不能是空的")
	}
	for i := 1; i < len(fields); i++ {
		if fields[i-1] == fields[i] {
			t.Errorf("Fields() 含重复字段 %q", fields[i])
		}
		if fields[i-1] > fields[i] {
			t.Errorf("Fields() 未升序：%q 出现在 %q 之前", fields[i-1], fields[i])
		}
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
