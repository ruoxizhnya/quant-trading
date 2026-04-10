package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func initLogger() zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: zerolog.TestWriter{T: &testing.T{}}})
}

func TestNewTracker(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	if tracker == nil {
		t.Fatal("Tracker should not be nil")
	}

	if tracker.GetCash() != 1000000 {
		t.Errorf("Expected initial cash 1000000, got %f", tracker.GetCash())
	}
}

func TestTracker_ExecuteTrade_Long(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	quantity := 1000.0
	timestamp := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, timestamp, nil)
	if err != nil {
		t.Fatalf("ExecuteTrade failed: %v", err)
	}

	if trade == nil {
		t.Fatal("Trade should not be nil")
	}

	if trade.Symbol != symbol {
		t.Errorf("Expected symbol %s, got %s", symbol, trade.Symbol)
	}

	if trade.Direction != domain.DirectionLong {
		t.Errorf("Expected direction long, got %s", trade.Direction)
	}

	if trade.Quantity != quantity {
		t.Errorf("Expected quantity %f, got %f", quantity, trade.Quantity)
	}

	expectedCommission := math.Max(quantity*price*0.0003, DefaultMinCommission)
	if math.Abs(trade.Commission-expectedCommission) > 1e-6 {
		t.Errorf("Expected commission %f, got %f", expectedCommission, trade.Commission)
	}

	pos, exists := tracker.GetPosition(symbol)
	if !exists {
		t.Fatal("Position should exist after long trade")
	}

	if pos.Quantity != quantity {
		t.Errorf("Expected position quantity %f, got %f", quantity, pos.Quantity)
	}

	if pos.AvgCost != price*(1+0.0001) {
		t.Errorf("Expected avg cost with slippage, got %f", pos.AvgCost)
	}

	newCash := tracker.GetCash()
	expectedCash := 1000000 - (quantity * price * (1 + 0.0003)) - expectedCommission - (quantity * price * DefaultTransferFeeRate)
	if math.Abs(newCash-expectedCash) > 10 {
		t.Errorf("Expected cash %f, got %f, diff=%f", expectedCash, newCash, math.Abs(newCash-expectedCash))
	}
}

func TestTracker_ExecuteTrade_Short(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	quantity := 500.0
	timestamp := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionShort, quantity, price, timestamp, nil)
	if err != nil {
		t.Fatalf("ExecuteTrade failed: %v", err)
	}

	if trade.Direction != domain.DirectionShort {
		t.Errorf("Expected direction short, got %s", trade.Direction)
	}

	pos, _ := tracker.GetPosition(symbol)
	if pos.Quantity >= 0 {
		t.Error("Short position should have negative quantity")
	}
}

func TestTracker_ClosePosition_Long(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	buyPrice := 10.0
	sellPrice := 12.0
	quantity := 1000.0
	buyDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	sellDate := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	_, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, buyPrice, buyDate, nil)
	if err != nil {
		t.Fatalf("Buy trade failed: %v", err)
	}

	tracker.AdvanceDay(buyDate.AddDate(0, 0, 1))

	closeTrade, err := tracker.ClosePosition(symbol, sellPrice, sellDate)
	if err != nil {
		t.Fatalf("ClosePosition failed: %v", err)
	}

	if closeTrade.Direction != domain.DirectionClose {
		t.Errorf("Expected direction close, got %s", closeTrade.Direction)
	}

	if closeTrade.StampTax <= 0 {
		t.Error("Close trade should have stamp tax for selling")
	}

	_, exists := tracker.GetPosition(symbol)
	if exists {
		t.Error("Position should be removed after close")
	}

	finalCash := tracker.GetCash()
	if finalCash <= 1000000 {
		t.Errorf("Should have profit after selling at higher price, cash=%f", finalCash)
	}
}

func TestTracker_TPlusOne_Settlement(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	quantity := 1000.0
	day1 := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, day1, nil)

	pos, _ := tracker.GetPosition(symbol)
	if pos.QuantityToday != quantity {
		t.Errorf("QuantityToday should be %f on buy day, got %f", quantity, pos.QuantityToday)
	}

	if pos.QuantityYesterday != 0 {
		t.Error("QuantityYesterday should be 0 immediately after buy")
	}

	_, err := tracker.ClosePosition(symbol, price, day2)
	if err == nil {
		t.Error("T+1 violation: should not be able to sell shares bought today")
	}

	tracker.AdvanceDay(day2)

	pos2, _ := tracker.GetPosition(symbol)
	if pos2.QuantityYesterday != quantity {
		t.Errorf("After AdvanceDay, QuantityYesterday should be %f, got %f", quantity, pos2.QuantityYesterday)
	}

	if pos2.QuantityToday != 0 {
		t.Error("After AdvanceDay, QuantityToday should be reset to 0")
	}

	closeTrade, err := tracker.ClosePosition(symbol, price, day2)
	if err != nil {
		t.Errorf("After T+1 settlement, should be able to sell: %v", err)
	} else if closeTrade == nil {
		t.Error("Close trade should not be nil")
	}
}

func TestTracker_RecordDailyValue(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	prices := map[string]float64{symbol: price}

	pv := tracker.RecordDailyValue(date, prices)
	if pv.TotalValue != 1000000 {
		t.Errorf("Total value should equal initial capital before any positions, got %f", pv.TotalValue)
	}

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, price, date, nil)

	prices[symbol] = 11.0
	nextDate := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)
	pv2 := tracker.RecordDailyValue(nextDate, prices)

	if pv2.TotalValue <= pv.TotalValue {
		t.Errorf("Portfolio value should increase when stock price rises, prev=%f curr=%f", pv.TotalValue, pv2.TotalValue)
	}

	if pv2.Positions <= 0 {
		t.Error("Positions value should be positive after buying stock")
	}
}

func TestTracker_GetTrades(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tradesBefore := tracker.GetTrades()
	if len(tradesBefore) != 0 {
		t.Error("Should have no trades initially")
	}

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, price, date, nil)
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 500, price, date.Add(time.Hour), nil)

	tradesAfter := tracker.GetTrades()
	if len(tradesAfter) != 2 {
		t.Errorf("Expected 2 trades, got %d", len(tradesAfter))
	}
}

func TestTracker_GetPortfolioValues(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	date1 := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)
	prices := map[string]float64{symbol: price}

	tracker.RecordDailyValue(date1, prices)
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, price, date1, nil)
	prices[symbol] = 11.0
	tracker.RecordDailyValue(date2, prices)

	values := tracker.GetPortfolioValues()
	if len(values) != 2 {
		t.Errorf("Expected 2 portfolio values, got %d", len(values))
	}

	if !values[0].Date.Equal(date1) || !values[1].Date.Equal(date2) {
		t.Error("Portfolio value dates don't match input dates")
	}
}

func TestTracker_InsufficientCash(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(10000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 100.0
	quantity := 10000.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	_, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, date, nil)
	if err == nil {
		t.Error("Should fail with insufficient cash")
	}
}

func TestTracker_PartialFill_Liquidity(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	quantity := 100000.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	dayBar := &domain.OHLCV{
		Symbol:    symbol,
		Date:      date,
		Volume:    100000,
		Close:     price,
		LimitUp:   false,
		LimitDown: false,
	}

	opts := &OrderExecutionOpts{
		OrderType: domain.OrderTypeMarket,
		DayBar:    dayBar,
	}

	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, date, opts)
	if err != nil {
		t.Fatalf("ExecuteTrade with liquidity check failed: %v", err)
	}

	maxLiquidity := dayBar.Volume * tracker.liquidityFactor
	if trade.FilledQty > maxLiquidity {
		t.Errorf("Filled qty %f should not exceed max liquidity %f", trade.FilledQty, maxLiquidity)
	}

	if trade.PendingQty <= 0 {
		t.Error("Should have pending qty due to partial fill")
	}
}

func TestTracker_LimitOrder_Expired(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	limitPrice := 9.0
	quantity := 1000.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	dayBar := &domain.OHLCV{
		Symbol: symbol,
		Date:   date,
		Open:   10.0,
		High:   10.5,
		Low:    9.5,
		Close:  10.0,
		Volume: 1000000,
	}

	opts := &OrderExecutionOpts{
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: limitPrice,
		DayBar:     dayBar,
	}

	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, limitPrice, date, opts)
	if err == nil && trade != nil {
		t.Error("Limit order should expire when low > limit price for buy")
	}
}

func TestTracker_LimitOrder_Filled(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	limitPrice := 9.5
	quantity := 1000.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	dayBar := &domain.OHLCV{
		Symbol: symbol,
		Date:   date,
		Open:   10.0,
		High:   10.5,
		Low:    9.3,
		Close:  9.4,
		Volume: 1000000,
	}

	opts := &OrderExecutionOpts{
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: limitPrice,
		DayBar:     dayBar,
	}

	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, limitPrice, date, opts)
	if err != nil {
		t.Fatalf("Limit order should fill when low <= limit price: %v", err)
	}

	if trade.Price > limitPrice {
		t.Errorf("Limit buy execution price %f should be <= limit price %f", trade.Price, limitPrice)
	}
}

func TestTracker_DividendProcessing(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	quantity := 1000.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, date, nil)

	dividend := domain.Dividend{
		Symbol:  symbol,
		DivAmt:  0.5,
		PayDate: time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
	}

	cashBefore := tracker.GetCash()
	err := tracker.ProcessDividend(symbol, dividend)
	if err != nil {
		t.Fatalf("ProcessDividend failed: %v", err)
	}

	cashAfter := tracker.GetCash()
	expectedDividendCredit := quantity * dividend.DivAmt
	if math.Abs(cashAfter-cashBefore-expectedDividendCredit) > 0.01 {
		t.Errorf("Cash should increase by dividend amount %f, diff=%f", expectedDividendCredit, cashAfter-cashBefore)
	}
}

func TestTracker_Reset(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbol := "600000.SH"
	price := 10.0
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, price, date, nil)
	tracker.Reset(2000000)

	if tracker.GetCash() != 2000000 {
		t.Errorf("After reset, cash should be 2000000, got %f", tracker.GetCash())
	}

	if len(tracker.GetTrades()) != 0 {
		t.Error("After reset, trades should be empty")
	}

	if len(tracker.GetPortfolioValues()) != 0 {
		t.Error("After reset, portfolio values should be empty")
	}
}

func TestTracker_MultiplePositions(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(10000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	symbols := []string{"600000.SH", "600519.SH", "000001.SZ"}
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	prices := map[string]float64{
		symbols[0]: 10.0,
		symbols[1]: 50.0,
		symbols[2]: 20.0,
	}

	for _, sym := range symbols {
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 1000, prices[sym], date, nil)
		if err != nil {
			t.Fatalf("Failed to execute trade for %s: %v", sym, err)
		}
	}

	allPositions := tracker.GetAllPositions()
	if len(allPositions) != 3 {
		t.Errorf("Expected 3 positions, got %d", len(allPositions))
	}

	totalValue := tracker.GetPortfolioValue(prices)
	if totalValue < 9990000 {
		t.Errorf("Total portfolio value should be close to initial capital (minus fees), got %f", totalValue)
	}
}

func TestTracker_CloseNonExistentPosition(t *testing.T) {
	logger := initLogger()
	tracker := NewTracker(1000000, 0.0003, 0.0001, defaultTradingConfig(), logger)

	_, err := tracker.ClosePosition("NONEXISTENT.SH", 10.0, time.Now())
	if err == nil {
		t.Error("Should fail when closing non-existent position")
	}
}
