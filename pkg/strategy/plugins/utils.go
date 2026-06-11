// Package plugins contains built-in strategy implementations.
package plugins

import (
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// sortOHLCV sorts OHLCV data by date in ascending order.
//
// It creates a copy of the data to avoid modifying the original slice.
//
// Fast path: if the data is already sorted by date ascending (the common case
// for backtest L1 cache, which is pre-sorted in warmCache / InMemoryProvider),
// this returns the input slice unchanged. This avoids one allocation + N*log(N)
// sort comparisons per call — and momentum/mean-reversion strategies call
// sortOHLCV inside their per-day hot path (50 stocks × 240 days = 12,000 calls
// per backtest run). Skipping the copy+sort shaves significant GC pressure
// from the 5s backtest budget.
func sortOHLCV(data []domain.OHLCV) []domain.OHLCV {
	if isSortedByDate(data) {
		return data
	}
	sorted := make([]domain.OHLCV, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.Before(sorted[j].Date)
	})
	return sorted
}

// isSortedByDate returns true if data is sorted by Date in ascending order,
// or has 0/1 elements. O(n) verification on the data.
func isSortedByDate(data []domain.OHLCV) bool {
	if len(data) < 2 {
		return true
	}
	prev := data[0].Date
	for i := 1; i < len(data); i++ {
		cur := data[i].Date
		if cur.Before(prev) {
			return false
		}
		prev = cur
	}
	return true
}

// getLatestPrice extracts the latest closing price from OHLCV data.
// Returns 0 if data is empty.
//
// Fast path: if data is already sorted by date (the common case), just return
// the last element. The previous implementation always allocated a copy and
// sorted — which was a hot allocation site in the per-day strategy loop.
func getLatestPrice(data []domain.OHLCV) float64 {
	if len(data) == 0 {
		return 0
	}
	if isSortedByDate(data) {
		return data[len(data)-1].Close
	}
	sorted := sortOHLCV(data)
	return sorted[len(sorted)-1].Close
}

// calculateMean computes the arithmetic mean of a float64 slice.
// Returns 0 for empty slices.
func calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// calculateStdDev computes the population standard deviation.
// Returns 0 for empty slices or when mean is 0.
func calculateStdDev(values []float64, mean float64) float64 {
	if len(values) == 0 || mean == 0 {
		return 0
	}
	var sumSqDiff float64
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}
	variance := sumSqDiff / float64(len(values))
	if variance < 0 {
		variance = 0
	}
	return variance
}

// calculateCV computes the coefficient of variation (stdDev / mean).
// Returns 0 if mean is 0.
func calculateCV(stdDev, mean float64) float64 {
	if mean == 0 {
		return 0
	}
	return stdDev / mean
}

// extractClosePrices extracts closing prices from a slice of OHLCV.
func extractClosePrices(bars []domain.OHLCV) []float64 {
	prices := make([]float64, len(bars))
	for i, bar := range bars {
		prices[i] = bar.Close
	}
	return prices
}

// isInTopN checks if a symbol is in the top N results.
// results should be a slice of structs with a symbol field.
// This is a generic helper that uses a comparator function.
type symbolRanker interface {
	symbol() string
	score() float64
}

// checkInTopN checks if the given symbol exists in the results slice up to topN items.
func checkInTopN(symbol string, results []string, topN int) bool {
	n := len(results)
	if n > topN {
		n = topN
	}
	for i := 0; i < n; i++ {
		if results[i] == symbol {
			return true
		}
	}
	return false
}

// safeDivide performs division with zero-check.
// Returns 0 if denominator is 0.
func safeDivide(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// clampFloat restricts a float64 value to the range [min, max].
func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// clampInt restricts an int value to the range [min, max].
func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// parseIntParam safely parses an integer parameter from map[string]any.
// Supports float64 (JSON numbers) and int types.
func parseIntParam(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	default:
		return 0, false
	}
}

// parseFloatParam safely parses a float64 parameter from map[string]any.
// Supports float64 and int types.
func parseFloatParam(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	default:
		return 0, false
	}
}

// parseStringParam safely parses a string parameter from map[string]any.
func parseStringParam(v any) (string, bool) {
	val, ok := v.(string)
	return val, ok
}

// ValidationResult holds the result of parameter validation.
type ValidationResult struct {
	Valid   bool
	Field   string
	Message string
}

// validateIntRange checks if an int value is within [min, max].
func validateIntRange(name string, value, min, max int) ValidationResult {
	if value < min {
		return ValidationResult{Valid: false, Field: name, Message: name + " must be >= " + itoa(min)}
	}
	if value > max {
		return ValidationResult{Valid: false, Field: name, Message: name + " must be <= " + itoa(max)}
	}
	return ValidationResult{Valid: true}
}

// validateFloatRange checks if a float64 value is within [min, max].
func validateFloatRange(name string, value, min, max float64) ValidationResult {
	if value < min {
		return ValidationResult{Valid: false, Field: name, Message: name + " must be >= " + ftoa(min)}
	}
	if value > max {
		return ValidationResult{Valid: false, Field: name, Message: name + " must be <= " + ftoa(max)}
	}
	return ValidationResult{Valid: true}
}

// validateStringChoice checks if a string value is in the allowed set.
func validateStringChoice(name string, value string, choices []string) ValidationResult {
	for _, c := range choices {
		if value == c {
			return ValidationResult{Valid: true}
		}
	}
	return ValidationResult{Valid: false, Field: name, Message: name + " must be one of: " + joinStrings(choices)}
}

// itoa converts int to string (minimal implementation to avoid strconv import).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf) - 1
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	if neg {
		buf[i] = '-'
		i--
	}
	return string(buf[i+1:])
}

// ftoa converts float64 to string with 2 decimal places.
func ftoa(f float64) string {
	// Simple implementation: multiply by 100, round, then format
	if f < 0 {
		return "-" + ftoa(-f)
	}
	whole := int(f)
	frac := int((f - float64(whole)) * 100)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac/10) + itoa(frac%10)
}

// joinStrings joins a slice of strings with commas.
func joinStrings(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += ", " + strs[i]
	}
	return result
}
