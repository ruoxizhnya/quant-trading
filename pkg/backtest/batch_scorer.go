package backtest

import (
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Scorer computes batch result scores including overfitting and stability metrics.
type Scorer struct {
	// Weights for composite score calculation
	sharpeWeight    float64
	returnWeight    float64
	drawdownWeight  float64
	winRateWeight   float64
	tradesWeight    float64
	calmarWeight    float64
	overfitWeight   float64
	stabilityWeight float64
}

// NewScorer creates a Scorer with default weights.
func NewScorer() *Scorer {
	return &Scorer{
		sharpeWeight:    0.25,
		returnWeight:    0.15,
		drawdownWeight:  0.15,
		winRateWeight:   0.10,
		tradesWeight:    0.05,
		calmarWeight:    0.10,
		overfitWeight:   0.15,
		stabilityWeight: 0.05,
	}
}

// ScoreResult computes a BatchScore from a single batch result.
// It uses walk-forward metrics when available, otherwise falls back to heuristics.
func (s *Scorer) ScoreResult(r *BatchResult) *BatchScore {
	if r == nil || r.Result == nil {
		return nil
	}

	score := &BatchScore{}
	br := r.Result

	// Overfit and stability scores from walk-forward if available
	overfit := 0.5
	stability := 0.5
	if r.WalkForward != nil {
		overfit = r.WalkForward.OverfitScore
		stability = r.WalkForward.StabilityScore
	} else {
		// Fallback heuristic: estimate overfit from train/test gap if we had it,
		// otherwise use a moderate default indicating unknown.
		overfit = estimateOverfitHeuristic(br)
		stability = estimateStabilityHeuristic(br)
	}

	// Normalize individual metrics to [0,1]
	sharpeNorm := normalizeMetric(br.SharpeRatio, -1, 3)
	returnNorm := normalizeMetric(br.AnnualReturn, -0.3, 0.5)
	ddNorm := normalizeMetric(-br.MaxDrawdown, -0.5, -0.05)
	winRateNorm := normalizeMetric(br.WinRate, 0.3, 0.7)
	tradesNorm := normalizeMetric(math.Log10(float64(br.TotalTrades+1)), 0.5, 2.5)
	calmarNorm := normalizeMetric(br.CalmarRatio, 0, 5)

	// Raw performance component (before robustness adjustments)
	rawPerformance := sharpeNorm*s.sharpeWeight +
		returnNorm*s.returnWeight +
		ddNorm*s.drawdownWeight +
		winRateNorm*s.winRateWeight +
		tradesNorm*s.tradesWeight +
		calmarNorm*s.calmarWeight

	// Robustness adjustments
	robustnessPenalty := overfit * s.overfitWeight
	stabilityBonus := stability * s.stabilityWeight

	composite := rawPerformance - robustnessPenalty + stabilityBonus
	composite = math.Max(0, math.Min(composite, 1))

	score.OverfitScore = overfit
	score.StabilityScore = stability
	score.CompositeScore = composite
	score.Grade = gradeFromScore(composite)

	return score
}

// ScoreBatch computes scores for all completed results in a batch report
// and assigns ranks based on composite score descending.
func (s *Scorer) ScoreBatch(report *BatchReport) {
	if report == nil {
		return
	}

	scores := make([]*BatchScore, 0, len(report.Results))
	for _, r := range report.Results {
		if r.Status != "completed" || r.Result == nil {
			continue
		}
		score := s.ScoreResult(r)
		r.Score = score
		scores = append(scores, score)
	}

	// Assign ranks by composite score descending
	type indexedScore struct {
		score *BatchScore
		idx   int
	}
	indexed := make([]indexedScore, 0, len(scores))
	for i, sc := range scores {
		indexed = append(indexed, indexedScore{score: sc, idx: i})
	}

	// Simple bubble sort by composite score descending to assign ranks
	for i := 0; i < len(indexed); i++ {
		for j := i + 1; j < len(indexed); j++ {
			if indexed[j].score.CompositeScore > indexed[i].score.CompositeScore {
				indexed[i], indexed[j] = indexed[j], indexed[i]
			}
		}
	}
	for i, is := range indexed {
		is.score.Rank = i + 1
	}
}

// estimateOverfitHeuristic provides a rough overfit estimate when walk-forward data is unavailable.
// It penalizes extremely high Sharpe ratios with very few trades as potentially overfit.
func estimateOverfitHeuristic(br *domain.BacktestResult) float64 {
	if br == nil {
		return 0.5
	}
	overfit := 0.3 // base unknown

	// High sharpe with very few trades is suspicious
	if br.SharpeRatio > 2.5 && br.TotalTrades < 10 {
		overfit += 0.4
	} else if br.SharpeRatio > 3.0 && br.TotalTrades < 20 {
		overfit += 0.3
	}

	// Extremely high returns with low drawdown may be overfit
	if br.AnnualReturn > 0.8 && br.MaxDrawdown > -0.05 {
		overfit += 0.2
	}

	return math.Min(overfit, 1.0)
}

// estimateStabilityHeuristic provides a rough stability estimate when walk-forward data is unavailable.
// It uses trade count and Calmar ratio consistency as proxies.
func estimateStabilityHeuristic(br *domain.BacktestResult) float64 {
	if br == nil {
		return 0.5
	}
	stability := 0.5

	// More trades generally indicate more stable results
	if br.TotalTrades >= 50 {
		stability += 0.15
	} else if br.TotalTrades >= 20 {
		stability += 0.05
	} else if br.TotalTrades < 5 {
		stability -= 0.15
	}

	// Positive Calmar with moderate drawdown suggests stability
	if br.CalmarRatio > 1.0 && br.MaxDrawdown < -0.15 {
		stability += 0.1
	}

	return math.Max(0, math.Min(stability, 1.0))
}
