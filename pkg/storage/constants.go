package storage

import "time"

// Database connection pool constants
const (
	// PostgresMaxConns is the maximum number of connections in the PostgreSQL pool
	PostgresMaxConns = 20

	// PostgresMinConns is the minimum number of idle connections in the PostgreSQL pool
	PostgresMinConns = 5

	// PostgresConnMaxLifetime is the maximum lifetime of a connection before it's closed
	PostgresConnMaxLifetime = time.Hour

	// PostgresConnMaxIdleTime is the maximum time a connection can sit idle before being closed
	PostgresConnMaxIdleTime = 30 * time.Minute
)

// Redis connection constants
const (
	// RedisConnectTimeout is the timeout for establishing a Redis connection
	RedisConnectTimeout = 5 * time.Second

	// RedisDefaultTTL is the default TTL for cached items (24 hours)
	RedisDefaultTTL = 24 * time.Hour

	// RedisShortTTL is a shorter TTL for frequently-updated data (1 hour)
	RedisShortTTL = 1 * time.Hour
)

// Cache key namespace
//
// All Redis keys written by the Quant Lab system are prefixed with
// `KeyNamespace` (currently "quantlab:") so that:
//
//  1. Multiple applications can share a single Redis instance without
//     key collisions (e.g. dev/staging sharing a Redis box).
//  2. Operators can `KEYS quantlab:*` (or `SCAN MATCH quantlab:*`)
//     to enumerate every key owned by this project, and `DEL` the
//     whole namespace for a cold reset.
//
// IMPORTANT: changing KeyNamespace is a **breaking change** for any
// process holding pre-existing keys — they become unreachable and
// will re-populate on next request. The 24h TTL on stock / OHLCV
// entries ensures stale keys expire naturally.
const (
	// KeyNamespace is the global prefix prepended to every Redis
	// key the system writes. MUST end with ":" so a `MATCH
	// quantlab:*` pattern is unambiguous.
	KeyNamespace = "quantlab:"
)

// Cache key prefixes
//
// All key prefix constants embed KeyNamespace so call sites only
// ever compose against the prefixed form. Tests pin the literal
// values to detect accidental renames.
var (
	// KeyPrefixOHLCV is the prefix for OHLCV cache keys.
	// Full key shape: "quantlab:ohlcv:{symbol}:{startYYYYMMDD}:{endYYYYMMDD}"
	KeyPrefixOHLCV = KeyNamespace + "ohlcv:"

	// KeyPrefixFund is the prefix for fundamental data cache keys.
	// Full key shape: "quantlab:fund:{symbol}:{dateYYYYMMDD}"
	KeyPrefixFund = KeyNamespace + "fund:"

	// KeyPrefixStocksList is the prefix for the per-exchange
	// stock list. "all" / "" requests use "*" (no suffix).
	// Full key shape: "quantlab:stocks:list:{exchange}"
	KeyPrefixStocksList = KeyNamespace + "stocks:list:"

	// KeyPrefixStock is the prefix for a single stock record.
	// Full key shape: "quantlab:stock:{symbol}"
	KeyPrefixStock = KeyNamespace + "stock:"
)

// Batch operation limits
const (
	// DefaultBatchSize is the default batch size for bulk database operations
	DefaultBatchSize = 1000

	// MaxScanBatchSize is the maximum number of keys to scan in one Redis SCAN operation
	MaxScanBatchSize = 100
)
