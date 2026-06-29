package marketdata

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements Provider for testing
type mockProvider struct {
	name            string
	connectivityErr error
	ohlcvData       map[string][]domain.OHLCV
	fundamentals    map[string]map[time.Time]*domain.Fundamental
	stocks          []domain.Stock
	prices          map[string]float64
	indexConst      map[string][]string
	tradingDays     []time.Time
	stockMap        map[string]domain.Stock
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{
		name:         name,
		ohlcvData:    make(map[string][]domain.OHLCV),
		fundamentals: make(map[string]map[time.Time]*domain.Fundamental),
		prices:       make(map[string]float64),
		indexConst:   make(map[string][]string),
		stockMap:     make(map[string]domain.Stock),
	}
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) CheckConnectivity(ctx context.Context) error {
	return m.connectivityErr
}

func (m *mockProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	if m.connectivityErr != nil {
		return nil, m.connectivityErr
	}
	data, ok := m.ohlcvData[symbol]
	if !ok {
		return nil, errors.New("not found")
	}
	var result []domain.OHLCV
	for _, bar := range data {
		if !bar.Date.Before(start) && !bar.Date.After(end) {
			result = append(result, bar)
		}
	}
	return result, nil
}

func (m *mockProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	if m.connectivityErr != nil {
		return nil, m.connectivityErr
	}
	symData, ok := m.fundamentals[symbol]
	if !ok {
		return nil, errors.New("not found")
	}
	f, ok := symData[date]
	if !ok {
		return nil, errors.New("not found")
	}
	return f, nil
}

func (m *mockProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	if m.connectivityErr != nil {
		return nil, m.connectivityErr
	}
	if exchange == "" {
		return m.stocks, nil
	}
	var filtered []domain.Stock
	for _, s := range m.stocks {
		if s.Exchange == exchange {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func (m *mockProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if m.connectivityErr != nil {
		return 0, m.connectivityErr
	}
	price, ok := m.prices[symbol]
	if !ok {
		return 0, errors.New("not found")
	}
	return price, nil
}

func (m *mockProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	if m.connectivityErr != nil {
		return nil, m.connectivityErr
	}
	symbols, ok := m.indexConst[indexCode]
	if !ok {
		return nil, errors.New("not found")
	}
	return symbols, nil
}

func (m *mockProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	if m.connectivityErr != nil {
		return nil, m.connectivityErr
	}
	var result []time.Time
	for _, d := range m.tradingDays {
		if !d.Before(start) && !d.After(end) {
			result = append(result, d)
		}
	}
	return result, nil
}

func (m *mockProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	if m.connectivityErr != nil {
		return domain.Stock{}, m.connectivityErr
	}
	stock, ok := m.stockMap[symbol]
	if !ok {
		return domain.Stock{Symbol: symbol}, nil
	}
	return stock, nil
}

func (m *mockProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	if m.connectivityErr != nil {
		return nil, m.connectivityErr
	}
	result := make(map[string][]domain.OHLCV)
	for _, sym := range symbols {
		data, err := m.GetOHLCV(ctx, sym, start, end)
		if err != nil {
			continue
		}
		result[sym] = data
	}
	return result, nil
}

func (m *mockProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	if m.connectivityErr != nil {
		return false, m.connectivityErr
	}
	return len(m.tradingDays) > 0, nil
}

func TestDataAdapter_NewDataAdapter(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	logger := zerolog.Nop()

	adapter := NewDataAdapter(bus, primary, nil, logger)
	require.NotNil(t, adapter)
	assert.Equal(t, "primary", adapter.Primary())
	assert.Equal(t, "adapter:primary", adapter.Name())
}

func TestDataAdapter_NewDataAdapter_NilPrimary(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	logger := zerolog.Nop()
	adapter := NewDataAdapter(bus, nil, nil, logger)
	require.NotNil(t, adapter)
	assert.Equal(t, "unknown", adapter.Primary())
}

func TestDataAdapter_SetPrimary(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	newPrimary := newMockProvider("new_primary")
	logger := zerolog.Nop()

	adapter := NewDataAdapter(bus, primary, nil, logger)

	err := adapter.SetPrimary("new_primary", newPrimary)
	require.NoError(t, err)
	assert.Equal(t, "new_primary", adapter.Primary())
}

func TestDataAdapter_SetPrimary_NilProvider(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	logger := zerolog.Nop()

	adapter := NewDataAdapter(bus, primary, nil, logger)

	err := adapter.SetPrimary("nil", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestDataAdapter_SetPrimary_ConnectivityFail(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	badProvider := newMockProvider("bad")
	badProvider.connectivityErr = errors.New("connection failed")
	logger := zerolog.Nop()

	adapter := NewDataAdapter(bus, primary, nil, logger)

	err := adapter.SetPrimary("bad", badProvider)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connectivity check failed")
}

func TestDataAdapter_GetOHLCV_Primary(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	primary.ohlcvData["A"] = []domain.OHLCV{
		{Symbol: "A", Date: day, Close: 100},
	}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	bars, err := adapter.GetOHLCV(context.Background(), "A", day, day)
	require.NoError(t, err)
	assert.Len(t, bars, 1)
	assert.Equal(t, 100.0, bars[0].Close)
}

func TestDataAdapter_GetOHLCV_Fallback(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.connectivityErr = errors.New("primary down")

	fallback := newMockProvider("fallback")
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fallback.ohlcvData["A"] = []domain.OHLCV{
		{Symbol: "A", Date: day, Close: 200},
	}

	adapter := NewDataAdapter(bus, primary, fallback, zerolog.Nop())

	bars, err := adapter.GetOHLCV(context.Background(), "A", day, day)
	require.NoError(t, err)
	assert.Len(t, bars, 1)
	assert.Equal(t, 200.0, bars[0].Close)
}

func TestDataAdapter_GetFundamental(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	primary.fundamentals["A"] = map[time.Time]*domain.Fundamental{
		date: {Symbol: "A", Date: date, PE: 15.5},
	}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	fund, err := adapter.GetFundamental(context.Background(), "A", date)
	require.NoError(t, err)
	assert.Equal(t, 15.5, fund.PE)
}

func TestDataAdapter_GetStocks(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.stocks = []domain.Stock{
		{Symbol: "A", Exchange: "SH"},
		{Symbol: "B", Exchange: "SZ"},
	}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	stocks, err := adapter.GetStocks(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, stocks, 2)
}

func TestDataAdapter_GetLatestPrice(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.prices["A"] = 100.5

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	price, err := adapter.GetLatestPrice(context.Background(), "A")
	require.NoError(t, err)
	assert.Equal(t, 100.5, price)
}

func TestDataAdapter_GetIndexConstituents(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.indexConst["000300.SH"] = []string{"A", "B", "C"}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	symbols, err := adapter.GetIndexConstituents(context.Background(), "000300.SH")
	require.NoError(t, err)
	assert.Len(t, symbols, 3)
}

func TestDataAdapter_GetTradingDays(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.tradingDays = []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	days, err := adapter.GetTradingDays(context.Background(),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Len(t, days, 2)
}

func TestDataAdapter_GetStock(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.stockMap["A"] = domain.Stock{Symbol: "A", Name: "Test"}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	stock, err := adapter.GetStock(context.Background(), "A")
	require.NoError(t, err)
	assert.Equal(t, "Test", stock.Name)
}

func TestDataAdapter_BulkLoadOHLCV(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	primary.ohlcvData["A"] = []domain.OHLCV{{Symbol: "A", Date: day, Close: 100}}
	primary.ohlcvData["B"] = []domain.OHLCV{{Symbol: "B", Date: day, Close: 200}}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	data, err := adapter.BulkLoadOHLCV(context.Background(), []string{"A", "B"}, day, day)
	require.NoError(t, err)
	assert.Len(t, data, 2)
}

func TestDataAdapter_CheckCalendarExists(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.tradingDays = []time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	exists, err := adapter.CheckCalendarExists(context.Background(),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestDataAdapter_CheckConnectivity(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	err := adapter.CheckConnectivity(context.Background())
	require.NoError(t, err)
}

func TestDataAdapter_Stop(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	assert.False(t, adapter.Stopped())
	adapter.Stop()
	assert.True(t, adapter.Stopped())
}

func TestDataAdapter_PushHistoricalData(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	primary.tradingDays = []time.Time{day1, day2}
	primary.ohlcvData["A"] = []domain.OHLCV{
		{Symbol: "A", Date: day1, Close: 100},
		{Symbol: "A", Date: day2, Close: 101},
	}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	var receivedEvents atomic.Int32
	bus.Subscribe(EventTypeOHLCV, func(event DataEvent) {
		receivedEvents.Add(1)
	})

	err := adapter.PushHistoricalData(context.Background(), []string{"A"}, day1, day2)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(2), receivedEvents.Load())
}

func TestDataAdapter_PushHistoricalData_ContextCancelled(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	primary.tradingDays = []time.Time{day1}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.PushHistoricalData(ctx, []string{"A"}, day1, day1)
	assert.Error(t, err)
}

func TestDataAdapter_PushHistoricalData_Stopped(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	primary.tradingDays = []time.Time{day1}

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())
	adapter.Stop()

	err := adapter.PushHistoricalData(context.Background(), []string{"A"}, day1, day1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stopped")
}

func TestDataAdapter_StartRealtime(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	err := adapter.StartRealtime(context.Background(), []string{"A"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestDataAdapter_ResolveProvider_FallbackWhenPrimaryUnhealthy(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.connectivityErr = errors.New("unhealthy")

	fallback := newMockProvider("fallback")
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fallback.ohlcvData["A"] = []domain.OHLCV{{Symbol: "A", Date: day, Close: 200}}

	adapter := NewDataAdapter(bus, primary, fallback, zerolog.Nop())

	bars, err := adapter.GetOHLCV(context.Background(), "A", day, day)
	require.NoError(t, err)
	assert.Equal(t, 200.0, bars[0].Close)
}

func TestDataAdapter_ResolveProvider_NoFallback(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()

	primary := newMockProvider("primary")
	primary.connectivityErr = errors.New("unhealthy")

	adapter := NewDataAdapter(bus, primary, nil, zerolog.Nop())

	_, err := adapter.GetOHLCV(context.Background(), "A", time.Now(), time.Now())
	assert.Error(t, err)
}
