// Package marketimpact — 平方根市场冲击模型。
//
// K5 切片 1：本包从 pkg/backtest/marketimpact **上提到顶层**（与 pkg/fees
// 平级）。归位理由：冲击是「执行/成本」子域的共享概念，不是回测的内部细节。
//
//   - K4 之前它只被 pkg/backtest/execution 的 "impact" 滑点分支消费，住在
//     backtest 下尚可自圆其说；
//   - K5 起 paper 侧（pkg/live 的 MockTrader）也要用它，与回测**同构**地
//     给大单算冲击成本。若仍留在 pkg/backtest 下，paper 就要反向依赖回测，
//     依赖方向错乱（成本模型是两者的**共同前置**，不是回测的下游）。
//
// 先例是 pkg/fees：同为「被多个平级域共用的成本模型」，被
// backtest / live / portfolio / domain / ai 共用，故独立成顶层包。本包同理。
// 包名不变（marketimpact），仅 import path 从
// `.../pkg/backtest/marketimpact` 变为 `.../pkg/marketimpact`。
package marketimpact

import "math"

// MarketImpactModel calculates volume-based slippage using the
// square-root impact model.
//
//	impact = sigma * sqrt(orderQty / ADV) * liquidityFactor
//
// where sigma is the daily volatility (e.g. 0.02 for 2%), ADV is the
// average daily volume of the instrument, and liquidityFactor is a
// tunable scaling constant (default 1.0).
type MarketImpactModel struct {
	Sigma           float64 // daily volatility (e.g. 0.02 for 2%)
	LiquidityFactor float64 // scaling factor (default 1.0)
}

// CalculateImpact returns the price impact as a fraction of price.
// Returns 0 when adv <= 0 (unknown liquidity) or orderQty <= 0 (no
// order), which also guards against NaN from a negative sqrt argument.
//
// A zero-value LiquidityFactor is treated as the documented default
// (1.0) so a model configured with only Sigma behaves as expected.
func (m *MarketImpactModel) CalculateImpact(orderQty, adv float64) float64 {
	if adv <= 0 || orderQty <= 0 {
		return 0
	}
	lf := m.LiquidityFactor
	if lf == 0 {
		lf = 1.0
	}
	return m.Sigma * math.Sqrt(orderQty/adv) * lf
}

// CalculateSlippageCost returns the absolute slippage cost in CNY.
//
//	cost = impact * orderQty * price
func (m *MarketImpactModel) CalculateSlippageCost(orderQty, adv, price float64) float64 {
	return m.CalculateImpact(orderQty, adv) * orderQty * price
}
