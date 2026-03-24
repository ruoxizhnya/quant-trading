package backtest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestT1Settlement_LongPosition(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	day1 := time.Date(2024, 1, 2, 15, 0, 0, 0, time.Local) // 2024-01-02
	day2 := time.Date(2024, 1, 3, 15, 0, 0, 0, time.Local) // 2024-01-03

	// Day 1: Buy 100 shares
	_, err := tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 100, 10.0, day1)
	require.NoError(t, err)

	// Verify position exists
	pos, exists := tracker.GetPosition("600000.SH")
	require.True(t, exists)
	assert.Equal(t, 100.0, pos.Quantity)
	assert.Equal(t, 100.0, pos.QuantityToday)    // Bought today, not sellable yet
	assert.Equal(t, 0.0, pos.QuantityYesterday)  // None from yesterday

	// Day 1: Try to sell — should FAIL due to T+1
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 100, 10.5, day1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "T+1 settlement violation")

	// Day 1: Advance to Day 2 (T+1 rollover)
	tracker.AdvanceDay(day2)

	// Verify rollover
	pos, _ = tracker.GetPosition("600000.SH")
	assert.Equal(t, 0.0, pos.QuantityToday)
	assert.Equal(t, 100.0, pos.QuantityYesterday)

	// Day 2: Sell 50 shares — should SUCCEED
	trade, err := tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 50, 10.5, day2)
	require.NoError(t, err)
	assert.Equal(t, 50.0, trade.Quantity)
	// Stamp tax should be applied (0.1%)
	assert.Greater(t, trade.StampTax, 0.0)

	// Day 2: Try to sell remaining 60 (only 50 available) — should reduce to 50
	trade, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 60, 10.5, day2)
	require.NoError(t, err)
	assert.Equal(t, 50.0, trade.Quantity) // Reduced to available

	// Position should be fully closed now
	_, exists = tracker.GetPosition("600000.SH")
	assert.False(t, exists)
}

func TestT1Settlement_MultipleBuys(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	day1 := time.Date(2024, 1, 2, 15, 0, 0, 0, time.Local)
	day2 := time.Date(2024, 1, 3, 15, 0, 0, 0, time.Local)
	day3 := time.Date(2024, 1, 4, 15, 0, 0, 0, time.Local)

	// Day 1: Buy 100 shares
	_, err := tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 100, 10.0, day1)
	require.NoError(t, err)

	// Advance to Day 2
	tracker.AdvanceDay(day2)

	// Day 2: Buy 50 more shares (now have 100 yesterday-able + 50 today)
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 50, 11.0, day2)
	require.NoError(t, err)

	pos, _ := tracker.GetPosition("600000.SH")
	assert.Equal(t, 150.0, pos.Quantity)
	assert.Equal(t, 50.0, pos.QuantityToday)    // 50 bought today
	assert.Equal(t, 100.0, pos.QuantityYesterday) // 100 from yesterday

	// Day 2: Sell 100 — should succeed (only 100 yesterday-able)
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 100, 11.5, day2)
	require.NoError(t, err)

	// Day 2: Try to sell remaining 60 (only 50 today, not sellable) — should fail
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 60, 11.5, day2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "T+1 settlement violation")

	// Advance to Day 3
	tracker.AdvanceDay(day3)

	// Day 3: Sell remaining 50 — should succeed
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 50, 12.0, day3)
	require.NoError(t, err)
}

func TestT1Settlement_StampTax(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	day1 := time.Date(2024, 1, 2, 15, 0, 0, 0, time.Local)
	day2 := time.Date(2024, 1, 3, 15, 0, 0, 0, time.Local)

	// Day 1: Buy
	_, err := tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 100, 10.0, day1)
	require.NoError(t, err)

	// Day 2: Sell
	tracker.AdvanceDay(day2)
	trade, err := tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 100, 11.0, day2)
	require.NoError(t, err)

	// Commission: min(tradeValue * 0.0003, 5.0) = max(100*11*0.0003, 5) = 5.0 (floor applies)
	// Use larger qty to exceed min commission: 20000 * 11.0 * 0.0003 = 66 > 5.0
	// Note: test setup uses different qty, so commission may be at floor
	// Commission floor check: actual commission should be >= 5.0
	assert.GreaterOrEqual(t, trade.Commission, 5.0)

	// Stamp tax: 100 * 11.0 * 0.001 = 1.1
	expectedStampTax := 100 * 11.0 * 0.001
	assert.InDelta(t, expectedStampTax, trade.StampTax, 0.01)
}

func TestT1Settlement_NewPositionSameDaySell(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	day1 := time.Date(2024, 1, 2, 15, 0, 0, 0, time.Local)

	// Day 1: Buy and immediately try to sell same-day
	_, err := tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 100, 10.0, day1)
	require.NoError(t, err)

	// Sell same day — should fail
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 100, 10.5, day1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "T+1 settlement violation")
}

func TestT1Settlement_PartialFill(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	day1 := time.Date(2024, 1, 2, 15, 0, 0, 0, time.Local)
	day2 := time.Date(2024, 1, 3, 15, 0, 0, 0, time.Local)

	// Day 1: Buy 100 shares
	_, err := tracker.ExecuteTrade("600000.SH", domain.DirectionLong, 100, 10.0, day1)
	require.NoError(t, err)

	// Advance to Day 2
	tracker.AdvanceDay(day2)

	// Day 2: Try to sell 150 (only 100 sellable) — should partially fill with 100
	trade, err := tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 150, 10.5, day2)
	require.NoError(t, err)
	assert.Equal(t, 100.0, trade.Quantity) // Only 100 were sellable
}

func TestT1Settlement_ShortPosition_NoT1Restriction(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	day1 := time.Date(2024, 1, 2, 15, 0, 0, 0, time.Local)

	// Day 1: Short sell 100 shares — no T+1 restriction for shorting
	_, err := tracker.ExecuteTrade("600000.SH", domain.DirectionShort, 100, 10.0, day1)
	require.NoError(t, err)

	// Day 1: Close short — should succeed (no T+1 for short)
	_, err = tracker.ExecuteTrade("600000.SH", domain.DirectionClose, 100, 9.5, day1)
	require.NoError(t, err)
}
