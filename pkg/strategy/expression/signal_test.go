package expression

import (
	"testing"
	"time"

	aiexpr "github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// mockFieldProvider implements aiexpr.DataProvider for signal tests.
// It returns per-symbol, per-field float64 slices so we can control
// the cross-sectional snapshot the evaluator sees.
type mockFieldProvider struct {
	symbols []string
	data    map[string]map[string][]float64
}

func (m *mockFieldProvider) GetField(symbol, field string, lookback int) ([]float64, error) {
	fields, ok := m.data[symbol]
	if !ok {
		return []float64{}, nil
	}
	vals, ok := fields[field]
	if !ok {
		return []float64{}, nil
	}
	return vals, nil
}

func (m *mockFieldProvider) GetSymbols() []string { return m.symbols }

// makeBars builds a bars map where each symbol has one OHLCV bar with
// the given close price (used for Signal.Price verification).
func makeBars(symbols []string, prices map[string]float64) map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV, len(symbols))
	for _, s := range symbols {
		bars[s] = []domain.OHLCV{{Close: prices[s], Date: time.Now()}}
	}
	return bars
}

// ─── NewSignalGenerator tests ─────────────────────────────────────────

func TestNewSignalGenerator_HappyPath(t *testing.T) {
	g, err := NewSignalGenerator(SignalConfig{
		Expression: "cs_rank(close) > 0.5",
	})
	if err != nil {
		t.Fatalf("NewSignalGenerator error: %v", err)
	}
	if g.cfg.Action != "buy" {
		t.Errorf("default Action = %q, want %q", g.cfg.Action, "buy")
	}
	if g.cfg.Direction != domain.DirectionLong {
		t.Errorf("default Direction = %q, want %q", g.cfg.Direction, domain.DirectionLong)
	}
	if g.lookback != defaultLookback {
		t.Errorf("lookback = %d, want %d", g.lookback, defaultLookback)
	}
}

func TestNewSignalGenerator_InvalidExpression(t *testing.T) {
	_, err := NewSignalGenerator(SignalConfig{
		Expression: "cs_rank(close", // unmatched paren
	})
	if err == nil {
		t.Fatal("expected error for invalid expression, got nil")
	}
}

func TestNewSignalGenerator_EmptyExpression(t *testing.T) {
	_, err := NewSignalGenerator(SignalConfig{Expression: ""})
	if err == nil {
		t.Fatal("expected error for empty expression, got nil")
	}
}

func TestNewSignalGenerator_InvalidAction(t *testing.T) {
	_, err := NewSignalGenerator(SignalConfig{
		Expression: "close > 10",
		Action:     "hold",
	})
	if err == nil {
		t.Fatal("expected error for action=hold, got nil")
	}
}

// ─── Generate tests ───────────────────────────────────────────────────

// TestGenerate_ComparisonFilter_HappyPath verifies that a comparison
// expression (close > 10) emits signals only for symbols where the
// comparison is true, with Strength=1.0.
func TestGenerate_ComparisonFilter_HappyPath(t *testing.T) {
	provider := &mockFieldProvider{
		symbols: []string{"A", "B", "C"},
		data: map[string]map[string][]float64{
			"A": {"close": {5}},  // 5 > 10 → false
			"B": {"close": {15}}, // 15 > 10 → true
			"C": {"close": {20}}, // 20 > 10 → true
		},
	}
	bars := makeBars([]string{"A", "B", "C"}, map[string]float64{"A": 5, "B": 15, "C": 20})

	g, err := NewSignalGenerator(SignalConfig{
		Expression: "close > 10",
	})
	if err != nil {
		t.Fatalf("NewSignalGenerator: %v", err)
	}
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(bars, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(signals))
	}
	// Sorted by symbol: B, C
	if signals[0].Symbol != "B" || signals[1].Symbol != "C" {
		t.Errorf("signal order: got %s, %s; want B, C", signals[0].Symbol, signals[1].Symbol)
	}
	for _, s := range signals {
		if s.Strength != 1.0 {
			t.Errorf("symbol %s: Strength = %.2f, want 1.0 (comparison result)", s.Symbol, s.Strength)
		}
		if s.Action != "buy" {
			t.Errorf("symbol %s: Action = %q, want %q", s.Symbol, s.Action, "buy")
		}
	}
}

// TestGenerate_NoSymbolsPass verifies empty signals when no symbol
// satisfies the filter.
func TestGenerate_NoSymbolsPass(t *testing.T) {
	provider := &mockFieldProvider{
		symbols: []string{"A", "B"},
		data: map[string]map[string][]float64{
			"A": {"close": {5}},
			"B": {"close": {8}},
		},
	}
	bars := makeBars([]string{"A", "B"}, map[string]float64{"A": 5, "B": 8})
	g, _ := NewSignalGenerator(SignalConfig{Expression: "close > 10"})
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(bars, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

// TestGenerate_EmptyBars verifies empty bars produce empty signals
// without error.
func TestGenerate_EmptyBars(t *testing.T) {
	provider := &mockFieldProvider{
		symbols: []string{},
		data:    map[string]map[string][]float64{},
	}
	g, _ := NewSignalGenerator(SignalConfig{Expression: "close > 10"})
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(map[string][]domain.OHLCV{}, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

// TestGenerate_SellAction_ShortDirection verifies the action and
// direction are propagated to signals.
func TestGenerate_SellAction_ShortDirection(t *testing.T) {
	provider := &mockFieldProvider{
		symbols: []string{"A"},
		data: map[string]map[string][]float64{
			"A": {"close": {20}},
		},
	}
	bars := makeBars([]string{"A"}, map[string]float64{"A": 20})
	g, _ := NewSignalGenerator(SignalConfig{
		Expression: "close > 10",
		Action:     "sell",
		Direction:  domain.DirectionShort,
	})
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(bars, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Action != "sell" {
		t.Errorf("Action = %q, want %q", signals[0].Action, "sell")
	}
	if signals[0].Direction != domain.DirectionShort {
		t.Errorf("Direction = %q, want %q", signals[0].Direction, domain.DirectionShort)
	}
}

// TestGenerate_MinStrengthFiltering verifies MinStrength filters out
// signals below the threshold (raw factor mode, not comparison).
func TestGenerate_MinStrengthFiltering(t *testing.T) {
	// cs_rank returns 0..1; with MinStrength=0.5, only rank ≥ 0.5 pass.
	provider := &mockFieldProvider{
		symbols: []string{"A", "B", "C", "D"},
		data: map[string]map[string][]float64{
			"A": {"close": {10}},
			"B": {"close": {20}},
			"C": {"close": {30}},
			"D": {"close": {40}},
		},
	}
	bars := makeBars([]string{"A", "B", "C", "D"}, map[string]float64{"A": 10, "B": 20, "C": 30, "D": 40})
	g, _ := NewSignalGenerator(SignalConfig{
		Expression:  "cs_rank(close)",
		MinStrength: 0.5,
	})
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(bars, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// cs_rank of [10,20,30,40]: ranks are 0, 1/3, 2/3, 1.
	// MinStrength=0.5 → pass C (0.667) and D (1.0).
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (rank ≥ 0.5), got %d: %+v", len(signals), signals)
	}
	if signals[0].Symbol != "C" || signals[1].Symbol != "D" {
		t.Errorf("expected C and D, got %s and %s", signals[0].Symbol, signals[1].Symbol)
	}
}

// TestGenerate_NilEvaluator verifies nil evaluator returns an error.
func TestGenerate_NilEvaluator(t *testing.T) {
	g, _ := NewSignalGenerator(SignalConfig{Expression: "close > 10"})
	_, err := g.Generate(makeBars([]string{"A"}, map[string]float64{"A": 20}), nil)
	if err == nil {
		t.Fatal("expected error for nil evaluator, got nil")
	}
}

// TestGenerate_NaNValuesFiltered verifies NaN comparison results are
// filtered out (don't emit signals).
func TestGenerate_NaNValuesFiltered(t *testing.T) {
	// ts_delta(close, 1) on a single-bar series produces NaN (insufficient
	// data). The comparison NaN > 10 should yield 0 (filtered).
	provider := &mockFieldProvider{
		symbols: []string{"A"},
		data: map[string]map[string][]float64{
			"A": {"close": {20}}, // single bar → ts_delta = NaN
		},
	}
	bars := makeBars([]string{"A"}, map[string]float64{"A": 20})
	g, _ := NewSignalGenerator(SignalConfig{
		Expression: "ts_delta(close, 1) > 10",
	})
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(bars, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals (NaN filtered), got %d", len(signals))
	}
}

// TestGenerate_PriceFromBars verifies Signal.Price comes from the
// latest bar's close.
func TestGenerate_PriceFromBars(t *testing.T) {
	provider := &mockFieldProvider{
		symbols: []string{"A"},
		data: map[string]map[string][]float64{
			"A": {"close": {20}},
		},
	}
	bars := map[string][]domain.OHLCV{
		"A": {{Close: 42.5, Date: time.Now()}},
	}
	g, _ := NewSignalGenerator(SignalConfig{Expression: "close > 10"})
	ev := aiexpr.NewEvaluator(provider)
	signals, err := g.Generate(bars, ev)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Price != 42.5 {
		t.Errorf("Price = %.2f, want 42.5", signals[0].Price)
	}
}
