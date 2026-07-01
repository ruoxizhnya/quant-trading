package backtest

import (
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
)

// S7-P2-1: A-share trading-rule constants (DefaultStampTaxRate,
// DefaultMinCommission, DefaultTransferFeeRate, DefaultPriceLimitNormal/ST/New,
// DefaultNewStockDays, DefaultShortSellingRate, TradingDaysPerYear) moved to
// pkg/backtest/contracts/contracts.go. Re-exported here via aliases.go.
// The drift guard test in constants_drift_test.go still passes because the
// aliases resolve to the same fees.Default* values.

// Backtest engine operational constants
const (
	// DefaultInitialCapital is the default starting capital for backtests (¥1M)
	DefaultInitialCapital = 1_000_000.0

	// DefaultCommissionRate is the default broker commission rate (0.03%)
	DefaultCommissionRate = fees.DefaultCommissionRate

	// DefaultSlippageRate is the default slippage assumption (0.01%)
	DefaultSlippageRate = fees.DefaultSlippageRate

	// DefaultRiskFreeRate is the annual risk-free rate (3%, approx. Chinese bond yield)
	DefaultRiskFreeRate = 0.03
)

// Polling and timeout constants
const (
	// JobPollInterval is how often to poll for async job status updates
	JobPollInterval = 2 * time.Second

	// JobPollTimeout is the maximum time to wait for a job to complete
	JobPollTimeout = 5 * time.Minute

	// MaxJobPollAttempts is the maximum number of polling attempts before giving up
	MaxJobPollAttempts = 150 // 5min / 2s ≈ 150 attempts
)
