# Cache Design — Sprint 1

## Overview

A two-layer cache-aside architecture reduces backtest latency from ~minutes to <5s by eliminating repeated DB queries during historical simulation.

```
Backtest Engine
    │
    ▼
pkg/data/cache.go  ← cache-aside facade
    │
    ├── Redis (pkg/storage/cache.go)   ← L1: hot data
    └── PostgreSQL (pkg/storage/postgres.go) ← L2: source of truth
```

## Redis Key Design

| Data        | Key Pattern                          | TTL                          |
|-------------|--------------------------------------|------------------------------|
| OHLCV bars  | `ohlcv:{symbol}:{start}:{end}`       | 1h (recent), 24h (historical) |
| Fundamentals| `fund:{symbol}:{date}`               | 24h                          |

- Keys use YYYYMMDD date format.
- OHLCV TTL is shorter for recent data (includes last 7 days) to stay fresh for live trading; longer for historical data.
- Fundamentals are less volatile → 24h TTL.

## L1 — Redis (`pkg/storage/cache.go`)

Low-level Redis wrapper using `go-redis/v9`. Provides:
- `Get` / `SetEX` — raw bytes with TTL (SETEX semantics)
- `CacheOHLCV` / `GetCachedOHLCV` — domain-level OHLCV caching (1h TTL)
- `CacheStocks` / `GetCachedStocks` — stock list caching (24h TTL)
- `Ping` — health check

## L2 — Cache-Aside Facade (`pkg/data/cache.go`)

`DataCache` wraps `storage.Cache` (Redis) + `storage.PostgresStore` (PostgreSQL):

```go
// Check Redis first → on miss, query PostgreSQL → cache result
func (dc *DataCache) GetOHLCV(ctx, symbol, start, end string) ([]domain.OHLCV, error)
func (dc *DataCache) SetOHLCV(ctx, symbol, start, end string, bars []domain.OHLCV) error
func (dc *DataCache) GetFundamentals(ctx, symbol, date string) ([]domain.Fundamental, error)
```

TTL selection is automatic based on data age:
- Data within last 7 days → 1h TTL
- Older data → 24h TTL

## Cache Warming

Called at the start of every backtest run:

```go
func (dc *DataCache) WarmCache(ctx, []symbols, start, end string) error
```

Iterates the stock universe, fetches OHLCV from PostgreSQL, and pre-writes to Redis.
On a warm cache, `GetOHLCV` serves entirely from Redis, avoiding per-symbol DB round-trips
during the tight backtest loop.

**Cost estimate:** For 4000 symbols × 250 trading days ≈ 1M rows → ~200–400ms to write Redis,
then ~0ms per symbol read during backtest.

## Health Checks (Docker Compose)

All services declare a health check; dependent services use `depends_on: condition: service_healthy`:

```yaml
postgres:  pg_isready -U postgres
redis:     redis-cli ping
Go services: curl -f localhost:{port}/health
```

This ensures data-service is fully up (DB migrations + Redis connection) before
strategy-service/risk-service start, and those are healthy before analysis-service.

## Interaction with data-service HTTP API

The backtest engine in `analysis-service` calls data-service over HTTP. The HTTP handler
in `cmd/data/main.go` uses `storage.Cache` directly. `pkg/data/cache.go` is used
for backtest cache warming and any direct cache-aside access from the backtest engine
(to pre-warm before the backtest loop begins).
