package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/rs/zerolog"
)

const (
	ohlcvTTL    = 1 * time.Hour
	stockTTL    = 24 * time.Hour
	keyPrefixOHLCV    = "ohlcv:"
	keyPrefixStock    = "stock:"
	keyPrefixStockList = "stocks:list:"
)

// Cache provides Redis caching operations.
type Cache struct {
	client *redis.Client
	logger zerolog.Logger
}

// NewCache creates a new Redis cache instance.
func NewCache(redisURL string) (*Cache, error) {
	logger := logging.WithContext(map[string]any{"component": "cache"})

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info().Str("url", redisURL).Msg("Redis connection established")
	return &Cache{client: client, logger: logger}, nil
}

// Close closes the Redis connection.
func (c *Cache) Close() error {
	return c.client.Close()
}

// CacheOHLCV caches OHLCV data with 1-hour TTL.
func (c *Cache) CacheOHLCV(ctx context.Context, symbol string, startDate, endDate time.Time, data []domain.OHLCV) error {
	key := c.ohlcvKey(symbol, startDate, endDate)
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal OHLCV data: %w", err)
	}
	if err := c.client.Set(ctx, key, jsonData, ohlcvTTL).Err(); err != nil {
		return fmt.Errorf("failed to cache OHLCV: %w", err)
	}
	c.logger.Debug().Str("symbol", symbol).Msg("OHLCV cached")
	return nil
}

// GetCachedOHLCV retrieves cached OHLCV data.
func (c *Cache) GetCachedOHLCV(ctx context.Context, symbol string, startDate, endDate time.Time) ([]domain.OHLCV, error) {
	key := c.ohlcvKey(symbol, startDate, endDate)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get cached OHLCV: %w", err)
	}
	var result []domain.OHLCV
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached OHLCV: %w", err)
	}
	c.logger.Debug().Str("symbol", symbol).Msg("OHLCV cache hit")
	return result, nil
}

// CacheStocks caches stock list with 24-hour TTL.
func (c *Cache) CacheStocks(ctx context.Context, exchange string, stocks []domain.Stock) error {
	key := c.stockListKey(exchange)
	jsonData, err := json.Marshal(stocks)
	if err != nil {
		return fmt.Errorf("failed to marshal stocks: %w", err)
	}
	if err := c.client.Set(ctx, key, jsonData, stockTTL).Err(); err != nil {
		return fmt.Errorf("failed to cache stocks: %w", err)
	}
	c.logger.Debug().Str("exchange", exchange).Msg("Stocks list cached")
	return nil
}

// GetCachedStocks retrieves cached stock list.
func (c *Cache) GetCachedStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	key := c.stockListKey(exchange)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get cached stocks: %w", err)
	}
	var result []domain.Stock
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached stocks: %w", err)
	}
	c.logger.Debug().Str("exchange", exchange).Msg("Stocks cache hit")
	return result, nil
}

// CacheStock caches a single stock with 24-hour TTL.
func (c *Cache) CacheStock(ctx context.Context, stock *domain.Stock) error {
	key := c.stockKey(stock.Symbol)
	jsonData, err := json.Marshal(stock)
	if err != nil {
		return fmt.Errorf("failed to marshal stock: %w", err)
	}
	if err := c.client.Set(ctx, key, jsonData, stockTTL).Err(); err != nil {
		return fmt.Errorf("failed to cache stock: %w", err)
	}
	return nil
}

// GetCachedStock retrieves a cached stock by symbol.
func (c *Cache) GetCachedStock(ctx context.Context, symbol string) (*domain.Stock, error) {
	key := c.stockKey(symbol)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get cached stock: %w", err)
	}
	var result domain.Stock
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached stock: %w", err)
	}
	return &result, nil
}

// InvalidateOHLCV removes OHLCV from cache.
func (c *Cache) InvalidateOHLCV(ctx context.Context, symbol string, startDate, endDate time.Time) error {
	key := c.ohlcvKey(symbol, startDate, endDate)
	return c.client.Del(ctx, key).Err()
}

// InvalidateStocks removes stock list from cache.
func (c *Cache) InvalidateStocks(ctx context.Context, exchange string) error {
	key := c.stockListKey(exchange)
	return c.client.Del(ctx, key).Err()
}

// Ping checks the Redis connection.
func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Get retrieves raw bytes from Redis. Returns nil slice on cache miss.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return data, err
}

// SetEX stores a value with expiration (SETEX semantics).
func (c *Cache) SetEX(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.SetEx(ctx, key, value, ttl).Err()
}

// RedisClient returns the underlying redis client for advanced use.
func (c *Cache) RedisClient() *redis.Client {
	return c.client
}

func (c *Cache) ohlcvKey(symbol string, start, end time.Time) string {
	return fmt.Sprintf("%s%s:%s:%s", keyPrefixOHLCV, symbol, start.Format("20060102"), end.Format("20060102"))
}

func (c *Cache) stockKey(symbol string) string {
	return keyPrefixStock + symbol
}

func (c *Cache) stockListKey(exchange string) string {
	if exchange == "" {
		exchange = "all"
	}
	return keyPrefixStockList + exchange
}
