package agents

import (
	"strings"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// TestFactorDSLSyntax_DerivesFromRegistry 是 OBS-08 的护栏：研究提示词里
// 广告的字段与算子清单必须与 pkg/expression 注册表一致。
//
// 这条护栏的由来：2026-10-09 实测该段曾硬编码，广告了 `market_cap`
// （provider 永不可供的幻影字段 ⇒ AI 产出必然过不了闸门的表达式），
// 同时漏掉 `ps`/`roa`/`revenue`/`profit` 与 9 个算子。
// 现在还加了反向断言：已从注册表移除的幻影字段不得再被广告。
func TestFactorDSLSyntax_DerivesFromRegistry(t *testing.T) {
	got := factorDSLSyntax()

	for _, f := range expression.AvailableDataFields() {
		if !strings.Contains(got, f) {
			t.Errorf("提示词缺少可用字段 %q（注册表有、提示词没广告 ⇒ AI 少用能力）", f)
		}
	}
	for _, op := range expression.AvailableOperators() {
		if !strings.Contains(got, op) {
			t.Errorf("提示词缺少算子 %q（注册表有、提示词没广告 ⇒ AI 少用能力）", op)
		}
	}

	// 反向：这些字段当前 provider 永不可供，提示词不得再广告它们。
	// 注意 `volatility` 不在此列 —— 它是 factor 的 *category* 名，不是 DSL 字段。
	for _, phantom := range []string{"market_cap", "roe_ttm", "eps", "vwap", "turnover_rate"} {
		if strings.Contains(got, phantom) {
			t.Errorf("提示词广告了不存在的字段 %q（OBS-08：AI 会产出必然过不了闸门的表达式）", phantom)
		}
	}
}
