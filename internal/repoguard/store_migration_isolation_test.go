package repoguard

// DATA-09 / structural guard: **跑默认测试套件不得改变共享开发库的迁移状态。**
//
// The defect, measured (2026-10-15, 台账 DATA-05「迁移执行方式的意外」)
// --------------------------------------------------------------------
// `NewPostgresStore`（`pkg/storage/postgres.go:19`）在构造里直接调
// `store.migrate(ctx)`（`:43`），而默认构建（无 build tag）的库侧测试直连
// `localhost:5432/quant_trading` —— **开发库本身**。后果当场实测：一条
// `go test ./pkg/data/...` 就把 Migration 054 的整表 ×1000 真的作用在
// 3,926,510 行历史成交额上，无授权、无提示（`data_migrations` 里
// `054_…` 的 applied_at = 2026-10-15 11:07:45 就是那次测试留下的哨兵）。
//
// 为什么这件事单独严重：052/054 这类 **DML 迁移不可重放**（哨兵置位后重启不补跑），
// 跑错一次只能靠备份表回滚；而本仓确实有多个会话共用同一个库
// （2026-10-16 开工实测：`quant_trading` 里已存在别的会话经测试触发 056
// 建出的 `market_activity` 表）。也就是说「一次普通的 `go test` 改了别人的库」
// 不是假想，是当天的日常。
//
// The rule
// --------
// 两个构造入口，各自只许一侧调用：
//
//   - `NewPostgresStore` —— 连库 **+ 跑 migrate()**。这是「应用迁移」这一动作的
//     唯一载体，只许出现在**启动 / 部署路径**（生产入口）与**显式的 integration
//     套件**（一次性容器，见本文件末 allowlist 理由）。
//   - `NewPostgresStoreConnected` —— 连库 + ping，**不跑迁移、不 seed**。给库侧测试用。
//     它在**生产侧零调用方**（下方第一条护栏），否则「不迁移的入口」就成了绕过
//     迁移门禁的后门：谁都能拿它把 schema 漂移带进服务。
//
// 于是：**默认构建的 `_test.go` 不许调 `NewPostgresStore`**（第二条护栏），
// **非测试代码不许调 `NewPostgresStoreConnected`**（第一条护栏）。
//
// Why a structural guard and not only a behavioural one
// -----------------------------------------------------
// 行为测试能钉住「今天的 15 个调用点不迁移」，钉不住「明天新增的第 16 个调用点
// 又用回迁移入口」—— 而那正是 DATA-09 要堵的复发路径。护栏是静态的：新文件
// 一旦出现违规调用就红，作者必须显式加进 allowlist 并写明理由。
//
// It parses calls via go/ast, not raw text: a text grep cannot tell a call
// apart from a mention in a comment, so a text guard goes red on its own
// documentation (AUD-29; `pkg/storage/dsn_single_impl_test.go` 记录了同一条教训).
// Comments are therefore invisible to it, by construction.
//
// Boundary of this check
// ---------------------
//   - Only **direct** call sites are judged. A test that reaches a migrating
//     constructor through a helper (e.g. `bootstrap.OpenStore`) is not matched;
//     the repo has no such wrapper today (实测：`migrate()` 全仓只有一个调用方，
//     即 `NewPostgresStore` 自身)。The second half of the defence is behavioural
//     and lives in DATA-09 ⑤：拿一条假的 DML 迁移证明测试跑完不落库。
//   - Build-tag detection is textual on the file's leading constraint lines
//     (`//go:build` / `// +build`). A file whose constraint is computed some other
//     way (e.g. filename suffix `_integration_test.go`, which the go tool does
//     **not** treat as a constraint) is treated as default-build — fail closed.
//   - Files that fail to parse are reported as errors, not skipped silently.
//   - The walkers never match themselves: the two constructor names are built
//     from fragments, so the literal spellings do not occur in this file.

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

// 拼出来的符号名 —— 本护栏文件的正文里不许出现完整字面量（含注释），
// 否则遍历会认出自己。
var (
	migratingCtor = "NewPostgres" + "Store"
	noMigrateCtor = "NewPostgres" + "Store" + "Connected"
)

// migratingCtorAllowlist names every non-test caller of the migrating
// constructor: the startup / deploy paths that are *supposed* to apply
// migrations, plus the manual bench script. Adding an entry has to be a
// deliberate act, which is the whole point of listing them.
var migratingCtorAllowlist = map[string]string{
	"cmd/data/setup.go":               "data-service 启动路径：迁移的显式执行点之一",
	"pkg/bootstrap/engine.go":         "analysis-service 装配路径：迁移的显式执行点之一",
	"cmd/dualrun/main.go":             "对拍 CLI：人工显式运行，不是测试副产物",
	"tools/warmcache_bench.go":        "人工 bench 脚本（//go:build ignore），不参与 go build ./...",
	"pkg/storage/integration_test.go": "integration tag：dockertest 一次性容器，构造 migrate() 就是被测对象",
}

// noMigrateCtorTestFiles is kept empty on purpose — every test file may call
// the non-migrating constructor, so there is nothing to allowlist there. The
// guard asserts the opposite direction only (production must not call it).
type ctorCall struct {
	rel       string
	line      int
	selector  string // 解析后的接收者（"" 表示包内裸调用）
	isTest    bool
	hasIntTag bool
}

// TestMigratingStoreConstructorHasNoDefaultBuildTestCallers is the DATA-09
// guard: a test that runs under the default build must not go through the
// constructor that applies migrations.
func TestMigratingStoreConstructorHasNoDefaultBuildTestCallers(t *testing.T) {
	t.Parallel()

	calls, scanned, err := scanStoreConstructorCalls(filepath.Join("..", ".."))
	require.NoError(t, err)

	// 覆盖面自检（照 module_lifecycle 的格式先例）： walker 瞎了的时候「没有违规」
	// 什么也不证明。这两个文件是默认构建下真实连库的测试，必须被看见。
	assert.Contains(t, scanned, "pkg/storage/postgres_test.go",
		"遍历没走到 DATA-09 点名的库侧测试文件上 —— 覆盖面声明是假的")
	assert.Contains(t, scanned, "cmd/data/handlers_ingest_test.go",
		"遍历没走到 cmd/data 的库侧测试文件上 —— 覆盖面声明是假的")

	var violations []string
	for _, c := range calls {
		if c.selector != migratingCtor || !c.isTest || c.hasIntTag {
			continue // 生产侧由 allowlist 单独判；tagged 集成套件是显式迁移路径
		}
		violations = append(violations, fmt.Sprintf(
			"%s:%d 默认构建的测试调了会跑 migrate() 的 %s —— 一次 go test 会真的改共享开发库；改用 %s",
			c.rel, c.line, migratingCtor, noMigrateCtor))
	}
	sort.Strings(violations)
	assert.Empty(t, violations, "DATA-09 违反项：\n%s", strings.Join(violations, "\n"))
}

// TestMigratingStoreConstructorProductionCallersAreNamed pins the other half:
// the migrating constructor exists exactly for the startup / deploy path, so
// every non-test caller must be listed with a reason (and the list must not
// rot — a listed file that no longer calls it is also reported).
func TestMigratingStoreConstructorProductionCallersAreNamed(t *testing.T) {
	t.Parallel()

	calls, _, err := scanStoreConstructorCalls(filepath.Join("..", ".."))
	require.NoError(t, err)

	var unexpected, stale []string
	seen := map[string]bool{}
	for _, c := range calls {
		if c.selector != migratingCtor {
			continue
		}
		// seen 统计**全部**调用方（含 tagged 集成套件），否则 allowlist 里的
		// pkg/storage/integration_test.go 会被误判成死条目。
		seen[c.rel] = true
		if c.isTest && !c.hasIntTag {
			continue // 默认构建的测试由上一条护栏负责，别在这里重复报 14 次
		}
		if _, ok := migratingCtorAllowlist[c.rel]; !ok {
			reason := "（生产入口该走显式迁移路径；若确为新入口，加进 migratingCtorAllowlist 并写明理由）"
			unexpected = append(unexpected, fmt.Sprintf("%s:%d %s", c.rel, c.line, reason))
		}
	}
	for rel := range migratingCtorAllowlist {
		if !seen[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(stale)
	assert.Empty(t, unexpected,
		"未登记的 %s 生产调用方：%v —— 迁移只应由启动/部署路径触发", migratingCtor, unexpected)
	assert.Empty(t, stale,
		"migratingCtorAllowlist 里的文件已经不再调用 %s（%v）—— 收口这条登记，别留死条目",
		migratingCtor, stale)
}

// TestNoMigrateConstructorHasZeroProductionCallers is the anti-backdoor guard
// required by DATA-09 ③(a2): the connection-only entry must never be reachable
// from production code, or it becomes a way to skip migrations inside a
// service.
func TestNoMigrateConstructorHasZeroProductionCallers(t *testing.T) {
	t.Parallel()

	calls, _, err := scanStoreConstructorCalls(filepath.Join("..", ".."))
	require.NoError(t, err)

	// 自检腿：不迁移入口必须真的被默认构建的测试用着。若一个调用方都没抓到，
	// 说明识别逻辑瞎了（改名 / 换了写法），那这条护栏的绿是假的。
	testSites := 0
	var violations []string
	for _, c := range calls {
		if c.selector != noMigrateCtor {
			continue
		}
		if c.isTest {
			testSites++
			continue
		}
		violations = append(violations, fmt.Sprintf(
			"%s:%d 生产代码调了 %s —— 这个入口的意义就是「生产侧永不用它」，"+
				"否则服务可以带着没迁移的 schema 起来", c.rel, c.line, noMigrateCtor))
	}
	sort.Strings(violations)
	assert.Greater(t, testSites, 0,
		"一个 %s 的测试调用方都没识别出来 —— 识别逻辑瞎了，这条护栏的绿不可信", noMigrateCtor)
	assert.Empty(t, violations, "DATA-09 违反项：\n%s", strings.Join(violations, "\n"))
}

// TestStoreConstructorGuardJudgesSnippets is the guard's own counterexample
// leg: feed the scanner the shapes it claims to catch and require it to reject
// them, plus control shapes (including a comment-only mention) it must leave
// alone. Without this leg a scanner that silently matches nothing would report
// a green that means nothing.
func TestStoreConstructorGuardJudgesSnippets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		filename   string
		src        string
		wantCtor   string // "" = 不该有任何调用
		wantTest   bool
		wantIntTag bool
	}{
		{
			name:     "默认构建测试直连迁移入口 —— 本单要抓的形状",
			filename: "x_test.go",
			src: `package p
import "github.com/ruoxizhnya/quant-trading/pkg/storage"
func TestA(t *testing.T) {
	s, _ := storage.NewPostgres` + "Store(ctx, dsn)\n_ = s\n}",
			wantCtor: migratingCtor, wantTest: true, wantIntTag: false,
		},
		{
			name:     "同包裸调用（pkg/storage 自己的测试）",
			filename: "y_test.go",
			src: `package storage
func TestB(t *testing.T) {
	s, _ := NewPostgres` + "Store(ctx, dsn)\n_ = s\n}",
			wantCtor: migratingCtor, wantTest: true, wantIntTag: false,
		},
		{
			name:     "对照腿：不迁移入口放行",
			filename: "z_test.go",
			src: `package p
import "github.com/ruoxizhnya/quant-trading/pkg/storage"
func TestC(t *testing.T) {
	s, _ := storage.NewPostgres` + "StoreConnected(ctx, dsn)\n_ = s\n}",
			wantCtor: noMigrateCtor, wantTest: true, wantIntTag: false,
		},
		{
			name:     "对照腿：只在注释里提到 —— AST 看不见",
			filename: "comment_test.go",
			src: `package p
// 以前这里用 NewPostgres` + "Store，现在不用了\nfunc TestD(t *testing.T) { _ = 1 }",
			wantCtor: "", wantTest: true, wantIntTag: false,
		},
		{
			name:     "对照腿：integration tag 的测试由 tag 判定，不由文件名",
			filename: "tagged_test.go",
			src: `//go:build integration

package storage
func TestE(t *testing.T) {
	s, _ := NewPostgres` + "Store(ctx, dsn)\n_ = s\n}",
			wantCtor: migratingCtor, wantTest: true, wantIntTag: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls, err := ctorCallsInFile(tc.filename, []byte(tc.src))
			require.NoError(t, err)
			if tc.wantCtor == "" {
				assert.Empty(t, calls, "注释里的提及不该被当成调用")
				return
			}
			require.Len(t, calls, 1, "识别逻辑漏了这个形状：%s", tc.src)
			assert.Equal(t, tc.wantCtor, calls[0].selector)
			assert.Equal(t, tc.wantTest, calls[0].isTest)
			assert.Equal(t, tc.wantIntTag, calls[0].hasIntTag)
		})
	}
}

// TestNonMigratingEntryBodyNeverMigrates closes the other direction: the caller
// guards above are worthless if a later edit puts migrate() back into the
// connection-only path (that would make「测试走不迁移入口」a no-op again). So the
// body of NewPostgresStoreConnected / its shared helper must never call
// migrate(), while NewPostgresStore's body must still call it exactly once.
//
// 破坏腿：在 pkg/storage/postgres.go 的 connectPostgresStore 里加一行
// `_ = store.migrate(ctx)` ⇒ 本测试红；把迁移入口里那一句删掉 ⇒ 也红。
func TestNonMigratingEntryBodyNeverMigrates(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "pkg", "storage", "postgres.go"))
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "postgres.go", src, 0)
	require.NoError(t, err, "pkg/storage/postgres.go 解析失败 —— 本护栏判不了形状，别把绿当结论")

	bodies := map[string]int{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		n := 0
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "migrate" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "store" {
				n++
			}
			return true
		})
		bodies[fn.Name.Name] = n
	}

	// 形状判据必须真的看得见这两个入口 —— 改名 / 删除会让下面的绿变成空转。
	_, hasMigrating := bodies[migratingCtor]
	_, hasConnected := bodies[noMigrateCtor]
	require.True(t, hasMigrating, "找不到 %s 的函数体 —— 形状判据已经瞎了", migratingCtor)
	require.True(t, hasConnected, "找不到 %s 的函数体 —— 形状判据已经瞎了", noMigrateCtor)

	assert.Equal(t, 1, bodies[migratingCtor],
		"%s 必须恰好调一次 migrate()（迁移的显式载体）；实测 %d 次",
		migratingCtor, bodies[migratingCtor])
	assert.Equal(t, 0, bodies[noMigrateCtor],
		"%s 不得调用 migrate() —— 它是库侧测试的入口，跑迁移就等于改共享开发库；实测 %d 次",
		noMigrateCtor, bodies[noMigrateCtor])
	// 两个公开入口共用 connectPostgresStore；连接逻辑里出现迁移，等于任何一侧都会迁移。
	assert.Equal(t, 0, bodies["connectPostgresStore"],
		"connectPostgresStore（两个入口共用的连接段）里不得有 migrate() —— 否则不迁移入口名不副实；实测 %d 次",
		bodies["connectPostgresStore"])
}

// scanStoreConstructorCalls walks the repo and returns every call site of the
// two store constructors, plus the set of scanned .go files (slash-separated,
// repo-relative) so callers can prove their own coverage.
func scanStoreConstructorCalls(root string) ([]ctorCall, map[string]bool, error) {
	var calls []ctorCall
	scanned := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", "node_modules", "vendor", "dist", ".vite":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		scanned[rel] = true

		found, err := ctorCallsInFile(path, raw)
		if err != nil {
			return err
		}
		for _, c := range found {
			c.rel = rel
			calls = append(calls, c)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return calls, scanned, nil
}

// ctorCallsInFile parses one file and reports the two constructors' call sites.
// Parse failures are returned as errors (fail closed) rather than skipped.
func ctorCallsInFile(path string, src []byte) ([]ctorCall, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	isTest := strings.HasSuffix(filepath.Base(path), "_test.go")
	hasIntTag := fileHasIntegrationBuildTag(file)

	var out []ctorCall
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var sel string
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fn.X.(*ast.Ident); ok && id.Name == "storage" {
				sel = fn.Sel.Name
			} else {
				return true
			}
		case *ast.Ident:
			sel = fn.Name
		default:
			return true
		}
		if sel != migratingCtor && sel != noMigrateCtor {
			return true
		}
		out = append(out, ctorCall{
			rel:       filepath.Base(path),
			line:      fset.Position(call.Pos()).Line,
			selector:  sel,
			isTest:    isTest,
			hasIntTag: hasIntTag,
		})
		return true
	})
	return out, nil
}

// fileHasIntegrationBuildTag reports whether a file is restricted to the
// integration build tag. Constraints live before the package clause; the go
// tool accepts both the modern //go:build line and the legacy // +build line.
func fileHasIntegrationBuildTag(file *ast.File) bool {
	for _, cg := range file.Comments {
		if cg.Pos() > file.Package {
			break // 包声明之后的注释不是构建约束
		}
		for _, c := range cg.List {
			text := strings.TrimSpace(c.Text)
			if !strings.HasPrefix(text, "//go:build") && !strings.HasPrefix(text, "// +build") {
				continue
			}
			fields := strings.Fields(text)
			for _, f := range fields {
				if f == "integration" {
					return true
				}
			}
		}
	}
	return false
}
