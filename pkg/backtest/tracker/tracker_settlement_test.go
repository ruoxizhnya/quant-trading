package tracker

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
	"github.com/ruoxizhnya/quant-trading/pkg/settlement"
	"github.com/stretchr/testify/assert"
)

// Two fixed trading days either side of the 2023-08-28 stamp-tax cut
// (fees.StampTaxCutDate), used by the AUD-20 guards below.
var (
	stampTaxPreCutDay  = time.Date(2023, 8, 25, 0, 0, 0, 0, time.UTC)
	stampTaxPostCutDay = time.Date(2023, 9, 1, 0, 0, 0, 0, time.UTC)
)

// TestS7P1_1_TrackerUsesSharedFeePrimitive — S7-P1-1 regression guard.
//
// Prior to S7-P1-1, tracker.go inlined the fee formula in 5 sites:
//
//	commission := max(tradeValue*t.commissionRate, t.trading.MinCommission)
//	transferFee := tradeValue * t.trading.TransferFeeRate
//	stampTax    := tradeValue * t.trading.StampTaxRate   (sell only)
//
// and the IsFlat threshold in 7 sites as `abs(x) < 1e-8`. Both diverged
// from mock_trader.go's copy. This test locks the refactored tracker to
// the shared primitives so a future edit cannot silently reintroduce the
// inlined formula.
func TestS7P1_1_TrackerUsesSharedFeePrimitive(t *testing.T) {
	tracker := NewTracker(
		1_000_000,
		fees.DefaultCommissionRate,
		fees.DefaultSlippageRate,
		contracts.DefaultTradingConfig(),
		zerolog.Nop(),
	)

	sched := tracker.feeSchedule(stampTaxPostCutDay)
	assert.Equal(t, fees.DefaultCommissionRate, sched.CommissionRate)
	assert.Equal(t, fees.DefaultStampTaxRate, sched.StampTaxRate)
	assert.Equal(t, fees.DefaultTransferFeeRate, sched.TransferFeeRate)
	assert.Equal(t, fees.DefaultMinCommission, sched.MinCommission)

	// The shared primitive must produce the same numbers as the old
	// inlined formula for both buy and sell sides.
	const tradeValue = 100_000.0
	buyFb := portfolio.ComputeFees(tradeValue, false, sched)
	assert.Greater(t, buyFb.Commission, 0.0)
	assert.Greater(t, buyFb.TransferFee, 0.0)
	assert.Zero(t, buyFb.StampTax, "buys do not incur stamp tax")
	assert.InDelta(t, tradeValue*fees.DefaultCommissionRate, buyFb.Commission, 1e-9)
	assert.InDelta(t, tradeValue*fees.DefaultTransferFeeRate, buyFb.TransferFee, 1e-9)

	sellFb := portfolio.ComputeFees(tradeValue, true, sched)
	assert.InDelta(t, tradeValue*fees.DefaultStampTaxRate, sellFb.StampTax, 1e-9)
	assert.Equal(t, buyFb.Commission, sellFb.Commission, "commission is direction-agnostic")
	assert.Equal(t, buyFb.TransferFee, sellFb.TransferFee, "transfer fee is direction-agnostic")
}

// TestTracker_StampTaxIsDateSegmented — AUD-20 regression guard.
//
// Before AUD-20, feeSchedule() took no date and read t.trading.StampTaxRate
// directly, so a backtest spanning the 2023-08-28 halving charged the
// post-cut 0.05% to every sell in the window. That UNDERSTATES cost — and
// therefore OVERSTATES return — for the pre-cut part, which is the
// dangerous direction for a project whose goal is calibration.
//
// This test pins the WIRING (feeSchedule must pass asOf through), not the
// resolver — fees.StampTaxRateFor has its own table test. It fails if the
// date stops being threaded, even while the resolver stays correct.
func TestTracker_StampTaxIsDateSegmented(t *testing.T) {
	tracker := NewTracker(
		1_000_000,
		fees.DefaultCommissionRate,
		fees.DefaultSlippageRate,
		contracts.DefaultTradingConfig(),
		zerolog.Nop(),
	)

	preCut := tracker.feeSchedule(stampTaxPreCutDay)
	postCut := tracker.feeSchedule(stampTaxPostCutDay)

	assert.InDelta(t, fees.DefaultStampTaxRateBefore, preCut.StampTaxRate, 1e-12,
		"a sell before 2023-08-28 must pay 0.1%%")
	assert.InDelta(t, fees.DefaultStampTaxRate, postCut.StampTaxRate, 1e-12,
		"a sell on/after 2023-08-28 must pay 0.05%%")

	// Absolute money assertion through the real fee primitive, so the
	// number is human-checkable: 100,000 CNY sold pays 100 CNY before the
	// cut and 50 CNY after.
	const sellValue = 100_000.0
	assert.InDelta(t, 100.0, portfolio.ComputeFees(sellValue, true, preCut).StampTax, 1e-9,
		"pre-cut sell of 100k CNY -> 100 CNY stamp tax")
	assert.InDelta(t, 50.0, portfolio.ComputeFees(sellValue, true, postCut).StampTax, 1e-9,
		"post-cut sell of 100k CNY -> 50 CNY stamp tax")

	// A window that does NOT span the cut must be unaffected: both days
	// after the cut resolve to the same rate.
	laterDay := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, postCut.StampTaxRate, tracker.feeSchedule(laterDay).StampTaxRate,
		"a post-cut-only window must not change")
}

// TestTracker_StampTaxBeforeFallsBackToConstant — a caller that supplies
// only the current rate (leaving StampTaxRateBefore at its zero value)
// must still get the historical rate before the cut, rather than a free
// sell. Mirrors TestResolvePriceLimit_STBeforeZeroFallsBack.
func TestTracker_StampTaxBeforeFallsBackToConstant(t *testing.T) {
	// StampTaxRate is non-zero, so NewTracker keeps this config as-is
	// instead of replacing it with DefaultTradingConfig().
	tracker := NewTracker(
		1_000_000,
		fees.DefaultCommissionRate,
		fees.DefaultSlippageRate,
		contracts.TradingConfig{StampTaxRate: fees.DefaultStampTaxRate},
		zerolog.Nop(),
	)

	got := tracker.feeSchedule(stampTaxPreCutDay).StampTaxRate
	assert.InDelta(t, fees.DefaultStampTaxRateBefore, got, 1e-12,
		"unset StampTaxRateBefore must fall back to the historical constant")
}

// TestS7P1_1_TrackerSettlementFlatThreshold — locks the IsFlat threshold
// so the S7-P0-17 ghost-position guard cannot regress to a different
// constant. If settlement.FlatThreshold changes, this test fails loudly
// rather than silently re-opening the ghost-position bug.
func TestS7P1_1_TrackerSettlementFlatThreshold(t *testing.T) {
	assert.Equal(t, 1e-8, settlement.FlatThreshold)
	assert.True(t, settlement.IsFlat(0))
	assert.True(t, settlement.IsFlat(1e-9))
	assert.False(t, settlement.IsFlat(1e-7))
	assert.False(t, settlement.IsFlat(100))
}
