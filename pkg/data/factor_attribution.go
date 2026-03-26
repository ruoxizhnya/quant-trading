package data

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

const defaultForwardDays = 20

// FactorAttributor computes factor quintile returns and IC (Information Coefficient).
type FactorAttributor struct {
	store *storage.PostgresStore
}

// NewFactorAttributor creates a new FactorAttributor.
func NewFactorAttributor(store *storage.PostgresStore) *FactorAttributor {
	return &FactorAttributor{store: store}
}

// ComputeFactorReturns computes factor quintile returns for a given date.
// 1. Get factor z-scores from factor_cache for all stocks on date
// 2. Rank stocks into 5 quintile groups by z-score
// 3. Compute equal-weight average return of each quintile over the next N days
// 4. Compute top-minus-bottom spread
func (f *FactorAttributor) ComputeFactorReturns(ctx context.Context, factor domain.FactorType, date time.Time) error {
	// Get factor z-scores for this date
	cacheEntries, err := f.store.GetFactorCacheRange(ctx, factor, date, date)
	if err != nil {
		return fmt.Errorf("failed to get factor cache: %w", err)
	}
	if len(cacheEntries) == 0 {
		return fmt.Errorf("no factor cache entries found for date %s", date.Format("2006-01-02"))
	}

	// Build z-score map
	zScores := make(map[string]float64)
	for _, entry := range cacheEntries {
		zScores[entry.Symbol] = entry.ZScore
	}

	// Get symbols sorted by z-score
	symbols := make([]string, 0, len(zScores))
	for sym := range zScores {
		symbols = append(symbols, sym)
	}
	sort.Slice(symbols, func(i, j int) bool { return zScores[symbols[i]] < zScores[symbols[j]] })

	n := len(symbols)
	quintileSize := n / 5
	if quintileSize == 0 {
		return fmt.Errorf("not enough stocks (%d) to form quintiles", n)
	}

	// Get trading days for forward period
	tradingDays, err := f.store.GetTradingDays(ctx, date, date.AddDate(0, 0, defaultForwardDays*2))
	if err != nil {
		return fmt.Errorf("failed to get trading days: %w", err)
	}
	if len(tradingDays) < defaultForwardDays+1 {
		return fmt.Errorf("not enough trading days for forward period (%d available, need %d)", len(tradingDays), defaultForwardDays+1)
	}

	currentDay := tradingDays[0]
	forwardDay := tradingDays[defaultForwardDays]

	// Get close prices for current day
	type priceEntry struct {
		symbol string
		close  float64
	}
	var currentPrices []priceEntry
	for _, sym := range symbols {
		ohlcv, err := f.store.GetOHLCV(ctx, sym, currentDay, currentDay)
		if err != nil || len(ohlcv) == 0 {
			continue
		}
		currentPrices = append(currentPrices, priceEntry{sym, ohlcv[0].Close})
	}

	// Get close prices for forward day
	type forwardPriceEntry struct {
		symbol string
		close  float64
	}
	var forwardPrices []forwardPriceEntry
	for _, sym := range symbols {
		ohlcv, err := f.store.GetOHLCV(ctx, sym, forwardDay, forwardDay)
		if err != nil || len(ohlcv) == 0 {
			continue
		}
		forwardPrices = append(forwardPrices, forwardPriceEntry{sym, ohlcv[0].Close})
	}

	// Build price maps
	currentPriceMap := make(map[string]float64)
	for _, p := range currentPrices {
		currentPriceMap[p.symbol] = p.close
	}
	forwardPriceMap := make(map[string]float64)
	for _, p := range forwardPrices {
		forwardPriceMap[p.symbol] = p.close
	}

	// Assign stocks to quintiles and compute returns
	quintileReturns := make([][]float64, 5)
	for i := 0; i < 5; i++ {
		quintileReturns[i] = make([]float64, 0)
	}

	for i, sym := range symbols {
		currPrice, ok1 := currentPriceMap[sym]
		fwdPrice, ok2 := forwardPriceMap[sym]
		if !ok1 || !ok2 || currPrice <= 0 {
			continue
		}
		ret := (fwdPrice / currPrice) - 1

		quintileIdx := i / quintileSize
		if quintileIdx > 4 {
			quintileIdx = 4
		}
		quintileReturns[quintileIdx] = append(quintileReturns[quintileIdx], ret)
	}

	// Compute average returns per quintile
	avgReturns := make([]float64, 5)
	for q := 0; q < 5; q++ {
		if len(quintileReturns[q]) == 0 {
			avgReturns[q] = 0
			continue
		}
		var sum float64
		for _, r := range quintileReturns[q] {
			sum += r
		}
		avgReturns[q] = sum / float64(len(quintileReturns[q]))
	}

	// Compute cumulative return (compound) for each quintile over forward period
	cumulativeReturns := make([]float64, 5)
	for q := 0; q < 5; q++ {
		if len(quintileReturns[q]) == 0 {
			cumulativeReturns[q] = 0
			continue
		}
		// Equal-weight cumulative return
		cumulativeReturns[q] = avgReturns[q] * float64(defaultForwardDays)
	}

	topMinusBot := avgReturns[4] - avgReturns[0]

	// Save factor returns to DB
	var records []*domain.FactorReturn
	for q := 0; q < 5; q++ {
		records = append(records, &domain.FactorReturn{
			FactorName:       factor,
			TradeDate:        date,
			Quintile:         q + 1,
			AvgReturn:        avgReturns[q],
			CumulativeReturn: cumulativeReturns[q],
			TopMinusBot:      topMinusBot,
		})
	}

	return f.store.SaveFactorReturnBatch(ctx, records)
}

// ComputeIC computes Spearman rank IC between factor z-scores and forward returns.
func (f *FactorAttributor) ComputeIC(ctx context.Context, factor domain.FactorType, date time.Time, forwardDays int) (*domain.ICEntry, error) {
	if forwardDays <= 0 {
		forwardDays = defaultForwardDays
	}

	// Get factor z-scores for this date
	cacheEntries, err := f.store.GetFactorCacheRange(ctx, factor, date, date)
	if err != nil {
		return nil, fmt.Errorf("failed to get factor cache: %w", err)
	}
	if len(cacheEntries) == 0 {
		return nil, fmt.Errorf("no factor cache entries found for date %s", date.Format("2006-01-02"))
	}

	zScores := make(map[string]float64)
	for _, entry := range cacheEntries {
		zScores[entry.Symbol] = entry.ZScore
	}

	// Get trading days for forward period
	tradingDays, err := f.store.GetTradingDays(ctx, date, date.AddDate(0, 0, forwardDays*2))
	if err != nil {
		return nil, fmt.Errorf("failed to get trading days: %w", err)
	}
	if len(tradingDays) < forwardDays+1 {
		return nil, fmt.Errorf("not enough trading days (%d available, need %d)", len(tradingDays), forwardDays+1)
	}

	currentDay := tradingDays[0]
	forwardDay := tradingDays[forwardDays]

	// Get all OHLCV data for current and forward day in one query per symbol
	symbols := make([]string, 0, len(zScores))
	for sym := range zScores {
		symbols = append(symbols, sym)
	}

	// Get current day prices
	currentPrices := make(map[string]float64)
	for _, sym := range symbols {
		ohlcv, err := f.store.GetOHLCV(ctx, sym, currentDay, currentDay)
		if err != nil || len(ohlcv) == 0 {
			continue
		}
		currentPrices[sym] = ohlcv[0].Close
	}

	// Get forward day prices
	forwardPrices := make(map[string]float64)
	for _, sym := range symbols {
		ohlcv, err := f.store.GetOHLCV(ctx, sym, forwardDay, forwardDay)
		if err != nil || len(ohlcv) == 0 {
			continue
		}
		forwardPrices[sym] = ohlcv[0].Close
	}

	// Compute forward returns
	forwardReturns := make(map[string]float64)
	for sym, currPrice := range currentPrices {
		fwdPrice, ok := forwardPrices[sym]
		if !ok || currPrice <= 0 {
			continue
		}
		forwardReturns[sym] = (fwdPrice / currPrice) - 1
	}

	// Compute Spearman IC
	ic := SpearmanRankIC(zScores, forwardReturns)

	// Approximate p-value based on IC magnitude and sample size
	n := float64(len(zScores))
	pValue := approximatePValue(ic, n)

	// Compute top IC: average IC when z-score is in top quintile
	topICSymbols := make([]string, 0, len(zScores))
	sortedSymbols := make([]string, 0, len(zScores))
	for sym := range zScores {
		sortedSymbols = append(sortedSymbols, sym)
	}
	sort.Slice(sortedSymbols, func(i, j int) bool { return zScores[sortedSymbols[i]] < zScores[sortedSymbols[j]] })

	quintileSize := len(sortedSymbols) / 5
	if quintileSize > 0 {
		for i := len(sortedSymbols) - 1; i >= len(sortedSymbols)-quintileSize && i >= 0; i-- {
			topICSymbols = append(topICSymbols, sortedSymbols[i])
		}
	}

	var topIC float64
	if len(topICSymbols) > 1 {
		// Compute IC among top quintile stocks
		topZScores := make(map[string]float64)
		topFwdReturns := make(map[string]float64)
		for _, sym := range topICSymbols {
			topZScores[sym] = zScores[sym]
			if ret, ok := forwardReturns[sym]; ok {
				topFwdReturns[sym] = ret
			}
		}
		if len(topZScores) > 1 {
			topIC = SpearmanRankIC(topZScores, topFwdReturns)
		}
	}

	entry := &domain.ICEntry{
		FactorName: factor,
		TradeDate:  date,
		IC:         ic,
		PValue:     pValue,
		TopIC:      topIC,
	}

	if err := f.store.SaveICEntryBatch(ctx, []*domain.ICEntry{entry}); err != nil {
		return nil, fmt.Errorf("failed to save IC entry: %w", err)
	}

	return entry, nil
}

// GetFactorReturnsTimeSeries retrieves factor returns for a date range.
func (f *FactorAttributor) GetFactorReturnsTimeSeries(ctx context.Context, factor domain.FactorType, startDate, endDate time.Time) ([]*domain.FactorReturn, error) {
	return f.store.GetFactorReturns(ctx, factor, startDate, endDate)
}

// GetICTimeSeries retrieves IC series for a factor over a date range.
func (f *FactorAttributor) GetICTimeSeries(ctx context.Context, factor domain.FactorType, startDate, endDate time.Time) ([]*domain.ICEntry, error) {
	return f.store.GetICEntries(ctx, factor, startDate, endDate)
}

// GetTradingDaysForRange returns trading days within a date range.
func (f *FactorAttributor) GetTradingDaysForRange(ctx context.Context, startDate, endDate time.Time) ([]time.Time, error) {
	return f.store.GetTradingDays(ctx, startDate, endDate)
}

// SpearmanRankIC computes Spearman rank correlation coefficient between zScores and forwardReturns.
// Both maps must have the same symbols for paired comparison.
func SpearmanRankIC(zScores map[string]float64, forwardReturns map[string]float64) float64 {
	// Get paired symbols
	symbols := make([]string, 0)
	for sym := range zScores {
		if _, ok := forwardReturns[sym]; ok {
			symbols = append(symbols, sym)
		}
	}
	n := len(symbols)
	if n < 2 {
		return 0
	}

	// Sort by z-score and assign ranks
	sortedByZ := make([]string, len(symbols))
	copy(sortedByZ, symbols)
	sort.Slice(sortedByZ, func(i, j int) bool { return zScores[sortedByZ[i]] < zScores[sortedByZ[j]] })
	zRank := make(map[string]float64)
	for i, sym := range sortedByZ {
		zRank[sym] = float64(i + 1)
	}

	// Sort by forward returns and assign ranks
	sortedByR := make([]string, len(symbols))
	copy(sortedByR, symbols)
	sort.Slice(sortedByR, func(i, j int) bool { return forwardReturns[sortedByR[i]] < forwardReturns[sortedByR[j]] })
	rRank := make(map[string]float64)
	for i, sym := range sortedByR {
		rRank[sym] = float64(i + 1)
	}

	// Compute sum of squared rank differences
	nF := float64(n)
	var sumD2 float64
	for _, sym := range symbols {
		d := zRank[sym] - rRank[sym]
		sumD2 += d * d
	}

	// Spearman: 1 - (6 * sum(d^2)) / (n * (n^2 - 1))
	denom := nF * (nF*nF - 1)
	if denom == 0 {
		return 0
	}
	return 1 - (6*sumD2)/denom
}

// approximatePValue provides a rough p-value estimate for a Spearman IC given sample size.
// Uses the t-distribution approximation: t = IC * sqrt(n-2) / sqrt(1 - IC^2)
func approximatePValue(ic float64, n float64) float64 {
	if n < 3 || math.IsNaN(ic) || math.IsInf(ic, 0) {
		return 1.0
	}
	absIC := math.Abs(ic)
	if absIC > 0.9999 {
		return 0.0
	}
	// t-statistic
	t := ic * math.Sqrt(n-2) / math.Sqrt(1-ic*ic)
	absT := math.Abs(t)
	// Approximate using standard normal for large n (central limit)
	// For smaller n, use a conservative estimate
	if n > 30 {
		// Standard normal tail approximation
		// p ≈ 2 * (1 - Φ(|t|)) but simplified
		return 2 * mathExp(-0.5*absT*absT) / (absT + 1.5)
	}
	// Conservative small-sample estimate
	return 1.0 / (1.0 + absT*absT/(n-2))
}

func mathExp(x float64) float64 {
	if x < -700 {
		return 0
	}
	return math.Exp(x)
}
