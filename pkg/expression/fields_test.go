// OBS-08 切片 1 —— 字段注册表的闸门行为用例。
//
// 记录「改前 vs 改后」的三类处置（见 TASKS OBS-08 / ADR-030）：
//   - 误拒修复：ps / roa 从「闸门不认」变为「接受」；
//   - 假合法清退：market_cap / roe_ttm / eps 从「接受」变为「闸门拒绝」；
//   - group 正名：sector 仍是语言合法字段，但不算数据字段。
package expression

import (
	"strings"
	"testing"
)

// TestFieldsRegistry_AcceptedFields 钉住「应被闸门接受」的字段全集。
func TestFieldsRegistry_AcceptedFields(t *testing.T) {
	accepted := []string{
		// market
		"open", "high", "low", "close", "volume", "turnover",
		// fundamentals（ps/roa 是 OBS-08 修复的「误拒」；revenue/profit 是补接线的）
		"pe", "pb", "ps", "roe", "roa", "revenue", "profit",
		// group 标签（语法合法）
		"sector",
	}
	for _, f := range accepted {
		ex, err := NewParser().Parse(f)
		if err != nil {
			t.Errorf("字段 %q 解析失败: %v", f, err)
			continue
		}
		if err := ex.Validate(); err != nil {
			t.Errorf("字段 %q 应被闸门接受，却被拒: %v", f, err)
		}
	}
}

// TestFieldsRegistry_PhantomFieldsRejected 钉住「假合法」字段已被清退：
// market_cap / roe_ttm / eps 在 domain 里没有对应数据，provider 永不可求值，
// 闸门必须直接拒绝并给出可用字段清单。
func TestFieldsRegistry_PhantomFieldsRejected(t *testing.T) {
	for _, f := range []string{"market_cap", "roe_ttm", "eps"} {
		ex, err := NewParser().Parse(f)
		if err != nil {
			t.Fatalf("字段 %q 解析失败: %v", f, err)
		}
		err = ex.Validate()
		if err == nil {
			t.Errorf("字段 %q 应被闸门拒绝（无可求值来源的假合法），却通过了", f)
			continue
		}
		if !strings.Contains(err.Error(), f) {
			t.Errorf("拒绝报错应点名字段 %q，got: %v", f, err)
		}
		if !strings.Contains(err.Error(), "可用字段") {
			t.Errorf("拒绝报错应附可用字段清单，got: %v", err)
		}
	}
}

// TestAvailableDataFields_ExcludesGroupLabel 钉住 group 标签与数据字段的分野。
func TestAvailableDataFields_ExcludesGroupLabel(t *testing.T) {
	dataFields := AvailableDataFields()
	for _, f := range dataFields {
		if f == "sector" {
			t.Errorf("sector 是 group 分组标签，不应出现在 AvailableDataFields()：%v", dataFields)
		}
	}
	// sector 仍必须是语言合法字段（cs_neutralize(x, sector) 要用它）。
	allFields := AvailableFields()
	found := false
	for _, f := range allFields {
		if f == "sector" {
			found = true
		}
	}
	if !found {
		t.Errorf("sector 应仍是语言合法字段（分组标签），却不在 AvailableFields()：%v", allFields)
	}
	if len(allFields) != len(dataFields)+1 {
		t.Errorf("AvailableFields() 应恰好多出 1 个 group 标签：data=%d all=%d",
			len(dataFields), len(allFields))
	}
}

// TestFieldSourceOf 钉住来源声明。
func TestFieldSourceOf(t *testing.T) {
	cases := map[string]FieldSource{
		"close":   FieldSourceMarket,
		"pe":      FieldSourceFundamentals,
		"ps":      FieldSourceFundamentals,
		"roa":     FieldSourceFundamentals,
		"revenue": FieldSourceFundamentals,
		"profit":  FieldSourceFundamentals,
		"sector":  FieldSourceGroup,
	}
	for name, want := range cases {
		got, ok := FieldSourceOf(name)
		if !ok {
			t.Errorf("FieldSourceOf(%q) 未登记，期望来源 %q", name, want)
			continue
		}
		if got != want {
			t.Errorf("FieldSourceOf(%q) = %q, want %q", name, got, want)
		}
	}
	if _, ok := FieldSourceOf("market_cap"); ok {
		t.Error("market_cap 已从注册表移除，FieldSourceOf 应返回 false")
	}
}

// TestAvailableFieldsHint_ListsSources 钉住提示文本按来源分组。
func TestAvailableFieldsHint_ListsSources(t *testing.T) {
	hint := AvailableFieldsHint()
	for _, want := range []string{"market:", "fundamentals:", "group:", "sector", "revenue"} {
		if !strings.Contains(hint, want) {
			t.Errorf("AvailableFieldsHint() 缺少 %q：%s", want, hint)
		}
	}
}
