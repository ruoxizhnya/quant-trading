package metrics

import (
	"math"
	"sort"
)

// ICCalculator computes Information Coefficient metrics.
type ICCalculator struct{}

// NewICCalculator creates a new ICCalculator.
func NewICCalculator() *ICCalculator {
	return &ICCalculator{}
}

// ICResult holds the result of IC calculation.
type ICResult struct {
	IC          float64   `json:"ic"`
	RankIC      float64   `json:"rank_ic"`
	ICMean      float64   `json:"ic_mean"`
	ICStd       float64   `json:"ic_std"`
	IR          float64   `json:"ir"`
	ICWinRate   float64   `json:"ic_win_rate"`
	ICTimeseries []float64 `json:"ic_timeseries"`
}

// ComputeIC calculates the Pearson correlation between factor values and forward returns.
func (c *ICCalculator) ComputeIC(factorValues, forwardReturns []float64) *ICResult {
	if len(factorValues) == 0 || len(factorValues) != len(forwardReturns) {
		return &ICResult{}
	}

	// Remove NaN pairs
	var cleanF, cleanR []float64
	for i := range factorValues {
		if !math.IsNaN(factorValues[i]) && !math.IsNaN(forwardReturns[i]) {
			cleanF = append(cleanF, factorValues[i])
			cleanR = append(cleanR, forwardReturns[i])
		}
	}

	if len(cleanF) < 2 {
		return &ICResult{}
	}

	ic := pearsonCorrelation(cleanF, cleanR)
	rankIC := spearmanCorrelation(cleanF, cleanR)

	return &ICResult{
		IC:     ic,
		RankIC: rankIC,
	}
}

// ComputeICTimeseries calculates IC for each time period.
func (c *ICCalculator) ComputeICTimeseries(factorValuesByDate, forwardReturnsByDate map[string][]float64) *ICResult {
	var icSeries []float64
	var validDates []string

	for date := range factorValuesByDate {
		if _, ok := forwardReturnsByDate[date]; !ok {
			continue
		}

		fVals := factorValuesByDate[date]
		rVals := forwardReturnsByDate[date]

		if len(fVals) != len(rVals) || len(fVals) < 2 {
			continue
		}

		// Clean NaN
		var cleanF, cleanR []float64
		for i := range fVals {
			if !math.IsNaN(fVals[i]) && !math.IsNaN(rVals[i]) {
				cleanF = append(cleanF, fVals[i])
				cleanR = append(cleanR, rVals[i])
			}
		}

		if len(cleanF) < 2 {
			continue
		}

		ic := pearsonCorrelation(cleanF, cleanR)
		if !math.IsNaN(ic) {
			icSeries = append(icSeries, ic)
			validDates = append(validDates, date)
		}
	}

	if len(icSeries) == 0 {
		return &ICResult{}
	}

	icMean := mean(icSeries)
	icStd := stdDev(icSeries)
	ir := 0.0
	if icStd > 0 {
		ir = icMean / icStd
	}

	winRate := 0.0
	wins := 0
	for _, ic := range icSeries {
		if ic > 0 {
			wins++
		}
	}
	winRate = float64(wins) / float64(len(icSeries))

	return &ICResult{
		IC:           icMean,
		ICMean:       icMean,
		ICStd:        icStd,
		IR:           ir,
		ICWinRate:    winRate,
		ICTimeseries: icSeries,
	}
}

// ComputeIR calculates the Information Ratio (IC mean / IC std).
func (c *ICCalculator) ComputeIR(icSeries []float64) float64 {
	if len(icSeries) == 0 {
		return 0
	}
	mean := mean(icSeries)
	std := stdDev(icSeries)
	// Use small epsilon to handle floating point precision issues
	if std < 1e-10 {
		return 0
	}
	return mean / std
}

// pearsonCorrelation computes Pearson correlation coefficient.
func pearsonCorrelation(x, y []float64) float64 {
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

// spearmanCorrelation computes Spearman rank correlation.
func spearmanCorrelation(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return math.NaN()
	}

	rx := rank(x)
	ry := rank(y)

	return pearsonCorrelation(rx, ry)
}

// rank returns the rank of each element (1-based, ties get average rank).
func rank(data []float64) []float64 {
	type pair struct {
		value float64
		idx   int
	}

	pairs := make([]pair, len(data))
	for i, v := range data {
		pairs[i] = pair{value: v, idx: i}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].value < pairs[j].value
	})

	ranks := make([]float64, len(data))
	i := 0
	for i < len(pairs) {
		j := i
		for j < len(pairs)-1 && pairs[j].value == pairs[j+1].value {
			j++
		}

		avgRank := float64(i+j+2) / 2.0 // 1-based average rank
		for k := i; k <= j; k++ {
			ranks[pairs[k].idx] = avgRank
		}
		i = j + 1
	}

	return ranks
}

// mean calculates the arithmetic mean.
func mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	var sum float64
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

// stdDev calculates the sample standard deviation.
func stdDev(data []float64) float64 {
	if len(data) < 2 {
		return 0
	}
	m := mean(data)
	var sumSq float64
	for _, v := range data {
		diff := v - m
		sumSq += diff * diff
	}
	return math.Sqrt(sumSq / float64(len(data)-1))
}
