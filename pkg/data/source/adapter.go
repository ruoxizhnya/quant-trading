// Package source provides a unified abstraction over external data sources.
//
// ADR-016 / ODR-011: Multi-Source Data Architecture.
//
// Each data source (Tushare, mootdx, eastmoney, etc.) implements the
// DataSourceAdapter interface. The Registry manages a set of adapters and
// a fallback chain per data type. ETL pipelines invoke Registry.Fetch to
// obtain a normalized response, then persist it with explicit source
// attribution.
//
// Design principles:
//
//   - Backward compatible: existing pkg/data/tushare.go keeps working
//     through a TushareAdapter wrapper.
//   - Composable: an adapter can support multiple data types
//     (e.g. eastmoney: capital_flow, sectors, top_list, news).
//   - Observable: every adapter exposes HealthCheck, RateLimit, and Schema.
//   - No SDK lock-in: adapters use minimal surface area so swapping the
//     underlying library (e.g. mootdx) does not ripple to consumers.
package source

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AdapterType categorizes the transport/protocol of a source.
type AdapterType string

const (
	// AdapterTypeHTTP indicates a plain HTTP/HTTPS client.
	AdapterTypeHTTP AdapterType = "http"
	// AdapterTypeSDK indicates a Go SDK (e.g. mootdx uses TCP/binary protocol).
	AdapterTypeSDK AdapterType = "sdk"
	// AdapterTypeWebSocket indicates a long-lived streaming connection.
	AdapterTypeWebSocket AdapterType = "websocket"
)

// Common data type constants used across adapters.
//
// Keep these in sync with the `data_fallback_chain.data_type` table
// seeded in migration 015.
const (
	DataTypeOHLCDaily    = "ohlcv_daily"
	DataTypeOHLCMinute   = "ohlcv_minute"
	DataTypeRealtime     = "realtime_quote"
	DataTypeCapitalFlow  = "capital_flow"
	DataTypeFundamental  = "fundamentals"
	DataTypeSectors      = "sectors"
	DataTypeStockSector  = "stock_sector"
	DataTypeTopList      = "top_list"
	DataTypeLimitUpPool  = "limit_up_pool"
	DataTypeAnnounce     = "announcements"
	DataTypeNews         = "news"
	DataTypeHotSearch    = "hot_search"
	DataTypeGlobalOHLCV  = "global_ohlcv"
)

// ErrUnsupported indicates the adapter cannot serve the requested data type.
var ErrUnsupported = errors.New("data source: unsupported data type")

// ErrRateLimited indicates the adapter exhausted its rate budget.
// The Registry treats this as a strong signal to fall back to the next
// adapter in the chain rather than retrying the current one.
var ErrRateLimited = errors.New("data source: rate limited")

// ErrUpstreamUnavailable signals transport-level failure (timeout, 5xx, ...).
// Registry falls back on this as well.
var ErrUpstreamUnavailable = errors.New("data source: upstream unavailable")

// FetchRequest is the universal input to DataSourceAdapter.Fetch.
//
// Symbols is optional for data types that are not symbol-scoped
// (e.g. sector list, hot search). StartDate/EndDate are inclusive.
type FetchRequest struct {
	// DataType identifies the kind of data being requested.
	// Adapters should validate DataType against their SupportedTypes().
	DataType string

	// Symbols optionally scopes the request.
	// Empty means "all symbols" for adapters that support that mode.
	Symbols []string

	// StartDate / EndDate define the time window (inclusive).
	// For realtime data, EndDate should be time.Now().
	StartDate time.Time
	EndDate   time.Time

	// Period subdivides the time window for things like
	// "5d" / "10d" / "60d" capital flow aggregations.
	Period string

	// Extra carries adapter-specific options.
	// For example: {"market": "SH", "fiscal_period": "Q3"}.
	Extra map[string]interface{}
}

// DataItem is a single record returned by an adapter.
//
// The Data map preserves the raw fields; normalization to domain
// types happens in ETL pipelines.
type DataItem struct {
	// Symbol is the canonical ticker (e.g. "600519.SH" or "AAPL").
	Symbol string
	// TradeTime is the timestamp the data point refers to.
	// For realtime, this is the snapshot timestamp; for daily OHLCV, the trade date.
	TradeTime time.Time
	// Data is the raw field map; ETL normalizes from this.
	Data map[string]interface{}
}

// FetchResponse is the result of a successful Fetch.
type FetchResponse struct {
	// Source echoes the adapter's Name() so the caller can attribute.
	Source string
	// Items is the normalized data points (post-source-format conversion).
	Items []DataItem
	// FetchedAt is when the adapter completed the upstream call.
	FetchedAt time.Time
	// Latency is the time spent in the upstream call (excludes normalization).
	Latency time.Duration
	// HasMore indicates whether more pages/cursor data is available.
	HasMore bool
	// NextCursor can be used for pagination (adapter-specific).
	NextCursor string
	// Metadata carries adapter-specific extra info (e.g. partition name).
	Metadata map[string]interface{}
}

// RateLimitConfig describes how often a source may be hit.
type RateLimitConfig struct {
	// RequestsPerMinute is the advisory ceiling.
	// 0 means "no limit enforced by the adapter" (caller is responsible).
	RequestsPerMinute int
	// Burst allows short spikes above the steady rate.
	Burst int
}

// DataSchema declares the fields an adapter can produce for a given data type.
// Used by the registry to validate fetch requests and by the ETL to know
// which normalizer to dispatch to.
type DataSchema struct {
	DataType string
	Fields   []SchemaField
}

// SchemaField describes a single field produced by an adapter.
type SchemaField struct {
	Name     string
	Type     string // "string" | "float" | "int" | "date" | "datetime"
	Required bool
	Unit     string // "yuan" | "percent" | "share" | ""
}

// DataSourceAdapter is the contract every concrete source must implement.
//
// Implementations should be safe for concurrent use: the Registry may call
// Fetch from multiple goroutines.
type DataSourceAdapter interface {
	// Name returns the stable identifier persisted in the data_source_registry.
	// Example: "tushare", "mootdx", "eastmoney".
	Name() string

	// Type returns the transport category.
	Type() AdapterType

	// Enabled reports whether the adapter is currently usable.
	// The Registry skips disabled adapters when picking fallbacks.
	Enabled() bool

	// SupportedTypes returns the data types this adapter can serve.
	// Used by the Registry when building fallback chains at startup.
	SupportedTypes() []string

	// Schema returns the schema for a given data type.
	// Returns ErrUnsupported if the data type is not served.
	Schema(dataType string) (DataSchema, error)

	// Fetch retrieves data from the upstream source.
	// Implementations should:
	//   - Respect ctx cancellation.
	//   - Apply internal rate limiting before returning ErrRateLimited.
	//   - Translate transport errors into ErrUpstreamUnavailable.
	Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error)

	// HealthCheck performs a lightweight liveness probe.
	// Implementations should use a fast endpoint and a short timeout
	// (e.g. 5 seconds).
	HealthCheck(ctx context.Context) error

	// RateLimit returns the rate limit configuration.
	RateLimit() RateLimitConfig
}

// AdapterBase provides reusable state (logger, enabled flag, name) for
// concrete adapters. Embedding it is optional but recommended.
//
// Note: This is intentionally minimal to avoid premature abstraction.
// Logging is plumbed in later via composition when we wire a logger.
type AdapterBase struct {
	adapterName string
	enabled     bool
}

// NewAdapterBase constructs an AdapterBase with sensible defaults.
func NewAdapterBase(name string, enabled bool) AdapterBase {
	return AdapterBase{adapterName: name, enabled: enabled}
}

// Name implements DataSourceAdapter.Name.
func (b AdapterBase) Name() string { return b.adapterName }

// Enabled implements DataSourceAdapter.Enabled.
func (b AdapterBase) Enabled() bool { return b.enabled }

// SetEnabled toggles the enabled flag. Used by config reload.
func (b *AdapterBase) SetEnabled(v bool) { b.enabled = v }

// Validate checks a FetchRequest for common errors before passing it to
// an adapter. Adapters should call this as their first step.
func Validate(req FetchRequest) error {
	if req.DataType == "" {
		return fmt.Errorf("data source: empty DataType")
	}
	if req.EndDate.Before(req.StartDate) {
		return fmt.Errorf("data source: EndDate %s before StartDate %s",
			req.EndDate, req.StartDate)
	}
	return nil
}

// IsRetryable reports whether err is one of the recognized transient
// failures (rate limit, upstream unavailable) that justify falling back
// to the next adapter in the chain.
//
// Permanent errors (validation, parse) are not retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUpstreamUnavailable) {
		return true
	}
	return false
}
