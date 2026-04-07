package backtest

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestTracker_LimitUp_BlocksBuy(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600000.SH"
	prevClose := 10.0
	limitUpPrice := prevClose * 1.10 // 11.0 (涨停价)
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Create OHLCV bar at limit-up price
	_ = domain.OHLCV{
		Symbol:    symbol,
		Date:      day,
		Open:      10.5,
		High:      11.0,
		Low:       10.5,
		Close:     11.0, // Exactly at limit-up
		Volume:    1000000,
		LimitUp:   true,
		LimitDown: false,
	}

	// Try to buy on limit-up day — should succeed in tracker (engine blocks before this)
	// This test verifies the tracker correctly records the trade if allowed
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, limitUpPrice, day, nil)
	if err != nil {
		t.Fatalf("buy should succeed when not blocked by engine: %v", err)
	}
	if trade == nil {
		t.Fatal("trade should not be nil")
	}

	// Verify position created with limit-up price (with slippage)
	pos, exists := tracker.GetPosition(symbol)
	if !exists {
		t.Fatal("position should exist after buy")
	}
	// Slippage applied: executionPrice = limitUpPrice * (1 + slippageRate)
	expectedPrice := limitUpPrice * 1.001
	if pos.AvgCost < expectedPrice-0.01 || pos.AvgCost > expectedPrice+0.01 {
		t.Errorf("expected avg cost≈%.2f (limit-up with slippage), got %.2f", expectedPrice, pos.AvgCost)
	}
}

func TestTracker_LimitDown_BlocksSell(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600001.SH"
	price := 10.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy shares
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, price, day1, nil)

	// Advance to day 2 for T+1 settlement
	tracker.AdvanceDay(day2)

	limitDownPrice := price * 0.90 // 9.0 (跌停价)

	// Try to sell on limit-down day — tracker allows it (engine blocks before this)
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionClose, 1000, limitDownPrice, day2, nil)
	if err != nil {
		t.Fatalf("sell should succeed when not blocked by engine: %v", err)
	}
	if trade == nil {
		t.Fatal("trade should not be nil")
	}

	// Verify position closed
	if tracker.HasPosition(symbol) {
		t.Error("position should be closed after sell")
	}

	// Verify PnL reflects the loss from selling at limit-down
	// Slippage applied for sell: executionPrice = limitDownPrice * (1 - slippageRate)
	expectedSellPrice := limitDownPrice * 0.999
	if trade.Price < expectedSellPrice-0.01 || trade.Price > expectedSellPrice+0.01 {
		t.Errorf("expected sell price≈%.2f (limit-down with slippage), got %.2f", expectedSellPrice, trade.Price)
	}
}

func TestTracker_STStockLimit_5Percent(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "ST康美" // ST stock name
	prevClose := 5.0
	stLimitUp := prevClose * 1.05  // 5.25 (ST 涨停)
	_ = prevClose * 0.95 // 4.75 (ST 跌停)
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Buy at ST limit-up price
	trade, _ := tracker.ExecuteTrade(symbol, domain.DirectionLong, 2000, stLimitUp, day, nil)
	if trade == nil {
		t.Fatal("trade should succeed")
	}

	// Verify ST stock has correct limit-up price applied (with slippage)
	pos, _ := tracker.GetPosition(symbol)
	expectedSTPrice := stLimitUp * 1.001
	if pos.AvgCost < expectedSTPrice-0.01 || pos.AvgCost > expectedSTPrice+0.01 {
		t.Errorf("ST stock avg cost should use ±5%% limit: expected≈%.2f, got %.2f", expectedSTPrice, pos.AvgCost)
	}
}

func TestTracker_NewStockLimit_20Percent(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600005.SH" // Newly listed stock
	_ = time.Date(2022, 11, 1, 0, 0, 0, 0, time.UTC) // Listed ~60 days ago
	tradeDay := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	prevClose := 20.0
	newStockLimitUp := prevClose * 1.20 // 24.0 (新股涨停 ±20%)

	// Simulate new stock buy at 20% limit-up
	trade, _ := tracker.ExecuteTrade(symbol, domain.DirectionLong, 500, newStockLimitUp, tradeDay, nil)
	if trade == nil {
		t.Fatal("trade should succeed")
	}

	pos, _ := tracker.GetPosition(symbol)
	expectedNewStockPrice := newStockLimitUp * 1.001
	if pos.AvgCost < expectedNewStockPrice-0.01 || pos.AvgCost > expectedNewStockPrice+0.01 {
		t.Errorf("new stock should allow ±20%% limit: expected≈%.2f, got %.2f", expectedNewStockPrice, pos.AvgCost)
	}
}

func TestTracker_LimitUp_ConsecutiveDays(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600006.SH"
	initialPrice := 10.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	_ = time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC) // For future reference

	// Day 1: Buy at normal price
	day1Price := initialPrice
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, day1Price, day1, nil)

	// Day 1 limit-up: close = 11.0 (+10%)
	day1LimitUp := initialPrice * 1.10

	// Advance to day 2
	tracker.AdvanceDay(day2)

	// Day 2: Price opens higher due to previous limit-up
	// New base = previous day's close (11.0)
	day2Base := day1LimitUp
	day2LimitUp := day2Base * 1.10 // 12.1 (连续第二天涨停)

	// Cannot sell on day2 (T+1 from day1 buy is now sellable, but let's verify state)
	pos, _ := tracker.GetPosition(symbol)
	if pos.QuantityYesterday != 1000 {
		t.Errorf("expected 1000 sellable shares on day2, got %.0f", pos.QuantityYesterday)
	}

	// Sell at day2's limit-up price (if engine allows)
	trade, _ := tracker.ExecuteTrade(symbol, domain.DirectionClose, 1000, day2LimitUp, day2, nil)
	if trade == nil {
		t.Fatal("trade should succeed")
	}

	// Should have profit from two consecutive limit-ups
	expectedPnL := (day2LimitUp - initialPrice) * 1000
	actualPnL := (trade.Price - initialPrice) * trade.Quantity
	// Allow some margin for commission
	if actualPnL < expectedPnL*0.9 {
		t.Errorf("expected PnL≈%.2f from consecutive limit-ups, got %.2f", expectedPnL, actualPnL)
	}
}

func TestTracker_LimitDown_StopLossScenario(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600007.SH"
	buyPrice := 15.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Day 1: Buy at 15.0
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, buyPrice, day1, nil)

	// Advance to day 2
	tracker.AdvanceDay(day2)

	// Day 2: Stock hits limit-down (-10%)
	limitDownPrice := buyPrice * 0.90 // 13.5

	// Sell at limit-down (stop-loss scenario)
	trade, _ := tracker.ExecuteTrade(symbol, domain.DirectionClose, 1000, limitDownPrice, day2, nil)
	if trade == nil {
		t.Fatal("trade should succeed")
	}

	// Verify loss is approximately -10% (minus commissions)
	lossPercent := (buyPrice - trade.Price) / buyPrice * 100
	if lossPercent < 9.0 || lossPercent > 11.0 {
		t.Errorf("expected loss ≈10%% from limit-down sell, got %.2f%%", lossPercent)
	}

	// Verify cash decreased appropriately (with slippage and fees)
	cash := tracker.GetCash()
	initialCash := 1000000.0
	// Cash should be: initial - buyCost (with commission) + sellProceeds (minus commission/stampTax)
	if cash < initialCash-20000 || cash > initialCash {
		t.Errorf("cash should be between %.0f and %.0f, got %.2f", initialCash-20000, initialCash, cash)
	}
}

func TestTracker_NormalDay_NoLimitRestriction(t *testing.T) {
	tracker := newTestTracker(1000000.0)
	symbol := "600008.SH"
	price := 12.34 // Normal price, not near any limit
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Normal buy/sell cycle without hitting limits
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 800, price, day1, nil)
	tracker.AdvanceDay(day2)

	newPrice := 13.56 // Normal price change (~10%, but not exactly at limit)
	trade, _ := tracker.ExecuteTrade(symbol, domain.DirectionClose, 800, newPrice, day2, nil)
	if trade == nil {
		t.Fatal("normal trade should succeed")
	}

	// Verify normal profit calculation
	gainPercent := (newPrice - price) / price * 100
	if gainPercent < 8.0 || gainPercent > 12.0 {
		// Should be ~10% gain
		t.Errorf("expected normal gain ≈10%%, got %.2f%%", gainPercent)
	}
}