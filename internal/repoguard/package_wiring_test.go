package repoguard

// AUD-45 (ODR-065) structural guard: **every package under pkg/ and internal/
// must have at least one importer inside this module**, unless it is named in
// unwiredPackages with a reason.
//
// Why this exists. AUD-43 was registered as "pkg/testutil has zero importers".
// That was true — and true of **13** packages, not one. The repo deliberately
// keeps capability packages that are written and tested *before* they are
// wired (P2-5 「删的是服务不是能力」, P2-9wire 「零件先写、后接线」). So
// "zero importers" is not by itself a defect. But it is a state that should be
// **visible and deliberate** rather than discovered by accident, which is
// exactly what happened here.
//
// The importer set is derived from the source (go/ast over every .go file), not
// from a hand-written list — a hand-written list goes stale the moment someone
// adds a package, and then the guard is green while the repo is not
// (PITFALLS §41). The allowlist therefore only has to name the exceptions, and
// it is self-policing: an entry whose package has since gained an importer is
// reported as stale and must be deleted.
//
// Boundary of this check:
//   - Only pkg/** and internal/** are inspected. cmd/** are entry points and
//     have no importers by design.
//   - A directory with no non-test .go file is skipped: a test-only package is
//     not importable, so "zero importers" says nothing about it. (This package
//     is itself test-only, so it does not check itself.)
//   - Imports are collected from every parsed .go file **regardless of build
//     constraints**, so a package imported only by a platform-gated file still
//     counts as imported. The check is therefore conservative: it can miss a
//     dead package, never invent one.
//   - Transitive deadness is not detected. If a wired package imports an
//     unwired one, the latter is counted as imported.
//   - Files that fail to parse are reported as errors, not skipped silently.

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unwiredPackages maps an import path to the reason it currently has no
// importer. Every entry is a decision: "we are keeping this on purpose, and
// here is what would wire it". An entry that has gained an importer is stale
// and must be removed (the test reports it).
//
// 2026-09-22 (AUD-45) initial inventory — 13 packages.
var unwiredPackages = map[string]string{
	"github.com/ruoxizhnya/quant-trading/pkg/api":                   "P2-16 API 版本化基础设施（APIVersionMiddleware）；各服务目前各自手写 /api/v1，待统一接线",
	"github.com/ruoxizhnya/quant-trading/pkg/decimal":               "定点小数工具库，尚未被采用（portfolio / 回测仍用 float64）—— 属「从未采用」，需裁决采用还是删除（见 AUD-46）",
	"github.com/ruoxizhnya/quant-trading/pkg/metrics":               "⚠️ 与 pkg/observability 重复实现（两者都定义 Metrics / NewMetrics，服务实际用 observability）—— 是「疑似死代码」不是「待接线」，需裁决删除（见 AUD-46）",
	"github.com/ruoxizhnya/quant-trading/pkg/ai/factor":             "因子计算（资金流 / 板块轮动），待 ETL + IC 回测接线",
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/auction":      "P1-6 集合竞价撮合（9:15-9:25 / 14:57-15:00），待回测引擎接线",
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/marketimpact": "市场冲击模型，待回测引擎接线",
	"github.com/ruoxizhnya/quant-trading/pkg/alert/systemalert":     "系统级运维告警（数据同步停滞 / 回测失败 / 策略退化），待运维链路接线",
	"github.com/ruoxizhnya/quant-trading/internal/sandbox/wasm":     "P2-27 WASM 沙箱（ADR-007 Phase 3 / ADR-019），待策略插件执行接线",
	"github.com/ruoxizhnya/quant-trading/pkg/live/margin":           "融资融券账户管理 / 可融券判定，待实盘链路接线",
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/options":      "期权策略（Black-Scholes / 二叉树），待策略层接线",
	"github.com/ruoxizhnya/quant-trading/pkg/data/source/hkex":      "港股数据源，待数据同步接线",
	"github.com/ruoxizhnya/quant-trading/pkg/live/broker/xtp":       "中泰证券 XTP 券商适配，待实盘链路接线",
	"github.com/ruoxizhnya/quant-trading/pkg/testutil":              "预留的 DB 集成测试底座（AUD-43 裁决：保留，不删；归档审查报告曾把它列为「有 DB 环境下可选集成验证」的设想）",
}

func TestEveryPackageHasAConsumer(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	mod := readModulePath(t, root)

	imported := map[string]bool{}
	candidates := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", "node_modules", "web", "testdata", "vendor":
				return fs.SkipDir
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if isCheckedTree(rel) && dirHasNonTestGo(path) {
				candidates[mod+"/"+rel] = true
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, raw, parser.ImportsOnly)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		for _, imp := range file.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			if strings.HasPrefix(p, mod+"/") {
				imported[p] = true
			}
		}
		return nil
	})
	require.NoError(t, err)

	var unwired, stale []string
	for p := range candidates {
		if imported[p] {
			continue
		}
		if _, ok := unwiredPackages[p]; !ok {
			unwired = append(unwired, p)
		}
	}
	for p := range unwiredPackages {
		if imported[p] {
			stale = append(stale, p)
		}
	}
	sort.Strings(unwired)
	sort.Strings(stale)

	assert.Empty(t, unwired,
		"这些包在 pkg/ 或 internal/ 下，但没有任何导入者 —— 要么接线，要么加进 "+
			"unwiredPackages 并写明「为什么留着、什么会接上它」：%v", unwired)
	assert.Empty(t, stale,
		"unwiredPackages 里这些包已经有导入者了 —— 删掉它们的白名单条目"+
			"（白名单只该列「当前无人用」的包，否则它就在替死代码打掩护）：%v", stale)
}

// isCheckedTree reports whether rel is the pkg/ or internal/ tree (the trees
// whose packages are expected to have consumers).
func isCheckedTree(rel string) bool {
	return rel == "pkg" || rel == "internal" ||
		strings.HasPrefix(rel, "pkg/") || strings.HasPrefix(rel, "internal/")
}

// dirHasNonTestGo reports whether dir contains at least one non-test .go file,
// i.e. whether it defines an importable package.
func dirHasNonTestGo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	return false
}

// readModulePath reads the module path from go.mod rather than hard-coding it,
// so the guard keeps working if the module is ever renamed.
func readModulePath(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	require.FailNow(t, "go.mod has no module directive")
	return ""
}
