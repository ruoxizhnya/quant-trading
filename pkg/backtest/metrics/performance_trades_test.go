package metrics

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestCalculateTradeMetrics_Empty(t *testing.T) {
	winRate, total, win, lose, avgDays := calculateTradeMetrics(nil, nil)
	assert.Equal(t, 0.0, winRate)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, win)
	assert.Equal(t, 0, lose)
	assert.Equal(t, 0.0, avgDays)
}

func TestCalculateTradeMetrics_SimpleLongClose(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day5 := time.Date(2023, 1, 6, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 100, Timestamp: day1, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionClose, Price: 12.0, Quantity: 100, Timestamp: day5, Commission: 5},
	}

	winRate, total, win, lose, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, win)
	assert.Equal(t, 0, lose)
	assert.Equal(t, 1.0, winRate)
}

func TestCalculateTradeMetrics_LosingTrade(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 100, Timestamp: day1, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionClose, Price: 8.0, Quantity: 100, Timestamp: day3, Commission: 5},
	}

	winRate, total, win, lose, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 1, total)
	assert.Equal(t, 0, win)
	assert.Equal(t, 1, lose)
	assert.Equal(t, 0.0, winRate)
}

func TestCalculateTradeMetrics_ShortTrade(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day5 := time.Date(2023, 1, 6, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600001.SH", Direction: domain.DirectionShort, Price: 20.0, Quantity: 50, Timestamp: day1, Commission: 5},
		{Symbol: "600001.SH", Direction: domain.DirectionClose, Price: 18.0, Quantity: 50, Timestamp: day5, Commission: 5},
	}

	winRate, total, win, lose, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, win)
	assert.Equal(t, 0, lose)
	assert.Equal(t, 1.0, winRate)
}

func TestCalculateTradeMetrics_MultipleSymbols(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	day5 := time.Date(2023, 1, 6, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 100, Timestamp: day1, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionClose, Price: 12.0, Quantity: 100, Timestamp: day3, Commission: 5},
		{Symbol: "600001.SH", Direction: domain.DirectionLong, Price: 20.0, Quantity: 50, Timestamp: day1, Commission: 5},
		{Symbol: "600001.SH", Direction: domain.DirectionClose, Price: 18.0, Quantity: 50, Timestamp: day5, Commission: 5},
	}

	winRate, total, win, lose, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, win)
	assert.Equal(t, 1, lose)
	assert.InDelta(t, 0.5, winRate, 0.01)
}

func TestCalculateTradeMetrics_AddingToLong(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	day5 := time.Date(2023, 1, 6, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 100, Timestamp: day1, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 11.0, Quantity: 100, Timestamp: day2, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionClose, Price: 12.0, Quantity: 200, Timestamp: day5, Commission: 5},
	}

	winRate, total, win, lose, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, win)
	assert.Equal(t, 0, lose)
	assert.Equal(t, 1.0, winRate)
}

func TestCalculateTradeMetrics_Reversal(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day5 := time.Date(2023, 1, 6, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 100, Timestamp: day1, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionShort, Price: 12.0, Quantity: 100, Timestamp: day5, Commission: 5},
	}

	_, total, win, _, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, win)
}

func TestCalculateTradeMetrics_PartialClose(t *testing.T) {
	day1 := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	day5 := time.Date(2023, 1, 6, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 200, Timestamp: day1, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionClose, Price: 12.0, Quantity: 100, Timestamp: day3, Commission: 5},
		{Symbol: "600000.SH", Direction: domain.DirectionClose, Price: 11.0, Quantity: 100, Timestamp: day5, Commission: 5},
	}

	_, total, win, _, _ := calculateTradeMetrics(trades, nil)
	assert.Equal(t, 2, total)
	assert.GreaterOrEqual(t, win, 1)
}
