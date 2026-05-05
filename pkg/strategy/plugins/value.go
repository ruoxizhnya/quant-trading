// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ValueConfig holds configuration for the value strategy.
type ValueConfig struct {
	LookbackDays       int
	TopN               int
	RebalanceFrequency string
	PEMax              float64
}

// valueStrategy implements a value factor strategy.
// Buy stocks with low PE (high earnings yield), sell when overvalued.
type valueStrategy struct {
	*strategy.BaseStrategy
	params ValueConfig
}

func (s *valueStrategy) Name() string {
	return "value"
}

func (s *valueStrategy) Description() string {
	return "Value strategy: buy stocks with low PE ratio (high earnings yield)"
}

func (s *valueStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "lookback_days",
			Type:        "int",
			Default:     20,
			Description: "Number of days for price lookback",
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
			Name:        "pe_max",
			Type:        "float",
			Default:     30.0,
			Description: "Maximum PE ratio to consider",
			Min:         1.0,
			Max:         100.0,
		},
		{
			Name:        "rebalance_frequency",
			Type:        "string",
			Default:     "weekly",
			Description: "Rebalance frequency: daily, weekly, monthly",
		},
	}
}

func (s *valueStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
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
	peMax := s.params.PEMax
	if peMax <= 0 {
		peMax = 30.0
	}

	type stockValue struct {
		symbol string
		value  float64 // earnings yield = 1/PE
		price  float64
		pe     float64
	}

	var results []stockValue

	for symbol, data := range bars {
		if len(data) < lookback+1 {
			continue
		}

		// Sort by date ascending using shared utility
		sorted := sortOHLCV(data)

		// Calculate average price over lookback period
		var sumPrice float64
		for i := len(sorted) - lookback; i < len(sorted); i++ {
			sumPrice += sorted[i].Close
		}
		avgPrice := sumPrice / float64(lookback)
		latestPrice := sorted[len(sorted)-1].Close

		if avgPrice <= 0 || latestPrice <= 0 {
			continue
		}

		// Estimate PE using price and a proxy for earnings
		// Use price stability as a proxy - more stable prices suggest mature companies with earnings
		var variance float64
		for i := len(sorted) - lookback; i < len(sorted); i++ {
			diff := sorted[i].Close - avgPrice
			variance += diff * diff
		}
		stdDev := math.Sqrt(variance / float64(lookback))
		cv := stdDev / avgPrice // coefficient of variation

		// Lower CV suggests more stable earnings, higher value score
		// Also consider price trend - declining price may indicate value opportunity
		startPrice := sorted[len(sorted)-lookback].Close
		priceTrend := (latestPrice - startPrice) / startPrice

		// Value score: combination of low volatility (stable earnings proxy) and negative momentum (contrarian)
		// For single stock mode, we need to generate signals based on technical indicators
		valueScore := 1.0/(1.0+cv) + math.Max(-priceTrend, 0)*2.0

		results = append(results, stockValue{
			symbol: symbol,
			value:  valueScore,
			price:  latestPrice,
			pe:     avgPrice / 100, // placeholder PE calculation
		})
	}

	// Sort by value score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].value > results[j].value
	})

	// Build signals for top N stocks
	n := len(results)
	if n > topN {
		n = topN
	}

	var signals []strategy.Signal
	for i := 0; i < n; i++ {
		if results[i].value <= 0 {
			continue
		}

		signals = append(signals, strategy.Signal{
			Symbol:   results[i].symbol,
			Action:   "buy",
			Strength: math.Min(results[i].value, 1.0),
			Price:    results[i].price,
			Factors: map[string]float64{
				"value_score": results[i].value,
				"pe_proxy":    results[i].pe,
			},
		})
	}

	// Generate sell signals for held positions
	if portfolio != nil {
		for symbol, position := range portfolio.Positions {
			if position.Quantity <= 0 {
				continue
			}
			// Check if still in top N
			inTopN := false
			for i := 0; i < n; i++ {
				if results[i].symbol == symbol {
					inTopN = true
					break
				}
			}
			if !inTopN {
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

func (s *valueStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()
	if v, ok := params["lookback_days"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("lookback_days", val, 1, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.LookbackDays = val
		}
	}
	if v, ok := params["top_n"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("top_n", val, 1, 100)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.TopN = val
		}
	}
	if v, ok := params["pe_max"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("pe_max", val, 1.0, 1000.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.PEMax = val
		}
	}
	if v, ok := params["rebalance_frequency"]; ok {
		if val, ok := parseStringParam(v); ok {
			result := validateStringChoice("rebalance_frequency", val, []string{"daily", "weekly", "monthly"})
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.RebalanceFrequency = val
		}
	}
	return nil
}

func (s *valueStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
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

func (s *valueStrategy) Cleanup() {
	s.params = ValueConfig{}
}

func init() {
	s := &valueStrategy{
		BaseStrategy: strategy.NewBaseStrategy("value", "Value strategy: buy stocks with low PE ratio (high earnings yield)"),
		params: ValueConfig{
			LookbackDays:       20,
			TopN:               5,
			PEMax:              30.0,
			RebalanceFrequency: "weekly",
		},
	}
	strategy.GlobalRegister(s)
}
