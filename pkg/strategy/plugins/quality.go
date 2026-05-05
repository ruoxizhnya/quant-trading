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

// QualityConfig holds configuration for the quality strategy.
type QualityConfig struct {
	LookbackDays       int
	TopN               int
	RebalanceFrequency string
	MinROE             float64
}

// qualityStrategy implements a quality factor strategy.
// Buy stocks with strong profitability metrics (ROE proxy).
type qualityStrategy struct {
	*strategy.BaseStrategy
	params QualityConfig
}

func (s *qualityStrategy) Name() string {
	return "quality"
}

func (s *qualityStrategy) Description() string {
	return "Quality strategy: buy stocks with strong profitability (ROE proxy via price stability and trend consistency)"
}

func (s *qualityStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "lookback_days",
			Type:        "int",
			Default:     20,
			Description: "Number of days for quality calculation",
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
			Name:        "min_roe",
			Type:        "float",
			Default:     0.0,
			Description: "Minimum ROE threshold",
			Min:         -1.0,
			Max:         1.0,
		},
		{
			Name:        "rebalance_frequency",
			Type:        "string",
			Default:     "weekly",
			Description: "Rebalance frequency: daily, weekly, monthly",
		},
	}
}

func (s *qualityStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
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

	type stockQuality struct {
		symbol  string
		quality float64
		price   float64
	}

	var results []stockQuality

	for symbol, data := range bars {
		if len(data) < lookback+1 {
			continue
		}

		// Sort by date ascending using shared utility
		sorted := sortOHLCV(data)

		// Calculate quality metrics from price action
		// Quality = consistent upward trend with low volatility
		var sumPrice, sumSq float64
		for i := len(sorted) - lookback; i < len(sorted); i++ {
			sumPrice += sorted[i].Close
			sumSq += sorted[i].Close * sorted[i].Close
		}
		avgPrice := sumPrice / float64(lookback)
		if avgPrice <= 0 {
			continue
		}

		variance := sumSq/float64(lookback) - avgPrice*avgPrice
		if variance < 0 {
			variance = 0
		}
		stdDev := math.Sqrt(variance)
		cv := stdDev / avgPrice

		// Trend consistency: count up days vs down days
		upDays := 0
		downDays := 0
		for i := len(sorted) - lookback; i < len(sorted); i++ {
			if i > 0 {
				if sorted[i].Close > sorted[i-1].Close {
					upDays++
				} else if sorted[i].Close < sorted[i-1].Close {
					downDays++
				}
			}
		}

		// Quality score: high up-day ratio + low volatility
		totalDays := upDays + downDays
		upRatio := 0.5
		if totalDays > 0 {
			upRatio = float64(upDays) / float64(totalDays)
		}

		// Also consider overall trend
		startPrice := sorted[len(sorted)-lookback].Close
		latestPrice := sorted[len(sorted)-1].Close
		priceTrend := (latestPrice - startPrice) / startPrice

		// Quality = trend consistency * trend direction * stability
		qualityScore := upRatio * (1.0 + priceTrend) * (1.0 / (1.0 + cv))
		if qualityScore < 0 {
			qualityScore = 0
		}

		results = append(results, stockQuality{
			symbol:  symbol,
			quality: qualityScore,
			price:   latestPrice,
		})
	}

	// Sort by quality score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].quality > results[j].quality
	})

	// Build signals for top N stocks
	n := len(results)
	if n > topN {
		n = topN
	}

	var signals []strategy.Signal
	for i := 0; i < n; i++ {
		if results[i].quality <= 0 {
			continue
		}

		signals = append(signals, strategy.Signal{
			Symbol:   results[i].symbol,
			Action:   "buy",
			Strength: math.Min(results[i].quality, 1.0),
			Price:    results[i].price,
			Factors: map[string]float64{
				"quality_score": results[i].quality,
			},
		})
	}

	// Generate sell signals for held positions not in top N
	if portfolio != nil {
		for symbol, position := range portfolio.Positions {
			if position.Quantity <= 0 {
				continue
			}
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

func (s *qualityStrategy) Configure(params map[string]any) error {
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
	if v, ok := params["min_roe"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("min_roe", val, -1.0, 1.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.MinROE = val
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

func (s *qualityStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
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

func (s *qualityStrategy) Cleanup() {
	s.params = QualityConfig{}
}

func init() {
	s := &qualityStrategy{
		BaseStrategy: strategy.NewBaseStrategy("quality", "Quality strategy: buy stocks with strong profitability"),
		params: QualityConfig{
			LookbackDays:       20,
			TopN:               5,
			MinROE:             0.0,
			RebalanceFrequency: "weekly",
		},
	}
	strategy.GlobalRegister(s)
}
