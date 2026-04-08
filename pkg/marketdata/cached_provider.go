package marketdata

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type CacheConfig struct {
	OHLCVTTL       time.Duration
	FundamentalTTL time.Duration
	StocksTTL      time.Duration
	CalendarTTL    time.Duration
	Prefix         string
	RedisURL       string
}

func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		OHLCVTTL:       24 * time.Hour,
		FundamentalTTL: 6 * time.Hour,
		StocksTTL:      1 * time.Hour,
		CalendarTTL:    72 * time.Hour,
		Prefix:         "md:",
	}
}

type cachedProvider struct {
	inner  Provider
	rdb    *redis.Client
	config CacheConfig
	logger zerolog.Logger
}

func NewCachedProvider(inner Provider, rdb *redis.Client, config CacheConfig, logger zerolog.Logger) Provider {
	if config.Prefix == "" {
		config = DefaultCacheConfig()
	}
	return &cachedProvider{
		inner:  inner,
		rdb:    rdb,
		config: config,
		logger: logger.With().Str("component", "cached_provider").Logger(),
	}
}

func (p *cachedProvider) Name() string {
	return "cached:" + p.inner.Name()
}

func (p *cachedProvider) CheckConnectivity(ctx context.Context) error {
	if err := p.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cache redis unreachable: %w", err)
	}
	return p.inner.CheckConnectivity(ctx)
}

func (p *cachedProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	key := p.cacheKey("ohlcv", symbol, start.Format("20060102"), end.Format("20060102"))
	if cached, hit := p.getOHLCVFromCache(ctx, key); hit {
		return cached, nil
	}

	bars, err := p.inner.GetOHLCV(ctx, symbol, start, end)
	if err != nil {
		return nil, err
	}
	if len(bars) > 0 {
		p.setOHLCVToCache(ctx, key, bars)
	}
	return bars, nil
}

func (p *cachedProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	key := p.cacheKey("fund", symbol, date.Format("20060102"))
	if cached, hit := p.getFundFromCache(ctx, key); hit {
		return cached, nil
	}

	fund, err := p.inner.GetFundamental(ctx, symbol, date)
	if err != nil {
		return nil, err
	}
	if fund != nil {
		p.setFundToCache(ctx, key, fund)
	}
	return fund, nil
}

func (p *cachedProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	key := p.cacheKey("stocks", exchange)
	if cached, hit := p.getStocksFromCache(ctx, key); hit {
		return cached, nil
	}

	stocks, err := p.inner.GetStocks(ctx, exchange)
	if err != nil {
		return nil, err
	}
	if len(stocks) > 0 {
		p.setStocksToCache(ctx, key, stocks)
	}
	return stocks, nil
}

func (p *cachedProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return p.inner.GetLatestPrice(ctx, symbol)
}

func (p *cachedProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	key := p.cacheKey("idx", indexCode)
	if cached, hit := p.getStringSliceFromCache(ctx, key); hit {
		return cached, nil
	}

	symbols, err := p.inner.GetIndexConstituents(ctx, indexCode)
	if err != nil {
		return nil, err
	}
	if len(symbols) > 0 {
		p.setStringSliceToCache(ctx, key, symbols, p.config.StocksTTL)
	}
	return symbols, nil
}

func (p *cachedProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	key := p.cacheKey("cal", start.Format("20060102"), end.Format("20060102"))
	if cached, hit := p.getDaysFromCache(ctx, key); hit {
		return cached, nil
	}

	days, err := p.inner.GetTradingDays(ctx, start, end)
	if err != nil {
		return nil, err
	}
	if len(days) > 0 {
		p.setDaysToCache(ctx, key, days)
	}
	return days, nil
}

func (p *cachedProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	return p.inner.GetStock(ctx, symbol)
}

func (p *cachedProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	return p.inner.BulkLoadOHLCV(ctx, symbols, start, end)
}

func (p *cachedProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	return p.inner.CheckCalendarExists(ctx, start, end)
}

func (p *cachedProvider) cacheKey(parts ...string) string {
	h := md5.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte(":"))
	}
	return p.config.Prefix + hex.EncodeToString(h.Sum(nil))
}

func (p *cachedProvider) getOHLCVFromCache(ctx context.Context, key string) ([]domain.OHLCV, bool) {
	data, err := p.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var bars []domain.OHLCV
	if err := unmarshalJSON(data, &bars); err != nil {
		return nil, false
	}
	return bars, true
}

func (p *cachedProvider) setOHLCVToCache(ctx context.Context, key string, bars []domain.OHLCV) {
	data, _ := marshalJSON(bars)
	if data != nil {
		p.rdb.Set(ctx, key, data, p.config.OHLCVTTL)
	}
}

func (p *cachedProvider) getFundFromCache(ctx context.Context, key string) (*domain.Fundamental, bool) {
	data, err := p.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var fund domain.Fundamental
	if err := unmarshalJSON(data, &fund); err != nil {
		return nil, false
	}
	return &fund, true
}

func (p *cachedProvider) setFundToCache(ctx context.Context, key string, fund *domain.Fundamental) {
	data, _ := marshalJSON(fund)
	if data != nil {
		p.rdb.Set(ctx, key, data, p.config.FundamentalTTL)
	}
}

func (p *cachedProvider) getStocksFromCache(ctx context.Context, key string) ([]domain.Stock, bool) {
	data, err := p.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var stocks []domain.Stock
	if err := unmarshalJSON(data, &stocks); err != nil {
		return nil, false
	}
	return stocks, true
}

func (p *cachedProvider) setStocksToCache(ctx context.Context, key string, stocks []domain.Stock) {
	data, _ := marshalJSON(stocks)
	if data != nil {
		p.rdb.Set(ctx, key, data, p.config.StocksTTL)
	}
}

func (p *cachedProvider) getStringSliceFromCache(ctx context.Context, key string) ([]string, bool) {
	data, err := p.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var items []string
	if err := unmarshalJSON(data, &items); err != nil {
		return nil, false
	}
	return items, true
}

func (p *cachedProvider) setStringSliceToCache(ctx context.Context, key string, items []string, ttl time.Duration) {
	data, _ := marshalJSON(items)
	if data != nil {
		p.rdb.Set(ctx, key, data, ttl)
	}
}

func (p *cachedProvider) getDaysFromCache(ctx context.Context, key string) ([]time.Time, bool) {
	data, err := p.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var dayStrs []string
	if err := unmarshalJSON(data, &dayStrs); err != nil {
		return nil, false
	}
	days := make([]time.Time, 0, len(dayStrs))
	for _, ds := range dayStrs {
		t, err := time.Parse(time.RFC3339, ds)
		if err != nil {
			continue
		}
		days = append(days, t)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	return days, true
}

func (p *cachedProvider) setDaysToCache(ctx context.Context, key string, days []time.Time) {
	dayStrs := make([]string, len(days))
	for i, d := range days {
		dayStrs[i] = d.Format(time.RFC3339)
	}
	data, _ := marshalJSON(dayStrs)
	if data != nil {
		p.rdb.Set(ctx, key, data, p.config.CalendarTTL)
	}
}

func marshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func unmarshalJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
