package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Mootdx unit tests ----------

func TestSplitMarketSymbol(t *testing.T) {
	tests := []struct {
		sym      string
		wantMkt  int
		wantCode string
		wantErr  bool
	}{
		{"600519.SH", 1, "600519", false},
		{"000001.SZ", 0, "000001", false},
		{"AAPL", 0, "AAPL", false},   // leading 'A' → SZ heuristic
		{"688017", 1, "688017", false}, // starts with 6 → SH
		{"300476", 0, "300476", false}, // starts with 3 → SZ
		{"", 0, "", true},
	}
	for _, tc := range tests {
		mkt, code, err := splitMarketSymbol(tc.sym)
		if tc.wantErr {
			assert.Error(t, err, "expected error for %q", tc.sym)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.wantMkt, mkt, "market mismatch for %q", tc.sym)
		assert.Equal(t, tc.wantCode, code, "code mismatch for %q", tc.sym)
	}
}

func TestParseMootdxCategory(t *testing.T) {
	tests := []struct {
		period string
		want   int
		ok     bool
	}{
		{"1m", 7, true},
		{"5m", 8, true},
		{"15m", 9, true},
		{"30m", 10, true},
		{"60m", 11, true},
		{"1d", 4, true},
		{"daily", 4, true},
		{"unknown", 0, false},
	}
	for _, tc := range tests {
		got, ok := parseMootdxCategory(tc.period)
		assert.Equal(t, tc.ok, ok, "ok mismatch for %q", tc.period)
		if ok {
			assert.Equal(t, tc.want, got, "category mismatch for %q", tc.period)
		}
	}
}

func TestBarCountFromWindow(t *testing.T) {
	assert.Equal(t, 800, barCountFromWindow(time.Now(), time.Now(), 4))
	assert.Equal(t, 240*5, barCountFromWindow(time.Now(), time.Now(), 7))
}

// fakeMootdxTransport is a unit-test transport for the mootdx adapter.
type fakeMootdxTransport struct {
	quotes    []MootdxQuote
	quotesErr error
	bars      []MootdxBar
	barsErr   error
}

func (f *fakeMootdxTransport) GetSecurityQuotes(_ context.Context, _ int, _ []string) ([]MootdxQuote, error) {
	return f.quotes, f.quotesErr
}
func (f *fakeMootdxTransport) GetSecurityBars(_ context.Context, _ int, _ string, _, _ int) ([]MootdxBar, error) {
	return f.bars, f.barsErr
}
func (f *fakeMootdxTransport) GetSecurityTransaction(_ context.Context, _ int, _, _ string) ([]MootdxTransaction, error) {
	return nil, nil
}
func (f *fakeMootdxTransport) GetFinanceSnapshot(_ context.Context, _ int, _ string) (*MootdxFinanceSnapshot, error) {
	return nil, nil
}
func (f *fakeMootdxTransport) Ping(_ context.Context) error { return nil }

func TestMootdxAdapter_Realtime(t *testing.T) {
	transport := &fakeMootdxTransport{
		quotes: []MootdxQuote{
			{
				Symbol:    "600519",
				Price:     1500.0,
				Open:      1495.0,
				High:      1510.0,
				Low:       1490.0,
				LastClose: 1490.0,
				Volume:    10000,
				Amount:    1.5e7,
				Bid1:      1499.5,
				Bid1Vol:   100,
				Ask1:      1500.5,
				Ask1Vol:   200,
				ServerTime: time.Now(),
			},
		},
	}
	a := NewMootdxAdapter(transport)
	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType: DataTypeRealtime,
		Symbols:  []string{"600519.SH"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "mootdx", resp.Source)
	require.Len(t, resp.Items, 1)
	item := resp.Items[0]
	assert.Equal(t, "600519.SH", item.Symbol)
	assert.Equal(t, 1500.0, item.Data["price"])
	assert.Equal(t, 1499.5, item.Data["bid1"])
}

func TestMootdxAdapter_DailyBars(t *testing.T) {
	transport := &fakeMootdxTransport{
		bars: []MootdxBar{
			{Open: 100, High: 105, Low: 99, Close: 104, Volume: 1000, Amount: 1e5, Date: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)},
		},
	}
	a := NewMootdxAdapter(transport)
	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeOHLCDaily,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Items, 1)
	item := resp.Items[0]
	assert.Equal(t, 100.0, item.Data["open"])
	assert.Equal(t, 104.0, item.Data["close"])
}

// ---------- Eastmoney unit tests ----------

func TestParseEastmoneyCapitalFlowKLines(t *testing.T) {
	// 19 fields: date, close, change_pct, main_net, main_buy, main_sell,
	// main_net_ratio, super_buy, super_sell, super_net, large_buy,
	// large_sell, large_net, medium_buy, medium_sell, medium_net,
	// small_buy, small_sell, small_net
	lines := []string{
		"2026-05-14,1500.00,1.5,12345678,20000000,7654322,12.34,15000000,5000000,10000000,8000000,2000000,6000000,4000000,1000000,3000000,2000000,1500000,500000",
		"2026-05-13,1480.00,0.5,-1000000,5000000,6000000,-5.5,2000000,3000000,-1000000,1500000,2500000,-1000000,1000000,2000000,-1000000,800000,1500000,-700000",
	}
	rows, err := parseEastmoneyCapitalFlowKLines(lines)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, 12345678.0, rows[0].MainNet)
	assert.Equal(t, 12.34, rows[0].MainNetRatio)
	assert.Equal(t, 10000000.0, rows[0].SuperNet)
	assert.Equal(t, 6000000.0, rows[0].LargeNet)
	assert.Equal(t, 3000000.0, rows[0].MediumNet)
	assert.Equal(t, 500000.0, rows[0].SmallNet)
	// Retail = -(super + large + medium + small)
	//        = -(10M + 6M + 3M + 0.5M) = -19.5M
	assert.InDelta(t, -19500000.0, rows[0].RetailNet, 1.0)
}

func TestSymbolToEastmoneySecid(t *testing.T) {
	tests := []struct {
		sym, want string
		wantErr   bool
	}{
		{"600519.SH", "1.600519", false},
		{"000001.SZ", "0.000001", false},
		{"300476", "0.300476", false},
		{"688017", "1.688017", false},
		{"", "", true},
	}
	for _, tc := range tests {
		got, err := symbolToEastmoneySecid(tc.sym)
		if tc.wantErr {
			assert.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

func TestEastmoneyAdapter_CapitalFlow_HTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request shape.
		assert.Equal(t, "1.600519", r.URL.Query().Get("secid"))
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]interface{}{
			"data": map[string]interface{}{
				"code": "1.600519",
				"name": "贵州茅台",
				"klines": []string{
					"2026-05-14,1500.00,1.5,12345678,20000000,7654322,12.34,15000000,5000000,10000000,8000000,2000000,6000000,4000000,1000000,3000000,2000000,1500000,500000",
				},
			},
			"rc": 0,
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client := NewEastmoneyClient()
	client.BaseURL = server.URL
	a := NewEastmoneyAdapter(client)
	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Items, 1)
	item := resp.Items[0]
	assert.Equal(t, "600519.SH", item.Symbol)
	assert.Equal(t, "daily", item.Data["period"])
	assert.Equal(t, 12345678.0, item.Data["main_net"])
}

// ---------- Registry unit tests ----------

func TestRegistry_RegisterAndFetch(t *testing.T) {
	reg := NewRegistry()
	// Add a fake adapter that always succeeds.
	ok := &testAdapter{name: "ok", supported: []string{DataTypeRealtime}}
	require.NoError(t, reg.Register(ok))
	// Add a fallback that always fails.
	bad := &testAdapter{
		name:      "bad",
		supported: []string{DataTypeRealtime},
		fetchErr:  errRetryable,
	}
	require.NoError(t, reg.Register(bad))
	// Reorder: bad is registered first (lower priority in fallback chain
	// because it was added first), so the registry should try bad then ok.
	resp, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now(),
		EndDate:   time.Now(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "ok", resp.Source)
}

func TestRegistry_UnsupportedDataType(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  "nonexistent",
		StartDate: time.Now(),
		EndDate:   time.Now(),
	})
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestRegistry_AllFail(t *testing.T) {
	reg := NewRegistry()
	bad := &testAdapter{
		name:      "bad",
		supported: []string{DataTypeRealtime},
		fetchErr:  errRetryable,
	}
	require.NoError(t, reg.Register(bad))
	_, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now(),
		EndDate:   time.Now(),
	})
	assert.Error(t, err)
}

// ---------- UnifiedDataPoint unit tests ----------

func TestUnifiedDataPoint_Deduplicate(t *testing.T) {
	now := time.Now()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", TradeTime: now, DataType: DataTypeGlobalOHLCV, Data: map[string]interface{}{"close": 100.0}},
		{Symbol: "AAPL", TradeTime: now, DataType: DataTypeGlobalOHLCV, Data: map[string]interface{}{"close": 101.0}},
		{Symbol: "MSFT", TradeTime: now, DataType: DataTypeGlobalOHLCV, Data: map[string]interface{}{"close": 200.0}},
	}
	deduped := Deduplicate(points)
	assert.Len(t, deduped, 2)
	// First occurrence is kept.
	assert.Equal(t, 100.0, deduped[0].Data["close"])
}

func TestUnifiedDataPoint_DeduplicateKey(t *testing.T) {
	p := UnifiedDataPoint{
		Symbol:    "AAPL",
		TradeTime: time.Date(2026, 5, 14, 9, 30, 0, 0, time.UTC),
		DataType:  DataTypeGlobalOHLCV,
	}
	key := p.DeduplicateKey()
	assert.True(t, strings.HasPrefix(key, "AAPL|global_ohlcv|"))
}

// ---------- helpers ----------

type testAdapter struct {
	name      string
	supported []string
	fetchErr  error
	items     []DataItem
}

var errRetryable = wrapRetryable("test retryable")

type retryableErr struct{ msg string }

func (e *retryableErr) Error() string { return e.msg }
func (e *retryableErr) Unwrap() error { return ErrUpstreamUnavailable }
func wrapRetryable(s string) error    { return &retryableErr{msg: s} }

func (a *testAdapter) Name() string                              { return a.name }
func (a *testAdapter) Type() AdapterType                         { return AdapterTypeHTTP }
func (a *testAdapter) Enabled() bool                             { return true }
func (a *testAdapter) SupportedTypes() []string                  { return a.supported }
func (a *testAdapter) Schema(_ string) (DataSchema, error)       { return DataSchema{}, nil }
func (a *testAdapter) HealthCheck(_ context.Context) error       { return nil }
func (a *testAdapter) RateLimit() RateLimitConfig                { return RateLimitConfig{} }
func (a *testAdapter) Fetch(_ context.Context, _ FetchRequest) (*FetchResponse, error) {
	if a.fetchErr != nil {
		return nil, a.fetchErr
	}
	return &FetchResponse{Source: a.name, Items: a.items}, nil
}
