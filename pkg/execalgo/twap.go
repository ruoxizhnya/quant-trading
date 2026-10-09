// K4：TWAP（Time-Weighted Average Price）执行算法。
//
// 把父订单按 [StartAt, EndAt] 均分成 n 个等长时间片，每片等量，到点
// 报送。语义见 docs/design/kernel/target-architecture-modular-kernel.md
// §8 UC4（父订单 → Schedule 拆子订单 → 每片过风控 → broker）。
//
// 本文件只实现算法本身（Schedule / OnBar / OnFill），**不接发单主路径**
// （发单接线属 K5/后续）——Schedule 是纯排程，返回计划由调用方逐片过
// risk.RiskEngine.CheckOrder 后报送。
package execalgo

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// TWAP 是时间加权拆单算法。
//
// 片数策略（裁决）：NewTWAP(slices) 中 slices > 0 → 固定片数；
// slices <= 0 → 按窗口时长 / DefaultSliceInterval 推导（见 schedule.go）。
type TWAP struct {
	slices int
	st     planState
}

// NewTWAP 构造 TWAP 算法。slices <= 0 表示「按窗口时长自动推导片数」。
func NewTWAP(slices int) *TWAP {
	return &TWAP{slices: slices}
}

// Name 返回注册表键。
func (a *TWAP) Name() string { return "twap" }

// Schedule 把父订单排成等时、等量的子订单计划。
//
// Σ 子单 Qty == parent.Qty（整数手逐位精确）；SubmitAt 严格等差。
// 子单号幂等：同一 (parent.OrderID, 片序号) 两次 Schedule 产出同一串 ID。
// 非法输入（Qty<=0 / EndAt<=StartAt / Symbol==""）→ error。
func (a *TWAP) Schedule(ctx context.Context, parent ParentOrder) ([]ChildOrder, error) {
	if err := validateParent(parent); err != nil {
		return nil, err
	}

	n := a.slices
	if n <= 0 {
		n = deriveSlices(parent.EndAt.Sub(parent.StartAt))
	}
	n = effectiveSlices(n, parent.Qty)

	// TWAP：等权。allocateQty 保证 Σ == parent.Qty。
	weights := make([]float64, n)
	for i := range weights {
		weights[i] = 1.0 / float64(n)
	}
	qtys := allocateQty(parent.Qty, weights)
	times := splitWindow(parent.StartAt, parent.EndAt, n)
	plan := buildPlan(parent, qtys, times)

	a.st.reset(parent, plan)
	return plan, nil
}

// OnBar 推进拆单节奏：到点的片进入待报送缓冲。
func (a *TWAP) OnBar(ctx context.Context, bar domain.OHLCV) error {
	return a.st.onBar(bar)
}

// OnFill 累计成交进度。
func (a *TWAP) OnFill(ctx context.Context, f Fill) error {
	return a.st.onFill(f)
}

// DueChildOrders 取走并清空已到点、待报送的子单。
func (a *TWAP) DueChildOrders() []ChildOrder { return a.st.dueChildOrders() }

// Progress 返回 (已成交量, 总量, 是否全部成交)。
func (a *TWAP) Progress() (filled, total float64, done bool) { return a.st.progress() }

// 编译期守卫：TWAP 满足 ExecAlgorithm（K0 冻结契约）。
var _ ExecAlgorithm = (*TWAP)(nil)
