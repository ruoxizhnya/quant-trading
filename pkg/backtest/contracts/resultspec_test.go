package contracts

// K9 契约护栏：ResultSpec canonical schema 冻结。
//
// 这是「变更须破坏本测试、走变更评审」的执行点（docs/SPEC.md「ResultSpec 冻结」）。
// 三条腿：
//   1. 字段集护栏 —— BacktestResponse 的 JSON 字段名集合（含顺序）与冻结清单
//      逐项相等。增删字段、改 json tag ⇒ 红。破坏腿：随便删一个字段或改 tag。
//   2. status 三值护栏 —— 语义值集合恰好是 {completed, invalid, failed}，
//      常量值与语义一一对应。新增第四种取值 ⇒ 必须显式改这里（= 走评审）。
//   3. 文档互锁 —— docs/SPEC.md 必须覆盖冻结字段集与三值语义。改字段不改
//      文档 ⇒ 红。这条腿的来由：本节前身「Backtest Report Metrics」列着根本
//      不存在的 Profit factor / Turnover —— 文档与实现静默漂移的标本。

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// frozenResultSpecFields 是 BacktestResponse 的冻结 JSON 字段集，按声明序。
// 这份清单是契约本身：它与 docs/SPEC.md 的字段表一一对应。
var frozenResultSpecFields = []string{
	"id",
	"status",
	"strategy",
	"strategy_git_hash",
	"start_date",
	"end_date",
	"total_return",
	"annual_return",
	"sharpe_ratio",
	"sortino_ratio",
	"max_drawdown",
	"max_drawdown_date",
	"win_rate",
	"total_trades",
	"win_trades",
	"lose_trades",
	"avg_holding_days",
	"calmar_ratio",
	"started_at",
	"completed_at",
	"error",
	"portfolio_values",
	"trades",
	"stock_pool",
	"initial_capital",
	"invalid_reasons",
	"universe_max_size",
}

// frozenStatusValues 是 status 的冻结语义值集。
var frozenStatusValues = []string{StatusCompleted, StatusInvalid, StatusFailed}

func TestResultSpec_FieldSetFrozen(t *testing.T) {
	typ := reflect.TypeOf(BacktestResponse{})
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			t.Fatalf("字段 %s 缺少有效 json tag —— ResultSpec 的每个字段都必须可寻址", f.Name)
		}
		got = append(got, name)
	}

	if len(got) != len(frozenResultSpecFields) {
		t.Fatalf("ResultSpec 字段集变了（契约，需走变更评审）:\n got(%d): %v\nwant(%d): %v",
			len(got), got, len(frozenResultSpecFields), frozenResultSpecFields)
	}
	for i := range got {
		if got[i] != frozenResultSpecFields[i] {
			t.Fatalf("ResultSpec 字段集第 %d 项变了（契约，需走变更评审）:\n got: %v\nwant: %v",
				i, got, frozenResultSpecFields)
		}
	}
}

func TestResultSpec_StatusTriStateFrozen(t *testing.T) {
	// 常量与语义值一一对应：改常量值 = 改线上判据，必须走评审。
	want := map[string]string{
		"completed": StatusCompleted,
		"invalid":   StatusInvalid,
		"failed":    StatusFailed,
	}
	if len(frozenStatusValues) != len(want) {
		t.Fatalf("status 语义值集大小变了: %d, want %d（新增取值 = 契约变更）", len(frozenStatusValues), len(want))
	}
	for _, v := range frozenStatusValues {
		if _, ok := want[v]; !ok {
			t.Errorf("status 出现冻结集之外的取值 %q —— 契约变更需走评审", v)
		}
	}
	for semantic, constant := range want {
		if constant != semantic {
			t.Errorf("常量值 %q 与语义 %q 不一致", constant, semantic)
		}
	}

	// 不变量涉及的三个键必须都在字段集里，否则三值语义无处安放。
	for _, key := range []string{"status", "error", "invalid_reasons"} {
		found := false
		for _, f := range frozenResultSpecFields {
			if f == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("三值不变量依赖字段 %q，但它不在冻结字段集里", key)
		}
	}
}

func TestResultSpec_SpecDocCoversFrozenFieldSet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "SPEC.md"))
	if err != nil {
		t.Fatalf("读 docs/SPEC.md 失败（护栏需要文档互锁）: %v", err)
	}
	doc := string(raw)

	if !strings.Contains(doc, "ResultSpec 冻结") {
		t.Fatal("docs/SPEC.md 缺「ResultSpec 冻结」节 —— 契约文本必须在文档里")
	}
	for _, key := range frozenResultSpecFields {
		if !strings.Contains(doc, "`"+key+"`") {
			t.Errorf("docs/SPEC.md 字段表缺 %q —— 改字段集必须同步文档并走变更评审", key)
		}
	}
	for _, v := range frozenStatusValues {
		if !strings.Contains(doc, "`"+v+"`") {
			t.Errorf("docs/SPEC.md 缺 status 取值 %q", v)
		}
	}
}
