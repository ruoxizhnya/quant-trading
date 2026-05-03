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

// RedisCache implements the Cache interface using Redis.
type RedisCache struct {
	client *redis.Client
	logger zerolog.Logger
}

// NewCache creates a new Redis cache client.
func NewCache(redisURL string) (*RedisCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opts)
	logger := logging.WithContext(map[string]any{"component": "redis_cache"})

	ctx, cancel := context.WithTimeout(context.Background(), RedisConnectTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info().Msg("Redis connection established")
	return &RedisCache{client: client, logger: logger}, nil
}

// Close closes the Redis connection.
func (c *RedisCache) Close() error {
	if c.client == nil {
		return nil
	}
	err := c.client.Close()
	c.logger.Info().Msg("Redis connection closed")
	return err
}

// Ping checks the Redis connection.
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Get retrieves a value from Redis by key.
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}
	return val, nil
}

// Set stores a value in Redis with TTL in seconds.
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttlSeconds int) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	return c.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err()
}

// SetEX stores a value in Redis with TTL as duration.
func (c *RedisCache) SetEX(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// Del deletes one or more keys from Redis.
func (c *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(ctx, keys...).Err()
}

// Exists checks if one or more keys exist in Redis.
func (c *RedisCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	return c.client.Exists(ctx, keys...).Result()
}

// InvalidateStocks invalidates stock-related cache entries.
func (c *RedisCache) InvalidateStocks(ctx context.Context, exchange string) error {
	pattern := "stocks:list:*"
	if exchange != "" && exchange != "all" {
		pattern = "stocks:list:" + exchange
	}

	var cursor uint64
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %w", err)
		}
		if len(keys) > 0 {
			if err := c.Del(ctx, keys...); err != nil {
				c.logger.Warn().Err(err).Int("count", len(keys)).Msg("Failed to delete some cache keys")
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	c.logger.Info().Str("exchange", exchange).Msg("Stock cache invalidated")
	return nil
}

// GetCachedStocks retrieves cached stocks list from Redis for an exchange.
func (c *RedisCache) GetCachedStocks(ctx context.Context, exchange string) (interface{}, error) {
	key := "stocks:list:" + exchange
	data, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var stocks []interface{}
	if err := json.Unmarshal(data, &stocks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stocks list: %w", err)
	}
	return stocks, nil
}

// CacheStocks stores a stocks list in Redis with TTL for an exchange.
func (c *RedisCache) CacheStocks(ctx context.Context, exchange string, stocks interface{}) error {
	key := "stocks:list:" + exchange
	data, err := json.Marshal(stocks)
	if err != nil {
		return fmt.Errorf("failed to marshal stocks list: %w", err)
	}
	return c.client.Set(ctx, key, data, 24*time.Hour).Err()
}

// GetCachedStock retrieves a single cached stock from Redis.
func (c *RedisCache) GetCachedStock(ctx context.Context, symbol string) (interface{}, error) {
	key := "stock:" + symbol
	data, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var stock interface{}
	if err := json.Unmarshal(data, &stock); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stock: %w", err)
	}
	return stock, nil
}

// CacheStock stores a single stock in Redis with TTL.
func (c *RedisCache) CacheStock(ctx context.Context, stock interface{}) error {
	key := ""
	switch s := stock.(type) {
	case *domain.Stock:
		key = "stock:" + s.Symbol
	case domain.Stock:
		key = "stock:" + s.Symbol
	case map[string]interface{}:
		if sym, ok := s["symbol"].(string); ok {
			key = "stock:" + sym
		} else if sym, ok := s["Symbol"].(string); ok {
			key = "stock:" + sym
		}
	default:
		return fmt.Errorf("invalid stock format: unsupported type %T", stock)
	}
	if key == "" {
		return fmt.Errorf("invalid stock format: missing symbol")
	}
	data, err := json.Marshal(stock)
	if err != nil {
		return fmt.Errorf("failed to marshal stock: %w", err)
	}
	return c.client.Set(ctx, key, data, 24*time.Hour).Err()
}

// RedisClient returns the underlying Redis client.
func (c *RedisCache) RedisClient() *redis.Client {
	return c.client
}

// CacheOHLCV caches OHLCV data for a symbol in a date range.
func (c *RedisCache) CacheOHLCV(ctx context.Context, symbol string, start, end time.Time, bars interface{}) error {
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixOHLCV, symbol, start.Format("20060102"), end.Format("20060102"))
	data, err := json.Marshal(bars)
	if err != nil {
		return fmt.Errorf("failed to marshal OHLCV: %w", err)
	}
	return c.client.Set(ctx, key, data, 24*time.Hour).Err()
}

// GetCachedOHLCV retrieves cached OHLCV data for a symbol in a date range.
func (c *RedisCache) GetCachedOHLCV(ctx context.Context, symbol string, start, end time.Time) (interface{}, error) {
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixOHLCV, symbol, start.Format("20060102"), end.Format("20060102"))
	data, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var bars interface{}
	if err := json.Unmarshal(data, &bars); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OHLCV: %w", err)
	}
	return bars, nil
}

// InvalidateOHLCV invalidates cached OHLCV data for a symbol.
func (c *RedisCache) InvalidateOHLCV(ctx context.Context, symbol string, start, end time.Time) error {
	pattern := fmt.Sprintf("%s%s:*:*", KeyPrefixOHLCV, symbol)
	var cursor uint64
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %w", err)
		}
		if len(keys) > 0 {
			c.Del(ctx, keys...)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}
