package storage

import (
	"context"
	"time"
)

// Cache defines the interface for Redis cache operations.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value interface{}, ttlSeconds int) error
	SetEX(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (int64, error)
	InvalidateStocks(ctx context.Context, exchange string) error
	Ping(ctx context.Context) error
	Close() error
	GetCachedStocks(ctx context.Context, exchange string) (interface{}, error)
	CacheStocks(ctx context.Context, exchange string, stocks interface{}) error
	GetCachedStock(ctx context.Context, symbol string) (interface{}, error)
	CacheStock(ctx context.Context, stock interface{}) error
}
