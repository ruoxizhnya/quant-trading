package contracts

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
)

// TestDefaultTradingConfig_Values (S7-P2-1) verifies every field of the
// default A-share trading config. If any upstream default changes
// (intentionally), this test must be updated in lockstep.
func TestDefaultTradingConfig_Values(t *testing.T) {
	cfg := DefaultTradingConfig()

	assert.Equal(t, DefaultStampTaxRate, cfg.StampTaxRate, "StampTaxRate must match the default constant")
	assert.Equal(t, DefaultMinCommission, cfg.MinCommission, "MinCommission must match the default constant")
	assert.Equal(t, DefaultTransferFeeRate, cfg.TransferFeeRate, "TransferFeeRate must match the default constant")
	assert.Equal(t, DefaultNewStockDays, cfg.NewStockDays, "NewStockDays must match the default constant")

	// PriceLimit sub-struct
	assert.Equal(t, DefaultPriceLimitNormal, cfg.PriceLimit.Normal, "PriceLimit.Normal must match the default constant")
	assert.Equal(t, DefaultPriceLimitST, cfg.PriceLimit.ST, "PriceLimit.ST must match the default constant")
	assert.Equal(t, DefaultPriceLimitNew, cfg.PriceLimit.New, "PriceLimit.New must match the default constant")
}

// TestDefaultTradingConfig_FeeConstantsAliasedToFees (S7-P2-1) is the
// contracts-level drift guard. It mirrors the parent-package
// constants_drift_test.go: the three fee constants here must stay
// const-aliased to pkg/fees so a rate change in pkg/fees (the canonical
// source) propagates to contracts automatically. If someone ever
// reverts a constant to an independent literal, this test fails.
//
// The parent-package test guards the alias chain
// (backtest.DefaultX -> contracts.DefaultX -> fees.DefaultX); this
// test guards the second hop directly.
func TestDefaultTradingConfig_FeeConstantsAliasedToFees(t *testing.T) {
	assert.Equal(t, fees.DefaultStampTaxRate, DefaultStampTaxRate,
		"contracts.DefaultStampTaxRate must alias fees.DefaultStampTaxRate")
	assert.Equal(t, fees.DefaultMinCommission, DefaultMinCommission,
		"contracts.DefaultMinCommission must alias fees.DefaultMinCommission")
	assert.Equal(t, fees.DefaultTransferFeeRate, DefaultTransferFeeRate,
		"contracts.DefaultTransferFeeRate must alias fees.DefaultTransferFeeRate")
}

// TestBacktestResponse_ZeroValue (S7-P2-1) is a smoke test that the
// BacktestResponse struct compiles and its JSON tags are intact. It
// also documents that a freshly-declared response carries no hidden
// sentinel values — every numeric field is zero-valued.
func TestBacktestResponse_ZeroValue(t *testing.T) {
	var resp BacktestResponse
	assert.Empty(t, resp.ID)
	assert.Empty(t, resp.Status)
	assert.Equal(t, 0.0, resp.TotalReturn)
	assert.Nil(t, resp.PortfolioValues)
	assert.Nil(t, resp.Trades)
}

// TestPriceLimitConfig_Fractions (S7-P2-1) documents the three
// A-share price-limit fractions so a typo (e.g. 0.5 instead of 0.05)
// is caught at the source.
func TestPriceLimitConfig_Fractions(t *testing.T) {
	assert.Equal(t, 0.10, DefaultPriceLimitNormal, "normal stocks ±10%")
	assert.Equal(t, 0.05, DefaultPriceLimitST, "ST stocks ±5%")
	assert.Equal(t, 0.20, DefaultPriceLimitNew, "new stocks ±20%")
}

// TestTradingDaysPerYear (S7-P2-1) guards the 252 convention used by
// the tracker for daily short-selling interest accrual. The live
// margin module uses 365 (natural days); the backtest engine advances
// one trading day at a time, so 252 is the correct divisor here.
func TestTradingDaysPerYear(t *testing.T) {
	assert.Equal(t, 252, TradingDaysPerYear, "backtest uses 252 trading days/year")
	assert.Equal(t, 0.106, DefaultShortSellingRate, "short-selling rate 10.6%/year per VISION.md")
}
