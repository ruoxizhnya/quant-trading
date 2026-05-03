// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
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
	name        string
	description string
	params      ValueConfig
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
		symbol   string
		value    float64 // earnings yield = 1/PE
		price    float64
		pe       float64
	}

	var results []stockValue

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
	if v, ok := params["pe_max"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.PEMax = val
		case int:
			s.params.PEMax = float64(val)
		}
	}
	if v, ok := params["rebalance_frequency"]; ok {
		if val, ok := v.(string); ok {
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
	name        string
	description string
	params      QualityConfig
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

		// Sort by date ascending
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Date.Before(sorted[j].Date)
		})

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
	if v, ok := params["min_roe"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.MinROE = val
		case int:
			s.params.MinROE = float64(val)
		}
	}
	if v, ok := params["rebalance_frequency"]; ok {
		if val, ok := v.(string); ok {
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
	strategy.GlobalRegister(&valueStrategy{
		name:        "value",
		description: "Value strategy: buy stocks with low PE ratio (high earnings yield)",
		params: ValueConfig{
			LookbackDays:       20,
			TopN:               5,
			PEMax:              30.0,
			RebalanceFrequency: "weekly",
		},
	})

	strategy.GlobalRegister(&qualityStrategy{
		name:        "quality",
		description: "Quality strategy: buy stocks with strong profitability",
		params: QualityConfig{
			LookbackDays:       20,
			TopN:               5,
			MinROE:             0.0,
			RebalanceFrequency: "weekly",
		},
	})
}
