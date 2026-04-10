package data

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

const (
	// TTLs — recent data gets shorter TTL to stay fresh
	ohlcvTTLOld     = 24 * time.Hour // historical data older than last 7 days
	ohlcvTTLRecent  = 1 * time.Hour  // data that includes the last 7 days
	fundamentalsTTL = 24 * time.Hour
)

// DataCache is a cache-aside data access layer.
// It wraps storage.Cache (Redis) and storage.PostgresStore (PostgreSQL)
// to serve OHLCV and fundamental data with Redis-first lookup and
// automatic fallback to PostgreSQL on cache miss.
// All keys use the pattern defined by the task: ohlcv:{symbol}:{start}:{end}
// and fund:{symbol}:{date} (dates in YYYYMMDD).
type DataCache struct {
	cache  storage.Cache
	store  *storage.PostgresStore
	logger zerolog.Logger
}

// NewDataCache creates a DataCache wrapping the provided Redis cache and Postgres store.
func NewDataCache(cache storage.Cache, store *storage.PostgresStore) *DataCache {
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
	return dc.WarmCacheWithWorkers(ctx, symbols, start, end, 8)
}

// WarmCacheWithWorkers pre-fetches OHLCV data in parallel using `workers` goroutines.
// This significantly speeds up cache warm-up for large universes (500+ symbols).
func (dc *DataCache) WarmCacheWithWorkers(ctx context.Context, symbols []string, start, end string, workers int) error {
	if len(symbols) == 0 {
		return nil
	}

	if workers <= 0 {
		workers = 1
	}

	dc.logger.Info().
		Int("symbols", len(symbols)).
		Str("start", start).
		Str("end", end).
		Int("workers", workers).
		Msg("Cache warm-up started (parallel)")

	startDate := parseDate(start)
	endDate := parseDate(end)

	type result struct {
		symbol string
		bars   []domain.OHLCV
		err    error
	}

	symbolChan := make(chan string, len(symbols))
	resultChan := make(chan result, len(symbols))

	// Launch workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range symbolChan {
				bars, err := dc.store.GetOHLCV(ctx, sym, startDate, endDate)
				if err != nil || len(bars) == 0 {
					resultChan <- result{symbol: sym, err: err}
					continue
				}
				resultChan <- result{symbol: sym, bars: bars}
			}
		}()
	}

	// Feed symbols
	go func() {
		for _, sym := range symbols {
			symbolChan <- sym
		}
		close(symbolChan)
	}()

	// Collect results and write to Redis
	var warmed, failed int
	warmedChan := make(chan int)
	failedChan := make(chan int)
	go func() {
		w, f := 0, 0
		for r := range resultChan {
			if r.err != nil || len(r.bars) == 0 {
				f++
				continue
			}
			if err := dc.SetOHLCV(ctx, r.symbol, start, end, r.bars); err != nil {
				dc.logger.Warn().Err(err).Str("symbol", r.symbol).Msg("Cache warm-up: set failed")
				f++
				continue
			}
			w++
		}
		warmedChan <- w
		failedChan <- f
	}()

	wg.Wait()
	close(resultChan)
	warmed = <-warmedChan
	failed = <-failedChan

	dc.logger.Info().
		Int("symbols", len(symbols)).
		Int("warmed", warmed).
		Int("failed", failed).
		Msg("Cache warm-up completed (parallel)")

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
	return fmt.Sprintf("%s%s:%s:%s", storage.KeyPrefixOHLCV, symbol, start, end)
}

// fundKey builds: fund:{symbol}:{dateYYYYMMDD}
func fundKey(symbol, date string) string {
	return fmt.Sprintf("%s%s:%s", storage.KeyPrefixFund, symbol, date)
}

// parseDate parses a YYYYMMDD string. Returns zero time on error.
func parseDate(s string) time.Time {
	t, _ := time.Parse("20060102", s)
	return t
}
