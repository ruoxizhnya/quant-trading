package data

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// FactorComputer computes and caches factor z-scores.
type FactorComputer struct {
	store *storage.PostgresStore
}

// NewFactorComputer creates a new FactorComputer.
func NewFactorComputer(store *storage.PostgresStore) *FactorComputer {
	return &FactorComputer{store: store}
}

// ZScore normalizes raw values to z-scores (cross-sectional, per date).
// Returns a map from symbol to z-score. Symbols with NaN or zero variance
// are assigned a z-score of 0.
func (f *FactorComputer) ZScore(values map[string]float64) map[string]float64 {
	n := len(values)
	if n == 0 {
		return make(map[string]float64)
	}

	// Compute mean
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)

	// Compute standard deviation
	var varianceSum float64
	for _, v := range values {
		diff := v - mean
		varianceSum += diff * diff
	}
	stdDev := math.Sqrt(varianceSum / float64(n))

	result := make(map[string]float64, n)
	if stdDev == 0 || math.IsNaN(stdDev) {
		// All values are the same or invalid — return 0 for all
		for symbol := range values {
			result[symbol] = 0
		}
		return result
	}

	for symbol, v := range values {
		result[symbol] = (v - mean) / stdDev
	}

	return result
}

// PercentileRank converts z-scores to percentile ranks [0, 100].
// Returns a map from symbol to percentile rank.
func (f *FactorComputer) PercentileRank(values map[string]float64) map[string]float64 {
	n := len(values)
	if n == 0 {
		return make(map[string]float64)
	}

	// Collect values and sort
	type kv struct {
		symbol string
		value  float64
	}
	sorted := make([]kv, 0, n)
	for symbol, v := range values {
		sorted = append(sorted, kv{symbol, v})
	}

	// Sort ascending
	for i := 0; i < n-1; i++ {
		for j := i + 1; j < n; j++ {
			if sorted[j].value < sorted[i].value {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Assign percentile rank: (rank - 0.5) / n * 100
	result := make(map[string]float64, n)
	for i, kv := range sorted {
		rank := float64(i+1) - 0.5 // mid-rank
		result[kv.symbol] = (rank / float64(n)) * 100
	}

	return result
}

// ComputeMomentumFactor computes 20-day return for all stocks on a given date.
// Returns the raw momentum values (not yet z-scored) and saves to DB.
func (f *FactorComputer) ComputeMomentumFactor(ctx context.Context, date time.Time) error {
	// TODO: Load OHLCV for all stocks for the past 21 days (including today)
	// For each stock, compute: (close_today / close_20days_ago) - 1
	// Then z-score and save to factor_cache
	return fmt.Errorf("not yet implemented: ComputeMomentumFactor requires OHLCV data loading")
}

// ComputeValueFactor computes EP, BP, SP from fundamentals for all stocks on a given date.
// EP = 1/PE, BP = 1/PB, SP = 1/PS.
func (f *FactorComputer) ComputeValueFactor(ctx context.Context, date time.Time) error {
	// TODO: Load fundamentals for all stocks for the given date
	// For each stock: EP = 1/PE, BP = 1/PB, SP = 1/PS
	// Then z-score and save to factor_cache
	return fmt.Errorf("not yet implemented: ComputeValueFactor requires fundamentals data loading")
}

// ComputeQualityFactor computes ROE, ROA, gross_margin z-scores for all stocks on a given date.
func (f *FactorComputer) ComputeQualityFactor(ctx context.Context, date time.Time) error {
	// TODO: Load fundamentals for all stocks for the given date
	// For each stock: use ROE, ROA, gross_margin directly
	// Then z-score and save to factor_cache
	return fmt.Errorf("not yet implemented: ComputeQualityFactor requires fundamentals data loading")
}

// ComputeAllFactors runs all factor computations for a given date.
// It is a placeholder that calls each individual factor computation.
func (f *FactorComputer) ComputeAllFactors(ctx context.Context, date time.Time) error {
	// Momentum
	if err := f.ComputeMomentumFactor(ctx, date); err != nil {
		return fmt.Errorf("momentum factor: %w", err)
	}
	// Value
	if err := f.ComputeValueFactor(ctx, date); err != nil {
		return fmt.Errorf("value factor: %w", err)
	}
	// Quality
	if err := f.ComputeQualityFactor(ctx, date); err != nil {
		return fmt.Errorf("quality factor: %w", err)
	}
	return nil
}
