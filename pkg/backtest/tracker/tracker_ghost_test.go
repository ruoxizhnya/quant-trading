package tracker

// S7-P0-17 (ODR-043): regression test for the "ghost zero-quantity
// position" bug discovered by TestProperty_T1Enforced.
//
// Root cause: ExecuteTrade's DirectionLong and DirectionShort branches
// update an existing position's Quantity but never delete the position
// when the update brings Quantity to exactly 0 (e.g. short 100 + buy 100
// = flat). Only the DirectionClose branch had the
// `if abs(pos.Quantity) < 1e-8 { delete(...) }` cleanup. The lingering
// zero-quantity "ghost" position then made a subsequent DirectionClose
// trip the `closeQty <= 0` guard and return
// "cannot close position: quantity is zero" — an error class the
// property test (correctly) did not recognize as acceptable.
//
// A secondary symptom: the long-buy AvgCost update divided by totalQty
// (== 0 on a flip-to-zero), producing NaN.
//
// This file provides a deterministic, hermetic reproduction that does
// not depend on testing/quick's random seed.

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGhostPosition_LongBuyOffsetsShort verifies that buying exactly the
// short quantity flattens the position (deletes it) so a later close
// returns "position not found" rather than "quantity is zero".
func TestGhostPosition_LongBuyOffsetsShort(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1_000_000, 0.0003, 0.0001, contracts.DefaultTradingConfig(), logger)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 1. Open short 100 shares @ 10.00.
	_, err := tracker.ExecuteTrade("S1", domain.DirectionShort, 100, 10.0, ts, nil)
	require.NoError(t, err)
	pos, ok := tracker.GetPosition("S1")
	require.True(t, ok, "short position should exist after shorting")
	assert.Equal(t, -100.0, pos.Quantity)

	// 2. Buy 100 (DirectionLong) — exactly offsets the short → flat.
	_, err = tracker.ExecuteTrade("S1", domain.DirectionLong, 100, 10.0, ts, nil)
	require.NoError(t, err)

	// BUG: the ghost position used to linger with Quantity == 0 and
	// AvgCost == NaN. After the fix, the position must be gone.
	_, ok = tracker.GetPosition("S1")
	assert.False(t, ok, "position must be deleted when long buy exactly offsets a short (no ghost)")

	// 3. A subsequent close must report "position not found", NOT
	// "cannot close position: quantity is zero".
	_, err = tracker.ExecuteTrade("S1", domain.DirectionClose, 100, 10.0, ts, nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "position not found"),
		"close on a flat symbol must return 'position not found', got: %v", err)
}

// TestGhostPosition_ShortOffsetsLong verifies the mirror case: shorting
// exactly the long quantity flattens the position.
func TestGhostPosition_ShortOffsetsLong(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1_000_000, 0.0003, 0.0001, contracts.DefaultTradingConfig(), logger)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 1. Buy 100 long @ 10.00.
	_, err := tracker.ExecuteTrade("S1", domain.DirectionLong, 100, 10.0, ts, nil)
	require.NoError(t, err)

	// 2. Short 100 — exactly offsets the long → flat.
	_, err = tracker.ExecuteTrade("S1", domain.DirectionShort, 100, 10.0, ts, nil)
	require.NoError(t, err)

	_, ok := tracker.GetPosition("S1")
	assert.False(t, ok, "position must be deleted when short exactly offsets a long (no ghost)")

	// 3. Subsequent close → "position not found".
	_, err = tracker.ExecuteTrade("S1", domain.DirectionClose, 100, 10.0, ts, nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "position not found"),
		"close on a flat symbol must return 'position not found', got: %v", err)
}

// TestGhostPosition_AvgCostNotNaNAfterOffset confirms the secondary
// symptom (AvgCost NaN from divide-by-zero) is also resolved.
func TestGhostPosition_AvgCostNotNaNAfterOffset(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1_000_000, 0.0003, 0.0001, contracts.DefaultTradingConfig(), logger)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	_, _ = tracker.ExecuteTrade("S1", domain.DirectionShort, 100, 10.0, ts, nil)
	_, err := tracker.ExecuteTrade("S1", domain.DirectionLong, 100, 10.0, ts, nil)
	require.NoError(t, err)

	// Position should be gone; if it lingered, AvgCost would be NaN.
	// Guard against a future regression that keeps the position but
	// fixes the NaN: also assert no NaN leaks into GetAllPositions.
	for _, p := range tracker.GetAllPositions() {
		assert.False(t, math.IsNaN(p.AvgCost), "AvgCost must not be NaN for %s", p.Symbol)
	}
}
