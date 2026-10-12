package repoguard

// K12 / K1-P2-4 structural guard: **a kernel module must never call
// Kernel.Module() from inside a lifecycle method (Init / Start / Stop).**
//
// Why this exists (the defect it prevents)
// ---------------------------------------
// `StandardKernel.Boot` and `Shutdown` hold `k.mu` for their whole run
// (pkg/kernel/kernel.go). `Kernel.Module()` takes the same mutex, and
// `sync.Mutex` is not reentrant — a module that calls back into `Module()`
// from `Init`/`Start`/`Stop` deadlocks the kernel against itself. Today no
// module holds a kernel reference (dependencies are injected at construction,
// e.g. `StrategyRuntimeDeps`), so the hole is closed by circumstance, not by
// structure. The contract text lives in pkg/kernel/interfaces.go (Module 注释
// 「禁令（K12 / K1-P2-4）」); this file is its machine-checked half.
//
// The rule
// --------
// In any .go file under the repo root, a method named `Init`, `Start` or
// `Stop` (the Module lifecycle names) must not contain a call expression to
// `Module` — bare (`Module(x)`) or selector (`k.Module(x)`).
//
// Boundary of this check: it is a name-based approximation without type
// information. It also flags a non-module type whose `Start` calls some other
// `Module()` method — accepted cost: the fix (call it outside the lifecycle
// method, or take the dependency at construction) is the same. Comments and
// strings mentioning `Module()` are NOT flagged: the scan walks the AST, so
// prose cannot trip it (that failure mode is why e2e/guard documents
// scanInside(FuncName) instead of scanning source text).
//
// 已知盲区（披露，暂不闭合，K12 审查修复）：本扫描只认生命周期方法体里
// **直接写出**的 Module() 调用，不追同包 helper 的调用链——`Init -> m.setup()
// -> k.Module()` 会逃逸（setup 不在生命周期名表里，不在扫描面内）。接受
// 它的理由：禁令正文由 pkg/kernel/interfaces.go 的契约注释承载，且当前
// 没有任何模块持有内核引用；要闭合得做包内调用可达性分析，留到真有模块
// 拿到内核句柄时再做。对照腿「生命周期方法没有回调 → 放行」里的
// `m.connect()` 正是这个盲区的入口形态，那里放行是正确的（体内确无直接
// 回调），不是漏判。
//
// Anti-blindness: TestModuleLifecycleCallbackGuardSeesTheTree asserts the
// walker reaches real lifecycle methods (pkg/kernel has plenty), and
// TestModuleLifecycleCallbackGuardJudgesSnippets feeds the scanner the exact
// shapes it claims to catch — including a comment-only mention it must
// ignore.

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

// lifecycleMethodNames are the kernel.Module lifecycle methods.
var lifecycleMethodNames = map[string]bool{"Init": true, "Start": true, "Stop": true}

// moduleLifecycleSkipDirs mirrors the other repoguard walkers: generated /
// vendored / frontend trees carry no kernel modules.
var moduleLifecycleSkipDirs = map[string]bool{
	".git": true, ".workbuddy-ai": true, "node_modules": true,
	"web": true, "testdata": true, "vendor": true,
}

// moduleLifecycleViolation is one offending site.
type moduleLifecycleViolation struct {
	where string // file:line
	why   string
}

// TestModuleLifecycleMethodsNeverCallBackModule is the guard.
func TestModuleLifecycleMethodsNeverCallBackModule(t *testing.T) {
	t.Parallel()

	violations, _, _, err := scanModuleLifecycleCallbacks(filepath.Join("..", ".."))
	require.NoError(t, err)

	msgs := make([]string, 0, len(violations))
	for _, v := range violations {
		msgs = append(msgs, v.where+" → "+v.why)
	}
	sort.Strings(msgs)

	assert.Empty(t, msgs,
		"模块在生命周期方法（Init/Start/Stop）里回调 Module() —— Boot/Shutdown 全程持内核锁，"+
			"这会在同一把锁上自死锁（K12/K1-P2-4，契约见 pkg/kernel/interfaces.go）。"+
			"模块间依赖走构造期注入（装配处传前序模块产物字段），不要在生命周期里检索：\n%s",
		strings.Join(msgs, "\n"))
}

// TestModuleLifecycleCallbackGuardSeesTheTree makes the guard above
// non-blind: a walker that sees nothing would report "no violations" and the
// green would be worthless.
func TestModuleLifecycleCallbackGuardSeesTheTree(t *testing.T) {
	t.Parallel()

	_, scanned, lifecycleMethods, err := scanModuleLifecycleCallbacks(filepath.Join("..", ".."))
	require.NoError(t, err)

	assert.Contains(t, scanned, "pkg/kernel/adapters.go",
		"遍历没有走到模块实现所在的文件上 —— 那么「没有违规」什么也没证明")
	assert.Contains(t, scanned, "pkg/kernel/modules_analysis.go",
		"遍历没有走到业务模块包装文件上 —— 覆盖面声明是假的")
	assert.GreaterOrEqual(t, lifecycleMethods, 10,
		"一个生命周期方法都没识别出来（got %d）—— 识别逻辑瞎了，护栏的绿是假的", lifecycleMethods)
}

// TestModuleLifecycleCallbackGuardJudgesSnippets is the guard's own
// counterexample leg: the scanner is fed the shapes it claims to catch and
// must reject them, plus control shapes (including a comment-only mention)
// it must leave alone.
func TestModuleLifecycleCallbackGuardJudgesSnippets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "Start 里选择器回调 Module() —— 本仓真实风险形状",
			src: `package p
func (m *T) Start(ctx context.Context) error {
	_, _ = k.Module("data-engine")
	return nil
}`,
			want: true,
		},
		{
			name: "Init 里裸调用 Module()",
			src: `package p
func (m *T) Init(ctx context.Context) error {
	_ = Module("data-engine")
	return nil
}`,
			want: true,
		},
		{
			name: "Stop 里回调 Module()",
			src: `package p
func (m *T) Stop(ctx context.Context) error {
	k.Module("msgbus")
	return nil
}`,
			want: true,
		},
		{
			// m.connect() 体内若有 Module() 回调，会经 helper 逃逸本护栏——
			// 那是文件头披露的已知盲区；本腿只钉「体内无直接回调 → 放行」。
			name: "对照腿：生命周期方法没有回调 → 放行",
			src: `package p
func (m *T) Start(ctx context.Context) error {
	return m.connect()
}`,
			want: false,
		},
		{
			name: "对照腿：非生命周期方法里调 Module() 不在本护栏范围 → 放行",
			src: `package p
func (k *K) Lookup() {
	_, _ = k.Module("data-engine")
}`,
			want: false,
		},
		{
			name: "对照腿：注释里提到 Module() 不算（证明是 AST 扫而非扫原文）",
			src: `package p
func (m *T) Start(ctx context.Context) error {
	// 禁止在生命周期里调用 Module() —— 会自死锁
	return nil
}`,
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", tc.src, 0)
			require.NoError(t, err)

			violations := inspectModuleLifecycleCallbacks(fset, "snippet.go", f)
			if tc.want {
				assert.NotEmpty(t, violations, "该报违规却没报 —— 扫描器对这个形状是瞎的")
			} else {
				assert.Empty(t, violations, "不该报却报了：%v", violations)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────
// scanner
// ──────────────────────────────────────────────────────────────────────

// scanModuleLifecycleCallbacks walks every .go file under root and returns
// the violations, the files inspected, and how many lifecycle methods were
// identified (for the anti-blindness assertions).
func scanModuleLifecycleCallbacks(root string) ([]moduleLifecycleViolation, []string, int, error) {
	var (
		violations []moduleLifecycleViolation
		scanned    []string
		lifecycle  int
	)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if moduleLifecycleSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		scanned = append(scanned, rel)

		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, raw, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}

		vs := inspectModuleLifecycleCallbacks(fset, rel, f)
		violations = append(violations, vs...)
		lifecycle += countLifecycleMethods(f)
		return nil
	})
	if err != nil {
		return nil, nil, 0, err
	}
	sort.Strings(scanned)
	return violations, scanned, lifecycle, nil
}

// inspectModuleLifecycleCallbacks inspects one parsed file and reports every
// Module() call found inside a lifecycle method body.
func inspectModuleLifecycleCallbacks(fset *token.FileSet, rel string, f *ast.File) []moduleLifecycleViolation {
	var violations []moduleLifecycleViolation

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil || !lifecycleMethodNames[fd.Name.Name] {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				if fun.Name == "Module" {
					violations = append(violations, moduleLifecycleViolation{
						where: fmt.Sprintf("%s:%d", rel, fset.Position(call.Pos()).Line),
						why: fmt.Sprintf("生命周期方法 %s 内调用了 Module() —— Boot/Shutdown 全程持内核锁，"+
							"这里回调会自死锁", fd.Name.Name),
					})
				}
			case *ast.SelectorExpr:
				if fun.Sel.Name == "Module" {
					violations = append(violations, moduleLifecycleViolation{
						where: fmt.Sprintf("%s:%d", rel, fset.Position(call.Pos()).Line),
						why: fmt.Sprintf("生命周期方法 %s 内调用了 .Module() —— Boot/Shutdown 全程持内核锁，"+
							"这里回调会自死锁", fd.Name.Name),
					})
				}
			}
			return true
		})
	}
	return violations
}

// countLifecycleMethods counts receiver methods named Init/Start/Stop.
func countLifecycleMethods(f *ast.File) int {
	n := 0
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Recv != nil && lifecycleMethodNames[fd.Name.Name] {
			n++
		}
	}
	return n
}
