package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventDriven_Name(t *testing.T) {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
	}
	assert.Equal(t, "event_driven", s.Name())
}

func TestEventDriven_Description(t *testing.T) {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
	}
	desc := s.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "Event-driven")
}

func TestEventDriven_Parameters(t *testing.T) {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         20,
			HoldingDays:          5,
		},
	}
	params := s.Parameters()
	assert.Len(t, params, 4)
	assert.Equal(t, "price_jump_threshold", params[0].Name)
	assert.Equal(t, "volume_ratio_threshold", params[1].Name)
	assert.Equal(t, "lookback_days", params[2].Name)
	assert.Equal(t, "holding_days", params[3].Name)
}

func TestEventDriven_Configure(t *testing.T) {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         20,
			HoldingDays:          5,
		},
	}

	t.Run("valid params", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"price_jump_threshold":   0.05,
			"volume_ratio_threshold": 2.0,
			"lookback_days":          10,
			"holding_days":           3,
		})
		require.NoError(t, err)
		assert.Equal(t, 0.05, s.params.PriceJumpThreshold)
		assert.Equal(t, 2.0, s.params.VolumeRatioThreshold)
		assert.Equal(t, 10, s.params.LookbackDays)
		assert.Equal(t, 3, s.params.HoldingDays)
	})

	t.Run("invalid price_jump_threshold (zero)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"price_jump_threshold": 0.0,
		})
		assert.Error(t, err)
	})

	t.Run("invalid price_jump_threshold (too large)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"price_jump_threshold": 1.0,
		})
		assert.Error(t, err)
	})

	t.Run("invalid volume_ratio_threshold (below 1.0)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"volume_ratio_threshold": 0.5,
		})
		assert.Error(t, err)
	})

	t.Run("invalid lookback_days (zero)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"lookback_days": 0,
		})
		assert.Error(t, err)
	})

	t.Run("invalid holding_days (negative)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"holding_days": -1,
		})
		assert.Error(t, err)
	})
}

func TestEventDriven_GenerateSignals_PositiveEvent(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         5,
			HoldingDays:          5,
		},
	}
	// 5 stable bars (close=100, vol=1000) then a +5% jump on 2x volume.
	bars := map[string][]domain.OHLCV{
		"AAPL": makeEventBars("AAPL", 100, 1000, 5, 105, 2000),
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "buy", signals[0].Action)
	assert.Equal(t, "AAPL", signals[0].Symbol)
	assert.Greater(t, signals[0].Strength, 0.0)
	assert.LessOrEqual(t, signals[0].Strength, 1.0)
	assert.Greater(t, signals[0].Price, 0.0)
	assert.Equal(t, "positive", signals[0].Metadata["event_type"])
	assert.Equal(t, 5, signals[0].Metadata["holding_days"])
}

func TestEventDriven_GenerateSignals_NegativeEvent(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         5,
			HoldingDays:          5,
		},
	}
	// 5 stable bars then a -5% drop on 2x volume.
	bars := map[string][]domain.OHLCV{
		"AAPL": makeEventBars("AAPL", 100, 1000, 5, 95, 2000),
	}
	portfolio := &domain.Portfolio{
		Positions: map[string]domain.Position{
			"AAPL": {Symbol: "AAPL", Quantity: 100, CurrentPrice: 95},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, portfolio)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "sell", signals[0].Action)
	assert.Equal(t, "AAPL", signals[0].Symbol)
	assert.Greater(t, signals[0].Strength, 0.0)
	assert.LessOrEqual(t, signals[0].Strength, 1.0)
	assert.Equal(t, "negative", signals[0].Metadata["event_type"])
}

func TestEventDriven_GenerateSignals_NegativeEventNotHeld(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         5,
			HoldingDays:          5,
		},
	}
	// Negative event without a held position → no sell signal (no shorting).
	bars := map[string][]domain.OHLCV{
		"AAPL": makeEventBars("AAPL", 100, 1000, 5, 95, 2000),
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestEventDriven_GenerateSignals_NoEvent(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         5,
			HoldingDays:          5,
		},
	}
	// Normal day: +1% move (below 3% threshold) on baseline volume.
	bars := map[string][]domain.OHLCV{
		"AAPL": makeEventBars("AAPL", 100, 1000, 5, 101, 1000),
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestEventDriven_GenerateSignals_PriceJumpWithoutVolumeSpike(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         5,
			HoldingDays:          5,
		},
	}
	// +5% jump but volume only 1.0x baseline → not confirmed, no signal.
	bars := map[string][]domain.OHLCV{
		"AAPL": makeEventBars("AAPL", 100, 1000, 5, 105, 1000),
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestEventDriven_GenerateSignals_InsufficientData(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         20,
			HoldingDays:          5,
		},
	}
	// Only 2 bars but lookback=20 → need 21 bars.
	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100, Volume: 1000},
			{Symbol: "AAPL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 105, Volume: 2000},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestEventDriven_GenerateSignals_EmptyBars(t *testing.T) {
	ctx := context.Background()
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         5,
			HoldingDays:          5,
		},
	}
	signals, err := s.GenerateSignals(ctx, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestEventDriven_Weight(t *testing.T) {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         20,
			HoldingDays:          5,
		},
	}

	t.Run("normal strength", func(t *testing.T) {
		signal := strategy.Signal{Strength: 0.5}
		weight := s.Weight(signal, 1000000)
		assert.Greater(t, weight, 0.0)
		assert.LessOrEqual(t, weight, 0.05)
	})

	t.Run("capped at max", func(t *testing.T) {
		signal := strategy.Signal{Strength: 2.0}
		weight := s.Weight(signal, 1000000)
		assert.LessOrEqual(t, weight, 0.05)
	})

	t.Run("floored at min", func(t *testing.T) {
		signal := strategy.Signal{Strength: 0.01}
		weight := s.Weight(signal, 1000000)
		assert.GreaterOrEqual(t, weight, 0.01)
	})
}

func TestEventDriven_Cleanup(t *testing.T) {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "test"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         20,
			HoldingDays:          5,
		},
	}
	s.Cleanup()
	assert.Equal(t, EventDrivenConfig{}, s.params)
}

// makeEventBars builds a deterministic OHLCV series for event-driven tests:
// `stableDays` bars at (close=stableClose, volume=stableVol) followed by a
// final bar at (close=finalClose, volume=finalVol) on the next calendar day.
func makeEventBars(symbol string, stableClose, stableVol float64, stableDays int, finalClose, finalVol float64) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, stableDays+1)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < stableDays; i++ {
		bars = append(bars, domain.OHLCV{
			Symbol: symbol,
			Date:   base.AddDate(0, 0, i),
			Open:   stableClose,
			High:   stableClose,
			Low:    stableClose,
			Close:  stableClose,
			Volume: stableVol,
		})
	}
	bars = append(bars, domain.OHLCV{
		Symbol: symbol,
		Date:   base.AddDate(0, 0, stableDays),
		Open:   stableClose,
		High:   finalClose,
		Low:    stableClose,
		Close:  finalClose,
		Volume: finalVol,
	})
	return bars
}
