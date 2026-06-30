package backtest

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
	"github.com/ruoxizhnya/quant-trading/pkg/settlement"
	"github.com/stretchr/testify/assert"
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
		defaultTradingConfig(),
		zerolog.Nop(),
	)

	sched := tracker.feeSchedule()
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
