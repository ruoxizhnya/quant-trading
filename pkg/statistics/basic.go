package statistics

import "math"

// Sum returns the arithmetic sum of values. Returns 0 for an empty
// slice.
func Sum(values []float64) float64 {
	var s float64
	for _, v := range values {
		s += v
	}
	return s
}

// Mean returns the arithmetic mean of values. Returns 0 for an empty
// slice. NaN / Inf values are included in the denominator (they
// poison the result) — use MeanFinite if you need to filter them.
func Mean(values []float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	return Sum(values) / float64(n)
}

// MeanFinite returns the mean of the finite (non-NaN, non-Inf)
// subset of values. Returns 0 for an empty slice AND for a slice
// with zero finite values.
func MeanFinite(values []float64) float64 {
	sum, count := sumAndCountFinite(values)
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// VarianceFromMean returns the variance of `values` given the
// pre-computed `mean`.
//
//   - sample=true  → divide by (n-1) (Bessel-corrected, unbiased)
//   - sample=false → divide by n     (population)
//
// Returns 0 for slices shorter than 2 elements. Negative variance
// (caused by catastrophic cancellation) is clamped to 0.
func VarianceFromMean(values []float64, mean float64, sample bool) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}
	var sumSqDiff float64
	for _, v := range values {
		d := v - mean
		sumSqDiff += d * d
	}
	denom := float64(n)
	if sample {
		denom = float64(n - 1)
	}
	v := sumSqDiff / denom
	if v < 0 {
		// Catastrophic cancellation guard. Real variance is
		// non-negative; anything below zero is a floating-point
		// artifact (e.g. Welford's algorithm underflow). Clamp
		// instead of returning -1e-300.
		return 0
	}
	return v
}

// Variance is the convenience wrapper that calls Mean internally.
func Variance(values []float64, sample bool) float64 {
	return VarianceFromMean(values, Mean(values), sample)
}

// SampleVariance returns the unbiased (n-1) variance. Returns 0 for
// slices shorter than 2.
func SampleVariance(values []float64) float64 {
	return Variance(values, true)
}

// PopulationVariance returns the population (n) variance. Returns 0
// for empty slices.
func PopulationVariance(values []float64) float64 {
	return Variance(values, false)
}

// StdDevFromMean is the sqrt of VarianceFromMean. See its docs.
func StdDevFromMean(values []float64, mean float64, sample bool) float64 {
	return math.Sqrt(VarianceFromMean(values, mean, sample))
}

// SampleStdDev returns the unbiased (n-1) standard deviation. Returns
// 0 for slices shorter than 2.
func SampleStdDev(values []float64) float64 {
	return StdDevFromMean(values, Mean(values), true)
}

// PopulationStdDev returns the population (n) standard deviation.
// Returns 0 for empty slices.
func PopulationStdDev(values []float64) float64 {
	return StdDevFromMean(values, Mean(values), false)
}

// sumAndCountFinite is an internal helper that returns the sum and
// count of finite (non-NaN, non-Inf) entries in `values`. It is the
// building block for MeanFinite / finite-aware variance routines.
func sumAndCountFinite(values []float64) (sum float64, count int) {
	for _, v := range values {
		if isFinite(v) {
			sum += v
			count++
		}
	}
	return sum, count
}

// isFinite mirrors math.IsNaN + math.IsInf without pulling both
// branches in.
func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
