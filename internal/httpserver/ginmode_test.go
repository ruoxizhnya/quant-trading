package httpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGinModeFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw       string
		wantMode  string
		wantKnown bool
		because   string
	}{
		{"debug", GinModeDebug, true, "显式 debug"},
		{"release", GinModeRelease, true, "显式 release"},
		{"test", GinModeTest, true, "显式 test"},
		{"  DEBUG  ", GinModeDebug, true, "大小写与空白都要能吃下（配置是人写的）"},
		{"Release", GinModeRelease, true, "同上"},
		{"", GinModeRelease, true, "未配置 = 文档默认 release，不算「不认识」"},
		{"production", GinModeRelease, false, "不认识的值要能被调用方识别出来并告警"},
		{"true", GinModeRelease, false, "布尔不是 mode"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			mode, known := ginModeFor(tc.raw)
			assert.Equal(t, tc.wantMode, mode, tc.because)
			assert.Equal(t, tc.wantKnown, known, tc.because)
		})
	}
}

// TestApplyGinModeSetsGlobalMode 覆盖 ginModeFor 之外的那一步：真的写进 gin。
//
// **故意不加 t.Parallel()**：它改进程级 gin 全局，而本包的其它测试是并行的。
// 顶层测试里「没有 t.Parallel 的会先跑完，再恢复并行的那些」，所以串行执行时
// 不存在并发写 —— 加了 t.Parallel 反而会把它变成 AUD-28 那类竞争。
// 结尾用 ApplyGinMode 复位（而不是裸调 gin.SetMode），保持「只有 ginmode.go
// 写这个全局」这条不变量。
func TestApplyGinModeSetsGlobalMode(t *testing.T) {
	cases := []struct {
		raw      string
		wantGin  string
		wantName string
	}{
		{"debug", gin.DebugMode, GinModeDebug},
		{"release", gin.ReleaseMode, GinModeRelease},
		{"test", gin.TestMode, GinModeTest},
		{"nonsense", gin.ReleaseMode, GinModeRelease},
	}
	for _, tc := range cases {
		applied, _ := ApplyGinMode(tc.raw)
		assert.Equal(t, tc.wantName, applied, "返回值应是实际生效的 mode 名")
		assert.Equal(t, tc.wantGin, gin.Mode(), "gin 自己的全局应被改到 %s", tc.wantGin)
	}
	// 复位到 TestMain 选的那个，别把本包留在别的 mode 上。
	ApplyGinMode(GinModeTest)
}

// TestGinSetModeOnlyInSanctionedPlaces is the AUD-29 regression guard.
//
// gin's run mode is a process-wide global (see ginmode.go). A call to
// gin's SetMode is allowed in exactly two places:
//
//  1. internal/httpserver/ginmode.go — the one production helper, applied
//     once per process during startup;
//  2. inside a `func TestMain` in a _test.go file — the AUD-12 / AUD-28
//     pattern, which runs before any test goroutine starts.
//
// Everything else is the bug AUD-29 fixed: all three services used to call
// it from buildRouter(), keyed off whichever logging setting that service
// happened to pick — a process-wide write reachable from tests.
//
// 这个护栏解析 AST 而不是 grep 文本：注释和字符串里提到 gin 的 SetMode 不算数
// （第一版按文本扫，被两处文档注释里的引文误报了）。它检查的也不是「有没有调用
// ApplyGinMode」—— 那样改一次调用点就会误报 —— 而是「有没有绕过它」。
//
// 已知局限：只认限定符写作 `gin` 的调用。若有人把 gin 以别名导入
// （`g "github.com/gin-gonic/gin"`）后写 `g.SetMode`，这里看不见。
func TestGinSetModeOnlyInSanctionedPlaces(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "dist" || name == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "internal/httpserver/ginmode.go" {
			return nil
		}
		offenders = append(offenders, ginSetModeOffenders(t, path, rel)...)
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"gin.SetMode 只允许出现在 internal/httpserver/ginmode.go，"+
			"或测试文件的 TestMain 里；其它位置请改用 httpserver.ApplyGinMode，"+
			"并在启动期调一次")
}

// ginSetModeOffenders returns "file:line" for every gin.SetMode call in
// path that is NOT inside a TestMain function.
func ginSetModeOffenders(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// 解析不了就无法核实 —— 报出来，别静默放过。
		t.Errorf("parse %s: %v", rel, err)
		return nil
	}
	isTest := strings.HasSuffix(rel, "_test.go")

	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		positions := ginSetModeCalls(fn.Body)
		if len(positions) == 0 {
			continue
		}
		if isTest && fn.Name.Name == "TestMain" {
			continue
		}
		for _, pos := range positions {
			out = append(out, rel+":"+strconv.Itoa(fset.Position(pos).Line))
		}
	}
	return out
}

// ginSetModeCalls returns the position of every `gin.SetMode(...)` call
// expression inside body.
func ginSetModeCalls(body *ast.BlockStmt) []token.Pos {
	var positions []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "gin" || sel.Sel.Name != "SetMode" {
			return true
		}
		positions = append(positions, call.Pos())
		return true
	})
	return positions
}

// repoRoot returns the repository root, derived from this file's location
// (<root>/internal/httpserver/ginmode_test.go).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
