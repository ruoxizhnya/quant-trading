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

// Cache key prefixes
const (
	// KeyPrefixOHLCV is the prefix for OHLCV cache keys
	KeyPrefixOHLCV = "ohlcv:"

	// KeyPrefixFund is the prefix for fundamental data cache keys
	KeyPrefixFund  = "fund:"
)

// Batch operation limits
const (
	// DefaultBatchSize is the default batch size for bulk database operations
	DefaultBatchSize = 1000

	// MaxScanBatchSize is the maximum number of keys to scan in one Redis SCAN operation
	MaxScanBatchSize = 100
)
