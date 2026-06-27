package backtest

import (
	"math"
	"testing"
)

// genEquityCurve builds an equity curve of length n starting at start,
// applying the per-step percent change function pct(i). This keeps
// tests free of hand-typed 30-element arrays.
func genEquityCurve(n int, start float64, pct func(i int) float64) []float64 {
	curve := make([]float64, n)
	curve[0] = start
	for i := 1; i < n; i++ {
		curve[i] = curve[i-1] * (1 + pct(i))
	}
	return curve
}

func TestCalculateStrategyCorrelation_TwoStrategies(t *testing.T) {
	// Two independent-ish strategies, both with 40 data points.
	curveA := genEquityCurve(40, 100.0, func(i int) float64 { return 0.001 * float64(i%5) })
	curveB := genEquityCurve(40, 100.0, func(i int) float64 { return -0.001 * float64(i%3) })

	matrix, err := CalculateStrategyCorrelation(map[string][]float64{
		"alpha": curveA,
		"beta":  curveB,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(matrix.Strategies) != 2 {
		t.Fatalf("Strategies len = %d, want 2", len(matrix.Strategies))
	}
	// Strategies are sorted alphabetically.
	if matrix.Strategies[0] != "alpha" || matrix.Strategies[1] != "beta" {
		t.Errorf("Strategies = %v, want [alpha beta]", matrix.Strategies)
	}
	if len(matrix.Correlations) != 2 || len(matrix.Correlations[0]) != 2 {
		t.Fatalf("matrix shape = %v, want 2x2", shapeOf(matrix.Correlations))
	}

	// Diagonal must be 1.0 (self-correlation).
	if math.Abs(matrix.Correlations[0][0]-1.0) > 1e-9 {
		t.Errorf("matrix[0][0] = %v, want 1.0", matrix.Correlations[0][0])
	}
	if math.Abs(matrix.Correlations[1][1]-1.0) > 1e-9 {
		t.Errorf("matrix[1][1] = %v, want 1.0", matrix.Correlations[1][1])
	}
	// Off-diagonal must be symmetric.
	if matrix.Correlations[0][1] != matrix.Correlations[1][0] {
		t.Errorf("matrix not symmetric: [0][1]=%v [1][0]=%v", matrix.Correlations[0][1], matrix.Correlations[1][0])
	}
	// Off-diagonal must be a valid correlation in [-1, 1].
	c := matrix.Correlations[0][1]
	if math.IsNaN(c) || c < -1.0 || c > 1.0 {
		t.Errorf("off-diagonal = %v, want value in [-1, 1]", c)
	}
}

func TestCalculateStrategyCorrelation_PerfectCorrelation(t *testing.T) {
	// curveB is a scalar multiple of curveA → identical return series
	// → Pearson correlation is exactly 1.0.
	curveA := genEquityCurve(30, 100.0, func(i int) float64 { return 0.001 * float64(i%4) })
	curveB := make([]float64, len(curveA))
	for i := range curveA {
		curveB[i] = 2.0 * curveA[i]
	}

	matrix, err := CalculateStrategyCorrelation(map[string][]float64{
		"a": curveA,
		"b": curveB,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := matrix.Correlations[0][1]
	if math.Abs(c-1.0) > 1e-9 {
		t.Errorf("perfect correlation = %v, want 1.0", c)
	}
}

func TestCalculateStrategyCorrelation_NegativeCorrelation(t *testing.T) {
	// curveA oscillates up/down; curveB oscillates down/up — their
	// returns are strongly anti-correlated.
	curveA := genEquityCurve(30, 100.0, func(i int) float64 {
		if i%2 == 0 {
			return 0.01
		}
		return -0.0099
	})
	curveB := genEquityCurve(30, 100.0, func(i int) float64 {
		if i%2 == 0 {
			return -0.01
		}
		return 0.0101
	})

	matrix, err := CalculateStrategyCorrelation(map[string][]float64{
		"a": curveA,
		"b": curveB,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := matrix.Correlations[0][1]
	// Returns are perfectly opposite in sign → strongly negative.
	if c >= -0.99 {
		t.Errorf("negative correlation = %v, want < -0.99", c)
	}
}

func TestCalculateStrategyCorrelation_InsufficientData(t *testing.T) {
	t.Run("single strategy", func(t *testing.T) {
		_, err := CalculateStrategyCorrelation(map[string][]float64{
			"only": genEquityCurve(30, 100.0, func(i int) float64 { return 0.01 }),
		})
		if err == nil {
			t.Fatal("expected error for single strategy, got nil")
		}
	})

	t.Run("too few points", func(t *testing.T) {
		short := genEquityCurve(29, 100.0, func(i int) float64 { return 0.01 }) // 29 < 30
		long := genEquityCurve(30, 100.0, func(i int) float64 { return 0.01 })
		_, err := CalculateStrategyCorrelation(map[string][]float64{
			"a": short,
			"b": long,
		})
		if err == nil {
			t.Fatal("expected error for <30 points, got nil")
		}
	})

	t.Run("empty map", func(t *testing.T) {
		_, err := CalculateStrategyCorrelation(map[string][]float64{})
		if err == nil {
			t.Fatal("expected error for empty map, got nil")
		}
	})
}

// shapeOf returns the row x col dimensions of a jagged matrix for
// error messages.
func shapeOf(m [][]float64) string {
	if len(m) == 0 {
		return "0x0"
	}
	return string(rune('0'+len(m))) + "x" + string(rune('0'+len(m[0])))
}
