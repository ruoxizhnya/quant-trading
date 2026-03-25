package data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/rs/zerolog"
)

const (
	// TTLs — recent data gets shorter TTL to stay fresh
	ohlcvTTLOld     = 24 * time.Hour // historical data older than last 7 days
	ohlcvTTLRecent  = 1 * time.Hour   // data that includes the last 7 days
	fundamentalsTTL = 24 * time.Hour

	// Redis key prefixes
	keyPrefixOHLCV = "ohlcv:"
	keyPrefixFund   = "fund:"
)

// DataCache is a cache-aside data access layer.
// It wraps storage.Cache (Redis) and storage.PostgresStore (PostgreSQL)
// to serve OHLCV and fundamental data with Redis-first lookup and
// automatic fallback to PostgreSQL on cache miss.
// All keys use the pattern defined by the task: ohlcv:{symbol}:{start}:{end}
// and fund:{symbol}:{date} (dates in YYYYMMDD).
type DataCache struct {
	cache  *storage.Cache
	store  *storage.PostgresStore
	logger zerolog.Logger
}

// NewDataCache creates a DataCache wrapping the provided Redis cache and Postgres store.
func NewDataCache(cache *storage.Cache, store *storage.PostgresStore) *DataCache {
	return &DataCache{
		cache:  cache,
		store:  store,
		logger: logging.WithContext(map[string]any{"component": "data_cache"}),
	}
}

// GetOHLCV retrieves OHLCV bars for a symbol in [start, end] (YYYYMMDD strings).
// Cache-aside: check Redis first, fall back to PostgreSQL, cache the result.
func (dc *DataCache) GetOHLCV(ctx context.Context, symbol, start, end string) ([]domain.OHLCV, error) {
	key := ohlcvKey(symbol, start, end)

	// Try Redis first
	jsonData, err := dc.cache.Get(ctx, key)
	if err == nil && jsonData != nil {
		var bars []domain.OHLCV
		if err := json.Unmarshal(jsonData, &bars); err == nil {
			dc.logger.Debug().Str("symbol", symbol).Str("key", key).Msg("OHLCV cache hit")
			return bars, nil
		}
	}

	// Fallback to PostgreSQL
	startDate := parseDate(start)
	endDate := parseDate(end)
	bars, err := dc.store.GetOHLCV(ctx, symbol, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("DB query failed for OHLCV: %w", err)
	}

	// Cache with TTL
	if len(bars) > 0 {
		ttl := ohlcvTTLOld
		if time.Since(endDate) < 7*24*time.Hour {
			ttl = ohlcvTTLRecent
		}
		if err := dc.setWithTTL(ctx, key, bars, ttl); err != nil {
			dc.logger.Warn().Err(err).Str("key", key).Msg("OHLCV cache write failed")
		} else {
			dc.logger.Debug().Str("symbol", symbol).Int("bars", len(bars)).Str("ttl", ttl.String()).Msg("OHLCV cached after DB fallback")
		}
	}

	return bars, nil
}

// SetOHLCV stores OHLCV bars in Redis with TTL using SETEX.
// TTL is 1h for recent data (last 7 days), 24h otherwise.
func (dc *DataCache) SetOHLCV(ctx context.Context, symbol, start, end string, bars []domain.OHLCV) error {
	if len(bars) == 0 {
		return nil
	}
	key := ohlcvKey(symbol, start, end)
	ttl := ohlcvTTLOld
	if time.Since(bars[len(bars)-1].Date) < 7*24*time.Hour {
		ttl = ohlcvTTLRecent
	}
	return dc.setWithTTL(ctx, key, bars, ttl)
}

// GetFundamentals retrieves fundamental data for a symbol on a given date (YYYYMMDD).
// Returns fundamental records closest to the requested date.
// Cache-aside: check Redis first (key = fund:{symbol}:{date}), fall back to DB.
func (dc *DataCache) GetFundamentals(ctx context.Context, symbol, date string) ([]domain.Fundamental, error) {
	key := fundKey(symbol, date)

	// Try Redis first
	jsonData, err := dc.cache.Get(ctx, key)
	if err == nil && jsonData != nil {
		var records []domain.Fundamental
		if err := json.Unmarshal(jsonData, &records); err == nil {
			dc.logger.Debug().Str("symbol", symbol).Str("key", key).Msg("Fundamentals cache hit")
			return records, nil
		}
	}

	// Fallback to PostgreSQL
	dt := parseDate(date)
	records, err := dc.store.GetFundamentals(ctx, symbol, dt)
	if err != nil {
		return nil, fmt.Errorf("DB query failed for fundamentals: %w", err)
	}

	// Cache the result
	if len(records) > 0 {
		if err := dc.setWithTTL(ctx, key, records, fundamentalsTTL); err != nil {
			dc.logger.Warn().Err(err).Str("key", key).Msg("Fundamentals cache write failed")
		}
	}

	return records, nil
}

// SetFundamentals stores fundamental data in Redis with 24h TTL.
func (dc *DataCache) SetFundamentals(ctx context.Context, symbol, date string, records []domain.Fundamental) error {
	if len(records) == 0 {
		return nil
	}
	key := fundKey(symbol, date)
	return dc.setWithTTL(ctx, key, records, fundamentalsTTL)
}

// WarmCache pre-fetches OHLCV data for the entire stock universe into Redis.
// Call this once at the start of a backtest to eliminate per-symbol cache misses
// during the backtest run — critical for hitting the 5s backtest SLA.
//
// It fetches OHLCV from PostgreSQL and writes each symbol's range to Redis
// using the same key format as GetOHLCV/SetOHLCV.
func (dc *DataCache) WarmCache(ctx context.Context, symbols []string, start, end string) error {
	if len(symbols) == 0 {
		return nil
	}

	dc.logger.Info().
		Int("symbols", len(symbols)).
		Str("start", start).
		Str("end", end).
		Msg("Cache warm-up started")

	startDate := parseDate(start)
	endDate := parseDate(end)
	warmed := 0
	failed := 0

	for i, symbol := range symbols {
		select {
		case <-ctx.Done():
			dc.logger.Info().Int("warmed", warmed).Int("failed", failed).Msg("Cache warm-up cancelled")
			return ctx.Err()
		default:
		}

		if i%50 == 0 && i > 0 {
			dc.logger.Info().Int("progress", i).Int("total", len(symbols)).Msg("Cache warm-up in progress")
		}

		bars, err := dc.store.GetOHLCV(ctx, symbol, startDate, endDate)
		if err != nil || len(bars) == 0 {
			failed++
			continue
		}

		if err := dc.SetOHLCV(ctx, symbol, start, end, bars); err != nil {
			failed++
			dc.logger.Warn().Err(err).Str("symbol", symbol).Msg("Cache warm-up: set failed")
			continue
		}
		warmed++
	}

	dc.logger.Info().
		Int("symbols", len(symbols)).
		Int("warmed", warmed).
		Int("failed", failed).
		Msg("Cache warm-up completed")

	return nil
}

// ---- Private helpers ----

// setWithTTL serializes value to JSON and stores it in Redis with TTL (SETEX semantics).
func (dc *DataCache) setWithTTL(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("json marshal failed: %w", err)
	}
	return dc.cache.SetEX(ctx, key, data, ttl)
}

// ohlcvKey builds: ohlcv:{symbol}:{startYYYYMMDD}:{endYYYYMMDD}
func ohlcvKey(symbol, start, end string) string {
	return fmt.Sprintf("%s%s:%s:%s", keyPrefixOHLCV, symbol, start, end)
}

// fundKey builds: fund:{symbol}:{dateYYYYMMDD}
func fundKey(symbol, date string) string {
	return fmt.Sprintf("%s%s:%s", keyPrefixFund, symbol, date)
}

// parseDate parses a YYYYMMDD string. Returns zero time on error.
func parseDate(s string) time.Time {
	t, _ := time.Parse("20060102", s)
	return t
}
