package backtest

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
