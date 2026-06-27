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

func TestRiskParity_Name(t *testing.T) {
	s := &riskParityStrategy{
		BaseStrategy: strategy.NewBaseStrategy("risk_parity", "test"),
	}
	assert.Equal(t, "risk_parity", s.Name())
}

func TestRiskParity_Parameters(t *testing.T) {
	s := &riskParityStrategy{
		BaseStrategy: strategy.NewBaseStrategy("risk_parity", "test"),
		params: RiskParityConfig{
			LookbackDays:  60,
			MinWeight:     0.01,
			MaxWeight:     0.30,
			RebalanceDays: 5,
		},
	}
	params := s.Parameters()
	assert.Len(t, params, 4)
	assert.Equal(t, "lookback_days", params[0].Name)
	assert.Equal(t, "min_weight", params[1].Name)
	assert.Equal(t, "max_weight", params[2].Name)
	assert.Equal(t, "rebalance_days", params[3].Name)
}

func TestRiskParity_Configure(t *testing.T) {
	s := &riskParityStrategy{
		BaseStrategy: strategy.NewBaseStrategy("risk_parity", "test"),
		params: RiskParityConfig{
			LookbackDays:  60,
			MinWeight:     0.01,
			MaxWeight:     0.30,
			RebalanceDays: 5,
		},
	}

	t.Run("valid params", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"lookback_days":  30,
			"min_weight":     0.02,
			"max_weight":     0.25,
			"rebalance_days": 10,
		})
		require.NoError(t, err)
		assert.Equal(t, 30, s.params.LookbackDays)
		assert.Equal(t, 0.02, s.params.MinWeight)
		assert.Equal(t, 0.25, s.params.MaxWeight)
		assert.Equal(t, 10, s.params.RebalanceDays)
	})

	t.Run("invalid lookback too small", func(t *testing.T) {
		err := s.Configure(map[string]any{"lookback_days": 1})
		assert.Error(t, err)
	})

	t.Run("invalid rebalance zero", func(t *testing.T) {
		err := s.Configure(map[string]any{"rebalance_days": 0})
		assert.Error(t, err)
	})

	t.Run("min_weight greater than max_weight", func(t *testing.T) {
		err := s.Configure(map[string]any{
			"min_weight": 0.5,
			"max_weight": 0.1,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min_weight")
	})
}

func TestRiskParity_GenerateSignals(t *testing.T) {
	ctx := context.Background()
	s := &riskParityStrategy{
		BaseStrategy: strategy.NewBaseStrategy("risk_parity", "test"),
		params: RiskParityConfig{
			LookbackDays:  3,
			MinWeight:     0.01,
			MaxWeight:     0.30,
			RebalanceDays: 5,
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
				{Symbol: "AAPL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 101},
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, nil)
		require.NoError(t, err)
		assert.Empty(t, signals)
	})

	t.Run("generate buy signals with inverse-volatility weights", func(t *testing.T) {
		// LOWVOL has tiny daily moves (low volatility) → should get a
		// LARGER target weight than HIGHVOL under risk parity.
		bars := map[string][]domain.OHLCV{
			"LOWVOL": {
				{Symbol: "LOWVOL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "LOWVOL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100.5},
				{Symbol: "LOWVOL", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Close: 101},
				{Symbol: "LOWVOL", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Close: 100.5},
			},
			"HIGHVOL": {
				{Symbol: "HIGHVOL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "HIGHVOL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 110},
				{Symbol: "HIGHVOL", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Close: 95},
				{Symbol: "HIGHVOL", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Close: 105},
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, nil)
		require.NoError(t, err)
		// No portfolio → every universe symbol gets a buy signal.
		require.Len(t, signals, 2)

		// Deterministic order: HIGHVOL before LOWVOL (alphabetical).
		assert.Equal(t, "HIGHVOL", signals[0].Symbol)
		assert.Equal(t, "LOWVOL", signals[1].Symbol)
		assert.Equal(t, "buy", signals[0].Action)
		assert.Equal(t, "buy", signals[1].Action)

		// LOWVOL has lower volatility → larger target weight.
		lowW := signals[1].Strength
		highW := signals[0].Strength
		assert.Greater(t, lowW, highW, "low-vol symbol should get larger weight")

		// Weights must be clipped to [MinWeight, MaxWeight].
		for _, sig := range signals {
			assert.GreaterOrEqual(t, sig.Strength, 0.01)
			assert.LessOrEqual(t, sig.Strength, 0.30)
			tw, ok := sig.Metadata["target_weight"]
			require.True(t, ok)
			assert.Equal(t, sig.Strength, tw)
		}
	})

	t.Run("sell signals for holdings outside universe", func(t *testing.T) {
		bars := map[string][]domain.OHLCV{
			"AAPL": {
				{Symbol: "AAPL", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 101},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Close: 102},
				{Symbol: "AAPL", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Close: 103},
			},
		}
		portfolio := &domain.Portfolio{
			Cash:       1000,
			TotalValue: 1100,
			Positions: map[string]domain.Position{
				"GOOGL": {Symbol: "GOOGL", Quantity: 1, CurrentPrice: 100, MarketValue: 100},
			},
		}
		signals, err := s.GenerateSignals(ctx, bars, portfolio)
		require.NoError(t, err)
		// One buy for AAPL, one sell (exit) for GOOGL.
		var actions []string
		for _, sig := range signals {
			actions = append(actions, sig.Action)
		}
		assert.Contains(t, actions, "buy")
		assert.Contains(t, actions, "sell")
		for _, sig := range signals {
			if sig.Symbol == "GOOGL" {
				assert.Equal(t, "sell", sig.Action)
				assert.Equal(t, 1.0, sig.Strength)
			}
		}
	})
}

func TestRiskParity_Weight(t *testing.T) {
	s := &riskParityStrategy{
		BaseStrategy: strategy.NewBaseStrategy("risk_parity", "test"),
		params: RiskParityConfig{
			MinWeight: 0.01,
			MaxWeight: 0.30,
		},
	}

	t.Run("buy within range returns unchanged", func(t *testing.T) {
		w := s.Weight(strategy.Signal{Action: "buy", Strength: 0.20}, 1_000_000)
		assert.Equal(t, 0.20, w)
	})

	t.Run("buy above max is clamped", func(t *testing.T) {
		w := s.Weight(strategy.Signal{Action: "buy", Strength: 0.90}, 1_000_000)
		assert.Equal(t, 0.30, w)
	})

	t.Run("buy below min is clamped", func(t *testing.T) {
		w := s.Weight(strategy.Signal{Action: "buy", Strength: 0.001}, 1_000_000)
		assert.Equal(t, 0.01, w)
	})

	t.Run("sell strength is clamped to [0,1]", func(t *testing.T) {
		w := s.Weight(strategy.Signal{Action: "sell", Strength: 0.4}, 1_000_000)
		assert.Equal(t, 0.4, w)
		over := s.Weight(strategy.Signal{Action: "sell", Strength: 5.0}, 1_000_000)
		assert.Equal(t, 1.0, over)
	})
}
