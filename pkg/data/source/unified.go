package source

import (
	"encoding/json"
	"time"
)

// UnifiedDataPoint is the canonical in-memory representation of any data
// point flowing through the ETL pipeline. The storage layer persists
// Data to type-specific tables (ohlcv_daily_qfq, capital_flow, ...) with
// the Source and IngestTime columns populated.
//
// Why JSONB Data? It lets the storage layer evolve without forcing every
// adapter to learn a new struct, and it makes schema migrations a SQL
// problem (ALTER TABLE) rather than a Go-code problem.
type UnifiedDataPoint struct {
	// Symbol is the canonical ticker in the project convention.
	// For A-shares: "600519.SH" or "000001.SZ".
	// For US stocks: "AAPL".
	Symbol string

	// TradeTime is the timestamp the data point refers to.
	// For daily OHLCV: the trade date at 00:00:00 local.
	// For realtime: the snapshot timestamp (typically millisecond precision).
	// For fundamentals: the announcement date or fiscal end date.
	TradeTime time.Time

	// Source is the adapter name (e.g. "tushare", "mootdx").
	// Persisted in the `source` column.
	Source string

	// DataType is the kind of data (e.g. "ohlcv_daily", "capital_flow").
	// Determines which storage table to write to.
	DataType string

	// Data carries the type-specific fields as JSON.
	// Required keys depend on DataType; see the per-adapter normalizer.
	Data map[string]interface{}

	// IngestTime is set by the ETL pipeline when the point is created.
	// Persisted in the `ingest_time` column.
	IngestTime time.Time
}

// NewUnifiedDataPoint constructs a point with IngestTime set to now.
func NewUnifiedDataPoint(symbol, source, dataType string, tradeTime time.Time, data map[string]interface{}) UnifiedDataPoint {
	return UnifiedDataPoint{
		Symbol:     symbol,
		TradeTime:  tradeTime,
		Source:     source,
		DataType:   dataType,
		Data:       data,
		IngestTime: time.Now().UTC(),
	}
}

// ToJSON serializes the Data map for storage (e.g. into a JSONB column or
// for log/audit trails).
func (p UnifiedDataPoint) ToJSON() ([]byte, error) {
	return json.Marshal(p.Data)
}

// DeduplicateKey returns the key used for deduplication in ETL.
//
// Two points with the same (Symbol, DataType, TradeTime) refer to the same
// logical data point. Source is intentionally excluded from the key
// because the Registry's fallback chain may load the same data from
// multiple sources; we keep the one with higher source priority (caller's
// responsibility to enforce).
func (p UnifiedDataPoint) DeduplicateKey() string {
	return p.Symbol + "|" + p.DataType + "|" + p.TradeTime.UTC().Format(time.RFC3339Nano)
}

// Deduplicate drops duplicate points in a slice, keeping the first
// occurrence (which callers can control by ordering the input).
//
// This is a simple O(n) implementation; for very large batches a
// sorted-input variant would be faster.
func Deduplicate(points []UnifiedDataPoint) []UnifiedDataPoint {
	out, _ := DeduplicateWithCount(points)
	return out
}

// DeduplicateWithCount is like Deduplicate but also returns the number
// of points dropped. The etl pipeline uses this to attribute skips
// to the deduplication stage.
func DeduplicateWithCount(points []UnifiedDataPoint) ([]UnifiedDataPoint, int) {
	seen := make(map[string]struct{}, len(points))
	out := make([]UnifiedDataPoint, 0, len(points))
	skipped := 0
	for _, p := range points {
		k := p.DeduplicateKey()
		if _, ok := seen[k]; ok {
			skipped++
			continue
		}
		seen[k] = struct{}{}
		out = append(out, p)
	}
	return out, skipped
}
