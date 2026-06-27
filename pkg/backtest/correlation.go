package backtest

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// minEquityPoints is the minimum number of data points each equity
// curve must have to produce a statistically meaningful correlation.
// After converting to returns the series has minEquityPoints-1 points,
// which is well above the Pearson minimum of 2.
const minEquityPoints = 30

// CorrelationMatrix holds pairwise strategy return correlations.
type CorrelationMatrix struct {
	Strategies   []string
	Correlations [][]float64 // NxN correlation matrix
}

// CalculateStrategyCorrelation computes pairwise Pearson correlation
// of strategy returns from multiple backtest equity curves.
//
// Input: map[strategyName][]equityCurve, where each equity curve is a
// series of portfolio values (e.g. total equity at each bar). Each
// curve is converted to simple daily returns internally:
//
//	r[i] = (eq[i] - eq[i-1]) / eq[i-1]
//
// Validation:
//   - at least 2 strategies
//   - each equity curve must have at least minEquityPoints (30) data points
//
// Return series are truncated to the minimum length across strategies
// so that every pair has equal length for Pearson correlation. The
// result is an NxN symmetric matrix with 1.0 on the diagonal.
// Strategy names are sorted alphabetically for deterministic ordering.
func CalculateStrategyCorrelation(equityCurves map[string][]float64) (*CorrelationMatrix, error) {
	if len(equityCurves) < 2 {
		return nil, errors.New("correlation requires at least 2 strategies")
	}

	// Deterministic strategy ordering (sorted by name).
	strategies := make([]string, 0, len(equityCurves))
	for name := range equityCurves {
		strategies = append(strategies, name)
	}
	sort.Strings(strategies)

	// Convert each equity curve to daily returns with validation.
	returnsByStrategy := make(map[string][]float64, len(strategies))
	minLen := -1
	for _, name := range strategies {
		curve := equityCurves[name]
		if len(curve) < minEquityPoints {
			return nil, fmt.Errorf("strategy %q has %d points, need at least %d", name, len(curve), minEquityPoints)
		}
		rets := equityToReturns(curve)
		returnsByStrategy[name] = rets
		if minLen < 0 || len(rets) < minLen {
			minLen = len(rets)
		}
	}

	n := len(strategies)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
	}

	for i := 0; i < n; i++ {
		// Diagonal: a strategy is perfectly correlated with itself.
		matrix[i][i] = 1.0
		for j := i + 1; j < n; j++ {
			xi := returnsByStrategy[strategies[i]][:minLen]
			xj := returnsByStrategy[strategies[j]][:minLen]
			c := statistics.Pearson(xi, xj)
			matrix[i][j] = c
			matrix[j][i] = c
		}
	}

	return &CorrelationMatrix{
		Strategies:   strategies,
		Correlations: matrix,
	}, nil
}

// equityToReturns converts an equity curve (portfolio values) to simple
// returns: r[i] = (eq[i+1] - eq[i]) / eq[i]. A zero previous value
// yields a 0 return to avoid division by zero. Returns nil if the
// input has fewer than 2 points.
func equityToReturns(equity []float64) []float64 {
	if len(equity) < 2 {
		return nil
	}
	rets := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		prev := equity[i-1]
		if prev == 0 {
			rets = append(rets, 0)
			continue
		}
		rets = append(rets, (equity[i]-prev)/prev)
	}
	return rets
}
