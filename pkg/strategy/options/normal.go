package options

import "math"

// sqrt2Pi is the constant √(2π) used by the normal density.
const sqrt2Pi = 2.5066282746310005024157652848110452530069867406099

// NormPDF returns the standard normal probability density function
// evaluated at x:
//
//	NormPDF(x) = (1/√(2π)) · e^(-x²/2)
func NormPDF(x float64) float64 {
	return math.Exp(-x*x/2) / sqrt2Pi
}

// NormCDF returns the standard normal cumulative distribution function
// evaluated at x using the Abramowitz & Stegun (1965) approximation 7.1.26,
// which has an absolute error below 7.5e-8 across the entire real line.
//
//	For x >= 0:
//	  N(x) = 1 - φ(x)·(a1·k + a2·k² + a3·k³ + a4·k⁴ + a5·k⁵)
//	  where k = 1 / (1 + p·x), p = 0.2316419
//	For x < 0 we use the symmetry N(x) = 1 - N(-x).
func NormCDF(x float64) float64 {
	// Constants from A&S 7.1.26.
	const (
		p  = 0.2316419
		a1 = 0.319381530
		a2 = -0.356563782
		a3 = 1.781477937
		a4 = -1.821255978
		a5 = 1.330274429
	)

	// Tail approximation: for |x| very large the polynomial underflows;
	// clamp to the asymptotic limits to avoid spurious NaNs.
	switch {
	case x < -10:
		return 0
	case x > 10:
		return 1
	}

	absX := math.Abs(x)
	k := 1 / (1 + p*absX)
	poly := a1*k + a2*k*k + a3*k*k*k + a4*k*k*k*k + a5*k*k*k*k*k
	tail := NormPDF(absX) * poly

	if x >= 0 {
		return 1 - tail
	}
	return tail
}
