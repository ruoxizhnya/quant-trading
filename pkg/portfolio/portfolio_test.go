package portfolio

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
)

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
	fb := ComputeFees(20000, true, f)
	assert.Equal(t, 20000*f.CommissionRate, fb.Commission)   // 6.0
	assert.Equal(t, 20000*f.TransferFeeRate, fb.TransferFee) // 0.2
	assert.Equal(t, 20000*f.StampTaxRate, fb.StampTax)       // 20.0
	assert.InDelta(t, 26.2, fb.Total(), 0.001)
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
