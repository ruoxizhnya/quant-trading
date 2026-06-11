package statistics

import (
	"math"
	"sort"
)

// Percentile returns the p-th percentile of values using linear
// interpolation. `p` must be in [0, 1]; values outside the range
// are clamped. Returns 0 for an empty slice.
//
// Implementation matches the historical value_momentum strategy
// helper (the canonical behavior) so migration is bit-for-bit
// equivalent:
//
//	index = p * (n-1)
//	lower, upper = floor(index), ceil(index)
//	if lower == upper: return sorted[lower]
//	else:             return sorted[lower] * (1-frac) + sorted[upper] * frac
//
// The slice is copied before sorting — the input is never mutated.
func Percentile(values []float64, p float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	index := p * float64(n-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}
	frac := index - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// Median is a convenience wrapper for the 50th percentile.
func Median(values []float64) float64 {
	return Percentile(values, 0.5)
}

// Rank returns the 1-based ranks of each element in values, where 1
// is the smallest. Ties receive the average rank (a.k.a. "fractional
// ranking" or "competition ranking"). NaN values are dropped from
// the ranking and assigned rank 0 in the output, so the caller can
// filter them downstream.
//
// The input is **not** mutated. The output has the same length as
// the input.
func Rank(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return nil
	}

	type pair struct {
		value float64
		idx   int
	}
	ps := make([]pair, n)
	for i, v := range values {
		ps[i] = pair{value: v, idx: i}
	}
	sort.Slice(ps, func(i, j int) bool {
		return ps[i].value < ps[j].value
	})

	ranks := make([]float64, n)
	i := 0
	// nonNaNRank is the 1-based rank counter that increments only
	// for finite values. This keeps ranks dense (1, 2, 3, …) even
	// when NaN entries are interspersed in the sort order.
	nonNaNRank := 1.0
	for i < n {
		if math.IsNaN(ps[i].value) {
			// Skip NaN — keep rank 0 (callers can use that as
			// a "missing" signal). nonNaNRank does NOT advance.
			i++
			continue
		}
		// Find the end of this tie group.
		j := i
		for j < n && ps[j].value == ps[i].value && !math.IsNaN(ps[j].value) {
			j++
		}
		// Average rank in 1-based non-NaN ranking: center of
		// [nonNaNRank, nonNaNRank + count - 1].
		count := j - i
		avgRank := nonNaNRank + float64(count-1)/2.0
		for k := i; k < j; k++ {
			ranks[ps[k].idx] = avgRank
		}
		nonNaNRank += float64(count)
		i = j
	}
	return ranks
}

// PercentileRank normalizes the values to the [0, 1] range based on
// rank. NaN values become 0. This is a convenience for
// cross-sectional ranking pipelines (see pkg/data/factor.go).
func PercentileRank(values []float64) []float64 {
	ranks := Rank(values)
	if len(ranks) == 0 {
		return nil
	}
	out := make([]float64, len(ranks))
	denom := float64(len(ranks) - 1)
	if denom <= 0 {
		// All-NaN or single-element input — return zeros.
		return out
	}
	for i, r := range ranks {
		if r == 0 {
			out[i] = 0
		} else {
			out[i] = (r - 1) / denom
		}
	}
	return out
}
