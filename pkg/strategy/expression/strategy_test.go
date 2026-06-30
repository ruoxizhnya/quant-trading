package expression

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// barsWithClose builds a bars map where each symbol has a single OHLCV
// bar with the given close price. Used for ExpressionStrategy tests
// where we control which symbols pass the DSL filter.
func barsWithClose(symbols map[string]float64) map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV, len(symbols))
	now := time.Now()
	for s, c := range symbols {
		bars[s] = []domain.OHLCV{{Symbol: s, Close: c, Date: now}}
	}
	return bars
}

// testConfig returns a minimal valid config for ExpressionStrategy
// tests: filter "close > 50", equal sizing at 10% per stock, standard
// risk limits. Tests that need different values override fields.
func testConfig() ExpressionStrategyConfig {
	return ExpressionStrategyConfig{
		SignalCfg: SignalConfig{
			Expression: "close > 50",
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

// ─── NewExpressionStrategy ─────────────────────────────────────────────

func TestNewExpressionStrategy_HappyPath(t *testing.T) {
	s, err := NewExpressionStrategy("my_expr", testConfig())
	if err != nil {
		t.Fatalf("NewExpressionStrategy error: %v", err)
	}
	if s.Name() != "my_expr" {
		t.Errorf("Name() = %q, want %q", s.Name(), "my_expr")
	}
	if !strings.Contains(s.Description(), "close > 50") {
		t.Errorf("Description() should contain the expression, got: %q", s.Description())
	}
}

func TestNewExpressionStrategy_DefaultsApplied(t *testing.T) {
	// Empty SignalCfg.Expression → default expression applied.
	s, err := NewExpressionStrategy("defaulted", ExpressionStrategyConfig{})
	if err != nil {
		t.Fatalf("NewExpressionStrategy with empty config: %v", err)
	}
	def := defaultExpressionStrategyConfig()
	if s.cfg.SignalCfg.Expression != def.SignalCfg.Expression {
		t.Errorf("default Expression = %q, want %q", s.cfg.SignalCfg.Expression, def.SignalCfg.Expression)
	}
	if s.cfg.SignalCfg.Action != "buy" {
		t.Errorf("default Action = %q, want %q", s.cfg.SignalCfg.Action, "buy")
	}
}

func TestNewExpressionStrategy_InvalidExpression(t *testing.T) {
	cfg := testConfig()
	cfg.SignalCfg.Expression = "close >" // syntax error: missing right operand
	_, err := NewExpressionStrategy("bad", cfg)
	if err == nil {
		t.Fatal("expected error for invalid expression, got nil")
	}
	if !strings.Contains(err.Error(), "expression strategy") {
		t.Errorf("error should be wrapped by expression strategy, got: %v", err)
	}
}

// ─── Configure ─────────────────────────────────────────────────────────

func TestExpressionStrategy_Configure_ChangesFilter(t *testing.T) {
	s, _ := NewExpressionStrategy("test", testConfig())
	bars := barsWithClose(map[string]float64{"A": 40, "B": 60, "C": 70})

	// Initial filter "close > 50": B(60) and C(70) pass → 2 signals.
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	if err != nil {
		t.Fatalf("GenerateSignals (initial): %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("initial: expected 2 signals (B, C), got %d: %+v", len(signals), signals)
	}

	// Reconfigure to "close > 65": only C(70) passes → 1 signal.
	if err := s.Configure(map[string]interface{}{
		"signal_expr": "close > 65",
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	signals, err = s.GenerateSignals(context.Background(), bars, nil)
	if err != nil {
		t.Fatalf("GenerateSignals (after configure): %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("after configure: expected 1 signal (C), got %d: %+v", len(signals), signals)
	}
	if signals[0].Symbol != "C" {
		t.Errorf("after configure: expected signal for C, got %q", signals[0].Symbol)
	}
}

// ─── GenerateSignals ───────────────────────────────────────────────────

func TestExpressionStrategy_GenerateSignals_HappyPath(t *testing.T) {
	s, _ := NewExpressionStrategy("test", testConfig())
	bars := barsWithClose(map[string]float64{"A": 40, "B": 60, "C": 70})

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	if err != nil {
		t.Fatalf("GenerateSignals: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (B, C), got %d: %+v", len(signals), signals)
	}

	// Signals should be sorted by symbol: B, C.
	if signals[0].Symbol != "B" || signals[1].Symbol != "C" {
		t.Errorf("expected [B, C], got [%s, %s]", signals[0].Symbol, signals[1].Symbol)
	}

	// Each signal should have weight 0.10 (equal sizing, MaxPerStock cap).
	for _, sig := range signals {
		if !approxEqual(sig.Strength, 0.10) {
			t.Errorf("%s: Strength = %.4f, want 0.10 (equal weight capped)", sig.Symbol, sig.Strength)
		}
		if sig.Action != "buy" {
			t.Errorf("%s: Action = %q, want %q", sig.Symbol, sig.Action, "buy")
		}
		if sig.Direction != domain.DirectionLong {
			t.Errorf("%s: Direction = %q, want %q", sig.Symbol, sig.Direction, domain.DirectionLong)
		}
		// raw_strength should preserve the original DSL value (1.0 for comparison pass).
		if raw, ok := sig.Metadata["raw_strength"]; !ok {
			t.Errorf("%s: Metadata missing raw_strength", sig.Symbol)
		} else if !approxEqual(raw.(float64), 1.0) {
			t.Errorf("%s: raw_strength = %v, want 1.0", sig.Symbol, raw)
		}
	}
}

func TestExpressionStrategy_GenerateSignals_EmptyBars(t *testing.T) {
	s, _ := NewExpressionStrategy("test", testConfig())
	signals, err := s.GenerateSignals(context.Background(), map[string][]domain.OHLCV{}, nil)
	if err != nil {
		t.Fatalf("GenerateSignals with empty bars: %v", err)
	}
	if signals != nil && len(signals) != 0 {
		t.Errorf("expected nil or empty signals for empty bars, got %d", len(signals))
	}
}

// ─── Weight ────────────────────────────────────────────────────────────

func TestExpressionStrategy_Weight_ReturnsStrength(t *testing.T) {
	s, _ := NewExpressionStrategy("test", testConfig())
	sig := strategy.Signal{Symbol: "A", Strength: 0.15}
	got := s.Weight(sig, 100000.0)
	if !approxEqual(got, 0.15) {
		t.Errorf("Weight() = %.4f, want 0.15 (signal.Strength)", got)
	}
}
