package domain

import "github.com/ruoxizhnya/quant-trading/pkg/fees"

// ExecutionConfig represents execution configuration
type ExecutionConfig struct {
	OrderType      OrderType `json:"order_type"`
	SlippageModel  string    `json:"slippage_model"`
	CommissionRate float64   `json:"commission_rate"`
	MinCommission  float64   `json:"min_commission"`
	InitialCapital float64   `json:"initial_capital"`

	// K4（D5）：平方根市场冲击模型的**可选**参数，仅当
	// SlippageModel == "impact" 时生效（pkg/backtest/execution）。
	//
	// 零值语义（对齐 pkg/backtest/marketimpact 的既有约定）：
	//   - ImpactSigma 零值 → 冲击 = 0（模型退化为无冲击；要启用大单
	//     冲击必须显式给出日波动率，如 0.02）。
	//   - ImpactLiquidityFactor 零值 → 1.0（市场冲击模型内部把
	//     LiquidityFactor==0 视为文档默认 1.0）。
	//
	// 用**可选字段**而非新增 SlippageModel 默认值的理由：默认回测行为
	// 必须零变化（硬约束）——默认 SlippageModel 仍是 "fixed"，这两个零值
	// 字段对既有路径无任何影响。omitempty 让旧 JSON 配置序列化不变。
	ImpactSigma           float64 `json:"impact_sigma,omitempty"`
	ImpactLiquidityFactor float64 `json:"impact_liquidity_factor,omitempty"`
}

// DefaultExecutionConfig returns default execution configuration.
//
// S7-P1-4 (ODR-043, D1): Previously hardcoded CommissionRate = 0.00025,
// which diverged from the canonical fees.DefaultCommissionRate (0.0003).
// The 0.00025 literal was a stale value that caused backtest-vs-live
// P&L drift. Fee fields now source from pkg/fees (single source of truth).
func DefaultExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		OrderType:      OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: fees.DefaultCommissionRate,
		MinCommission:  fees.DefaultMinCommission,
		InitialCapital: 1000000,
	}
}
