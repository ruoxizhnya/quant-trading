package marketdata

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

type inmemoryProvider struct {
	mu           sync.RWMutex
	ohlcv        map[string][]domain.OHLCV
	fund         map[string]map[time.Time]*domain.Fundamental
	stocks       []domain.Stock
	stockMap     map[string]domain.Stock
	prices       map[string]float64
	indexes      map[string][]string
	tradingDays  []time.Time
}

func NewInMemoryProvider() *inmemoryProvider {
	return &inmemoryProvider{
		ohlcv:       make(map[string][]domain.OHLCV),
		fund:        make(map[string]map[time.Time]*domain.Fundamental),
		stockMap:    make(map[string]domain.Stock),
		prices:      make(map[string]float64),
		indexes:     make(map[string][]string),
		tradingDays: []time.Time{},
	}
}

func (p *inmemoryProvider) LoadOHLCV(symbol string, bars []domain.OHLCV) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Date.Before(bars[j].Date)
	})
	p.ohlcv[symbol] = append(p.ohlcv[symbol][:0], bars...)
}

func (p *inmemoryProvider) LoadFundamentals(symbol string, data map[time.Time]*domain.Fundamental) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fund[symbol] == nil {
		p.fund[symbol] = make(map[time.Time]*domain.Fundamental)
	}
	for d, f := range data {
		p.fund[symbol][d] = f
	}
}

func (p *inmemoryProvider) LoadStocks(stocks []domain.Stock) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stocks = stocks
	for _, s := range stocks {
		p.stockMap[s.Symbol] = s
	}
}

func (p *inmemoryProvider) SetPrice(symbol string, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prices[symbol] = price
}

func (p *inmemoryProvider) SetIndexConstituents(indexCode string, symbols []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.indexes[indexCode] = symbols
}

func (p *inmemoryProvider) Name() string {
	return "inmemory"
}

func (p *inmemoryProvider) CheckConnectivity(ctx context.Context) error {
	return nil
}

func (p *inmemoryProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	p.mu.RLock()
	bars := p.ohlcv[symbol]
	p.mu.RUnlock()

	var result []domain.OHLCV
	for _, bar := range bars {
		if !bar.Date.Before(start) && !bar.Date.After(end) {
			result = append(result, bar)
		}
	}
	return result, nil
}

func (p *inmemoryProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	symbolData, ok := p.fund[symbol]
	if !ok {
		return nil, apperrors.NotFound("fundamental", symbol)
	}
	f, ok := symbolData[date]
	if !ok {
		return nil, apperrors.NotFound("fundamental", fmt.Sprintf("%s on %s", symbol, date.Format("2006-01-02")))
	}
	return f, nil
}

func (p *inmemoryProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if exchange == "" {
		return p.stocks, nil
	}

	var filtered []domain.Stock
	for _, s := range p.stocks {
		if s.Exchange == exchange {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func (p *inmemoryProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	p.mu.RLock()
	price, ok := p.prices[symbol]
	p.mu.RUnlock()

	if !ok {
		return 0, apperrors.NotFound("price", symbol)
	}
	return price, nil
}

func (p *inmemoryProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	p.mu.RLock()
	symbols, ok := p.indexes[indexCode]
	p.mu.RUnlock()

	if !ok {
		return nil, apperrors.NotFound("index", indexCode)
	}
	return symbols, nil
}

func (p *inmemoryProvider) SetTradingDays(days []time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tradingDays = days
}

func (p *inmemoryProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []time.Time
	for _, d := range p.tradingDays {
		if (d.Equal(start) || d.After(start)) && (d.Before(end) || d.Equal(end)) {
			result = append(result, d)
		}
	}
	if len(result) == 0 {
		result = p.tradingDays
	}
	return result, nil
}

func (p *inmemoryProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	p.mu.RLock()
	s, ok := p.stockMap[symbol]
	p.mu.RUnlock()

	if !ok {
		return domain.Stock{Symbol: symbol}, nil
	}
	return s, nil
}

func (p *inmemoryProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	data := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars, err := p.GetOHLCV(ctx, sym, start, end)
		if err != nil {
			continue
		}
		data[sym] = bars
	}
	return data, nil
}

func (p *inmemoryProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	p.mu.RLock()
	hasData := len(p.tradingDays) > 0
	p.mu.RUnlock()
	return hasData, nil
}
