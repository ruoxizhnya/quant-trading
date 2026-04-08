package marketdata

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

type DataAdapter struct {
	bus       *EventBus
	primary   Provider
	fallback  Provider
	logger    zerolog.Logger

	switchMu   sync.RWMutex
	currentName string

	stopCh   chan struct{}
	stopped  bool
	stopOnce sync.Once
}

func NewDataAdapter(bus *EventBus, primary, fallback Provider, logger zerolog.Logger) *DataAdapter {
	name := "unknown"
	if primary != nil {
		name = primary.Name()
	}
	return &DataAdapter{
		bus:         bus,
		primary:     primary,
		fallback:    fallback,
		logger:      logger.With().Str("component", "data_adapter").Logger(),
		currentName: name,
		stopCh:      make(chan struct{}),
	}
}

func (a *DataAdapter) Primary() string {
	a.switchMu.RLock()
	defer a.switchMu.RUnlock()
	return a.currentName
}

func (a *DataAdapter) SetPrimary(name string, p Provider) error {
	if p == nil {
		return fmt.Errorf("provider cannot be nil")
	}
	if err := p.CheckConnectivity(context.Background()); err != nil {
		return fmt.Errorf("new provider %q connectivity check failed: %w", name, err)
	}

	a.switchMu.Lock()
	oldPrimary := a.primary
	a.primary = p
	a.currentName = name
	a.switchMu.Unlock()

	a.bus.Publish(DataEvent{
		Type:   EventTypeSourceSwitch,
		Source: name,
		Payload: map[string]string{"old": oldPrimary.Name(), "new": name},
	})

	a.logger.Info().
		Str("old", oldPrimary.Name()).
		Str("new", name).
		Msg("Data source switched")

	return nil
}

func (a *DataAdapter) PushHistoricalData(ctx context.Context, symbols []string, start, end time.Time) error {
	a.logger.Info().
		Int("symbols", len(symbols)).
		Time("start", start).
		Time("end", end).
		Msg("Pushing historical data to EventBus")

	p := a.resolveProvider(ctx)

	tradingDays, err := p.GetTradingDays(ctx, start, end)
	if err != nil {
		return fmt.Errorf("failed to get trading days: %w", err)
	}
	sort.Slice(tradingDays, func(i, j int) bool { return tradingDays[i].Before(tradingDays[j]) })

	for _, date := range tradingDays {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.stopCh:
			return fmt.Errorf("adapter stopped")
		default:
		}

		dayStart := date
		dayEnd := date.Add(24*time.Hour - time.Nanosecond)

		for _, symbol := range symbols {
			bars, err := p.GetOHLCV(ctx, symbol, dayStart, dayEnd)
			if err != nil {
				a.logger.Debug().Str("symbol", symbol).Err(err).Msg("Skip symbol for this day")
				continue
			}
			for _, bar := range bars {
				if bar.Date.Equal(date) || (bar.Date.After(dayStart) && bar.Date.Before(dayEnd)) {
					a.bus.Publish(DataEvent{
						Type:      EventTypeOHLCV,
						Symbol:    symbol,
						Timestamp: bar.Date.Unix(),
						Payload:   bar,
						Source:    a.Primary(),
					})
				}
			}
		}

		a.bus.PublishSync(DataEvent{
			Type:      EventTypeTradeCal,
			Timestamp: date.Unix(),
			Payload:   date,
			Source:    a.Primary(),
		})
	}

	a.logger.Info().
		Int("symbols", len(symbols)).
		Int("days", len(tradingDays)).
		Msg("Historical data push complete")

	return nil
}

func (a *DataAdapter) StartRealtime(ctx context.Context, symbols []string) error {
	a.logger.Warn().Msg("StartRealtime not yet implemented — requires real-time data feed")
	return fmt.Errorf("realtime mode not implemented (Phase 4)")
}

func (a *DataAdapter) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		a.stopped = true
	})
}

func (a *DataAdapter) Stopped() bool {
	return a.stopped
}

func (a *DataAdapter) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	p := a.resolveProvider(ctx)
	bars, err := p.GetOHLCV(ctx, symbol, start, end)
	if err != nil && a.fallback != nil && p == a.primary {
		a.logger.Warn().Str("symbol", symbol).Err(err).Msg("Primary failed, trying fallback")
		bars, err = a.fallback.GetOHLCV(ctx, symbol, start, end)
	}
	return bars, err
}

func (a *DataAdapter) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	p := a.resolveProvider(ctx)
	fund, err := p.GetFundamental(ctx, symbol, date)
	if err != nil && a.fallback != nil && p == a.primary {
		a.logger.Warn().Str("symbol", symbol).Err(err).Msg("Primary failed, trying fallback")
		fund, err = a.fallback.GetFundamental(ctx, symbol, date)
	}
	return fund, err
}

func (a *DataAdapter) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	p := a.resolveProvider(ctx)
	stocks, err := p.GetStocks(ctx, exchange)
	if err != nil && a.fallback != nil && p == a.primary {
		stocks, err = a.fallback.GetStocks(ctx, exchange)
	}
	return stocks, err
}

func (a *DataAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	p := a.resolveProvider(ctx)
	price, err := p.GetLatestPrice(ctx, symbol)
	if err != nil && a.fallback != nil && p == a.primary {
		price, err = a.fallback.GetLatestPrice(ctx, symbol)
	}
	return price, err
}

func (a *DataAdapter) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	p := a.resolveProvider(ctx)
	symbols, err := p.GetIndexConstituents(ctx, indexCode)
	if err != nil && a.fallback != nil && p == a.primary {
		symbols, err = a.fallback.GetIndexConstituents(ctx, indexCode)
	}
	return symbols, err
}

func (a *DataAdapter) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	p := a.resolveProvider(ctx)
	days, err := p.GetTradingDays(ctx, start, end)
	if err != nil && a.fallback != nil && p == a.primary {
		days, err = a.fallback.GetTradingDays(ctx, start, end)
	}
	return days, err
}

func (a *DataAdapter) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	p := a.resolveProvider(ctx)
	stock, err := p.GetStock(ctx, symbol)
	if err != nil && a.fallback != nil && p == a.primary {
		stock, err = a.fallback.GetStock(ctx, symbol)
	}
	return stock, err
}

func (a *DataAdapter) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	p := a.resolveProvider(ctx)
	data, err := p.BulkLoadOHLCV(ctx, symbols, start, end)
	if err != nil && a.fallback != nil && p == a.primary {
		a.logger.Warn().Err(err).Msg("Primary bulk load failed, trying fallback")
		data, err = a.fallback.BulkLoadOHLCV(ctx, symbols, start, end)
	}
	return data, err
}

func (a *DataAdapter) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	p := a.resolveProvider(ctx)
	ok, err := p.CheckCalendarExists(ctx, start, end)
	if err != nil && a.fallback != nil && p == a.primary {
		ok, err = a.fallback.CheckCalendarExists(ctx, start, end)
	}
	return ok, err
}

func (a *DataAdapter) CheckConnectivity(ctx context.Context) error {
	return a.resolveProvider(ctx).CheckConnectivity(ctx)
}

func (a *DataAdapter) Name() string {
	return "adapter:" + a.Primary()
}

func (a *DataAdapter) resolveProvider(ctx context.Context) Provider {
	a.switchMu.RLock()
	primary := a.primary
	a.switchMu.RUnlock()

	if primary == nil {
		return a.fallback
	}
	if err := primary.CheckConnectivity(ctx); err != nil {
		if a.fallback != nil {
			a.logger.Warn().
				Str("primary", primary.Name()).
				Err(err).
				Str("fallback", a.fallback.Name()).
				Msg("Primary unhealthy, using fallback")
			return a.fallback
		}
		a.logger.Warn().
			Str("primary", primary.Name()).
			Err(err).
			Msg("Primary unhealthy but no fallback available")
	}
	return primary
}
