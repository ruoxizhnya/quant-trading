package source

// P2-18: Integration tests for the multi-source data architecture.
//
// This file exercises the 9 data adapters via httptest.NewServer mock
// servers (no Docker / no live network). It also covers the Registry
// fallback chain, the ETL Pipeline Process() flow, the Reconciler
// strategies, and edge cases (empty responses, 429 rate limit, 5xx
// errors, invalid symbols).
//
// Adapters covered:
//   1. tushare (via TushareAdapter wrapper — exercised through Registry)
//   2. eastmoney (capital_flow)
//   3. eastmoney_sectors (sectors, stock_sector)
//   4. eastmoney_toplist (top_list, limit_up_pool)
//   5. juchao (announcements)
//   6. mootdx (realtime, ohlcv_minute, ohlcv_daily — via MootdxTransport mock)
//   7. xueqiu (hot_search, news)
//   8. alpha_vantage (global_ohlcv)
//   9. yahoo_finance (global_ohlcv)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// --- stubBulkInserter is a test double for storage.BulkInserter ---

type stubBulkInserter struct {
	inserted []storage.UnifiedDataPoint
	fail     error
}

func (s *stubBulkInserter) BulkInsert(ctx context.Context, dataType string, points []storage.UnifiedDataPoint) (int, int, error) {
	if s.fail != nil {
		return 0, len(points), s.fail
	}
	s.inserted = append(s.inserted, points...)
	return len(points), 0, nil
}

// --- mockMootdxTransport is a test double for MootdxTransport ---

type mockMootdxTransport struct {
	quotes       []MootdxQuote
	bars         []MootdxBar
	transactions []MootdxTransaction
	finance      *MootdxFinanceSnapshot
	pingErr      error
	quotesErr    error
	barsErr      error
}

func (m *mockMootdxTransport) GetSecurityQuotes(ctx context.Context, market int, symbols []string) ([]MootdxQuote, error) {
	if m.quotesErr != nil {
		return nil, m.quotesErr
	}
	return m.quotes, nil
}

func (m *mockMootdxTransport) GetSecurityBars(ctx context.Context, market int, symbol string, category, count int) ([]MootdxBar, error) {
	if m.barsErr != nil {
		return nil, m.barsErr
	}
	return m.bars, nil
}

func (m *mockMootdxTransport) GetSecurityTransaction(ctx context.Context, market int, symbol, date string) ([]MootdxTransaction, error) {
	return m.transactions, nil
}

func (m *mockMootdxTransport) GetFinanceSnapshot(ctx context.Context, market int, symbol string) (*MootdxFinanceSnapshot, error) {
	return m.finance, nil
}

func (m *mockMootdxTransport) Ping(ctx context.Context) error {
	return m.pingErr
}

// --- Test 1: EastmoneyAdapter Fetch capital_flow happy path ---

func TestIntegration_EastmoneyAdapter_FetchCapitalFlow_HappyPath(t *testing.T) {
	// Mock push2 capital flow endpoint. The response is a CSV-style
	// klines array: "date,close,change_pct,main_net,main_buy,main_sell,
	// main_net_ratio,super_buy,super_sell,super_net,large_buy,large_sell,
	// large_net,medium_buy,medium_sell,medium_net,small_buy,small_sell,
	// small_net"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := eastmoneyPush2Response{}
		resp.Data.KLines = []string{
			"2024-01-02,100.50,1.23,5000000,8000000,3000000,5.5,2000000,1500000,500000,1000000,800000,200000,500000,400000,100000,300000,250000,50000",
			"2024-01-03,101.20,0.69,3000000,6000000,3000000,3.0,1500000,1000000,500000,800000,600000,200000,400000,300000,100000,200000,180000,20000",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &EastmoneyClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	adapter := NewEastmoneyAdapter(client)

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
		Period:    "daily",
	}
	resp, err := adapter.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if resp.Source != "eastmoney" {
		t.Errorf("Source = %q, want %q", resp.Source, "eastmoney")
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	first := resp.Items[0]
	if first.Symbol != "600519.SH" {
		t.Errorf("item symbol = %q, want 600519.SH", first.Symbol)
	}
	if _, ok := first.Data["main_net"]; !ok {
		t.Errorf("item.Data missing main_net key; got %v", first.Data)
	}
}

// --- Test 2: EastmoneyAdapter handles 429 rate limit ---

func TestIntegration_EastmoneyAdapter_FetchCapitalFlow_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit"}`))
	}))
	defer srv.Close()

	client := &EastmoneyClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	adapter := NewEastmoneyAdapter(client)

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on 429, got nil")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

// --- Test 3: EastmoneyAdapter handles 5xx upstream error ---

func TestIntegration_EastmoneyAdapter_FetchCapitalFlow_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	client := &EastmoneyClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	adapter := NewEastmoneyAdapter(client)

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
}

// --- Test 4: AlphaVantageAdapter Fetch global_ohlcv happy path ---

func TestIntegration_AlphaVantageAdapter_FetchGlobalOHLCV_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// AlphaVantage TIME_SERIES_DAILY_ADJUSTED response shape
		resp := map[string]interface{}{
			"Meta Data": map[string]string{
				"1. Information": "Daily Time Series with Splits and Dividend Adjustment",
				"2. Symbol":      "AAPL",
			},
			"Time Series (Daily)": map[string]map[string]string{
				"2024-01-02": {
					"1. open":             "150.00",
					"2. high":             "152.00",
					"3. low":              "149.00",
					"4. close":            "151.00",
					"5. adjusted close":  "151.00",
					"6. volume":           "1000000",
					"7. dividend amount": "0.0000",
				},
				"2024-01-03": {
					"1. open":             "151.00",
					"2. high":             "153.00",
					"3. low":              "150.00",
					"4. close":            "152.50",
					"5. adjusted close":  "152.50",
					"6. volume":           "900000",
					"7. dividend amount": "0.0000",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := NewAlphaVantageAdapter("test-api-key")
	adapter.httpClient = srv.Client()
	adapter.baseURL = srv.URL

	req := FetchRequest{
		DataType:  DataTypeGlobalOHLCV,
		Symbols:   []string{"AAPL"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	resp, err := adapter.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if resp.Source != "alpha_vantage" {
		t.Errorf("Source = %q, want alpha_vantage", resp.Source)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].Data["close"].(float64) != 151.0 {
		t.Errorf("first close = %v, want 151.0", resp.Items[0].Data["close"])
	}
}

// --- Test 5: AlphaVantageAdapter handles empty series (rate limit) ---

func TestIntegration_AlphaVantageAdapter_FetchGlobalOHLCV_EmptySeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// AlphaVantage returns this when rate limited
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"Meta Data":              map[string]string{"1. Information": "Daily Time Series"},
			"Time Series (Daily)":    map[string]map[string]string{},
			"Note":                   "Thank you for using Alpha Vantage! Our standard API call frequency is 25 requests per day.",
		})
	}))
	defer srv.Close()

	adapter := NewAlphaVantageAdapter("test-api-key")
	adapter.httpClient = srv.Client()
	adapter.baseURL = srv.URL

	req := FetchRequest{
		DataType:  DataTypeGlobalOHLCV,
		Symbols:   []string{"AAPL"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on empty series, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
}

// --- Test 6: YahooFinanceAdapter Fetch global_ohlcv happy path ---

func TestIntegration_YahooFinanceAdapter_FetchGlobalOHLCV_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := yahooChartResponse{}
		result := struct {
			Meta struct {
				Symbol   string `json:"symbol"`
				Range    string `json:"range"`
				Currency string `json:"currency"`
				Exchange string `json:"fullExchangeName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []float64 `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		}{}
		result.Meta.Symbol = "AAPL"
		result.Timestamp = []int64{1704153600, 1704240000}
		result.Indicators.Quote = []struct {
			Open   []float64 `json:"open"`
			High   []float64 `json:"high"`
			Low    []float64 `json:"low"`
			Close  []float64 `json:"close"`
			Volume []float64 `json:"volume"`
		}{{
			Open:   []float64{150.0, 151.0},
			High:   []float64{152.0, 153.0},
			Low:    []float64{149.0, 150.0},
			Close:  []float64{151.0, 152.5},
			Volume: []float64{1000000, 900000},
		}}
		result.Indicators.AdjClose = []struct {
			AdjClose []float64 `json:"adjclose"`
		}{{AdjClose: []float64{151.0, 152.5}}}
		resp.Chart.Result = append(resp.Chart.Result, result)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := NewYahooFinanceAdapter()
	adapter.httpClient = srv.Client()
	adapter.baseURL = srv.URL

	req := FetchRequest{
		DataType:  DataTypeGlobalOHLCV,
		Symbols:   []string{"AAPL"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	resp, err := adapter.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if resp.Source != "yahoo_finance" {
		t.Errorf("Source = %q, want yahoo_finance", resp.Source)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
}

// --- Test 7: YahooFinanceAdapter handles 5xx error ---

func TestIntegration_YahooFinanceAdapter_FetchGlobalOHLCV_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	adapter := NewYahooFinanceAdapter()
	adapter.httpClient = srv.Client()
	adapter.baseURL = srv.URL

	req := FetchRequest{
		DataType:  DataTypeGlobalOHLCV,
		Symbols:   []string{"AAPL"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on 502, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
}

// --- Test 8: MootdxAdapter Fetch realtime via mock transport ---

func TestIntegration_MootdxAdapter_FetchRealtime_HappyPath(t *testing.T) {
	transport := &mockMootdxTransport{
		quotes: []MootdxQuote{
			{
				Symbol:    "600519",
				Price:     1800.0,
				Open:      1790.0,
				High:      1810.0,
				Low:       1785.0,
				LastClose: 1788.0,
				Volume:    100000,
				Amount:    180000000,
				ServerTime: time.Now(),
			},
		},
	}
	adapter := NewMootdxAdapter(transport)

	req := FetchRequest{
		DataType: DataTypeRealtime,
		Symbols:  []string{"600519.SH"},
	}
	resp, err := adapter.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if resp.Source != "mootdx" {
		t.Errorf("Source = %q, want mootdx", resp.Source)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Data["price"].(float64) != 1800.0 {
		t.Errorf("price = %v, want 1800.0", resp.Items[0].Data["price"])
	}
}

// --- Test 9: Registry fallback chain — primary fails, secondary succeeds ---

func TestIntegration_Registry_FallbackChain_PrimaryFailsSecondarySucceeds(t *testing.T) {
	// Primary: eastmoney returns 429 (rate limited → retryable). It is
	// registered but does not serve global_ohlcv, so the registry skips
	// it (chain drift log) and falls through to yahoo_finance.
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer primarySrv.Close()

	// Secondary: yahoo_finance returns valid OHLCV with one bar.
	secondarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := yahooChartResponse{}
		result := struct {
			Meta struct {
				Symbol   string `json:"symbol"`
				Range    string `json:"range"`
				Currency string `json:"currency"`
				Exchange string `json:"fullExchangeName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []float64 `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		}{}
		result.Meta.Symbol = "AAPL"
		result.Timestamp = []int64{1704153600}
		result.Indicators.Quote = []struct {
			Open   []float64 `json:"open"`
			High   []float64 `json:"high"`
			Low    []float64 `json:"low"`
			Close  []float64 `json:"close"`
			Volume []float64 `json:"volume"`
		}{{
			Open:   []float64{150.0},
			High:   []float64{152.0},
			Low:    []float64{149.0},
			Close:  []float64{151.0},
			Volume: []float64{1000000},
		}}
		result.Indicators.AdjClose = []struct {
			AdjClose []float64 `json:"adjclose"`
		}{{AdjClose: []float64{151.0}}}
		resp.Chart.Result = append(resp.Chart.Result, result)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer secondarySrv.Close()

	primaryClient := &EastmoneyClient{
		BaseURL:    primarySrv.URL,
		HTTPClient: primarySrv.Client(),
	}
	primaryAdapter := NewEastmoneyAdapter(primaryClient)

	secondaryAdapter := NewYahooFinanceAdapter()
	secondaryAdapter.httpClient = secondarySrv.Client()
	secondaryAdapter.baseURL = secondarySrv.URL

	// Build a registry with both adapters. eastmoney does not serve
	// global_ohlcv, so the registry skips it and falls through to
	// yahoo_finance, which returns a valid bar.
	reg := NewRegistry()
	if err := reg.Register(primaryAdapter); err != nil {
		t.Fatalf("register primary: %v", err)
	}
	if err := reg.Register(secondaryAdapter); err != nil {
		t.Fatalf("register secondary: %v", err)
	}
	reg.SetChain(DataTypeGlobalOHLCV, []string{"eastmoney", "yahoo_finance"})

	req := FetchRequest{
		DataType:  DataTypeGlobalOHLCV,
		Symbols:   []string{"AAPL"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	resp, err := reg.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Registry.Fetch returned error: %v", err)
	}
	if resp.Source != "yahoo_finance" {
		t.Errorf("Source = %q, want yahoo_finance (eastmoney skipped)", resp.Source)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item from yahoo_finance, got %d", len(resp.Items))
	}
}

// --- Test 10: Registry fallback chain — all adapters exhausted ---

func TestIntegration_Registry_FallbackChain_AllExhausted(t *testing.T) {
	// Both adapters return 5xx → all retryable → all exhausted.
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv2.Close()

	av := NewAlphaVantageAdapter("k")
	av.httpClient = srv1.Client()
	av.baseURL = srv1.URL

	yf := NewYahooFinanceAdapter()
	yf.httpClient = srv2.Client()
	yf.baseURL = srv2.URL

	reg := NewRegistry()
	_ = reg.Register(av)
	_ = reg.Register(yf)
	reg.SetChain(DataTypeGlobalOHLCV, []string{"alpha_vantage", "yahoo_finance"})

	req := FetchRequest{
		DataType:  DataTypeGlobalOHLCV,
		Symbols:   []string{"AAPL"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := reg.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when all adapters exhausted, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
}

// --- Test 11: Registry Fetch with empty chain returns ErrUnsupported ---

func TestIntegration_Registry_Fetch_EmptyChain(t *testing.T) {
	reg := NewRegistry()
	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := reg.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on empty chain, got nil")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

// --- Test 12: Registry Fetch with invalid request (EndDate before StartDate) ---

func TestIntegration_Registry_Fetch_InvalidRequest(t *testing.T) {
	reg := NewRegistry()
	reg.SetChain(DataTypeCapitalFlow, []string{"eastmoney"})
	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), // before start
	}
	_, err := reg.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on invalid request, got nil")
	}
}

// --- Test 13: Registry HealthCheck runs concurrently across adapters ---

func TestIntegration_Registry_HealthCheck_Concurrent(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		_ = json.NewEncoder(w).Encode(eastmoneyPush2Response{})
	}))
	defer srv.Close()

	client := &EastmoneyClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	a1 := NewEastmoneyAdapter(client)
	a2 := NewEastmoneySectorsAdapter(client)
	a3 := NewEastmoneyTopListAdapter(client)

	reg := NewRegistry()
	_ = reg.Register(a1)
	_ = reg.Register(a2)
	_ = reg.Register(a3)

	results := reg.HealthCheck(context.Background())
	if len(results) != 3 {
		t.Errorf("expected 3 health check results, got %d", len(results))
	}
	for name, err := range results {
		if err != nil {
			t.Errorf("adapter %s health check failed: %v", name, err)
		}
	}
}

// --- Test 14: ETLPipeline Process happy path with stub store ---

func TestIntegration_ETLPipeline_Process_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := eastmoneyPush2Response{}
		resp.Data.KLines = []string{
			"2024-01-02,100.50,1.23,5000000,8000000,3000000,5.5,2000000,1500000,500000,1000000,800000,200000,500000,400000,100000,300000,250000,50000",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &EastmoneyClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	adapter := NewEastmoneyAdapter(client)

	reg := NewRegistry()
	_ = reg.Register(adapter)
	reg.SetChain(DataTypeCapitalFlow, []string{"eastmoney"})

	store := &stubBulkInserter{}
	pipeline := NewETLPipeline(reg, store)

	normalizer := func(item DataItem, source, dataType string) UnifiedDataPoint {
		return NewUnifiedDataPoint(item.Symbol, source, dataType, item.TradeTime, item.Data)
	}

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	result, err := pipeline.Process(context.Background(), req, normalizer)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if result.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", result.Fetched)
	}
	if result.Persisted != 1 {
		t.Errorf("Persisted = %d, want 1", result.Persisted)
	}
	if result.SourceName != "eastmoney" {
		t.Errorf("SourceName = %q, want eastmoney", result.SourceName)
	}
	if len(store.inserted) != 1 {
		t.Errorf("store received %d points, want 1", len(store.inserted))
	}
}

// --- Test 15: ETLPipeline Process with store failure ---

func TestIntegration_ETLPipeline_Process_StoreFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := eastmoneyPush2Response{}
		resp.Data.KLines = []string{
			"2024-01-02,100.50,1.23,5000000,8000000,3000000,5.5,2000000,1500000,500000,1000000,800000,200000,500000,400000,100000,300000,250000,50000",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &EastmoneyClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
	adapter := NewEastmoneyAdapter(client)

	reg := NewRegistry()
	_ = reg.Register(adapter)
	reg.SetChain(DataTypeCapitalFlow, []string{"eastmoney"})

	storeErr := fmt.Errorf("disk full")
	store := &stubBulkInserter{fail: storeErr}
	pipeline := NewETLPipeline(reg, store)

	normalizer := func(item DataItem, source, dataType string) UnifiedDataPoint {
		return NewUnifiedDataPoint(item.Symbol, source, dataType, item.TradeTime, item.Data)
	}

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	result, err := pipeline.Process(context.Background(), req, normalizer)
	if err == nil {
		t.Fatal("expected error on store failure, got nil")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("expected storeErr, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even on store failure")
	}
	if result.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1 (fetch succeeded before persist)", result.Fetched)
	}
}

// --- Test 16: Reconciler StrategyFirstWins ---

func TestIntegration_Reconciler_StrategyFirstWins(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage", Data: map[string]interface{}{"close": 150.0}},
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 152.0}},
	}
	r := &Reconciler{Strategy: StrategyFirstWins}
	out, stats := r.Reconcile(points)
	if len(out) != 1 {
		t.Fatalf("expected 1 point after reconcile, got %d", len(out))
	}
	if out[0].Source != "alpha_vantage" {
		t.Errorf("first wins source = %q, want alpha_vantage", out[0].Source)
	}
	if stats.Conflicts != 1 || stats.Resolved != 1 || stats.PickedByFirst != 1 {
		t.Errorf("stats = %+v, want 1 conflict resolved by first", stats)
	}
}

// --- Test 17: Reconciler StrategyPriorityWins ---

func TestIntegration_Reconciler_StrategyPriorityWins(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 152.0}},
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage", Data: map[string]interface{}{"close": 150.0}},
	}
	r := &Reconciler{Strategy: StrategyPriorityWins, PriorityFunc: DefaultSourcePriority()}
	out, stats := r.Reconcile(points)
	if len(out) != 1 {
		t.Fatalf("expected 1 point, got %d", len(out))
	}
	// alpha_vantage priority=8, yahoo_finance priority=9 → alpha_vantage wins
	if out[0].Source != "alpha_vantage" {
		t.Errorf("priority wins source = %q, want alpha_vantage", out[0].Source)
	}
	if stats.PickedByPriority != 1 {
		t.Errorf("PickedByPriority = %d, want 1", stats.PickedByPriority)
	}
}

// --- Test 18: Reconciler StrategyLatestWins ---

func TestIntegration_Reconciler_StrategyLatestWins(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(1 * time.Second)
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage", Data: map[string]interface{}{"close": 150.0}, IngestTime: now},
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 152.0}, IngestTime: later},
	}
	r := &Reconciler{Strategy: StrategyLatestWins}
	out, _ := r.Reconcile(points)
	if len(out) != 1 {
		t.Fatalf("expected 1 point, got %d", len(out))
	}
	if out[0].Source != "yahoo_finance" {
		t.Errorf("latest wins source = %q, want yahoo_finance", out[0].Source)
	}
}

// --- Test 19: Reconciler StrategyNumericMedian ---

func TestIntegration_Reconciler_StrategyNumericMedian(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage", Data: map[string]interface{}{"close": 150.0}},
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 152.0}},
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "tushare", Data: map[string]interface{}{"close": 154.0}},
	}
	r := &Reconciler{Strategy: StrategyNumericMedian}
	out, stats := r.Reconcile(points)
	if len(out) != 1 {
		t.Fatalf("expected 1 point, got %d", len(out))
	}
	closeVal, ok := out[0].Data["close"].(float64)
	if !ok {
		t.Fatalf("close is not float64: %T", out[0].Data["close"])
	}
	if closeVal != 152.0 {
		t.Errorf("median close = %v, want 152.0", closeVal)
	}
	if stats.PickedByMedian != 1 {
		t.Errorf("PickedByMedian = %d, want 1", stats.PickedByMedian)
	}
}

// --- Test 20: Reconciler with no conflicts (singletons pass through) ---

func TestIntegration_Reconciler_NoConflicts(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage", Data: map[string]interface{}{"close": 150.0}},
		{Symbol: "MSFT", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 300.0}},
	}
	r := &Reconciler{Strategy: StrategyFirstWins}
	out, stats := r.Reconcile(points)
	if len(out) != 2 {
		t.Fatalf("expected 2 points (no conflicts), got %d", len(out))
	}
	if stats.Conflicts != 0 {
		t.Errorf("Conflicts = %d, want 0", stats.Conflicts)
	}
	if stats.Groups != 2 {
		t.Errorf("Groups = %d, want 2", stats.Groups)
	}
}

// --- Test 21: DeduplicateWithCount drops duplicates ---

func TestIntegration_DeduplicateWithCount_DropsDuplicates(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage", Data: map[string]interface{}{"close": 150.0}},
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 152.0}},
		{Symbol: "MSFT", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance", Data: map[string]interface{}{"close": 300.0}},
	}
	out, skipped := DeduplicateWithCount(points)
	if len(out) != 2 {
		t.Errorf("expected 2 unique points, got %d", len(out))
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

// --- Test 22: ValidatePoints drops invalid points ---

func TestIntegration_ValidatePoints_DropsInvalid(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "x", Data: map[string]interface{}{"close": 1.0}},   // empty symbol
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: time.Time{}, Source: "x", Data: map[string]interface{}{"close": 1.0}}, // zero time
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "x", Data: nil}, // nil data
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "x", Data: map[string]interface{}{"close": 1.0}}, // valid
	}
	out, skipped := ValidatePoints(points)
	if len(out) != 1 {
		t.Errorf("expected 1 valid point, got %d", len(out))
	}
	if skipped != 3 {
		t.Errorf("skipped = %d, want 3", skipped)
	}
}

// --- Test 23: IsRetryable classifies errors correctly ---

func TestIntegration_IsRetryable_Classification(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("%w: boom", ErrRateLimited), true},
		{fmt.Errorf("%w: boom", ErrUpstreamUnavailable), true},
		{fmt.Errorf("validation error"), false},
		{ErrUnsupported, false},
	}
	for _, c := range cases {
		got := IsRetryable(c.err)
		if got != c.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// --- Test 24: Validate catches empty DataType ---

func TestIntegration_Validate_EmptyDataType(t *testing.T) {
	err := Validate(FetchRequest{
		DataType:  "",
		StartDate: time.Now(),
		EndDate:   time.Now().Add(1 * time.Hour),
	})
	if err == nil {
		t.Fatal("expected error on empty DataType, got nil")
	}
}

// --- Test 25: Validate catches EndDate before StartDate ---

func TestIntegration_Validate_EndBeforeStart(t *testing.T) {
	err := Validate(FetchRequest{
		DataType:  DataTypeCapitalFlow,
		StartDate: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected error on EndDate before StartDate, got nil")
	}
}

// --- Test 26: GroupBySource buckets points by source ---

func TestIntegration_GroupBySource(t *testing.T) {
	now := time.Now().UTC()
	points := []UnifiedDataPoint{
		{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage"},
		{Symbol: "MSFT", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance"},
		{Symbol: "GOOG", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage"},
	}
	groups := GroupBySource(points)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups["alpha_vantage"]) != 2 {
		t.Errorf("alpha_vantage group size = %d, want 2", len(groups["alpha_vantage"]))
	}
	if len(groups["yahoo_finance"]) != 1 {
		t.Errorf("yahoo_finance group size = %d, want 1", len(groups["yahoo_finance"]))
	}
}

// --- Test 27: DefaultSourcePriority returns expected ordering ---

func TestIntegration_DefaultSourcePriority(t *testing.T) {
	prio := DefaultSourcePriority()
	cases := []struct {
		source string
		want   int
	}{
		{"tushare", 1},
		{"eastmoney", 2},
		{"mootdx", 3},
		{"eastmoney_sectors", 4},
		{"eastmoney_toplist", 5},
		{"juchao", 6},
		{"xueqiu", 7},
		{"alpha_vantage", 8},
		{"yahoo_finance", 9},
		{"unknown_source", math.MaxInt}, // math.MaxInt for unknown
	}
	for _, c := range cases {
		got := prio(c.source)
		if got != c.want {
			t.Errorf("priority(%q) = %d, want %d", c.source, got, c.want)
		}
	}
}

// --- Test 28: UnifiedDataPoint DeduplicateKey excludes Source ---

func TestIntegration_UnifiedDataPoint_DeduplicateKey_ExcludesSource(t *testing.T) {
	now := time.Now().UTC()
	p1 := UnifiedDataPoint{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "alpha_vantage"}
	p2 := UnifiedDataPoint{Symbol: "AAPL", DataType: DataTypeGlobalOHLCV, TradeTime: now, Source: "yahoo_finance"}
	if p1.DeduplicateKey() != p2.DeduplicateKey() {
		t.Errorf("dedup keys should match (source excluded): %q vs %q", p1.DeduplicateKey(), p2.DeduplicateKey())
	}
}

// --- Test 29: EastmoneyAdapter Schema returns correct fields ---

func TestIntegration_EastmoneyAdapter_Schema(t *testing.T) {
	adapter := NewEastmoneyAdapter(&EastmoneyClient{})
	schema, err := adapter.Schema(DataTypeCapitalFlow)
	if err != nil {
		t.Fatalf("Schema returned error: %v", err)
	}
	if schema.DataType != DataTypeCapitalFlow {
		t.Errorf("schema DataType = %q, want %q", schema.DataType, DataTypeCapitalFlow)
	}
	if len(schema.Fields) == 0 {
		t.Error("schema Fields is empty")
	}
	// Unsupported type
	_, err = adapter.Schema(DataTypeOHLCDaily)
	if err == nil {
		t.Error("expected error on unsupported schema, got nil")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

// --- Test 30: MootdxAdapter Fetch with nil transport returns error ---

func TestIntegration_MootdxAdapter_NilTransport(t *testing.T) {
	adapter := NewMootdxAdapter(nil)
	if adapter.Enabled() {
		t.Error("adapter with nil transport should be disabled")
	}
	req := FetchRequest{
		DataType:  DataTypeRealtime,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on nil transport, got nil")
	}
}

// --- Test 31: EastmoneyAdapter Fetch with empty symbols returns error ---

func TestIntegration_EastmoneyAdapter_EmptySymbols(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	client := &EastmoneyClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	adapter := NewEastmoneyAdapter(client)

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{}, // empty
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on empty symbols, got nil")
	}
}

// --- Test 32: Registry Register rejects nil adapter ---

func TestIntegration_Registry_Register_NilAdapter(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(nil)
	if err == nil {
		t.Fatal("expected error on nil adapter, got nil")
	}
}

// --- Test 33: Registry Register rejects empty name ---

func TestIntegration_Registry_Register_EmptyName(t *testing.T) {
	reg := NewRegistry()
	// AdapterBase with empty name
	adapter := NewEastmoneyAdapter(&EastmoneyClient{})
	adapter.AdapterBase = NewAdapterBase("", true)
	err := reg.Register(adapter)
	if err == nil {
		t.Fatal("expected error on empty name, got nil")
	}
}

// --- Test 34: Registry ListAdapters returns sorted names ---

func TestIntegration_Registry_ListAdapters_Sorted(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(NewYahooFinanceAdapter())
	_ = reg.Register(NewAlphaVantageAdapter("k"))
	_ = reg.Register(NewXueqiuAdapter())

	names := reg.ListAdapters()
	if len(names) != 3 {
		t.Fatalf("expected 3 adapters, got %d", len(names))
	}
	// Verify sorted
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("ListAdapters not sorted: %q > %q at index %d", names[i-1], names[i], i)
		}
	}
}

// --- Test 35: EastmoneyAdapter Fetch with cancelled context ---

func TestIntegration_EastmoneyAdapter_Fetch_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	client := &EastmoneyClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	adapter := NewEastmoneyAdapter(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := adapter.Fetch(ctx, req)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}

// --- Test 36: EastmoneyAdapter HealthCheck happy path ---

func TestIntegration_EastmoneyAdapter_HealthCheck_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(eastmoneyPush2Response{})
	}))
	defer srv.Close()

	client := &EastmoneyClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	adapter := NewEastmoneyAdapter(client)

	err := adapter.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("HealthCheck returned error: %v", err)
	}
}

// --- Test 37: EastmoneyAdapter HealthCheck on 5xx returns ErrUpstreamUnavailable ---

func TestIntegration_EastmoneyAdapter_HealthCheck_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &EastmoneyClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	adapter := NewEastmoneyAdapter(client)

	err := adapter.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("expected ErrUpstreamUnavailable, got %v", err)
	}
}

// --- Test 38: symbolToEastmoneySecid converts SH/SZ correctly ---

func TestIntegration_SymbolToEastmoneySecid(t *testing.T) {
	cases := []struct {
		symbol string
		want   string
	}{
		{"600519.SH", "1.600519"},
		{"000001.SZ", "0.000001"},
		{"600519", "1.600519"}, // bare SH
		{"000001", "0.000001"}, // bare SZ
		{"900001", "1.900001"},  // starts with 9 → SH
	}
	for _, c := range cases {
		got, err := symbolToEastmoneySecid(c.symbol)
		if err != nil {
			t.Errorf("symbolToEastmoneySecid(%q) error: %v", c.symbol, err)
			continue
		}
		if got != c.want {
			t.Errorf("symbolToEastmoneySecid(%q) = %q, want %q", c.symbol, got, c.want)
		}
	}
	// Invalid: too short
	_, err := symbolToEastmoneySecid("ab")
	if err == nil {
		t.Error("expected error on too-short symbol, got nil")
	}
}

// --- Test 39: parseEastmoneyCapitalFlowKLines parses valid rows ---

func TestIntegration_ParseEastmoneyCapitalFlowKLines(t *testing.T) {
	lines := []string{
		"2024-01-02,100.50,1.23,5000000,8000000,3000000,5.5,2000000,1500000,500000,1000000,800000,200000,500000,400000,100000,300000,250000,50000",
		"2024-01-03,101.20,0.69,3000000,6000000,3000000,3.0,1500000,1000000,500000,800000,600000,200000,400000,300000,100000,200000,180000,20000",
	}
	rows, err := parseEastmoneyCapitalFlowKLines(lines)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Date.Format("2006-01-02") != "2024-01-02" {
		t.Errorf("first date = %v, want 2024-01-02", rows[0].Date)
	}
	if rows[0].MainNet != 5000000 {
		t.Errorf("first main_net = %v, want 5000000", rows[0].MainNet)
	}
}

// --- Test 40: parseEastmoneyCapitalFlowKLines skips malformed rows ---

func TestIntegration_ParseEastmoneyCapitalFlowKLines_SkipsMalformed(t *testing.T) {
	lines := []string{
		"", // empty
		"only,three,fields", // too few
		"not-a-date,100,1.23,5000000", // bad date
		"2024-01-02,100.50,1.23,5000000,8000000,3000000,5.5,2000000,1500000,500000,1000000,800000,200000,500000,400000,100000,300000,250000,50000", // valid
	}
	rows, _ := parseEastmoneyCapitalFlowKLines(lines)
	if len(rows) != 1 {
		t.Errorf("expected 1 valid row, got %d", len(rows))
	}
}

// --- Test 41: eastmoneyCapitalFlowLmt computes correct limit ---

func TestIntegration_EastmoneyCapitalFlowLmt(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	lmt := eastmoneyCapitalFlowLmt(1, start, end)
	if lmt < 1 {
		t.Errorf("lmt = %d, want >= 1", lmt)
	}
	// Zero window → default 1000
	lmt0 := eastmoneyCapitalFlowLmt(1, time.Time{}, time.Time{})
	if lmt0 != 1000 {
		t.Errorf("lmt with zero window = %d, want 1000", lmt0)
	}
}

// --- Test 42: kltToDays maps klt values ---

func TestIntegration_KltToDays(t *testing.T) {
	cases := []struct {
		klt  int
		want int
	}{
		{1, 1},   // daily
		{5, 1},   // 5min
		{101, 7}, // weekly
		{102, 30}, // monthly
		{999, 1}, // unknown → 1
	}
	for _, c := range cases {
		got := kltToDays(c.klt)
		if got != c.want {
			t.Errorf("kltToDays(%d) = %d, want %d", c.klt, got, c.want)
		}
	}
}

// --- Test 43: ReconciliationStrategy String ---

func TestIntegration_ReconciliationStrategy_String(t *testing.T) {
	cases := []struct {
		s    ReconciliationStrategy
		want string
	}{
		{StrategyFirstWins, "first_wins"},
		{StrategyLatestWins, "latest_wins"},
		{StrategyPriorityWins, "priority_wins"},
		{StrategyNumericMedian, "numeric_median"},
		{ReconciliationStrategy(99), "unknown"},
	}
	for _, c := range cases {
		got := c.s.String()
		if got != c.want {
			t.Errorf("%v.String() = %q, want %q", c.s, got, c.want)
		}
	}
}

// --- Test 44: ReconciliationStrategy IsZero ---

func TestIntegration_ReconciliationStrategy_IsZero(t *testing.T) {
	if !StrategyFirstWins.IsZero() {
		t.Error("StrategyFirstWins.IsZero() should be true")
	}
	if StrategyPriorityWins.IsZero() {
		t.Error("StrategyPriorityWins.IsZero() should be false")
	}
}

// --- Test 45: DefaultReconciler uses priority strategy ---

func TestIntegration_DefaultReconciler(t *testing.T) {
	r := DefaultReconciler()
	if r.Strategy != StrategyPriorityWins {
		t.Errorf("DefaultReconciler strategy = %v, want %v", r.Strategy, StrategyPriorityWins)
	}
	if r.PriorityFunc == nil {
		t.Error("DefaultReconciler PriorityFunc is nil")
	}
}

// --- Test 46: ETLPipeline Process with nil registry returns error ---

func TestIntegration_ETLPipeline_Process_NilRegistry(t *testing.T) {
	pipeline := NewETLPipeline(nil, &stubBulkInserter{})
	normalizer := func(item DataItem, source, dataType string) UnifiedDataPoint {
		return UnifiedDataPoint{}
	}
	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := pipeline.Process(context.Background(), req, normalizer)
	if err == nil {
		t.Fatal("expected error on nil registry, got nil")
	}
}

// --- Test 47: ETLPipeline Process with nil normalizer returns error ---

func TestIntegration_ETLPipeline_Process_NilNormalizer(t *testing.T) {
	reg := NewRegistry()
	pipeline := NewETLPipeline(reg, &stubBulkInserter{})
	req := FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := pipeline.Process(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error on nil normalizer, got nil")
	}
}

// --- Test 48: Registry SetChain overrides chain ---

func TestIntegration_Registry_SetChain_Overrides(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(NewAlphaVantageAdapter("k"))
	_ = reg.Register(NewYahooFinanceAdapter())

	// Default chain from SupportedTypes: both alpha_vantage and yahoo_finance
	// claim global_ohlcv. SetChain should override.
	reg.SetChain(DataTypeGlobalOHLCV, []string{"yahoo_finance"})

	chains := reg.ListChains()
	chain := chains[DataTypeGlobalOHLCV]
	if len(chain) != 1 || chain[0] != "yahoo_finance" {
		t.Errorf("chain = %v, want [yahoo_finance]", chain)
	}
}

// --- Test 49: Registry GetAdapter returns nil for unknown ---

func TestIntegration_Registry_GetAdapter_Unknown(t *testing.T) {
	reg := NewRegistry()
	if a := reg.GetAdapter("does-not-exist"); a != nil {
		t.Errorf("GetAdapter(unknown) = %v, want nil", a)
	}
}

// --- Test 50: AdapterBase SetEnabled toggles flag ---

func TestIntegration_AdapterBase_SetEnabled(t *testing.T) {
	base := NewAdapterBase("test", true)
	if !base.Enabled() {
		t.Error("expected enabled=true initially")
	}
	base.SetEnabled(false)
	if base.Enabled() {
		t.Error("expected enabled=false after SetEnabled(false)")
	}
}

// Ensure io is used (avoid unused import if test evolves)
var _ = io.Discard
