// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// MeanReversionConfig holds configuration for the mean reversion strategy.
type MeanReversionConfig struct {
	MAPeriod         int
	BuyThresholdPct  float64
	SellThresholdPct float64
}

// meanReversionStrategy implements a mean reversion strategy.
// Buy when price is below moving average, sell when above.
type meanReversionStrategy struct {
	*strategy.BaseStrategy
	params MeanReversionConfig
}

func (s *meanReversionStrategy) Name() string {
	return "mean_reversion"
}

func (s *meanReversionStrategy) Description() string {
	return "Mean reversion: buy when price below moving average, sell when above"
}

func (s *meanReversionStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "ma_period",
			Type:       "int",
			Default:     20,
			Description: "Moving average period in days",
			Min:        5,
			Max:        200,
		},
		{
			Name:        "buy_threshold_pct",
			Type:       "float",
			Default:     0.95,
			Description: "Buy when price is below MA by this percentage (e.g., 0.95 = 5% below)",
			Min:        0.5,
			Max:        1.0,
		},
		{
			Name:        "sell_threshold_pct",
			Type:       "float",
			Default:     1.05,
			Description: "Sell when price is above MA by this percentage (e.g., 1.05 = 5% above)",
			Min:        1.0,
			Max:        2.0,
		},
	}
}

func (s *meanReversionStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	maPeriod := s.params.MAPeriod
	if maPeriod <= 0 {
		maPeriod = 20
	}
	buyThreshold := s.params.BuyThresholdPct
	if buyThreshold <= 0 {
		buyThreshold = 0.95
	}
	sellThreshold := s.params.SellThresholdPct
	if sellThreshold <= 0 {
		sellThreshold = 1.05
	}

	// For single/few stocks, use more relaxed thresholds
	isSingleStock := len(bars) <= 3
	if isSingleStock {
		// Relax thresholds: 0.98 instead of 0.97, 1.02 instead of 1.03
		if buyThreshold > 0.98 {
			buyThreshold = 0.98
		}
		if sellThreshold < 1.02 {
			sellThreshold = 1.02
		}
	}

	var signals []strategy.Signal

	for symbol, data := range bars {
		if len(data) < maPeriod {
			continue
		}

		// Sort by date ascending using shared utility
		sorted := sortOHLCV(data)

		// Calculate simple moving average
		var sum float64
		for i := len(sorted) - maPeriod; i < len(sorted); i++ {
			sum += sorted[i].Close
		}
		ma := sum / float64(maPeriod)

		// Get latest price
		latestPrice := sorted[len(sorted)-1].Close

		if latestPrice <= 0 || ma <= 0 {
			continue
		}

		priceRatio := latestPrice / ma

		// Generate signal based on price relative to MA
		if priceRatio < buyThreshold {
			// Price is below MA, buy signal
			strength := (buyThreshold - priceRatio) / buyThreshold
			if strength > 1.0 {
				strength = 1.0
			}
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "buy",
				Strength: strength,
				Price:    latestPrice,
			})
		} else if priceRatio > sellThreshold {
			// Price is above MA, sell signal
			strength := (priceRatio - sellThreshold) / sellThreshold
			if strength > 1.0 {
				strength = 1.0
			}

			// Only sell if we hold the position
			if portfolio != nil {
				if pos, exists := portfolio.Positions[symbol]; exists && pos.Quantity > 0 {
					signals = append(signals, strategy.Signal{
						Symbol:   symbol,
						Action:   "sell",
						Strength: strength,
						Price:    latestPrice,
					})
				}
			}
		}
	}

	return signals, nil
}

// Configure sets the strategy parameters with validation.
func (s *meanReversionStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()
	if v, ok := params["ma_period"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("ma_period", val, 1, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.MAPeriod = val
		}
	}
	if v, ok := params["buy_threshold_pct"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("buy_threshold_pct", val, -50.0, 0.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.BuyThresholdPct = val
		}
	}
	if v, ok := params["sell_threshold_pct"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("sell_threshold_pct", val, 0.0, 50.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.SellThresholdPct = val
		}
	}
	return nil
}

// Weight returns the position weight based on signal strength.
// For mean reversion: weight is proportional to deviation from MA (capped at 0.05).
func (s *meanReversionStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	weight := signal.Strength * 0.1
	if weight > 0.05 {
		weight = 0.05
	}
	if weight < 0.01 {
		weight = 0.01
	}
	return weight
}

// Cleanup releases any resources held by the strategy.
func (s *meanReversionStrategy) Cleanup() {
	s.params = MeanReversionConfig{}
}

func init() {
	// Auto-register with global registry for backward compatibility
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "Mean reversion: buy when price below moving average, sell when above"),
		params: MeanReversionConfig{
			MAPeriod:         20,
			BuyThresholdPct:  0.97,
			SellThresholdPct: 1.03,
		},
	}
	strategy.GlobalRegister(s)
}
