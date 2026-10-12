package msgbus_test

// K11 护栏 ② —— msgbus.Publish 必须「先记录、后分发」（BusTap 语义）。
//
// 被钉住的不变量（pkg/msgbus/interfaces.go 包注释 §先记录后分发 / Publish 方法
// 注释第 4、5 步，原本只有注释）：
//
//	消息在派发给订阅者之前经 eventstore 落 audit.message_log——
//	订阅者看不到未落库的消息。
//
// 为什么必须机器验证：反序（先分发后记录）的运行期症状是**零报错**——订阅者照
// 常收到消息，业务照常跑，只是 audit.message_log 缺行。缺口的代价要到事后回放
// 对账时才付，而那时现场早没了。BusTap 的全部价值就是「订阅者看到的 == 回放得
// 出来的」，这句话一旦违反，审计层就退化成装饰。
//
// ─── 作用域（scanInside 语义，防注释误报）───────────────────────────
// 只判 pkg/msgbus 根目录下、签名与 MsgBus.Publish 一致的方法**体**。不扫原文：
// 这个文件里「先记录后分发」四个字出现了十几处（注释、error 文案、本文件自
// 述），字符串扫描会**因为**注释而变红、也会因为漏掉实现而变绿——两头都不可信。
//
// 四条判定：
//
//	A. 有记录：存在落库调用（Tap.Append 的实现形态 = 名为 Append 的调用）；
//	B. 有分发：存在 handler 调用（认不出分发一律 fail closed —— 否则 C 的
//	           「先后」在「压根没有分发」的代码上是永真的）；
//	C. 先后  ：首个记录调用必须早于首个分发调用；
//	D. 闸门  ：记录失败必须立刻 return（一个 handler 都不许跑）。只有 C 没有 D
//	           等于「落库失败也照发」，BusTap 仍然破。
//
// 是不是永真，由 TestFirstRecordThenDispatchGuardIsNotVacuous 的合成源码腿证明
// （正反例都有），不靠「扫完了没找到问题」这种假绿。

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
	// busRecordSelector 是 BusTap 的落库调用（Tap.Append）。
	busRecordSelector = "Append"

	// busTapGuardFile 是护栏自己，全文都在谈 BusTap，不参与扫描。
	busTapGuardFile = "publish_record_before_dispatch_test.go"
)

// busHandlerContainers 是「订阅者集合」的字段名。分发只有两种写法：把它 range
// 出来逐个调用，或直接按下标取出来调用——两种都以这个名字为锚。
var busHandlerContainers = map[string]bool{
	"handlers": true, "subs": true, "subscribers": true, "handler": true,
}

// publishSite 是一个被判定过的 Publish 方法体。
type publishSite struct {
	file  string
	recv  string
	line  int
	recN  int // 记录调用数
	disN  int // 分发调用数
	recLn int
	disLn int
	// aborted 记录「落库失败即刻 return」是否成立。
	aborted  bool
	problems []string
}

func (s publishSite) where() string { return fmt.Sprintf("%s:%d %s.Publish", s.file, s.line, s.recv) }

// ─── 护栏本体 ──────────────────────────────────────────────────────

// TestPublishRecordsBeforeDispatching 是护栏：包内所有合契 Publish 实现体都必
// 须满足 A/B/C/D。
func TestPublishRecordsBeforeDispatching(t *testing.T) {
	t.Parallel()

	sites, err := scanPublishBodies(".")
	require.NoError(t, err)

	var msgs []string
	for _, s := range sites {
		msgs = append(msgs, s.problems...)
	}
	sort.Strings(msgs)

	assert.Empty(t, msgs,
		"msgbus.Publish 必须**先**经 EventStore 记录再分发（BusTap 语义）。"+
			"反序或缺闸门不报错、不缺七拐八绕——只是 audit.message_log 缺行，事后回放在另一个时间点付账：\n%s",
		strings.Join(msgs, "\n"))
}

// TestFirstRecordThenDispatchGuardSeesTheTree 是反瞎腿：「没有违规」可能是因为
// 遍历没走到 interfaces.go、签名没匹配上、或者根本没认出分发调用。
func TestFirstRecordThenDispatchGuardSeesTheTree(t *testing.T) {
	t.Parallel()

	sites, err := scanPublishBodies(".")
	require.NoError(t, err)

	var files []string
	for _, s := range sites {
		files = append(files, s.file)
	}
	assert.Contains(t, files, "interfaces.go",
		"扫描没有走到 interfaces.go —— SyncBus.Publish 就在那里，没走到就等于没扫")
	require.NotEmpty(t, sites,
		"一个 Publish 都没识别出来 —— 签名匹配瞎了，上面那条护栏的绿是假的")

	for _, s := range sites {
		assert.Greater(t, s.recN, 0,
			"%s 没能识别出记录调用（Append）—— 识别逻辑瞎了，「先后」这条判定在该方法上是永真的", s.where())
		assert.Greater(t, s.disN, 0,
			"%s 没能识别出分发调用（handler 调用）—— 识别不出分发时「记录先于分发」是**恒真**的，"+
				"必须 fail closed 而不是静默放行", s.where())
	}
}

// TestFirstRecordThenDispatchGuardIsNotVacuous 是反证腿：把判据喂它声称能抓的
// 形状，必须报出**具体**的问题；对照组（合规写法 / 名字一样但签名不同的方法）
// 必须放行。
func TestFirstRecordThenDispatchGuardIsNotVacuous(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		src     string
		wantWhy string // 非空 = 期望被判红，且问题文本必须包含这一段
	}{
		{
			name: "反序：先分发后记录",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	msg := newEnvelope(topic, b.now(), payload)
	var errs []error
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			errs = append(errs, err)
		}
	}
	if err := b.tap.Append(ctx, msg); err != nil {
		return err
	}
	return errors.Join(errs...)
}`,
			wantWhy: "才落库",
		},
		{
			name: "没有记录：注释写满「先记录后分发」也没用",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	// BusTap：先记录后分发——落库钩子由 eventstore 模块提供。
	msg := newEnvelope(topic, b.now(), payload)
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}`,
			wantWhy: "落库调用",
		},
		{
			name: "落库失败没 return：闸门不成立",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	msg := newEnvelope(topic, b.now(), payload)
	b.tap.Append(ctx, msg)
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}`,
			wantWhy: "闸门",
		},
		{
			name: "只有记录没有分发 —— 「先后」不能假绿",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	msg := newEnvelope(topic, b.now(), payload)
	if err := b.tap.Append(ctx, msg); err != nil {
		return err
	}
	notifyResult(msg)
	return nil
}`,
			wantWhy: "分发调用",
		},
		{
			name: "对照：合规实现（记录 → 闸门 → 分发）→ 放行",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	if b.tap == nil {
		return ErrNoTap
	}
	msg := newEnvelope(topic, b.now(), payload)
	if err := b.tap.Append(ctx, msg); err != nil {
		return fmt.Errorf("append failed: %w", err)
	}
	var errs []error
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}`,
		},
		{
			name: "对照：两行式错误检查也算闸门 → 放行（别把护栏做成格式检查）",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	msg := newEnvelope(topic, b.now(), payload)
	err := b.tap.Append(ctx, msg)
	if err != nil {
		return fmt.Errorf("append failed: %w", err)
	}
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}`,
		},
		{
			name: "对照：按下标取值分发也算分发 → 放行",
			src: `package p
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	msg := newEnvelope(topic, b.now(), payload)
	if err := b.tap.Append(ctx, msg); err != nil {
		return err
	}
	for i := range b.subs[topic] {
		if err := b.subs[topic][i](ctx, msg); err != nil {
			return err
		}
	}
	return nil
}`,
		},
		{
			name: "对照：同名但签名不是 MsgBus.Publish → 不在作用域内",
			src: `package p
func (b *SyncBus) Publish(topic string) error {
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			problems := inspectPublishSnippet(t, tc.src)

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

// scanPublishBodies 扫 dir（不递归：一个 Go 包 = 一个目录）下的非测试 .go 文件，
// 返回每个合契 Publish 方法体的判定结果。
func scanPublishBodies(dir string) ([]publishSite, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var sites []publishSite
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == busTapGuardFile {
			continue
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", name, perr)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !matchesMsgBusPublish(fn) {
				continue
			}
			site := publishSite{
				file: name,
				recv: recvTypeNameBus(fn),
				line: fset.Position(fn.Pos()).Line,
			}
			site.problems = inspectPublishBody(fset, fn, &site)
			sites = append(sites, site)
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].file < sites[j].file })
	return sites, nil
}

// inspectPublishBody 判定一个 Publish 方法体：把 A/B/C/D 的结果写进 site 并
// 返回问题列表。
func inspectPublishBody(fset *token.FileSet, fn *ast.FuncDecl, site *publishSite) []string {
	if fn.Body == nil {
		return []string{recvTypeNameBus(fn) + ".Publish 没有函数体 —— 静态判不定（fail closed）"}
	}

	var (
		parents []ast.Node
		recOff  = -1
		disOff  = -1
		recN    int
		disN    int
		recLn   int
		disLn   int

		// 每个落库调用（连同它把结果交给谁）与函数体内所有 if。
		recordCalls []busRecordCall
		allIfs      []*ast.IfStmt
	)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			if len(parents) > 0 {
				parents = parents[:len(parents)-1]
			}
			return true
		}

		if call, ok := n.(*ast.CallExpr); ok {
			off := fset.Position(call.Pos()).Offset
			ln := fset.Position(call.Pos()).Line

			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == busRecordSelector {
				recN++
				if recOff < 0 {
					recOff, recLn = off, ln
				}
				recordCalls = append(recordCalls, busRecordCall{
					call: call, off: off, errVars: busAssignedVars(parents, call),
				})
			}
			if isBusDispatchCall(parents, call) {
				disN++
				if disOff < 0 {
					disOff, disLn = off, ln
				}
			}
		}
		if ifStmt, ok := n.(*ast.IfStmt); ok {
			allIfs = append(allIfs, ifStmt)
		}

		parents = append(parents, n)
		return true
	})

	// D. 闸门：挑出「主体里有 return」的 if，看有没有一个能挂在某次落库调用上。
	var gates []busReturnGate
	for _, ifStmt := range allIfs {
		if subtreeHasReturnBus(ifStmt.Body) {
			gates = append(gates, busReturnGate{ifStmt: ifStmt, off: fset.Position(ifStmt.Pos()).Offset})
		}
	}
	aborted := false
	for _, rc := range recordCalls {
		if busRecordGated(rc, gates) {
			aborted = true
			break
		}
	}

	site.recN, site.disN, site.recLn, site.disLn, site.aborted = recN, disN, recLn, disLn, aborted

	var problems []string
	where := fmt.Sprintf("%s:%d", recvTypeNameBus(fn), fset.Position(fn.Pos()).Line)

	if recN == 0 {
		problems = append(problems, fmt.Sprintf(
			"%s 的 Publish 函数体内没有 %s 落库调用 —— BusTap「先记录后分发」被违反："+
				"订阅者会看到一条 audit.message_log 里查不到的消息", where, busRecordSelector))
	}
	if disN == 0 {
		problems = append(problems, fmt.Sprintf(
			"%s 的 Publish 函数体内没有识别出任何 handler 分发调用 —— "+
				"认不出分发时「记录先于分发」是恒真的，判据选择 fail closed（要么写清分发形态，要么补进 busHandlerContainers）", where))
	}
	if recN > 0 && disN > 0 && recOff > disOff {
		problems = append(problems, fmt.Sprintf(
			"%s 的 Publish 在第 %d 行就把消息分发给订阅者了，第 %d 行才落库 —— "+
				"反序之后订阅者看到的是「库里查不到」的消息，回放缺行而现场早已消失", where, disLn, recLn))
	}
	if recN > 0 && !aborted {
		problems = append(problems, fmt.Sprintf(
			"%s 的 Publish 落库之后没有「失败即刻 return」的闸门 —— "+
				"记不下来也照样发出去，audit.message_log 静默缺行", where))
	}
	return problems
}

// matchesMsgBusPublish 判定是否是 MsgBus.Publish 的实现：
// `Publish(ctx context.Context, topic string, payload any) error`。
// 用签名而不是名字收窄作用域：别处的 Publish-helpers 不该被这条护栏误伤。
func matchesMsgBusPublish(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || fn.Name.Name != "Publish" {
		return false
	}
	params := fn.Type.Params
	if params == nil || len(params.List) != 3 {
		return false
	}
	if !isContextFieldBus(params.List[0]) {
		return false
	}
	if identNameBus(params.List[1].Type) != "string" {
		return false
	}
	switch params.List[2].Type.(type) {
	case *ast.Ident, *ast.InterfaceType: // any / interface{}
	default:
		return false
	}
	res := fn.Type.Results
	return res != nil && len(res.List) == 1 && identNameBus(res.List[0].Type) == "error"
}

func isContextFieldBus(f *ast.Field) bool {
	sel, ok := f.Type.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Context" && identNameBus(sel.X) == "context"
}

// isBusDispatchCall 识别 handler 调用。两种形态：
//
//	for _, h := range b.handlers[topic] { h(ctx, msg) }   —— range 变量被调用
//	b.subs[topic][i](ctx, msg)                            —— 直接按下标取出来调
//
// 两种都以 busHandlerContainers 里的字段名为锚，不与具体变量名耦合（否则换个
// 变量名就静默失效）。
func isBusDispatchCall(parents []ast.Node, call *ast.CallExpr) bool {
	for _, p := range parents {
		rs, ok := p.(*ast.RangeStmt)
		if !ok || !mentionsBusContainer(rs.X) {
			continue
		}
		name := identNameBus(call.Fun)
		if name == "" {
			continue
		}
		for _, v := range []ast.Expr{rs.Key, rs.Value} {
			if identNameBus(v) == name {
				return true
			}
		}
	}
	return mentionsBusContainer(call.Fun)
}

// busRecordCall 是一次落库调用：它自己在哪儿、结果交给了哪些变量。
type busRecordCall struct {
	call    *ast.CallExpr
	off     int
	errVars []string
}

// busReturnGate 是一个「主体里有 return」的 if —— 闸门只能由它构成。
type busReturnGate struct {
	ifStmt *ast.IfStmt
	off    int
}

// busRecordGated 判定某次落库调用是否挂在「失败即刻 return」的闸门上。
//
// 接受两种合规写法：
//
//	a. 一行式：if err := b.tap.Append(ctx, msg); err != nil { return … }
//	b. 两行式：err := b.tap.Append(ctx, msg)
//	          if err != nil { return … }
//
// 认 b 的理由是护栏自身的存亡：把语义等价的写法判成违规，这条护栏就退化成
// 「照我这个样子写」的格式检查——那不是不变量，而是会被绕开的负资产。认不出
// 两行式的人迟早会写出两行式，然后第一反应是把它改成别的样子而不是改护栏。
func busRecordGated(rc busRecordCall, gates []busReturnGate) bool {
	for _, g := range gates {
		if subtreeContainsBus(g.ifStmt.Init, rc.call) || subtreeContainsBus(g.ifStmt.Cond, rc.call) {
			return true // 形态 a
		}
		if len(rc.errVars) == 0 || g.off < rc.off {
			continue
		}
		if busIfRefersAny(g.ifStmt, rc.errVars) {
			return true // 形态 b
		}
	}
	return false
}

// busAssignedVars 取 call 所在赋值语句左侧的非 `_` 变量名（认形态 b 用）。
func busAssignedVars(parents []ast.Node, call *ast.CallExpr) []string {
	for i := len(parents) - 1; i >= 0; i-- {
		as, ok := parents[i].(*ast.AssignStmt)
		if !ok {
			continue
		}
		found := false
		for _, rhs := range as.Rhs {
			if subtreeContainsBus(rhs, call) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		var out []string
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				out = append(out, id.Name)
			}
		}
		return out
	}
	return nil
}

// busIfRefersAny 判定 if 的 Init/Cond 是否引用了 names 里的任一变量名。
// 只看 Init/Cond：Body 里的赋值不构成「闸门条件」。
func busIfRefersAny(ifStmt *ast.IfStmt, names []string) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	hit := false
	for _, root := range []ast.Node{ifStmt.Init, ifStmt.Cond} {
		if root == nil {
			continue
		}
		ast.Inspect(root, func(n ast.Node) bool {
			if hit || n == nil {
				return !hit
			}
			if id, ok := n.(*ast.Ident); ok && set[id.Name] {
				hit = true
				return false
			}
			return true
		})
	}
	return hit
}

func subtreeContainsBus(root ast.Node, target ast.Node) bool {
	if root == nil {
		return false
	}
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found || n == nil {
			return !found
		}
		if n == target {
			found = true
			return false
		}
		return true
	})
	return found
}

func subtreeHasReturnBus(root ast.Node) bool {
	if root == nil {
		return false
	}
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found || n == nil {
			return !found
		}
		if _, ok := n.(*ast.ReturnStmt); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

func mentionsBusContainer(root ast.Node) bool {
	if root == nil {
		return false
	}
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found || n == nil {
			return !found
		}
		switch e := n.(type) {
		case *ast.Ident:
			if busHandlerContainers[strings.ToLower(e.Name)] {
				found = true
				return false
			}
		case *ast.SelectorExpr:
			if busHandlerContainers[strings.ToLower(e.Sel.Name)] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func recvTypeNameBus(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		return "*" + identNameBus(star.X)
	}
	return identNameBus(t)
}

func identNameBus(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}

// inspectPublishSnippet 把一段合成源码里的 Publish 方法体喂给判据。绕过文件系
// 统：即使仓库里的实现形态变化，判据本身的真假也仍有证据。
func inspectPublishSnippet(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", src, 0)
	require.NoError(t, err)

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !matchesMsgBusPublish(fn) {
			continue
		}
		site := &publishSite{}
		return inspectPublishBody(fset, fn, site)
	}
	// 签名不匹配 Publish 的对照组：不在作用域内 ⇒ 没有问题。
	return nil
}
