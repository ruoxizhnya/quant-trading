package options

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binomATM is a canonical ATM European call spec for binomial tests.
func binomATM(t *testing.T) OptionSpec {
	t.Helper()
	return OptionSpec{
		Spot: 100, Strike: 100, TimeToExpiry: 1.0,
		RiskFreeRate: 0.05, Volatility: 0.20, DividendYield: 0.0,
		Type: Call, Style: European,
	}
}

// TestBinomial_DefaultSteps verifies that steps<=0 falls back to the default.
func TestBinomial_DefaultSteps(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	p0, err := b.Price(spec, 0)
	require.NoError(t, err)
	pDef, err := b.Price(spec, DefaultBinomialSteps)
	require.NoError(t, err)
	assert.InDelta(t, pDef, p0, 1e-12)
}

// TestBinomial_Call_ATM checks the ATM European call against BS.
func TestBinomial_Call_ATM(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)

	bp, err := b.Price(spec, 1000)
	require.NoError(t, err)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, bsP, bp, 0.01) // convergence tolerance per spec
}

// TestBinomial_Put_ATM checks the ATM European put against BS.
func TestBinomial_Put_ATM(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	spec.Type = Put

	bp, err := b.Price(spec, 1000)
	require.NoError(t, err)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, bsP, bp, 0.01)
}

// TestBinomial_ConvergesToBS verifies convergence as steps increase.
func TestBinomial_ConvergesToBS(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)

	prev := math.Abs(b.priceTree(spec, 50) - bsP)
	cur := math.Abs(b.priceTree(spec, 200) - bsP)
	assert.Less(t, cur, prev, "error should decrease with more steps")
}

// TestBinomial_Call_ITM checks an ITM European call.
func TestBinomial_Call_ITM(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	spec.Spot = 130

	bp, err := b.Price(spec, 1000)
	require.NoError(t, err)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, bsP, bp, 0.01)
}

// TestBinomial_Put_OTM checks an OTM European put.
func TestBinomial_Put_OTM(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	spec.Spot = 130
	spec.Type = Put

	bp, err := b.Price(spec, 1000)
	require.NoError(t, err)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, bsP, bp, 0.01)
}

// TestBinomial_WithDividend checks dividend-aware pricing against BS.
func TestBinomial_WithDividend(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	spec.DividendYield = 0.03

	bp, err := b.Price(spec, 1000)
	require.NoError(t, err)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, bsP, bp, 0.01)
}

// TestBinomial_AtExpiry returns intrinsic value when T==0.
func TestBinomial_AtExpiry(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.TimeToExpiry = 0
	spec.Spot = 110

	price, err := b.Price(spec, 100)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, price, 1e-12)

	spec.Type = Put
	spec.Spot = 90
	price, err = b.Price(spec, 100)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, price, 1e-12)
}

// TestBinomial_ZeroVolatility checks the deterministic payoff when σ=0.
func TestBinomial_ZeroVolatility(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.Volatility = 0
	price, err := b.Price(spec, 100)
	require.NoError(t, err)
	fwd := spec.Spot * math.Exp((spec.RiskFreeRate-spec.DividendYield)*spec.TimeToExpiry)
	want := math.Max(fwd-spec.Strike, 0) * math.Exp(-spec.RiskFreeRate*spec.TimeToExpiry)
	assert.InDelta(t, want, price, 1e-10)
}

// TestBinomial_AmericanCall_NoEarlyExercise verifies that an American call
// on a non-dividend-paying asset equals the European call (no early exercise).
func TestBinomial_AmericanCall_NoEarlyExercise(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	spec.Style = American
	// No dividend → American call = European call.
	bp, err := b.Price(spec, 1000)
	require.NoError(t, err)
	bsP, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, bsP, bp, 0.01)
}

// TestBinomial_AmericanPut_GreaterThanEuropean checks that an American put
// is worth at least as much as the European put (early-exercise premium).
func TestBinomial_AmericanPut_GreaterThanEuropean(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.Type = Put

	eur, err := b.Price(spec, 1000)
	require.NoError(t, err)
	spec.Style = American
	amer, err := b.Price(spec, 1000)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, amer, eur-1e-9, "American put >= European put")
}

// TestBinomial_AmericanPut_DeepITM checks early exercise for a deep ITM put.
func TestBinomial_AmericanPut_DeepITM(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.Type = Put
	spec.Spot = 50 // deep ITM
	spec.Style = American

	amer, err := b.Price(spec, 1000)
	require.NoError(t, err)
	// Should be close to intrinsic (K - S = 50) but slightly above due to
	// the time value of the strike.
	assert.Greater(t, amer, 50.0-1e-9)
	assert.Less(t, amer, 50.0+1.0)
}

// TestBinomial_AmericanPut_WithDividend verifies dividend raises the
// early-exercise premium for puts.
func TestBinomial_AmericanPut_WithDividend(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.Type = Put
	spec.Style = American

	noDiv := spec
	noDiv.DividendYield = 0
	withDiv := spec
	withDiv.DividendYield = 0.05

	pNo, err := b.Price(noDiv, 1000)
	require.NoError(t, err)
	pDiv, err := b.Price(withDiv, 1000)
	require.NoError(t, err)
	// Higher dividend → lower spot drift → put more valuable.
	assert.Greater(t, pDiv, pNo)
}

// TestBinomial_ValidationErrors covers invalid-spec paths.
func TestBinomial_ValidationErrors(t *testing.T) {
	b := NewBinomial()
	cases := []struct {
		name string
		mod  func(OptionSpec) OptionSpec
		err  error
	}{
		{"negative spot", func(s OptionSpec) OptionSpec { s.Spot = -1; return s }, ErrNegativeSpot},
		{"zero strike", func(s OptionSpec) OptionSpec { s.Strike = 0; return s }, ErrInvalidStrike},
		{"negative time", func(s OptionSpec) OptionSpec { s.TimeToExpiry = -1; return s }, ErrNegativeTime},
		{"negative vol", func(s OptionSpec) OptionSpec { s.Volatility = -0.1; return s }, ErrNegativeVolatility},
		{"bad type", func(s OptionSpec) OptionSpec { s.Type = "binary"; return s }, ErrInvalidOptionType},
		{"bad style", func(s OptionSpec) OptionSpec { s.Style = "asian"; return s }, ErrInvalidExerciseStyle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := b.Price(c.mod(binomATM(t)), 100)
			assert.ErrorIs(t, err, c.err)
		})
	}
}

// TestBinomial_Greeks_Call_ATM compares binomial Greeks to BS Greeks.
func TestBinomial_Greeks_Call_ATM(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)

	bg, err := b.Greeks(spec, 1000)
	require.NoError(t, err)
	bsg, err := bs.Greeks(spec)
	require.NoError(t, err)

	assert.InDelta(t, bsg.Delta, bg.Delta, 0.01)
	assert.InDelta(t, bsg.Gamma, bg.Gamma, 0.01)
	assert.InDelta(t, bsg.Vega, bg.Vega, 0.01)
	// Theta and Rho are noisier under finite differences; allow more slack.
	assert.InDelta(t, bsg.Theta, bg.Theta, 0.05)
	assert.InDelta(t, bsg.Rho, bg.Rho, 0.01)
}

// TestBinomial_Greeks_Put_ATM compares binomial put Greeks to BS.
func TestBinomial_Greeks_Put_ATM(t *testing.T) {
	b := NewBinomial()
	bs := NewBlackScholes()
	spec := binomATM(t)
	spec.Type = Put

	bg, err := b.Greeks(spec, 1000)
	require.NoError(t, err)
	bsg, err := bs.Greeks(spec)
	require.NoError(t, err)

	assert.InDelta(t, bsg.Delta, bg.Delta, 0.01)
	assert.InDelta(t, bsg.Gamma, bg.Gamma, 0.01)
	assert.InDelta(t, bsg.Vega, bg.Vega, 0.01)
}

// TestBinomial_Greeks_AmericanPut verifies Greeks are computed for American puts.
func TestBinomial_Greeks_AmericanPut(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.Type = Put
	spec.Style = American

	g, err := b.Greeks(spec, 500)
	require.NoError(t, err)
	// Put delta is non-positive.
	assert.LessOrEqual(t, g.Delta, 0.0)
	// Gamma is non-negative.
	assert.GreaterOrEqual(t, g.Gamma, 0.0)
	// Long option theta is negative.
	assert.Less(t, g.Theta, 0.0)
	// Vega is non-negative.
	assert.GreaterOrEqual(t, g.Vega, 0.0)
}

// TestBinomial_Greeks_DefaultSteps checks steps<=0 falls back to default.
func TestBinomial_Greeks_DefaultSteps(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	g0, err := b.Greeks(spec, 0)
	require.NoError(t, err)
	gDef, err := b.Greeks(spec, DefaultBinomialSteps)
	require.NoError(t, err)
	assert.InDelta(t, gDef.Delta, g0.Delta, 1e-12)
}

// TestBinomial_Greeks_FiniteDifferenceCrossCheck verifies the binomial
// Greeks are consistent with finite differences of the binomial price.
func TestBinomial_Greeks_FiniteDifferenceCrossCheck(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.Spot = 105
	steps := 500

	g, err := b.Greeks(spec, steps)
	require.NoError(t, err)

	// Independent central FD of the binomial price.
	const hS = 0.5
	upSpec, dnSpec := spec, spec
	upSpec.Spot = spec.Spot + hS
	dnSpec.Spot = spec.Spot - hS
	pUp, err := b.Price(upSpec, steps)
	require.NoError(t, err)
	pDn, err := b.Price(dnSpec, steps)
	require.NoError(t, err)
	fdDelta := (pUp - pDn) / (2 * hS)

	assert.InDelta(t, fdDelta, g.Delta, 0.01)
}

// TestBinomial_PutCallParity_European verifies C - P = S·e^(-qT) - K·e^(-rT)
// for European binomial prices (with enough steps).
func TestBinomial_PutCallParity_European(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	spec.DividendYield = 0.02

	callSpec, putSpec := spec, spec
	putSpec.Type = Put
	c, err := b.Price(callSpec, 1000)
	require.NoError(t, err)
	p, err := b.Price(putSpec, 1000)
	require.NoError(t, err)

	lhs := c - p
	rhs := spec.Spot*math.Exp(-spec.DividendYield*spec.TimeToExpiry) -
		spec.Strike*math.Exp(-spec.RiskFreeRate*spec.TimeToExpiry)
	assert.InDelta(t, rhs, lhs, 0.02)
}

// TestBinomial_ConcurrentSafety runs pricers concurrently under -race.
func TestBinomial_ConcurrentSafety(t *testing.T) {
	b := NewBinomial()
	spec := binomATM(t)
	done := make(chan float64, 8)
	for i := 0; i < 8; i++ {
		go func() {
			p, _ := b.Price(spec, 200)
			done <- p
		}()
	}
	first := <-done
	for i := 1; i < 8; i++ {
		assert.InDelta(t, first, <-done, 1e-12)
	}
}
