package tracker

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracker_ProcessSplit_StockDividend(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, contracts.DefaultTradingConfig(), logger)

	symbol := "600010.SH"
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, 10.0, day, nil)

	cashBefore := tracker.GetCash()
	pos, exists := tracker.GetPosition(symbol)
	require.True(t, exists)
	assert.Equal(t, 1000.0, pos.Quantity)
	assert.InDelta(t, 10.0, pos.AvgCost, 0.05)

	split := domain.Split{
		Symbol:       symbol,
		TradeDate:    day,
		StkDivRatio:  0.5,
		CashDivRatio: 0.0,
	}
	err := tracker.ProcessSplit(symbol, split)
	require.NoError(t, err)

	pos, exists = tracker.GetPosition(symbol)
	require.True(t, exists)
	assert.Equal(t, 1500.0, pos.Quantity)
	assert.InDelta(t, 6.667, pos.AvgCost, 0.05)
	assert.Equal(t, cashBefore, tracker.GetCash())
}

func TestTracker_ProcessSplit_CashDividend(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, contracts.DefaultTradingConfig(), logger)

	symbol := "600011.SH"
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, 10.0, day, nil)

	cashBefore := tracker.GetCash()

	split := domain.Split{
		Symbol:       symbol,
		TradeDate:    day,
		StkDivRatio:  0.0,
		CashDivRatio: 0.3,
	}
	err := tracker.ProcessSplit(symbol, split)
	require.NoError(t, err)

	expectedCashCredit := 1000.0 * 0.3
	assert.InDelta(t, cashBefore+expectedCashCredit, tracker.GetCash(), 1.0)

	pos, exists := tracker.GetPosition(symbol)
	require.True(t, exists)
	assert.Equal(t, 1000.0, pos.Quantity)
}

func TestTracker_ProcessSplit_BothDividends(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, contracts.DefaultTradingConfig(), logger)

	symbol := "600012.SH"
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 1000, 10.0, day, nil)

	cashBefore := tracker.GetCash()

	split := domain.Split{
		Symbol:       symbol,
		TradeDate:    day,
		StkDivRatio:  0.2,
		CashDivRatio: 0.5,
	}
	err := tracker.ProcessSplit(symbol, split)
	require.NoError(t, err)

	pos, exists := tracker.GetPosition(symbol)
	require.True(t, exists)
	assert.Equal(t, 1200.0, pos.Quantity)

	expectedCashCredit := 1000.0 * 0.5
	assert.InDelta(t, cashBefore+expectedCashCredit, tracker.GetCash(), 1.0)
}

func TestTracker_ProcessSplit_NoPosition(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, contracts.DefaultTradingConfig(), logger)

	split := domain.Split{
		Symbol:       "NOPOSITION.SH",
		TradeDate:    time.Now(),
		StkDivRatio:  0.5,
		CashDivRatio: 0.3,
	}
	err := tracker.ProcessSplit("NOPOSITION.SH", split)
	assert.NoError(t, err)
}

func TestTracker_GetTotalValue(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.001, contracts.DefaultTradingConfig(), logger)

	symbol := "600013.SH"
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)

	total := tracker.GetTotalValue(map[string]float64{})
	assert.Equal(t, 1000000.0, total)

	tracker.ExecuteTrade(symbol, domain.DirectionLong, 500, 20.0, day, nil)

	prices := map[string]float64{symbol: 25.0}
	total = tracker.GetTotalValue(prices)
	cashAfterBuy := tracker.GetCash()
	assert.InDelta(t, cashAfterBuy+500*25.0, total, 1.0)
}
