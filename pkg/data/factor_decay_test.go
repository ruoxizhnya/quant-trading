package data

import (
	"math"
	"testing"
)

func TestCalculateFactorDecay_BasicDecay(t *testing.T) {
	// 5 stocks with ascending factor scores. Day-0 returns are aligned
	// with the factor (IC = 1.0 at horizon 1), but days 1-2 reverse the
	// ranking so the 3-day cumulative return is anti-correlated with the
	// factor (IC = -1.0 at horizon 3).
	factorScores := []float64{1, 2, 3, 4, 5}
	returns := [][]float64{
		{0.01, 0.02, 0.03, 0.04, 0.05}, // day 0: aligned with factor
		{0.05, 0.04, 0.03, 0.02, 0.01}, // day 1: reversed
		{0.05, 0.04, 0.03, 0.02, 0.01}, // day 2: reversed
	}
	horizons := []int{1, 3}

	result, err := CalculateFactorDecay(factorScores, returns, horizons)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ICs) != 2 || len(result.Decays) != 2 {
		t.Fatalf("ICs/Decays len = %d/%d, want 2/2", len(result.ICs), len(result.Decays))
	}
	if len(result.Horizons) != 2 || result.Horizons[0] != 1 || result.Horizons[1] != 3 {
		t.Errorf("Horizons = %v, want [1 3]", result.Horizons)
	}

	// Horizon 1: factor and day-0 returns are perfectly monotonic → IC = 1.0
	if math.Abs(result.ICs[0]-1.0) > 1e-9 {
		t.Errorf("ICs[0] = %v, want 1.0", result.ICs[0])
	}
	// Horizon 3: cumulative return is anti-correlated → IC = -1.0
	if math.Abs(result.ICs[1]-(-1.0)) > 1e-9 {
		t.Errorf("ICs[1] = %v, want -1.0", result.ICs[1])
	}
	// Decay relative to the shortest-horizon IC.
	if math.Abs(result.Decays[0]-1.0) > 1e-9 {
		t.Errorf("Decays[0] = %v, want 1.0", result.Decays[0])
	}
	if math.Abs(result.Decays[1]-(-1.0)) > 1e-9 {
		t.Errorf("Decays[1] = %v, want -1.0", result.Decays[1])
	}
}

func TestCalculateFactorDecay_SingleHorizon(t *testing.T) {
	factorScores := []float64{1, 2, 3, 4, 5}
	returns := [][]float64{
		{0.01, 0.02, 0.03, 0.04, 0.05},
		{0.01, 0.02, 0.03, 0.04, 0.05},
	}
	horizons := []int{2}

	result, err := CalculateFactorDecay(factorScores, returns, horizons)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ICs) != 1 || len(result.Decays) != 1 {
		t.Fatalf("ICs/Decays len = %d/%d, want 1/1", len(result.ICs), len(result.Decays))
	}
	// Both days aligned with factor → cumulative return stays monotonic → IC = 1.0.
	if math.Abs(result.ICs[0]-1.0) > 1e-9 {
		t.Errorf("ICs[0] = %v, want 1.0", result.ICs[0])
	}
	// Decay relative to itself is always 1.0.
	if math.Abs(result.Decays[0]-1.0) > 1e-9 {
		t.Errorf("Decays[0] = %v, want 1.0", result.Decays[0])
	}
}

func TestCalculateFactorDecay_InsufficientData(t *testing.T) {
	t.Run("too few stocks", func(t *testing.T) {
		_, err := CalculateFactorDecay([]float64{1}, [][]float64{{0.01}}, []int{1})
		if err == nil {
			t.Fatal("expected error for <2 stocks, got nil")
		}
	})

	t.Run("no horizons", func(t *testing.T) {
		_, err := CalculateFactorDecay([]float64{1, 2}, [][]float64{{0.01, 0.02}}, []int{})
		if err == nil {
			t.Fatal("expected error for empty horizons, got nil")
		}
	})

	t.Run("non-positive horizon", func(t *testing.T) {
		_, err := CalculateFactorDecay([]float64{1, 2}, [][]float64{{0.01, 0.02}}, []int{0})
		if err == nil {
			t.Fatal("expected error for horizon <= 0, got nil")
		}
	})

	t.Run("not enough return days", func(t *testing.T) {
		// Need 3 days for horizon 3, only provide 2.
		returns := [][]float64{
			{0.01, 0.02},
			{0.01, 0.02},
		}
		_, err := CalculateFactorDecay([]float64{1, 2}, returns, []int{3})
		if err == nil {
			t.Fatal("expected error for insufficient return days, got nil")
		}
	})

	t.Run("stock count mismatch", func(t *testing.T) {
		// factorScores has 3 stocks but returns rows have 2.
		returns := [][]float64{
			{0.01, 0.02},
		}
		_, err := CalculateFactorDecay([]float64{1, 2, 3}, returns, []int{1})
		if err == nil {
			t.Fatal("expected error for stock count mismatch, got nil")
		}
	})
}
