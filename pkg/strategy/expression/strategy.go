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
//   - OHLCVDataProvider: bridges bars map → aiexpr.DataProvider
//   - ExpressionStrategy: composes all four into a strategy.Strategy
//
// The package depends on pkg/ai/expression for the DSL parser/evaluator
// and on pkg/strategy for the Strategy interface and BaseStrategy.
package expression

import (
	"context"
	"fmt"
	"sort"

	aiexpr "github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ExpressionStrategyConfig holds all configuration for an
// ExpressionStrategy. It is a plain struct so callers can construct it
// literally; NewExpressionStrategy applies defaults for zero-value fields.
type ExpressionStrategyConfig struct {
	SignalCfg SignalConfig
	SizingCfg SizingConfig
	RiskCfg   RiskConfig
}

// defaultExpressionStrategyConfig returns the default config used by the
// self-registered "expression_template" strategy. The default buys the
// top-20% of stocks by close-price rank (cs_rank(close) > 0.8), equally
// weighted at 10% per stock, capped at 20 positions with a 5% cash buffer.
func defaultExpressionStrategyConfig() ExpressionStrategyConfig {
	return ExpressionStrategyConfig{
		SignalCfg: SignalConfig{
			Expression: "cs_rank(close) > 0.8",
			Action:     "buy",
			Direction:  domain.DirectionLong,
		},
		SizingCfg: SizingConfig{
			Method:      SizingEqual,
			MaxPerStock: 0.10,
			MaxTotal:    1.0,
		},
		RiskCfg: RiskConfig{
			MaxPositionPct:   0.10,
			MaxOpenPositions: 20,
			MinCashBuffer:    0.05,
		},
	}
}

// ExpressionStrategy composes SignalGenerator + PositionSizer +
// RiskController + OHLCVDataProvider into a strategy.Strategy.
//
// This is the top-level adapter that lets a DSL expression run as a
// strategy inside the backtest engine. The flow in GenerateSignals is:
//
//  1. Build OHLCVDataProvider from the bars map (strategy engine input).
//  2. Build an aiexpr.Evaluator backed by that provider.
//  3. SignalGenerator evaluates the DSL expression → raw signals.
//  4. PositionSizer computes target weights from signals.
//  5. RiskController filters/clamps weights (per-position cap, max open
//     positions, cash buffer).
//  6. Surviving signals have their Strength set to the final weight;
//     the original DSL value is preserved in Metadata["raw_strength"].
//
// Weight() returns the precomputed weight (signal.Strength) — risk
// checks are set-level and cannot be decomposed into per-signal calls.
type ExpressionStrategy struct {
	*strategy.BaseStrategy

	signalGen *SignalGenerator
	sizer     *PositionSizer
	riskCtl   *RiskController
	cfg       ExpressionStrategyConfig

	// fundamentals 由引擎在回测开始前注入（strategy.FundamentalAware）。
	// nil = 这一路没拿到财报，表达式用了 pe/pb 会明确失败。
	fundamentals map[string]strategy.FundamentalSeries
}

// SetFundamentals 让引擎把基本面数据塞进来（strategy.FundamentalAware，P2-12）。
//
// 传进来的 map 由引擎持有，本策略只读，不复制（回测期间每根 K 线都要对齐一次，
// 复制几万条记录不值得）。
func (s *ExpressionStrategy) SetFundamentals(records map[string]strategy.FundamentalSeries) {
	s.Lock()
	defer s.Unlock()
	s.fundamentals = records
}

// Compile-time check: 引擎靠这个断言决定要不要去预热财报数据。
var _ strategy.FundamentalAware = (*ExpressionStrategy)(nil)

// NewExpressionStrategy constructs a strategy with the given name and
// config. Zero-value fields in cfg are filled with defaults from
// defaultExpressionStrategyConfig. Returns an error if the signal
// expression fails to parse or the sizing/risk configs are invalid.
func NewExpressionStrategy(name string, cfg ExpressionStrategyConfig) (*ExpressionStrategy, error) {
	// Apply defaults for empty signal config.
	if cfg.SignalCfg.Expression == "" {
		def := defaultExpressionStrategyConfig()
		cfg.SignalCfg = def.SignalCfg
	}

	signalGen, err := NewSignalGenerator(cfg.SignalCfg)
	if err != nil {
		return nil, fmt.Errorf("expression strategy: %w", err)
	}
	sizer, err := NewPositionSizer(cfg.SizingCfg)
	if err != nil {
		return nil, fmt.Errorf("expression strategy: %w", err)
	}
	riskCtl, err := NewRiskController(cfg.RiskCfg)
	if err != nil {
		return nil, fmt.Errorf("expression strategy: %w", err)
	}

	return &ExpressionStrategy{
		BaseStrategy: strategy.NewBaseStrategy(name, "Expression-based strategy: "+cfg.SignalCfg.Expression),
		signalGen:    signalGen,
		sizer:        sizer,
		riskCtl:      riskCtl,
		cfg:          cfg,
	}, nil
}

// Parameters returns the configuration schema for UI / YAML generation.
func (s *ExpressionStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{Name: "signal_expr", Type: "string", Default: s.cfg.SignalCfg.Expression, Description: "DSL expression for signal generation"},
		{Name: "action", Type: "string", Default: s.cfg.SignalCfg.Action, Description: "Signal action: buy or sell"},
		{Name: "direction", Type: "string", Default: string(s.cfg.SignalCfg.Direction), Description: "Trade direction: long, short, close"},
		{Name: "min_strength", Type: "float", Default: s.cfg.SignalCfg.MinStrength, Description: "Minimum signal strength to emit"},
		{Name: "sizing_method", Type: "string", Default: string(s.cfg.SizingCfg.Method), Description: "Position sizing: equal, strength_prop, fixed"},
		{Name: "fixed_weight", Type: "float", Default: s.cfg.SizingCfg.FixedWeight, Description: "Per-signal weight for fixed sizing", Min: 0, Max: 1},
		{Name: "max_per_stock", Type: "float", Default: s.cfg.SizingCfg.MaxPerStock, Description: "Max weight per single position", Min: 0, Max: 1},
		{Name: "max_total", Type: "float", Default: s.cfg.SizingCfg.MaxTotal, Description: "Max total exposure", Min: 0, Max: 1},
		{Name: "max_position_pct", Type: "float", Default: s.cfg.RiskCfg.MaxPositionPct, Description: "Risk: max weight per position", Min: 0, Max: 1},
		{Name: "max_open_positions", Type: "int", Default: s.cfg.RiskCfg.MaxOpenPositions, Description: "Risk: max concurrent positions", Min: 1, Max: 100},
		{Name: "min_cash_buffer", Type: "float", Default: s.cfg.RiskCfg.MinCashBuffer, Description: "Risk: min cash buffer fraction", Min: 0, Max: 1},
		{Name: "lookback", Type: "int", Default: s.cfg.SignalCfg.Lookback, Description: "Evaluator lookback window (trading days)", Min: 1, Max: 500},
	}
}

// Configure applies a partial parameter update and rebuilds all
// components (SignalGenerator, PositionSizer, RiskController).
//
// Missing keys preserve the current config (partial update semantics).
// Parameters are read directly from the params map using inline type
// assertions (same pattern as momentumStrategy.Configure) to avoid
// re-entrant locking on BaseStrategy's mutex.
func (s *ExpressionStrategy) Configure(params map[string]interface{}) error {
	s.Lock()
	defer s.Unlock()

	// Read values with current config as defaults (partial update).
	cfg := s.cfg
	cfg.SignalCfg.Expression = getStringParam(params, "signal_expr", cfg.SignalCfg.Expression)
	cfg.SignalCfg.Action = getStringParam(params, "action", cfg.SignalCfg.Action)
	cfg.SignalCfg.Direction = domain.Direction(getStringParam(params, "direction", string(cfg.SignalCfg.Direction)))
	cfg.SignalCfg.MinStrength = getFloatParam(params, "min_strength", cfg.SignalCfg.MinStrength)
	cfg.SignalCfg.Lookback = getIntParam(params, "lookback", cfg.SignalCfg.Lookback)
	cfg.SizingCfg.Method = SizingMethod(getStringParam(params, "sizing_method", string(cfg.SizingCfg.Method)))
	cfg.SizingCfg.FixedWeight = getFloatParam(params, "fixed_weight", cfg.SizingCfg.FixedWeight)
	cfg.SizingCfg.MaxPerStock = getFloatParam(params, "max_per_stock", cfg.SizingCfg.MaxPerStock)
	cfg.SizingCfg.MaxTotal = getFloatParam(params, "max_total", cfg.SizingCfg.MaxTotal)
	cfg.RiskCfg.MaxPositionPct = getFloatParam(params, "max_position_pct", cfg.RiskCfg.MaxPositionPct)
	cfg.RiskCfg.MaxOpenPositions = getIntParam(params, "max_open_positions", cfg.RiskCfg.MaxOpenPositions)
	cfg.RiskCfg.MinCashBuffer = getFloatParam(params, "min_cash_buffer", cfg.RiskCfg.MinCashBuffer)

	// Rebuild components with new config.
	signalGen, err := NewSignalGenerator(cfg.SignalCfg)
	if err != nil {
		return fmt.Errorf("expression strategy: %w", err)
	}
	sizer, err := NewPositionSizer(cfg.SizingCfg)
	if err != nil {
		return fmt.Errorf("expression strategy: %w", err)
	}
	riskCtl, err := NewRiskController(cfg.RiskCfg)
	if err != nil {
		return fmt.Errorf("expression strategy: %w", err)
	}

	s.signalGen = signalGen
	s.sizer = sizer
	s.riskCtl = riskCtl
	s.cfg = cfg
	return nil
}

// ─── Inline parameter parse helpers ────────────────────────────────────
//
// These mirror the unexported helpers in pkg/strategy/plugins/utils.go.
// They are inlined here (rather than imported) because:
//   1. The plugins package's helpers are unexported.
//   2. Importing plugins would create an import cycle
//      (plugins → strategy → expression → plugins).
//   3. BaseStrategy.GetParam* cannot be used inside Configure because
//      Configure already holds the write lock and GetParam* acquires
//      the read lock → deadlock.

// getStringParam reads a string from params[key], returning fallback if
// the key is missing or the value is not a string.
func getStringParam(params map[string]interface{}, key, fallback string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

// getFloatParam reads a float64 from params[key], supporting float64
// (JSON numbers) and int. Returns fallback if missing or wrong type.
func getFloatParam(params map[string]interface{}, key string, fallback float64) float64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		}
	}
	return fallback
}

// getIntParam reads an int from params[key], supporting float64 (JSON
// numbers) and int. Returns fallback if missing or wrong type.
func getIntParam(params map[string]interface{}, key string, fallback int) int {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		}
	}
	return fallback
}

// GenerateSignals runs the full pipeline: bars → provider → evaluator →
// signals → sizing → risk → filtered signals with final weights.
//
// The final signals have Strength set to the risk-checked weight; the
// original DSL value is preserved in Metadata["raw_strength"] for
// debugging. Signals are sorted by symbol for deterministic output.
func (s *ExpressionStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	_ = ctx

	if len(bars) == 0 {
		return nil, nil
	}

	// Read current components under read-lock for thread safety.
	s.RLock()
	signalGen := s.signalGen
	sizer := s.sizer
	riskCtl := s.riskCtl
	fundamentals := s.fundamentals
	s.RUnlock()

	if signalGen == nil {
		return nil, fmt.Errorf("expression strategy: signal generator not initialized")
	}

	// 1. Build evaluator from bars (+ 注入的财报，P2-12)。
	provider := NewOHLCVDataProviderWithFundamentals(bars, fundamentals)
	evaluator := aiexpr.NewEvaluator(provider)

	// 2. Generate raw signals.
	signals, err := signalGen.Generate(bars, evaluator)
	if err != nil {
		return nil, fmt.Errorf("expression strategy: generate: %w", err)
	}
	if len(signals) == 0 {
		return nil, nil
	}

	// 3. Size positions.
	portfolioValue := 0.0
	if portfolio != nil {
		portfolioValue = portfolio.TotalValue
	}
	weights, err := sizer.Size(signals, portfolioValue)
	if err != nil {
		return nil, fmt.Errorf("expression strategy: size: %w", err)
	}

	// 4. Apply risk checks.
	filtered, err := riskCtl.Check(weights, portfolio)
	if err != nil {
		return nil, fmt.Errorf("expression strategy: risk: %w", err)
	}

	// 5. Build final signals: keep only symbols with weight > 0,
	//    set Strength = weight, preserve Action/Direction/Price,
	//    stash original DSL value in Metadata.
	result := make([]strategy.Signal, 0, len(filtered))
	for _, sig := range signals {
		w, ok := filtered[sig.Symbol]
		if !ok || w <= 0 {
			continue
		}
		raw := sig.Strength
		sig.Strength = w
		if sig.Metadata == nil {
			sig.Metadata = make(map[string]interface{})
		}
		sig.Metadata["raw_strength"] = raw
		result = append(result, sig)
	}

	// 6. Sort by symbol for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Symbol < result[j].Symbol
	})

	return result, nil
}

// Weight returns the precomputed weight stored in signal.Strength.
//
// The weight is computed in GenerateSignals by the PositionSizer and
// RiskController; Weight simply returns it so the engine gets the
// final target fraction. This is necessary because risk checks are
// set-level (MaxOpenPositions, MinCashBuffer) and cannot be decomposed
// into per-signal Weight() calls.
func (s *ExpressionStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	_ = portfolioValue
	return signal.Strength
}

// Compile-time interface satisfaction check.
var _ strategy.Strategy = (*ExpressionStrategy)(nil)

// init self-registers a default "expression_template" strategy so it
// appears in the registry and passes the compliance test
// (TestRegisteredStrategies_SatisfyComposite). Users can configure it
// at runtime via strategy.ConfigureStrategy("expression_template", params)
// or construct a custom instance with NewExpressionStrategy.
func init() {
	s, err := NewExpressionStrategy("expression_template", defaultExpressionStrategyConfig())
	if err != nil {
		// This should never happen with a valid default config; if it
		// does, the package is broken and should fail loudly.
		panic(fmt.Sprintf("expression: failed to register expression_template: %v", err))
	}
	strategy.GlobalRegister(s)
}
