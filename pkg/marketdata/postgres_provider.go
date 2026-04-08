package marketdata

import (
	"context"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type postgresProvider struct {
	store  *storage.PostgresStore
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

func NewPostgresProvider(store *storage.PostgresStore, logger zerolog.Logger) Provider {
	return &postgresProvider{
		store:  store,
		pool:  store.DB(),
		logger: logger.With().Str("component", "postgres_provider").Logger(),
	}
}

func (p *postgresProvider) Name() string {
	return "postgres"
}

func (p *postgresProvider) CheckConnectivity(ctx context.Context) error {
	return p.store.Ping(ctx)
}

func (p *postgresProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	bars, err := p.store.GetOHLCV(ctx, symbol, start, end)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal, "postgres get ohlcv failed", "GetOHLCV")
	}
	if len(bars) == 0 {
		return nil, nil
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date.Before(bars[j].Date) })
	return bars, nil
}

func (p *postgresProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	funds, err := p.store.GetFundamentals(ctx, symbol, date)
	if err != nil {
		return nil, err
	}
	if len(funds) == 0 {
		fund, err := p.store.GetFundamental(ctx, symbol, date)
		if err != nil {
			return nil, apperrors.NotFound("fundamental", symbol)
		}
		return fund, nil
	}
	var best *domain.Fundamental
	for i := range funds {
		if !funds[i].Date.After(date) {
			best = &funds[i]
		}
	}
	if best == nil {
		best = &funds[len(funds)-1]
	}
	return best, nil
}

func (p *postgresProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	stocks, err := p.store.GetStocks(ctx, exchange)
	if err != nil {
		return nil, err
	}
	if len(stocks) == 0 {
		stocks, err = p.store.GetAllStocks(ctx)
		if err != nil {
			return nil, err
		}
	}
	return stocks, nil
}

func (p *postgresProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -5)
	bars, err := p.store.GetOHLCV(ctx, symbol, start, end)
	if err != nil {
		return 0, err
	}
	if len(bars) == 0 {
		return 0, apperrors.NotFound("OHLCV", symbol)
	}
	return bars[len(bars)-1].Close, nil
}

func (p *postgresProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	constituents, err := p.store.GetIndexConstituents(ctx, indexCode)
	if err != nil {
		return nil, err
	}
	symbols := make([]string, len(constituents))
	for i, c := range constituents {
		symbols[i] = c.Symbol
	}
	return symbols, nil
}

func (p *postgresProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	days, err := p.store.GetTradingDates(ctx, start, end)
	if err != nil {
		return nil, err
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	return days, nil
}

func (p *postgresProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	stock, err := p.store.GetStock(ctx, symbol)
	if err != nil {
		return domain.Stock{Symbol: symbol}, nil
	}
	if stock == nil {
		return domain.Stock{Symbol: symbol}, nil
	}
	return *stock, nil
}

func (p *postgresProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	data := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars, err := p.store.GetOHLCV(ctx, sym, start, end)
		if err != nil {
			p.logger.Warn().Str("symbol", sym).Err(err).Msg("BulkLoadOHLCV skip symbol")
			continue
		}
		if len(bars) > 0 {
			sort.Slice(bars, func(i, j int) bool { return bars[i].Date.Before(bars[j].Date) })
			data[sym] = bars
		}
	}
	return data, nil
}

func (p *postgresProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	days, err := p.store.GetTradingDates(ctx, start, end)
	if err != nil {
		return false, err
	}
	return len(days) > 0, nil
}
