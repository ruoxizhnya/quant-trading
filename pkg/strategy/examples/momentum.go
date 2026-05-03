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

func (s *momentumStrategy) Name() string            { return "momentum" }
func (s *momentumStrategy) Description() string    { return "Simple price momentum strategy based on N-day returns" }
func (s *momentumStrategy) Configure(config map[string]any) error {
	if c, ok := config["lookback_days"]; ok {
		switch v := c.(type) {
		case float64: s.config.LookbackDays = int(v)
		case int: s.config.LookbackDays = v
		}
	}
	if c, ok := config["long_threshold"]; ok {
		switch v := c.(type) {
		case float64: s.config.LongThreshold = v
		case int: s.config.LongThreshold = float64(v)
		}
	}
	if c, ok := config["short_threshold"]; ok {
		switch v := c.(type) {
		case float64: s.config.ShortThreshold = v
		case int: s.config.ShortThreshold = float64(v)
		}
	}
	if c, ok := config["max_positions"]; ok {
		switch v := c.(type) {
		case float64: s.config.MaxPositions = int(v)
		case int: s.config.MaxPositions = v
		}
	}
	if c, ok := config["top_n"]; ok {
		switch v := c.(type) {
		case float64: s.config.TopN = int(v)
		case int: s.config.TopN = v
		}
	}
	if c, ok := config["rebalance_frequency"]; ok {
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

func (s *momentumStrategy) Signals(ctx context.Context, stocks []domain.Stock, ohlcv map[string][]domain.OHLCV, fundamental map[string][]domain.Fundamental, date time.Time) ([]domain.Signal, error) {
	if len(stocks) == 0 {
		return nil, nil
	}

	// Check if today is a rebalance day
	if !strategy.IsRebalanceDay(date, s.config.RebalanceFrequency) {
		return nil, nil // no signals on non-rebalance days
	}

	type stockMomentum struct {
		symbol  string
		momentum float64
		score   float64
	}

	var results []stockMomentum
	lookback := s.config.LookbackDays
	if lookback <= 0 {
		lookback = 20
	}
	longThresh := s.config.LongThreshold
	if longThresh == 0 {
		longThresh = 0.0  // any positive momentum = long
	}
	shortThresh := s.config.ShortThreshold
	if shortThresh == 0 {
		shortThresh = 0.0  // any negative momentum = short
	}
	topN := s.config.TopN
	if topN == 0 {
		topN = 5
	}

	for _, stock := range stocks {
		data, ok := ohlcv[stock.Symbol]
		if !ok || len(data) < lookback+1 {
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
			symbol:   stock.Symbol,
			momentum: momentum,
			score:   momentum,
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

	var signals []domain.Signal
	for i := 0; i < n; i++ {
		m := results[i].momentum
		var dir domain.Direction
		var strength float64

		if m > longThresh {
			dir = domain.DirectionLong
			strength = m
		} else if m < shortThresh {
			dir = domain.DirectionShort
			strength = -m
		} else {
			dir = domain.DirectionHold
			strength = 0
		}

		signals = append(signals, domain.Signal{
			Symbol:          results[i].symbol,
			Date:           date,
			Direction:      dir,
			Strength:       strength,
			CompositeScore: results[i].momentum,
			Factors: map[string]float64{
				"momentum_20d": results[i].momentum,
			},
		})
	}

	return signals, nil
}

func (s *momentumStrategy) Weight(sig domain.Signal, portfolioValue float64) float64 {
	if sig.Direction == domain.DirectionHold {
		return 0
	}
	return 0.2 // fixed 20% weight per position, max 5 positions = 100%
}

func (s *momentumStrategy) Cleanup() {}

// NewMomentumStrategy creates a new momentum strategy instance.
func NewMomentumStrategy() domain.Strategy {
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
