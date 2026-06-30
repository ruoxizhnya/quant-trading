package backtest

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
)

// TestBacktestFeeConstants_AliasedToFees (S7-P1-4, ODR-043)
//
// pkg/backtest previously defined DefaultCommissionRate / DefaultStampTaxRate
// / DefaultTransferFeeRate / DefaultMinCommission / DefaultSlippageRate as
// independent literals that happened to match pkg/fees. That is a drift
// hazard: changing the rate in pkg/fees (the canonical source) would NOT
// propagate to pkg/backtest, causing backtest-vs-live P&L divergence.
//
// After S7-P1-4 the backtest constants are const-aliased to fees.Default*.
// This test guards the aliasing: if someone ever reverts a constant to an
// independent literal, this test fails immediately.
func TestBacktestFeeConstants_AliasedToFees(t *testing.T) {
	assert.Equal(t, fees.DefaultCommissionRate, DefaultCommissionRate,
		"backtest.DefaultCommissionRate must alias fees.DefaultCommissionRate")
	assert.Equal(t, fees.DefaultStampTaxRate, DefaultStampTaxRate,
		"backtest.DefaultStampTaxRate must alias fees.DefaultStampTaxRate")
	assert.Equal(t, fees.DefaultTransferFeeRate, DefaultTransferFeeRate,
		"backtest.DefaultTransferFeeRate must alias fees.DefaultTransferFeeRate")
	assert.Equal(t, fees.DefaultMinCommission, DefaultMinCommission,
		"backtest.DefaultMinCommission must alias fees.DefaultMinCommission")
	assert.Equal(t, fees.DefaultSlippageRate, DefaultSlippageRate,
		"backtest.DefaultSlippageRate must alias fees.DefaultSlippageRate")
}
