// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// MomentumConfig holds configuration for the momentum strategy.
type MomentumConfig struct {
	LookbackDays       int
	TopN               int
	RebalanceFrequency string
}

// momentumStrategy implements a price momentum strategy.
// Buy stocks with strongest N-day returns, sell weakest.
type momentumStrategy struct {
	name        string
	description string
	params      MomentumConfig
}

func (s *momentumStrategy) Name() string {
	return "momentum"
}

func (s *momentumStrategy) Description() string {
	return "Momentum strategy: buy stocks with strongest N-day returns, sell weakest"
}

func (s *momentumStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "lookback_days",
			Type:        "int",
			Default:     20,
			Description: "Number of days to calculate momentum",
			Min:         5,
			Max:         100,
		},
		{
			Name:        "top_n",
			Type:        "int",
			Default:     5,
			Description: "Number of top stocks to buy",
			Min:         1,
			Max:         20,
		},
		{
			Name:        "rebalance_frequency",
			Type:        "string",
			Default:     "weekly",
			Description: "Rebalance frequency: daily, weekly, monthly",
		},
	}
}

func (s *momentumStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	lookback := s.params.LookbackDays
	if lookback <= 0 {
		lookback = 20
	}
	topN := s.params.TopN
	if topN <= 0 {
		topN = 5
	}

	type stockMomentum struct {
		symbol   string
		momentum float64
	}

	var results []stockMomentum

	for symbol, data := range bars {
		if len(data) < lookback+1 {
			continue
		}

		// Sort by date ascending
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Date.Before(sorted[j].Date)
		})

		endIdx := len(sorted) - 1

		// Check if we have enough data for lookback
		if endIdx < lookback {
			continue
		}

		startPrice := sorted[endIdx-lookback].Close
		endPrice := sorted[endIdx].Close
		if startPrice <= 0 {
			continue
		}

		momentum := (endPrice - startPrice) / startPrice

		results = append(results, stockMomentum{
			symbol:   symbol,
			momentum: momentum,
		})
	}

	// Sort by momentum descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].momentum > results[j].momentum
	})

	// Build signals for top N stocks
	n := len(results)
	if n > topN {
		n = topN
	}

	var signals []strategy.Signal
	for i := 0; i < n; i++ {
		if results[i].momentum <= 0 {
			continue // Only buy stocks with positive momentum
		}

		// Get the latest price
		var price float64
		if data, ok := bars[results[i].symbol]; ok && len(data) > 0 {
			sorted := make([]domain.OHLCV, len(data))
			copy(sorted, data)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Date.Before(sorted[j].Date)
			})
			price = sorted[len(sorted)-1].Close
		}

		signals = append(signals, strategy.Signal{
			Symbol:   results[i].symbol,
			Action:   "buy",
			Strength: results[i].momentum,
			Price:    price,
		})
	}

	// Generate sell signals for stocks with negative momentum that we hold
	if portfolio != nil {
		for symbol, position := range portfolio.Positions {
			var hasMomentum bool
			for _, r := range results {
				if r.symbol == symbol {
					hasMomentum = true
					break
				}
			}
			if !hasMomentum && position.Quantity > 0 {
				// Stock not in top N, sell it
				signals = append(signals, strategy.Signal{
					Symbol:   symbol,
					Action:   "sell",
					Strength: 1.0,
					Price:    position.CurrentPrice,
				})
			}
		}
	}

	return signals, nil
}

// Configure sets the strategy parameters.
func (s *momentumStrategy) Configure(params map[string]any) error {
	if v, ok := params["lookback_days"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.LookbackDays = int(val)
		case int:
			s.params.LookbackDays = val
		}
	}
	if v, ok := params["top_n"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.TopN = int(val)
		case int:
			s.params.TopN = val
		}
	}
	if v, ok := params["rebalance_frequency"]; ok {
		if val, ok := v.(string); ok {
			s.params.RebalanceFrequency = val
		}
	}
	return nil
}

// Weight returns the position weight based on signal strength.
// For momentum strategy: weight = strength * base_weight (capped at 0.05 per position).
func (s *momentumStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	baseWeight := 1.0 / float64(s.params.TopN)
	weight := signal.Strength * baseWeight
	if weight > 0.05 {
		weight = 0.05
	}
	if weight < 0.01 {
		weight = 0.01
	}
	return weight
}

// Cleanup releases any resources held by the strategy.
func (s *momentumStrategy) Cleanup() {
	s.params = MomentumConfig{}
}

func init() {
	strategy.GlobalRegister(&momentumStrategy{
		name:        "momentum",
		description: "Momentum strategy: buy stocks with strongest N-day returns, sell weakest",
		params: MomentumConfig{
			LookbackDays:       20,
			TopN:               5,
			RebalanceFrequency: "weekly",
		},
	})
}
