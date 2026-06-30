package expression

import (
	"fmt"
	"math"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// SizingMethod selects the position-sizing algorithm.
type SizingMethod string

const (
	// SizingEqual gives each signal an equal share: MaxTotal / N,
	// capped by MaxPerStock.
	SizingEqual SizingMethod = "equal"
	// SizingStrengthProp gives each signal a share proportional to its
	// Strength, normalized so the total equals MaxTotal (before per-stock
	// caps). Falls back to SizingEqual when total strength is zero.
	SizingStrengthProp SizingMethod = "strength_prop"
	// SizingFixed gives each signal a fixed FixedWeight, capped by
	// MaxPerStock.
	SizingFixed SizingMethod = "fixed"
)

// SizingConfig configures a PositionSizer.
//
// All weights are clamped to [0, MaxPerStock] and the total is clamped
// to MaxTotal (scaling down proportionally if caps are exceeded).
type SizingConfig struct {
	Method      SizingMethod
	FixedWeight float64 // per-signal weight for SizingFixed (default 0.05)
	MaxPerStock float64 // cap per single position (default 0.10)
	MaxTotal    float64 // cap total exposure (default 1.0)
}

// PositionSizer computes target weights for a set of signals.
//
// Config-driven (no DSL): the sizing method is chosen at construction
// and applied uniformly to all signals. This keeps the parser surface
// stable; a future enhancement may allow per-signal sizing formulas.
type PositionSizer struct {
	cfg SizingConfig
}

// NewPositionSizer validates the config and applies defaults.
//
// Defaults: FixedWeight=0.05, MaxPerStock=0.10, MaxTotal=1.0.
// Returns an error if Method is unrecognized or any cap is negative.
func NewPositionSizer(cfg SizingConfig) (*PositionSizer, error) {
	if cfg.Method == "" {
		cfg.Method = SizingEqual
	}
	switch cfg.Method {
	case SizingEqual, SizingStrengthProp, SizingFixed:
		// ok
	default:
		return nil, fmt.Errorf("sizing: unknown method %q", cfg.Method)
	}
	if cfg.FixedWeight <= 0 {
		cfg.FixedWeight = 0.05
	}
	if cfg.MaxPerStock <= 0 {
		cfg.MaxPerStock = 0.10
	}
	if cfg.MaxTotal <= 0 {
		cfg.MaxTotal = 1.0
	}
	if cfg.MaxPerStock < 0 || cfg.MaxTotal < 0 || cfg.FixedWeight < 0 {
		return nil, fmt.Errorf("sizing: caps cannot be negative")
	}
	return &PositionSizer{cfg: cfg}, nil
}

// Size computes target weights for each signal, keyed by symbol.
//
// The portfolioValue parameter is accepted for future use (risk-parity
// sizing that depends on absolute capital); current methods are
// fractional and don't require it.
//
// Returns an empty map for empty signals. Weights are clamped to
// [0, MaxPerStock] and scaled down proportionally if their sum exceeds
// MaxTotal.
func (p *PositionSizer) Size(signals []strategy.Signal, portfolioValue float64) (map[string]float64, error) {
	_ = portfolioValue // reserved for future risk-parity sizing

	if len(signals) == 0 {
		return map[string]float64{}, nil
	}

	// Deduplicate by symbol (last signal wins, matching engine semantics).
	bySymbol := make(map[string]float64, len(signals))
	for _, s := range signals {
		bySymbol[s.Symbol] = s.Strength
	}

	// Sort symbols for deterministic output.
	symbols := make([]string, 0, len(bySymbol))
	for s := range bySymbol {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)

	weights := make(map[string]float64, len(symbols))
	switch p.cfg.Method {
	case SizingEqual:
		each := p.cfg.MaxTotal / float64(len(symbols))
		if each > p.cfg.MaxPerStock {
			each = p.cfg.MaxPerStock
		}
		for _, s := range symbols {
			weights[s] = each
		}

	case SizingStrengthProp:
		total := 0.0
		for _, s := range symbols {
			strength := bySymbol[s]
			if !math.IsNaN(strength) && strength > 0 {
				total += strength
			}
		}
		if total == 0 {
			// Fall back to equal weight when all strengths are zero/NaN.
			each := p.cfg.MaxTotal / float64(len(symbols))
			if each > p.cfg.MaxPerStock {
				each = p.cfg.MaxPerStock
			}
			for _, s := range symbols {
				weights[s] = each
			}
		} else {
			for _, s := range symbols {
				strength := bySymbol[s]
				if math.IsNaN(strength) || strength <= 0 {
					weights[s] = 0
					continue
				}
				w := p.cfg.MaxTotal * strength / total
				if w > p.cfg.MaxPerStock {
					w = p.cfg.MaxPerStock
				}
				weights[s] = w
			}
		}

	case SizingFixed:
		w := p.cfg.FixedWeight
		if w > p.cfg.MaxPerStock {
			w = p.cfg.MaxPerStock
		}
		for _, s := range symbols {
			weights[s] = w
		}
	}

	// Enforce MaxTotal: if sum exceeds it, scale down proportionally.
	sum := 0.0
	for _, w := range weights {
		sum += w
	}
	if sum > p.cfg.MaxTotal && sum > 0 {
		scale := p.cfg.MaxTotal / sum
		for s := range weights {
			weights[s] *= scale
		}
	}

	return weights, nil
}
