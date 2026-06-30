package expression

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestCheck_ClampSinglePosition(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{
		MaxPositionPct: 0.10,
	})
	weights := map[string]float64{"A": 0.25} // above cap
	got, err := r.Check(weights, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !approxEqual(got["A"], 0.10) {
		t.Errorf("A: got %.4f, want 0.10 (clamped)", got["A"])
	}
}

func TestCheck_TruncateToMaxOpenPositions(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{
		MaxPositionPct:   1.0, // no per-stock cap
		MaxOpenPositions: 3,
		MinCashBuffer:    0,
	})
	// 5 positions with different weights; top-3 should be kept.
	weights := map[string]float64{
		"A": 0.30, "B": 0.10, "C": 0.25, "D": 0.05, "E": 0.20,
	}
	got, err := r.Check(weights, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 positions after truncation, got %d: %+v", len(got), got)
	}
	// Top-3 by weight: A (0.30), C (0.25), E (0.20)
	for _, s := range []string{"A", "C", "E"} {
		if _, ok := got[s]; !ok {
			t.Errorf("expected %s in result (top-3), missing", s)
		}
	}
	for _, s := range []string{"B", "D"} {
		if _, ok := got[s]; ok {
			t.Errorf("expected %s to be truncated (bottom-2), present", s)
		}
	}
}

func TestCheck_ScaleDownForCashBuffer(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{
		MaxPositionPct: 1.0,  // no per-stock cap
		MinCashBuffer:  0.10, // max total = 0.90
	})
	// 5 positions × 0.20 = 1.0 > 0.90 → scale to 0.18 each
	weights := map[string]float64{
		"A": 0.20, "B": 0.20, "C": 0.20, "D": 0.20, "E": 0.20,
	}
	got, err := r.Check(weights, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	sum := 0.0
	for _, w := range got {
		sum += w
	}
	maxTotal := 1.0 - 0.10
	if sum > maxTotal+1e-9 {
		t.Errorf("sum = %.6f, want ≤ %.2f (cash buffer)", sum, maxTotal)
	}
	if !approxEqual(sum, maxTotal) {
		t.Errorf("sum = %.6f, want exactly %.2f (scaled to cap)", sum, maxTotal)
	}
}

func TestCheck_NilPortfolio(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{
		MaxPositionPct: 0.10,
	})
	weights := map[string]float64{"A": 0.05}
	got, err := r.Check(weights, nil)
	if err != nil {
		t.Fatalf("Check with nil portfolio: %v", err)
	}
	if !approxEqual(got["A"], 0.05) {
		t.Errorf("A: got %.4f, want 0.05", got["A"])
	}
}

func TestCheck_NonNilPortfolio(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{
		MaxPositionPct: 0.10,
	})
	weights := map[string]float64{"A": 0.05}
	portfolio := &domain.Portfolio{Cash: 1000, TotalValue: 10000}
	got, err := r.Check(weights, portfolio)
	if err != nil {
		t.Fatalf("Check with portfolio: %v", err)
	}
	if !approxEqual(got["A"], 0.05) {
		t.Errorf("A: got %.4f, want 0.05", got["A"])
	}
}

func TestCheck_EmptyWeights(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{})
	got, err := r.Check(map[string]float64{}, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestCheck_NegativeWeightsClamped(t *testing.T) {
	r, _ := NewRiskController(RiskConfig{
		MaxPositionPct: 0.10,
	})
	weights := map[string]float64{"A": -0.05, "B": 0.08}
	got, err := r.Check(weights, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// Negative weight should be clamped to 0 and excluded.
	if _, ok := got["A"]; ok {
		t.Errorf("A: expected to be excluded (negative weight), got %.4f", got["A"])
	}
	if !approxEqual(got["B"], 0.08) {
		t.Errorf("B: got %.4f, want 0.08", got["B"])
	}
}

// TestCheck_MaxDrawdownFieldAccepted verifies MaxDrawdown is accepted
// without error (documented no-op in v1).
func TestCheck_MaxDrawdownFieldAccepted(t *testing.T) {
	r, err := NewRiskController(RiskConfig{
		MaxPositionPct: 0.10,
		MaxDrawdown:    0.20, // accepted but not enforced
	})
	if err != nil {
		t.Fatalf("NewRiskController with MaxDrawdown: %v", err)
	}
	weights := map[string]float64{"A": 0.05}
	_, err = r.Check(weights, nil)
	if err != nil {
		t.Errorf("Check with MaxDrawdown cfg: unexpected error: %v", err)
	}
}
