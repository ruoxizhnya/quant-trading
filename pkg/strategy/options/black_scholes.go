package options

import (
	"math"
)

// BlackScholes implements the Black-Scholes-Merton option pricing model
// for European call and put options with continuous dividend yield q.
//
// The model assumes:
//   - European exercise (only at expiry)
//   - Lognormal underlying
//   - Constant r, σ, q
//   - No arbitrage, continuous trading
//
// Greeks are computed analytically; ImpliedVol uses Newton-Raphson on the
// BS price (with bisection fallback protection handled by the caller).
type BlackScholes struct{}

// NewBlackScholes returns a BlackScholes pricer. It is safe for concurrent
// use by any number of goroutines — the type holds no state.
func NewBlackScholes() *BlackScholes { return &BlackScholes{} }

// Price returns the Black-Scholes price for a European option.
//
// Edge cases handled explicitly:
//   - TimeToExpiry == 0 → intrinsic value max(S-K, 0) for calls.
//   - Volatility == 0 (and T > 0) → deterministic discounted payoff.
//   - Spot == 0 → 0 for calls, K·e^(-rT) for puts.
func (bs *BlackScholes) Price(spec OptionSpec) (float64, error) {
	if err := spec.Validate(); err != nil {
		return 0, err
	}
	if spec.Style == American {
		// BS is a European model. We still price it (the formula is valid
		// for the European component) but callers should use Binomial for
		// American options to capture the early-exercise premium.
	}

	S, K, T := spec.Spot, spec.Strike, spec.TimeToExpiry
	r, q, sigma := spec.RiskFreeRate, spec.DividendYield, spec.Volatility

	// --- Edge case: at expiry ---
	if T == 0 {
		return intrinsic(S, K, spec.Type), nil
	}

	// --- Edge case: zero spot ---
	if S == 0 {
		if spec.Type == Call {
			return 0, nil
		}
		return K * math.Exp(-r*T), nil
	}

	// --- Edge case: zero volatility → deterministic payoff ---
	if sigma == 0 {
		discount := math.Exp(-r * T)
		if spec.Type == Call {
			// max(S·e^((r-q)T) - K, 0) · e^(-rT)
			fwd := S * math.Exp((r-q)*T)
			return math.Max(fwd-K, 0) * discount, nil
		}
		fwd := S * math.Exp((r-q)*T)
		return math.Max(K-fwd, 0) * discount, nil
	}

	d1, d2 := bsD1D2(S, K, T, r, q, sigma)
	discountQ := math.Exp(-q * T)
	discountR := math.Exp(-r * T)

	if spec.Type == Call {
		return S*discountQ*NormCDF(d1) - K*discountR*NormCDF(d2), nil
	}
	return K*discountR*NormCDF(-d2) - S*discountQ*NormCDF(-d1), nil
}

// Greeks returns the analytical Black-Scholes Greeks for a European option.
//
// Conventions (matching the Greeks struct doc):
//   - Vega:  per 1% absolute change in σ (raw vega / 100)
//   - Theta: per calendar day (raw theta / 365)
//   - Rho:   per 1% absolute change in r (raw rho / 100)
//
// Delta, Gamma are reported in their natural (per-unit) form.
func (bs *BlackScholes) Greeks(spec OptionSpec) (Greeks, error) {
	if err := spec.Validate(); err != nil {
		return Greeks{}, err
	}

	S, K, T := spec.Spot, spec.Strike, spec.TimeToExpiry
	r, q, sigma := spec.RiskFreeRate, spec.DividendYield, spec.Volatility

	// --- Edge case: at expiry ---
	// Delta collapses to the step function; Gamma/Vega/Theta/Rho → 0.
	if T == 0 {
		var delta float64
		if spec.Type == Call {
			switch {
			case S > K:
				delta = 1
			case S < K:
				delta = 0
			default:
				delta = 0.5 // at-the-money at expiry: convention
			}
		} else {
			switch {
			case S > K:
				delta = 0
			case S < K:
				delta = -1
			default:
				delta = -0.5
			}
		}
		return Greeks{Delta: delta}, nil
	}

	// --- Edge case: zero volatility ---
	// Greeks are degenerate; report intrinsic delta only.
	if sigma == 0 {
		var delta float64
		fwd := S * math.Exp((r-q)*T)
		ITM := (spec.Type == Call && fwd > K) || (spec.Type == Put && fwd < K)
		if ITM {
			if spec.Type == Call {
				delta = math.Exp(-q * T)
			} else {
				delta = -math.Exp(-q * T)
			}
		}
		return Greeks{Delta: delta}, nil
	}

	d1, d2 := bsD1D2(S, K, T, r, q, sigma)
	discountQ := math.Exp(-q * T)
	discountR := math.Exp(-r * T)
	pdfD1 := NormPDF(d1)

	var delta, theta, rho float64
	if spec.Type == Call {
		delta = discountQ * NormCDF(d1)
		theta = (-S * pdfD1 * sigma * discountQ / (2 * math.Sqrt(T))) -
			r*K*discountR*NormCDF(d2) +
			q*S*discountQ*NormCDF(d1)
		rho = K * T * discountR * NormCDF(d2)
	} else {
		delta = -discountQ * NormCDF(-d1)
		theta = (-S * pdfD1 * sigma * discountQ / (2 * math.Sqrt(T))) +
			r*K*discountR*NormCDF(-d2) -
			q*S*discountQ*NormCDF(-d1)
		rho = -K * T * discountR * NormCDF(-d2)
	}

	// Gamma and Vega are identical for calls and puts.
	gamma := discountQ * pdfD1 / (S * sigma * math.Sqrt(T))
	vega := S * discountQ * pdfD1 * math.Sqrt(T)

	return Greeks{
		Delta: delta,
		Gamma: gamma,
		Vega:  vega / 100,
		Theta: theta / 365,
		Rho:   rho / 100,
	}, nil
}

// ImpliedVol recovers the implied volatility from a market option price
// using Newton-Raphson iteration seeded at σ₀ = 0.2.
//
// The iteration uses the analytic BS vega for the derivative:
//
//	σ_{n+1} = σ_n - (BS(σ_n) - marketPrice) / vega(σ_n)
//
// Termination:
//   - |BS(σ_n) - marketPrice| < tol  → success
//   - σ_n <= 0 or σ_n > 5 (500% vol) → ErrNoConvergence
//   - maxIter (100) reached without convergence → ErrNoConvergence
//
// ImpliedVol works for European options. For American options the caller
// should use a Binomial-based root finder instead.
func (bs *BlackScholes) ImpliedVol(marketPrice float64, spec OptionSpec) (float64, error) {
	if err := spec.Validate(); err != nil {
		return 0, err
	}
	if marketPrice < 0 {
		return 0, ErrNegativeSpot // reuse: invalid market price
	}

	const (
		maxIter = 100
		tol     = 1e-6
	)

	// Sanity: market price must be ≥ intrinsic value (no arbitrage).
	intr := intrinsic(spec.Spot, spec.Strike, spec.Type)
	if marketPrice < intr-tol {
		// Price below intrinsic — no positive IV exists. Return 0 to
		// signal "no information" rather than erroring, matching common
		// market practice for stale/deeply mispriced quotes.
		return 0, nil
	}

	sigma := 0.2 // seed
	for i := 0; i < maxIter; i++ {
		priceSpec := spec
		priceSpec.Volatility = sigma
		price, err := bs.Price(priceSpec)
		if err != nil {
			return 0, err
		}
		diff := price - marketPrice
		if math.Abs(diff) < tol {
			return sigma, nil
		}

		g, err := bs.Greeks(priceSpec)
		if err != nil {
			return 0, err
		}
		vegaRaw := g.Vega * 100 // undo the /100 scaling
		if vegaRaw < 1e-12 {
			// Vega too small to make progress — bisection-style fallback.
			break
		}

		sigma -= diff / vegaRaw
		if sigma <= 0 {
			sigma = 1e-4
		}
		if sigma > 5 {
			return 0, ErrNoConvergence
		}
	}

	// Fallback: bisection on [1e-6, 5].
	lo, hi := 1e-6, 5.0
	loSpec, hiSpec := spec, spec
	loSpec.Volatility = lo
	hiSpec.Volatility = hi
	loP, err := bs.Price(loSpec)
	if err != nil {
		return 0, err
	}
	hiP, err := bs.Price(hiSpec)
	if err != nil {
		return 0, err
	}
	if marketPrice < loP-tol || marketPrice > hiP+tol {
		return 0, ErrNoConvergence
	}

	for i := 0; i < maxIter; i++ {
		mid := (lo + hi) / 2
		midSpec := spec
		midSpec.Volatility = mid
		midP, err := bs.Price(midSpec)
		if err != nil {
			return 0, err
		}
		if math.Abs(midP-marketPrice) < tol {
			return mid, nil
		}
		if midP < marketPrice {
			lo = mid
		} else {
			hi = mid
		}
	}
	return 0, ErrNoConvergence
}

// bsD1D2 computes the standard Black-Scholes d1 and d2.
//
//	d1 = (ln(S/K) + (r - q + σ²/2)·T) / (σ·√T)
//	d2 = d1 - σ·√T
func bsD1D2(S, K, T, r, q, sigma float64) (float64, float64) {
	sqrtT := math.Sqrt(T)
	d1 := (math.Log(S/K) + (r-q+0.5*sigma*sigma)*T) / (sigma * sqrtT)
	d2 := d1 - sigma*sqrtT
	return d1, d2
}

// intrinsic returns the option payoff at expiry.
func intrinsic(S, K float64, t OptionType) float64 {
	if t == Call {
		return math.Max(S-K, 0)
	}
	return math.Max(K-S, 0)
}
