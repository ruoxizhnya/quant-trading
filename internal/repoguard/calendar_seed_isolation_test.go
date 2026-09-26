package repoguard

// AUD-62 / structural guard: **tests must not seed `trading_calendar` with a
// date that real data already occupies.**
//
// Why this exists (the defect, measured)
// --------------------------------------
// `trading_calendar` is keyed by `trade_date` **alone** (`trade_date DATE
// PRIMARY KEY`, `exchange VARCHAR(10)` is just a column). A test that writes
//
//	&storage.TradingCalendarEntry{Exchange: "TESTEX", TradeDate: <a real date>}
//
// therefore writes **the real row** — the UPSERT rewrites its `exchange` and
// `is_trading_day` — and the matching cleanup
//
//	DELETE FROM trading_calendar WHERE exchange='TESTEX' AND trade_date=<that date>
//
// deletes **the real row**. The `exchange` value looks like an isolation
// dimension and is not one. There is no way to isolate by exchange on this
// table; the only isolation available is the date itself.
//
// Measured cost (2026-09-26, default build): after `go test ./...` the live
// `quant_trading` database had lost 4 rows — 2024-12-31, 2025-01-01,
// 2025-01-02, 2025-01-03. Three of those are real trading days (the OHLCV
// table holds ~5,370 symbols for each of them), so a backtest spanning the
// 2024/2025 year boundary silently skipped three sessions. The two culprits
// were `TestSaveTradingCalendarEntry_and_GetTradingCalendar` (TESTEX @
// 2024-12-31) and `TestSaveTradingCalendarBatch` (TESTEX2 @ 2025-01-01..03)
// in pkg/storage/postgres_test.go — both of which connect to the **live** DSN
// (`postgres://…@localhost:5432/quant_trading`).
//
// Why a structural guard and not just those two fixes: the same file already
// contained `seedIsolatedCalendar`, whose comment explains this exact trap and
// which fixes it by using 1990 dates. The knowledge existed in one function and
// was violated two functions away. A one-off fix would leave the next author to
// rediscover the trap.
//
// Why CI never caught it: CI's postgres is an empty database. Deleting a row
// that does not exist reports no error, so the tests are green everywhere the
// data they destroy does not exist.
//
// The rule
// --------
// In any `*_test.go`, a date that
//
//	(a) initialises the `TradeDate` field of a `TradingCalendarEntry`
//	    composite literal (including the elided `{…}` elements of a
//	    `[]*TradingCalendarEntry{…}` literal — that is the shape this repo
//	    actually writes), or
//	(b) appears inside a string literal that mentions `trading_calendar`
//	    (i.e. SQL text),
//
// must have a year <= guardIsolatedMaxYear.
//
// Expressions that cannot be resolved statically **fail closed**: an author who
// cannot say the date outright must be pushed to `parseDate("1990-01-02")` or a
// string constant holding it, not waved through.
//
// Boundary of this check: it is a static approximation. It cannot see a date
// assembled at runtime (`fmt.Sprintf("…'%s'…", d)`), and it does not care about
// any other table — the defect is specific to a table whose primary key is a
// column that real data also uses. The runtime half of the defence lives in
// pkg/storage (`assertIsolatedCalendarDate`), which is called at the seed site.
//
// Regression risk this guard carries: if the isolated epoch is ever raised
// toward real data, the guard is silently switched off. `guardIsolatedMaxYear <
// 2000` is asserted below for that reason.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// calendarEntryTypeName is the struct whose TradeDate is dangerous.
	calendarEntryTypeName = "TradingCalendarEntry"

	// guardIsolatedMaxYear is the newest year a test may seed. It deliberately
	// does not import pkg/storage's constant: a guard that reads its threshold
	// from the code under guard changes whenever that code changes. The
	// duplication is the point, and the two are pinned to each other by
	// TestCalendarSeedGuardSeesTheTree plus the repo-wide scan.
	guardIsolatedMaxYear = 1991

	// runtimeGateName is pkg/storage's runtime half of the defence
	// (`assertIsolatedCalendarDate`). The static scanner sees through it: the
	// wrapper validates at runtime, so its presence must not blind the static
	// check, and it must not become a way to smuggle a real date past it.
	runtimeGateName = "assertIsolatedCalendarDate"

	// guardFileName is skipped so this file's own prose/snippets — which quote
	// realistic dates on purpose — do not flag themselves.
	guardFileName = "calendar_seed_isolation_test.go"
)

// calendarSeedAllowlist lists files that seed calendar entries but provably
// cannot reach real data, each with the reason. Modelled on
// package_wiring_test.go's unwiredPackages: the entry is a claim that has to
// stay true, and TestCalendarSeedGuardSeesTheTree fails if the file disappears
// (so a stale entry cannot sit here silently widening the hole).
var calendarSeedAllowlist = map[string]string{
	"cmd/data/sync_handlers_test.go":  "in-memory only — dailyCalendar() builds entries that are fed to validateCalendarCoverage(); no store, no DSN, no DB call anywhere in the file",
	"pkg/storage/integration_test.go": "runs against its own ephemeral dockertest container (TestMain boots it, setUp() TRUNCATEs every table per test), so its time.Date(2024, 1, 1) values cannot reach the developer's live database",
}

var (
	// exactDateRe matches a whole value that is a date.
	exactDateRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	// sqlDateRe finds dates inside a SQL string.
	sqlDateRe = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
)

// calendarSeedViolation is one offending site.
type calendarSeedViolation struct {
	where string // file:line
	why   string
}

// TestCalendarSeedsInTestsStayInTheIsolatedEpoch is the guard.
func TestCalendarSeedsInTestsStayInTheIsolatedEpoch(t *testing.T) {
	t.Parallel()

	violations, _, _, err := scanCalendarSeeds(filepath.Join("..", ".."))
	require.NoError(t, err)

	msgs := make([]string, 0, len(violations))
	for _, v := range violations {
		msgs = append(msgs, v.where+" → "+v.why)
	}
	sort.Strings(msgs)

	assert.Empty(t, msgs,
		"这些测试用「真实感」日期去灌 trading_calendar —— 该表的主键只有 trade_date，"+
			"exchange 隔离不了测试数据，写进来就覆盖真实那一行、清理就把它删掉（AUD-62）。\n"+
			"改成隔离纪元的日期（<= %d 年），或把文件加进 calendarSeedAllowlist 并写明为什么碰不到真库：\n%s",
		guardIsolatedMaxYear, strings.Join(msgs, "\n"))
}

// TestCalendarSeedGuardSeesTheTree makes the guard above non-blind. Without
// this, a walker that sees nothing (wrong root, renamed table, broken
// extraction) reports "no violations" and the green is worthless — the exact
// failure mode PITFALLS §29/§54 describe and the recovery-wiring guard
// documents.
func TestCalendarSeedGuardSeesTheTree(t *testing.T) {
	t.Parallel()

	scanned, seeds, err := scanCalendarSeedsSummary(filepath.Join("..", ".."))
	require.NoError(t, err)

	assert.Contains(t, scanned, "pkg/storage/postgres_test.go",
		"遍历没有走到真正会被检查的文件上 —— 那么「没有违规」什么也没证明")
	assert.Greater(t, seeds, 0,
		"一个日历自灌点都没识别出来 —— 识别逻辑瞎了，上面那条护栏的绿是假的")

	for rel, why := range calendarSeedAllowlist {
		assert.Contains(t, scanned, rel,
			"白名单里的 %s 已不存在或改了路径 —— 删掉这条，否则它只是掩盖问题的空条目", rel)
		assert.NotEmpty(t, why, "白名单 %s 必须写明理由", rel)
	}

	assert.Less(t, guardIsolatedMaxYear, 2000,
		"隔离纪元的年份上限必须远早于任何真实行情数据；把它调大等于把这条护栏关掉")
}

// TestCalendarSeedGuardRejectsRealisticDates is the guard's own counterexample
// leg: the scanner is fed the exact shapes it claims to catch and must reject
// them, plus two control shapes it must leave alone. The repo-wide test above
// only ever proves "scanned a lot, found nothing wrong" — this one proves the
// scanner actually judges.
func TestCalendarSeedGuardRejectsRealisticDates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "真实日期字面量（本仓原来就是这么写的）",
			src: `package p
import "time"
func f() { _ = &TradingCalendarEntry{Exchange: "TESTEX", TradeDate: parseDate("2024-12-31"), IsTradingDay: false} }`,
			want: true,
		},
		{
			name: "切片省略元素类型 —— 内层字面量 Type 是 nil",
			src: `package p
func f() { _ = []*TradingCalendarEntry{
	{Exchange: "TESTEX2", TradeDate: parseDate("2025-01-02"), IsTradingDay: true},
} }`,
			want: true,
		},
		{
			name: "跨包选择器类型",
			src: `package p
func f() { _ = storage.TradingCalendarEntry{TradeDate: parseDate("2026-05-05")} }`,
			want: true,
		},
		{
			name: "含 trading_calendar 的 SQL 文本",
			src: `package p
func f(s S, ctx C) { s.DB().Exec(ctx, "DELETE FROM trading_calendar WHERE trade_date='2024-12-31'") }`,
			want: true,
		},
		{
			name: "无法静态求值 → fail closed",
			src: `package p
func f(d T) { _ = TradingCalendarEntry{TradeDate: d} }`,
			want: true,
		},
		{
			name: "常量但其值是真日期 → 必须解出常量值再判",
			src: `package p
func f() {
	const seedDate = "2025-01-03"
	_ = TradingCalendarEntry{TradeDate: parseDate(seedDate)}
}`,
			want: true,
		},
		{
			name: "反证腿：套上运行时闸门也不能绕过静态检查",
			src: `package p
func f(t T) { _ = TradingCalendarEntry{TradeDate: assertIsolatedCalendarDate(t, parseDate("2024-12-31"))} }`,
			want: true,
		},
		{
			name: "对照腿：隔离纪元 → 放行",
			src: `package p
func f() { _ = TradingCalendarEntry{Exchange: "TESTEX_IS", TradeDate: parseDate("1990-01-02"), IsTradingDay: true} }`,
			want: false,
		},
		{
			name: "对照腿：隔离纪元的 SQL → 放行",
			src: `package p
func f(s S, ctx C) { s.DB().Exec(ctx, "DELETE FROM trading_calendar WHERE trade_date IN ('1990-02-01','1990-02-02')") }`,
			want: false,
		},
		{
			name: "对照腿：别的结构体也有 TradeDate 字段 → 不误报",
			src: `package p
func f() { _ = domain.FundamentalData{TsCode: "TEST_FD_001.SH", TradeDate: parseDate("2024-09-30")} }`,
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet_test.go", tc.src, 0)
			require.NoError(t, err)

			violations, _ := inspectCalendarSeeds(fset, "snippet_test.go", f)
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

// scanCalendarSeedsSummary returns (files inspected, seed sites identified)
// without the violation detail, for the anti-blindness assertions.
func scanCalendarSeedsSummary(root string) ([]string, int, error) {
	violations, scanned, seeds, err := scanCalendarSeeds(root)
	_ = violations
	return scanned, seeds, err
}

// scanCalendarSeeds walks every *_test.go under root (skipping the same
// directories the other repoguard walkers skip) and returns the violations, the
// files inspected, and how many calendar seed sites were identified.
func scanCalendarSeeds(root string) ([]calendarSeedViolation, []string, int, error) {
	var (
		violations []calendarSeedViolation
		scanned    []string
		seeds      int
	)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", "node_modules", "web", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") || d.Name() == guardFileName {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		// Allowlisted files are still counted as inspected: the anti-blindness
		// test asserts they were reached.
		scanned = append(scanned, rel)
		if _, ok := calendarSeedAllowlist[rel]; ok {
			return nil
		}

		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, raw, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}

		vs, n := inspectCalendarSeeds(fset, rel, f)
		violations = append(violations, vs...)
		seeds += n
		return nil
	})
	if err != nil {
		return nil, nil, 0, err
	}
	sort.Strings(scanned)
	return violations, scanned, seeds, nil
}

// inspectCalendarSeeds inspects one parsed file.
func inspectCalendarSeeds(fset *token.FileSet, rel string, f *ast.File) ([]calendarSeedViolation, int) {
	consts := collectStringConsts(f)

	var (
		violations []calendarSeedViolation
		seeds      int
	)

	// ctxStack carries "is the enclosing composite literal a TradingCalendarEntry
	// (or a collection of them)?" for every open node.
	//
	// It has to be a stack rather than a per-literal type check because Go lets
	// you elide the element type: in `[]*TradingCalendarEntry{{TradeDate: x}}`
	// the inner literal's Type is nil, so looking only at each literal's own
	// type misses every entry of the shape this repo actually writes.
	var ctxStack []bool

	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			if len(ctxStack) > 0 {
				ctxStack = ctxStack[:len(ctxStack)-1]
			}
			return true
		}

		parentIsCalendar := len(ctxStack) > 0 && ctxStack[len(ctxStack)-1]

		isCalendar := false
		if lit, ok := n.(*ast.CompositeLit); ok {
			isCalendar = parentIsCalendar || isCalendarEntryType(lit.Type)
		}

		if kv, ok := n.(*ast.KeyValueExpr); ok && parentIsCalendar {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "TradeDate" {
				seeds++
				if why := checkDateExpr(kv.Value, consts); why != "" {
					violations = append(violations, calendarSeedViolation{
						where: fmt.Sprintf("%s:%d", rel, fset.Position(kv.Value.Pos()).Line),
						why:   why,
					})
				}
			}
		}

		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if val, uerr := strconv.Unquote(lit.Value); uerr == nil &&
				strings.Contains(strings.ToLower(val), "trading_calendar") {
				line := fset.Position(lit.Pos()).Line
				for _, m := range sqlDateRe.FindAllStringSubmatch(val, -1) {
					seeds++
					if y, cerr := strconv.Atoi(m[1]); cerr == nil && y > guardIsolatedMaxYear {
						violations = append(violations, calendarSeedViolation{
							where: fmt.Sprintf("%s:%d", rel, line),
							why: fmt.Sprintf(
								"含 trading_calendar 的 SQL 文本里出现真实感日期 %s —— 清理语句删的是真实那一行（年份需 <= %d）",
								m[0], guardIsolatedMaxYear),
						})
					}
				}
			}
		}

		ctxStack = append(ctxStack, isCalendar)
		return true
	})

	return violations, seeds
}

// isCalendarEntryType unwraps `[]`, `*` and parentheses and reports whether the
// base type is TradingCalendarEntry (same package or qualified).
func isCalendarEntryType(t ast.Expr) bool {
	for {
		switch e := t.(type) {
		case *ast.ArrayType:
			t = e.Elt
		case *ast.StarExpr:
			t = e.X
		case *ast.ParenExpr:
			t = e.X
		case *ast.Ident:
			return e.Name == calendarEntryTypeName
		case *ast.SelectorExpr:
			return e.Sel.Name == calendarEntryTypeName
		default:
			return false
		}
	}
}

// collectStringConsts maps every `name = "literal"` declaration in the file
// (top level or inside a function body) to its value, so `parseDate(seedDate)`
// can be resolved. Unresolvable identifiers fail closed in checkDateExpr.
func collectStringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return true
		}
		bl, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil {
			return true
		}
		out[spec.Names[0].Name] = v
		return true
	})
	return out
}

// checkDateExpr returns "" when the expression is an isolated-epoch date, or the
// reason to reject it. Shapes other than a date literal (optionally behind a
// string constant) are rejected on purpose — fail closed.
func checkDateExpr(expr ast.Expr, consts map[string]string) string {
	lit := ""

	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			lit, _ = strconv.Unquote(e.Value)
		}
	case *ast.CallExpr:
		id, ok := e.Fun.(*ast.Ident)
		if !ok {
			return "TradeDate 的表达式无法静态求值（fail closed）—— 请写成 parseDate(\"1990-01-02\")"
		}
		// runtimeGateName 是可穿透的包装：它自己会拒真实日期（pkg/storage 的
		// 运行时闸门），但里面那一层仍必须过静态检查 —— 否则写一层包装就等于
		// 绕开这条护栏。TestCalendarSeedGuardRejectsRealisticDates 里有专门一条
		// 反证腿钉住「包装不能被当绕过通道」。
		if id.Name == runtimeGateName && len(e.Args) == 2 {
			return checkDateExpr(e.Args[1], consts)
		}
		if id.Name != "parseDate" || len(e.Args) != 1 {
			return "TradeDate 的表达式无法静态求值（fail closed）—— 请写成 parseDate(\"1990-01-02\")"
		}
		switch a := e.Args[0].(type) {
		case *ast.BasicLit:
			if a.Kind == token.STRING {
				lit, _ = strconv.Unquote(a.Value)
			}
		case *ast.Ident:
			v, ok := consts[a.Name]
			if !ok {
				return fmt.Sprintf(
					"TradeDate 用了 parseDate(%s)，但 %s 不是本文件的字符串常量 —— 静态判不定（fail closed）；"+
						"请内联 parseDate(\"1990-01-02\") 或 `const %s = \"1990-01-02\"`", a.Name, a.Name, a.Name)
			}
			lit = v
		default:
			return "TradeDate 的 parseDate 参数不是字符串字面量/常量 —— 静态判不定（fail closed）"
		}
	}

	if lit == "" {
		return "TradeDate 的表达式无法静态求值（fail closed）—— 请写成 parseDate(\"1990-01-02\") 形式的隔离纪元日期"
	}
	m := exactDateRe.FindStringSubmatch(lit)
	if m == nil {
		return fmt.Sprintf("TradeDate 的值 %q 不是 YYYY-MM-DD 日期 —— 静态判不定（fail closed）", lit)
	}
	y, err := strconv.Atoi(m[1])
	if err != nil {
		return fmt.Sprintf("TradeDate 的值 %q 年份无法解析（fail closed）", lit)
	}
	if y > guardIsolatedMaxYear {
		return fmt.Sprintf(
			"TradeDate = %s 落在真实数据区间内（年份需 <= %d）—— 这张表的主键只有 trade_date，"+
				"exchange 隔离不了测试数据，写进来就覆盖真实那一行、清理就删掉它", lit, guardIsolatedMaxYear)
	}
	return ""
}
