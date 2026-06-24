package hkex

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/rs/zerolog"
)

// NorthboundFactor computes tradeable factors from raw northbound flow
// data. It is a thin layer over NorthboundFetcher: every method pulls
// the data it needs (caching is the caller's responsibility — the
// factor calculator is stateless so it can be reused across symbols
// and dates without stale-data bugs).
//
// All methods take context.Context as the first parameter and return
// an error; they never panic. Edge cases (empty data, zero division,
// insufficient history) return a descriptive error so the caller can
// decide whether to skip the symbol or halt the batch.
type NorthboundFactor struct {
	fetcher NorthboundFetcher
	logger  zerolog.Logger
}

// NewNorthboundFactor constructs a factor calculator backed by `fetcher`.
// The fetcher may be nil — methods will return a sentinel error, which
// keeps the constructor cheap in DI scenarios where the fetcher is
// wired later.
func NewNorthboundFactor(fetcher NorthboundFetcher) *NorthboundFactor {
	return &NorthboundFactor{
		fetcher: fetcher,
		logger:  zerolog.Nop(),
	}
}

// SetLogger wires a zerolog.Logger.
func (f *NorthboundFactor) SetLogger(l zerolog.Logger) {
	f.logger = l.With().Str("component", "hkex.factor").Logger()
}

// ----------------------------------------------------------------------------
// Aggregate-flow factors (NorthboundFlow-level)
// ----------------------------------------------------------------------------

// NetBuyMA computes the moving average of total northbound net buy
// over the `days` trading days ending on (and including) `date`.
//
// We walk backwards calendar day by day (skipping weekends to cut
// latency) and call FetchDaily until we have `days` non-nil rows. If
// fewer than `days` rows are available in the lookback window, we
// return the average of whatever we found plus an error — this lets
// callers degrade gracefully while still being warned.
//
// days <= 0 is treated as 1. An empty result set returns (0, error).
func (f *NorthboundFactor) NetBuyMA(ctx context.Context, date time.Time, days int) (float64, error) {
	if f.fetcher == nil {
		return 0, fmt.Errorf("hkex: nil fetcher")
	}
	if days <= 0 {
		days = 1
	}
	values, err := f.collectDailyNetBuy(ctx, date, days)
	if err != nil {
		return 0, fmt.Errorf("net_buy_ma: %w", err)
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("net_buy_ma: no data for %s in lookback %d days", date.Format("2006-01-02"), days)
	}
	return mean(values), nil
}

// NetBuyMomentum computes the northbound momentum factor:
//
//	momentum = (current_net_buy - MA) / |MA|
//
// The denominator uses |MA| (not MA) so the sign of momentum is
// determined solely by current_net_buy vs MA, not by the sign of MA.
// This matches the convention used by the sibling momentum factor in
// pkg/data/factor.go (which uses raw return, also sign-stable).
//
// Edge cases:
//   - MA == 0: returns 0 with no error (a flat market has no momentum).
//   - Insufficient data: returns (0, error).
func (f *NorthboundFactor) NetBuyMomentum(ctx context.Context, date time.Time, days int) (float64, error) {
	if f.fetcher == nil {
		return 0, fmt.Errorf("hkex: nil fetcher")
	}
	if days <= 0 {
		days = 1
	}
	values, err := f.collectDailyNetBuy(ctx, date, days)
	if err != nil {
		return 0, fmt.Errorf("net_buy_momentum: %w", err)
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("net_buy_momentum: no data for %s", date.Format("2006-01-02"))
	}
	ma := mean(values)
	if math.Abs(ma) < 1e-12 {
		// Flat market — no momentum signal. Return 0 rather than ±Inf
		// so downstream z-score math doesn't blow up.
		return 0, nil
	}
	current := values[len(values)-1]
	return (current - ma) / math.Abs(ma), nil
}

// ----------------------------------------------------------------------------
// Per-stock factors (StockFlow-level)
// ----------------------------------------------------------------------------

// HoldingChange computes the change in northbound holding ratio for
// `symbol` between `date - days` and `date`.
//
// Returns holding_ratio_today - holding_ratio_N_days_ago. A positive
// value means the northbound pool increased its stake; negative means
// it decreased.
//
// Edge cases:
//   - Fewer than 2 data points: returns (0, error).
//   - The window is widened by +5 calendar days on each side to absorb
//     holidays; if still no data, returns (0, error).
func (f *NorthboundFactor) HoldingChange(ctx context.Context, symbol string, date time.Time, days int) (float64, error) {
	if f.fetcher == nil {
		return 0, fmt.Errorf("hkex: nil fetcher")
	}
	if days <= 0 {
		days = 1
	}
	end := date
	start := date.AddDate(0, 0, -(days + 5)) // +5 calendar days headroom for holidays
	flows, err := f.fetcher.FetchStockFlow(ctx, symbol, start, end)
	if err != nil {
		return 0, fmt.Errorf("holding_change %s: %w", symbol, err)
	}
	if len(flows) < 2 {
		return 0, fmt.Errorf("holding_change %s: insufficient data (%d rows)", symbol, len(flows))
	}
	// flows are ascending by date (per FetchStockFlow contract).
	earliest := flows[0].HoldingRatio
	latest := flows[len(flows)-1].HoldingRatio
	return latest - earliest, nil
}

// NetBuyRank returns the top `limit` stocks ranked by northbound net
// buy on `date`. The slice is sorted descending by NetBuy.
//
// This is a thin wrapper over FetchTopHoldings; it exists as a factor
// method so callers can ask the factor calculator for "the strongest
// northbound names today" without knowing about the fetcher.
func (f *NorthboundFactor) NetBuyRank(ctx context.Context, date time.Time, limit int) ([]StockFlow, error) {
	if f.fetcher == nil {
		return nil, fmt.Errorf("hkex: nil fetcher")
	}
	rows, err := f.fetcher.FetchTopHoldings(ctx, date, limit)
	if err != nil {
		return nil, fmt.Errorf("net_buy_rank: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].NetBuy > rows[j].NetBuy
	})
	return rows, nil
}

// IsNetInflow reports whether `symbol` had positive northbound net buy
// for `days` consecutive trading days ending on (and including) `date`.
//
// A single non-positive day breaks the streak and returns false. If
// fewer than `days` rows are available, returns false (not enough
// evidence to call it a streak).
func (f *NorthboundFactor) IsNetInflow(ctx context.Context, symbol string, date time.Time, days int) (bool, error) {
	if f.fetcher == nil {
		return false, fmt.Errorf("hkex: nil fetcher")
	}
	if days <= 0 {
		return false, fmt.Errorf("hkex: days must be > 0")
	}
	start := date.AddDate(0, 0, -(days + 5))
	flows, err := f.fetcher.FetchStockFlow(ctx, symbol, start, date)
	if err != nil {
		return false, fmt.Errorf("is_net_inflow %s: %w", symbol, err)
	}
	if len(flows) < days {
		return false, nil
	}
	// Take the last `days` rows and check all are > 0.
	tail := flows[len(flows)-days:]
	for _, fl := range tail {
		if fl.NetBuy <= 0 {
			return false, nil
		}
	}
	return true, nil
}

// FlowSignal classifies the northbound flow for `symbol` on `date`
// into one of StrongInflow / Neutral / StrongOutflow.
//
// Signal logic (per P2-12 spec):
//   - StrongInflow:   net_buy_today > mean + 2*stddev AND holding_ratio increasing
//   - StrongOutflow:  net_buy_today < mean - 2*stddev AND holding_ratio decreasing
//   - otherwise:      Neutral
//
// `days` controls the lookback window for mean/stddev. A window shorter
// than 3 has no defined stddev (sample variance needs n>=2), so we
// require days >= 3 and return Neutral + error otherwise.
//
// "holding_ratio increasing/decreasing" is decided by HoldingChange
// over the same window: > 0 means increasing, < 0 means decreasing.
func (f *NorthboundFactor) FlowSignal(ctx context.Context, symbol string, date time.Time, days int) (FlowSignal, error) {
	if f.fetcher == nil {
		return FlowSignalNeutral, fmt.Errorf("hkex: nil fetcher")
	}
	if days < 3 {
		return FlowSignalNeutral, fmt.Errorf("hkex: flow signal needs days >= 3, got %d", days)
	}
	start := date.AddDate(0, 0, -(days + 5))
	flows, err := f.fetcher.FetchStockFlow(ctx, symbol, start, date)
	if err != nil {
		return FlowSignalNeutral, fmt.Errorf("flow_signal %s: %w", symbol, err)
	}
	if len(flows) < days {
		return FlowSignalNeutral, fmt.Errorf("flow_signal %s: insufficient data (%d < %d)", symbol, len(flows), days)
	}
	// Use the last `days` rows for the statistics window.
	tail := flows[len(flows)-days:]
	netBuys := make([]float64, len(tail))
	for i, fl := range tail {
		netBuys[i] = fl.NetBuy
	}
	mu := mean(netBuys)
	sigma := sampleStdDev(netBuys)
	current := netBuys[len(netBuys)-1]

	// Holding-ratio trend over the same window.
	holdingChange := tail[len(tail)-1].HoldingRatio - tail[0].HoldingRatio

	upper := mu + 2*sigma
	lower := mu - 2*sigma

	switch {
	case current > upper && holdingChange > 0:
		return FlowSignalStrongInflow, nil
	case current < lower && holdingChange < 0:
		return FlowSignalStrongOutflow, nil
	default:
		return FlowSignalNeutral, nil
	}
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

// collectDailyNetBuy walks backwards from `date` for up to `days*2`
// calendar days (the 2× headroom absorbs weekends + holidays) and
// collects non-nil NorthboundFlow.TotalNetBuy values. Returns the
// slice ordered oldest→newest.
//
// The walk stops as soon as `days` values are collected OR the context
// is cancelled OR the lookback is exhausted. This bounds the number
// of upstream calls even for long windows.
func (f *NorthboundFactor) collectDailyNetBuy(ctx context.Context, date time.Time, days int) ([]float64, error) {
	values := make([]float64, 0, days)
	lookback := days * 2
	if lookback < days+10 {
		lookback = days + 10
	}
	for offset := 0; offset <= lookback && len(values) < days; offset++ {
		select {
		case <-ctx.Done():
			return values, ctx.Err()
		default:
		}
		d := date.AddDate(0, 0, -offset)
		// Skip weekends — Eastmoney never has rows for them, so a
		// FetchDaily call would just waste a round-trip.
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		flow, err := f.fetcher.FetchDaily(ctx, d)
		if err != nil {
			// Log and continue — a single bad day shouldn't abort
			// the whole MA.
			f.logger.Warn().
				Time("date", d).
				Err(err).
				Msg("fetch daily failed, skipping")
			continue
		}
		if flow == nil {
			continue // holiday / pre-open
		}
		values = append(values, flow.TotalNetBuy)
	}
	// Reverse so values are oldest→newest (matches the convention used
	// by the per-stock flows and makes "last element = current day"
	// true for the momentum calc).
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
	return values, nil
}

// mean returns the arithmetic mean. Returns 0 for an empty slice.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// sampleStdDev returns the sample (n-1) standard deviation. Returns 0
// for slices shorter than 2 elements — a single data point has no
// variance, and FlowSignal requires days >= 3 so this only triggers
// for degenerate inputs.
func sampleStdDev(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}
	mu := mean(values)
	var sumSqDiff float64
	for _, v := range values {
		d := v - mu
		sumSqDiff += d * d
	}
	return math.Sqrt(sumSqDiff / float64(n-1))
}
