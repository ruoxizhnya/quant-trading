package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// TestEastmoneyCapitalFlowLmt (CR-41 ODR-012) pins the lmt-computation
// table. The previous hard-coded 1000 silently truncated anything
// wider than ~4 years of daily bars; the new helper scales with the
// window while keeping an upper bound.
func TestEastmoneyCapitalFlowLmt(t *testing.T) {
	// 244 trading days/year is the A-share norm; calendar days in
	// 1 year ≈ 365. The helper uses calendar days / period_days with
	// 20% headroom, so a 1-year daily window should give roughly
	// 365 * 1.2 + 1 ≈ 439. We assert the *order of magnitude* — exact
	// numbers depend on leap years, holidays, etc.
	oneYear := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	twoYears := oneYear.AddDate(-2, 0, 0)
	fourYears := oneYear.AddDate(-4, 0, 0)
	tenYears := oneYear.AddDate(-10, 0, 0)
	fiftyYears := oneYear.AddDate(-50, 0, 0)

	t.Run("no window returns the upstream default", func(t *testing.T) {
		// Zero start or end → no time window; preserve the old behaviour.
		got := eastmoneyCapitalFlowLmt(1, time.Time{}, oneYear)
		assert.Equal(t, 1000, got)
		got = eastmoneyCapitalFlowLmt(1, oneYear, time.Time{})
		assert.Equal(t, 1000, got)
	})

	t.Run("invalid window returns the upstream default", func(t *testing.T) {
		// end <= start is nonsensical — don't compute a negative window.
		got := eastmoneyCapitalFlowLmt(1, oneYear, oneYear)
		assert.Equal(t, 1000, got, "end == start should fall back to default")
		got = eastmoneyCapitalFlowLmt(1, oneYear, twoYears)
		assert.Equal(t, 1000, got, "end < start should fall back to default")
	})

	t.Run("1-year daily window is around 440", func(t *testing.T) {
		got := eastmoneyCapitalFlowLmt(1, oneYear, oneYear.AddDate(1, 0, 0))
		// 366 calendar days * 1.2 + 1 ≈ 440
		assert.Greater(t, got, 400)
		assert.Less(t, got, 500)
	})

	t.Run("2-year daily window is around 880 (was truncated to 1000 before)", func(t *testing.T) {
		got := eastmoneyCapitalFlowLmt(1, twoYears, oneYear)
		// 731 days * 1.2 + 1 ≈ 878
		assert.Greater(t, got, 800)
		assert.Less(t, got, 950)
	})

	t.Run("4-year daily window exceeds 1000 (the old hard cap)", func(t *testing.T) {
		got := eastmoneyCapitalFlowLmt(1, fourYears, oneYear)
		// 1462 days * 1.2 + 1 ≈ 1755
		assert.Greater(t, got, 1000,
			"CR-41: 4-year daily window MUST return lmt > 1000, "+
				"otherwise the response is silently truncated to 4 years")
		assert.Less(t, got, 2000)
	})

	t.Run("10-year daily window stays under maxLmt", func(t *testing.T) {
		got := eastmoneyCapitalFlowLmt(1, tenYears, oneYear)
		assert.Greater(t, got, 4000)
		assert.Less(t, got, 8000, "CR-41: maxLmt caps runaway windows")
	})

	t.Run("50-year daily window is clamped to maxLmt", func(t *testing.T) {
		got := eastmoneyCapitalFlowLmt(1, fiftyYears, oneYear)
		assert.Equal(t, 8000, got,
			"CR-41: windows beyond maxLmt should be clamped, not silently truncated")
	})

	t.Run("weekly klt=101 needs fewer periods for the same window", func(t *testing.T) {
		weekly := eastmoneyCapitalFlowLmt(101, twoYears, oneYear)
		daily := eastmoneyCapitalFlowLmt(1, twoYears, oneYear)
		assert.Less(t, weekly, daily/3,
			"CR-41: weekly bars over 2 years should be ~7x fewer than daily")
		assert.Greater(t, weekly, 50)
	})

	t.Run("monthly klt=102 returns the smallest lmt", func(t *testing.T) {
		monthly := eastmoneyCapitalFlowLmt(102, tenYears, oneYear)
		daily := eastmoneyCapitalFlowLmt(1, tenYears, oneYear)
		assert.Less(t, monthly, daily/15,
			"CR-41: monthly bars over 10 years should be ~30x fewer than daily")
	})

	t.Run("unknown klt defaults to daily", func(t *testing.T) {
		got := eastmoneyCapitalFlowLmt(999, oneYear, oneYear.AddDate(1, 0, 0))
		// Should match the klt=1 case.
		assert.Greater(t, got, 400)
		assert.Less(t, got, 500)
	})
}

// TestEastmoneyAdapter_CapitalFlow_LmtScalesWithWindow (CR-41 HTTP
// integration): the lmt query parameter sent to Eastmoney must grow
// with the requested window, not stay pinned at 1000.
func TestEastmoneyAdapter_CapitalFlow_LmtScalesWithWindow(t *testing.T) {
	var capturedLmt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLmt = r.URL.Query().Get("lmt")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":   map[string]interface{}{"code": "1.600519", "name": "贵州茅台"},
			"rc":     0,
		})
	}))
	defer server.Close()

	client := NewEastmoneyClient()
	client.BaseURL = server.URL
	a := NewEastmoneyAdapter(client)
	// Request 5 years of daily data — would have been silently
	// truncated to ~1000 rows under the old hard-coded lmt=1000.
	start := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	_, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeCapitalFlow,
		Symbols:   []string{"600519.SH"},
		StartDate: start,
		EndDate:   end,
	})
	require.NoError(t, err)
	gotLmt, err := strconv.Atoi(capturedLmt)
	require.NoError(t, err, "lmt must be a valid integer")
	assert.Greater(t, gotLmt, 1000,
		"CR-41: lmt for a 5-year daily window must exceed the old hard cap of 1000")
	// 5 years ≈ 1827 calendar days * 1.2 + 1 ≈ 2193
	assert.Less(t, gotLmt, 3000)
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

// TestRegistry_ChainReferencesUnregisteredAdapter (CR-40 ODR-012):
// when SetChain references an adapter that was never registered, Fetch
// must surface that as ErrAdapterNotRegistered (not as a generic
// "all adapters exhausted" error). The distinction matters because
// a missing-registration error is a deploy/config bug, whereas an
// exhausted-chain error is a real upstream outage — they trigger
// different operator runbooks.
func TestRegistry_ChainReferencesUnregisteredAdapter(t *testing.T) {
	reg := NewRegistry()
	// Set a chain that references an adapter that was never Register'd.
	reg.SetChain(DataTypeRealtime, []string{"ghost", "phantom"})

	_, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now(),
		EndDate:   time.Now(),
	})
	require.Error(t, err)
	// CR-40: errors.Is must work through the fmt.Errorf("%w: ...") wrapper.
	assert.ErrorIs(t, err, ErrAdapterNotRegistered,
		"unregistered adapter should be distinguishable from upstream-out via ErrAdapterNotRegistered")
	// And the message should mention the bad name so log greppers can
	// pinpoint the misconfigured chain entry.
	assert.Contains(t, err.Error(), "ghost")
}

// TestRegistry_AllUpstreamsDown (CR-40 ODR-012 complement):
// when the chain references REAL but failing adapters, the error must
// NOT be classified as ErrAdapterNotRegistered. The two paths must
// stay disjoint so operators can tell deploy bugs from real outages.
func TestRegistry_AllUpstreamsDown(t *testing.T) {
	reg := NewRegistry()
	bad1 := &testAdapter{name: "bad1", supported: []string{DataTypeRealtime}, fetchErr: errRetryable}
	bad2 := &testAdapter{name: "bad2", supported: []string{DataTypeRealtime}, fetchErr: errRetryable}
	require.NoError(t, reg.Register(bad1))
	require.NoError(t, reg.Register(bad2))
	reg.SetChain(DataTypeRealtime, []string{"bad1", "bad2"})

	_, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now(),
		EndDate:   time.Now(),
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAdapterNotRegistered,
		"all-upstreams-down must NOT be classified as ErrAdapterNotRegistered")
	// And it should wrap the last retryable error so the caller can see
	// the actual upstream cause.
	assert.ErrorIs(t, err, ErrUpstreamUnavailable)
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

// ---------- EastmoneySectors stock_sector tests (CR-38) ----------

// TestBuildStockSectorItems covers the field→category mapping for
// CR-38 (ODR-012). Industry (f100/f102) and concept (f101/f103) must
// each produce their own DataItem tagged with the right category, and
// rows with both name+code empty must be dropped.
func TestBuildStockSectorItems(t *testing.T) {
	t.Run("both industry and concept populated", func(t *testing.T) {
		industry := map[string]interface{}{
			"f100": "银行",
			"f101": "区块链",
			"f102": "BK0475",
			"f103": "BK0800",
		}
		items := buildStockSectorItems("600519.SH", industry)
		require.Len(t, items, 2)

		// Industry row first.
		assert.Equal(t, "600519.SH", items[0].Symbol)
		assert.Equal(t, "BK0475", items[0].Data["sector_code"])
		assert.Equal(t, "银行", items[0].Data["sector_name"])
		assert.Equal(t, "industry", items[0].Data["category"])

		// Concept row second.
		assert.Equal(t, "BK0800", items[1].Data["sector_code"])
		assert.Equal(t, "区块链", items[1].Data["sector_name"])
		assert.Equal(t, "concept", items[1].Data["category"])
	})

	t.Run("only industry populated", func(t *testing.T) {
		industry := map[string]interface{}{
			"f100": "银行",
			"f102": "BK0475",
		}
		items := buildStockSectorItems("000001.SZ", industry)
		require.Len(t, items, 1)
		assert.Equal(t, "BK0475", items[0].Data["sector_code"])
		assert.Equal(t, "industry", items[0].Data["category"])
	})

	t.Run("only concept populated", func(t *testing.T) {
		industry := map[string]interface{}{
			"f101": "区块链",
			"f103": "BK0800",
		}
		items := buildStockSectorItems("300750.SZ", industry)
		require.Len(t, items, 1)
		assert.Equal(t, "BK0800", items[0].Data["sector_code"])
		assert.Equal(t, "区块链", items[0].Data["sector_name"])
		assert.Equal(t, "concept", items[0].Data["category"])
	})

	t.Run("name without code falls back to name as code", func(t *testing.T) {
		// Eastmoney occasionally returns just a name for hot/new
		// concepts before assigning a BK code. Make sure those rows
		// are still ingestable.
		industry := map[string]interface{}{
			"f101": "新概念",
		}
		items := buildStockSectorItems("688981.SH", industry)
		require.Len(t, items, 1)
		assert.Equal(t, "新概念", items[0].Data["sector_code"])
		assert.Equal(t, "新概念", items[0].Data["sector_name"])
		assert.Equal(t, "concept", items[0].Data["category"])
	})

	t.Run("all empty drops everything", func(t *testing.T) {
		items := buildStockSectorItems("600519.SH", map[string]interface{}{})
		assert.Empty(t, items)
	})

	t.Run("nil industry map returns empty slice (not nil panic)", func(t *testing.T) {
		items := buildStockSectorItems("600519.SH", nil)
		assert.Empty(t, items)
	})

	t.Run("non-string f-code falls back to name as code", func(t *testing.T) {
		// Defensive: if Eastmoney ever returns a number for f102 but
		// the name is still a valid string, we keep the row using
		// the name as code (matches the name-only case above).
		industry := map[string]interface{}{
			"f100": "银行",
			"f102": 12345, // wrong type — stringField returns ""
		}
		items := buildStockSectorItems("600519.SH", industry)
		require.Len(t, items, 1)
		assert.Equal(t, "银行", items[0].Data["sector_code"])
		assert.Equal(t, "银行", items[0].Data["sector_name"])
		assert.Equal(t, "industry", items[0].Data["category"])
	})

	t.Run("non-string in both name and code drops the row", func(t *testing.T) {
		industry := map[string]interface{}{
			"f100": 999, // wrong type — no usable name
			"f102": 12345, // wrong type — no usable code
		}
		items := buildStockSectorItems("600519.SH", industry)
		assert.Empty(t, items, "rows with neither name nor code must be dropped")
	})
}

// TestEastmoneySectors_FetchStockSectors_HTTP (CR-38) verifies that
// fetchStockSectors actually requests the f100,f101,f102,f103 field
// list and emits two items per symbol (one per category) when the
// upstream returns a real-looking response.
func TestEastmoneySectors_FetchStockSectors_HTTP(t *testing.T) {
	var capturedFields string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedFields = r.URL.Query().Get("fields")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"code": "1.600519",
				"name": "贵州茅台",
				"industry": map[string]interface{}{
					"f100": "白酒",
					"f101": "国企改革",
					"f102": "BK0001",
					"f103": "BK0002",
				},
			},
			"rc": 0,
		})
	}))
	defer server.Close()

	client := NewEastmoneyClient()
	client.BaseURL = server.URL
	a := NewEastmoneySectorsAdapter(client)
	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType: DataTypeStockSector,
		Symbols:  []string{"600519.SH"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// CR-38: all four fields must be requested.
	assert.Equal(t, "f100,f101,f102,f103", capturedFields,
		"CR-38: must request all four f-codes to capture concept sectors")
	// One industry + one concept = 2 items per symbol.
	require.Len(t, resp.Items, 2)
	assert.Equal(t, "industry", resp.Items[0].Data["category"])
	assert.Equal(t, "concept", resp.Items[1].Data["category"])
	assert.Equal(t, "白酒", resp.Items[0].Data["sector_name"])
	assert.Equal(t, "国企改革", resp.Items[1].Data["sector_name"])
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
