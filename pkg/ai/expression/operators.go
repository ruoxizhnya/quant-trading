package expression

import (
	"math"
	"sort"
)

// Time-series operators

func tsMean(data []float64, window int) []float64 {
	if window <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		sum := 0.0
		count := 0
		for j := 0; j < window; j++ {
			if !math.IsNaN(data[i-j]) {
				sum += data[i-j]
				count++
			}
		}
		if count > 0 {
			result[i] = sum / float64(count)
		} else {
			result[i] = math.NaN()
		}
	}
	return result
}

func tsStd(data []float64, window int) []float64 {
	if window <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		mean := tsMean(data[i-window+1:i+1], window)[window-1]
		if math.IsNaN(mean) {
			result[i] = math.NaN()
			continue
		}
		sumSq := 0.0
		count := 0
		for j := 0; j < window; j++ {
			if !math.IsNaN(data[i-j]) {
				diff := data[i-j] - mean
				sumSq += diff * diff
				count++
			}
		}
		if count > 1 {
			result[i] = math.Sqrt(sumSq / float64(count-1))
		} else {
			result[i] = math.NaN()
		}
	}
	return result
}

func tsSum(data []float64, window int) []float64 {
	if window <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		sum := 0.0
		for j := 0; j < window; j++ {
			if !math.IsNaN(data[i-j]) {
				sum += data[i-j]
			}
		}
		result[i] = sum
	}
	return result
}

func tsMax(data []float64, window int) []float64 {
	if window <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		maxVal := data[i]
		for j := 1; j < window; j++ {
			if !math.IsNaN(data[i-j]) && data[i-j] > maxVal {
				maxVal = data[i-j]
			}
		}
		result[i] = maxVal
	}
	return result
}

func tsMin(data []float64, window int) []float64 {
	if window <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		minVal := data[i]
		for j := 1; j < window; j++ {
			if !math.IsNaN(data[i-j]) && data[i-j] < minVal {
				minVal = data[i-j]
			}
		}
		result[i] = minVal
	}
	return result
}

func tsDelay(data []float64, periods int) []float64 {
	if periods <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < periods {
			result[i] = math.NaN()
		} else {
			result[i] = data[i-periods]
		}
	}
	return result
}

func tsDelta(data []float64, periods int) []float64 {
	if periods <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < periods {
			result[i] = math.NaN()
		} else {
			result[i] = data[i] - data[i-periods]
		}
	}
	return result
}

func tsPctChange(data []float64, periods int) []float64 {
	if periods <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < periods || data[i-periods] == 0 {
			result[i] = math.NaN()
		} else {
			result[i] = (data[i] - data[i-periods]) / data[i-periods]
		}
	}
	return result
}

func tsCorr(data1, data2 []float64, window int) []float64 {
	if window <= 0 || len(data1) == 0 || len(data2) == 0 || len(data1) != len(data2) {
		return []float64{}
	}
	result := make([]float64, len(data1))
	for i := range data1 {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		// Extract window data
		x := make([]float64, 0, window)
		y := make([]float64, 0, window)
		for j := 0; j < window; j++ {
			if !math.IsNaN(data1[i-j]) && !math.IsNaN(data2[i-j]) {
				x = append(x, data1[i-j])
				y = append(y, data2[i-j])
			}
		}
		if len(x) < 2 {
			result[i] = math.NaN()
			continue
		}
		result[i] = correlation(x, y)
	}
	return result
}

func tsRank(data []float64, window int) []float64 {
	if window <= 0 || len(data) == 0 {
		return []float64{}
	}
	result := make([]float64, len(data))
	for i := range data {
		if i < window-1 {
			result[i] = math.NaN()
			continue
		}
		// Get window values
		windowVals := make([]float64, 0, window)
		for j := 0; j < window; j++ {
			if !math.IsNaN(data[i-j]) {
				windowVals = append(windowVals, data[i-j])
			}
		}
		if len(windowVals) == 0 {
			result[i] = math.NaN()
			continue
		}
		// Rank current value within window
		rank := 0
		for _, v := range windowVals {
			if data[i] > v {
				rank++
			}
		}
		result[i] = float64(rank) / float64(len(windowVals)-1)
	}
	return result
}

// Cross-sectional operators

func csRank(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return []float64{}
	}

	// Create indices and sort by value
	type pair struct {
		idx   int
		value float64
	}
	pairs := make([]pair, 0, n)
	for i, v := range values {
		if !math.IsNaN(v) {
			pairs = append(pairs, pair{idx: i, value: v})
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].value < pairs[j].value
	})

	result := make([]float64, n)
	for i := range result {
		result[i] = math.NaN()
	}

	for rank, p := range pairs {
		result[p.idx] = float64(rank) / float64(len(pairs)-1)
	}

	return result
}

func csZScore(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return []float64{}
	}

	// Calculate mean
	sum := 0.0
	count := 0
	for _, v := range values {
		if !math.IsNaN(v) {
			sum += v
			count++
		}
	}
	if count == 0 {
		result := make([]float64, n)
		for i := range result {
			result[i] = math.NaN()
		}
		return result
	}
	mean := sum / float64(count)

	// Calculate std
	sumSq := 0.0
	for _, v := range values {
		if !math.IsNaN(v) {
			diff := v - mean
			sumSq += diff * diff
		}
	}
	std := math.Sqrt(sumSq / float64(count))

	result := make([]float64, n)
	for i, v := range values {
		if math.IsNaN(v) || std == 0 {
			result[i] = math.NaN()
		} else {
			result[i] = (v - mean) / std
		}
	}
	return result
}

func csPercentile(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return []float64{}
	}

	// Sort non-NaN values
	valid := make([]float64, 0, n)
	for _, v := range values {
		if !math.IsNaN(v) {
			valid = append(valid, v)
		}
	}
	if len(valid) == 0 {
		result := make([]float64, n)
		for i := range result {
			result[i] = math.NaN()
		}
		return result
	}

	sort.Float64s(valid)

	result := make([]float64, n)
	for i, v := range values {
		if math.IsNaN(v) {
			result[i] = math.NaN()
			continue
		}
		// Find percentile
		count := 0
		for _, val := range valid {
			if val <= v {
				count++
			}
		}
		result[i] = float64(count) / float64(len(valid))
	}
	return result
}

// Utility functions

func correlation(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return math.NaN()
	}

	n := float64(len(x))
	sumX, sumY, sumXY, sumX2, sumY2 := 0.0, 0.0, 0.0, 0.0, 0.0

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
