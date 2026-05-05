package domain

// ExecutionConfig represents execution configuration
type ExecutionConfig struct {
	OrderType      OrderType `json:"order_type"`
	SlippageModel  string    `json:"slippage_model"`
	CommissionRate float64   `json:"commission_rate"`
	MinCommission  float64   `json:"min_commission"`
	InitialCapital float64   `json:"initial_capital"`
}

// DefaultExecutionConfig returns default execution configuration
func DefaultExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		OrderType:      OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: 0.00025,
		MinCommission:  5.0,
		InitialCapital: 1000000,
	}
}
