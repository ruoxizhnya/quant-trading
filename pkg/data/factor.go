package data

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// FactorStore defines the interface for factor data operations.
// This interface allows for easy mocking in tests.
type FactorStore interface {
	GetOHLCVForDateRange(ctx context.Context, startDate, endDate time.Time) ([]domain.OHLCV, error)
	GetFundamentalsSnapshot(ctx context.Context, cutoffDate time.Time) ([]domain.FundamentalData, error)
	GetTradingDays(ctx context.Context, startDate, endDate time.Time) ([]time.Time, error)
	SaveFactorCacheBatch(ctx context.Context, entries []*domain.FactorCacheEntry) error
}

// FactorComputer computes and caches factor z-scores.
// It reads raw data (OHLCV, fundamentals) from PostgresStore,
// computes cross-sectional z-scores, and persists to factor_cache table.
type FactorComputer struct {
	store  FactorStore
	logger zerolog.Logger
}

// NewFactorComputer creates a new FactorComputer.
func NewFactorComputer(store FactorStore) *FactorComputer {
	return &FactorComputer{
		store:  store,
		logger: zerolog.Nop(),
	}
}

func (f *FactorComputer) SetLogger(l zerolog.Logger) {
	f.logger = l.With().Str("component", "factor_computer").Logger()
}

// ZScore normalizes raw values to z-scores (cross-sectional, per date).
// Returns a map from symbol to z-score. Symbols with NaN or zero variance
// are assigned a z-score of 0.
//
// The math (mean / population stddev) is delegated to pkg/statistics
// (ODR-013 P1-21). The map input/output is kept here because the
// factor pipeline passes OHLCV-derived symbol keys; converting to
// []float64 at every call site would lose that mapping.
//
// We build a (symbol, value) slice once so that the same key index is
// used both for the z-score lookup and the result assignment; relying
// on `for symbol := range values` twice would mis-pair entries because
// Go map iteration order is not stable across iterations.
func ZScore(values map[string]float64) map[string]float64 {
	n := len(values)
	if n == 0 {
		return make(map[string]float64)
	}
	type kv struct {
		symbol string
		value  float64
	}
	pairs := make([]kv, 0, n)
	for symbol, v := range values {
		pairs = append(pairs, kv{symbol, v})
	}
	raw := make([]float64, n)
	for i, p := range pairs {
		raw[i] = p.value
	}
	zs := statistics.ZScore(raw)
	result := make(map[string]float64, n)
	for i, p := range pairs {
		result[p.symbol] = zs[i]
	}
	return result
}

// PercentileRank converts raw values to percentile ranks [0, 100].
//
// The formula here is the **ordinal** percentile (no tie handling):
// for the i-th smallest value in a slice of length n, the rank is
// (i+1 - 0.5) / n * 100. This is the standard SAS / SQL PERCENT_RANK
// flavour and is what the factor pipeline stores downstream. It is
// intentionally distinct from statistics.PercentileRank, which
// uses the **average** (competition) rank. Do not migrate this to
// the shared helper without first auditing factor-cache consumers.
func PercentileRank(values map[string]float64) map[string]float64 {
	n := len(values)
	if n == 0 {
		return make(map[string]float64)
	}
	type kv struct {
		symbol string
		value  float64
	}
	sorted := make([]kv, 0, n)
	for symbol, v := range values {
		sorted = append(sorted, kv{symbol, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].value < sorted[j].value })
	result := make(map[string]float64, n)
	for i, kv := range sorted {
		rank := float64(i+1) - 0.5
		result[kv.symbol] = (rank / float64(n)) * 100
	}
	return result
}

// ComputeMomentumFactor computes N-day return for all stocks on a given date.
// Momentum = (close_today / close_N_days_ago) - 1.
// Default lookback is 20 trading days.
func (f *FactorComputer) ComputeMomentumFactor(ctx context.Context, date time.Time, lookbackDays int) error {
	if lookbackDays <= 0 {
		lookbackDays = 20
	}
	startDate := date.AddDate(0, 0, -(lookbackDays + 10))
	allOHLCV, err := f.store.GetOHLCVForDateRange(ctx, startDate, date)
	if err != nil {
		return fmt.Errorf("load OHLCV for momentum: %w", err)
	}
	rawValues := f.computeMomentumRaw(allOHLCV, date, lookbackDays)
	if len(rawValues) == 0 {
		f.logger.Warn().Time("date", date).Msg("No momentum values computed (insufficient OHLCV data)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	var entries []*domain.FactorCacheEntry
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: domain.FactorMomentum,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save momentum factor_cache: %w", err)
	}
	f.logger.Info().
		Time("date", date).
		Int("stocks", len(entries)).
		Int("lookback", lookbackDays).
		Msg("Momentum factor computed and cached")
	return nil
}

func (f *FactorComputer) computeMomentumRaw(allOHLCV []domain.OHLCV, targetDate time.Time, lookback int) map[string]float64 {
	type bar struct {
		date  time.Time
		close float64
	}
	stockBars := make(map[string][]bar)
	for _, o := range allOHLCV {
		stockBars[o.Symbol] = append(stockBars[o.Symbol], bar{o.Date, o.Close})
	}
	results := make(map[string]float64)
	for symbol, bars := range stockBars {
		sort.Slice(bars, func(i, j int) bool { return bars[i].date.Before(bars[j].date) })
		endIdx := -1
		for i := len(bars) - 1; i >= 0; i-- {
			if !bars[i].date.After(targetDate) {
				endIdx = i
				break
			}
		}
		if endIdx < 0 || endIdx < lookback {
			continue
		}
		endPrice := bars[endIdx].close
		startPrice := bars[endIdx-lookback].close
		if startPrice <= 0 || endPrice <= 0 {
			continue
		}
		results[symbol] = (endPrice - startPrice) / startPrice
	}
	return results
}

// ComputeValueFactor computes value factor (EP composite) for all stocks on a given date.
// Value = 1/PE (earnings yield). Uses fundamentals snapshot as of cutoff date.
func (f *FactorComputer) ComputeValueFactor(ctx context.Context, date time.Time) error {
	fundamentals, err := f.store.GetFundamentalsSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("load fundamentals for value factor: %w", err)
	}
	rawValues := make(map[string]float64)
	for _, fd := range fundamentals {
		if fd.PE != nil && *fd.PE > 0 {
			rawValues[fd.TsCode] = 1.0 / (*fd.PE)
		}
	}
	if len(rawValues) == 0 {
		f.logger.Warn().Time("date", date).Msg("No value factor values (no valid PE data)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	var entries []*domain.FactorCacheEntry
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: domain.FactorValue,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save value factor_cache: %w", err)
	}
	f.logger.Info().
		Time("date", date).
		Int("stocks", len(entries)).
		Msg("Value factor computed and cached")
	return nil
}

// ComputeQualityFactor computes quality factor (ROE) for all stocks on a given date.
func (f *FactorComputer) ComputeQualityFactor(ctx context.Context, date time.Time) error {
	fundamentals, err := f.store.GetFundamentalsSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("load fundamentals for quality factor: %w", err)
	}
	rawValues := make(map[string]float64)
	for _, fd := range fundamentals {
		if fd.ROE != nil && *fd.ROE > 0 {
			rawValues[fd.TsCode] = *fd.ROE
		}
	}
	if len(rawValues) == 0 {
		f.logger.Warn().Time("date", date).Msg("No quality factor values (no valid ROE data)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	var entries []*domain.FactorCacheEntry
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: domain.FactorQuality,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save quality factor_cache: %w", err)
	}
	f.logger.Info().
		Time("date", date).
		Int("stocks", len(entries)).
		Msg("Quality factor computed and cached")
	return nil
}

// ComputeAllFactors runs all factor computations for a single date.
// When parallel=true, the three factors are computed concurrently using goroutines.
func (f *FactorComputer) ComputeAllFactors(ctx context.Context, date time.Time, momentumLookback int, parallel bool) error {
	if !parallel {
		// Sequential execution (original behavior)
		if err := f.ComputeMomentumFactor(ctx, date, momentumLookback); err != nil {
			return fmt.Errorf("momentum: %w", err)
		}
		if err := f.ComputeValueFactor(ctx, date); err != nil {
			return fmt.Errorf("value: %w", err)
		}
		if err := f.ComputeQualityFactor(ctx, date); err != nil {
			return fmt.Errorf("quality: %w", err)
		}
		return nil
	}

	// Parallel execution: compute all three factors concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := f.ComputeMomentumFactor(ctx, date, momentumLookback); err != nil {
			errCh <- fmt.Errorf("momentum: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := f.ComputeValueFactor(ctx, date); err != nil {
			errCh <- fmt.Errorf("value: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := f.ComputeQualityFactor(ctx, date); err != nil {
			errCh <- fmt.Errorf("quality: %w", err)
		}
	}()

	wg.Wait()
	close(errCh)

	// Return the first error if any
	for err := range errCh {
		return err
	}
	return nil
}

// ComputeFactorsForRange computes all factors for every trading day in [startDate, endDate].
// This is the main entry point for batch pre-computation.
// Returns the number of dates processed and any error.
func (f *FactorComputer) ComputeFactorsForRange(ctx context.Context, startDate, endDate time.Time, momentumLookback int) (int, error) {
	tradingDays, err := f.store.GetTradingDays(ctx, startDate, endDate)
	if err != nil {
		return 0, fmt.Errorf("get trading days: %w", err)
	}
	if len(tradingDays) == 0 {
		return 0, fmt.Errorf("no trading days in range [%s, %s]", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	}
	f.logger.Info().
		Time("start", startDate).
		Time("end", endDate).
		Int("trading_days", len(tradingDays)).
		Int("momentum_lookback", momentumLookback).
		Msg("Starting batch factor computation")

	count := 0
	for _, day := range tradingDays {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
		if err := f.ComputeAllFactors(ctx, day, momentumLookback, false); err != nil {
			f.logger.Warn().Time("date", day).Err(err).Msg("Skipping date due to error")
			continue
		}
		count++
	}
	f.logger.Info().
		Int("dates_processed", count).
		Int("total_dates", len(tradingDays)).
		Msg("Batch factor computation complete")
	return count, nil
}

// LoadFactorCacheIntoMap loads pre-computed factor cache into an in-memory map
// suitable for Engine integration. Structure: factorType -> tradeDate -> symbol -> zScore.
func LoadFactorCacheIntoMap(entries []*domain.FactorCacheEntry) map[domain.FactorType]map[time.Time]map[string]float64 {
	result := make(map[domain.FactorType]map[time.Time]map[string]float64)
	for _, e := range entries {
		if result[e.FactorName] == nil {
			result[e.FactorName] = make(map[time.Time]map[string]float64)
		}
		if result[e.FactorName][e.TradeDate] == nil {
			result[e.FactorName][e.TradeDate] = make(map[string]float64)
		}
		result[e.FactorName][e.TradeDate][e.Symbol] = e.ZScore
	}
	return result
}
