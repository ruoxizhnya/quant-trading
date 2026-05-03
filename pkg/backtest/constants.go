package backtest

import "time"

// A-Share trading rules (default values)
const (
	// StampTaxRate is the stamp tax rate for selling A-shares (0.1%)
	DefaultStampTaxRate = 0.001

	// MinCommission is the minimum commission per transaction (¥5)
	DefaultMinCommission = 5.0

	// TransferFeeRate is the transfer fee rate (0.001%)
	DefaultTransferFeeRate = 0.00001

	// PriceLimitNormal is the daily price limit for normal stocks (±10%)
	DefaultPriceLimitNormal = 0.10

	// PriceLimitST is the daily price limit for ST stocks (±5%)
	DefaultPriceLimitST = 0.05

	// PriceLimitNew is the daily price limit for new stocks on listing day (±20% for ChiNext/STAR)
	DefaultPriceLimitNew = 0.20

	// NewStockDays is the number of days a stock is considered "new" after IPO
	DefaultNewStockDays = 60
)

// Backtest engine operational constants
const (
	// DefaultInitialCapital is the default starting capital for backtests (¥1M)
	DefaultInitialCapital = 1_000_000.0

	// DefaultCommissionRate is the default broker commission rate (0.03%)
	DefaultCommissionRate = 0.0003

	// DefaultSlippageRate is the default slippage assumption (0.01%)
	DefaultSlippageRate = 0.0001

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
