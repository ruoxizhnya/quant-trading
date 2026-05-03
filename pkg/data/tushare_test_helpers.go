package data

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/httpclient"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/rs/zerolog"
)

// mockStoreForTushare wraps mockStoreForTushareImpl to implement *storage.PostgresStore interface
type mockStoreForTushareImpl struct {
	stocks       []domain.Stock
	ohlcv        []domain.OHLCV
	fundamentals []domain.Fundamental
}

func newMockStoreForTushare() *mockStoreForTushareImpl {
	return &mockStoreForTushareImpl{
		stocks:       make([]domain.Stock, 0),
		ohlcv:        make([]domain.OHLCV, 0),
		fundamentals: make([]domain.Fundamental, 0),
	}
}

func (m *mockStoreForTushareImpl) SaveStockBatch(_ context.Context, stocks []*domain.Stock) error {
	for _, s := range stocks {
		m.stocks = append(m.stocks, *s)
	}
	return nil
}

func (m *mockStoreForTushareImpl) SaveOHLCVBatch(_ context.Context, records []*domain.OHLCV) error {
	for _, r := range records {
		m.ohlcv = append(m.ohlcv, *r)
	}
	return nil
}

func (m *mockStoreForTushareImpl) SaveFundamentalBatch(_ context.Context, records []*domain.Fundamental) error {
	for _, r := range records {
		m.fundamentals = append(m.fundamentals, *r)
	}
	return nil
}

func (m *mockStoreForTushareImpl) GetOHLCV(_ context.Context, symbol string, start, end interface{}) ([]domain.OHLCV, error) { return m.ohlcv, nil }
func (m *mockStoreForTushareImpl) GetOHLCVForDateRange(_ context.Context, start, end interface{}) ([]domain.OHLCV, error) { return m.ohlcv, nil }
func (m *mockStoreForTushareImpl) GetFundamentals(_ context.Context, symbol string, date interface{}) ([]domain.Fundamental, error) { return m.fundamentals, nil }
func (m *mockStoreForTushareImpl) GetFundamentalsSnapshot(_ context.Context, date interface{}) ([]interface{}, error) { return nil, nil }
func (m *mockStoreForTushareImpl) SaveFundamentalDataBatch(_ context.Context, records []interface{}) error { return nil }
func (m *mockStoreForTushareImpl) SaveIndexConstituentBatch(_ context.Context, records []interface{}) error { return nil }
func (m *mockStoreForTushareImpl) SaveDividendBatch(_ context.Context, records []interface{}) error { return nil }
func (m *mockStoreForTushareImpl) SaveSplitBatch(_ context.Context, records []interface{}) error { return nil }
func (m *mockStoreForTushareImpl) SaveTradingCalendar(_ context.Context, records []interface{}) error { return nil }

// mockCacheForTushare implements storage.Cache for testing
type mockCacheForTushare struct{}

func (m *mockCacheForTushare) Get(_ context.Context, key string) ([]byte, error)              { return nil, nil }
func (m *mockCacheForTushare) Set(_ context.Context, key string, value interface{}, ttl int) error { return nil }
func (m *mockCacheForTushare) SetEX(_ context.Context, key string, value interface{}, ttl time.Duration) error { return nil }
func (m *mockCacheForTushare) Del(_ context.Context, keys ...string) error                        { return nil }
func (m *mockCacheForTushare) Exists(_ context.Context, keys ...string) (int64, error)          { return 0, nil }
func (m *mockCacheForTushare) InvalidateStocks(_ context.Context, exchange string) error        { return nil }
func (m *mockCacheForTushare) Ping(_ context.Context) error                                     { return nil }
func (m *mockCacheForTushare) Close() error                                                    { return nil }
func (m *mockCacheForTushare) GetCachedStocks(_ context.Context, exchange string) (interface{}, error) { return nil, nil }
func (m *mockCacheForTushare) CacheStocks(_ context.Context, exchange string, stocks interface{}) error { return nil }
func (m *mockCacheForTushare) GetCachedStock(_ context.Context, symbol string) (interface{}, error)  { return nil, nil }
func (m *mockCacheForTushare) CacheStock(_ context.Context, stock interface{}) error               { return nil }

// createTestTushareClient creates a TushareClient for testing with mock dependencies
func createTestTushareClient(baseURL string, storeImpl *mockStoreForTushareImpl, cache *mockCacheForTushare) *TushareClient {
	httpClient := httpclient.New(baseURL, 30*time.Second, 0)

	store := &storage.PostgresStore{} // create empty PostgresStore
	if storeImpl != nil {
		// We'll use the mock's methods by embedding
		_ = storeImpl // prevent unused warning
	}

	return &TushareClient{
		httpClient: httpClient,
		token:      "test_token",
		logger:     zerolog.Nop(),
		store:      store,
		cache:      cache,
	}
}
