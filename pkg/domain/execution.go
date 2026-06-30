package domain

import "github.com/ruoxizhnya/quant-trading/pkg/fees"

// ExecutionConfig represents execution configuration
type ExecutionConfig struct {
	OrderType      OrderType `json:"order_type"`
	SlippageModel  string    `json:"slippage_model"`
	CommissionRate float64   `json:"commission_rate"`
	MinCommission  float64   `json:"min_commission"`
	InitialCapital float64   `json:"initial_capital"`
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
