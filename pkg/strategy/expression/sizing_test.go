package expression

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

func makeSignals(strengths map[string]float64) []strategy.Signal {
	signals := make([]strategy.Signal, 0, len(strengths))
	for symbol, strength := range strengths {
		signals = append(signals, strategy.Signal{
			Symbol:   symbol,
			Strength: strength,
			Action:   "buy",
		})
	}
	return signals
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestNewPositionSizer_UnknownMethod(t *testing.T) {
	_, err := NewPositionSizer(SizingConfig{Method: "risk_parity"})
	if err == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
}

func TestSize_EqualWeight(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingEqual,
		MaxTotal:    1.0,
		MaxPerStock: 1.0, // no per-stock cap for this test
	})
	signals := makeSignals(map[string]float64{
		"A": 1.0, "B": 0.5, "C": 0.3, "D": 0.9, "E": 0.1,
	})
	weights, err := sizer.Size(signals, 100000)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	// 5 signals, MaxTotal=1.0 → 0.20 each
	for _, s := range []string{"A", "B", "C", "D", "E"} {
		if !approxEqual(weights[s], 0.20) {
			t.Errorf("weight[%s] = %.4f, want 0.20", s, weights[s])
		}
	}
}

func TestSize_EqualWeight_CappedByMaxPerStock(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingEqual,
		MaxTotal:    1.0,
		MaxPerStock: 0.15,
	})
	signals := makeSignals(map[string]float64{"A": 1, "B": 1, "C": 1, "D": 1, "E": 1})
	weights, _ := sizer.Size(signals, 0)
	// 1.0/5 = 0.20, but MaxPerStock=0.15 caps each → 0.15
	// Sum = 0.75 < MaxTotal, no scaling.
	for _, s := range []string{"A", "B", "C", "D", "E"} {
		if !approxEqual(weights[s], 0.15) {
			t.Errorf("weight[%s] = %.4f, want 0.15 (MaxPerStock cap)", s, weights[s])
		}
	}
}

func TestSize_StrengthProportional(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingStrengthProp,
		MaxTotal:    1.0,
		MaxPerStock: 1.0, // no per-stock cap for this test
	})
	signals := makeSignals(map[string]float64{
		"A": 1.0, "B": 3.0,
	}) // total strength = 4.0
	weights, _ := sizer.Size(signals, 0)
	// A: 1.0/4.0 = 0.25, B: 3.0/4.0 = 0.75
	if !approxEqual(weights["A"], 0.25) {
		t.Errorf("A: got %.4f, want 0.25", weights["A"])
	}
	if !approxEqual(weights["B"], 0.75) {
		t.Errorf("B: got %.4f, want 0.75", weights["B"])
	}
}

func TestSize_FixedWeight(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingFixed,
		FixedWeight: 0.07,
		MaxPerStock: 0.10,
	})
	signals := makeSignals(map[string]float64{"A": 1, "B": 2})
	weights, _ := sizer.Size(signals, 0)
	for _, s := range []string{"A", "B"} {
		if !approxEqual(weights[s], 0.07) {
			t.Errorf("weight[%s] = %.4f, want 0.07", s, weights[s])
		}
	}
}

func TestSize_FixedWeight_CappedByMaxPerStock(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingFixed,
		FixedWeight: 0.15,
		MaxPerStock: 0.10,
	})
	signals := makeSignals(map[string]float64{"A": 1})
	weights, _ := sizer.Size(signals, 0)
	if !approxEqual(weights["A"], 0.10) {
		t.Errorf("weight[A] = %.4f, want 0.10 (capped)", weights["A"])
	}
}

func TestSize_EmptySignals(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{Method: SizingEqual})
	weights, err := sizer.Size([]strategy.Signal{}, 0)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if len(weights) != 0 {
		t.Errorf("expected empty map, got %d entries", len(weights))
	}
}

func TestSize_ZeroTotalStrength_FallbackEqual(t *testing.T) {
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingStrengthProp,
		MaxTotal:    1.0,
		MaxPerStock: 1.0, // no per-stock cap for this test
	})
	signals := makeSignals(map[string]float64{"A": 0, "B": 0, "C": 0})
	weights, _ := sizer.Size(signals, 0)
	// All strengths zero → fall back to equal: 1.0/3 ≈ 0.333
	for _, s := range []string{"A", "B", "C"} {
		if !approxEqual(weights[s], 1.0/3.0) {
			t.Errorf("weight[%s] = %.4f, want %.4f (equal fallback)", s, weights[s], 1.0/3.0)
		}
	}
}

func TestSize_MaxTotalEnforcement(t *testing.T) {
	// 10 signals with fixed 0.15 each → sum=1.5 > MaxTotal=1.0 → scale to 0.10
	sizer, _ := NewPositionSizer(SizingConfig{
		Method:      SizingFixed,
		FixedWeight: 0.15,
		MaxPerStock: 0.20, // per-stock cap above fixed weight
		MaxTotal:    1.0,
	})
	strengths := map[string]float64{}
	for _, s := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"} {
		strengths[s] = 1.0
	}
	weights, _ := sizer.Size(makeSignals(strengths), 0)
	sum := 0.0
	for _, w := range weights {
		sum += w
	}
	// Sum should be ≤ MaxTotal (1.0), with small float tolerance.
	if sum > 1.0+1e-9 {
		t.Errorf("total weight = %.6f, want ≤ 1.0 (MaxTotal)", sum)
	}
	if !approxEqual(sum, 1.0) {
		t.Errorf("total weight = %.6f, want exactly 1.0 (scaled to cap)", sum)
	}
}
