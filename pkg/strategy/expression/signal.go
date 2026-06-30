// Package expression provides the expression-based strategy stack:
// signal generation, position sizing, risk control, and the
// ExpressionStrategy adapter that composes them into a strategy.Strategy.
//
// S7-P3-1 (ODR-043 Sprint 7): The AI expression engine (pkg/ai/expression)
// was factor-only — it could evaluate a DSL formula to a numeric value but
// had no way to turn that value into trading signals, size positions, or
// enforce risk limits. This package fills those gaps:
//
//   - SignalGenerator: DSL comparison expression → []strategy.Signal
//   - PositionSizer:   signals → target weights (equal/strength/fixed)
//   - RiskController:  weights → risk-checked weights
//   - ExpressionStrategy: composes all three into a strategy.Strategy
//
// The package depends on pkg/ai/expression for the DSL parser/evaluator
// and on pkg/strategy for the Strategy interface and BaseStrategy.
package expression

import (
	"fmt"
	"math"
	"sort"

	aiexpr "github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// defaultLookback is the default evaluator lookback window (trading days).
const defaultLookback = 60

// SignalConfig configures a SignalGenerator.
//
// Expression is a DSL formula that, when evaluated, produces a per-symbol
// numeric value. A symbol emits a signal when its latest value is truthy
// (≠ 0, not NaN) AND ≥ MinStrength. This supports two patterns:
//
//   - Comparison filter: "cs_rank(close) > 0.8" → result is 1.0/0.0,
//     MinStrength=0 → passes where result=1.0, Strength=1.0.
//   - Raw factor filter: "cs_rank(close)" → result is 0..1, MinStrength=0.8
//     → passes where rank ≥ 0.8, Strength=rank (enables proportional sizing).
type SignalConfig struct {
	Expression  string           // DSL formula, e.g. "cs_rank(ts_delta(close, 5)) > 0.8"
	Action      string           // "buy" or "sell"
	Direction   domain.Direction // DirectionLong, DirectionShort, DirectionClose
	MinStrength float64          // minimum value to emit a signal (default 0)
	Lookback    int              // evaluator lookback window in trading days (default 60 if <= 0)
}

// SignalGenerator turns a DSL expression into trading signals.
//
// The expression is parsed once at construction (NewSignalGenerator) and
// the AST is cached. Generate evaluates the AST against live data via
// the provided evaluator and emits signals for passing symbols.
type SignalGenerator struct {
	cfg      SignalConfig
	ast      *aiexpr.Expression
	lookback int
}

// NewSignalGenerator parses the expression and validates the config.
//
// Defaults: Action="buy", Direction=DirectionLong, MinStrength=0,
// lookback=60. Returns an error if the expression fails to parse or
// the action is not "buy"/"sell".
func NewSignalGenerator(cfg SignalConfig) (*SignalGenerator, error) {
	if cfg.Expression == "" {
		return nil, fmt.Errorf("signal: expression cannot be empty")
	}
	if cfg.Action == "" {
		cfg.Action = "buy"
	}
	if cfg.Action != "buy" && cfg.Action != "sell" {
		return nil, fmt.Errorf("signal: action must be \"buy\" or \"sell\", got %q", cfg.Action)
	}
	if cfg.Direction == "" {
		cfg.Direction = domain.DirectionLong
	}

	p := aiexpr.NewParser()
	expr, err := p.Parse(cfg.Expression)
	if err != nil {
		return nil, fmt.Errorf("signal: parse expression %q: %w", cfg.Expression, err)
	}

	lookback := cfg.Lookback
	if lookback <= 0 {
		lookback = defaultLookback
	}

	return &SignalGenerator{
		cfg:      cfg,
		ast:      expr,
		lookback: lookback,
	}, nil
}

// Generate evaluates the expression and emits signals for symbols whose
// latest value is truthy (≠ 0, not NaN) and ≥ MinStrength.
//
// The evaluator provides the per-symbol values; bars provides the
// latest close price for Signal.Price. Symbols present in the evaluator
// result but missing from bars get Price=0.
//
// Signals are returned in deterministic order (sorted by symbol) to
// keep downstream sizing/risk reproducible.
func (g *SignalGenerator) Generate(bars map[string][]domain.OHLCV, evaluator *aiexpr.Evaluator) ([]strategy.Signal, error) {
	if evaluator == nil {
		return nil, fmt.Errorf("signal: evaluator cannot be nil")
	}

	values, err := evaluator.Evaluate(g.ast.AST, g.lookback)
	if err != nil {
		return nil, fmt.Errorf("signal: evaluate: %w", err)
	}

	// Collect passing symbols for deterministic ordering.
	type candidate struct {
		symbol   string
		strength float64
		price    float64
	}
	var candidates []candidate

	for symbol, vals := range values {
		if len(vals) == 0 {
			continue
		}
		v := vals[len(vals)-1]
		// Truthy: non-zero, non-NaN. This handles comparison results
		// (1.0 = pass) and raw factor values (any non-zero).
		if math.IsNaN(v) || v == 0 {
			continue
		}
		if v < g.cfg.MinStrength {
			continue
		}
		price := latestClose(bars[symbol])
		candidates = append(candidates, candidate{
			symbol:   symbol,
			strength: v,
			price:    price,
		})
	}

	// Sort by symbol for deterministic output.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].symbol < candidates[j].symbol
	})

	signals := make([]strategy.Signal, 0, len(candidates))
	for _, c := range candidates {
		signals = append(signals, strategy.Signal{
			Symbol:    c.symbol,
			Action:    g.cfg.Action,
			Strength:  c.strength,
			Price:     c.price,
			Direction: g.cfg.Direction,
		})
	}
	return signals, nil
}

// latestClose returns the closing price of the most recent bar, or 0
// if the slice is empty. Assumes bars are sorted by date ascending
// (the strategy engine contract).
func latestClose(bars []domain.OHLCV) float64 {
	if len(bars) == 0 {
		return 0
	}
	return bars[len(bars)-1].Close
}
