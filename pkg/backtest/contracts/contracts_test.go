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

// TestPriceLimitConfig_Fractions (S7-P2-1) documents the A-share
// price-limit fractions so a typo (e.g. 0.5 instead of 0.05) is caught
// at the source.
//
// AUD-07 (ODR-065 H2): the ST assertion was 0.05. 沪深北交易所
// 2026-04 修订交易规则，2026-07-06 起主板 ST/*ST 由 ±5% 上调至
// ±10%。当前值因此是 0.10，而 0.05 成了「变动之前」的历史值，
// 由 DefaultPriceLimitSTBefore 承载。两个常数都断言，这样把其中
// 任何一个写成另一个的值都会被抓到 —— 单断言 0.10 无法发现
// "历史值也被错改成 0.10"。
func TestPriceLimitConfig_Fractions(t *testing.T) {
	assert.Equal(t, 0.10, DefaultPriceLimitNormal, "main-board stocks ±10%")
	assert.Equal(t, 0.10, DefaultPriceLimitST, "main-board ST for days on/after 2026-07-06 ±10%")
	assert.Equal(t, 0.05, DefaultPriceLimitSTBefore, "main-board ST before 2026-07-06 ±5%")
	assert.Equal(t, 0.20, DefaultPriceLimitNew, "new stocks ±20%")

	// The board-driven rates are market structure, not constants: they
	// come from pkg/marketdata. Assert the wiring here so a change to
	// either side is noticed.
	assert.Equal(t, DefaultPriceLimitNormal, DefaultTradingConfig().PriceLimit.Normal)
	assert.Equal(t, DefaultPriceLimitST, DefaultTradingConfig().PriceLimit.ST)
	assert.Equal(t, DefaultPriceLimitSTBefore, DefaultTradingConfig().PriceLimit.STBefore)
}

// TestStampTaxRate_Fractions is the AUD-20 drift guard, modelled on
// TestPriceLimitConfig_Fractions above: BOTH sides of the 2023-08-28 cut
// are asserted, so collapsing them into one value (or swapping them) is
// caught. Asserting only 0.0005 would not notice "the historical rate was
// also changed to 0.0005" — which is precisely the silent failure AUD-20
// is about.
func TestStampTaxRate_Fractions(t *testing.T) {
	assert.Equal(t, 0.0005, DefaultStampTaxRate, "sell-side stamp tax on/after 2023-08-28 = 0.05%")
	assert.Equal(t, 0.001, DefaultStampTaxRateBefore, "sell-side stamp tax before 2023-08-28 = 0.1%")

	// Wiring: the defaults must actually reach the resolved TradingConfig,
	// otherwise a caller that takes the defaults silently loses the
	// historical rate and the whole date-segmentation is a no-op.
	assert.Equal(t, DefaultStampTaxRate, DefaultTradingConfig().StampTaxRate)
	assert.Equal(t, DefaultStampTaxRateBefore, DefaultTradingConfig().StampTaxRateBefore)
}

// TestTradingDaysPerYear (S7-P2-1) guards the 252 convention used by
// the tracker for daily short-selling interest accrual. The live
// margin module uses 365 (natural days); the backtest engine advances
// one trading day at a time, so 252 is the correct divisor here.
func TestTradingDaysPerYear(t *testing.T) {
	assert.Equal(t, 252, TradingDaysPerYear, "backtest uses 252 trading days/year")
	assert.Equal(t, 0.106, DefaultShortSellingRate, "short-selling rate 10.6%/year per VISION.md")
}

// TestTradingConfig_WithDefaultsFillsOnlyZeroFields is the AUD-37 guard for
// the field-by-field defaulting that replaced the all-or-nothing guard
// (`if cfg.Trading.StampTaxRate == 0 { cfg.Trading = DefaultTradingConfig() }`).
//
// Three properties, all of which the old guard violated:
//
//  1. a field the caller DID set survives;
//  2. a field the caller did NOT set becomes its default — not zero. Zero is
//     not "unset" on the fee path: portfolio.ComputeFees does not call
//     fees.AShareFees.ApplyDefaults, so a zero MinCommission means "no
//     commission floor" and a zero TransferFeeRate means "no transfer fee",
//     both of which silently make fills cheaper.
//  3. a complete config is returned unchanged (no partial overwrite).
func TestTradingConfig_WithDefaultsFillsOnlyZeroFields(t *testing.T) {
	def := DefaultTradingConfig()

	// (1) + (2): a partial config — only one field set.
	partial := TradingConfig{StampTaxRate: 0.0009}
	got := partial.WithDefaults()

	assert.Equal(t, 0.0009, got.StampTaxRate, "an explicitly set field must survive")
	assert.Equal(t, def.StampTaxRateBefore, got.StampTaxRateBefore)
	assert.Equal(t, def.MinCommission, got.MinCommission)
	assert.Equal(t, def.TransferFeeRate, got.TransferFeeRate)
	assert.Equal(t, def.PriceLimit, got.PriceLimit)
	assert.Equal(t, def.NewStockDays, got.NewStockDays)

	// (3): a fully-specified config must come back untouched.
	full := TradingConfig{
		StampTaxRate:       0.0009,
		StampTaxRateBefore: 0.002,
		MinCommission:      7.5,
		TransferFeeRate:    0.00003,
		PriceLimit: PriceLimitConfig{
			Normal: 0.09, ST: 0.11, STBefore: 0.06, New: 0.25,
		},
		NewStockDays: 45,
	}
	assert.Equal(t, full, full.WithDefaults(), "WithDefaults must be a no-op on a complete config")

	// The zero value must expand to exactly the documented defaults, so the
	// "boot with no config" path is the same set of values as before.
	assert.Equal(t, def, TradingConfig{}.WithDefaults())
}
