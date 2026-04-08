package marketdata

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Provider defines the interface for accessing market data.
// Implementations can fetch from HTTP services, databases, or in-memory stores.
type Provider interface {
	GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error)
	GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error)
	GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error)
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)
	GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error)

	// Extended methods for backtest engine
	GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error)
	GetStock(ctx context.Context, symbol string) (domain.Stock, error)
	BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error)
	CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error)
}
