package backtest

// S7-P2-1: fakeProvider test stub extracted from cache_test.go when
// CacheManager moved to pkg/backtest/cache/. The cache/ subpackage
// keeps its own copy (Go test files are package-scoped). This copy
// serves the parent-package tests that need a marketdata.Provider
// stub — notably options_test.go's NewEngineWithOptions tests.

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// fakeProvider — test marketdata.Provider stub that records bulk /
// single OHLCV calls. Used by parent-package tests that construct an
// Engine without a real data source.
type fakeProvider struct {
	bulkCalls   [][]string
	singleCalls []string
	bulkData    map[string][]domain.OHLCV
	singleData  map[string][]domain.OHLCV
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		bulkData:   make(map[string][]domain.OHLCV),
		singleData: make(map[string][]domain.OHLCV),
	}
}

func (f *fakeProvider) Name() string                                { return "fake" }
func (f *fakeProvider) CheckConnectivity(ctx context.Context) error { return nil }
func (f *fakeProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	return nil, nil
}
func (f *fakeProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	return nil, nil
}
func (f *fakeProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (f *fakeProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	return nil, nil
}
func (f *fakeProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	f.bulkCalls = append(f.bulkCalls, symbols)
	result := make(map[string][]domain.OHLCV)
	for _, s := range symbols {
		if bars, ok := f.bulkData[s]; ok {
			result[s] = bars
		}
	}
	return result, nil
}

func (f *fakeProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	f.singleCalls = append(f.singleCalls, symbol)
	return f.singleData[symbol], nil
}

func (f *fakeProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	return nil, nil
}
func (f *fakeProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	return domain.Stock{}, nil
}
func (f *fakeProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	return true, nil
}

// Compile-time check
var _ marketdata.Provider = (*fakeProvider)(nil)
