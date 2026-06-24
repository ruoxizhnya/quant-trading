package hkex

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

// ============================================================================
// Parser tests (no HTTP)
// ============================================================================

func TestParseKamtRow_Basic(t *testing.T) {
	// date, sh_net, sz_net, sh_buy, sh_sell, sz_buy, sz_sell, total_net, total_buy, total_sell
	line := "2024-01-15,12.34,5.67,100,80,50,40,18.01,150,120"
	flow := parseKamtRow(line)
	require.NotNil(t, flow)
	assert.Equal(t, 12.34, flow.SHConnectNetBuy)
	assert.Equal(t, 5.67, flow.SZConnectNetBuy)
	assert.Equal(t, 18.01, flow.TotalNetBuy)
	assert.Equal(t, 150.0, flow.TotalBuy)
	assert.Equal(t, 120.0, flow.TotalSell)
}

func TestParseKamtRow_PartialColumns_FallbackTotal(t *testing.T) {
	// Only 3 columns: date, sh_net, sz_net — total should fall back to sh+sz.
	line := "2024-01-15,10.0,5.0"
	flow := parseKamtRow(line)
	require.NotNil(t, flow)
	assert.Equal(t, 10.0, flow.SHConnectNetBuy)
	assert.Equal(t, 5.0, flow.SZConnectNetBuy)
	assert.Equal(t, 15.0, flow.TotalNetBuy, "total should fall back to sh+sz")
	assert.Equal(t, 0.0, flow.TotalBuy)
}

func TestParseKamtRow_EmptyLine(t *testing.T) {
	flow := parseKamtRow("")
	require.NotNil(t, flow)
	assert.Equal(t, 0.0, flow.TotalNetBuy)
}

func TestParseStockFlowKLines_Basic(t *testing.T) {
	klines := []string{
		"2024-01-15,1800,1.5,5000,8000,3000,3.42",
		"2024-01-16,1820,1.1,6000,9000,3000,3.55",
	}
	rows := parseStockFlowKLines(klines, "600519.SH", "贵州茅台")
	require.Len(t, rows, 2)
	assert.Equal(t, "600519.SH", rows[0].Symbol)
	assert.Equal(t, "贵州茅台", rows[0].Name)
	assert.Equal(t, 5000.0, rows[0].NetBuy)
	assert.Equal(t, 8000.0, rows[0].BuyAmount)
	assert.Equal(t, 3000.0, rows[0].SellAmount)
	assert.Equal(t, 3.42, rows[0].HoldingRatio)
	assert.Equal(t, "2024-01-15", rows[0].Date.Format("2006-01-02"))
}

func TestParseStockFlowKLines_SkipsInvalidDate(t *testing.T) {
	klines := []string{
		"not-a-date,1800,1.5,5000,8000,3000,3.42",
		"2024-01-16,1820,1.1,6000,9000,3000,3.55",
	}
	rows := parseStockFlowKLines(klines, "600519.SH", "贵州茅台")
	require.Len(t, rows, 1, "invalid date row should be skipped")
	assert.Equal(t, "2024-01-16", rows[0].Date.Format("2006-01-02"))
}

func TestParseStockFlowKLines_SkipsShortRow(t *testing.T) {
	klines := []string{
		"2024-01-15,1800", // only 2 fields, need >= 4
		"2024-01-16,1820,1.1,6000",
	}
	rows := parseStockFlowKLines(klines, "600519.SH", "贵州茅台")
	require.Len(t, rows, 1)
}

func TestParseRankDiff_Basic(t *testing.T) {
	diff := []map[string]interface{}{
		{"f12": "600519", "f14": "贵州茅台", "f62": 5.5e8, "f55": 8.0e8, "f56": 2.5e8, "f57": 3.42},
		{"f12": "000001", "f14": "平安银行", "f62": 3.2e8, "f55": 5.0e8, "f56": 1.8e8, "f57": 2.11},
	}
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := parseRankDiff(diff, date)
	require.Len(t, rows, 2)
	assert.Equal(t, "600519.SH", rows[0].Symbol)
	assert.Equal(t, "贵州茅台", rows[0].Name)
	assert.Equal(t, 5.5e8, rows[0].NetBuy)
	assert.Equal(t, 3.42, rows[0].HoldingRatio)
	assert.Equal(t, "000001.SZ", rows[1].Symbol)
}

func TestParseRankDiff_SkipsEmptySymbol(t *testing.T) {
	diff := []map[string]interface{}{
		{"f14": "no symbol", "f62": 1.0},
		{"f12": "600519", "f14": "贵州茅台", "f62": 5.5e8},
	}
	rows := parseRankDiff(diff, time.Now())
	require.Len(t, rows, 1, "row with empty f12 should be skipped")
	assert.Equal(t, "600519.SH", rows[0].Symbol)
}

func TestParseRankDiff_HandlesNumericStrings(t *testing.T) {
	diff := []map[string]interface{}{
		{"f12": "600519", "f14": "贵州茅台", "f62": "550000000", "f57": "3.42"},
	}
	rows := parseRankDiff(diff, time.Now())
	require.Len(t, rows, 1)
	assert.Equal(t, 550000000.0, rows[0].NetBuy)
	assert.Equal(t, 3.42, rows[0].HoldingRatio)
}

// ============================================================================
// Helper tests
// ============================================================================

func TestSymbolToEastmoneySecid(t *testing.T) {
	cases := []struct {
		symbol string
		want   string
	}{
		{"600519.SH", "1.600519"},
		{"000001.SZ", "0.000001"},
		{"300750.SZ", "0.300750"},
		{"688981.SH", "1.688981"},
	}
	for _, c := range cases {
		got, err := symbolToEastmoneySecid(c.symbol)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, "symbol %s", c.symbol)
	}
}

func TestSymbolToEastmoneySecid_Invalid(t *testing.T) {
	_, err := symbolToEastmoneySecid("AB")
	assert.Error(t, err)
}

func TestSymbolToEastmoneySecid_BareCode(t *testing.T) {
	// Bare 6-digit codes without a suffix: 6xx → SH, else SZ.
	got, err := symbolToEastmoneySecid("600519")
	require.NoError(t, err)
	assert.Equal(t, "1.600519", got)
	got, err = symbolToEastmoneySecid("000001")
	require.NoError(t, err)
	assert.Equal(t, "0.000001", got)
}

func TestNormalizeSymbol(t *testing.T) {
	assert.Equal(t, "600519.SH", normalizeSymbol("600519"))
	assert.Equal(t, "000001.SZ", normalizeSymbol("000001"))
	assert.Equal(t, "600519.SH", normalizeSymbol("600519.SH"), "already-suffixed should pass through")
}

func TestNormalizeDate(t *testing.T) {
	in := time.Date(2024, 1, 15, 14, 30, 45, 123, time.UTC)
	out := normalizeDate(in)
	assert.Equal(t, "2024-01-15", out.Format("2006-01-02"))
	assert.Equal(t, 0, out.Hour())
}

func TestEastmoneyStockFlowLmt(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	lmt := eastmoneyStockFlowLmt(start, end)
	assert.Greater(t, lmt, 31, "31-day window should need > 31 rows after headroom")

	// Zero window → fallback.
	assert.Equal(t, 1000, eastmoneyStockFlowLmt(time.Time{}, time.Time{}))
}

func TestFlowSignal_String(t *testing.T) {
	assert.Equal(t, "strong_inflow", FlowSignalStrongInflow.String())
	assert.Equal(t, "strong_outflow", FlowSignalStrongOutflow.String())
	assert.Equal(t, "neutral", FlowSignalNeutral.String())
}

// ============================================================================
// Fetcher HTTP tests (httptest)
// ============================================================================

// newTestFetcher wires an EastmoneyNorthboundFetcher against a test
// server. RequestInterval is zeroed so tests don't pay the polite delay.
func newTestFetcher(t *testing.T, handler http.HandlerFunc) (*EastmoneyNorthboundFetcher, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	f := NewEastmoneyNorthboundFetcher(srv.URL)
	f.RequestInterval = 0
	return f, srv
}

func TestFetchDaily_Success(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "kamt.kline")
		resp := eastmoneyKamtResponse{}
		resp.Data.KLines = []string{
			"2024-01-12,10.0,5.0,100,80,50,40,15.0,150,120",
			"2024-01-15,12.34,5.67,100,80,50,40,18.01,150,120",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	flow, err := f.FetchDaily(context.Background(), date)
	require.NoError(t, err)
	require.NotNil(t, flow)
	assert.Equal(t, 12.34, flow.SHConnectNetBuy)
	assert.Equal(t, 5.67, flow.SZConnectNetBuy)
	assert.Equal(t, 18.01, flow.TotalNetBuy)
	assert.Equal(t, "2024-01-15", flow.Date.Format("2006-01-02"))
}

func TestFetchDaily_Holiday_ReturnsNil(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		resp := eastmoneyKamtResponse{}
		resp.Data.KLines = []string{
			"2024-01-12,10.0,5.0,100,80,50,40,15.0,150,120",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	// 2024-01-15 not in the klines → holiday → (nil, nil).
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	flow, err := f.FetchDaily(context.Background(), date)
	require.NoError(t, err)
	assert.Nil(t, flow, "missing date should return nil flow, not error")
}

func TestFetchDaily_HTTPError(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	_, err := f.FetchDaily(context.Background(), time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchDaily_RateLimited(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer srv.Close()

	_, err := f.FetchDaily(context.Background(), time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestFetchDaily_JSONParseError(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	})
	defer srv.Close()

	_, err := f.FetchDaily(context.Background(), time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestFetchDaily_EmptyKLines(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(eastmoneyKamtResponse{})
	})
	defer srv.Close()

	flow, err := f.FetchDaily(context.Background(), time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Nil(t, flow, "empty klines → no matching date → nil flow")
}

func TestFetchStockFlow_Success(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "fflow/daykline")
		// Verify secid was built from the symbol.
		assert.Equal(t, "1.600519", r.URL.Query().Get("secid"))
		resp := eastmoneyStockFlowResponse{}
		resp.Data.Code = "600519"
		resp.Data.Name = "贵州茅台"
		resp.Data.KLines = []string{
			"2024-01-15,1800,1.5,5000,8000,3000,3.42",
			"2024-01-16,1820,1.1,6000,9000,3000,3.55",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	rows, err := f.FetchStockFlow(context.Background(), "600519.SH", start, end)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "600519.SH", rows[0].Symbol)
	assert.Equal(t, "贵州茅台", rows[0].Name)
	assert.Equal(t, 5000.0, rows[0].NetBuy)
}

func TestFetchStockFlow_InvalidSymbol(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make an HTTP call for an invalid symbol")
	})
	defer srv.Close()

	_, err := f.FetchStockFlow(context.Background(), "AB", time.Time{}, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid symbol")
}

func TestFetchStockFlow_EmptyResponse(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(eastmoneyStockFlowResponse{})
	})
	defer srv.Close()

	rows, err := f.FetchStockFlow(context.Background(), "600519.SH", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestFetchTopHoldings_Success(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "fflow/rank")
		assert.Equal(t, "5", r.URL.Query().Get("pz"), "limit should map to pz")
		resp := eastmoneyRankResponse{}
		resp.Data.Total = 2
		resp.Data.Diff = []map[string]interface{}{
			{"f12": "600519", "f14": "贵州茅台", "f62": 5.5e8, "f55": 8.0e8, "f56": 2.5e8, "f57": 3.42},
			{"f12": "000001", "f14": "平安银行", "f62": 3.2e8, "f55": 5.0e8, "f56": 1.8e8, "f57": 2.11},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows, err := f.FetchTopHoldings(context.Background(), date, 5)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "600519.SH", rows[0].Symbol)
	assert.Equal(t, 5.5e8, rows[0].NetBuy)
}

func TestFetchTopHoldings_DefaultLimit(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "10", r.URL.Query().Get("pz"), "limit<=0 should default to 10")
		_ = json.NewEncoder(w).Encode(eastmoneyRankResponse{})
	})
	defer srv.Close()

	rows, err := f.FetchTopHoldings(context.Background(), time.Now(), 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestFetchTopHoldings_HTTPError(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	defer srv.Close()

	_, err := f.FetchTopHoldings(context.Background(), time.Now(), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestFetchDaily_CancelledContext(t *testing.T) {
	f, srv := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := f.FetchDaily(ctx, time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
}

func TestNewEastmoneyNorthboundFetcher_Defaults(t *testing.T) {
	f := NewEastmoneyNorthboundFetcher("")
	assert.Equal(t, defaultEastmoneyBaseURL, f.BaseURL)
	assert.NotNil(t, f.HTTPClient)
	assert.Equal(t, defaultEastmoneyUserAgent, f.UserAgent)
	assert.Equal(t, defaultRequestInterval, f.RequestInterval)
}

func TestNewEastmoneyNorthboundFetcher_CustomBaseURL(t *testing.T) {
	f := NewEastmoneyNorthboundFetcher("http://example.test")
	assert.Equal(t, "http://example.test", f.BaseURL)
}

// ============================================================================
// ExchangeRateConverter tests
// ============================================================================

func TestExchangeRateConverter_Default(t *testing.T) {
	c := NewExchangeRateConverter()
	assert.Equal(t, defaultHKDCNYRate, c.HKDCNY)
}

func TestExchangeRateConverter_SetRate(t *testing.T) {
	c := NewExchangeRateConverter()
	require.NoError(t, c.SetRate(0.88))
	assert.Equal(t, 0.88, c.HKDCNY)
}

func TestExchangeRateConverter_SetRate_Invalid(t *testing.T) {
	c := NewExchangeRateConverter()
	err := c.SetRate(0)
	assert.Error(t, err)
	err = c.SetRate(-0.5)
	assert.Error(t, err)
}

func TestExchangeRateConverter_HKDToCNY(t *testing.T) {
	c := NewExchangeRateConverter()
	// 100 HKD * 0.91 = 91 CNY
	assert.Equal(t, 91.0, c.HKDToCNY(100))
	assert.Equal(t, 0.0, c.HKDToCNY(0))
}

func TestExchangeRateConverter_CNYToHKD(t *testing.T) {
	c := NewExchangeRateConverter()
	// 91 CNY / 0.91 = 100 HKD
	assert.InDelta(t, 100.0, c.CNYToHKD(91), 1e-9)
}

func TestExchangeRateConverter_CNYToHKD_ZeroRate(t *testing.T) {
	c := NewExchangeRateConverter()
	c.HKDCNY = 0
	// Zero rate must NOT produce +Inf — return 0 to protect factor math.
	assert.Equal(t, 0.0, c.CNYToHKD(100), "zero rate should return 0, not +Inf")
}

func TestExchangeRateConverter_FetchRate(t *testing.T) {
	c := NewExchangeRateConverter()
	require.NoError(t, c.SetRate(0.92))
	rate, err := c.FetchRate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0.92, rate)
}

func TestExchangeRateConverter_FetchRate_CancelledContext(t *testing.T) {
	c := NewExchangeRateConverter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.FetchRate(ctx)
	assert.Error(t, err, "cancelled ctx should return ctx.Err()")
}

func TestExchangeRateConverter_RoundTrip(t *testing.T) {
	c := NewExchangeRateConverter()
	require.NoError(t, c.SetRate(0.91))
	original := 1234.56
	cny := c.HKDToCNY(original)
	back := c.CNYToHKD(cny)
	assert.InDelta(t, original, back, 1e-6, "HKD→CNY→HKD should round-trip")
}

// ============================================================================
// NorthboundFetcher interface conformance
// ============================================================================

// Compile-time check that EastmoneyNorthboundFetcher satisfies the
// NorthboundFetcher interface. If the interface drifts, this fails to
// compile — catching signature mismatches at build time rather than at
// the first call site.
var _ NorthboundFetcher = (*EastmoneyNorthboundFetcher)(nil)

// stubFetcher is a minimal NorthboundFetcher used by factor_test.go.
// It uses function fields so each test can customize behavior per-call
// (e.g. return different data for different dates). It is declared here
// (next to the interface conformance check) so the two stay in sync.
type stubFetcher struct {
	dailyFn     func(ctx context.Context, date time.Time) (*NorthboundFlow, error)
	stockFlowFn func(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error)
	topFn       func(ctx context.Context, date time.Time, limit int) ([]StockFlow, error)
}

func (s *stubFetcher) FetchDaily(ctx context.Context, date time.Time) (*NorthboundFlow, error) {
	if s.dailyFn == nil {
		return nil, nil
	}
	return s.dailyFn(ctx, date)
}

func (s *stubFetcher) FetchStockFlow(ctx context.Context, symbol string, start, end time.Time) ([]StockFlow, error) {
	if s.stockFlowFn == nil {
		return nil, nil
	}
	return s.stockFlowFn(ctx, symbol, start, end)
}

func (s *stubFetcher) FetchTopHoldings(ctx context.Context, date time.Time, limit int) ([]StockFlow, error) {
	if s.topFn == nil {
		return nil, nil
	}
	return s.topFn(ctx, date, limit)
}

var _ NorthboundFetcher = (*stubFetcher)(nil)

// silence unused-import warnings for strings when the test build
// trims dead code paths.
var _ = strings.Contains
