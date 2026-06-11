// Package plugins contains built-in strategy implementations.
// This file provides an example plugin strategy that can be compiled as a .so plugin.
//
// Build as plugin:
//   go build -buildmode=plugin -o example_strategy.so ./example_plugin.go
//
// The plugin must export a symbol named "Strategy" that implements strategy.Strategy.
package plugins

import (
	"context"
	"math"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ExampleConfig holds configuration for the example strategy.
type ExampleConfig struct {
	Period   int
	TopN     int
	BuyThreshold  float64
	SellThreshold float64
}

// exampleStrategy implements a simple moving average crossover strategy.
// It serves as a reference implementation for plugin-based strategies.
type exampleStrategy struct {
	*strategy.BaseStrategy
	params ExampleConfig
}

// Name returns the strategy name.
func (s *exampleStrategy) Name() string {
	return "example_ma_cross"
}

// Description returns the strategy description.
func (s *exampleStrategy) Description() string {
	return "Example MA Crossover: buy when short MA crosses above long MA, sell when crossing below"
}

// Parameters returns configurable parameters.
func (s *exampleStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "period",
			Type:        "int",
			Default:     20,
			Description: "Moving average period",
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
			Name:        "buy_threshold",
			Type:        "float",
			Default:     0.02,
			Description: "Minimum crossover strength to trigger buy",
			Min:         0.001,
			Max:         0.1,
		},
		{
			Name:        "sell_threshold",
			Type:        "float",
			Default:     -0.02,
			Description: "Maximum crossover strength to trigger sell",
			Min:         -0.1,
			Max:         -0.001,
		},
	}
}

// Configure sets the strategy parameters.
func (s *exampleStrategy) Configure(params map[string]any) error {
	if v, ok := params["period"]; ok {
		if val, ok := parseIntParam(v); ok {
			s.params.Period = val
		}
	}
	if v, ok := params["top_n"]; ok {
		if val, ok := parseIntParam(v); ok {
			s.params.TopN = val
		}
	}
	if v, ok := params["buy_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			s.params.BuyThreshold = val
		}
	}
	if v, ok := params["sell_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			s.params.SellThreshold = val
		}
	}
	return nil
}

// GenerateSignals generates trading signals based on MA crossover.
func (s *exampleStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	period := s.params.Period
	if period <= 0 {
		period = 20
	}
	topN := s.params.TopN
	if topN <= 0 {
		topN = 5
	}
	buyThreshold := s.params.BuyThreshold
	if buyThreshold == 0 {
		buyThreshold = 0.02
	}
	sellThreshold := s.params.SellThreshold
	if sellThreshold == 0 {
		sellThreshold = -0.02
	}

	type signalScore struct {
		symbol   string
		score    float64
		price    float64
		crossPct float64
	}

	var results []signalScore

	for symbol, data := range bars {
		if len(data) < period*2+1 {
			continue
		}

		sorted := sortOHLCV(data)

		// Calculate short and long moving averages
		shortWindow := sorted[len(sorted)-period:]
		longWindow := sorted[len(sorted)-period*2:]

		shortMA := statistics.Mean(extractClosePrices(shortWindow))
		longMA := statistics.Mean(extractClosePrices(longWindow))

		if longMA <= 0 {
			continue
		}

		// Crossover percentage
		crossPct := (shortMA - longMA) / longMA
		latestPrice := sorted[len(sorted)-1].Close

		results = append(results, signalScore{
			symbol:   symbol,
			score:    crossPct,
			price:    latestPrice,
			crossPct: crossPct,
		})
	}

	// Sort by crossover strength descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	var signals []strategy.Signal

	// Generate buy signals for top N with positive crossover
	for i := 0; i < len(results) && i < topN; i++ {
		if results[i].crossPct >= buyThreshold && results[i].price > 0 {
			strength := math.Min(results[i].crossPct/buyThreshold, 1.0)
			signals = append(signals, strategy.Signal{
				Symbol:   results[i].symbol,
				Action:   "buy",
				Strength: strength,
				Price:    results[i].price,
				Factors: map[string]float64{
					"cross_pct": results[i].crossPct,
					"short_ma":  0,
					"long_ma":   0,
				},
			})
		}
	}

	// Generate sell signals for negative crossover
	if portfolio != nil {
		for _, r := range results {
			if r.crossPct <= sellThreshold {
				if pos, ok := portfolio.Positions[r.symbol]; ok && pos.Quantity > 0 {
					strength := math.Min(math.Abs(r.crossPct/sellThreshold), 1.0)
					signals = append(signals, strategy.Signal{
						Symbol:   r.symbol,
						Action:   "sell",
						Strength: strength,
						Price:    r.price,
						Factors: map[string]float64{
							"cross_pct": r.crossPct,
						},
					})
				}
			}
		}
	}

	return signals, nil
}

// Weight returns position weight based on signal strength.
func (s *exampleStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	w := signal.Strength * 0.10
	if w > 0.08 {
		w = 0.08
	}
	if w < 0.01 {
		w = 0.01
	}
	return w
}

// Cleanup releases resources.
func (s *exampleStrategy) Cleanup() {
	s.params = ExampleConfig{}
}

// Strategy is the exported symbol for plugin loading.
// The plugin loader looks up this symbol to retrieve the strategy instance.
var Strategy strategy.Strategy = &exampleStrategy{
	BaseStrategy: strategy.NewBaseStrategy("example_ma_cross", "Example MA Crossover: buy when short MA crosses above long MA, sell when crossing below"),
	params: ExampleConfig{
		Period:        20,
		TopN:          5,
		BuyThreshold:  0.02,
		SellThreshold: -0.02,
	},
}
