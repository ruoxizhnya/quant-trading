// Package examples contains example strategy implementations.
package examples

import (
	"context"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// MomentumConfig holds configuration for the momentum strategy.
type MomentumConfig struct {
	LookbackDays       int     `mapstructure:"lookback_days"`        // days to calculate momentum
	LongThreshold      float64 `mapstructure:"long_threshold"`        // momentum > this → long
	ShortThreshold     float64 `mapstructure:"short_threshold"`        // momentum < this → short
	MaxPositions       int     `mapstructure:"max_positions"`         // max signals to return
	TopN               int     `mapstructure:"top_n"`                 // return top N by momentum
	RebalanceFrequency string  `mapstructure:"rebalance_frequency"`    // "daily", "weekly", "monthly"
}

// momentumStrategy implements a simple price momentum strategy.
// Signals: long if price momentum > longThreshold, short if < shortThreshold.
type momentumStrategy struct {
	config MomentumConfig
}

func (s *momentumStrategy) Name() string { return "momentum" }
func (s *momentumStrategy) Description() string {
	return "Simple price momentum strategy based on N-day returns"
}

func (s *momentumStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{Name: "lookback_days", Type: "int", Default: 20, Description: "Days to calculate momentum", Min: 1, Max: 252},
		{Name: "long_threshold", Type: "float", Default: 0.0, Description: "Momentum above this → long", Min: -1, Max: 1},
		{Name: "short_threshold", Type: "float", Default: 0.0, Description: "Momentum below this → short", Min: -1, Max: 1},
		{Name: "max_positions", Type: "int", Default: 5, Description: "Max signals to return", Min: 1, Max: 100},
		{Name: "top_n", Type: "int", Default: 5, Description: "Top N candidates by momentum", Min: 1, Max: 100},
		{Name: "rebalance_frequency", Type: "string", Default: "weekly", Description: "daily | weekly | monthly"},
	}
}

func (s *momentumStrategy) Configure(params map[string]interface{}) error {
	if c, ok := params["lookback_days"]; ok {
		switch v := c.(type) {
		case float64: s.config.LookbackDays = int(v)
		case int: s.config.LookbackDays = v
		}
	}
	if c, ok := params["long_threshold"]; ok {
		switch v := c.(type) {
		case float64: s.config.LongThreshold = v
		case int: s.config.LongThreshold = float64(v)
		}
	}
	if c, ok := params["short_threshold"]; ok {
		switch v := c.(type) {
		case float64: s.config.ShortThreshold = v
		case int: s.config.ShortThreshold = float64(v)
		}
	}
	if c, ok := params["max_positions"]; ok {
		switch v := c.(type) {
		case float64: s.config.MaxPositions = int(v)
		case int: s.config.MaxPositions = v
		}
	}
	if c, ok := params["top_n"]; ok {
		switch v := c.(type) {
		case float64: s.config.TopN = int(v)
		case int: s.config.TopN = v
		}
	}
	if c, ok := params["rebalance_frequency"]; ok {
		switch v := c.(type) {
		case string: s.config.RebalanceFrequency = v
		case float64: s.config.RebalanceFrequency = string(rune(v))
		}
	}
	// Default to daily if not set
	if s.config.RebalanceFrequency == "" {
		s.config.RebalanceFrequency = "daily"
	}
	return nil
}

func (s *momentumStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	// Date comes from portfolio snapshot (or fallback to now)
	var date time.Time
	if portfolio != nil {
		date = portfolio.UpdatedAt
	}
	if date.IsZero() {
		date = time.Now()
	}

	// Check if today is a rebalance day
	if !strategy.IsRebalanceDay(date, s.config.RebalanceFrequency) {
		return nil, nil // no signals on non-rebalance days
	}

	type stockMomentum struct {
		symbol   string
		momentum float64
		score    float64
	}

	var results []stockMomentum
	lookback := s.config.LookbackDays
	if lookback <= 0 {
		lookback = 20
	}
	longThresh := s.config.LongThreshold
	shortThresh := s.config.ShortThreshold
	topN := s.config.TopN
	if topN == 0 {
		topN = 5
	}

	// Iterate over all symbols present in `bars` (the canonical "stock universe"
	// under the new strategy interface is the keys of the OHLCV map).
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

		// Find the last index where date <= signal date
		endIdx := -1
		for i := len(sorted) - 1; i >= 0; i-- {
			if !sorted[i].Date.After(date) {
				endIdx = i
				break
			}
		}
		if endIdx < 0 || endIdx < lookback {
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
			score:    momentum,
		})
	}

	// Sort by momentum descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].momentum > results[j].momentum
	})

	// Build signals
	n := len(results)
	if n > topN {
		n = topN
	}
	if n > s.config.MaxPositions && s.config.MaxPositions > 0 {
		n = s.config.MaxPositions
	}

	var signals []strategy.Signal
	for i := 0; i < n; i++ {
		m := results[i].momentum
		var dir domain.Direction
		var strength float64
		var action string

		switch {
		case m > longThresh:
			dir = domain.DirectionLong
			action = "buy"
			strength = m
		case m < shortThresh:
			dir = domain.DirectionShort
			action = "sell"
			strength = -m
		default:
			dir = domain.DirectionHold
			action = "hold"
			strength = 0
		}

		signals = append(signals, strategy.Signal{
			Symbol:    results[i].symbol,
			Action:    action,
			Strength:  strength,
			Direction: dir,
			Date:      date,
			Factors: map[string]float64{
				"momentum_20d": results[i].momentum,
			},
		})
	}

	return signals, nil
}

func (s *momentumStrategy) Weight(sig strategy.Signal, portfolioValue float64) float64 {
	if sig.Direction == domain.DirectionHold {
		return 0
	}
	return 0.2 // fixed 20% weight per position, max 5 positions = 100%
}

func (s *momentumStrategy) Cleanup() {}

// NewMomentumStrategy creates a new momentum strategy instance.
func NewMomentumStrategy() strategy.Strategy {
	return &momentumStrategy{
		config: MomentumConfig{
			LookbackDays:       20,
			LongThreshold:      0.03,
			ShortThreshold:     -0.03,
			TopN:               5,
			MaxPositions:       5,
			RebalanceFrequency: "weekly", // weekly rebalancing, not daily (avoids position accumulation)
		},
	}
}
