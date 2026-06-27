package data

import (
	"errors"
	"fmt"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// FactorDecayResult measures factor predictive power over multiple horizons.
type FactorDecayResult struct {
	Factor   string
	Horizons []int     // e.g., [21, 63, 126] for 1M/3M/6M
	ICs      []float64 // IC at each horizon
	Decays   []float64 // decay ratio relative to the shortest horizon's IC
}

// CalculateFactorDecay computes IC at multiple forward-return horizons.
//
// Inputs:
//   - factorScores: cross-sectional factor score for each stock, measured
//     at the ranking date (day 0).
//   - returns: returns[day][stock] = daily return of stock `stock` on day
//     `day`. Day 0 is the first trading day AFTER the factor measurement
//     date, so forward returns never overlap the ranking signal.
//   - horizons: forward-return horizons in trading days, e.g. [21, 63, 126].
//
// For each horizon h the forward return of stock s is the cumulative
// return over the first h days:
//
//	forwardReturn[s] = ∏_{d=0}^{h-1} (1 + returns[d][s]) - 1
//
// and the IC is the Spearman rank correlation between factorScores and
// the forward returns:
//
//	IC_h = Spearman(factorScores, forwardReturn_h)
//
// Decays[i] = ICs[i] / ICs[0], i.e. the decay ratio relative to the
// shortest horizon's IC. If ICs[0] is zero or NaN every decay entry is
// NaN because the ratio is undefined.
//
// The horizons slice is copied into the result to avoid aliasing the
// caller's slice.
func CalculateFactorDecay(factorScores []float64, returns [][]float64, horizons []int) (*FactorDecayResult, error) {
	if len(factorScores) < 2 {
		return nil, errors.New("factor decay requires at least 2 stocks")
	}
	if len(horizons) == 0 {
		return nil, errors.New("factor decay requires at least 1 horizon")
	}

	maxHorizon := 0
	for _, h := range horizons {
		if h <= 0 {
			return nil, fmt.Errorf("horizon must be positive, got %d", h)
		}
		if h > maxHorizon {
			maxHorizon = h
		}
	}
	if len(returns) < maxHorizon {
		return nil, fmt.Errorf("not enough return data: have %d days, need %d for the largest horizon", len(returns), maxHorizon)
	}

	nStocks := len(factorScores)
	for d := 0; d < maxHorizon; d++ {
		if len(returns[d]) != nStocks {
			return nil, fmt.Errorf("returns[%d] has %d stocks, expected %d", d, len(returns[d]), nStocks)
		}
	}

	ics := make([]float64, len(horizons))
	for i, h := range horizons {
		forwardReturns := computeForwardReturns(returns, h, nStocks)
		ics[i] = statistics.Spearman(factorScores, forwardReturns)
	}

	decays := make([]float64, len(horizons))
	if math.IsNaN(ics[0]) || ics[0] == 0 {
		// The shortest-horizon IC is degenerate (zero variance factor
		// or zero IC), so every decay ratio is undefined.
		for i := range decays {
			decays[i] = math.NaN()
		}
	} else {
		for i := range decays {
			decays[i] = ics[i] / ics[0]
		}
	}

	horizonsCopy := make([]int, len(horizons))
	copy(horizonsCopy, horizons)

	return &FactorDecayResult{
		Factor:   "",
		Horizons: horizonsCopy,
		ICs:      ics,
		Decays:   decays,
	}, nil
}

// computeForwardReturns computes the cumulative return over the first
// `horizon` days for each stock:
//
//	forwardReturn[s] = ∏_{d=0}^{horizon-1} (1 + returns[d][s]) - 1
func computeForwardReturns(returns [][]float64, horizon, nStocks int) []float64 {
	forward := make([]float64, nStocks)
	for s := 0; s < nStocks; s++ {
		prod := 1.0
		for d := 0; d < horizon; d++ {
			prod *= (1 + returns[d][s])
		}
		forward[s] = prod - 1
	}
	return forward
}
