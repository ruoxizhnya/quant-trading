package backtest

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

func TestPosition_T1Fields_Exist(t *testing.T) {
	pos := domain.Position{
		Symbol:           "600000.SH",
		Quantity:         1000,
		QuantityToday:    500,
		QuantityYesterday: 500,
		AvgCost:          10.0,
	}

	t.Run("T1_Fields_Populated", func(t *testing.T) {
		if pos.QuantityToday != 500 {
			t.Errorf("expected QuantityToday=500, got %.2f", pos.QuantityToday)
		}
		if pos.QuantityYesterday != 500 {
			t.Errorf("expected QuantityYesterday=500, got %.2f", pos.QuantityYesterday)
		}
	})

	t.Run("TotalQuantity_Correct", func(t *testing.T) {
		total := pos.QuantityToday + pos.QuantityYesterday
		if total != pos.Quantity {
			t.Errorf("expected Quantity=%d, but Today+Yesterday=%d", int(pos.Quantity), int(total))
		}
	})
}

func TestTracker_AdvanceDay_T1Rollover(t *testing.T) {
	logger := zerolog.Nop()
	tracker := NewTracker(1000000, 0.0003, 0.0001, logger)

	tradeDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tracker.positions["600000.SH"] = &domain.Position{
		Symbol:           "600000.SH",
		Quantity:         1000,
		QuantityToday:    400,
		QuantityYesterday: 600,
		AvgCost:          10.0,
		BuyDate:          tradeDate,
	}

	t.Run("AdvanceDay_Rolls_Today_To_Yesterday", func(t *testing.T) {
		nextDay := tradeDate.AddDate(0, 0, 1)
		tracker.AdvanceDay(nextDay)

		pos, exists := tracker.GetPosition("600000.SH")
		if !exists {
			t.Fatal("position should exist after AdvanceDay")
		}

		if pos.QuantityToday != 0 {
			t.Errorf("after AdvanceDay, QuantityToday should be 0, got %.2f", pos.QuantityToday)
		}
		if pos.QuantityYesterday != 1000 {
			t.Errorf("after AdvanceDay, QuantityYesterday should be 1000 (600+400), got %.2f", pos.QuantityYesterday)
		}
	})
}
