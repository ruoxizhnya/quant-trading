// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// MeanReversionConfig holds configuration for the Bollinger + RSI mean reversion strategy.
type MeanReversionConfig struct {
	BollingerPeriod  int     // Bollinger Bands SMA period (default 20)
	BollingerStdDev  float64 // Number of standard deviations (default 2.0)
	RSIPeriod         int     // RSI calculation period (default 14)
	RSIOversold       float64 // RSI oversold threshold (default 30)
	RSIOverbought     float64 // RSI overbought threshold (default 70)
}

// meanReversionStrategy implements a mean reversion strategy using
// Bollinger Bands + RSI confirmation.
//
// Buy signal: price touches/breaks below the lower Bollinger Band AND RSI < oversold threshold.
// Sell signal: price touches/breaks above the upper Bollinger Band AND RSI > overbought threshold.
type meanReversionStrategy struct {
	*strategy.BaseStrategy
	params MeanReversionConfig
}

func (s *meanReversionStrategy) Name() string {
	return "mean_reversion"
}

func (s *meanReversionStrategy) Description() string {
	return "Bollinger Bands + RSI mean reversion: buy when price below lower band and RSI oversold, sell when above upper band and RSI overbought"
}

func (s *meanReversionStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "bollinger_period",
			Type:       "int",
			Default:     20,
			Description: "Bollinger Bands SMA period in days",
			Min:        5,
			Max:        100,
		},
		{
			Name:        "bollinger_stddev",
			Type:       "float",
			Default:     2.0,
			Description: "Number of standard deviations for Bollinger Bands",
			Min:        0.5,
			Max:        4.0,
		},
		{
			Name:        "rsi_period",
			Type:       "int",
			Default:     14,
			Description: "RSI calculation period in days",
			Min:        2,
			Max:        50,
		},
		{
			Name:        "rsi_oversold",
			Type:       "float",
			Default:     30,
			Description: "RSI oversold threshold (buy when RSI below this)",
			Min:        5,
			Max:        50,
		},
		{
			Name:        "rsi_overbought",
			Type:       "float",
			Default:     70,
			Description: "RSI overbought threshold (sell when RSI above this)",
			Min:        50,
			Max:        95,
		},
	}
}

func (s *meanReversionStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	bollPeriod := s.params.BollingerPeriod
	if bollPeriod <= 0 {
		bollPeriod = 20
	}
	bollStdDev := s.params.BollingerStdDev
	if bollStdDev <= 0 {
		bollStdDev = 2.0
	}
	rsiPeriod := s.params.RSIPeriod
	if rsiPeriod <= 0 {
		rsiPeriod = 14
	}
	rsiOversold := s.params.RSIOversold
	if rsiOversold <= 0 {
		rsiOversold = 30
	}
	rsiOverbought := s.params.RSIOverbought
	if rsiOverbought <= 0 {
		rsiOverbought = 70
	}

	// Need enough data for both Bollinger and RSI
	minDataLen := bollPeriod
	if rsiPeriod > minDataLen {
		minDataLen = rsiPeriod
	}
	minDataLen++ // need one extra bar for the latest price

	var signals []strategy.Signal

	for symbol, data := range bars {
		if len(data) < minDataLen {
			continue
		}

		sorted := sortOHLCV(data)
		closes := extractClosePrices(sorted)

		latestPrice := closes[len(closes)-1]
		if latestPrice <= 0 {
			continue
		}

		// Calculate Bollinger Bands
		upperBand, lowerBand := calculateBollingerBands(closes, bollPeriod, bollStdDev)
		if upperBand == 0 && lowerBand == 0 {
			continue // not enough data
		}

		// Calculate RSI
		rsi := calculateRSI(closes, rsiPeriod)

		// Buy signal: price below lower band AND RSI oversold
		if latestPrice <= lowerBand && rsi <= rsiOversold {
			// Strength based on how far below the band and how oversold
			bandDeviation := (lowerBand - latestPrice) / lowerBand
			rsiDeviation := (rsiOversold - rsi) / rsiOversold
			strength := (bandDeviation + rsiDeviation) / 2
			if strength > 1.0 {
				strength = 1.0
			}
			if strength < 0.1 {
				strength = 0.1
			}
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "buy",
				Strength: strength,
				Price:    latestPrice,
			})
		}

		// Sell signal: price above upper band AND RSI overbought
		if latestPrice >= upperBand && rsi >= rsiOverbought {
			// Only sell if we hold the position
			if portfolio != nil {
				if pos, exists := portfolio.Positions[symbol]; exists && pos.Quantity > 0 {
					bandDeviation := (latestPrice - upperBand) / upperBand
					rsiDeviation := (rsi - rsiOverbought) / (100 - rsiOverbought)
					strength := (bandDeviation + rsiDeviation) / 2
					if strength > 1.0 {
						strength = 1.0
					}
					if strength < 0.1 {
						strength = 0.1
					}
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

// calculateBollingerBands computes the upper and lower Bollinger Bands
// using the last `period` closing prices.
// Returns (0, 0) if not enough data.
func calculateBollingerBands(closes []float64, period int, numStdDev float64) (upper, lower float64) {
	if len(closes) < period {
		return 0, 0
	}

	// Use last `period` prices
	start := len(closes) - period
	slice := closes[start:]

	// Calculate SMA
	var sum float64
	for _, p := range slice {
		sum += p
	}
	sma := sum / float64(period)

	// Calculate standard deviation
	var sqSum float64
	for _, p := range slice {
		diff := p - sma
		sqSum += diff * diff
	}
	stdDev := math.Sqrt(sqSum / float64(period))

	upper = sma + numStdDev*stdDev
	lower = sma - numStdDev*stdDev
	return upper, lower
}

// calculateRSI computes the Relative Strength Index using the
// last `period` closing prices.
// Returns 50 (neutral) if not enough data or all changes are zero.
func calculateRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}

	// Use last `period+1` prices to get `period` changes
	start := len(closes) - period - 1
	var avgGain, avgLoss float64

	// First average gain/loss
	for i := start + 1; i <= start+period; i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss += -change
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		if avgGain == 0 {
			return 50 // no movement
		}
		return 100 // all gains
	}

	rs := avgGain / avgLoss
	rsi := 100 - 100/(1+rs)
	return rsi
}

// Configure sets the strategy parameters with validation.
func (s *meanReversionStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()
	if v, ok := params["bollinger_period"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("bollinger_period", val, 1, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.BollingerPeriod = val
		}
	}
	if v, ok := params["bollinger_stddev"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("bollinger_stddev", val, 0.1, 10.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.BollingerStdDev = val
		}
	}
	if v, ok := params["rsi_period"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("rsi_period", val, 1, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.RSIPeriod = val
		}
	}
	if v, ok := params["rsi_oversold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("rsi_oversold", val, 1.0, 49.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.RSIOversold = val
		}
	}
	if v, ok := params["rsi_overbought"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("rsi_overbought", val, 51.0, 99.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.RSIOverbought = val
		}
	}
	return nil
}

// Weight returns the position weight based on signal strength.
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
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "Bollinger Bands + RSI mean reversion"),
		params: MeanReversionConfig{
			BollingerPeriod:  20,
			BollingerStdDev:  2.0,
			RSIPeriod:         14,
			RSIOversold:       30,
			RSIOverbought:     70,
		},
	}
	strategy.GlobalRegister(s)
}
