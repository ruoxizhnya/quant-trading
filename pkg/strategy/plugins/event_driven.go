// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// EventDrivenConfig holds configuration for the event-driven strategy.
type EventDrivenConfig struct {
	PriceJumpThreshold   float64 // daily price change threshold (default 0.03 = 3%)
	VolumeRatioThreshold float64 // volume / avg-volume threshold (default 1.5)
	LookbackDays         int     // average volume lookback (default 20)
	HoldingDays          int     // target holding period in trading days (default 5)
}

// eventDrivenStrategy implements an event-driven strategy that uses price
// jumps and volume spikes as proxies for earnings surprises and analyst
// rating changes when direct event feeds are unavailable.
//
//   - Buy signal:  price jumps up  >= PriceJumpThreshold AND volume >= avg*VolumeRatioThreshold (positive event)
//   - Sell signal: price drops     >= PriceJumpThreshold AND volume >= avg*VolumeRatioThreshold (negative event)
//
// Signal strength is a normalised blend of the jump magnitude and the
// volume ratio, mapped into [0.1, 1.0].
type eventDrivenStrategy struct {
	*strategy.BaseStrategy
	params EventDrivenConfig
}

func (s *eventDrivenStrategy) Name() string { return "event_driven" }

func (s *eventDrivenStrategy) Description() string {
	return "Event-driven strategy: detect price jumps + volume spikes as proxies for earnings surprises / analyst rating changes; buy on positive events, sell on negative events"
}

func (s *eventDrivenStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "price_jump_threshold",
			Type:        "float",
			Default:     0.03,
			Description: "Daily price change threshold to qualify as an event (e.g. 0.03 = 3%)",
			Min:         0.005,
			Max:         0.20,
		},
		{
			Name:        "volume_ratio_threshold",
			Type:        "float",
			Default:     1.5,
			Description: "Volume / average-volume ratio required to confirm an event",
			Min:         1.0,
			Max:         10.0,
		},
		{
			Name:        "lookback_days",
			Type:        "int",
			Default:     20,
			Description: "Number of days used to compute the average volume baseline",
			Min:         5,
			Max:         100,
		},
		{
			Name:        "holding_days",
			Type:        "int",
			Default:     5,
			Description: "Target holding period in trading days (attached to signal metadata)",
			Min:         1,
			Max:         60,
		},
	}
}

func (s *eventDrivenStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	_ = ctx
	if len(bars) == 0 {
		return nil, nil
	}

	priceThreshold := s.params.PriceJumpThreshold
	if priceThreshold <= 0 {
		priceThreshold = 0.03
	}
	volRatioThreshold := s.params.VolumeRatioThreshold
	if volRatioThreshold <= 0 {
		volRatioThreshold = 1.5
	}
	lookback := s.params.LookbackDays
	if lookback <= 0 {
		lookback = 20
	}
	holdingDays := s.params.HoldingDays
	if holdingDays <= 0 {
		holdingDays = 5
	}

	// Need at least lookback+1 bars: `lookback` for the avg-volume baseline
	// (prior bars) plus the latest bar whose volume we compare to that baseline.
	minLen := lookback + 1

	var signals []strategy.Signal

	for symbol, data := range bars {
		if len(data) < minLen {
			continue
		}

		sorted := sortOHLCV(data)

		latest := sorted[len(sorted)-1]
		prev := sorted[len(sorted)-2]
		if prev.Close <= 0 || latest.Close <= 0 {
			continue
		}

		priceJump := (latest.Close - prev.Close) / prev.Close

		// Average volume over the prior `lookback` bars (excluding the latest,
		// so the event-day volume is not part of the baseline).
		var volSum float64
		for i := len(sorted) - 1 - lookback; i < len(sorted)-1; i++ {
			volSum += sorted[i].Volume
		}
		avgVol := volSum / float64(lookback)
		if avgVol <= 0 {
			continue
		}
		volRatio := latest.Volume / avgVol

		// Confirm volume spike — without it, a price move is not a credible event.
		if volRatio < volRatioThreshold {
			continue
		}

		// Positive event → buy.
		if priceJump >= priceThreshold {
			strength := combineEventStrength(priceJump, priceThreshold, volRatio, volRatioThreshold)
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "buy",
				Strength: strength,
				Price:    latest.Close,
				Factors: map[string]float64{
					"price_jump":   priceJump,
					"volume_ratio": volRatio,
				},
				Metadata: map[string]interface{}{
					"holding_days": holdingDays,
					"event_type":   "positive",
					"event_date":   latest.Date,
				},
			})
			continue
		}

		// Negative event → sell (only when the position is held).
		if priceJump <= -priceThreshold {
			hold := false
			if portfolio != nil {
				if pos, ok := portfolio.Positions[symbol]; ok && pos.Quantity > 0 {
					hold = true
				}
			}
			if !hold {
				continue
			}
			strength := combineEventStrength(-priceJump, priceThreshold, volRatio, volRatioThreshold)
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "sell",
				Strength: strength,
				Price:    latest.Close,
				Factors: map[string]float64{
					"price_jump":   priceJump,
					"volume_ratio": volRatio,
				},
				Metadata: map[string]interface{}{
					"holding_days": holdingDays,
					"event_type":   "negative",
					"event_date":   latest.Date,
				},
			})
		}
	}

	return signals, nil
}

// combineEventStrength normalises the price-jump magnitude and volume ratio
// into a single signal-strength value in [0.1, 1.0]. Each dimension
// contributes 0 at its threshold and grows linearly with the excess, so a
// barely-qualifying event yields ~0.5 strength and a strongly-qualifying
// event approaches 1.0.
func combineEventStrength(jumpMag, jumpThreshold, volRatio, volRatioThreshold float64) float64 {
	jumpScore := 0.0
	if jumpThreshold > 0 {
		jumpScore = (jumpMag - jumpThreshold) / jumpThreshold
	}
	volScore := 0.0
	if volRatioThreshold > 0 {
		volScore = (volRatio - volRatioThreshold) / volRatioThreshold
	}
	strength := 0.5 + 0.25*jumpScore + 0.25*volScore
	return clampFloat(strength, 0.1, 1.0)
}

// Configure sets the strategy parameters with validation.
func (s *eventDrivenStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()
	if v, ok := params["price_jump_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("price_jump_threshold", val, 0.001, 0.5)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.PriceJumpThreshold = val
		}
	}
	if v, ok := params["volume_ratio_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("volume_ratio_threshold", val, 1.0, 20.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.VolumeRatioThreshold = val
		}
	}
	if v, ok := params["lookback_days"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("lookback_days", val, 1, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.LookbackDays = val
		}
	}
	if v, ok := params["holding_days"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("holding_days", val, 1, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.HoldingDays = val
		}
	}
	return nil
}

// Weight returns the position weight based on signal strength.
func (s *eventDrivenStrategy) Weight(signal strategy.Signal, _ float64) float64 {
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
func (s *eventDrivenStrategy) Cleanup() {
	s.params = EventDrivenConfig{}
}

func init() {
	s := &eventDrivenStrategy{
		BaseStrategy: strategy.NewBaseStrategy("event_driven", "Event-driven: price jump + volume spike proxy for earnings/analyst events"),
		params: EventDrivenConfig{
			PriceJumpThreshold:   0.03,
			VolumeRatioThreshold: 1.5,
			LookbackDays:         20,
			HoldingDays:          5,
		},
	}
	strategy.GlobalRegister(s)
}
