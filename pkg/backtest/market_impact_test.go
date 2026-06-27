package backtest

import (
	"math"
	"testing"
)

func TestMarketImpact_CalculateImpact(t *testing.T) {
	m := MarketImpactModel{Sigma: 0.02, LiquidityFactor: 1.0}
	// frac = 1000/100000 = 0.01, sqrt(0.01) = 0.1
	// impact = 0.02 * 0.1 * 1.0 = 0.002
	got := m.CalculateImpact(1000, 100000)
	want := 0.002
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CalculateImpact = %v, want %v", got, want)
	}
}

func TestMarketImpact_ZeroADV(t *testing.T) {
	m := MarketImpactModel{Sigma: 0.02, LiquidityFactor: 1.0}
	if got := m.CalculateImpact(1000, 0); got != 0 {
		t.Errorf("CalculateImpact(adv=0) = %v, want 0", got)
	}
	// Negative ADV must not produce NaN; it returns 0.
	if got := m.CalculateImpact(1000, -1); got != 0 {
		t.Errorf("CalculateImpact(adv=-1) = %v, want 0", got)
	}
}

func TestMarketImpact_LargeOrder(t *testing.T) {
	m := MarketImpactModel{Sigma: 0.02, LiquidityFactor: 1.0}
	const adv = 100000
	small := m.CalculateImpact(1000, adv)  // 1% of ADV
	large := m.CalculateImpact(10000, adv) // 10% of ADV
	if small <= 0 {
		t.Fatalf("small order impact = %v, want > 0", small)
	}
	if large <= small {
		t.Errorf("large order impact (%v) should exceed small order impact (%v)", large, small)
	}
}

func TestMarketImpact_SlippageCost(t *testing.T) {
	m := MarketImpactModel{Sigma: 0.02, LiquidityFactor: 1.0}
	// impact = 0.002, cost = 0.002 * 1000 * 50 = 100 CNY
	got := m.CalculateSlippageCost(1000, 100000, 50)
	want := 100.0
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("CalculateSlippageCost = %v, want %v", got, want)
	}
}
