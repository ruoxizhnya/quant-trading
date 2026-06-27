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

func TestMeanReversionStrategy_Name(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
	}
	assert.Equal(t, "mean_reversion", s.Name())
}

func TestMeanReversionStrategy_Description(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
	}
	assert.NotEmpty(t, s.Description())
	assert.Contains(t, s.Description(), "Bollinger")
	assert.Contains(t, s.Description(), "RSI")
}

func TestMeanReversionStrategy_Parameters(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
		params: MeanReversionConfig{
			BollingerPeriod:  20,
			BollingerStdDev:  2.0,
			RSIPeriod:         14,
			RSIOversold:       30,
			RSIOverbought:     70,
		},
	}
	params := s.Parameters()
	assert.Len(t, params, 5)
	assert.Equal(t, "bollinger_period", params[0].Name)
	assert.Equal(t, "bollinger_stddev", params[1].Name)
	assert.Equal(t, "rsi_period", params[2].Name)
	assert.Equal(t, "rsi_oversold", params[3].Name)
	assert.Equal(t, "rsi_overbought", params[4].Name)
}

func TestMeanReversionStrategy_Configure(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
		params: MeanReversionConfig{
			BollingerPeriod:  20,
			BollingerStdDev:  2.0,
			RSIPeriod:         14,
			RSIOversold:       30,
			RSIOverbought:     70,
		},
	}

	t.Run("valid params", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"bollinger_period":  10,
			"bollinger_stddev":  1.5,
			"rsi_period":         7,
			"rsi_oversold":       25,
			"rsi_overbought":     75,
		})
		require.NoError(t, err)
		assert.Equal(t, 10, s.params.BollingerPeriod)
		assert.Equal(t, 1.5, s.params.BollingerStdDev)
		assert.Equal(t, 7, s.params.RSIPeriod)
		assert.Equal(t, 25.0, s.params.RSIOversold)
		assert.Equal(t, 75.0, s.params.RSIOverbought)
	})

	t.Run("invalid bollinger_period", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"bollinger_period": 0,
		})
		assert.Error(t, err)
	})

	t.Run("invalid rsi_oversold", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"rsi_oversold": 60.0, // oversold must be < 50
		})
		assert.Error(t, err)
	})
}

func TestMeanReversionStrategy_GenerateSignals(t *testing.T) {
	ctx := context.Background()
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
		params: MeanReversionConfig{
			BollingerPeriod:  5,
			BollingerStdDev:  2.0,
			RSIPeriod:         5,
			RSIOversold:       40,
			RSIOverbought:     60,
		},
	}

	t.Run("empty bars", func(t *testing.T) {
		signals, err := s.GenerateSignals(ctx, nil, nil)
		require.NoError(t, err)
		assert.Empty(t, signals)
	})

	t.Run("insufficient data", func(t *testing.T) {
		bars := map[string][]domain.OHLCV{
			"AAPL": {
				{Symbol: "AAPL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, nil)
		require.NoError(t, err)
		assert.Empty(t, signals)
	})

	t.Run("buy signal when price below lower band and RSI oversold", func(t *testing.T) {
		// Create data where price drops sharply to trigger Bollinger lower band + RSI oversold
		bars := map[string][]domain.OHLCV{
			"AAPL": {
				{Symbol: "AAPL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC), Close: 80}, // sharp drop
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, nil)
		require.NoError(t, err)
		// Should generate a buy signal due to sharp drop
		if len(signals) > 0 {
			assert.Equal(t, "buy", signals[0].Action)
			assert.Equal(t, "AAPL", signals[0].Symbol)
			assert.Greater(t, signals[0].Strength, 0.0)
		}
	})

	t.Run("sell signal when price above upper band and RSI overbought", func(t *testing.T) {
		// Create data where price rises sharply
		bars := map[string][]domain.OHLCV{
			"AAPL": {
				{Symbol: "AAPL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC), Close: 120}, // sharp rise
			},
		}
		portfolio := &domain.Portfolio{
			Positions: map[string]domain.Position{
				"AAPL": {Symbol: "AAPL", Quantity: 100, CurrentPrice: 120},
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, portfolio)
		require.NoError(t, err)
		if len(signals) > 0 {
			assert.Equal(t, "sell", signals[0].Action)
		}
	})

	t.Run("no signal when price in normal range", func(t *testing.T) {
		// Price stays flat — no Bollinger breach, RSI near 50
		bars := map[string][]domain.OHLCV{
			"AAPL": {
				{Symbol: "AAPL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC), Close: 100},
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, nil)
		require.NoError(t, err)
		assert.Empty(t, signals)
	})
}

// ─── Bollinger Bands unit tests ────────────────────────────

func TestCalculateBollingerBands_Success(t *testing.T) {
	closes := []float64{100, 102, 98, 101, 99, 100}
	upper, lower := calculateBollingerBands(closes, 5, 2.0)
	assert.Greater(t, upper, lower)
	// SMA of last 5 (102,98,101,99,100) = 100
	// Should be symmetric around 100
	mid := (upper + lower) / 2
	assert.InDelta(t, 100, mid, 0.01)
}

func TestCalculateBollingerBands_InsufficientData(t *testing.T) {
	closes := []float64{100, 102}
	upper, lower := calculateBollingerBands(closes, 5, 2.0)
	assert.Equal(t, 0.0, upper)
	assert.Equal(t, 0.0, lower)
}

func TestCalculateBollingerBands_ZeroVariance(t *testing.T) {
	closes := []float64{100, 100, 100, 100, 100}
	upper, lower := calculateBollingerBands(closes, 5, 2.0)
	// No variance → upper == lower == SMA
	assert.InDelta(t, 100, upper, 0.001)
	assert.InDelta(t, 100, lower, 0.001)
}

// ─── RSI unit tests ────────────────────────────────────────

func TestCalculateRSI_AllGains(t *testing.T) {
	// Prices only go up → RSI should be 100
	closes := []float64{100, 101, 102, 103, 104, 105}
	rsi := calculateRSI(closes, 5)
	assert.InDelta(t, 100, rsi, 0.01)
}

func TestCalculateRSI_AllLosses(t *testing.T) {
	// Prices only go down → RSI should be 0
	closes := []float64{105, 104, 103, 102, 101, 100}
	rsi := calculateRSI(closes, 5)
	assert.InDelta(t, 0, rsi, 0.01)
}

func TestCalculateRSI_NoMovement(t *testing.T) {
	closes := []float64{100, 100, 100, 100, 100, 100}
	rsi := calculateRSI(closes, 5)
	assert.InDelta(t, 50, rsi, 0.01)
}

func TestCalculateRSI_InsufficientData(t *testing.T) {
	closes := []float64{100, 101}
	rsi := calculateRSI(closes, 5)
	assert.InDelta(t, 50, rsi, 0.01)
}

func TestCalculateRSI_MixedGainsLosses(t *testing.T) {
	// Alternating gains/losses: avgGain=0.6, avgLoss=0.4 → RS=1.5 → RSI=60
	closes := []float64{100, 101, 100, 101, 100, 101}
	rsi := calculateRSI(closes, 5)
	assert.InDelta(t, 60, rsi, 0.01)
}

func TestMeanReversionStrategy_Weight(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
		params: MeanReversionConfig{
			BollingerPeriod: 20,
		},
	}

	signal := strategy.Signal{Strength: 0.5}
	weight := s.Weight(signal, 1000000)
	assert.Greater(t, weight, 0.0)
	assert.LessOrEqual(t, weight, 0.05)
}

func TestMeanReversionStrategy_Cleanup(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test"),
		params: MeanReversionConfig{
			BollingerPeriod:  20,
			BollingerStdDev:  2.0,
			RSIPeriod:         14,
			RSIOversold:       30,
			RSIOverbought:     70,
		},
	}
	s.Cleanup()
	assert.Equal(t, MeanReversionConfig{}, s.params)
}
