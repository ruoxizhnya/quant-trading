package backtest

// K17-RV 复查收口的三条测试腿（TASKS.md K17-RV1 / RV2 / RV4）。
//
//  - RV1 · 调用方不得丢弃 OnBar 交还的 due 子单：行为判据在 execalgo 层
//    （pkg/execalgo/dispatch_onbar_test.go 的
//    TestDispatcher_OnBarErrorReturnsTakenChildren），调用方侧加一道
//    **AST 形态护栏**——OnBar 的错误分支正常接线不可达（Attach 后
//    planState.onBar 只在「未 Schedule」时报错，而 Attach 已拒绝空父单号），
//    要行为触发就得给 Engine 加测试缝；TASKS.md K17-RV1 ④ 的二选一里取
//    「execalgo 层断言 + 调用方静态护栏」。
//  - RV2 · execAlgoRunDiag.record 的 nil 接收者防护。
//  - RV4 · 口径 A（2026-10-13 拍板）：取消发生在某交易日内时，该日**已到点**
//    的子单仍照常报送；下一交易日的日循环顶部才中止。
import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ── RV2 · nil 接收者 ────────────────────────────────────────────────────

// TestExecAlgoRunDiag_RecordNilReceiverDoesNotPanic：record 与同组
// empty / invalidReasonCodes / logSummary 的 nil 语义对齐（后三者均
// nil-safe，唯独 record 曾不是）。
func TestExecAlgoRunDiag_RecordNilReceiverDoesNotPanic(t *testing.T) {
	var nilDiag *execAlgoRunDiag
	require.NotPanics(t, func() {
		nilDiag.record(execalgo.AttachFailureDuplicateParent, "EA-20260101-000001.SZ-long")
	}, "nil 接收者调用 record 不得 panic")

	// 零值结构体（非 nil 指针）仍可用：首写时惰性建 map。
	var zero execAlgoRunDiag
	zero.record(execalgo.AttachFailureOther, "EA-zero")
	assert.Equal(t, 1, zero.fails[execalgo.AttachFailureOther])
	assert.Equal(t, "EA-zero", zero.samples[execalgo.AttachFailureOther])
}

// ── RV4 · 取消当日已到点片仍报送（口径 A）──────────────────────────────

// k17rv4CancelProbe 在窗口首日挂 TWAP 5 片；窗口第 3 日取消 ctx 且不再动作。
type k17rv4CancelProbe struct {
	*strategy.BaseStrategy
	cancel context.CancelFunc
}

func (p *k17rv4CancelProbe) Name() string { return "k17rv4_ctx_cancel" }

func (p *k17rv4CancelProbe) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	series := bars["000001.SZ"]
	switch len(series) {
	case 1:
		return []strategy.Signal{{
			Symbol: "000001.SZ", Action: "buy", Direction: domain.DirectionLong,
			Strength: 1.0, Date: series[0].Date, ExecAlgo: "twap", ExecSlices: 5,
		}}, nil
	case 3:
		// 取消注入点：晚于日循环顶部的 ctx 检查、早于当日 exec-algo 报送。
		p.cancel()
	}
	return nil, nil
}

func (p *k17rv4CancelProbe) Weight(strategy.Signal, float64) float64 { return 1 }

// TestExecAlgo_CancelSameDayDueSliceStillSubmitted 钉住口径 A：取消当日的
// 到点片仍报送，次日才停。
//
// 破坏腿（改成「报送点判 ctx」→ 本测试红）：在 processExecAlgoChildren
// 的报送前加 `if ctx.Err() != nil { continue }`，取消当日那笔子单消失，
// 末笔日期会早于取消日。
func TestExecAlgo_CancelSameDayDueSliceStillSubmitted(t *testing.T) {
	logSink := &bytes.Buffer{}
	eng, _, _ := buildExecAlgoEngine(t, logSink)
	spy := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	eng.SetExecutionService(spy)

	ctx, cancel := context.WithCancel(context.Background())
	probe := &k17rv4CancelProbe{BaseStrategy: &strategy.BaseStrategy{}, cancel: cancel}
	require.NoError(t, strategy.DefaultRegistry.Register(probe))

	_, err := eng.RunBacktest(ctx, BacktestRequest{
		Strategy:       "k17rv4_ctx_cancel",
		StockPool:      []string{"000001.SZ", "600000.SH"},
		StartDate:      "2026-03-02",
		EndDate:        "2026-03-21",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	require.ErrorIs(t, err, context.Canceled, "取消在下一交易日日循环顶部生效，Run 以 ctx.Err 结束")

	orders := spy.orders()
	cancelDay := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC) // start + 2 天 = 窗口第 3 日
	require.NotEmpty(t, orders, "取消当日已到点的子单必须仍然报送（口径 A）")
	for i, rec := range orders {
		assert.False(t, rec.Order.Timestamp.After(cancelDay),
			"第 %d 笔子单（%s）落在取消日之后 —— 取消应「尽快停」但不得当日即停（口径 A）",
			i, rec.Order.Timestamp.Format("2006-01-02"))
	}
	assert.Equal(t, cancelDay, orders[len(orders)-1].Order.Timestamp,
		"末笔恰是取消当日到点的片（当日即停会让它消失）")
}

// ── RV1 · 调用方不得丢弃 OnBar 的 due 子单（AST 静态护栏）──────────────

// TestCallers_NeverDropOnBarDueChildren 断言两条下单主路径都不存在
// 「从 OnBar 取值后，在 err != nil 分支里 continue/return」的形态——
// 那会把派发器已计 submitted 的子单连同错误一起丢弃，父单进度账与报送
// 事实分叉（R2-R1 要治的病，只是移到了调用方层）。
func TestCallers_NeverDropOnBarDueChildren(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller 必须能定位测试文件")
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	for _, rel := range []string{
		filepath.Join("pkg", "backtest", "exec_algo.go"),
		filepath.Join("pkg", "live", "paper_exec_algo.go"),
	} {
		path := filepath.Join(repoRoot, rel)
		t.Run(rel, func(t *testing.T) {
			violations := onBarErrorBranchesThatBranch(t, path)
			assert.Empty(t, violations,
				"%s：OnBar 的错误分支不得 continue/return（会把已取 due 子单连同错误一起丢弃，见 K17-RV1）：%v",
				rel, violations)
		})
	}
}

// onBarErrorBranchesThatBranch 扫一个源文件，返回违规分支的位置串。
//
// 判据：某 `if err != nil { … }`（**无 Init**——err 来自前一条语句）
// 若其前一条语句是从 `….OnBar(...)` 取值，且该分支体内含 continue / break /
// goto / return，即判定违规。与文本/措辞无关，重命名与重排不影响。
func onBarErrorBranchesThatBranch(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err, "读取源文件失败：%s", path)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err, "解析源文件失败：%s", path)

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i := 1; i < len(block.List); i++ {
			ifStmt, ok := block.List[i].(*ast.IfStmt)
			if !ok || ifStmt.Init != nil || !isErrNotNil(ifStmt.Cond) || !bodyBranches(ifStmt.Body) {
				continue
			}
			asn, ok := block.List[i-1].(*ast.AssignStmt)
			if !ok || !assignsFromOnBar(asn) {
				continue
			}
			out = append(out, fset.Position(ifStmt.Pos()).String())
		}
		return true
	})
	return out
}

func isErrNotNil(cond ast.Expr) bool {
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || (be.Op != token.NEQ && be.Op != token.EQL) {
		return false
	}
	x, ok := be.X.(*ast.Ident)
	if !ok || x.Name != "err" {
		return false
	}
	y, ok := be.Y.(*ast.Ident)
	return ok && y.Name == "nil"
}

func bodyBranches(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.BranchStmt, *ast.ReturnStmt:
			found = true
		}
		return true
	})
	return found
}

func assignsFromOnBar(asn *ast.AssignStmt) bool {
	for _, rhs := range asn.Rhs {
		call, ok := rhs.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && sel.Sel != nil && sel.Sel.Name == "OnBar" {
			return true
		}
	}
	return false
}
