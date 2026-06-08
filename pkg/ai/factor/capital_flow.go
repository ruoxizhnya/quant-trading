// Package factor computes quantitative factors from multi-source data.
//
// Each factor file is a self-contained computation that consumes
// UnifiedDataPoint slices (produced by the ETL pipeline) and returns
// a Symbol→Value map suitable for IC backtesting.
//
// Conventions:
//   - Factor values are floats. NaN means "no signal" (e.g. missing
//     data for a symbol) and is excluded from IC computation by the
//     metrics package.
//   - All functions are pure: no I/O, no globals, no time.Now. The
//     caller is responsible for fetching the data and providing a
//     consistent timestamp. This makes every factor trivially testable.
package factor

import (
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data/source"
)

// CapitalFlowRow is a normalized capital-flow record. The ETL pipeline
// projects UnifiedDataPoint.Data into this struct; the factor logic
// stays in float land and is unaware of the underlying storage layout.
type CapitalFlowRow struct {
	Symbol     string
	TradeTime  time.Time
	Period     string  // "1d" | "5d" | "10d" | "60d"
	MainNet    float64 // 主力净流入 (yuan)
	SuperNet   float64 // 超大单净流入
	LargeNet   float64 // 大单净流入
	MediumNet  float64 // 中单净流入
	SmallNet   float64 // 小单净流入
	RetailNet  float64 // 散户净流入
	MainRatio  float64 // 主力净流入占比
	ClosePrice float64
	ChangePct  float64
}

// CapitalFlowFromPoints projects capital_flow UnifiedDataPoints into
// typed rows. Missing fields default to 0 (NaN is intentionally NOT
// used here so that downstream arithmetic does not become poisoned).
//
// This indirection lets the factor consume either freshly-fetched data
// (from the multi-source registry) or replayed data (from storage) with
// the same code path.
func CapitalFlowFromPoints(points []source.UnifiedDataPoint) []CapitalFlowRow {
	out := make([]CapitalFlowRow, 0, len(points))
	for _, p := range points {
		if p.DataType != source.DataTypeCapitalFlow {
			continue
		}
		row := CapitalFlowRow{
			Symbol:     p.Symbol,
			TradeTime:  p.TradeTime,
			Period:     stringField(p.Data, "period"),
			MainNet:    floatField(p.Data, "main_net"),
			SuperNet:   floatField(p.Data, "super_net"),
			LargeNet:   floatField(p.Data, "large_net"),
			MediumNet:  floatField(p.Data, "medium_net"),
			SmallNet:   floatField(p.Data, "small_net"),
			RetailNet:  floatField(p.Data, "retail_net"),
			MainRatio:  floatField(p.Data, "main_net_ratio"),
			ClosePrice: floatField(p.Data, "close_price"),
			ChangePct:  floatField(p.Data, "change_pct"),
		}
		out = append(out, row)
	}
	return out
}

// CapitalFlowFactor is the "main net inflow over the past N days" alpha.
//
// For each symbol, the factor value is the trailing sum of MainNet
// over the most recent `lookback` distinct trade dates, normalized by
// the stock's average daily turnover (approximated as ClosePrice to
// keep the factor unitless). A high value means institutional money
// is accumulating; a low (negative) value means distributing.
//
// lookback must be >= 1. The default of 5 covers one trading week and
// is a standard Chinese A-share momentum window.
func CapitalFlowFactor(rows []CapitalFlowRow, lookback int) map[string]float64 {
	if lookback <= 0 {
		lookback = 5
	}

	// Group by symbol, keep the most recent N distinct dates.
	bySym := make(map[string][]CapitalFlowRow, 64)
	for _, r := range rows {
		bySym[r.Symbol] = append(bySym[r.Symbol], r)
	}

	out := make(map[string]float64, len(bySym))
	for sym, symRows := range bySym {
		// Sort by trade time descending and take the first `lookback`.
		sort.Slice(symRows, func(i, j int) bool {
			return symRows[i].TradeTime.After(symRows[j].TradeTime)
		})
		if len(symRows) > lookback {
			symRows = symRows[:lookback]
		}
		if len(symRows) == 0 {
			continue
		}
		var mainSum, closeRef float64
		for _, r := range symRows {
			mainSum += r.MainNet
			if closeRef == 0 {
				closeRef = r.ClosePrice
			}
		}
		// Normalize by close to make the factor cross-section comparable
		// between high- and low-price stocks. We do NOT divide by shares
		// outstanding here because the data point doesn't carry it; for a
		// strict per-share metric, compute TurnoverFactor separately.
		if closeRef <= 0 {
			continue
		}
		out[sym] = mainSum / closeRef
	}
	return out
}

// CapitalFlowICSign is a quick sanity check: if the main net flow for
// symbol on the latest day is positive, return +1; if negative, -1;
// 0 otherwise. Useful as a coarse trading signal without full IC
// backtesting.
func CapitalFlowICSign(rows []CapitalFlowRow, symbol string) int {
	var latest CapitalFlowRow
	found := false
	for _, r := range rows {
		if r.Symbol != symbol {
			continue
		}
		if !found || r.TradeTime.After(latest.TradeTime) {
			latest = r
			found = true
		}
	}
	if !found {
		return 0
	}
	switch {
	case latest.MainNet > 0:
		return 1
	case latest.MainNet < 0:
		return -1
	default:
		return 0
	}
}

// IsCapitalFlowRowValid reports whether a row has the minimum fields
// required for downstream factor computation. Used by ETL to drop
// garbage rows before they poison an IC computation.
func IsCapitalFlowRowValid(r CapitalFlowRow) bool {
	if r.Symbol == "" {
		return false
	}
	if r.TradeTime.IsZero() {
		return false
	}
	if math.IsNaN(r.MainNet) || math.IsInf(r.MainNet, 0) {
		return false
	}
	return true
}

// floatField reads a float64 from a map[string]interface{}, returning
// 0 if the key is missing or the value is the wrong type. The ETL
// pipeline emits JSON-decoded values; the cast is safe for the
// primitives we expect (json.Unmarshal of a JSON number yields float64).
func floatField(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
