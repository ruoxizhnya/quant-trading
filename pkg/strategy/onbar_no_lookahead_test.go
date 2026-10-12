package strategy_test

// K11 护栏 ③ —— BarHandler.OnBar 不得回看未来 bar。
//
// 被钉住的不变量（pkg/strategy/streaming.go:52，原本只有一句注释）：
//
//	OnBar 逐 bar 回调，&mut self 语义。每根 bar 只回调一次，按时间升序；
//	不得在回调内回看未来 bar（前视 = 回测结果失真，fail-loud）。
//
// 为什么必须机器验证：前视是**唯一一种会把回测收益改好**的缺陷——结果更漂亮、
// 日志干净、单测也不会红（单元测试喂的 bar 序列是自己造的，拿没拿未来数据看不
// 出来）。实盘上线时同一个策略突然不赚钱，而回测报告还摆在那儿讲另一个故事。
// 它与 K8 的 SignalProvider 防前视契约同族：未来数据**物理不可见**才是契约。
//
// ─── 作用域（scanInside 语义，防注释误报）───────────────────────────
// 只判 pkg/strategy 根目录下名为 OnBar 的**方法体**（方法的 receiver 存在即为
// BarHandler 实现）。不扫原文：本包里「未来 bar / 前视 / lookahead」这些词在注
// 释里到处都是（本文件自己也写），字符串扫描会把讲清楚问题的注释判成缺陷。
// 代价是静态近似的边界——下面写在各自的条款里。
//
// 四条判定（对 OnBar 函数体）：
//
//	A. 正偏移索引：禁止 `X[i+1]` / `X[n+1:]`。未来 bar 只能靠**正**偏移拿到
//	   （负偏移 = 回看历史，是合法的递推输入），所以正偏移索引是这个错误唯一
//	   的语法指纹。边界：认不出「先算好 idx 再取未来」的多语句写法。
//	B. 未来语义取数：禁止调用名字属于 lookaheadFutureNames 的方法/函数
//	   （next / peek / future…）。边界：只看名字，同名未见得真是前视；
//	   `rejectFutureSignals` 这类**反前视**的名字刻意不在名单里（精确匹配，
//	   不做子串包含）。
//	C. 遍历 bar 序列：禁止 `for … range` 一个名字含 bar/history/window 等序列
//	   容器的表达式——一次回调只应处理当前这一根。
//	D. 不得改写当前 bar：给 bar 形参或其字段赋值等于伪造当前时刻。
//
// 是不是永真，由 TestOnBarLookaheadGuardIsNotVacuous 的合成源码腿证明（正反例
// 都有），不靠「扫完了没找到问题」这种假绿。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// lookaheadGuardFile 是护栏自己，全文都在谈前视，不参与扫描。
	lookaheadGuardFile = "onbar_no_lookahead_test.go"

	// lookaheadCheckName 是被约的方法名。
	lookaheadCheckName = "OnBar"
)

// lookaheadFutureNames 是「往前看向未来」的取数名。精确匹配（不是子串）：
// 本包已经有一个 `rejectFutureSignals`——它是**反**前视的守卫，子串匹配会把它
// 判成前视，那护栏就变成负资产。
//
// 入榜标准是「名字自带往后语义」：Next / Peek / Future / Lookahead / BarsAfter。
// `BarAt(i)`、`BarsUntil(t)` 一类**不含**方向信息的名字刻意不收——它取历史第 i
// 根与取未来第 i 根是同一个签名，判它违规只是把合法的历史递推读法赶出代码，
// 那是纯负收益（护栏假红一次，信誉掉一次）。
var lookaheadFutureNames = map[string]bool{
	"Next": true, "NextBar": true, "NextBars": true,
	"Peek": true, "PeekBar": true,
	"Future": true, "FutureBar": true, "FutureBars": true,
	"Lookahead": true, "LookAhead": true, "LookAheadBar": true,
	"BarsAfter": true,
}

// lookaheadSeriesWords 是「bar 序列容器」的名字关键词（小写包含匹配）。
// OnBar 一次只该拿到一根 bar，函数体内还在遍历某个序列本身就是信号。
var lookaheadSeriesWords = []string{"bars", "bar", "history", "histories", "series", "window", "windows"}

// onBarSite 是一个被判定过的 OnBar 方法体。
type onBarSite struct {
	file     string
	recv     string
	line     int
	problems []string
}

func (s onBarSite) where() string { return fmt.Sprintf("%s:%d %s.OnBar", s.file, s.line, s.recv) }

// ─── 护栏本体 ──────────────────────────────────────────────────────

// TestOnBarHandlersNeverLookAhead 是护栏：包内所有 OnBar 实现体都不得出现前视
// 形状。
func TestOnBarHandlersNeverLookAhead(t *testing.T) {
	t.Parallel()

	sites, err := scanOnBarBodies(".")
	require.NoError(t, err)

	var msgs []string
	for _, s := range sites {
		msgs = append(msgs, s.problems...)
	}
	sort.Strings(msgs)

	assert.Empty(t, msgs,
		"BarHandler.OnBar 不得回看未来 bar（streaming.go:52）。前视是唯一一种**把回测收益改好**的缺陷"+
			"——更漂亮的报告、干净的日志、绿灯的单测，代价在实盘付：\n%s",
		strings.Join(msgs, "\n"))
}

// TestOnBarLookaheadGuardSeesTheTree 是反瞎腿：务必拦住「一个 OnBar 都没扫到」
// 和「已知实现悄悄改名/搬家」这两种假绿。
func TestOnBarLookaheadGuardSeesTheTree(t *testing.T) {
	t.Parallel()

	sites, err := scanOnBarBodies(".")
	require.NoError(t, err)

	byFile := map[string]string{}
	files := make([]string, 0, len(sites))
	for _, s := range sites {
		files = append(files, s.file)
		byFile[s.file] = s.recv
	}

	// 这三个是 HEAD 上 pkg/strategy 的全部 BarHandler 实现。少了任何一个，
	// 说明改名/搬家/换接口了——上一条的绿不再代表「它们合规」。
	for file, recv := range map[string]string{
		"runtime.go":         "*BatchAdapter",
		"signal_strategy.go": "*SignalStrategy",
		"streaming.go":       "*StreamingStrategy",
	} {
		assert.Contains(t, files, file,
			"扫描没走到 %s —— 那里的 OnBar 实现正是本护栏的对象，没走到就等于没扫", file)
		assert.Equal(t, recv, byFile[file],
			"%s 上的 OnBar 接收者变了：护栏是按文件+接收者钉住这些实现的，请同步更新本断言", file)
	}
}

// TestOnBarLookaheadGuardIsNotVacuous 是反证腿：把判据喂它声称能抓的形状，必须
// 报出**具体**的条款；对照组（含「注释里大谈未来 bar 但代码干净」——正是防注释
// 误报要的那一刀）必须放行。
func TestOnBarLookaheadGuardIsNotVacuous(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		src     string
		wantWhy string // 非空 = 期望被判红，且问题文本必须包含这一段
	}{
		{
			name: "正偏移索引 = 偷看下一根 bar",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	s.n++
	if len(s.closes) > 1 {
		s.next = s.closes[s.n+1]
	}
	return nil
}`,
			wantWhy: "正偏移索引",
		},
		{
			name: "切片也是同一种取法",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	s.window = append(s.window, bar)
	tail := s.window[s.n+1:]
	_ = tail
	return nil
}`,
			wantWhy: "正偏移切片",
		},
		{
			name: "调用未来语义取数 API",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	nb, ok := s.feed.Next()
	if ok {
		s.pending = append(s.pending, toSignal(nb))
	}
	return nil
}`,
			wantWhy: "未来语义",
		},
		{
			name: "在回调里遍历 bar 序列",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	for _, b := range s.bars {
		s.sum += b.Close
	}
	return nil
}`,
			wantWhy: "遍历",
		},
		{
			name: "改写当前 bar 的字段 = 伪造当前时刻",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	bar.Close = bar.Close * 1.01
	s.sum += bar.Close
	return nil
}`,
			wantWhy: "改写",
		},
		{
			name: "对照：注释大谈未来 bar、代码干净 → 放行（防注释误报）",
			src: `package p
// OnBar 不得回看未来 bar —— 任何 future / next / lookahead 取数都会让回测失真。
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	// 只看历史：最后一根之前的数据
	if len(s.closes) == 0 {
		s.closes = append(s.closes, bar.Close)
		return nil
	}
	prev := s.closes[len(s.closes)-1]
	s.signal = bar.Close > prev
	s.closes = append(s.closes, bar.Close)
	return nil
}`,
		},
		{
			name: "对照：Range 的不是 bar 序列 → 放行",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	sigs, err := s.store.ListFor(ctx, s.modelID, bar.Symbol, DayOf(bar.Date))
	if err != nil {
		return err
	}
	if err := rejectFutureSignals(sigs, DayOf(bar.Date)); err != nil {
		return err
	}
	s.pending = append(s.pending, s.toSignal(sigs[0], DayOf(bar.Date)))
	return nil
}`,
		},
		{
			name: "对照：range 的不是 bar 序列 → 放行",
			src: `package p
func (s *strat) OnBar(ctx context.Context, bar domain.OHLCV) error {
	for _, sig := range s.pending {
		s.out = append(s.out, sig)
	}
	return nil
}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			problems := inspectOnBarSnippet(t, tc.src)

			if tc.wantWhy != "" {
				require.NotEmpty(t, problems, "该判红却判绿 —— 判据对这个形状是瞎的")
				assert.Contains(t, strings.Join(problems, "\n"), tc.wantWhy,
					"判红了，但没说清是为什么：%v", problems)
			} else {
				assert.Empty(t, problems, "不该判红却判红了：%v", problems)
			}
		})
	}
}

// ─── 扫描器 ────────────────────────────────────────────────────────

// scanOnBarBodies 扫 dir（不递归：一个 Go 包 = 一个目录）下的非测试 .go 文件，
// 返回每个 OnBar 方法体的判定结果。测试文件不参与：本包有一批 fake BarHandler
// （rtTraceHandler / ckptFakeHandler / pvStream…）——它们是**测试自己的装置**，
// 不是本护栏要约的策略实现；把它们纳入只会在写测试时误伤自己的装置。
func scanOnBarBodies(dir string) ([]onBarSite, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var sites []onBarSite
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == lookaheadGuardFile {
			continue
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", name, perr)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != lookaheadCheckName {
				continue
			}
			sites = append(sites, onBarSite{
				file:     name,
				recv:     recvTypeNameStrategy(fn),
				line:     fset.Position(fn.Pos()).Line,
				problems: inspectOnBarBody(fset, fn),
			})
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].file < sites[j].file })
	return sites, nil
}

// inspectOnBarBody 判定一个 OnBar 方法体，返回问题列表。
func inspectOnBarBody(fset *token.FileSet, fn *ast.FuncDecl) []string {
	if fn.Body == nil {
		return []string{recvTypeNameStrategy(fn) + ".OnBar 没有函数体 —— 静态判不定（fail closed）"}
	}
	barParam := onBarParamName(fn)
	var problems []string
	seen := map[string]bool{}

	add := func(pos token.Pos, why string) {
		key := fmt.Sprintf("%d:%s", fset.Position(pos).Line, why)
		if seen[key] {
			return
		}
		seen[key] = true
		problems = append(problems, fmt.Sprintf("%s:%d OnBar %s", recvTypeNameStrategy(fn),
			fset.Position(pos).Line, why))
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.IndexExpr:
			if be, ok := e.Index.(*ast.BinaryExpr); ok && be.Op == token.ADD {
				add(be.Pos(), "出现正偏移索引 X[i+k] —— 未来 bar 只能靠正偏移拿到，这是前视唯一的语法指纹")
			}
		case *ast.SliceExpr:
			for _, idx := range []ast.Expr{e.Low, e.Max, e.High} {
				if be, ok := idx.(*ast.BinaryExpr); ok && be.Op == token.ADD {
					add(be.Pos(), "出现正偏移切片 X[i+k:] —— 同上，是一次跨越当前 bar 的读取")
				}
			}
		case *ast.RangeStmt:
			if names := mentionsSeries(e.X); names != "" {
				add(e.X.Pos(), fmt.Sprintf("在函数体内遍历 %s —— 一次回调只该处理当前这一根 bar，遍历序列意味着读到了尚未回调的 bar", names))
			}
		case *ast.SelectorExpr:
			if lookaheadFutureNames[e.Sel.Name] {
				add(e.Pos(), fmt.Sprintf("引用 %s —— 未来语义取数出现在 OnBar 里，前视即回测失真", e.Sel.Name))
			}
		case *ast.Ident:
			if lookaheadFutureNames[e.Name] {
				add(e.Pos(), fmt.Sprintf("引用 %s —— 未来语义取数出现在 OnBar 里，前视即回测失真", e.Name))
			}
		case *ast.AssignStmt:
			if barParam != "" {
				for _, lhs := range e.Lhs {
					if baseIdentName(lhs) == barParam {
						add(lhs.Pos(), "给 bar 形参赋值 —— 改写当前 bar 等于伪造当前时刻")
					}
				}
			}
		}
		return true
	})

	sort.Strings(problems)
	return problems
}

// onBarParamName 取 OnBar 第二个形参（当前 bar）的名字；匿名则空。
func onBarParamName(fn *ast.FuncDecl) string {
	if fn.Type.Params == nil || len(fn.Type.Params.List) < 2 {
		return ""
	}
	names := fn.Type.Params.List[1].Names
	if len(names) == 0 {
		return ""
	}
	return names[0].Name
}

// mentionsSeries 返回表达式里第一个「像 bar 序列容器」的名字，没有则空。
func mentionsSeries(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	hit := ""
	ast.Inspect(expr, func(n ast.Node) bool {
		if hit != "" || n == nil {
			return hit == ""
		}
		var name string
		switch e := n.(type) {
		case *ast.Ident:
			name = e.Name
		case *ast.SelectorExpr:
			name = e.Sel.Name
		}
		if name != "" {
			lower := strings.ToLower(name)
			for _, w := range lookaheadSeriesWords {
				if strings.Contains(lower, w) {
					hit = name
					return false
				}
			}
		}
		return true
	})
	return hit
}

// baseIdentName 一路剥到最左边的标识符名：`bar.Close` → `bar`。
func baseIdentName(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return ""
		}
	}
}

func recvTypeNameStrategy(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		return "*" + identNameStrategy(star.X)
	}
	return identNameStrategy(t)
}

func identNameStrategy(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}

// inspectOnBarSnippet 把一段合成源码里的 OnBar 方法体喂给判据。
func inspectOnBarSnippet(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", src, 0)
	require.NoError(t, err)

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != lookaheadCheckName {
			continue
		}
		return inspectOnBarBody(fset, fn)
	}
	t.Fatalf("合成源码里没有找到 OnBar 方法 —— fixture 写错了")
	return nil
}
