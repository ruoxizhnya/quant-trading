// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// RiskParityConfig holds configuration for the risk parity strategy.
type RiskParityConfig struct {
	LookbackDays  int     // volatility lookback period (default 60)
	MinWeight     float64 // minimum position weight (default 0.01)
	MaxWeight     float64 // maximum position weight (default 0.30)
	RebalanceDays int     // rebalance frequency (default 5)
}

// riskParityStrategy implements equal risk contribution across positions.
// Weight ∝ 1/volatility, then normalize and clip to [MinWeight, MaxWeight].
type riskParityStrategy struct {
	*strategy.BaseStrategy
	params RiskParityConfig
}

func (s *riskParityStrategy) Name() string {
	return "risk_parity"
}

func (s *riskParityStrategy) Description() string {
	return "Risk parity strategy: equal risk contribution across positions, weight ∝ 1/volatility"
}

func (s *riskParityStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "lookback_days",
			Type:        "int",
			Default:     60,
			Description: "Volatility lookback period in trading days",
			Min:         5,
			Max:         252,
		},
		{
			Name:        "min_weight",
			Type:        "float",
			Default:     0.01,
			Description: "Minimum position weight",
			Min:         0,
			Max:         1,
		},
		{
			Name:        "max_weight",
			Type:        "float",
			Default:     0.30,
			Description: "Maximum position weight",
			Min:         0,
			Max:         1,
		},
		{
			Name:        "rebalance_days",
			Type:        "int",
			Default:     5,
			Description: "Rebalance frequency in trading days",
			Min:         1,
			Max:         60,
		},
	}
}

// Configure sets the strategy parameters with validation.
func (s *riskParityStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()

	if v, ok := params["lookback_days"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("lookback_days", val, 5, 252)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.LookbackDays = val
		}
	}
	if v, ok := params["min_weight"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("min_weight", val, 0, 1)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.MinWeight = val
		}
	}
	if v, ok := params["max_weight"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("max_weight", val, 0, 1)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.MaxWeight = val
		}
	}
	if v, ok := params["rebalance_days"]; ok {
		if val, ok := parseIntParam(v); ok {
			result := validateIntRange("rebalance_days", val, 1, 60)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.RebalanceDays = val
		}
	}

	// Ensure min_weight <= max_weight.
	if s.params.MinWeight > s.params.MaxWeight {
		return fmt.Errorf("invalid parameter: min_weight (%v) must be <= max_weight (%v)",
			s.params.MinWeight, s.params.MaxWeight)
	}
	return nil
}

// GenerateSignals computes inverse-volatility target weights for each
// symbol in the universe and emits buy/sell signals to adjust current
// holdings toward those target weights.
//
// Algorithm:
//  1. For each symbol, compute the sample standard deviation of daily
//     returns over the last LookbackDays bars.
//  2. Raw weight ∝ 1/volatility; normalize so weights sum to 1.
//  3. Clip each weight to [MinWeight, MaxWeight].
//  4. Emit buy signals for symbols whose target weight exceeds their
//     current portfolio weight (or that are not held).
//  5. Emit sell signals for held symbols whose target weight is below
//     their current weight (trim), and for held symbols that dropped
//     out of the universe (exit).
func (s *riskParityStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	lookback := s.params.LookbackDays
	if lookback <= 0 {
		lookback = 60
	}
	minW := s.params.MinWeight
	if minW <= 0 {
		minW = 0.01
	}
	maxW := s.params.MaxWeight
	if maxW <= 0 {
		maxW = 0.30
	}
	if minW > maxW {
		minW, maxW = maxW, minW
	}

	type stockVol struct {
		symbol string
		vol    float64
		price  float64
	}

	var universe []stockVol
	for symbol, data := range bars {
		sorted := sortOHLCV(data)
		if len(sorted) < lookback+1 {
			continue
		}
		endIdx := len(sorted) - 1
		startIdx := endIdx - lookback
		if startIdx < 0 {
			continue
		}

		returns := make([]float64, 0, lookback)
		for i := startIdx + 1; i <= endIdx; i++ {
			prev := sorted[i-1].Close
			if prev <= 0 {
				continue
			}
			returns = append(returns, (sorted[i].Close-prev)/prev)
		}
		if len(returns) < 2 {
			continue
		}
		vol := statistics.SampleStdDev(returns)
		if vol <= 0 {
			// Degenerate (zero-volatility) series cannot contribute to risk
			// parity weighting; skip to avoid division by zero.
			continue
		}
		universe = append(universe, stockVol{
			symbol: symbol,
			vol:    vol,
			price:  sorted[endIdx].Close,
		})
	}

	if len(universe) == 0 {
		return nil, nil
	}

	// weight ∝ 1/volatility, then normalize to sum=1.
	var invSum float64
	for _, x := range universe {
		invSum += 1.0 / x.vol
	}
	if invSum <= 0 {
		return nil, nil
	}

	target := make(map[string]float64, len(universe))
	for _, x := range universe {
		target[x.symbol] = (1.0 / x.vol) / invSum
	}
	// Clip to [minW, maxW] (per spec: normalize then clip).
	for sym, w := range target {
		target[sym] = clampFloat(w, minW, maxW)
	}

	// Current portfolio weights (if a portfolio is supplied).
	currentWeight := make(map[string]float64)
	if portfolio != nil {
		pv := portfolio.TotalValue
		if pv <= 0 {
			pv = portfolio.Cash
			for _, p := range portfolio.Positions {
				pv += p.MarketValue
			}
		}
		if pv > 0 {
			for sym, p := range portfolio.Positions {
				if p.Quantity > 0 {
					currentWeight[sym] = safeDivide(p.MarketValue, pv)
				}
			}
		}
	}

	// Deterministic output order.
	sort.Slice(universe, func(i, j int) bool {
		return universe[i].symbol < universe[j].symbol
	})

	var signals []strategy.Signal
	for _, x := range universe {
		tw := target[x.symbol]
		cw := currentWeight[x.symbol]
		price := x.price
		if price <= 0 && portfolio != nil {
			if p, ok := portfolio.Positions[x.symbol]; ok {
				price = p.CurrentPrice
			}
		}

		switch {
		case tw > cw:
			signals = append(signals, strategy.Signal{
				Symbol:   x.symbol,
				Action:   "buy",
				Strength: tw,
				Price:    price,
				Metadata: map[string]any{
					"target_weight":  tw,
					"current_weight": cw,
					"volatility":     x.vol,
				},
			})
		case tw < cw:
			signals = append(signals, strategy.Signal{
				Symbol:   x.symbol,
				Action:   "sell",
				Strength: cw - tw,
				Price:    price,
				Metadata: map[string]any{
					"target_weight":  tw,
					"current_weight": cw,
					"volatility":     x.vol,
				},
			})
		}
		// tw == cw: no rebalance needed, emit nothing.
	}

	// Exit positions that dropped out of the universe.
	if portfolio != nil {
		for sym, p := range portfolio.Positions {
			if p.Quantity <= 0 {
				continue
			}
			if _, ok := target[sym]; ok {
				continue
			}
			signals = append(signals, strategy.Signal{
				Symbol:   sym,
				Action:   "sell",
				Strength: 1.0,
				Price:    p.CurrentPrice,
				Metadata: map[string]any{
					"target_weight":  0.0,
					"current_weight": currentWeight[sym],
				},
			})
		}
	}

	return signals, nil
}

// Weight returns the position weight for a signal. For buy signals it
// returns the target weight clamped to [MinWeight, MaxWeight]; for sell
// signals it returns the fraction to reduce (clamped to [0, 1]).
func (s *riskParityStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	minW := s.params.MinWeight
	if minW <= 0 {
		minW = 0.01
	}
	maxW := s.params.MaxWeight
	if maxW <= 0 {
		maxW = 0.30
	}
	if minW > maxW {
		minW, maxW = maxW, minW
	}

	if signal.Action == "sell" {
		w := signal.Strength
		if w < 0 {
			w = 0
		}
		if w > 1 {
			w = 1
		}
		return w
	}
	// buy: target weight clamped to [minW, maxW]
	return clampFloat(signal.Strength, minW, maxW)
}

// Cleanup releases any resources held by the strategy.
func (s *riskParityStrategy) Cleanup() {
	s.Lock()
	defer s.Unlock()
	s.params = RiskParityConfig{}
}

func init() {
	s := &riskParityStrategy{
		BaseStrategy: strategy.NewBaseStrategy(
			"risk_parity",
			"Risk parity: equal risk contribution via inverse-volatility weighting",
		),
		params: RiskParityConfig{
			LookbackDays:  60,
			MinWeight:     0.01,
			MaxWeight:     0.30,
			RebalanceDays: 5,
		},
	}
	strategy.GlobalRegister(s)
}
