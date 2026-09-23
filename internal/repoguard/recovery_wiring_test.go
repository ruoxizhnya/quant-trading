package repoguard

// AUD-50 / AUD-49 structural guard: **a function that is written but never
// called from the place that has to call it is not a safety net.**
//
// Why this exists. `CleanupStaleRunning` was written for the backtest queue in
// P0-8, tested, and wired into `gracefulShutdown` — but only there. Its own doc
// comment has always said it is "also useful as a recovery tool after a hard
// process crash (kill -9, OOM, etc.) — call it on startup to repair stale rows
// from the previous run", and **no startup caller ever existed**. pkg/sync had
// no such function at all, so an interrupted sync job stayed `running` forever
// after a restart (workers only dequeue `pending`).
//
// ⚠️ **Why the call site is pinned to `file:function` and not to the file.**
// The first version of this guard asserted only "the calling file contains a
// call". Sabotage verification killed it: deleting the startup call from
// cmd/analysis/setup.go left the guard **green**, because gracefulShutdown in
// the same file still called the same method. Worse, that version would have
// been green on the *original buggy code* too — setup.go did call
// CleanupStaleRunning, just never at startup. A guard that passes on the bug it
// was written for is not a guard. Pinning the enclosing function is what makes
// it red on the actual shape (PITFALLS §54: the criterion has to be as precise
// as the defect).
//
// The call site is derived by parsing, not by grepping text: a text search
// cannot tell a call from a mention in a comment, and this file's own prose
// would match (PITFALLS §29 — the first version of the gin guard was caught by
// its own documentation comment).
//
// Boundary of this check:
//   - Only non-test .go files count. A call from a test is exactly the
//     "suite is green but the wire is missing" shape this is meant to catch.
//   - The enclosing function is identified by name only; an anonymous function
//     literal inside it is attributed to the enclosing declaration. That is
//     fine here (a closure inside Start/buildDataServices is still wired), but
//     it means the label is a location, not a proof of reachability.
//   - ⚠️ The search matches on the **method name**, not on the receiver's type
//     — resolving that needs a full type-check. Two same-named recovery methods
//     in different packages are indistinguishable to the walker. Today the two
//     `CleanupStaleRunning` methods live in different packages and are called
//     from different files, so pinning `file:function` disambiguates them. If
//     they ever end up called from the same function, this guard can no longer
//     tell them apart; teach the walker to resolve receivers first.
//   - A rename or move makes the entry stale, and staleness is asserted: if
//     the declaring file no longer declares the function, the guard goes red
//     rather than silently guarding a name that no longer exists.
//   - It says nothing about whether the call is reachable at runtime. Only a
//     test that drives the real startup path can show that (pkg/sync has one:
//     TestWorkerPool_StartRecoversInterruptedJob).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recoveryCallSites names the functions that must be called from a specific
// `file:function`.
//
// Every entry is a claim about wiring, so it has to name the caller down to the
// function. Naming only the file lets the backtest entry pass on the strength of
// the shutdown call — which is exactly how the startup gap survived unnoticed.
//
// The table covers two shapes of the same defect class ("written, but not wired
// from the place that has to wire it"): recovery functions that nobody calls,
// and setters that register a capability nobody registers.
var recoveryCallSites = []struct {
	declFile string // where the function is declared
	callSite string // "relative/file.go:EnclosingFunction" that must call it
	funcName string
	why      string
}{
	{
		declFile: "pkg/backtest/job/job.go",
		callSite: "cmd/analysis/setup.go:buildDataServices",
		funcName: "CleanupStaleRunning",
		why: "AUD-50: the shutdown half is wired in gracefulShutdown, but the startup " +
			"half — the one the doc comment promises — was missing, so a backtest " +
			"interrupted by kill -9 / OOM stays `running` until someone notices. " +
			"buildDataServices must call it before the HTTP server starts.",
	},
	{
		declFile: "pkg/sync/queue.go",
		callSite: "pkg/sync/worker.go:Start",
		funcName: "CleanupStaleRunning",
		why: "AUD-50: the sync worker pool only dequeues `pending`, so a row left " +
			"`running` by a restart is invisible forever. WorkerPool.Start must call " +
			"it before launching any worker — hanging it there is what makes the " +
			"recovery impossible to forget.",
	},
	{
		declFile: "pkg/sync/job.go",
		callSite: "cmd/data/sync_handlers.go:NewSyncHandler",
		funcName: "SetRunningCanceller",
		why: "AUD-49: cancelling a running job needs two halves — settle the row, and " +
			"cancel the context of the goroutine executing it. CancelJob can only do the " +
			"first; the second needs this wiring, and without it there is no handle to the " +
			"executor at all, so the endpoint answers 200 while the sync keeps running. " +
			"NewSyncHandler must register workerPool.Cancel.",
	},
}

func TestRecoveryFunctionsAreCalledFromTheRightPlace(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	for _, want := range recoveryCallSites {
		declPath := filepath.Join(root, filepath.FromSlash(want.declFile))
		declared, err := declaresFunc(declPath, want.funcName)
		require.NoError(t, err, "解析声明文件 %s", want.declFile)
		if !declared {
			t.Errorf("%s 里没有声明 %s —— 这张表过期了。"+
				"改名或移动后必须同步更新，否则护栏会去守一个不存在的名字，"+
				"而它永远绿（这正是「白名单替死代码打掩护」的形态）",
				want.declFile, want.funcName)
			continue
		}

		sites, err := productionCallSites(root, want.funcName)
		require.NoError(t, err)

		if !contains(sites, want.callSite) {
			sort.Strings(sites)
			t.Errorf("%s 没有被 %s 调用 ——\n  %s\n"+
				"  现有生产调用点（file:function）：%v\n"+
				"  「写好了但没人从该调的地方调」等于没写。",
				want.funcName, want.callSite, want.why, sites)
			continue
		}
		t.Logf("%s: %s 调用了它（生产调用点共 %d 处）",
			want.funcName, want.callSite, len(sites))
	}
}

// declaresFunc reports whether path declares a function or method named name.
func declaresFunc(path, name string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false, err
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		if fn.Name.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// productionCallSites returns repo-relative "file:EnclosingFunc" entries for
// every non-test function body that calls a method named name.
func productionCallSites(root, name string) ([]string, error) {
	var out []string

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
		base := d.Name()
		if !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}

		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name == nil {
				continue
			}
			if callsMethodNamed(fn.Body, name) {
				out = append(out, rel+":"+fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// callsMethodNamed reports whether body contains a call of the form x.name(...).
func callsMethodNamed(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel != nil && sel.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// Guard the guard: the walker must actually see the tree it claims to scan, and
// must report call sites at function granularity.
//
// Without the first assertion, a wrong `root` would make productionCallSites
// return an empty list for everything — and the "stale entry" branch could never
// fire either, hiding a broken walker behind a red that looks like a real
// finding. Without the second, a regression that collapses the label back to
// file granularity would silently reintroduce the blindness described above.
func TestProductionCallSiteWalkerSeesTheTree(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	sites, err := productionCallSites(root, "Info")
	require.NoError(t, err)
	require.NotEmpty(t, sites,
		"连一个 .Info(...) 调用都扫不到 —— 说明遍历根本没走到仓库里，"+
			"那么上面那条护栏的「找不到调用点」是假的")

	withFunc := 0
	for _, s := range sites {
		if idx := strings.LastIndex(s, ":"); idx >= 0 && idx < len(s)-1 {
			withFunc++
		}
	}
	assert.Equal(t, len(sites), withFunc,
		"调用点标签必须是 file:function 形式 —— 退化成只有文件名就会重新变瞎")
}
