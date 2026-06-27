package risk

import (
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// VaRRequest holds parameters for Value at Risk calculation.
type VaRRequest struct {
	Returns        []float64 // historical daily returns
	Confidence     float64   // 0.95 or 0.99
	PortfolioValue float64   // total portfolio value in CNY
}

// VaRResult holds VaR and CVaR results.
type VaRResult struct {
	VaR        float64 // Value at Risk (absolute CNY amount, positive = loss)
	CVaR       float64 // Conditional VaR / Expected Shortfall (CNY)
	VaRPct     float64 // VaR as a fraction of portfolio (e.g. 0.023 for 2.3%)
	CVaRPct    float64 // CVaR as a fraction of portfolio
	Confidence float64
	SampleSize int
}

// minVaRSamples is the minimum number of return observations required
// for historical simulation VaR. Below this the tail estimate is too
// noisy to be meaningful.
const minVaRSamples = 30

// CalculateVaR computes historical simulation VaR and CVaR (Expected
// Shortfall) from a series of daily returns.
//
//	VaR  = -percentile(returns, 1-confidence) * portfolioValue
//	CVaR = -mean(returns where returns <= percentile) * portfolioValue
//
// Both VaR and CVaR are returned as positive CNY amounts representing
// potential loss; a negative value indicates the corresponding
// quantile of historical returns was a gain (no estimated loss).
//
// Thread safety: CalculateVaR is a pure function — it reads no
// package-level state and never mutates req.Returns (the slice is
// copied before any use). It is safe for concurrent use, including
// concurrent calls sharing the same input slice.
func CalculateVaR(req VaRRequest) (*VaRResult, error) {
	if len(req.Returns) < minVaRSamples {
		return nil, errors.InvalidInput(
			fmt.Sprintf("insufficient returns for VaR: need at least %d, got %d", minVaRSamples, len(req.Returns)),
			"CalculateVaR",
		)
	}
	if req.Confidence < 0.90 || req.Confidence > 0.999 {
		return nil, errors.InvalidInput(
			fmt.Sprintf("confidence must be in [0.90, 0.999], got %v", req.Confidence),
			"CalculateVaR",
		)
	}
	if req.PortfolioValue <= 0 {
		return nil, errors.InvalidInput(
			fmt.Sprintf("portfolio value must be positive, got %v", req.PortfolioValue),
			"CalculateVaR",
		)
	}

	// Copy returns so the caller's slice is never mutated — required
	// for safe concurrent use and the no-side-effect contract.
	returns := make([]float64, len(req.Returns))
	copy(returns, req.Returns)

	tailProb := 1.0 - req.Confidence
	percentile := statistics.Percentile(returns, tailProb)

	// CVaR = -E[returns | returns <= percentile] (Expected Shortfall).
	// Inclusive `<=` matches the documented contract; with a finite
	// historical sample the boundary observations are part of the tail.
	var tailSum float64
	var tailCount int
	for _, r := range returns {
		if r <= percentile {
			tailSum += r
			tailCount++
		}
	}

	var cvarPerUnit float64
	if tailCount > 0 {
		cvarPerUnit = tailSum / float64(tailCount)
	}

	varPct := -percentile
	cvarPct := -cvarPerUnit

	return &VaRResult{
		VaR:        varPct * req.PortfolioValue,
		CVaR:       cvarPct * req.PortfolioValue,
		VaRPct:     varPct,
		CVaRPct:    cvarPct,
		Confidence: req.Confidence,
		SampleSize: len(returns),
	}, nil
}
