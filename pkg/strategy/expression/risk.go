package expression

import (
	"fmt"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// RiskConfig configures a RiskController.
//
// MaxDrawdown is accepted but NOT enforced in v1: the backtest engine
// owns the equity curve and is the right place for drawdown-based halt
// logic. v2 will wire it when the engine exposes a CurrentDrawdown()
// hook. The field is kept here so configs can carry the intent forward.
type RiskConfig struct {
	MaxPositionPct   float64 // max weight per single stock (default 0.10)
	MaxDrawdown      float64 // accepted but NOT enforced in v1 (see doc)
	MaxOpenPositions int     // max concurrent positions (default 20)
	MinCashBuffer    float64 // keep at least this fraction in cash (default 0.05)
}

// RiskController applies pre-trade risk checks to proposed weights.
//
// Config-driven (no DSL). The controller clamps per-position weights,
// enforces a maximum number of open positions (keeping the top-N by
// weight), and ensures a minimum cash buffer is preserved.
type RiskController struct {
	cfg RiskConfig
}

// NewRiskController validates the config and applies defaults.
//
// Defaults: MaxPositionPct=0.10, MaxOpenPositions=20, MinCashBuffer=0.05.
// Returns an error if any cap is negative.
func NewRiskController(cfg RiskConfig) (*RiskController, error) {
	if cfg.MaxPositionPct <= 0 {
		cfg.MaxPositionPct = 0.10
	}
	if cfg.MaxOpenPositions <= 0 {
		cfg.MaxOpenPositions = 20
	}
	if cfg.MinCashBuffer < 0 {
		cfg.MinCashBuffer = 0.05
	}
	if cfg.MaxPositionPct < 0 || cfg.MaxOpenPositions < 0 || cfg.MinCashBuffer < 0 {
		return nil, fmt.Errorf("risk: caps cannot be negative")
	}
	return &RiskController{cfg: cfg}, nil
}

// Check applies risk limits to a weights map and returns the filtered
// result.
//
//   - Each weight is clamped to [0, MaxPositionPct].
//   - If the number of non-zero weights exceeds MaxOpenPositions, only
//     the top-N by weight are kept; the rest are set to 0 (and removed
//     from the returned map).
//   - If the sum of weights exceeds (1 - MinCashBuffer), all weights
//     are scaled down proportionally to fit.
//
// The portfolio parameter is accepted for future use (e.g. counting
// existing positions toward MaxOpenPositions); it may be nil.
func (r *RiskController) Check(weights map[string]float64, portfolio *domain.Portfolio) (map[string]float64, error) {
	_ = portfolio // reserved for future position-aware checks

	if len(weights) == 0 {
		return map[string]float64{}, nil
	}

	// Step 1: clamp each weight to [0, MaxPositionPct].
	clamped := make(map[string]float64, len(weights))
	for symbol, w := range weights {
		if w < 0 {
			w = 0
		}
		if w > r.cfg.MaxPositionPct {
			w = r.cfg.MaxPositionPct
		}
		if w > 0 {
			clamped[symbol] = w
		}
	}

	// Step 2: enforce MaxOpenPositions (keep top-N by weight).
	if len(clamped) > r.cfg.MaxOpenPositions {
		type entry struct {
			symbol string
			weight float64
		}
		entries := make([]entry, 0, len(clamped))
		for s, w := range clamped {
			entries = append(entries, entry{s, w})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].weight > entries[j].weight
		})
		// Keep only the top MaxOpenPositions.
		kept := make(map[string]float64, r.cfg.MaxOpenPositions)
		for i := 0; i < r.cfg.MaxOpenPositions && i < len(entries); i++ {
			kept[entries[i].symbol] = entries[i].weight
		}
		clamped = kept
	}

	// Step 3: enforce MinCashBuffer (sum ≤ 1 - MinCashBuffer).
	maxTotal := 1.0 - r.cfg.MinCashBuffer
	sum := 0.0
	for _, w := range clamped {
		sum += w
	}
	if sum > maxTotal && sum > 0 {
		scale := maxTotal / sum
		for s := range clamped {
			clamped[s] *= scale
		}
	}

	return clamped, nil
}
