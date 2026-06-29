package statistics

import "math"

// Slope returns the slope of the ordinary least-squares line
// y = slope * x + intercept, where x is implicitly the index
// [0, 1, 2, ..., n-1] of `y`.
//
// This matches the formula used by pkg/risk/regime.go:228-233
// (slope of normalized price). The denominator guard mirrors the
// upstream code (returns 0.0 when all x are equal, which is
// mathematically impossible here but kept for safety).
//
// Returns 0 for slices with fewer than 2 elements.
func Slope(y []float64) float64 {
	n := len(y)
	if n < 2 {
		return 0
	}
	nf := float64(n)
	var sumX, sumY, sumXY, sumX2 float64
	for i, v := range y {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (nf*sumXY - sumX*sumY) / denom
}

// LinearRegression fits y = slope*x + intercept using OLS, where x
// is implicitly [0, 1, ..., n-1].
//
// Returns (slope, intercept, rSquared):
//   - slope: same as Slope(y)
//   - intercept: y_mean - slope * x_mean
//   - rSquared: coefficient of determination; 0 if R^2 is
//     negative (e.g. perfectly anti-correlated noise)
//
// Returns (0, 0, 0) for slices with fewer than 2 elements.
func LinearRegression(y []float64) (slope, intercept, rSquared float64) {
	n := len(y)
	if n < 2 {
		return 0, 0, 0
	}
	nf := float64(n)
	var sumX, sumY, sumXY, sumX2 float64
	for i, v := range y {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, 0, 0
	}
	slope = (nf*sumXY - sumX*sumY) / denom
	xMean := sumX / nf
	yMean := sumY / nf
	intercept = yMean - slope*xMean

	// R^2 = 1 - SS_res / SS_tot
	ssTot := 0.0
	ssRes := 0.0
	for i, v := range y {
		predicted := slope*float64(i) + intercept
		ssRes += (v - predicted) * (v - predicted)
		ssTot += (v - yMean) * (v - yMean)
	}
	if ssTot == 0 {
		return slope, intercept, 0
	}
	rSquared = 1 - ssRes/ssTot
	if rSquared < 0 {
		rSquared = 0
	}
	return slope, intercept, rSquared
}

// ZScore normalizes values to (v - mean) / stddev using the
// population standard deviation. Returns 0 for slices shorter than
// 2 (matches the convention in the rest of the package).
//
// NaN is propagated naturally: if any input is NaN, the result is
// NaN for that index.
func ZScore(values []float64) []float64 {
	n := len(values)
	if n < 2 {
		out := make([]float64, n)
		return out
	}
	m := Mean(values)
	s := PopulationStdDev(values)
	out := make([]float64, n)
	if s == 0 {
		// All values identical; z-score is 0.
		return out
	}
	for i, v := range values {
		out[i] = (v - m) / s
	}
	return out
}

// LogReturns converts a price series into log-returns:
//
//	r[i] = ln(prices[i+1] / prices[i])
//
// The result has len(prices) - 1 elements. NaN is returned for any
// transition where EITHER price is non-finite (NaN, Inf) or
// non-positive (the log of a non-positive number is undefined).
// Returns nil for slices with fewer than 2 elements.
func LogReturns(prices []float64) []float64 {
	n := len(prices)
	if n < 2 {
		return nil
	}
	out := make([]float64, n-1)
	for i := 1; i < n; i++ {
		prev := prices[i-1]
		curr := prices[i]
		if !isFinitePositive(prev) || !isFinitePositive(curr) {
			out[i-1] = math.NaN()
			continue
		}
		out[i-1] = math.Log(curr / prev)
	}
	return out
}

// isFinitePositive returns true iff v is a finite float > 0.
// Used by LogReturns to guard the log() call.
func isFinitePositive(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

// AnnualizeVolatility scales a daily (or per-period) volatility to
// an annual figure. `periodsPerYear` is typically 252 for daily
// stock data or 365 for crypto.
func AnnualizeVolatility(dailyVol float64, periodsPerYear int) float64 {
	if periodsPerYear <= 0 {
		return 0
	}
	return dailyVol * math.Sqrt(float64(periodsPerYear))
}
