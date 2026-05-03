package backtest

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestNewTracker_InitialCash(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	if tracker.GetCash() != 1_000_000 {
		t.Errorf("expected initial cash 1000000, got %.2f", tracker.GetCash())
	}
}

func TestTracker_ExecuteTrade_Long(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Symbol != "600519" {
		t.Errorf("expected symbol 600519, got %s", trade.Symbol)
	}
	if trade.Direction != domain.DirectionLong {
		t.Errorf("expected direction long, got %s", trade.Direction)
	}
	if trade.Quantity != 100 {
		t.Errorf("expected quantity 100, got %.2f", trade.Quantity)
	}

	if trade.Price <= 50.0 {
		t.Errorf("expected slippage to increase buy price above 50.0, got %.4f", trade.Price)
	}

	if !tracker.HasPosition("600519") {
		t.Error("expected position for 600519")
	}

	pos, exists := tracker.GetPosition("600519")
	if !exists {
		t.Fatal("expected position to exist")
	}
	if pos.Quantity != 100 {
		t.Errorf("expected position quantity 100, got %.2f", pos.Quantity)
	}
	if pos.QuantityToday != 100 {
		t.Errorf("expected QuantityToday=100 (T+1 rule), got %.2f", pos.QuantityToday)
	}
	if pos.QuantityYesterday != 0 {
		t.Errorf("expected QuantityYesterday=0, got %.2f", pos.QuantityYesterday)
	}

	expectedCash := 1_000_000 - (trade.Quantity * trade.Price) - trade.Commission - trade.TransferFee
	actualCash := tracker.GetCash()
	if abs(actualCash-expectedCash) > 0.01 {
		t.Errorf("expected cash ~%.2f, got %.2f", expectedCash, actualCash)
	}
}

func TestTracker_ExecuteTrade_Short(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionShort, 100, 50.0, date, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Direction != domain.DirectionShort {
		t.Errorf("expected direction short, got %s", trade.Direction)
	}

	if trade.StampTax <= 0 {
		t.Errorf("expected stamp tax on short sell, got %.4f", trade.StampTax)
	}

	pos, _ := tracker.GetPosition("600519")
	if pos.Quantity != -100 {
		t.Errorf("expected short position -100, got %.2f", pos.Quantity)
	}

	if tracker.GetCash() <= 1_000_000 {
		t.Errorf("expected cash to increase from short sell, got %.2f", tracker.GetCash())
	}
}

func TestTracker_ExecuteTrade_CloseLong(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	buyDate := time.Date(2026, 4, 9, 0, 0, 0, 0, time.Local)
	sellDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, buyDate, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	tracker.AdvanceDay(buyDate)

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionClose, 100, 55.0, sellDate, nil)
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if trade.Direction != domain.DirectionClose {
		t.Errorf("expected direction close, got %s", trade.Direction)
	}
	if trade.StampTax <= 0 {
		t.Errorf("expected stamp tax on sell, got %.4f", trade.StampTax)
	}

	if tracker.HasPosition("600519") {
		t.Error("expected position to be removed after full close")
	}
}

func TestTracker_T1Violation(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	_, err = tracker.ExecuteTrade("600519", domain.DirectionClose, 100, 55.0, date, nil)
	if err == nil {
		t.Error("expected T+1 violation error, got nil")
	}
}

func TestTracker_T1PartialSell(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	buyDate := time.Date(2026, 4, 9, 0, 0, 0, 0, time.Local)
	sellDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 200, 50.0, buyDate, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	tracker.AdvanceDay(buyDate)

	_, err = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 52.0, sellDate, nil)
	if err != nil {
		t.Fatalf("second buy failed: %v", err)
	}

	pos, _ := tracker.GetPosition("600519")
	if pos.QuantityYesterday != 200 {
		t.Errorf("expected QuantityYesterday=200, got %.2f", pos.QuantityYesterday)
	}
	if pos.QuantityToday != 100 {
		t.Errorf("expected QuantityToday=100, got %.2f", pos.QuantityToday)
	}

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionClose, 250, 55.0, sellDate, nil)
	if err != nil {
		t.Fatalf("partial sell failed: %v", err)
	}

	if trade.Quantity > 200 {
		t.Errorf("should only sell up to QuantityYesterday (200), got %.2f", trade.Quantity)
	}
}

func TestTracker_AdvanceDay(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)

	pos, _ := tracker.GetPosition("600519")
	if pos.QuantityToday != 100 {
		t.Errorf("before AdvanceDay: expected QuantityToday=100, got %.2f", pos.QuantityToday)
	}
	if pos.QuantityYesterday != 0 {
		t.Errorf("before AdvanceDay: expected QuantityYesterday=0, got %.2f", pos.QuantityYesterday)
	}

	tracker.AdvanceDay(date)

	pos, _ = tracker.GetPosition("600519")
	if pos.QuantityToday != 0 {
		t.Errorf("after AdvanceDay: expected QuantityToday=0, got %.2f", pos.QuantityToday)
	}
	if pos.QuantityYesterday != 100 {
		t.Errorf("after AdvanceDay: expected QuantityYesterday=100, got %.2f", pos.QuantityYesterday)
	}
}

func TestTracker_InsufficientCash(t *testing.T) {
	tracker := newTestTracker(1000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	if err == nil {
		t.Error("expected insufficient cash error, got nil")
	}
}

func TestTracker_CloseNonexistentPosition(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, err := tracker.ClosePosition("999999", 50.0, date)
	if err == nil {
		t.Error("expected error closing nonexistent position, got nil")
	}
}

func TestTracker_ProcessDividend(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 1000, 50.0, date, nil)
	cashBefore := tracker.GetCash()

	dividend := domain.Dividend{
		Symbol:  "600519",
		PayDate: date,
		DivAmt:  0.50,
	}
	err := tracker.ProcessDividend("600519", dividend)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCredit := 1000 * 0.50
	actualCredit := tracker.GetCash() - cashBefore
	if abs(actualCredit-expectedCredit) > 0.01 {
		t.Errorf("expected dividend credit %.2f, got %.2f", expectedCredit, actualCredit)
	}
}

func TestTracker_ProcessDividend_NoPosition(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	dividend := domain.Dividend{
		Symbol:  "600519",
		PayDate: date,
		DivAmt:  0.50,
	}
	err := tracker.ProcessDividend("600519", dividend)
	if err != nil {
		t.Fatalf("should not error on no position, got: %v", err)
	}
}

func TestTracker_ProcessSplit(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	buyTrade, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 1000, 50.0, date, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}
	actualBuyPrice := buyTrade.Price

	split := domain.Split{
		Symbol:       "600519",
		TradeDate:    date,
		StkDivRatio:  0.1,
		CashDivRatio: 0.5,
	}
	err = tracker.ProcessSplit("600519", split)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pos, _ := tracker.GetPosition("600519")
	expectedQty := 1000 + 1000*0.1
	if abs(pos.Quantity-expectedQty) > 0.01 {
		t.Errorf("expected quantity %.2f after split, got %.2f", expectedQty, pos.Quantity)
	}

	expectedCost := (actualBuyPrice * 1000) / expectedQty
	if abs(pos.AvgCost-expectedCost) > 0.01 {
		t.Errorf("expected avg cost %.4f after split, got %.4f", expectedCost, pos.AvgCost)
	}

	expectedCashCredit := 1000 * 0.5
	actualCash := tracker.GetCash()
	expectedMinCash := 1_000_000 - 1000*actualBuyPrice - buyTrade.Commission - buyTrade.TransferFee + expectedCashCredit
	if actualCash < expectedMinCash-1 {
		t.Errorf("expected cash >= %.2f (including cash dividend credit of %.2f), got %.2f", expectedMinCash, expectedCashCredit, actualCash)
	}
}

func TestTracker_RecordDailyValue(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)

	prices := map[string]float64{"600519": 55.0}
	pv := tracker.RecordDailyValue(date, prices)

	if pv.Date != date {
		t.Errorf("expected date %v, got %v", date, pv.Date)
	}
	if pv.Cash <= 0 {
		t.Errorf("expected positive cash, got %.2f", pv.Cash)
	}
	if pv.Positions <= 0 {
		t.Errorf("expected positive positions value, got %.2f", pv.Positions)
	}
	if pv.TotalValue <= 0 {
		t.Errorf("expected positive total value, got %.2f", pv.TotalValue)
	}

	values := tracker.GetPortfolioValues()
	if len(values) != 1 {
		t.Errorf("expected 1 portfolio value, got %d", len(values))
	}
}

func TestTracker_GetPortfolio(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	buyTrade, _ := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)

	prices := map[string]float64{"600519": 55.0}
	portfolio := tracker.GetPortfolio(prices)

	if portfolio.Cash <= 0 {
		t.Errorf("expected positive cash, got %.2f", portfolio.Cash)
	}
	if len(portfolio.Positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(portfolio.Positions))
	}
	pos := portfolio.Positions["600519"]
	expectedPnL := (55.0 - buyTrade.Price) * 100
	if abs(pos.UnrealizedPnL-expectedPnL) > 1.0 {
		t.Errorf("expected unrealized PnL ~%.2f, got %.2f", expectedPnL, pos.UnrealizedPnL)
	}
}

func TestTracker_Reset(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	tracker.RecordDailyValue(date, map[string]float64{"600519": 55.0})

	tracker.Reset(500_000)

	if tracker.GetCash() != 500_000 {
		t.Errorf("expected cash 500000 after reset, got %.2f", tracker.GetCash())
	}
	if tracker.HasPosition("600519") {
		t.Error("expected no positions after reset")
	}
	if len(tracker.GetTrades()) != 0 {
		t.Errorf("expected 0 trades after reset, got %d", len(tracker.GetTrades()))
	}
	if len(tracker.GetPortfolioValues()) != 0 {
		t.Errorf("expected 0 portfolio values after reset, got %d", len(tracker.GetPortfolioValues()))
	}
}

func TestTracker_LimitOrderBuy(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	dayBar := &domain.OHLCV{Open: 51, High: 52, Low: 49, Close: 50, Volume: 1000000}
	opts := &OrderExecutionOpts{
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 49.5,
		DayBar:     dayBar,
	}

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Price > 49.5 {
		t.Errorf("limit buy should execute at or below limit price, got %.4f", trade.Price)
	}
}

func TestTracker_LimitOrderExpired(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	dayBar := &domain.OHLCV{Open: 52, High: 53, Low: 51, Close: 52, Volume: 1000000}
	opts := &OrderExecutionOpts{
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 49.5,
		DayBar:     dayBar,
	}

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, opts)
	if err == nil {
		t.Error("expected limit order to expire, got nil error")
	}
}

func TestTracker_LimitOrderSell(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	buyDate := time.Date(2026, 4, 9, 0, 0, 0, 0, time.Local)
	sellDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, buyDate, nil)
	tracker.AdvanceDay(buyDate)

	dayBar := &domain.OHLCV{Open: 54, High: 56, Low: 53, Close: 55, Volume: 1000000}
	opts := &OrderExecutionOpts{
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 55.0,
		DayBar:     dayBar,
	}

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionShort, 100, 54.0, sellDate, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Price < 55.0 {
		t.Errorf("limit sell should execute at or above limit price, got %.4f", trade.Price)
	}
}

func TestTracker_GetAllPositions(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	_, _ = tracker.ExecuteTrade("000858", domain.DirectionLong, 200, 30.0, date, nil)

	positions := tracker.GetAllPositions()
	if len(positions) != 2 {
		t.Errorf("expected 2 positions, got %d", len(positions))
	}
}

func TestTracker_MultipleBuysSameSymbol(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	trade1, _ := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	trade2, _ := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 55.0, date, nil)

	pos, _ := tracker.GetPosition("600519")
	if pos.Quantity != 200 {
		t.Errorf("expected total quantity 200, got %.2f", pos.Quantity)
	}

	expectedAvgCost := (trade1.Price*100 + trade2.Price*100) / 200
	if abs(pos.AvgCost-expectedAvgCost) > 0.01 {
		t.Errorf("expected avg cost %.4f, got %.4f", expectedAvgCost, pos.AvgCost)
	}
}

func TestTracker_TradeHistory(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	_, _ = tracker.ExecuteTrade("000858", domain.DirectionLong, 200, 30.0, date, nil)

	trades := tracker.GetTrades()
	if len(trades) != 2 {
		t.Errorf("expected 2 trades, got %d", len(trades))
	}
}

func TestTracker_PortfolioValueCalculation(t *testing.T) {
	tracker := newTestTracker(1_000_000)
	date := time.Date(2026, 4, 10, 0, 0, 0, 0, time.Local)

	_, _ = tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)

	prices := map[string]float64{"600519": 55.0}
	totalValue := tracker.GetPortfolioValue(prices)

	cash := tracker.GetCash()
	positionValue := 100 * 55.0
	expectedTotal := cash + positionValue

	if abs(totalValue-expectedTotal) > 1.0 {
		t.Errorf("expected total value ~%.2f, got %.2f", expectedTotal, totalValue)
	}
}
