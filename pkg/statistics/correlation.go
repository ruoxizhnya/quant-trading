package statistics

import "math"

// Pearson computes the Pearson product-moment correlation
// coefficient between two equal-length slices x and y.
//
// Returns NaN when:
//   - len(x) != len(y)
//   - len(x) < 2
//   - either variable has zero variance (denominator is zero)
//
// This matches the behavior of pkg/ai/metrics/pearsonCorrelation
// so the migration is a 1:1 replacement.
func Pearson(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return math.NaN()
	}
	n := float64(len(x))
	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := range x {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumX2 += x[i] * x[i]
		sumY2 += y[i] * y[i]
	}
	numerator := n*sumXY - sumX*sumY
	denominator := math.Sqrt((n*sumX2 - sumX*sumX) * (n*sumY2 - sumY*sumY))
	if denominator == 0 {
		return math.NaN()
	}
	return numerator / denominator
}

// Spearman computes the Spearman rank correlation coefficient
// between x and y. Internally it converts both inputs to their
// ranks and delegates to Pearson. Ties get the average rank; the
// resulting correlation is therefore the standard Spearman
// (sometimes called "Spearman rho with tie correction").
//
// Returns NaN under the same conditions as Pearson.
func Spearman(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return math.NaN()
	}
	return Pearson(Rank(x), Rank(y))
}
