package live

// K17：exec-algo 接进 **paper** 下单主路径（TASKS.md K17）。
//
// ─── 接线点（两条下单主路径之二）────────────────────────────────────
//
// paper 侧的订单路径是 PaperSession.Run 的日循环：
//
//	generateSignals → toDomainSignals → executeSignal → LiveTrader.SubmitOrder
//
// K17 的改法：信号若指定了 exec_algo（domain.Signal.ExecAlgo 非空），
// 不当场整单报送，而是把父单挂到本会话的 execalgo.Dispatcher（Schedule
// 拆单）；随后每个交易日由 dispatchExecAlgoChildren 把「当日到点」的子单
// 逐笔经**同一个** LiveTrader 报送（MockTrader 的同一成本核，含 impact
// ——大单拆细后单笔冲击下降，即 D5「模拟真实大单」的正证据），OnFill
// 回填父单进度。未指定 exec_algo 的信号走原路径，逐字节不变。
//
// ─── 与回测侧的关系（口径一致，代码不共享）──────────────────────────
//
// 窗口口径、父单号派生、撤销裁决与 pkg/backtest/exec_algo.go **逐条
// 一致**（同一个 ResolveExecWindow / 同一种 EA-日期-symbol-方向纯函数），
// 但代码不共享：pkg/live 不能 import pkg/backtest（live_bridge.go 已
// backtest → live，反向即循环）。两侧靠「同一算法包 pkg/execalgo +
// 同一裁决注释」保持一致，靠 e2e 测试钉住数值。
//
// ─── LiveTrader / MockTrader 签名不动（边界）────────────────────────
//
// 子单只是「更小的委托」：SubmitOrder(ctx, symbol, direction, orderType,
// quantity, price) 原样调用，qty 取子单量。子单号（(父单号, 片序号)
// 纯函数）只活在派发器与 OnFill 回填里——PaperFill 刻意不含 OrderID
// （成交流确定性契约，见 paper_session.go），MockTrader 内部为每笔子单
// 生成自己的 broker 侧 ID（非确定，但不进成交流，故不回放结果仍确定）。
//
// ─── 会话结束裁决（K17 要求 3：显式裁决）────────────────────────────
//
// 回放结束（日循环耗尽）时仍未报完的父单：**撤销**剩余子单。理由与回测
// 侧相同（pkg/backtest/exec_algo.go finalizeExecAlgo 注释）：没有更多
// bar 时「强制撮完」等于虚构本未发生的成交；「撤销」停在部分成交，是
// 保守且可解释的终态。两条路径同一裁决。
import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
)

// execAlgoParentID 派生父单号：纯函数 (日期, symbol, 方向)——与回测侧
// pkg/backtest/exec_algo.go 的同名函数同规则（禁时间戳派生，K17 验收 4）。
func execAlgoParentID(date time.Time, symbol string, dir domain.Direction) string {
	return fmt.Sprintf("EA-%s-%s-%s", date.Format("20060102"), symbol, string(dir))
}

// attachExecAlgo 把「指定了执行算法」的信号挂到派发器（拆单）。
// 调用方已判定 sig.ExecAlgo != ""；挂载失败（未知算法 / 父单号重复 /
// Schedule 非法输入）返回 error，由调用方记 Warn 并跳过该信号。
func (s *PaperSession) attachExecAlgo(sig domain.Signal, d time.Time) error {
	if sig.ExecAlgo == "" {
		return fmt.Errorf("paper session: attachExecAlgo called without exec algo (caller bug)")
	}
	n, end := execalgo.ResolveExecWindow(d, sig.ExecSlices, sig.ExecProfile)
	orderType := sig.OrderType
	if orderType == "" {
		orderType = domain.OrderTypeMarket
	}
	// paper 路径的委托量口径：signalQuantity（与 executeSignal 同一函数），
	// 保证「算法单与整单是同一笔意图的不同执行方式」。
	spec := execalgo.AttachSpec{
		ParentID:   execAlgoParentID(d, sig.Symbol, sig.Direction),
		Symbol:     sig.Symbol,
		Side:       sig.Direction,
		Qty:        signalQuantity(sig.Strength),
		LimitPrice: sig.LimitPrice,
		Style:      sig.ExecAlgo,
		Slices:     n,
		Profile:    sig.ExecProfile,
		OrderType:  orderType,
		StartAt:    d,
		EndAt:      end,
	}
	if _, err := s.execAlgo.Attach(context.Background(), spec); err != nil {
		return err
	}
	return nil
}

// dispatchExecAlgoChildren 把**当日**到点的 exec-algo 子单经同一
// LiveTrader 报送并回填进度，返回本日产生的子单成交（PaperFill）。
//
// 调用时机：每个交易日信号循环之后（新挂父单的首片 SubmitAt = 信号当日，
// 当日到点即发，与回测侧 processExecAlgoChildren 同序）。
func (s *PaperSession) dispatchExecAlgoChildren(ctx context.Context, d time.Time) []PaperFill {
	if s.execAlgo.Len() == 0 {
		return nil
	}
	var fills []PaperFill
	for _, sym := range s.execAlgo.Symbols() {
		q, ok := s.currentBars[sym]
		if !ok {
			// 当日无 bar（停牌）：不到点，父单剩余子单顺延。
			continue
		}
		due, err := s.execAlgo.OnBar(ctx, quoteToBar(q))
		if err != nil {
			s.logger.Warn().Err(err).Str("symbol", sym).
				Msg("paper session: exec-algo OnBar failed, parent slices stall")
			continue
		}
		for _, dc := range due {
			if fill := s.submitExecAlgoChild(ctx, d, dc); fill != nil {
				fills = append(fills, *fill)
			}
		}
	}
	return fills
}

// submitExecAlgoChild 报送一笔到点子单，产出 PaperFill 并回填父单进度。
//
// 价格口径与 executeSignal 一致：市价单 price=0（MockTrader 经
// CurrentBarSource 取当根 bar Close）；限价单缺价时退回当根 Close。
func (s *PaperSession) submitExecAlgoChild(ctx context.Context, d time.Time, dc execalgo.DueChild) *PaperFill {
	price := 0.0
	if dc.OrderType == domain.OrderTypeLimit {
		price = dc.LimitPrice
		if price <= 0 {
			if q, ok := s.currentBars[dc.Symbol]; ok {
				price = q.Close
			}
		}
	}
	result, err := s.trader.SubmitOrder(ctx, dc.Symbol, dc.Side, dc.OrderType, dc.Qty, price)
	if err != nil {
		// 单笔子单失败（现金不足 / T+1）不中断回放——与 executeSignal 的
		// 「单信号失败不中断」一致；父单进度缺一块，会话结束显式呈现。
		s.logger.Warn().Err(err).
			Str("date", d.Format("2006-01-02")).Str("symbol", dc.Symbol).
			Str("child_id", dc.OrderID).Str("parent_id", dc.ParentID).
			Msg("paper session: exec-algo child order skipped")
		return nil
	}
	if result == nil {
		return nil
	}

	// OnFill 先记账：超额 fail-loud 时该笔不进成交流（正常接线里不可达，
	// 纵深防御，与回测侧 executeExecAlgoChild 同序）。
	if err := s.execAlgo.OnFill(execalgo.Fill{
		OrderID: dc.OrderID,
		Symbol:  dc.Symbol,
		Side:    dc.Side,
		Qty:     result.FilledQty,
		Price:   result.FillPrice,
		Ts:      d,
	}); err != nil {
		s.logger.Error().Err(err).
			Str("child_id", dc.OrderID).Str("parent_id", dc.ParentID).
			Msg("paper session: exec-algo parent order over-filled — child fill NOT recorded (fail-loud)")
		return nil
	}

	fillPrice := result.FillPrice
	if fillPrice <= 0 {
		fillPrice = result.Price // 兜底：未回填 FillPrice 的 trader
	}
	return &PaperFill{
		Date:   d,
		Symbol: dc.Symbol,
		Side:   result.Direction,
		Qty:    result.FilledQty,
		Price:  fillPrice,
		Fee:    result.Fee,
	}
}

// finalizeExecAlgo 回放结束时的显式裁决：**撤销**未报完父单的剩余子单
// （K17 要求 3）。裁决理由见文件头注释与回测侧 finalizeExecAlgo——
// 「撤销」而非「强制撮完」，两条路径同一裁决。
func (s *PaperSession) finalizeExecAlgo() {
	for _, p := range s.execAlgo.Unfinished() {
		s.logger.Warn().
			Str("parent_id", p.ParentID).Str("symbol", p.Symbol).
			Float64("filled_qty", p.Filled).Float64("parent_qty", p.Total).
			Float64("cancelled_qty", p.Total-p.Filled).
			Msg("paper session: exec-algo parent order unfinished at session end — remaining child slices CANCELLED (adjudication: cancel, not force-fill; see pkg/live/paper_exec_algo.go)")
	}
}

// quoteToBar 把当根 bar 快照（marketdata.Quote）换成算法要的 domain.OHLCV。
// 算法只用 Date 推进节奏、Symbol 路由——其余字段带上是为将来成本模型
// 直读 bar 时不必再改接线。
func quoteToBar(q Quote) domain.OHLCV {
	return domain.OHLCV{
		Symbol: q.Symbol,
		Date:   q.Timestamp,
		Open:   q.Open,
		High:   q.High,
		Low:    q.Low,
		Close:  q.Close,
		Volume: float64(q.Volume),
	}
}
