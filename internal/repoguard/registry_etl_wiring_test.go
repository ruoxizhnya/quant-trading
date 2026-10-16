package repoguard

// DATA-08 结构护栏：多源 Registry 的取数链今天**没有生产调用方**，这是
// 2026-10-16 主任裁决的「停用路线」—— 链保持配置（观测端点要展示它），
// 但「没有生产路径走 Fetch」必须是一个**可见且刻意**的状态，而不是
// 「反正没人管，谁想接就接」。
//
// 守两条，各管一条漂移路径：
//
//   A. 生产代码（cmd/ pkg/ internal/ 的非测试文件）不得调用
//      `NewETLPipeline(...)`；包外（非 package source 的文件）不得出现
//      `ETLPipeline{...}` 复合字面量。Registry.Fetch 的唯一生产调用方是
//      ETLPipeline.Process，而 ETLPipeline 零生产构造点 —— 哪天有人把
//      它接进 HTTP/作业层，这条守卫当场红，逼他先走 DATA-10 的接线计划
//      （单位边界 → Normalizer → sync_jobs 关系），而不是静默多一条
//      生产写路径。
//      包内字面量刻意放行：`NewETLPipeline` 构造器自己的函数体就返回
//      `&ETLPipeline{...}`，那是**实现**不是接线 —— 第一版判据把包内
//      字面量也算违规，结果守卫从第一天起就对着 etl.go 常红（假护栏
//      形状），故按「包外才算接线」收窄。
//
//   B. cmd/data/registry_handlers.go 这个观测面文件不得出现 `.Fetch(`
//      选择器调用。Registry 的 HTTP 端点今天是也只应该是只读观测
//      （status/health/chains）；一个会触发真实取数的端点必须经过与 A
//      同样的显式决策。
//
// 为什么放在 repoguard：要扫的是全仓生产代码，walk 机器在这个包里已有
// 先例（package_wiring_test.go，AUD-45/46）。测试文件被刻意排除 ——
// etl_test.go / integration_test.go 对 ETLPipeline 的行为覆盖正是今天
// 这条链唯一的「使用者」，把它们算进违规会让守卫从第一天起就红。
//
// 两条规则的牙都由替身用例当场证明（TestETLWiringGuard_* /
// TestRegistryObservabilityGuard_*）：合成源码喂进去，该红的红、该放的
// 放 —— 不靠「将来真有人踩」。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// etlPackageName 是 ETLPipeline 的定义包名。包内的复合字面量是实现
// （构造器本体），包外才是接线。
const etlPackageName = "source"

// etlWiringOffendersInSource 返回一份源码里所有「把 ETLPipeline 接进
// 生产」的站点，元素形如 `file:line: 形态`。
//
// 判定用 AST 而不是文本匹配，两个文本陷阱因此不存在：
//   - 定义不是调用：`func NewETLPipeline(...)` 是 FuncDecl，不进 CallExpr；
//   - 注释不是代码：文件头注释里点名 NewETLPipeline（etl.go 自己就有）
//     不会进 AST —— 解析时刻意不带 comment。
//
// 注意解析模式：必须完整解析（mode 0）。package_wiring_test.go 用的
// parser.ImportsOnly 只解析 import 声明、整个跳过函数体 —— 第一版图省
// 事照抄了它，结果替身腿全部落空、仓库级测试「假绿」（扫了个空 AST）。
func etlWiringOffendersInSource(src, filename string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		// 解析失败如实报出来：文件坏掉是错误，不是静默跳过。
		return []string{filename + ": parse error: " + err.Error()}
	}
	inETLPackage := f.Name.Name == etlPackageName
	var offenders []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if name := calleeName(x.Fun); name == "NewETLPipeline" || name == etlPackageName+".NewETLPipeline" {
				offenders = append(offenders, fmt.Sprintf("%s:%d: NewETLPipeline 调用", filename, fset.Position(x.Pos()).Line))
			}
		case *ast.CompositeLit:
			// 包内的字面量是构造器实现，放行；包外手搓 pipeline 算接线。
			if inETLPackage {
				return true
			}
			if name := typeName(x.Type); name == "ETLPipeline" || name == etlPackageName+".ETLPipeline" {
				offenders = append(offenders, fmt.Sprintf("%s:%d: ETLPipeline 复合字面量", filename, fset.Position(x.Pos()).Line))
			}
		}
		return true
	})
	return offenders
}

// calleeName 把调用表达式的被调方渲染成可比对的名字：包级函数是 Ident
// （NewETLPipeline），跨包调用是 SelectorExpr（source.NewETLPipeline）。
// 其他形态（函数字面量立即调用、方法值……）返回 ""，不误伤。
func calleeName(fun ast.Expr) string {
	switch e := fun.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			return pkg.Name + "." + e.Sel.Name
		}
	}
	return ""
}

// typeName 渲染复合字面量的类型名：跨包是 SelectorExpr（source.ETLPipeline），
// 同包是 Ident（ETLPipeline）。指针形态 `&ETLPipeline{}` 的 Type 仍是
// CompositeLit 里的那个类型表达式，一并覆盖。
func typeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			return pkg.Name + "." + e.Sel.Name
		}
	}
	return ""
}

// registryObservabilityOffendersInSource 返回观测面源码里所有 `.Fetch(`
// 选择器调用站点。只认精确的 `Fetch`——`FetchStocks` /
// `FetchDailyOHLCV` 这类选择器不是它，不误伤。
func registryObservabilityOffendersInSource(src, filename string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return []string{filename + ": parse error: " + err.Error()}
	}
	var offenders []string
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Fetch" {
			offenders = append(offenders, fmt.Sprintf("%s:%d: .Fetch 选择器调用", filename, fset.Position(sel.Pos()).Line))
		}
		return true
	})
	return offenders
}

// walkProductionGoFiles 枚举仓库里所有**生产** Go 文件（非 _test.go），
// 范围 cmd/ pkg/ internal/ —— 接线只可能发生在这三棵树里。跳过目录与
// package_wiring_test.go 保持一致（.workbuddy-ai 里躺着别的工单的
// worktree，扫进去会把别人的在途代码算进本仓状态）。
func walkProductionGoFiles(t *testing.T, root string, fn func(rel, src string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", "node_modules", "web", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "pkg/") && !strings.HasPrefix(rel, "internal/") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fn(rel, string(raw))
		return nil
	})
	require.NoError(t, err)
}

// TestProductionCodeDoesNotWireETLPipeline —— 规则 A 的仓库级断言：
// 今天没有任何生产文件构造 ETLPipeline。红的那一刻只有两种正当解释：
// DATA-10 接线落地了（来本测试把新站点加白并写理由），或者有人偷偷
// 接了线（回退）。
func TestProductionCodeDoesNotWireETLPipeline(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	var offenders []string
	walkProductionGoFiles(t, root, func(rel, src string) {
		offenders = append(offenders, etlWiringOffendersInSource(src, rel)...)
	})
	sort.Strings(offenders)

	assert.Empty(t, offenders,
		"生产代码里出现了 ETLPipeline 的构造/调用 —— Registry.Fetch 的取数链按 DATA-08（2026-10-16 主任裁决）保持停用，"+
			"接线必须先落 DATA-10 计划（源单位实测 → 同步边界换算 → Normalizer → 与 sync_jobs 的关系），"+
			"而不是先接一个静默的生产写路径。确认要走 DATA-10，就把站点加进白名单并写明理由：%v", offenders)
}

// TestRegistryObservabilitySurfaceDoesNotFetch —— 规则 B 的仓库级断言：
// 观测面文件不得触发真实取数。
func TestRegistryObservabilitySurfaceDoesNotFetch(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	rel := "cmd/data/registry_handlers.go"
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)

	offenders := registryObservabilityOffendersInSource(string(raw), rel)
	assert.Empty(t, offenders,
		"registry_handlers.go 出现了 .Fetch 调用 —— DATA-08 裁决 Registry 的 HTTP 面只做只读观测"+
			"（status/health/chains）。会触发真实取数的端点属于新的生产写路径，按 DATA-10 计划显式立项：%v", offenders)
}

// TestETLWiringGuard_FlagsSyntheticCallSite —— 规则 A 的替身五腿：
// 跨包调用、同包调用、包外字面量各红一次；构造器本体的包内字面量与
// 注释不误伤。少了这一组，仓库级测试今天的绿可能只是因为「扫的范围里
// 恰好没有」。
func TestETLWiringGuard_FlagsSyntheticCallSite(t *testing.T) {
	t.Parallel()

	cross := `
package main

import "github.com/ruoxizhnya/quant-trading/pkg/data/source"

func wire() {
	_ = source.NewETLPipeline(nil, nil)
}
`
	got := etlWiringOffendersInSource(cross, "synthetic_cross.go")
	require.Len(t, got, 1, "source.NewETLPipeline(...) 必须被抓到：%v", got)
	assert.Contains(t, got[0], "NewETLPipeline 调用")

	local := `
package source

func wire() {
	_ = NewETLPipeline(nil, nil)
}
`
	got = etlWiringOffendersInSource(local, "synthetic_local.go")
	require.Len(t, got, 1, "同包 NewETLPipeline(...) 同样必须被抓到：%v", got)

	literal := `
package main

import "github.com/ruoxizhnya/quant-trading/pkg/data/source"

func wire() {
	_ = &source.ETLPipeline{}
}
`
	got = etlWiringOffendersInSource(literal, "synthetic_literal.go")
	require.Len(t, got, 1, "包外手搓 ETLPipeline 字面量必须被抓到：%v", got)

	// 反证腿一：构造器本体的包内字面量是实现不是接线 —— 第一版判据
	// 把它算违规，守卫从第一天起就对着 etl.go 常红（假护栏形状）。
	definition := `
package source

// NewETLPipeline 的注释里点名 NewETLPipeline 不算代码。
func NewETLPipeline(reg *Registry, store any) *ETLPipeline {
	return &ETLPipeline{Registry: reg}
}
`
	require.Empty(t, etlWiringOffendersInSource(definition, "synthetic_def.go"),
		"package source 内的字面量与注释不是接线站点，不该被算进违规")
}

// TestRegistryObservabilityGuard_FlagsSyntheticFetch —— 规则 B 的替身：
// 合成一个带 .Fetch 的观测面源码必须红；只读形态必须放行。
func TestRegistryObservabilityGuard_FlagsSyntheticFetch(t *testing.T) {
	t.Parallel()

	fetching := `
package main

func (h *registryHandler) preview(c *gin.Context) {
	resp, err := h.reg.Fetch(c.Request.Context(), req)
	_ = resp
	_ = err
}
`
	got := registryObservabilityOffendersInSource(fetching, "synthetic_fetch.go")
	require.Len(t, got, 1, "观测面里的 .Fetch 必须被抓到：%v", got)

	readonly := `
package main

func (h *registryHandler) status(c *gin.Context) {
	_ = h.reg.ListAdapters()
	_ = h.reg.ListChains()
	_ = h.reg.HealthCheck(c.Request.Context())
}
`
	require.Empty(t, registryObservabilityOffendersInSource(readonly, "synthetic_readonly.go"),
		"只读观测形态（List/HealthCheck）必须放行 —— 否则判据不是「不得取数」而是别的什么")
}
