package backtest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func newTestTracker(initialCapital float64) *Tracker {
	logger := zerolog.Nop()
	return NewTracker(initialCapital, 0.0003, 0.001, logger)
}

func TestTracker_TPlusOne_BasicViolation(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600000.SH"
	price := 10.0
	quantity := 1000.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy shares
	_, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, day1, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	pos, exists := tracker.GetPosition(symbol)
	if !exists {
		t.Fatal("position should exist after buy")
	}
	if pos.QuantityToday != quantity {
		t.Errorf("expected QuantityToday=%.0f, got %.0f", quantity, pos.QuantityToday)
	}
	if pos.QuantityYesterday != 0 {
		t.Errorf("expected QuantityYesterday=0, got %.0f", pos.QuantityYesterday)
	}

	// Try to sell on same day — should fail with T+1 violation
	_, err = tracker.ExecuteTrade(symbol, domain.DirectionClose, quantity, price, day1, nil)
	if err == nil {
		t.Fatal("expected T+1 violation error when selling on same day")
	}

	// Verify position unchanged after failed sell
	pos, _ = tracker.GetPosition(symbol)
	if pos.Quantity != quantity {
		t.Errorf("position should be unchanged after failed sell: expected %.0f, got %.0f", quantity, pos.Quantity)
	}
}

func TestTracker_TPlusOne_AdvanceDayAllowsSell(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600001.SH"
	price := 20.0
	quantity := 500.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy shares
	_, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, day1, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	// Advance to day 2 (T+1 settlement)
	tracker.AdvanceDay(day2)

	// Verify QuantityToday rolled over to QuantityYesterday
	pos, _ := tracker.GetPosition(symbol)
	if pos.QuantityToday != 0 {
		t.Errorf("expected QuantityToday=0 after AdvanceDay, got %.0f", pos.QuantityToday)
	}
	if pos.QuantityYesterday != quantity {
		t.Errorf("expected QuantityYesterday=%.0f after AdvanceDay, got %.0f", quantity, pos.QuantityYesterday)
	}

	// Now selling should succeed
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionClose, quantity, price, day2, nil)
	if err != nil {
		t.Fatalf("sell should succeed after T+1: %v", err)
	}
	if trade == nil {
		t.Fatal("trade should not be nil")
	}
	if trade.Quantity != quantity {
		t.Errorf("expected sold quantity=%.0f, got %.0f", quantity, trade.Quantity)
	}

	// Verify position is closed
	if tracker.HasPosition(symbol) {
		t.Error("position should be closed after full sell")
	}
}

func TestTracker_TPlusOne_PartialSell(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600002.SH"
	price := 15.0
	buyQty := 1000.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy 1000 shares
	_, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, buyQty, price, day1, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	// Day 1: Buy another 500 shares (same day, different trade)
	_, err = tracker.ExecuteTrade(symbol, domain.DirectionLong, 500.0, price, day1, nil)
	if err != nil {
		t.Fatalf("second buy failed: %v", err)
	}

	// Verify total QuantityToday = 1500
	pos, _ := tracker.GetPosition(symbol)
	totalToday := pos.QuantityToday
	if totalToday != 1500.0 {
		t.Errorf("expected QuantityToday=1500, got %.0f", totalToday)
	}

	// Advance to day 2
	tracker.AdvanceDay(day2)

	// Try to sell only 800 of the 1500 available
	sellQty := 800.0
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionClose, sellQty, price, day2, nil)
	if err != nil {
		t.Fatalf("partial sell should succeed: %v", err)
	}
	if trade.Quantity != sellQty {
		t.Errorf("expected sold quantity=%.0f, got %.0f", sellQty, trade.Quantity)
	}

	// Verify remaining position
	pos, _ = tracker.GetPosition(symbol)
	expectedRemaining := 1500.0 - sellQty
	if pos.Quantity != expectedRemaining {
		t.Errorf("expected remaining position=%.0f, got %.0f", expectedRemaining, pos.Quantity)
	}
	if pos.QuantityYesterday != expectedRemaining {
		t.Errorf("expected QuantityYesterday=%.0f, got %.0f", expectedRemaining, pos.QuantityYesterday)
	}
}

func TestTracker_TPlusOne_MultiDayAccumulation(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600003.SH"
	price := 25.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy 500 shares
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 500.0, price, day1, nil)

	// Advance to day 2: 500 shares become sellable
	tracker.AdvanceDay(day2)

	// Day 2: Buy 300 more shares (total 800, but only 500 sellable today)
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 300.0, price, day2, nil)

	pos, _ := tracker.GetPosition(symbol)
	if pos.Quantity != 800.0 {
		t.Errorf("expected total quantity=800, got %.0f", pos.Quantity)
	}
	if pos.QuantityYesterday != 500.0 {
		t.Errorf("expected QuantityYesterday=500 (from day1), got %.0f", pos.QuantityYesterday)
	}
	if pos.QuantityToday != 300.0 {
		t.Errorf("expected QuantityToday=300 (from day2), got %.0f", pos.QuantityToday)
	}

	// Try to sell 700 — only 500 should be sold (QuantityYesterday limit)
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionClose, 700.0, price, day2, nil)
	if err != nil {
		t.Fatalf("sell should succeed (partial): %v", err)
	}
	if trade.Quantity != 500.0 {
		t.Errorf("expected actual sold=500 (limited by QuantityYesterday), got %.0f", trade.Quantity)
	}

	// Remaining position: 300 shares (all bought today)
	pos, _ = tracker.GetPosition(symbol)
	if pos.Quantity != 300.0 {
		t.Errorf("expected remaining=300, got %.0f", pos.Quantity)
	}

	// Cannot sell these 300 on day2 (bought today)
	_, err = tracker.ExecuteTrade(symbol, domain.DirectionClose, 300.0, price, day2, nil)
	if err == nil {
		t.Fatal("should not be able to sell shares bought today")
	}

	// Advance to day3: now all 300 are sellable
	tracker.AdvanceDay(day3)
	trade, err = tracker.ExecuteTrade(symbol, domain.DirectionClose, 300.0, price, day3, nil)
	if err != nil {
		t.Fatalf("sell should succeed on day3: %v", err)
	}
	if trade.Quantity != 300.0 {
		t.Errorf("expected sold=300, got %.0f", trade.Quantity)
	}
}

func TestTracker_TPlusOne_ShortPositionNoRestriction(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600004.SH"
	price := 30.0
	quantity := 200.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Open short position
	_, err := tracker.ExecuteTrade(symbol, domain.DirectionShort, quantity, price, day1, nil)
	if err != nil {
		t.Fatalf("short sell failed: %v", err)
	}

	// Close short on same day — should succeed (no T+1 for shorts)
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionClose, quantity, price, day1, nil)
	if err != nil {
		t.Fatalf("closing short should not have T+1 restriction: %v", err)
	}
	if trade == nil {
		t.Fatal("trade should not be nil")
	}

	// Position should be closed
	if tracker.HasPosition(symbol) {
		t.Error("short position should be closed")
	}
}

func TestTracker_TPlusOne_AvgCostUpdate(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600005.SH"
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy at price 10
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000.0, 10.0, day1, nil)

	// Advance to day2
	tracker.AdvanceDay(day2)

	// Day 2: Buy more at price 20 (should update avg cost)
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000.0, 20.0, day2, nil)

	pos, _ := tracker.GetPosition(symbol)
	// Avg cost includes commission, so it will be slightly higher than simple average
	if pos.AvgCost < 14.9 || pos.AvgCost > 15.1 {
		t.Errorf("expected avg cost≈15.00 (with commission), got %.2f", pos.AvgCost)
	}

	// Sell 1000 shares (from day1 purchase, now sellable)
	trade, _ := tracker.ExecuteTrade(symbol, domain.DirectionClose, 1000.0, 25.0, day2, nil)
	if trade == nil {
		t.Fatal("trade should not be nil")
	}

	// PnL should be positive (sold at 25, bought at ~15)
	if trade.Price <= pos.AvgCost {
		t.Errorf("expected sell price > avg cost for profit")
	}
}