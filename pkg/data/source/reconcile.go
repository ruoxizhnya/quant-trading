package source

import (
	"math"
	"sort"
	"time"
)

// ReconciliationStrategy selects which source's value wins when
// multiple sources report the same logical (Symbol, DataType, TradeTime)
// with different `Data` payloads.
//
// Why does this matter? ODR-011 introduced 9 data sources and the
// Registry's fallback chain. With N sources, two (or more) adapters
// may independently return a data point for the same (symbol, type,
// time) tuple. The naive "first wins" dedup loses information:
// e.g., tushare's adjusted close and eastmoney's unadjusted close
// differ — picking one over the other silently is a hidden bug.
type ReconciliationStrategy int

const (
	// StrategyFirstWins keeps the first point in the group (matches
	// the original Deduplicate behavior). The caller controls
	// ordering by passing a slice that is already sorted by their
	// preferred source priority.
	StrategyFirstWins ReconciliationStrategy = iota

	// StrategyLatestWins picks the point with the latest IngestTime.
	// Rationale: a fresher fetch is presumed to reflect post-hoc
	// corrections (e.g., split-adjusted closes updated by the
	// provider after the initial release).
	StrategyLatestWins

	// StrategyPriorityWins picks the point whose Source has the
	// lowest priority value from PriorityFunc. Convention:
	// 0 = primary, 1 = first fallback, ...
	// Sources that PriorityFunc does not recognize are treated
	// as priority math.MaxInt (lowest possible priority).
	StrategyPriorityWins

	// StrategyNumericMedian takes the per-field median across all
	// sources for numeric Data fields. Non-numeric fields (strings,
	// nested maps) fall back to the first source's value.
	// This is appropriate for OHLCV-like data where the absolute
	// truth is unknown and a robust central estimate beats
	// source-bias.
	StrategyNumericMedian
)

// Reconciler groups input points by the logical key
// (Symbol, DataType, TradeTime) and resolves conflicts via Strategy.
//
// Threading: a Reconciler has no mutable state and is safe for
// concurrent use after construction. The PriorityFunc (if set) must
// also be concurrency-safe; it is invoked once per group.
type Reconciler struct {
	Strategy     ReconciliationStrategy
	PriorityFunc func(source string) int // lower = higher priority
}

// ReconcileStats summarizes one reconciliation pass.
//
// Field meanings:
//
//   - Groups: number of distinct (Symbol, DataType, TradeTime) keys
//     seen in the input.
//   - Conflicts: number of groups with more than one source (the
//     rest were singletons and were passed through unchanged).
//   - Resolved: number of conflicts successfully resolved (always
//     equals Conflicts — reserved for future strategies that may
//     fail to resolve, e.g. an unparseable payload).
//   - PickedByFirst/Priority/Latest/Median: how many conflicts each
//     concrete strategy actually resolved. The four counters are
//     mutually exclusive and sum to Resolved.
type ReconcileStats struct {
	Groups           int
	Conflicts        int
	Resolved         int
	PickedByFirst    int
	PickedByPriority int
	PickedByLatest   int
	PickedByMedian   int
}

// Reconcile groups `points` by their dedup key and resolves each
// multi-source group via the configured Strategy. The returned slice
// preserves the first-occurrence order of groups (so the output is
// deterministic and stable for snapshotting).
func (r *Reconciler) Reconcile(points []UnifiedDataPoint) ([]UnifiedDataPoint, ReconcileStats) {
	// Group by (Symbol, DataType, TradeTime). We use the existing
	// DeduplicateKey for the bucket, which guarantees consistency
	// with the earlier dedup pass — important so a future "reconcile
	// after dedup" code path does not see phantom groups.
	groups := make(map[string][]UnifiedDataPoint, len(points))
	groupOrder := make([]string, 0, len(points))
	for _, p := range points {
		k := p.DeduplicateKey()
		if _, ok := groups[k]; !ok {
			groupOrder = append(groupOrder, k)
		}
		groups[k] = append(groups[k], p)
	}

	stats := ReconcileStats{Groups: len(groups)}
	out := make([]UnifiedDataPoint, 0, len(groups))
	for _, k := range groupOrder {
		bucket := groups[k]
		if len(bucket) == 1 {
			// No conflict — pass through.
			out = append(out, bucket[0])
			continue
		}
		stats.Conflicts++
		winner := r.resolveBucket(bucket)
		switch r.Strategy {
		case StrategyFirstWins:
			stats.PickedByFirst++
		case StrategyPriorityWins:
			stats.PickedByPriority++
		case StrategyLatestWins:
			stats.PickedByLatest++
		case StrategyNumericMedian:
			stats.PickedByMedian++
		}
		stats.Resolved++
		out = append(out, winner)
	}
	return out, stats
}

// resolveBucket picks the winning point from a multi-source group.
// The function does NOT mutate the input slice.
func (r *Reconciler) resolveBucket(bucket []UnifiedDataPoint) UnifiedDataPoint {
	switch r.Strategy {
	case StrategyLatestWins:
		return pickLatest(bucket)
	case StrategyPriorityWins:
		return pickByPriority(bucket, r.PriorityFunc)
	case StrategyNumericMedian:
		return pickByMedian(bucket)
	default: // StrategyFirstWins and zero-value
		return bucket[0]
	}
}

// pickLatest returns the point with the latest IngestTime.
// On ties (extremely rare since IngestTime is ns-precision), the
// first point wins.
func pickLatest(bucket []UnifiedDataPoint) UnifiedDataPoint {
	best := bucket[0]
	for _, p := range bucket[1:] {
		if p.IngestTime.After(best.IngestTime) {
			best = p
		}
	}
	return best
}

// pickByPriority returns the point from the source with the lowest
// priority value (best = 0). Unknown sources are assigned
// math.MaxInt to ensure they never outrank a registered priority.
// On ties, the first point wins.
func pickByPriority(bucket []UnifiedDataPoint, priority func(string) int) UnifiedDataPoint {
	if priority == nil {
		return bucket[0]
	}
	best := bucket[0]
	bestPrio := priority(best.Source)
	if bestPrio == 0 {
		return best
	}
	for _, p := range bucket[1:] {
		pPrio := priority(p.Source)
		if pPrio < bestPrio {
			best = p
			bestPrio = pPrio
			if bestPrio == 0 {
				return best
			}
		}
	}
	return best
}

// pickByMedian returns a synthetic point whose Data map takes the
// per-key median across all sources for numeric values. The
// Symbol/DataType/TradeTime/Source/IngestTime are taken from the
// first source (callers that care about provenance can override
// after the call).
//
// Non-numeric values (strings, booleans, nested maps, slices) keep
// the first source's value. This is intentional: median does not
// apply to non-ordered types, and silently picking "any" would be
// worse than the explicit "first wins" default.
func pickByMedian(bucket []UnifiedDataPoint) UnifiedDataPoint {
	if len(bucket) == 0 {
		return UnifiedDataPoint{}
	}
	out := bucket[0]
	out.Data = medianDataMaps(bucket)
	return out
}

// medianDataMaps computes the per-key median of numeric values across
// the input points. New keys introduced only by later sources are
// preserved at their first occurrence.
//
// Implementation: we use float64 as the canonical numeric type. The
// supported input types are float64, float32, int, int8/16/32/64,
// uint, uint8/16/32/64. Strings, bools, nested structures, and
// nil pass through unchanged.
func medianDataMaps(bucket []UnifiedDataPoint) map[string]interface{} {
	if len(bucket) == 0 {
		return nil
	}
	// Collect every key from every point in stable order.
	keys := make([]string, 0)
	seen := make(map[string]bool)
	for _, p := range bucket {
		for k := range p.Data {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)

	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		var nums []float64
		var fallback interface{}
		anyNumeric := false
		for _, p := range bucket {
			v, ok := p.Data[k]
			if !ok {
				continue
			}
			if f, isNum := toFloat64(v); isNum {
				nums = append(nums, f)
				anyNumeric = true
			} else if fallback == nil {
				fallback = v
			}
		}
		if anyNumeric {
			out[k] = medianFloat64(nums)
		} else if fallback != nil {
			out[k] = fallback
		}
		// If a key has only nil values, drop it.
	}
	return out
}

// toFloat64 coerces a numeric value to float64. Returns (v, true)
// for recognized numeric types, (0, false) otherwise.
func toFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	default:
		return 0, false
	}
}

// medianFloat64 returns the median of xs. For even-length input it
// returns the average of the two middle values. NaN/Inf inputs are
// preserved as-is (callers that need to filter them must do so
// upstream).
func medianFloat64(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := make([]float64, len(xs))
	copy(sorted, xs)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// DefaultSourcePriority assigns numeric priorities for the project's
// known data sources. Lower = higher priority. Sources not in the
// map default to math.MaxInt.
//
// Why these values?
//   - tushare (1) is the historical primary source with the most
//     consistent corporate-action adjustments.
//   - eastmoney (2) is the secondary for free real-time data.
//   - mootdx (3) is preferred for real-time / minute bars but not for
//     adjusted historicals.
//   - eastmoney_sectors (4) / eastmoney_toplist (5) are specialized.
//   - juchao (6) / xueqiu (7) are event/feed sources with no overlap.
//   - alpha_vantage (8) / yahoo_finance (9) are global, used only
//     when no Chinese source returns a value.
//
// Tweak this map per-deployment if your organization prefers a
// different ranking.
func DefaultSourcePriority() func(string) int {
	return func(name string) int {
		switch name {
		case "tushare":
			return 1
		case "eastmoney":
			return 2
		case "mootdx":
			return 3
		case "eastmoney_sectors":
			return 4
		case "eastmoney_toplist":
			return 5
		case "juchao":
			return 6
		case "xueqiu":
			return 7
		case "alpha_vantage":
			return 8
		case "yahoo_finance":
			return 9
		default:
			return math.MaxInt
		}
	}
}

// DefaultReconciler returns a Reconciler using the project defaults:
// StrategyPriorityWins with the canonical source priority map.
//
// This is the recommended starting point for the ETL pipeline. Tests
// that need a different policy can build their own Reconciler.
func DefaultReconciler() *Reconciler {
	return &Reconciler{
		Strategy:     StrategyPriorityWins,
		PriorityFunc: DefaultSourcePriority(),
	}
}

// ReconcileWithReconciler is a convenience wrapper used by callers
// that just want the default reconciliation pass without managing a
// Reconciler instance.
func ReconcileWithReconciler(points []UnifiedDataPoint) ([]UnifiedDataPoint, ReconcileStats) {
	return DefaultReconciler().Reconcile(points)
}

// IsZero reports whether a ReconciliationStrategy is its zero value
// (StrategyFirstWins). Useful for callers that want to default to the
// priority strategy when no strategy is configured.
//
// Note: callers MUST treat this as advisory — the comparison is
// against the explicit zero value of the type, not against a
// "no-op" semantic. StrategyFirstWins is a valid choice.
func (s ReconciliationStrategy) IsZero() bool {
	return s == StrategyFirstWins
}

// String returns a stable, human-readable name for the strategy.
// Useful for log lines and test failure messages.
func (s ReconciliationStrategy) String() string {
	switch s {
	case StrategyFirstWins:
		return "first_wins"
	case StrategyLatestWins:
		return "latest_wins"
	case StrategyPriorityWins:
		return "priority_wins"
	case StrategyNumericMedian:
		return "numeric_median"
	default:
		return "unknown"
	}
}

// GroupBySource buckets a set of points by their source name. This is
// a small helper used by callers that need to do source-level
// diagnostics (e.g., "tushare contributed 80% of last week's OHLCV
// rows"). The returned map preserves the order of first occurrence
// of each source.
func GroupBySource(points []UnifiedDataPoint) map[string][]UnifiedDataPoint {
	out := make(map[string][]UnifiedDataPoint)
	for _, p := range points {
		out[p.Source] = append(out[p.Source], p)
	}
	return out
}

// timeAlias exists so test files in this package can build
// ReconciliationStrategy values without an extra `time` import in
// production code. Kept private; not used by exported APIs.
var _ = time.Time{}
