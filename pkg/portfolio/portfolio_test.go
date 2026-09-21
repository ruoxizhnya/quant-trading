package portfolio

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
)

// TestComputeFees_Sell100k_StampTaxIs50Yuan is the AUD-06 acceptance
// criterion stated in terms of money rather than rate, so it fails
// loudly with a human-readable number if the rate regresses.
//
// 100,000 CNY sold => stamp tax 100000 * 0.05% = 50 CNY.
// Before AUD-06 the rate was 0.1%, so this case charged 100 CNY and
// every backtest that sold was overstating costs by 50 CNY per 100k
// of turnover (~5bp per round trip).
//
// Deliberately computed through ComputeFees (the real fee function)
// rather than asserting on the constant, so this guards the wiring as
// well as the value.
func TestComputeFees_Sell100k_StampTaxIs50Yuan(t *testing.T) {
	f := fees.DefaultAShareFees()
	fb := ComputeFees(100_000, true, f)

	assert.InDelta(t, 50.0, fb.StampTax, 1e-9,
		"selling 100,000 CNY must incur 50 CNY stamp tax (0.05%%)")
	// And the buy side must still be exempt.
	assert.Zero(t, ComputeFees(100_000, false, f).StampTax,
		"buy must never incur stamp tax")
}

// TestComputeFees_Buy (S7-P1-1)
// Buy trades must NOT charge stamp tax (stamp tax is sell-only).
// Trade value large enough that commission % exceeds the ¥5 floor.
func TestComputeFees_Buy(t *testing.T) {
	f := fees.DefaultAShareFees()
	// Buy 2000 shares @ 10.0 = tradeValue 20000; commission = 20000*0.0003 = 6.0 > 5.0 floor
	fb := ComputeFees(20000, false, f)
	assert.Equal(t, 20000*f.CommissionRate, fb.Commission)   // 6.0
	assert.Equal(t, 20000*f.TransferFeeRate, fb.TransferFee) // 0.2
	assert.Equal(t, 0.0, fb.StampTax, "buy must not charge stamp tax")
	assert.InDelta(t, 6.2, fb.Total(), 0.001)
}

// TestComputeFees_Sell (S7-P1-1)
// Sell trades must charge stamp tax on top of commission + transfer fee.
// Trade value large enough that commission % exceeds the ¥5 floor.
func TestComputeFees_Sell(t *testing.T) {
	f := fees.DefaultAShareFees()
	// Sell 2000 shares @ 10.0 = tradeValue 20000; commission = 6.0 > 5.0 floor
	const tradeValue = 20000.0
	fb := ComputeFees(tradeValue, true, f)
	assert.Equal(t, tradeValue*f.CommissionRate, fb.Commission)   // 6.0
	assert.Equal(t, tradeValue*f.TransferFeeRate, fb.TransferFee) // 0.2
	assert.Equal(t, tradeValue*f.StampTaxRate, fb.StampTax)       // 10.0 after AUD-06
	// AUD-06 (ODR-065): the total used to be hardcoded at 26.2, which
	// baked in the pre-2023-08 stamp tax rate (20.0 instead of 10.0).
	// Derived from the constants so a rate change cannot silently
	// desynchronise this assertion from the others in the same test.
	want := tradeValue * (f.CommissionRate + f.TransferFeeRate + f.StampTaxRate)
	assert.InDelta(t, want, fb.Total(), 0.001)
	assert.InDelta(t, 16.2, fb.Total(), 0.001, "6.0 commission + 0.2 transfer + 10.0 stamp tax")
}

// TestComputeFees_MinCommissionFloor (S7-P1-1)
// Small trades must pay the minimum commission floor (¥5), not the
// percentage rate (which would be < ¥5).
func TestComputeFees_MinCommissionFloor(t *testing.T) {
	f := fees.DefaultAShareFees()
	// Buy 1 share @ 1.0 = tradeValue 1.0; 1.0 * 0.0003 = 0.0003 < 5.0
	fb := ComputeFees(1.0, false, f)
	assert.Equal(t, f.MinCommission, fb.Commission, "commission must hit ¥5 floor")
}

// TestComputeFees_ZeroOrNegativeTradeValue (S7-P1-1)
// Defensive: zero or negative trade value must produce zero fees
// (not negative fees).
func TestComputeFees_ZeroOrNegativeTradeValue(t *testing.T) {
	f := fees.DefaultAShareFees()
	fb := ComputeFees(0, true, f)
	assert.Equal(t, FeeBreakdown{}, fb, "zero trade value -> zero fees")
	fb = ComputeFees(-100, true, f)
	assert.Equal(t, FeeBreakdown{}, fb, "negative trade value -> zero fees (defensive)")
}

// TestUpdateAvgCost_NewPosition (S7-P1-1)
// Buying into a flat position: avg cost = fill price, qty = filled qty.
func TestUpdateAvgCost_NewPosition(t *testing.T) {
	avgCost, qty := UpdateAvgCost(0, 0, 10.0, 100)
	assert.Equal(t, 10.0, avgCost)
	assert.Equal(t, 100.0, qty)
}

// TestUpdateAvgCost_ExistingPosition (S7-P1-1)
// Buying more of an existing position: weighted average.
// Old: 100 @ 10.0; Buy: 100 @ 12.0 → avg = (10*100 + 12*100) / 200 = 11.0
func TestUpdateAvgCost_ExistingPosition(t *testing.T) {
	avgCost, qty := UpdateAvgCost(10.0, 100, 12.0, 100)
	assert.Equal(t, 11.0, avgCost)
	assert.Equal(t, 200.0, qty)
}

// TestUpdateAvgCost_OffsetToFlat (S7-P1-1, S7-P0-17)
// A buy that exactly offsets an existing short must return (0, 0) to
// signal position deletion — this is the ghost-position guard.
// Old short: -100 @ 10.0; Buy: +100 @ 11.0 → flat.
func TestUpdateAvgCost_OffsetToFlat(t *testing.T) {
	avgCost, qty := UpdateAvgCost(10.0, -100, 11.0, 100)
	assert.Equal(t, 0.0, avgCost, "flat position avg cost must be 0 (deletion signal)")
	assert.Equal(t, 0.0, qty, "flat position qty must be 0 (deletion signal)")
}

// TestUpdateAvgCost_NearZeroButNotFlat (S7-P1-1)
// A quantity just above the 1e-8 threshold is NOT flat.
func TestUpdateAvgCost_NearZeroButNotFlat(t *testing.T) {
	avgCost, qty := UpdateAvgCost(10.0, 1e-7, 10.0, 0)
	assert.True(t, math.Abs(qty) >= 1e-8, "qty just above threshold is not flat")
	assert.Equal(t, 10.0, avgCost)
}
