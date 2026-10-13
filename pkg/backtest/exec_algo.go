package backtest

// K17：exec-algo 接进**回测**下单主路径（TASKS.md K17）。
//
// ─── 接线点（两条下单主路径之一）────────────────────────────────────
//
// 回测侧的订单路径是：
//
//	processSignalsAndExecuteTrades / processSignalsFallback
//	  → executeSignalTrade
//	    → executeViaExecutionService → ExecutionBridge → BacktestExecutionService
//
// K17 的改法：信号若指定了 exec_algo（domain.Signal.ExecAlgo 非空），
// executeSignalTrade **不当场整单成交**，而是把父单挂到本 run 的
// execalgo.Dispatcher（Schedule 拆单）；随后每个交易日由
// processExecAlgoChildren 把「当日到点」的子单逐笔经**同一条**
// ExecutionBridge 送撮合（同一 CostModel，含 impact——大单拆细后单笔
// 冲击下降，即 D5「模拟真实大单」的正证据），OnFill 回填父单进度。
//
// 未指定 exec_algo 的信号走原路径，逐字节不变（K17 硬约束）。
//
// ─── 父单号（确定性）────────────────────────────────────────────────
//
// 父单号 = "EA-" + 日期 + symbol + 方向的纯函数（execAlgoParentID）。
// 子单号由 K4 的 childOrderID（(父单号, 片序号) 纯函数）派生 ⇒ 同一输入
// 两次回测拆出同一串子单号（K17 验收 4：禁时间戳派生 ID）。
//
// ─── 执行窗口（裁决，与 paper 侧同一口径）──────────────────────────
//
// 窗口 = [信号当日, 信号当日 + n 个自然日]（execalgo.ResolveExecWindow）：
// 日线粒度下步长恰为 1 个自然日，SubmitAt 严格等差；非交易日不到点顺延
// 到下一个有 bar 的日子（K4 文档化的 bar 粒度效应）。
//
// ─── 会话结束裁决（K17 要求 3：显式裁决）────────────────────────────
//
// 回测结束（日循环耗尽）时仍未报完的父单：**撤销**剩余子单
// （finalizeExecAlgo）。理由：结束后没有更多 bar，「强制撮完」等于以
// 最后一个 bar 的价格虚构本未发生的成交、注入幽灵 P&L；「撤销」则父单
// 停在部分成交——该状态已被既有簿记建模（TargetPosition.PendingQty 与
// partial-fill 日志），且大单冲击对比不受影响。paper 侧取同一裁决。
//
// ─── 非目标（K17 ④）────────────────────────────────────────────────
//
// 默认路径数值零变化；真实券商算法单不做（D1）；VWAP 成交量预测不做；
// 不改 ExecAlgorithm 冻结签名；不动 NT 侧车。
//
// ─── K18：窗口内外部仓位变动 → 取消活跃父单 ────────────────────────
//
// K17 审查发现的边界（TASKS.md K18）：父单执行窗口内发生引擎外改仓
// （止损/止盈平仓、退市强平、拆股改仓）时，剩余未报送子单必须中止，
// 否则会把刚被风控平掉的仓位重新买回。接线点：engine.go 日循环（退市
// 强平 / 拆股改仓后）+ processStopLosses 的成交分支，统一走本文件的
// cancelExecAlgoParentsForSymbol。paper 侧同一语义见
// pkg/live/paper_exec_algo.go。
import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
)

// execAlgoParentID 派生父单号：纯函数 (日期, symbol, 方向)。
//
// 为什么敢用「日期+symbol+方向」做主键：回测日循环里同一 (symbol,
// direction) 一天最多一条有效信号（策略按 symbol 产信号）；真出现重复，
// Dispatcher.Attach 会 fail-loud（重复挂载 error），不会静默覆盖。
// 禁时间戳/随机派生——那是 K17 验收 4 明令禁止的（破坏腿 ③）。
func execAlgoParentID(date time.Time, symbol string, dir domain.Direction) string {
	return fmt.Sprintf("EA-%s-%s-%s", date.Format("20060102"), symbol, string(dir))
}

// attachExecAlgoSignal 把「指定了执行算法」的信号挂到派发器（拆单），
// 返回是否已按算法路径处理。true = 调用方**不得**再整单直下。
//
// 挂载失败返回 error，由调用方分类记入运行诊断（execAlgoRunDiag）并
// 拒绝该信号——run 不中断（K17-R1 要求 2「单信号失败不中断整场」），
// 但失败计数在会话结束挂进结果（见 engine.go runBacktestInternal）。
//
// K17-R1 要求 1（形态裁决）：未知算法名是**静态误配置**，在信号校验期
// 用 IsKnownStyle 直接拒绝，不等构造算法实例才发现。工单给出的两种形态
// 里选「该 run 走 invalid + reason code」、不选「直接 fail 该信号」——
// 后者只是把 Warn 换个说法，跑完照样没人知道少了交易；invalid 状态经
// ResponseFromState / 报告 / 对比全链路透传（OBS-01 读路径），误配置对
// 调用方与自动化层都可见，且与 K9「rejected, not ignored」同口径。
func attachExecAlgoSignal(
	ctx context.Context,
	d *execalgo.Dispatcher,
	signal domain.Signal,
	qty float64,
	runID string,
	date time.Time,
) (bool, error) {
	if signal.ExecAlgo == "" {
		return false, nil
	}
	if !execalgo.IsKnownStyle(signal.ExecAlgo) {
		return false, fmt.Errorf("exec-algo: %w: signal requests %q for %s on %s (want %s|%s)",
			execalgo.ErrUnknownAlgo, signal.ExecAlgo, signal.Symbol,
			execAlgoParentID(date, signal.Symbol, signal.Direction),
			execalgo.StyleTWAP, execalgo.StyleVWAP)
	}
	n, end := execalgo.ResolveExecWindow(date, signal.ExecSlices, signal.ExecProfile)
	orderType := signal.OrderType
	if orderType == "" {
		orderType = domain.OrderTypeMarket
	}
	spec := execalgo.AttachSpec{
		ParentID:   execAlgoParentID(date, signal.Symbol, signal.Direction),
		RunID:      runID,
		Symbol:     signal.Symbol,
		Side:       signal.Direction,
		Qty:        qty,
		LimitPrice: signal.LimitPrice,
		Style:      signal.ExecAlgo,
		Slices:     n,
		Profile:    signal.ExecProfile,
		OrderType:  orderType,
		StartAt:    date,
		EndAt:      end,
	}
	if _, err := d.Attach(ctx, spec); err != nil {
		return false, err
	}
	return true, nil
}

// processExecAlgoChildren 把**当日**到点的 exec-algo 子单送撮合并回填进度。
//
// 调用时机：每个交易日 processSignalsAndExecuteTrades **之后**（新挂父单
// 的首片 SubmitAt = 信号当日，当日到点即发，与 paper 侧同序）。
//
// 无活跃父单时零开销返回（Len()==0 快路径）——默认路径数值零变化的保证。
func (e *Engine) processExecAlgoChildren(
	ctx context.Context,
	state *BacktestState,
	d *execalgo.Dispatcher,
	marketDataCache map[string][]domain.OHLCV,
	date time.Time,
	logger zerolog.Logger,
) {
	if d.Len() == 0 {
		return
	}
	execSvc := e.executionBridge.Get()
	if execSvc == nil {
		// 有算法单却没有撮合服务：子单无法报送。fail-loud 记 Error，父单
		// 顺延停滞，会话结束由 finalizeExecAlgo 显式呈现（不静默吞掉）。
		logger.Error().Msg("exec-algo: active parent orders but no ExecutionService attached — child orders cannot be submitted")
		return
	}
	for _, sym := range d.Symbols() {
		bars, ok := marketDataCache[sym]
		if !ok || len(bars) == 0 {
			// 当日无 bar（停牌 / 跌出票池）：不到点，父单剩余子单顺延。
			continue
		}
		// K17-RV4 · 口径 A（2026-10-13 拍板，有意行为）：本报送点**不判 ctx**——
		// 取消 = 「尽快停」，当日 bar 已真实发生、片已到点即照发；下一交易日的
		// 日循环顶部（engine.go 的 ctx.Done 检查）才中止。**不要**把它当缺陷
		// 重报而改成「取消即零成交」——那会让「已到点成交」消失。paper 侧同
		// 口径（见 pkg/live/paper_exec_algo.go 同名注释），两侧必须一致。
		// K17-RV1 · OnBar 契约：错误时**已取子单照常交还调用方处置**。因此
		// err != nil 只降级为诊断信号，**不丢** due——否则派发器已按 submitted
		// 计账而实际未报，父单进度账与报送事实再次分叉（R2-R1 要治的病，
		// 只是从派发器层移到了调用方层）。
		due, err := d.OnBar(ctx, bars[len(bars)-1])
		if err != nil {
			logger.Warn().Err(err).Str("symbol", sym).
				Msg("exec-algo: OnBar reported an error (diagnostic); the due children it returned are still submitted")
		}
		for _, dc := range due {
			e.executeExecAlgoChild(ctx, state, d, execSvc, dc, bars[len(bars)-1], date, logger)
		}
	}
}

// executeExecAlgoChild 送一笔到点子单进撮合，回填父单进度并滚动 tp 账。
//
// 与 executeViaExecutionService 的关系：子单走**同一条** ExecutionBridge
// （同一 CostModel / 同一滑点模型 / 同一限价语义），差别只在单量更小——
// impact 分支按 orderQty/ADV 定冲击，拆细后单笔冲击下降（D5 效果）。
func (e *Engine) executeExecAlgoChild(
	ctx context.Context,
	state *BacktestState,
	d *execalgo.Dispatcher,
	execSvc ExecutionService,
	dc execalgo.DueChild,
	dayBar domain.OHLCV,
	date time.Time,
	logger zerolog.Logger,
) {
	// quote 口径与 executeViaExecutionService 一致：有当日 bar 就用整根
	// （Close 作参考价，High/Low/Volume 供 variable/impact 成本模型）。
	quote := Quote{
		Symbol: dc.Symbol,
		Open:   dayBar.Open,
		High:   dayBar.High,
		Low:    dayBar.Low,
		Close:  dayBar.Close,
		Volume: dayBar.Volume,
		Date:   date,
	}
	order := domain.Order{
		ID:         dc.OrderID,
		Symbol:     dc.Symbol,
		Direction:  dc.Side,
		OrderType:  dc.OrderType,
		Quantity:   dc.Qty,
		LimitPrice: dc.LimitPrice,
		Timestamp:  date,
	}
	trade, err := execSvc.ExecuteOrder(order, quote)
	if err != nil {
		// 子单报送失败（如现金不足）：本片不计成交，父单进度缺一块，
		// 会话结束由 finalizeExecAlgo 显式呈现。不重试（重试语义超出
		// K17 边界）。
		logger.Warn().Err(err).
			Str("child_id", dc.OrderID).Str("parent_id", dc.ParentID).
			Str("symbol", dc.Symbol).Float64("qty", dc.Qty).
			Msg("exec-algo: child order rejected by execution service")
		return
	}

	// OnFill 先于 ApplyTrade：超额 fail-loud 时该笔不落账，保证
	// 「父单量 == Σ子单成交量」不被破坏。（超额在正常接线里不可达——
	// 每片子单只被 OnBar 交出一次；这里是纵深防御。）
	if err := d.OnFill(execalgo.Fill{
		OrderID: dc.OrderID,
		Symbol:  dc.Symbol,
		Side:    dc.Side,
		Qty:     trade.Quantity,
		Price:   trade.Price,
		Ts:      date,
	}); err != nil {
		logger.Error().Err(err).
			Str("child_id", dc.OrderID).Str("parent_id", dc.ParentID).
			Msg("exec-algo: parent order over-filled — child fill NOT booked (fail-loud)")
		return
	}

	trackerTrade, err := state.Tracker.ApplyTrade(trade)
	if err != nil {
		// 与整单路径同语义：撮合层已成交、组合层拒收（现金/T+1）。
		logger.Warn().Err(err).
			Str("child_id", dc.OrderID).Str("symbol", dc.Symbol).
			Msg("exec-algo: child fill rejected by tracker")
		return
	}

	// tp 账随子单成交滚动（与 updateTargetPositionAfterTrade 的 long/short
	// 分支同口径：ActualQty 累计、PendingQty = 目标 − 实际）。
	if tp, ok := state.TargetPositions[dc.Symbol]; ok && tp != nil {
		tp.ActualQty += trackerTrade.Quantity
		tp.PendingQty = tp.TargetQty - tp.ActualQty
		tp.LastUpdated = date
	}

	logger.Debug().
		Str("child_id", dc.OrderID).Str("parent_id", dc.ParentID).
		Str("symbol", dc.Symbol).Str("direction", string(dc.Side)).
		Float64("qty", trade.Quantity).Float64("price", trade.Price).
		Float64("commission", trade.Commission).
		Msg("exec-algo: child order filled")
}

// finalizeExecAlgo 回测结束时的显式裁决：**撤销**所有未报完父单的剩余
// 子单（K17 要求 3）。裁决理由见文件头注释——「撤销」而非「强制撮完」：
// 没有更多 bar 时强撮等于虚构成交；撤销停在部分成交，既有簿记已建模。
// paper 侧（pkg/live/paper_exec_algo.go）取同一裁决，两侧语义一致。
func finalizeExecAlgo(d *execalgo.Dispatcher, logger zerolog.Logger) {
	for _, p := range d.Unfinished() {
		logger.Warn().
			Str("parent_id", p.ParentID).Str("symbol", p.Symbol).
			Float64("filled_qty", p.Filled).Float64("parent_qty", p.Total).
			Float64("cancelled_qty", p.Total-p.Filled).
			Msg("exec-algo: parent order unfinished at session end — remaining child slices CANCELLED (adjudication: cancel, not force-fill; see pkg/backtest/exec_algo.go)")
	}
}

// ─── K18：外部仓位变动 → 取消活跃父单 ──────────────────────────────
//
// 拆单父单跨多个交易日执行。若执行窗口**中途**发生「引擎外改仓」（止损/
// 止盈平仓、退市强平、拆股改仓），剩余未报送子单必须一并中止——否则第 k
// 天被风控平掉的仓位会被第 k+1.. 天的子单**重新买回**，策略「止损后应
// 空仓」的语义被拆单破坏。整单路径无此问题（当日一次成交，无跨日残留
// 委托）；真实算法单的业界解法即 cancel-on-stop。
//
// 与 finalizeExecAlgo 的**会话结束撤销**是两种判据：本组常量用于**窗口
// 中段**取消，reason 进 cancel-reason 日志；被取消父单不再进 Unfinished()，
// 故保留会话结束告警的语义单义（不会一次取消被记两遍）。
const (
	cancelReasonStopLoss   = "stop_loss"
	cancelReasonTakeProfit = "take_profit"
	cancelReasonDelisted   = "delisted"
	cancelReasonSplit      = "corporate_action_split"
)

// cancelExecAlgoParentsForSymbol 在「引擎外改仓」实际落账后，取消该 symbol
// 活跃父单的剩余未报送子单（K18）。已报送已成交部分保留、不回滚。
//
// 只丢弃未报送片；对**已全部成交**或本无语算法单的 symbol 是空操作
// （CancelSymbol 保守返回空切片）——默认路径数值零变化。
func cancelExecAlgoParentsForSymbol(
	d *execalgo.Dispatcher,
	symbol, reason string,
	logger zerolog.Logger,
) {
	if d == nil || d.Len() == 0 {
		return
	}
	for _, p := range d.CancelSymbol(symbol, reason) {
		logger.Warn().
			Str("parent_id", p.ParentID).Str("symbol", p.Symbol).
			Str("cancel_reason", reason).
			Float64("filled_qty", p.Filled).Float64("parent_qty", p.Total).
			Float64("cancelled_qty", p.Total-p.Filled).
			Msg("exec-algo: parent order cancelled mid-window — remaining child slices dropped (external position change; already-filled slices kept)")
	}
}

// ─── K17-R1：挂载失败的运行诊断 ────────────────────────────────────
//
// execAlgoRunDiag 累计本 run 内 exec-algo 挂载失败的分类计数（每类记
// 首个样例 parentID，供日志点名定位）。四类失败都意味着「信号被拒、
// 交易从结果里消失」——「单信号失败不中断整场」的既有约定保留，但
// 结束时由 runBacktestInternal 把计数汇总进日志、并把对应稳定 Code 挂
// 进 result.InvalidReasons（既有 invalid_reasons 通道，不新增字段）：
// 禁止「跑完没人知道少了交易」。
type execAlgoRunDiag struct {
	fails   map[execalgo.AttachFailureKind]int
	samples map[execalgo.AttachFailureKind]string
}

func newExecAlgoRunDiag() *execAlgoRunDiag {
	return &execAlgoRunDiag{
		fails:   make(map[execalgo.AttachFailureKind]int),
		samples: make(map[execalgo.AttachFailureKind]string),
	}
}

// record 记一次挂载失败。每类保留首个样例（诊断点名第一个现场）。
func (dg *execAlgoRunDiag) record(kind execalgo.AttachFailureKind, parentID string) {
	if dg == nil { // K17-RV2 · nil 接收者：静默返回（同组 empty/invalidReasonCodes/logSummary 均 nil-safe，唯独 record 曾不是）
		return
	}
	if dg.fails == nil { // 零值可用
		dg.fails = make(map[execalgo.AttachFailureKind]int)
		dg.samples = make(map[execalgo.AttachFailureKind]string)
	}
	dg.fails[kind]++
	if _, ok := dg.samples[kind]; !ok {
		dg.samples[kind] = parentID
	}
}

// empty 报告本 run 是否零挂载失败。
func (dg *execAlgoRunDiag) empty() bool {
	return dg == nil || len(dg.fails) == 0
}

// invalidReasonCodes 把计数收敛为两条稳定 Code（contracts/validity.go）：
//   - 未知算法名 → exec_algo_unknown_style（静态误配置，校验期拦下）；
//   - 其余各类 → exec_algo_attach_failed（挂载期拦下）。
//
// 输出定序固定（unknown_style 在前），同一输入两次 run 逐字节一致。
func (dg *execAlgoRunDiag) invalidReasonCodes() []string {
	if dg.empty() {
		return nil
	}
	var codes []string
	if dg.fails[execalgo.AttachFailureUnknownStyle] > 0 {
		codes = append(codes, contracts.InvalidExecAlgoUnknownStyle)
	}
	for kind, n := range dg.fails {
		if kind != execalgo.AttachFailureUnknownStyle && n > 0 {
			codes = append(codes, contracts.InvalidExecAlgoAttachFailed)
			break
		}
	}
	return codes
}

// logSummary 在会话结束（finalizeExecAlgo 之后）汇总挂载失败计数。
// Error 级：挂载失败不是「可忽略的 Warn 噪音」，它是「交易消失」的现场
// 记录——invalid 状态之外，这里是给排障者看的人类可读版。键序先排序，
// 不让 map 遍历序进日志。
func (dg *execAlgoRunDiag) logSummary(logger zerolog.Logger) {
	if dg.empty() {
		return
	}
	kinds := make([]string, 0, len(dg.fails))
	total := 0
	for kind, n := range dg.fails {
		kinds = append(kinds, string(kind))
		total += n
	}
	sort.Strings(kinds)
	evt := logger.Error().Int("attach_failures_total", total)
	for _, k := range kinds {
		kind := execalgo.AttachFailureKind(k)
		evt = evt.Int(k, dg.fails[kind]).Str(k+"_sample_parent", dg.samples[kind])
	}
	evt.Msg("exec-algo: parent order attach failures — signal trades dropped and counted in run diagnostics (result marked invalid; see invalid_reasons)")
}
