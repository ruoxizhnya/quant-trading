// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/data/sentiment"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// SentimentConfig holds configuration for the contrarian sentiment strategy.
type SentimentConfig struct {
	BuyThreshold  float64 // buy when daily sentiment <= this (default -0.3, 过度悲观)
	SellThreshold float64 // sell when daily sentiment >= this (default 0.7, 过度乐观)
	MinConfidence float64 // minimum aggregator confidence to act on a signal (default 0.3)
}

// sentimentStrategy implements a contrarian sentiment-driven strategy.
//
// It consumes daily aggregated sentiment scores from a SentimentAggregator
// (news / social / analyst) and fades the crowd:
//   - Buy  when sentiment <= BuyThreshold  (过度悲观 → 逆向买入)
//   - Sell when sentiment >= SellThreshold (过度乐观 → 逆向卖出)
//
// Only signals whose aggregator confidence >= MinConfidence are emitted.
// The aggregator reference is optional; when nil, no signals are produced
// (the strategy coexists cleanly with backtests that have no sentiment feed).
type sentimentStrategy struct {
	*strategy.BaseStrategy
	params     SentimentConfig
	aggregator *sentiment.SentimentAggregator
}

// SetAggregator wires a SentimentAggregator into the strategy. This must be
// called before GenerateSignals if sentiment-driven signals are desired.
// Safe to call concurrently with GenerateSignals (the pointer is read under
// the BaseStrategy read-lock on the hot path).
func (s *sentimentStrategy) SetAggregator(sa *sentiment.SentimentAggregator) {
	s.Lock()
	defer s.Unlock()
	s.aggregator = sa
}

func (s *sentimentStrategy) Name() string { return "sentiment" }

func (s *sentimentStrategy) Description() string {
	return "Contrarian sentiment strategy: buy when sentiment is overly pessimistic, sell when overly optimistic, using SentimentAggregator daily scores"
}

func (s *sentimentStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "buy_threshold",
			Type:        "float",
			Default:     -0.3,
			Description: "Buy when daily sentiment score falls at/below this threshold (contrarian buy on pessimism)",
			Min:         -1.0,
			Max:         0.0,
		},
		{
			Name:        "sell_threshold",
			Type:        "float",
			Default:     0.7,
			Description: "Sell when daily sentiment score rises at/above this threshold (contrarian sell on optimism)",
			Min:         0.0,
			Max:         1.0,
		},
		{
			Name:        "min_confidence",
			Type:        "float",
			Default:     0.3,
			Description: "Minimum aggregator confidence required to emit a signal (0 = no filter)",
			Min:         0.0,
			Max:         1.0,
		},
	}
}

func (s *sentimentStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	_ = ctx
	if len(bars) == 0 {
		return nil, nil
	}

	s.RLock()
	agg := s.aggregator
	s.RUnlock()
	if agg == nil {
		// No sentiment feed wired — produce no signals rather than erroring,
		// so the strategy can coexist with backtests that have no sentiment data.
		return nil, nil
	}

	buyThreshold := s.params.BuyThreshold
	if buyThreshold == 0 {
		buyThreshold = -0.3
	}
	sellThreshold := s.params.SellThreshold
	if sellThreshold == 0 {
		sellThreshold = 0.7
	}
	minConf := s.params.MinConfidence
	if minConf < 0 {
		minConf = 0.3
	}

	var signals []strategy.Signal

	for symbol, data := range bars {
		if len(data) == 0 {
			continue
		}
		sorted := sortOHLCV(data)
		latest := sorted[len(sorted)-1]
		if latest.Close <= 0 {
			continue
		}

		score, err := agg.GetDailyScore(symbol, latest.Date)
		if err != nil {
			// No sentiment data for this symbol/date → skip silently.
			continue
		}
		if score.Confidence < minConf {
			continue
		}

		// Contrarian buy: crowd is overly pessimistic.
		if score.Score <= buyThreshold {
			strength := contrarianStrength(score.Score, buyThreshold, sellThreshold, minConf, score.Confidence, true)
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "buy",
				Strength: strength,
				Price:    latest.Close,
				Factors: map[string]float64{
					"sentiment_score":      score.Score,
					"sentiment_confidence": score.Confidence,
				},
				Metadata: map[string]interface{}{
					"strategy":   "contrarian",
					"event_date": latest.Date,
				},
			})
			continue
		}

		// Contrarian sell: crowd is overly optimistic (only when held).
		if score.Score >= sellThreshold {
			hold := false
			if portfolio != nil {
				if pos, ok := portfolio.Positions[symbol]; ok && pos.Quantity > 0 {
					hold = true
				}
			}
			if !hold {
				continue
			}
			strength := contrarianStrength(score.Score, buyThreshold, sellThreshold, minConf, score.Confidence, false)
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "sell",
				Strength: strength,
				Price:    latest.Close,
				Factors: map[string]float64{
					"sentiment_score":      score.Score,
					"sentiment_confidence": score.Confidence,
				},
				Metadata: map[string]interface{}{
					"strategy":   "contrarian",
					"event_date": latest.Date,
				},
			})
		}
	}

	return signals, nil
}

// contrarianStrength maps the distance from the threshold (and the confidence)
// into a signal-strength value in [0.1, 1.0]. isBuy selects the direction:
// for buys, more negative sentiment → stronger; for sells, more positive → stronger.
//
// At-threshold strength is 0.5; extremity contributes up to +0.35, confidence
// up to +0.15.
func contrarianStrength(score, buyThreshold, sellThreshold, minConf, confidence float64, isBuy bool) float64 {
	var excess float64
	rangeSize := 1.0
	if isBuy {
		// distance from threshold toward -1.0
		excess = buyThreshold - score
		rangeSize = buyThreshold + 1.0 // e.g. -0.3 + 1.0 = 0.7
	} else {
		// distance from threshold toward +1.0
		excess = score - sellThreshold
		rangeSize = 1.0 - sellThreshold // e.g. 1.0 - 0.7 = 0.3
	}
	if rangeSize <= 0 {
		rangeSize = 1.0
	}
	scoreNorm := excess / rangeSize

	// Confidence contribution: 0 at minConf, 1 at confidence 1.0.
	confRange := 1.0 - minConf
	confNorm := 1.0
	if confRange > 0 {
		confNorm = (confidence - minConf) / confRange
	}

	strength := 0.5 + 0.35*clampFloat(scoreNorm, 0, 1) + 0.15*clampFloat(confNorm, 0, 1)
	return clampFloat(strength, 0.1, 1.0)
}

// Configure sets the strategy parameters with validation.
func (s *sentimentStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()
	if v, ok := params["buy_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("buy_threshold", val, -1.0, -0.01)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.BuyThreshold = val
		}
	}
	if v, ok := params["sell_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("sell_threshold", val, 0.01, 1.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.SellThreshold = val
		}
	}
	if v, ok := params["min_confidence"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("min_confidence", val, 0.0, 1.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.MinConfidence = val
		}
	}
	return nil
}

// Weight returns the position weight based on signal strength.
func (s *sentimentStrategy) Weight(signal strategy.Signal, _ float64) float64 {
	w := signal.Strength * 0.10
	if w > 0.05 {
		w = 0.05
	}
	if w < 0.01 {
		w = 0.01
	}
	return w
}

// Cleanup releases any resources held by the strategy.
func (s *sentimentStrategy) Cleanup() {
	s.Lock()
	defer s.Unlock()
	s.params = SentimentConfig{}
	s.aggregator = nil
}

func init() {
	s := &sentimentStrategy{
		BaseStrategy: strategy.NewBaseStrategy("sentiment", "Contrarian sentiment: fade extreme optimism/pessimism from SentimentAggregator"),
		params: SentimentConfig{
			BuyThreshold:  -0.3,
			SellThreshold: 0.7,
			MinConfidence: 0.3,
		},
	}
	strategy.GlobalRegister(s)
}
