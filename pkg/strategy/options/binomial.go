package options

import "math"

// DefaultBinomialSteps is the default number of time steps used by the
// binomial pricer when the caller does not specify one.
const DefaultBinomialSteps = 100

// Binomial implements the Cox-Ross-Rubinstein (CRR) binomial tree model.
// It supports both European and American exercise; for American options
// the backward induction checks for early exercise at every node.
//
// Greeks are computed by central finite differences (bumping spot and
// volatility), which is the standard approach for tree-based pricers.
type Binomial struct{}

// NewBinomial returns a Binomial pricer. It is safe for concurrent use.
func NewBinomial() *Binomial { return &Binomial{} }

// Price returns the binomial option price. If steps <= 0 the default
// (DefaultBinomialSteps) is used.
//
// For European options the result converges to the Black-Scholes price
// as steps → ∞; 1000 steps typically agree to within 0.01.
func (b *Binomial) Price(spec OptionSpec, steps int) (float64, error) {
	if err := spec.Validate(); err != nil {
		return 0, err
	}
	if steps <= 0 {
		steps = DefaultBinomialSteps
	}
	return b.priceTree(spec, steps), nil
}

// priceTree runs the CRR backward induction. It does not re-validate
// the spec — callers must have done so already.
func (b *Binomial) priceTree(spec OptionSpec, steps int) float64 {
	S, K, T := spec.Spot, spec.Strike, spec.TimeToExpiry
	r, q, sigma := spec.RiskFreeRate, spec.DividendYield, spec.Volatility

	// --- Edge case: at expiry ---
	if T == 0 {
		return intrinsic(S, K, spec.Type)
	}

	// --- Edge case: zero spot ---
	if S == 0 {
		if spec.Type == Call {
			return 0
		}
		return K * math.Exp(-r*T)
	}

	// --- Edge case: zero volatility → deterministic payoff ---
	if sigma == 0 {
		discount := math.Exp(-r * T)
		fwd := S * math.Exp((r-q)*T)
		if spec.Type == Call {
			return math.Max(fwd-K, 0) * discount
		}
		return math.Max(K-fwd, 0) * discount
	}

	dt := T / float64(steps)
	u := math.Exp(sigma * math.Sqrt(dt))
	d := 1 / u
	// Risk-neutral probability with dividend yield.
	p := (math.Exp((r-q)*dt) - d) / (u - d)
	discount := math.Exp(-r * dt)

	// Terminal asset prices: S·u^j·d^(n-j) = S·u^(2j-n)
	// Terminal option values: intrinsic at each node.
	values := make([]float64, steps+1)
	for j := 0; j <= steps; j++ {
		ST := S * math.Pow(u, float64(2*j-steps))
		values[j] = intrinsic(ST, K, spec.Type)
	}

	// Backward induction.
	for step := steps - 1; step >= 0; step-- {
		for j := 0; j <= step; j++ {
			hold := discount * (p*values[j+1] + (1-p)*values[j])
			if spec.Style == American {
				ST := S * math.Pow(u, float64(2*j-step))
				ex := intrinsic(ST, K, spec.Type)
				if ex > hold {
					hold = ex
				}
			}
			values[j] = hold
		}
	}
	return values[0]
}

// Greeks returns the option Greeks computed by central finite differences
// of the binomial price. The same step count is used for all bumps.
//
// Bumps:
//   - Delta, Gamma: spot ± hS (hS = 0.01·S, or 0.01 if S==0)
//   - Vega:         volatility ± hV (hV = 0.01)
//   - Theta:        T ± hT (hT = 1/365 year)
//   - Rho:          r ± hR (hR = 0.0001)
//
// Output scaling matches the Greeks struct convention (Vega per 1%,
// Theta per day, Rho per 1%).
func (b *Binomial) Greeks(spec OptionSpec, steps int) (Greeks, error) {
	if err := spec.Validate(); err != nil {
		return Greeks{}, err
	}
	if steps <= 0 {
		steps = DefaultBinomialSteps
	}

	// Use a smaller bump when spot is tiny to avoid negative spot prices.
	hS := 0.01 * spec.Spot
	if spec.Spot == 0 {
		hS = 0.01
	}
	hV := 0.01 // 1% absolute vol bump
	hT := 1.0 / 365.0
	hR := 0.0001 // 1bp rate bump

	// --- Delta & Gamma via spot bumps ---
	upSpec, dnSpec := spec, spec
	upSpec.Spot = spec.Spot + hS
	dnSpec.Spot = spec.Spot - hS
	if dnSpec.Spot < 0 {
		dnSpec.Spot = 0
	}
	pUp := b.priceTree(upSpec, steps)
	pDn := b.priceTree(dnSpec, steps)
	pMid := b.priceTree(spec, steps)

	delta := (pUp - pDn) / (2 * hS)
	// Guard against division by zero when S==0 (then hS=0.01, fine).
	gamma := (pUp - 2*pMid + pDn) / (hS * hS)

	// --- Vega via vol bumps ---
	vUpSpec, vDnSpec := spec, spec
	vUpSpec.Volatility = spec.Volatility + hV
	vDnSpec.Volatility = math.Max(spec.Volatility-hV, 0)
	vUp := b.priceTree(vUpSpec, steps)
	vDn := b.priceTree(vDnSpec, steps)
	vegaRaw := (vUp - vDn) / (2 * hV)

	// --- Rho via rate bumps ---
	rUpSpec, rDnSpec := spec, spec
	rUpSpec.RiskFreeRate = spec.RiskFreeRate + hR
	rDnSpec.RiskFreeRate = spec.RiskFreeRate - hR
	rUp := b.priceTree(rUpSpec, steps)
	rDn := b.priceTree(rDnSpec, steps)
	rhoRaw := (rUp - rDn) / (2 * hR)

	// --- Theta via time bumps ---
	// Theta = ∂V/∂t = -∂V/∂T. We bump T both ways when possible; if T is
	// very small we fall back to a one-sided difference to avoid T<0.
	var thetaRaw float64
	if spec.TimeToExpiry > hT {
		tUpSpec, tDnSpec := spec, spec
		tUpSpec.TimeToExpiry = spec.TimeToExpiry + hT
		tDnSpec.TimeToExpiry = spec.TimeToExpiry - hT
		tUp := b.priceTree(tUpSpec, steps)
		tDn := b.priceTree(tDnSpec, steps)
		// ∂V/∂T = (V(T+h) - V(T-h)) / (2h); theta = -∂V/∂T
		thetaRaw = -(tUp - tDn) / (2 * hT)
	} else {
		// One-sided: only bump forward.
		tUpSpec := spec
		tUpSpec.TimeToExpiry = spec.TimeToExpiry + hT
		tUp := b.priceTree(tUpSpec, steps)
		thetaRaw = -(tUp - pMid) / hT
	}

	return Greeks{
		Delta: delta,
		Gamma: gamma,
		Vega:  vegaRaw / 100, // per 1% vol
		Theta: thetaRaw / 365, // per day
		Rho:   rhoRaw / 100,   // per 1% rate
	}, nil
}
