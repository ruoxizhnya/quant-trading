package settlement

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsFlat_Zero (S7-P1-1)
func TestIsFlat_Zero(t *testing.T) {
	assert.True(t, IsFlat(0), "exactly zero is flat")
	assert.True(t, IsFlat(0.0), "0.0 is flat")
}

// TestIsFlat_NearZero (S7-P1-1, S7-P0-17)
// Quantities below the 1e-8 threshold are flat — this is the
// ghost-position guard threshold.
func TestIsFlat_NearZero(t *testing.T) {
	assert.True(t, IsFlat(1e-9), "1e-9 is flat")
	assert.True(t, IsFlat(-1e-9), "-1e-9 is flat")
	assert.True(t, IsFlat(FlatThreshold/2), "half threshold is flat")
}

// TestIsFlat_NonZero (S7-P1-1)
func TestIsFlat_NonZero(t *testing.T) {
	assert.False(t, IsFlat(1), "1 is not flat")
	assert.False(t, IsFlat(-100), "-100 is not flat")
	assert.False(t, IsFlat(1e-7), "1e-7 is above threshold, not flat")
	assert.False(t, IsFlat(FlatThreshold), "exactly at threshold is not flat (strict <)")
}

// TestRollOver_NormalCase (S7-P1-1)
// T+1 rollover: today's purchases become sellable tomorrow.
func TestRollOver_NormalCase(t *testing.T) {
	// Bought 100 today, had 50 from yesterday
	newYesterday, newToday := RollOver(50, 100)
	assert.Equal(t, 100.0, newYesterday, "today's qty becomes yesterday's")
	assert.Equal(t, 0.0, newToday, "today resets to 0")
}

// TestRollOver_ZeroQuantities (S7-P1-1)
func TestRollOver_ZeroQuantities(t *testing.T) {
	newYesterday, newToday := RollOver(0, 0)
	assert.Equal(t, 0.0, newYesterday)
	assert.Equal(t, 0.0, newToday)
}

// TestRollOver_NegativeShort (S7-P1-1)
// Short positions: rollover works the same way (negative quantities).
func TestRollOver_NegativeShort(t *testing.T) {
	// Short -100 today, had -50 from yesterday
	newYesterday, newToday := RollOver(-50, -100)
	assert.Equal(t, -100.0, newYesterday)
	assert.Equal(t, 0.0, newToday)
}

// TestSellableQty_NormalCase (S7-P1-1)
// T+1: can only sell what was held since yesterday.
func TestSellableQty_NormalCase(t *testing.T) {
	assert.Equal(t, 100.0, SellableQty(100), "can sell all of yesterday's qty")
	assert.Equal(t, 0.0, SellableQty(0), "no yesterday qty -> cannot sell")
}

// TestSellableQty_NegativeClamped (S7-P1-1)
// Defensive: a negative yesterday-qty (shouldn't happen, but...)
// must not return a negative sellable quantity.
func TestSellableQty_NegativeClamped(t *testing.T) {
	assert.Equal(t, 0.0, SellableQty(-50), "negative yesterday qty -> 0 sellable")
}

// TestSellableQty_PartialSell (S7-P1-1)
// Caller is responsible for clamping the sell order to SellableQty;
// this function just returns the ceiling.
func TestSellableQty_PartialSell(t *testing.T) {
	sellable := SellableQty(200)
	assert.True(t, sellable >= 0)
	assert.False(t, math.IsNaN(sellable))
}
