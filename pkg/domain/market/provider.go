package market

import (
	"context"
	"time"
)

// Provider defines the interface for accessing market data.
//
// Renamed from domain.MarketDataProvider during S7-P3-4 migration.
// Since no external consumers existed at migration time, the rename
// is safe. The legacy name remains accessible as
// `domain.MarketDataProvider` via type alias.
type Provider interface {
	GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]OHLCV, error)
	GetFundamental(ctx context.Context, symbol string, date time.Time) (*Fundamental, error)
	GetStocks(ctx context.Context, exchange string) ([]Stock, error)
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)
	GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error)
}
