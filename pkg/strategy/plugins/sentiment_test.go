package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data/sentiment"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSentiment_Name(t *testing.T) {
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
	}
	assert.Equal(t, "sentiment", s.Name())
}

func TestSentiment_Description(t *testing.T) {
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
	}
	desc := s.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "Contrarian")
}

func TestSentiment_Parameters(t *testing.T) {
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	params := s.Parameters()
	assert.Len(t, params, 3)
	assert.Equal(t, "buy_threshold", params[0].Name)
	assert.Equal(t, "sell_threshold", params[1].Name)
	assert.Equal(t, "min_confidence", params[2].Name)
}

func TestSentiment_Configure(t *testing.T) {
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}

	t.Run("valid params", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"buy_threshold":  -0.5,
			"sell_threshold": 0.8,
			"min_confidence": 0.5,
		})
		require.NoError(t, err)
		assert.Equal(t, -0.5, s.params.BuyThreshold)
		assert.Equal(t, 0.8, s.params.SellThreshold)
		assert.Equal(t, 0.5, s.params.MinConfidence)
	})

	t.Run("invalid buy_threshold (positive)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"buy_threshold": 0.5, // must be in [-1.0, -0.01]
		})
		assert.Error(t, err)
	})

	t.Run("invalid buy_threshold (zero)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"buy_threshold": 0.0, // 0.0 is excluded from the valid range
		})
		assert.Error(t, err)
	})

	t.Run("invalid sell_threshold (negative)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"sell_threshold": -0.5, // must be in [0.01, 1.0]
		})
		assert.Error(t, err)
	})

	t.Run("invalid min_confidence (above 1.0)", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"min_confidence": 1.5,
		})
		assert.Error(t, err)
	})
}

func TestSentiment_GenerateSignals_BuySignal(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	testDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	// Overly pessimistic sentiment → contrarian buy.
	agg.Add(sentiment.SentimentScore{
		Symbol:     "AAPL",
		Date:       testDate,
		Score:      -0.6,
		Confidence: 0.8,
		Source:     "news",
	})

	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: testDate, Close: 100, Volume: 1000},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "buy", signals[0].Action)
	assert.Equal(t, "AAPL", signals[0].Symbol)
	assert.Greater(t, signals[0].Strength, 0.0)
	assert.LessOrEqual(t, signals[0].Strength, 1.0)
	assert.Equal(t, "contrarian", signals[0].Metadata["strategy"])
	assert.InDelta(t, -0.6, signals[0].Factors["sentiment_score"], 1e-9)
}

func TestSentiment_GenerateSignals_SellSignal(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	testDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	// Overly optimistic sentiment → contrarian sell.
	agg.Add(sentiment.SentimentScore{
		Symbol:     "AAPL",
		Date:       testDate,
		Score:      0.85,
		Confidence: 0.9,
		Source:     "analyst",
	})

	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: testDate, Close: 100, Volume: 1000},
		},
	}
	portfolio := &domain.Portfolio{
		Positions: map[string]domain.Position{
			"AAPL": {Symbol: "AAPL", Quantity: 100, CurrentPrice: 100},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, portfolio)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "sell", signals[0].Action)
	assert.Equal(t, "AAPL", signals[0].Symbol)
	assert.Greater(t, signals[0].Strength, 0.0)
	assert.LessOrEqual(t, signals[0].Strength, 1.0)
	assert.InDelta(t, 0.85, signals[0].Factors["sentiment_score"], 1e-9)
}

func TestSentiment_GenerateSignals_SellSignalNotHeld(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	testDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	agg.Add(sentiment.SentimentScore{
		Symbol:     "AAPL",
		Date:       testDate,
		Score:      0.85,
		Confidence: 0.9,
		Source:     "analyst",
	})

	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: testDate, Close: 100, Volume: 1000},
		},
	}
	// No position held → no sell signal (no shorting).
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestSentiment_GenerateSignals_NoSignal(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	testDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	// Neutral sentiment → no contrarian signal.
	agg.Add(sentiment.SentimentScore{
		Symbol:     "AAPL",
		Date:       testDate,
		Score:      0.1,
		Confidence: 0.8,
		Source:     "news",
	})

	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: testDate, Close: 100, Volume: 1000},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestSentiment_GenerateSignals_LowConfidenceFiltered(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	testDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	// Extreme pessimism but very low confidence → filtered out.
	agg.Add(sentiment.SentimentScore{
		Symbol:     "AAPL",
		Date:       testDate,
		Score:      -0.8,
		Confidence: 0.1, // below min_confidence=0.3
		Source:     "news",
	})

	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: testDate, Close: 100, Volume: 1000},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestSentiment_GenerateSignals_NoAggregator(t *testing.T) {
	ctx := context.Background()
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	// No aggregator wired → returns nil signals, no error.
	bars := map[string][]domain.OHLCV{
		"AAPL": {
			{Symbol: "AAPL", Date: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC), Close: 100, Volume: 1000},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestSentiment_GenerateSignals_NoSentimentDataForSymbol(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	testDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	// Sentiment exists for AAPL but the bars ask for GOOGL.
	agg.Add(sentiment.SentimentScore{
		Symbol:     "AAPL",
		Date:       testDate,
		Score:      -0.6,
		Confidence: 0.8,
		Source:     "news",
	})

	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	bars := map[string][]domain.OHLCV{
		"GOOGL": {
			{Symbol: "GOOGL", Date: testDate, Close: 100, Volume: 1000},
		},
	}
	signals, err := s.GenerateSignals(ctx, bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestSentiment_GenerateSignals_EmptyBars(t *testing.T) {
	ctx := context.Background()
	agg := sentiment.NewSentimentAggregator()
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	s.SetAggregator(agg)

	signals, err := s.GenerateSignals(ctx, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestSentiment_Weight(t *testing.T) {
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
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

func TestSentiment_Cleanup(t *testing.T) {
	agg := sentiment.NewSentimentAggregator()
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "test"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
		aggregator: agg,
	}
	s.Cleanup()
	assert.Equal(t, SentimentConfig{}, s.params)
	assert.Nil(t, s.aggregator)
}
