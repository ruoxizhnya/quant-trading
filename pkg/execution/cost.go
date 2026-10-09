// Package execution — 回测与 paper **共用同一段代码**的执行成本核。
//
// ─── 为什么要有这个包（K5 切片 1 的读法 A 判据） ──────────────────────
//
// 读法 A：paper 与回测必须吃**同一批输入**，差异才只可能来自执行机制
// （时钟/broker/成本）⇒ 对账才可归因。若 paper 与回测各写一份滑点/费用
// 公式，那么「差出来的那部分 P&L」既可能是执行机制不同，也可能是两份
// 公式在某个边界上不一致 —— 归因就断了。所以成本公式**只有一份**，
// 住在 pkg/execution，被 pkg/backtest/execution 与 pkg/live 共同调用。
//
// ─── 落点裁决 ─────────────────────────────────────────────────────
//
// 任务给了两个候选：pkg/live/paper_cost.go 或与 pkg/backtest/execution
// 共享的新包。选**新顶层包 pkg/execution**，理由：
//   - 若放 pkg/live，pkg/backtest/execution 就要反向依赖 pkg/live，把
//     「成本模型」塞进「实盘」的语义里（backtest → live 的依赖方向本身
//     就错，虽然 live_bridge.go 已开了这个口子，但不该再扩大）；
//   - pkg/execution 是平级共享域，与 pkg/fees / pkg/marketimpact /
//     pkg/portfolio 同型 —— 正好是「被多个平级域共用的成本模型」的定位。
//
// ─── 与 pkg/fees / pkg/marketimpact 的关系 ────────────────────────
//
//   - 费用费率来自 pkg/fees（FixedSlippageRate / CommissionRate /
//     MinCommission），本包不自造任何费率常量；
//   - 市场冲击来自 pkg/marketimpact（平方根模型），本包只负责把它接到
//     配置（ExecutionConfig.ImpactSigma / ImpactLiquidityFactor）上。
//
// 因此「改费率」「改冲击公式」各自只有一个落点，paper 与回测同步生效。
//
// ─── 行为等价（硬约束） ───────────────────────────────────────────
//
// 本包的 SlippagePrice / Commission 逐位复刻 pkg/backtest/execution
// 改动前的 applySlippage / calculateCommission。K5 只做「搬家」，不改
// 回测默认行为：默认 SlippageModel 仍是 "fixed"，其值仍是
// fees.FixedSlippageRate。
package execution

import (
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/marketimpact"
)

// CostModel 是单一执行成本核：给定参考价 + 配置，算出成交价与佣金。
//
// 零值不可用——务必经 NewCostModel 构造，以便显式携带 ExecutionConfig。
// 它无内部可变状态，可安全被并发共享。
type CostModel struct {
	config domain.ExecutionConfig
}

// NewCostModel 从执行配置构造成本核。
func NewCostModel(config domain.ExecutionConfig) *CostModel {
	return &CostModel{config: config}
}

// Config 返回构造时携带的执行配置（只读副本）。
func (m *CostModel) Config() domain.ExecutionConfig { return m.config }

// SlippageModel 返回当前滑点模型名（"fixed"/"variable"/"impact"/"none"/空）。
func (m *CostModel) SlippageModel() string { return m.config.SlippageModel }

// SlippagePrice 把配置里的滑点模型作用到参考价上，返回成交价。
//
// 参数语义（与 pkg/backtest/execution 的 applySlippage 逐字对齐）：
//   - price    参考价（回测/paper 均为当根 bar 的 Close）；
//   - orderQty 本笔委托量，供 "impact" 模型按 orderQty/ADV 定冲击；
//   - adv      平均日成交量代理（本切片用当根 volume 近似，见下）；
//   - high/low 当根 bar 的高低价，供 "variable" 模型算波动率。
//
// 各分支语义：
//   - "fixed"    ：price*(1±fees.FixedSlippageRate)，买入加、卖出减；
//   - "variable" ：volatility=(high-low)/price，步长 = volatility*0.1；
//   - "impact"   ：impact=sigma*sqrt(orderQty/adv)*liquidityFactor；
//     **adv<=0 时退化为 fixed**（不静默返回零冲击——见下）；
//   - "none"     ：不加滑点，直接返回 price；
//   - 其它/空    ：返回 price（未知模型不臆造滑点）。
//
// 为什么 impact 在 adv<=0 时退化为 fixed 而不是零：零会被下游读成
// 「无成交量数据 = 无冲击 = 成本为零」，是危险的乐观偏差。退化到 fixed
// 至少保持一个保守 haircut。该语义与 pkg/backtest/execution 一致。
func (m *CostModel) SlippagePrice(price float64, direction domain.Direction, orderQty, adv, high, low float64) float64 {
	switch m.config.SlippageModel {
	case "fixed":
		slippage := fees.FixedSlippageRate
		if direction == domain.DirectionLong {
			return price * (1 + slippage)
		}
		return price * (1 - slippage)
	case "variable":
		volatility := (high - low) / price
		slippage := volatility * 0.1
		if direction == domain.DirectionLong {
			return price * (1 + slippage)
		}
		return price * (1 - slippage)
	case "impact":
		if adv <= 0 {
			slippage := fees.FixedSlippageRate
			if direction == domain.DirectionLong {
				return price * (1 + slippage)
			}
			return price * (1 - slippage)
		}
		model := marketimpact.MarketImpactModel{
			Sigma:           m.config.ImpactSigma,
			LiquidityFactor: m.config.ImpactLiquidityFactor,
		}
		impact := model.CalculateImpact(orderQty, adv)
		if direction == domain.DirectionLong {
			return price * (1 + impact)
		}
		return price * (1 - impact)
	case "none":
		return price
	default:
		return price
	}
}

// Commission 按成交额与配置里的费率算佣金（含最低佣金下限）。
//
//	commission = max(notional*CommissionRate, MinCommission)
//
// 与 pkg/backtest/execution 的 calculateCommission 逐字一致。注意：
// 这只是**执行佣金**；A 股另有印花税与过户费（pkg/fees / portfolio.ComputeFees
// 里的完整口径），paper 侧 MockTrader 会在此基础上补足，回测撮合服务
// 不建模这两项 —— 这是两侧**共同已知**的口径边界，不是公式漂移。
func (m *CostModel) Commission(notional float64) float64 {
	commission := notional * m.config.CommissionRate
	if commission < m.config.MinCommission {
		commission = m.config.MinCommission
	}
	return commission
}

// ExecuteMarket 是市价单的完整成本核算：一次给出成交价与佣金。
//
// 这是「(order, bar, config, adv) → (成交价, 费用)」的单入口，paper 与
// 回测的市价路径都走它：
//
//	refPrice := bar.Close
//	fillPrice, commission := core.ExecuteMarket(order, refPrice, bar.High, bar.Low, adv)
//
// quantity<=0 时返回 error（与回测一致：无量的单不该被定价）。
func (m *CostModel) ExecuteMarket(order domain.Order, refPrice, high, low, adv float64) (fillPrice, commission float64, err error) {
	if order.Quantity <= 0 {
		return 0, 0, fmt.Errorf("execution: invalid order quantity: %f", order.Quantity)
	}
	fillPrice = m.SlippagePrice(refPrice, order.Direction, order.Quantity, adv, high, low)
	commission = m.Commission(fillPrice * order.Quantity)
	return fillPrice, commission, nil
}
