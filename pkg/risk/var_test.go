package risk

import (
	"math"
	"testing"
)

// makeLinearReturns builds n ascending returns: start, start+step, ...
// Used to produce deterministic, exactly-indexable return series so
// the expected VaR/CVaR quantiles can be hand-computed.
func makeLinearReturns(n int, start, step float64) []float64 {
	r := make([]float64, n)
	for i := 0; i < n; i++ {
		r[i] = start + step*float64(i)
	}
	return r
}

// For the 95% / 99% tests we use 101 returns from -0.050 to +0.050
// (step 0.001). n=101 makes the percentile index land exactly on an
// integer for both confidence levels, so no interpolation occurs:
//
//	conf 0.95 → tailProb 0.05 → index 0.05*100 = 5.0 → r[5]  = -0.045
//	conf 0.99 → tailProb 0.01 → index 0.01*100 = 1.0 → r[1]  = -0.049
//
// CVaR uses the inclusive `<=` contract, so the boundary element is
// part of the tail.

func TestCalculateVaR_95(t *testing.T) {
	returns := makeLinearReturns(101, -0.050, 0.001)
	const pv = 1_000_000.0
	res, err := CalculateVaR(VaRRequest{Returns: returns, Confidence: 0.95, PortfolioValue: pv})
	if err != nil {
		t.Fatalf("CalculateVaR returned error: %v", err)
	}
	// VaR pct = 0.045 → VaR = 45000
	const wantVaR = 0.045 * pv
	if math.Abs(res.VaR-wantVaR) > 1.0 {
		t.Errorf("VaR = %v, want %v", res.VaR, wantVaR)
	}
	// Tail = r[0..5] (6 values), mean = -0.0475 → CVaR pct = 0.0475
	const wantCVaR = 0.0475 * pv
	if math.Abs(res.CVaR-wantCVaR) > 1.0 {
		t.Errorf("CVaR = %v, want %v", res.CVaR, wantCVaR)
	}
	if math.Abs(res.VaRPct-0.045) > 1e-9 {
		t.Errorf("VaRPct = %v, want 0.045", res.VaRPct)
	}
	if math.Abs(res.CVaRPct-0.0475) > 1e-9 {
		t.Errorf("CVaRPct = %v, want 0.0475", res.CVaRPct)
	}
	if res.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", res.Confidence)
	}
	if res.SampleSize != 101 {
		t.Errorf("SampleSize = %v, want 101", res.SampleSize)
	}
}

func TestCalculateVaR_99(t *testing.T) {
	returns := makeLinearReturns(101, -0.050, 0.001)
	const pv = 1_000_000.0
	res, err := CalculateVaR(VaRRequest{Returns: returns, Confidence: 0.99, PortfolioValue: pv})
	if err != nil {
		t.Fatalf("CalculateVaR returned error: %v", err)
	}
	// VaR pct = 0.049 → VaR = 49000
	const wantVaR = 0.049 * pv
	if math.Abs(res.VaR-wantVaR) > 1.0 {
		t.Errorf("VaR = %v, want %v", res.VaR, wantVaR)
	}
	// Tail = r[0..1] (2 values), mean = -0.0495 → CVaR pct = 0.0495
	const wantCVaR = 0.0495 * pv
	if math.Abs(res.CVaR-wantCVaR) > 1.0 {
		t.Errorf("CVaR = %v, want %v", res.CVaR, wantCVaR)
	}
	if math.Abs(res.VaRPct-0.049) > 1e-9 {
		t.Errorf("VaRPct = %v, want 0.049", res.VaRPct)
	}
	if math.Abs(res.CVaRPct-0.0495) > 1e-9 {
		t.Errorf("CVaRPct = %v, want 0.0495", res.CVaRPct)
	}
	if res.Confidence != 0.99 {
		t.Errorf("Confidence = %v, want 0.99", res.Confidence)
	}
	// 99% VaR must be at least as large as 95% VaR (deeper tail).
	res95, _ := CalculateVaR(VaRRequest{Returns: returns, Confidence: 0.95, PortfolioValue: pv})
	if res.VaR < res95.VaR {
		t.Errorf("99%% VaR (%v) should be >= 95%% VaR (%v)", res.VaR, res95.VaR)
	}
}

func TestCalculateVaR_InsufficientData(t *testing.T) {
	// 29 returns — one short of the 30-sample minimum.
	returns := makeLinearReturns(29, -0.010, 0.001)
	res, err := CalculateVaR(VaRRequest{Returns: returns, Confidence: 0.95, PortfolioValue: 1_000_000})
	if err == nil {
		t.Fatal("expected error for insufficient data, got nil")
	}
	if res != nil {
		t.Errorf("expected nil result, got %v", res)
	}
}

func TestCalculateVaR_InvalidConfidence(t *testing.T) {
	returns := makeLinearReturns(50, -0.010, 0.001)

	cases := []float64{0.50, 0.89, 1.5}
	for _, c := range cases {
		res, err := CalculateVaR(VaRRequest{Returns: returns, Confidence: c, PortfolioValue: 1_000_000})
		if err == nil {
			t.Errorf("confidence %v: expected error, got nil", c)
		}
		if res != nil {
			t.Errorf("confidence %v: expected nil result, got %v", c, res)
		}
	}
}

func TestCalculateVaR_ZeroVolatility(t *testing.T) {
	// All returns identical (zero dispersion) → no historical loss
	// → VaR and CVaR are both zero.
	returns := make([]float64, 50) // all 0.0
	res, err := CalculateVaR(VaRRequest{Returns: returns, Confidence: 0.95, PortfolioValue: 1_000_000})
	if err != nil {
		t.Fatalf("CalculateVaR returned error: %v", err)
	}
	if res.VaR != 0 {
		t.Errorf("VaR = %v, want 0 (zero volatility)", res.VaR)
	}
	if res.CVaR != 0 {
		t.Errorf("CVaR = %v, want 0 (zero volatility)", res.CVaR)
	}
	if res.VaRPct != 0 || res.CVaRPct != 0 {
		t.Errorf("VaRPct=%v CVaRPct=%v, want both 0", res.VaRPct, res.CVaRPct)
	}
	if res.SampleSize != 50 {
		t.Errorf("SampleSize = %v, want 50", res.SampleSize)
	}
}
