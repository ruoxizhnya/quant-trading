package backtest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestEngine_NewEngine_DefaultConfig(t *testing.T) {
	logger := zerolog.Nop()

	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	assert.Equal(t, 1000000.0, tracker.GetCash())
	assert.Equal(t, 1000000.0, tracker.initialCash)
	assert.NotNil(t, tracker.positions)
	assert.Empty(t, tracker.positions)
	assert.Equal(t, 0.0003, tracker.commissionRate)
	assert.Equal(t, 0.001, tracker.slippageRate)
}

func TestEngine_Tracker_BuyAndSellCycle(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbol := "600000.SH"
	price := 10.0
	quantity := 1000.0
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Buy
	trade, err := tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, day1, nil)
	require.NoError(t, err)
	require.NotNil(t, trade)
	assert.Equal(t, symbol, trade.Symbol)
	assert.Equal(t, domain.DirectionLong, trade.Direction)
	assert.True(t, trade.Quantity > 0)
	assert.True(t, trade.Commission >= 5.0) // minimum commission

	// Verify position
	pos, exists := tracker.GetPosition(symbol)
	assert.True(t, exists)
	assert.Equal(t, quantity, pos.Quantity)

	// Advance day for T+1
	tracker.AdvanceDay(day2)

	// Sell
	sellTrade, err := tracker.ExecuteTrade(symbol, domain.DirectionClose, quantity, price*1.1, day2, nil)
	require.NoError(t, err)
	require.NotNil(t, sellTrade)
	assert.Equal(t, domain.DirectionClose, sellTrade.Direction)

	// Verify position closed
	assert.False(t, tracker.HasPosition(symbol))
}

func TestEngine_Tracker_PortfolioValue(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbol := "600001.SH"
	price := 20.0
	quantity := 500.0
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Buy shares
	tracker.ExecuteTrade(symbol, domain.DirectionLong, quantity, price, day, nil)

	// Calculate portfolio value at current prices
	prices := map[string]float64{
		symbol: 25.0, // Price went up
	}
	totalValue := tracker.GetPortfolioValue(prices)

	expectedPositionValue := quantity * 25.0
	cashAfterBuy := tracker.GetCash()

	assert.InDelta(t, cashAfterBuy+expectedPositionValue, totalValue, 1.0,
		"portfolio value should equal cash + position market value")
}

func TestEngine_Tracker_RecordDailyValue(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbol := "600002.SH"
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Record initial value (no positions)
	prices := map[string]float64{}
	pv1 := tracker.RecordDailyValue(day1, prices)
	assert.Equal(t, 1000000.0, pv1.TotalValue)
	assert.Equal(t, 1000000.0, pv1.Cash)

	// Buy some shares
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 100, 10.0, day1, nil)

	// Record value after buy
	prices[symbol] = 12.0
	pv2 := tracker.RecordDailyValue(day2, prices)

	// Total value = cash (after buy) + positions value
	// Cash decreased by buy cost, but we have position value
	assert.True(t, pv2.Positions > 0, "should have position value")
	assert.True(t, pv2.Cash < 1000000.0, "cash should decrease after buy")

	// Verify equity curve has 2 entries
	curve := tracker.GetEquityCurve()
	assert.Len(t, curve, 2)
}

func TestEngine_Tracker_MultiplePositions(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbols := []string{"600003.SH", "600004.SH", "600005.SH"}
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Buy multiple positions
	for i, sym := range symbols {
		price := float64(10 + i)
		_, err := tracker.ExecuteTrade(sym, domain.DirectionLong, 200, price, day1, nil)
		require.NoError(t, err)
	}

	// Verify all positions exist
	for _, sym := range symbols {
		assert.True(t, tracker.HasPosition(sym), "should have position for %s", sym)
	}

	// Get all positions
	allPos := tracker.GetAllPositions()
	assert.Len(t, allPos, len(symbols))

	// Advance and sell all
	tracker.AdvanceDay(day2)
	for _, sym := range symbols {
		pos, _ := tracker.GetPosition(sym)
		_, err := tracker.ExecuteTrade(sym, domain.DirectionClose, pos.Quantity, pos.AvgCost*1.05, day2, nil)
		require.NoError(t, err)
	}

	// All positions closed
	for _, sym := range symbols {
		assert.False(t, tracker.HasPosition(sym), "position should be closed for %s", sym)
	}
}

func TestEngine_Tracker_ClosePosition(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbol := "600006.SH"
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)

	// Open position
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, 15.0, day1, nil)
	tracker.AdvanceDay(day2)

	// Close using ClosePosition helper
	trade, err := tracker.ClosePosition(symbol, 16.0, day2)
	require.NoError(t, err)
	require.NotNil(t, trade)
	assert.Equal(t, domain.DirectionClose, trade.Direction)

	assert.False(t, tracker.HasPosition(symbol))
}

func TestEngine_Tracker_Reset(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Do some trades
	tracker.ExecuteTrade("A.SH", domain.DirectionLong, 100, 10.0, day, nil)
	tracker.RecordDailyValue(day, map[string]float64{"A.SH": 11.0})

	// Reset
	tracker.Reset(2000000.0)

	assert.Equal(t, 2000000.0, tracker.GetCash())
	assert.False(t, tracker.HasPosition("A.SH"))
	assert.Empty(t, tracker.GetAllPositions())
	assert.Empty(t, tracker.GetTrades())
	assert.Empty(t, tracker.GetEquityCurve())
}

func TestEngine_Tracker_InsufficientCash(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000, 0.0003, 0.001, logger) // Only 1000 CNY

	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Try to buy more than we can afford
	_, err := tracker.ExecuteTrade("EXPENSIVE.SH", domain.DirectionLong, 10000, 100.0, day, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient cash")
}

func TestEngine_Tracker_DividendProcessing(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbol := "600007.SH"
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Buy shares
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, 10.0, day, nil)

	// Process dividend (0.5 CNY per share)
	dividend := domain.Dividend{
		Symbol: symbol,
		DivAmt: 0.5,
		PayDate: day.Add(24 * time.Hour),
	}
	err := tracker.ProcessDividend(symbol, dividend)
	require.NoError(t, err)

	// Cash should increase by 1000 * 0.5 = 500
	expectedDividendCredit := 1000 * 0.5
	assert.GreaterOrEqual(t, tracker.GetCash(), 1000000-10000+expectedDividendCredit-100)
}

func TestEngine_Tracker_GetPortfolioSnapshot(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, logger)

	symbol := "600008.SH"
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	// Buy shares
	tracker.ExecuteTrade(symbol, domain.DirectionLong, 500, 20.0, day, nil)

	// Get portfolio snapshot
	prices := map[string]float64{symbol: 25.0}
	portfolio := tracker.GetPortfolio(prices)

	require.NotNil(t, portfolio)
	assert.True(t, portfolio.TotalValue > 0)
	assert.True(t, portfolio.Cash > 0)
	assert.Contains(t, portfolio.Positions, symbol)

	pos := portfolio.Positions[symbol]
	assert.Equal(t, 25.0, pos.CurrentPrice)
	assert.Equal(t, 500*25.0, pos.MarketValue)
}

func TestEngine_PriceLimitDetection_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		prevClose  float64
		todayClose float64
		limitRate  float64
		wantUp     bool
		wantDown   bool
	}{
		{
			name:       "exactly at limit-up",
			prevClose:  10.0,
			todayClose: 11.0,
			limitRate:  0.10,
			wantUp:     true,
			wantDown:   false,
		},
		{
			name:       "exactly at limit-down",
			prevClose:  10.0,
			todayClose: 9.0,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   true,
		},
		{
			name:       "just below limit-up",
			prevClose:  10.0,
			todayClose: 10.99,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   false,
		},
		{
			name:       "just above limit-down",
			prevClose:  10.0,
			todayClose: 9.01,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   false,
		},
		{
			name:       "ST stock at 5% up",
			prevClose:  5.0,
			todayClose: 5.25,
			limitRate:  0.05,
			wantUp:     true,
			wantDown:   false,
		},
		{
			name:       "new stock at 20% up",
			prevClose:  20.0,
			todayClose: 24.0,
			limitRate:  0.20,
			wantUp:     true,
			wantDown:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upperLimit := tc.prevClose * (1 + tc.limitRate)
			lowerLimit := tc.prevClose * (1 - tc.limitRate)

			gotUp := tc.todayClose >= upperLimit
			gotDown := tc.todayClose <= lowerLimit

			assert.Equal(t, tc.wantUp, gotUp, "limit-up mismatch")
			assert.Equal(t, tc.wantDown, gotDown, "limit-down mismatch")
		})
	}
}