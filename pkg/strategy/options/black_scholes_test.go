package options

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bsATM is a canonical at-the-money European call spec used across tests.
func bsATM(t *testing.T) OptionSpec {
	t.Helper()
	return OptionSpec{
		Spot: 100, Strike: 100, TimeToExpiry: 1.0,
		RiskFreeRate: 0.05, Volatility: 0.20, DividendYield: 0.0,
		Type: Call, Style: European,
	}
}

// TestBS_Call_ATM checks the textbook ATM call value.
// Reference (via math.Erf): BS(S=100,K=100,T=1,r=5%,σ=20%,q=0) ≈ 10.450583572185565.
// Our NormCDF uses the A&S approximation (error ≤ 7.5e-8), which propagates
// to ~7.5e-6 in the price; tolerance 1e-4 comfortably covers this.
func TestBS_Call_ATM(t *testing.T) {
	bs := NewBlackScholes()
	price, err := bs.Price(bsATM(t))
	require.NoError(t, err)
	assert.InDelta(t, 10.450583572185565, price, 1e-4)
}

// TestBS_Put_ATM checks the textbook ATM put value via put-call parity.
func TestBS_Put_ATM(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Type = Put
	price, err := bs.Price(spec)
	require.NoError(t, err)
	// Put-call parity: P = C - S·e^(-qT) + K·e^(-rT)
	callSpec := bsATM(t)
	call, err := bs.Price(callSpec)
	require.NoError(t, err)
	parityPut := call - spec.Spot*math.Exp(-spec.DividendYield*spec.TimeToExpiry) +
		spec.Strike*math.Exp(-spec.RiskFreeRate*spec.TimeToExpiry)
	assert.InDelta(t, parityPut, price, 1e-8)
}

// TestBS_Call_ITM tests a deep in-the-money call.
func TestBS_Call_ITM(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Spot = 130 // deep ITM call
	price, err := bs.Price(spec)
	require.NoError(t, err)
	intrinsic := spec.Spot - spec.Strike
	assert.Greater(t, price, intrinsic)
	// Time value for a 1-year, 20%-vol call is ~5.4; allow headroom.
	assert.InDelta(t, intrinsic, price, 10.0)
}

// TestBS_Put_ITM tests a deep in-the-money put.
// Note: a European put can trade below intrinsic when r > 0, because the
// holder must wait until expiry to receive K. The no-arbitrage lower bound
// is K·e^(-rT) - S, not K - S.
func TestBS_Put_ITM(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Spot = 70
	spec.Type = Put
	price, err := bs.Price(spec)
	require.NoError(t, err)
	// Lower bound for European put: K·e^(-rT) - S.
	lowerBound := spec.Strike*math.Exp(-spec.RiskFreeRate*spec.TimeToExpiry) - spec.Spot
	assert.Greater(t, price, lowerBound, "put price must exceed European lower bound")
	// And it must be ≤ intrinsic (American upper bound).
	intrinsic := spec.Strike - spec.Spot
	assert.LessOrEqual(t, price, intrinsic+1e-9)
}

// TestBS_Call_OTM tests a deep out-of-the-money call (small positive value).
func TestBS_Call_OTM(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Spot = 70
	price, err := bs.Price(spec)
	require.NoError(t, err)
	assert.Greater(t, price, 0.0)
	assert.Less(t, price, 1.0) // deep OTM, small value
}

// TestBS_Put_OTM tests a deep out-of-the-money put.
func TestBS_Put_OTM(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Spot = 130
	spec.Type = Put
	price, err := bs.Price(spec)
	require.NoError(t, err)
	assert.Greater(t, price, 0.0)
	assert.Less(t, price, 1.0)
}

// TestBS_WithDividend checks that a positive dividend yield reduces the
// call price and increases the put price.
func TestBS_WithDividend(t *testing.T) {
	bs := NewBlackScholes()
	noDiv := bsATM(t)
	withDiv := bsATM(t)
	withDiv.DividendYield = 0.03

	callNo, err := bs.Price(noDiv)
	require.NoError(t, err)
	callDiv, err := bs.Price(withDiv)
	require.NoError(t, err)
	assert.Less(t, callDiv, callNo, "dividend should lower call price")

	noDiv.Type = Put
	withDiv.Type = Put
	putNo, err := bs.Price(noDiv)
	require.NoError(t, err)
	putDiv, err := bs.Price(withDiv)
	require.NoError(t, err)
	assert.Greater(t, putDiv, putNo, "dividend should raise put price")
}

// TestBS_AtExpiry returns the intrinsic value when T==0.
func TestBS_AtExpiry(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.TimeToExpiry = 0

	// ITM call at expiry
	spec.Spot = 110
	price, err := bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, price, 1e-12)

	// OTM call at expiry
	spec.Spot = 90
	price, err = bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, price, 1e-12)

	// ITM put at expiry
	spec.Type = Put
	spec.Spot = 90
	price, err = bs.Price(spec)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, price, 1e-12)
}

// TestBS_ZeroVolatility checks the deterministic payoff when σ=0.
func TestBS_ZeroVolatility(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Volatility = 0
	// Forward = S·e^((r-q)T) = 100·e^(0.05) ≈ 105.127; ITM call.
	price, err := bs.Price(spec)
	require.NoError(t, err)
	fwd := spec.Spot * math.Exp((spec.RiskFreeRate-spec.DividendYield)*spec.TimeToExpiry)
	want := math.Max(fwd-spec.Strike, 0) * math.Exp(-spec.RiskFreeRate*spec.TimeToExpiry)
	assert.InDelta(t, want, price, 1e-10)
}

// TestBS_ValidationErrors covers all invalid-spec paths.
func TestBS_ValidationErrors(t *testing.T) {
	bs := NewBlackScholes()
	cases := []struct {
		name string
		mod  func(OptionSpec) OptionSpec
		err  error
	}{
		{"negative spot", func(s OptionSpec) OptionSpec { s.Spot = -1; return s }, ErrNegativeSpot},
		{"zero strike", func(s OptionSpec) OptionSpec { s.Strike = 0; return s }, ErrInvalidStrike},
		{"negative strike", func(s OptionSpec) OptionSpec { s.Strike = -1; return s }, ErrInvalidStrike},
		{"negative time", func(s OptionSpec) OptionSpec { s.TimeToExpiry = -1; return s }, ErrNegativeTime},
		{"negative vol", func(s OptionSpec) OptionSpec { s.Volatility = -0.1; return s }, ErrNegativeVolatility},
		{"bad type", func(s OptionSpec) OptionSpec { s.Type = "straddle"; return s }, ErrInvalidOptionType},
		{"bad style", func(s OptionSpec) OptionSpec { s.Style = "bermudan"; return s }, ErrInvalidExerciseStyle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := bs.Price(c.mod(bsATM(t)))
			assert.ErrorIs(t, err, c.err)
		})
	}
}

// TestBS_Greeks_Call_ATM checks the analytical Greeks for an ATM call.
// Reference values computed with math.Erf (high-precision CDF). Our A&S
// NormCDF introduces ~6e-8 error in Delta; Gamma uses NormPDF (exact) so
// it matches to full precision.
func TestBS_Greeks_Call_ATM(t *testing.T) {
	bs := NewBlackScholes()
	g, err := bs.Greeks(bsATM(t))
	require.NoError(t, err)

	// Reference values from a textbook BS calculator (math.Erf).
	assert.InDelta(t, 0.6368306511756191, g.Delta, 1e-6) // A&S CDF error ~6e-8
	assert.InDelta(t, 0.0187620173458469, g.Gamma, 1e-8) // NormPDF is exact
	assert.InDelta(t, 0.3752405443020333, g.Vega, 1e-6)  // per 1% vol
	// Theta per day is small and negative for long calls.
	assert.Less(t, g.Theta, 0.0)
	// Rho per 1% rate is positive for calls.
	assert.Greater(t, g.Rho, 0.0)
}

// TestBS_Greeks_Put_ATM checks put Greeks and put-call relations.
func TestBS_Greeks_Put_ATM(t *testing.T) {
	bs := NewBlackScholes()
	callSpec := bsATM(t)
	putSpec := bsATM(t)
	putSpec.Type = Put

	gc, err := bs.Greeks(callSpec)
	require.NoError(t, err)
	gp, err := bs.Greeks(putSpec)
	require.NoError(t, err)

	// Put-call parity for delta: Δ_call - Δ_put = e^(-qT)
	assert.InDelta(t, math.Exp(-callSpec.DividendYield*callSpec.TimeToExpiry),
		gc.Delta-gp.Delta, 1e-10)
	// Gamma and Vega are identical for calls and puts.
	assert.InDelta(t, gc.Gamma, gp.Gamma, 1e-10)
	assert.InDelta(t, gc.Vega, gp.Vega, 1e-10)
}

// TestBS_Greeks_FiniteDifference verifies analytical Greeks against
// central finite differences of the BS price.
func TestBS_Greeks_FiniteDifference(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Spot = 105 // slightly ITM to exercise all branches

	g, err := bs.Greeks(spec)
	require.NoError(t, err)

	const hS = 0.01
	upSpec, dnSpec := spec, spec
	upSpec.Spot = spec.Spot + hS
	dnSpec.Spot = spec.Spot - hS
	pUp, err := bs.Price(upSpec)
	require.NoError(t, err)
	pDn, err := bs.Price(dnSpec)
	require.NoError(t, err)

	fdDelta := (pUp - pDn) / (2 * hS)
	fdGamma := (pUp - 2*bsPriceOrDie(t, bs, spec) + pDn) / (hS * hS)

	assert.InDelta(t, fdDelta, g.Delta, 1e-4)
	assert.InDelta(t, fdGamma, g.Gamma, 1e-4)
}

// TestBS_Greeks_AtExpiry checks the degenerate Greeks at expiry.
func TestBS_Greeks_AtExpiry(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.TimeToExpiry = 0
	spec.Spot = 110 // ITM call
	g, err := bs.Greeks(spec)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, g.Delta, 1e-12)
	assert.InDelta(t, 0.0, g.Gamma, 1e-12)
	assert.InDelta(t, 0.0, g.Vega, 1e-12)

	// ITM put at expiry (S < K → delta = -1).
	spec.Type = Put
	spec.Spot = 90
	g, err = bs.Greeks(spec)
	require.NoError(t, err)
	assert.InDelta(t, -1.0, g.Delta, 1e-12)

	// OTM put at expiry (S > K → delta = 0).
	spec.Spot = 110
	g, err = bs.Greeks(spec)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, g.Delta, 1e-12)
}

// TestBS_ImpliedVol_RoundTrip recovers a known volatility from a BS price.
func TestBS_ImpliedVol_RoundTrip(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Volatility = 0.2731 // arbitrary "true" IV
	price, err := bs.Price(spec)
	require.NoError(t, err)

	ivSpec := spec
	ivSpec.Volatility = 0 // unknown; solver should ignore this
	iv, err := bs.ImpliedVol(price, ivSpec)
	require.NoError(t, err)
	assert.InDelta(t, 0.2731, iv, 1e-6)
}

// TestBS_ImpliedVol_OTMOption checks IV recovery for an OTM option.
func TestBS_ImpliedVol_OTMOption(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Spot = 80 // OTM call
	spec.Volatility = 0.35
	price, err := bs.Price(spec)
	require.NoError(t, err)

	ivSpec := spec
	ivSpec.Volatility = 0
	iv, err := bs.ImpliedVol(price, ivSpec)
	require.NoError(t, err)
	assert.InDelta(t, 0.35, iv, 1e-5)
}

// TestBS_ImpliedVol_BelowIntrinsic checks the no-arbitrage guard.
func TestBS_ImpliedVol_BelowIntrinsic(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.TimeToExpiry = 0.0001          // ~intrinsic
	spec.Spot = 110                     // ITM call, intrinsic = 10
	iv, err := bs.ImpliedVol(5.0, spec) // below intrinsic
	require.NoError(t, err)
	assert.InDelta(t, 0.0, iv, 1e-12)
}

// TestBS_ImpliedVol_Put recovers IV for a put.
func TestBS_ImpliedVol_Put(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.Type = Put
	spec.Volatility = 0.18
	price, err := bs.Price(spec)
	require.NoError(t, err)

	ivSpec := spec
	ivSpec.Volatility = 0
	iv, err := bs.ImpliedVol(price, ivSpec)
	require.NoError(t, err)
	assert.InDelta(t, 0.18, iv, 1e-6)
}

// TestBS_PutCallParity verifies C - P = S·e^(-qT) - K·e^(-rT).
func TestBS_PutCallParity(t *testing.T) {
	bs := NewBlackScholes()
	spec := bsATM(t)
	spec.DividendYield = 0.02

	callSpec, putSpec := spec, spec
	putSpec.Type = Put
	c, err := bs.Price(callSpec)
	require.NoError(t, err)
	p, err := bs.Price(putSpec)
	require.NoError(t, err)

	lhs := c - p
	rhs := spec.Spot*math.Exp(-spec.DividendYield*spec.TimeToExpiry) -
		spec.Strike*math.Exp(-spec.RiskFreeRate*spec.TimeToExpiry)
	assert.InDelta(t, rhs, lhs, 1e-10)
}

// bsPriceOrDie is a small helper to keep finite-difference tests readable.
func bsPriceOrDie(t *testing.T, bs *BlackScholes, spec OptionSpec) float64 {
	t.Helper()
	p, err := bs.Price(spec)
	require.NoError(t, err)
	return p
}
