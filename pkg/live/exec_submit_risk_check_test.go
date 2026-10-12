package live_test

// K11 护栏 ① —— ExecEngine.Submit 必须先过 risk.RiskEngine.CheckOrder。
//
// 被钉住的不变量（pkg/live/interfaces.go:87，原本只有一行注释）：
//
//	Submit：报送订单。**必须**先过风控挂点（risk.RiskEngine.CheckOrder）；
//	被拒则发 msgbus.TopicExecOrderRejected 并返回 error，**不得**把被拒订单
//	送给 broker。
//
// 为什么要机器验证：注释约束只能靠 review 发现破坏，而这条被破坏的症状最难
// 查——订单绕过风控直达 broker，回测里表现为收益更好看（该减的仓没减），实盘
// 里是真实的资金敞口；两处都不报一个错。
//
// ─── 作用域（scanInside 语义，防注释误报）───────────────────────────
// 只判 pkg/live 根目录（包的目录边界）下、签名与 ExecEngine.Submit 一致的方
// 法**体**（契约签名见 interfaces.go:99）。不扫原文：一是任意注释里都写着
// "CheckOrder"（本文件自己也写），字符串扫描会被自己骗绿；二是只有 AST 能判
// 出「调用了但没把裁决接住」这种绕过。
//
// 三条判定（针对非 stub 的实现体）：
//
//	A. 存在性：必须有 CheckOrder 调用；
//	B. 先后  ：首个 CheckOrder 必须早于首个 broker 出口（SubmitOrder /
//	           Broker().…）——「先报送后补检」同样是不变量被破；
//	C. 实义  ：裁决必须被接到变量上。裸调用 / 全赋给 `_` 等于「叫了风控但不
//	           用它的裁决」，后果与没过风控一致。
//
// ─── 已知边界（静态近似，写在明处）─────────────────────────────────
//	1. 委派式写法看不见：若把报送包进 `e.dispatch(ctx, order)` 这类辅助函数，
//	   A 仍判得出（Submit 体内没有 CheckOrder 就红），但 B 的「先后」会失去参
//	   照物——B 只在两者都在同一个函数体里时才有意义。要在辅助函数里做风控，
//	   就把 CheckOrder 留在 Submit 体内当闸门。
//	2. C 只判「接住」，判不了「真用」：`verdict, err := …CheckOrder(…)` 之后把
//	   verdict 晾着不用，AST 上和真的用了是一样的——那部分由 code review 与
//	   pkg/risk 的行为测试兜。
//	3. 只看本包：`ExecEngine` 的其它实现若放在别的包（例如 NT 侧车的 Go 适配
//	   器），本护栏看不见。真出现时把扫描根参数化到 pkg/live 之外，或补同类护栏。
//
// ─── 现状（如实记录，别把它当成绿证明）─────────────────────────────
// HEAD 上唯一的 Submit 仍是 K1 契约 stub（body 只有 panic("contract stub:
// not implemented")），所以 A/B/C 现在没有活体对象。stub 必须逐条登记在
// execSubmitStubSlots 并写明理由，TestExecEngineSubmitGuardSeesTheTree 盯着
// 每条登记都仍有对应实体。于是「把 panic 换成随便一个实现」（假装 K1 已落地）
// 会**立刻变红** —— 这正是本次改动期间最可能发生的破口。K1 真实现填上时必须
// 同时满足 A/B/C，并把该条登记删掉。
//
// 判据本身是不是永真的，由 TestExecEngineSubmitGuardIsNotVacuous 的合成源码
// 腿证明（正例反例都有），不靠「扫完了没找到问题」这种假绿。

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
	// execRiskHook 是不变量要求的风控挂点方法名（risk.RiskEngine.CheckOrder）。
	execRiskHook = "CheckOrder"

	// execGuardFile 是护栏自己：它全文都在谈这条不变量，若被纳入扫描会自己
	// 触发自己的判据。
	execGuardFile = "exec_submit_risk_check_test.go"
)

// execBrokerExitSelectors 是「订单已经出门」的调用名。Broker 契约
// （interfaces.go:52）里唯一的报送口是 SubmitOrder；再补一条调用链形状：
// e.Broker().SubmitOrder(...) 的内层也是 Broker()。
var execBrokerExitSelectors = map[string]bool{"SubmitOrder": true}

// execSubmitStubSlots 登记「目前仍是契约 stub」的 Submit 实现，键是接收者类
// 型名。这是**允许表**而不是装饰：登记是一个会过期的声明，里面的每一条都必须
// 还能在包里找到实体（由 TestExecEngineSubmitGuardSeesTheTree 盯住），一旦实
// 体不再是 stub（被实现了）就必须同时满足 A/B/C 并把登记删掉。
var execSubmitStubSlots = map[string]string{
	"*StandardExecEngine": "K1 未落地（interfaces.go 的 contract stub）；K1 实现填进来时必须先过 risk.CheckOrder，届时删掉本条登记",
}

// execSubmitSite 是一个被判定过的 Submit 方法体。
type execSubmitSite struct {
	file     string
	recv     string // 接收者类型名，形如 "*StandardExecEngine"
	line     int
	stub     bool
	risk     int // CheckOrder 调用数
	broker   int // broker 出口调用数
	problems []string
}

// where 返回人类可读位置。
func (s execSubmitSite) where() string { return fmt.Sprintf("%s:%d %s.Submit", s.file, s.line, s.recv) }

// ─── 护栏本体 ──────────────────────────────────────────────────────

// TestExecEngineSubmitGoesThroughRiskCheck 是护栏：包内所有签名合契的 Submit
// 实现体都必须满足 A/B/C（stub 走登记）。
func TestExecEngineSubmitGoesThroughRiskCheck(t *testing.T) {
	t.Parallel()

	sites, err := scanExecSubmitBodies(".")
	require.NoError(t, err)

	var msgs []string
	for _, s := range sites {
		msgs = append(msgs, s.problems...)
	}
	sort.Strings(msgs)

	assert.Empty(t, msgs,
		"ExecEngine.Submit 必须**先**过 risk.RiskEngine.CheckOrder 才能报送（interfaces.go:87 的不变量）。"+
			"绕过风控的订单在回测里表现为收益更好看、实盘里表现为无闸门敞口，两处都不报错：\n%s",
		strings.Join(msgs, "\n"))
}

// TestExecEngineSubmitGuardSeesTheTree 是反瞎腿。没有它，「没有违规」可能只是
// 遍历没走到 interfaces.go、或签名没匹配上、或 stub 识别失效——那三种情况下
// 上面的绿都是假的。
func TestExecEngineSubmitGuardSeesTheTree(t *testing.T) {
	t.Parallel()

	sites, err := scanExecSubmitBodies(".")
	require.NoError(t, err)

	files := make([]string, 0, len(sites))
	registered := map[string]bool{}
	for recv := range execSubmitStubSlots {
		registered[recv] = true
	}
	for _, s := range sites {
		files = append(files, s.file)
		delete(registered, s.recv)
	}

	assert.Contains(t, files, "interfaces.go",
		"扫描没有走到 interfaces.go —— 那里正是 Submit 契约与唯一实现所在，没走到就等于没扫")
	assert.NotEmpty(t, sites,
		"一个 Submit 都没识别出来 —— 签名匹配或接收者解析瞎了，上面那条护栏的绿是假的")

	// 登记表必须与现实对齐：登记了却找不到实体（文件改了名字/类型被删），或
	// 实体已经不是 stub 了却还挂着登记，都是「注册万能infinity过期」。
	stale := make([]string, 0, len(registered))
	for recv := range registered {
		stale = append(stale, recv)
	}
	sort.Strings(stale)
	assert.Empty(t, stale,
		"下面这些 stub 登记在包里找不到对应实体（可能已改名/已删除）—— 留着它只会静默放大口子：%v", stale)

	for _, s := range sites {
		if execSubmitStubSlots[s.recv] != "" && !s.stub {
			t.Errorf("%s 在 execSubmitStubSlots 里登记为契约 stub，但它的函数体已经不是 stub 了 —— "+
				"实现既然填进来了就必须同时满足「存在 / 先后 / 实义」三条判定，请把这条登记删掉", s.where())
		}
	}
}

// TestExecEngineSubmitGuardIsNotVacuous 是反证腿：把判据喂它声称能抓的形状，
// 必须报出 **具体的** 问题；对照组（合规写法 / 契约 stub）必须放行。
//
// 没有这一条，「A/B/C 都过了」可能只说明代码里压根没有可判的东西。
func TestExecEngineSubmitGuardIsNotVacuous(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		src      string
		wantWhy  string // 非空 = 期望被判红，且问题文本必须包含这一段
		wantStub bool
	}{
		{
			name: "缺 CheckOrder：注释写了不算（正是防注释误报的那一刀）",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	// 必须先过风控挂点 CheckOrder，被拒则 TopicExecOrderRejected
	id, err := e.broker.SubmitOrder(intent)
	return id, err
}`,
			wantWhy: "CheckOrder",
		},
		{
			name: "顺序反了：先报送后补检",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	id, err := e.broker.SubmitOrder(intent)
	verdict, rerr := e.risk.CheckOrder(ctx, intent)
	if rerr != nil || !verdict.Allowed {
		return "", rerr
	}
	return id, err
}`,
			wantWhy: "姗姗来迟",
		},
		{
			name: "裁决被丢弃：裸调用",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	e.risk.CheckOrder(ctx, intent)
	return e.broker.SubmitOrder(intent)
}`,
			wantWhy: "裁决被丢弃",
		},
		{
			name: "裁决被丢弃：全赋给 _",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	_, _ = e.risk.CheckOrder(ctx, intent)
	return e.broker.SubmitOrder(intent)
}`,
			wantWhy: "裁决被丢弃",
		},
		{
			name: "绕 Broker() 访问器出门也算出口",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	orderID, err := e.Broker().SubmitOrder(toOrder(intent))
	verdict, _ := e.risk.CheckOrder(ctx, intent)
	_ = verdict
	return orderID, err
}`,
			wantWhy: "姗姗来迟",
		},
		{
			name: "对照：合规实现 → 放行",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	verdict, err := e.risk.CheckOrder(ctx, intent)
	if err != nil {
		return "", err
	}
	if !verdict.Allowed {
		return "", errors.New("rejected")
	}
	return e.broker.SubmitOrder(intent)
}`,
		},
		{
			name: "对照：直接在条件里用裁决 → 放行",
			src: `package p
func (e *engine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	if verdict, err := e.risk.CheckOrder(ctx, intent); err != nil || !verdict.Allowed {
		return "", err
	}
	return e.broker.SubmitOrder(intent)
}`,
		},
		{
			name: "对照：契约 stub → 放行且被识别为 stub",
			src: `package p
func (e *StandardExecEngine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	panic("contract stub: not implemented")
}`,
			wantStub: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, problems := inspectExecSubmitSnippet(t, tc.src)

			assert.Equal(t, tc.wantStub, stub, "stub 识别结果与预期不符")
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

// scanExecSubmitBodies 扫 dir（不递归：一个 Go 包 = 一个目录）下的非测试
// .go 文件，返回每个合契 Submit 方法体的判定结果。
func scanExecSubmitBodies(dir string) ([]execSubmitSite, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var sites []execSubmitSite
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == execGuardFile {
			continue
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", name, perr)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !matchesExecEngineSubmit(fn) {
				continue
			}
			site := execSubmitSite{
				file: name,
				recv: recvTypeName(fn),
				line: fset.Position(fn.Pos()).Line,
			}
			site.stub, site.risk, site.broker, site.problems =
				inspectExecSubmitBody(fset, fn)
			if execSubmitStubSlots[site.recv] == "" && site.stub {
				site.problems = append(site.problems, fmt.Sprintf(
					"%s 仍是契约 stub 但没有登记在 execSubmitStubSlots —— "+
						"stub 是被允许的例外，例外必须在明处并写明什么时候消失", site.where()))
			}
			sites = append(sites, site)
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file == sites[j].file {
			return sites[i].line < sites[j].line
		}
		return sites[i].file < sites[j].file
	})
	return sites, nil
}

// inspectExecSubmitBody 判定一个 Submit 方法体。返回 (是否契约 stub, CheckOrder
// 调用数, broker 出口数, 问题列表)。
func inspectExecSubmitBody(fset *token.FileSet, fn *ast.FuncDecl) (bool, int, int, []string) {
	if fn.Body == nil {
		return false, 0, 0, []string{recvTypeName(fn) + ".Submit 没有函数体（接口方法？）—— 静态判不定（fail closed）"}
	}
	// 契约 stub（`panic("contract stub: not implemented")` 独占函数体）走登记通道。
	if isSinglePanicBody(fn.Body) {
		return true, 0, 0, nil
	}

	var (
		parents  []ast.Node
		riskOff  = -1
		riskLine int
		brkOff   = -1
		brkLine  int
		riskN    int
		brkN     int

		problems []string
	)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			if len(parents) > 0 {
				parents = parents[:len(parents)-1]
			}
			return true
		}

		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				off := fset.Position(call.Pos()).Offset
				switch {
				case sel.Sel.Name == execRiskHook:
					riskN++
					if riskOff < 0 {
						riskOff, riskLine = off, fset.Position(call.Pos()).Line
					}
					if verdictDiscarded(parents) {
						problems = append(problems, fmt.Sprintf("%s:%d %s 的裁决被丢弃（裸调用或全赋给 `_`）—— 叫了风控不用它的裁决，等价于没过风控",
							recvTypeName(fn), fset.Position(call.Pos()).Line, sel.Sel.Name))
					}
				case execBrokerExitSelectors[sel.Sel.Name] || exitViaBrokerAccessor(sel):
					brkN++
					if brkOff < 0 {
						brkOff, brkLine = off, fset.Position(call.Pos()).Line
					}
				}
			}
		}

		parents = append(parents, n)
		return true
	})

	if riskN == 0 {
		problems = append(problems, fmt.Sprintf("%s:%d 的 Submit 函数体内没有 %s 调用 —— 订单绕过风控直达 broker（interfaces.go:87）",
			recvTypeName(fn), fset.Position(fn.Pos()).Line, execRiskHook))
	}
	if riskN > 0 && brkN > 0 && riskOff > brkOff {
		problems = append(problems, fmt.Sprintf("%s:%d 的 Submit 在第 %d 行就把订单送出门了，第 %d 行的 %s 才姗姗来迟 —— 先报送后补检同样是不变量被破",
			recvTypeName(fn), fset.Position(fn.Pos()).Line, brkLine, riskLine, execRiskHook))
	}

	return false, riskN, brkN, problems
}

// matchesExecEngineSubmit 判定是否是 ExecEngine.Submit 的实现：
// `Submit(ctx context.Context, intent OrderIntent) (string, error)`。
// 用签名而不是「名字叫 Submit」收窄作用域：同包还有一批语义不同的 Submit*
// 方法（Broker.SubmitOrder / OrderManager.SubmitOrder 等），不应被这条护栏误伤，
// 也不该稀释它。
func matchesExecEngineSubmit(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || fn.Name.Name != "Submit" {
		return false
	}
	params := fn.Type.Params
	if params == nil || len(params.List) != 2 {
		return false
	}
	if !isContextParam(params.List[0]) {
		return false
	}
	if baseName(params.List[1].Type) != "OrderIntent" {
		return false
	}
	res := fn.Type.Results
	if res == nil || len(res.List) != 2 {
		return false
	}
	return baseName(res.List[0].Type) == "string" && baseName(res.List[1].Type) == "error"
}

func isContextParam(f *ast.Field) bool {
	sel, ok := f.Type.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Context" && identName(sel.X) == "context"
}

// exitViaBrokerAccessor 识别 `e.Broker().SubmitOrder(...)` 之类：出口挂在
// Broker() 的返回值上，光看最外层选择器名会漏掉.
func exitViaBrokerAccessor(sel *ast.SelectorExpr) bool {
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	is, ok := inner.Fun.(*ast.SelectorExpr)
	return ok && is.Sel.Name == "Broker"
}

// verdictDiscarded 判定 CheckOrder 的返回值是不是被丢掉了。
// 只认三种「在用」的形状：赋值到至少一个非 `_` 的变量、出现在条件/实参里。
func verdictDiscarded(parents []ast.Node) bool {
	if len(parents) == 0 {
		return true
	}
	switch p := parents[len(parents)-1].(type) {
	case *ast.ExprStmt:
		return true // 裸调用，两只返回值全丢
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			if identName(lhs) != "_" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// isSinglePanicBody 判定是否是 K0 合约 stub 形态：函数体只有一条 panic 语句。
func isSinglePanicBody(body *ast.BlockStmt) bool {
	if len(body.List) != 1 {
		return false
	}
	stmt, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	return identName(call.Fun) == "panic"
}

// recvTypeName 返回接收者类型名的稳定形态（`*StandardExecEngine`）。
func recvTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		return "*" + identName(star.X)
	}
	return identName(t)
}

func baseName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		return baseName(star.X)
	}
	return identName(expr)
}

// identName 取表达式最末的名字（Ident 或 SelectorExpr 的选择器名）。
func identName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}

// inspectExecSubmitSnippet 把一段合成源码里的 Submit 方法体喂给判据，返回
// (是否 stub, 问题列表)。这是反证腿的入口：它绕过文件系统，因此「仓库里暂时
// 没有活体实现」不会让判据变成永真。
func inspectExecSubmitSnippet(t *testing.T, src string) (bool, []string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", src, 0)
	require.NoError(t, err)

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !matchesExecEngineSubmit(fn) {
			continue
		}
		stub, _, _, problems := inspectExecSubmitBody(fset, fn)
		return stub, problems
	}
	t.Fatalf("合成源码里没有找到合契的 Submit 方法 —— fixture 写错了")
	return false, nil
}
