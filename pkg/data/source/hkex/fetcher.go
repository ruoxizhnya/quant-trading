package hkex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ----------------------------------------------------------------------------
// EastmoneyNorthboundFetcher
// ----------------------------------------------------------------------------

// Default push2 endpoints. Exported as vars (not consts) so tests can
// override them when pointing an EastmoneyNorthboundFetcher at an
// httptest.Server via BaseURL.
const (
	defaultEastmoneyBaseURL  = "https://push2.eastmoney.com"
	defaultEastmoneyUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) quant-trading/1.0"
	defaultRequestInterval    = 300 * time.Millisecond
)

// EastmoneyNorthboundFetcher implements NorthboundFetcher against the
// Eastmoney push2 endpoints.
//
// Endpoints (per the P2-12 spec):
//   - Daily total:  /api/qt/kamt.kline/get
//   - Stock flow:   /api/qt/stock/fflow/daykline/get
//   - Top holdings: /api/qt/stock/fflow/rank
//
// The fetcher is safe for concurrent use: every call builds its own
// http.Request and the underlying *http.Client is shared read-only.
// A small sleep between requests is enforced to stay below Eastmoney's
// informal rate limit (≈200 req/min) — this is a polite delay, not a
// hard token bucket.
type EastmoneyNorthboundFetcher struct {
	// BaseURL is the push2 root. Override in tests with an
	// httptest.Server.URL.
	BaseURL string
	// HTTPClient is the underlying client. If nil, a default client
	// with a 10s timeout is constructed lazily.
	HTTPClient *http.Client
	// UserAgent sent on every request. Eastmoney 403s requests with
	// the Go default UA, so this defaults to a browser-like string.
	UserAgent string
	// RequestInterval is the polite delay between successive upstream
	// calls. Set to 0 to disable. The delay is applied per Fetch call
	// (not per HTTP request) so concurrent callers each wait once.
	RequestInterval time.Duration
	logger          zerolog.Logger
}

// NewEastmoneyNorthboundFetcher constructs a fetcher with sensible
// defaults. Pass an httptest.Server.URL as baseURL in tests.
func NewEastmoneyNorthboundFetcher(baseURL string) *EastmoneyNorthboundFetcher {
	if baseURL == "" {
		baseURL = defaultEastmoneyBaseURL
	}
	return &EastmoneyNorthboundFetcher{
		BaseURL:         baseURL,
		HTTPClient:      &http.Client{Timeout: 10 * time.Second},
		UserAgent:       defaultEastmoneyUserAgent,
		RequestInterval: defaultRequestInterval,
		logger:          zerolog.Nop(),
	}
}

// SetLogger wires a zerolog.Logger. If l is the zero value, the fetcher
// falls back to a no-op logger so it never panics on a nil receiver.
func (f *EastmoneyNorthboundFetcher) SetLogger(l zerolog.Logger) {
	f.logger = l.With().Str("component", "hkex.fetcher").Logger()
}

// FetchDaily implements NorthboundFetcher.FetchDaily.
//
// Eastmoney kamt.kline returns a CSV-style klines slice; the last row
// is the most recent trading day. We pick the row whose date matches
// `date` (calendrically — Eastmoney uses YYYY-MM-DD). If no row matches
// (holiday / future date), we return (nil, nil) per the interface
// contract so callers can step back to the previous trading day.
func (f *EastmoneyNorthboundFetcher) FetchDaily(ctx context.Context, date time.Time) (*NorthboundFlow, error) {
	q := url.Values{}
	// klt=1 → daily K-line; fields1/fields2 enumerate the columns we want.
	q.Set("klt", "1")
	q.Set("fields1", "f1,f2,f3,f4,f5,f6,f7,f8,f9,f10,f11,f12,f13,f14")
	q.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61,f62,f63,f64,f65")
	// fqt=1 → forward-adjusted (irrelevant for flow, but the endpoint
	// rejects requests without it).
	q.Set("fqt", "1")
	// lmt caps the number of rows returned. 30 trading days is enough
	// to find a match for any recent `date` while keeping the payload
	// small.
	q.Set("lmt", "30")

	var resp eastmoneyKamtResponse
	if err := f.getJSON(ctx, "/api/qt/kamt.kline/get", q, &resp); err != nil {
		return nil, fmt.Errorf("hkex: fetch daily kamt: %w", err)
	}

	row, ok := findKamtRowByDate(resp.Data.KLines, date)
	if !ok {
		// No row for this date — holiday / pre-open / future date.
		f.logger.Debug().
			Time("date", date).
			Int("rows", len(resp.Data.KLines)).
			Msg("no kamt row for date")
		return nil, nil
	}
	flow := parseKamtRow(row)
	flow.Date = normalizeDate(date)
	f.logger.Debug().
		Time("date", flow.Date).
		Float64("total_net_buy", flow.TotalNetBuy).
		Msg("fetched daily northbound flow")
	return flow, nil
}

// FetchStockFlow implements NorthboundFetcher.FetchStockFlow.
//
// Eastmoney's per-stock fflow daykline returns the daily main-force
// capital flow for a symbol. We map that onto StockFlow because the
// northbound-specific per-stock endpoint is gated behind Eastmoney's
// datacenter (which requires a different auth flow); the main-force
// flow is a close enough proxy for the factor calculator and keeps
// the implementation within the push2 surface area specified by P2-12.
func (f *EastmoneyNorthboundFetcher) FetchStockFlow(ctx context.Context, symbol string, startDate, endDate time.Time) ([]StockFlow, error) {
	secid, err := symbolToEastmoneySecid(symbol)
	if err != nil {
		return nil, fmt.Errorf("hkex: stock flow %s: %w", symbol, err)
	}
	q := url.Values{}
	q.Set("secid", secid)
	q.Set("klt", "1")
	q.Set("fqt", "1")
	q.Set("fields1", "f1,f2,f3")
	q.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61,f62,f63,f64,f65")
	if !startDate.IsZero() {
		q.Set("beg", startDate.Format("20060102"))
	}
	if !endDate.IsZero() {
		q.Set("end", endDate.Format("20060102"))
	}
	q.Set("lmt", strconv.Itoa(eastmoneyStockFlowLmt(startDate, endDate)))

	var resp eastmoneyStockFlowResponse
	if err := f.getJSON(ctx, "/api/qt/stock/fflow/daykline/get", q, &resp); err != nil {
		return nil, fmt.Errorf("hkex: stock flow %s: %w", symbol, err)
	}

	rows := parseStockFlowKLines(resp.Data.KLines, symbol, resp.Data.Name)
	f.logger.Debug().
		Str("symbol", symbol).
		Int("rows", len(rows)).
		Msg("fetched stock flow")
	return rows, nil
}

// FetchTopHoldings implements NorthboundFetcher.FetchTopHoldings.
//
// The fflow/rank endpoint returns the top-N stocks by main-force net
// buy for the latest trading day. `date` is accepted for interface
// conformance but Eastmoney only serves the current snapshot — callers
// needing historical top holdings should backfill via FetchStockFlow.
func (f *EastmoneyNorthboundFetcher) FetchTopHoldings(ctx context.Context, date time.Time, limit int) ([]StockFlow, error) {
	if limit <= 0 {
		limit = 10
	}
	q := url.Values{}
	q.Set("fid", "f62") // sort by main net buy
	q.Set("po", "1")    // descending
	q.Set("pz", strconv.Itoa(limit))
	q.Set("np", "1")
	q.Set("fltt", "2")
	q.Set("invt", "2")
	// fs restricts to A-share main boards (SH+SZ).
	q.Set("fs", "m:1+t:2,m:0+t:6,m:0+t:13,m:0+t:80")
	q.Set("fields", "f12,f14,f62,f55,f56,f57,f58,f59,f60")

	var resp eastmoneyRankResponse
	if err := f.getJSON(ctx, "/api/qt/stock/fflow/rank", q, &resp); err != nil {
		return nil, fmt.Errorf("hkex: top holdings: %w", err)
	}

	rows := parseRankDiff(resp.Data.Diff, normalizeDate(date))
	f.logger.Debug().
		Time("date", date).
		Int("rows", len(rows)).
		Msg("fetched top holdings")
	return rows, nil
}

// getJSON performs a GET with the fetcher's User-Agent and decodes the
// JSON body into v. It applies the polite RequestInterval delay before
// the call so concurrent callers each self-throttle.
func (f *EastmoneyNorthboundFetcher) getJSON(ctx context.Context, path string, q url.Values, v interface{}) error {
	if f.HTTPClient == nil {
		f.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if f.RequestInterval > 0 {
		// Polite delay. We use a timer instead of time.Sleep so the
		// caller's ctx can cancel the wait.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.RequestInterval):
		}
	}
	u := strings.TrimRight(f.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("eastmoney 429 rate limited: %s", string(body))
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("eastmoney %d: %s", resp.StatusCode, string(body))
	}
	if v == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Eastmoney response shapes
// ----------------------------------------------------------------------------

// eastmoneyKamtResponse mirrors the kamt.kline payload.
//
// Data.KLines is a slice of CSV strings. Column order (per the fields2
// request): date, sh_net_buy, sz_net_buy, sh_buy, sh_sell, sz_buy,
// sz_sell, total_net_buy, total_buy, total_sell, ...
//
// We only need the first ~7 columns; extra trailing columns are
// ignored by parseKamtRow.
type eastmoneyKamtResponse struct {
	Data struct {
		Code   string   `json:"code"`
		Name   string   `json:"name"`
		KLines []string `json:"klines"`
	} `json:"data"`
	RC int `json:"rc"`
}

// eastmoneyStockFlowResponse mirrors the per-stock fflow daykline.
//
// KLines column order: date, close, change_pct, main_net, main_buy,
// main_sell, main_net_ratio, super_buy, super_sell, super_net, ...
type eastmoneyStockFlowResponse struct {
	Data struct {
		Code   string   `json:"code"`
		Name   string   `json:"name"`
		KLines []string `json:"klines"`
	} `json:"data"`
	RC int `json:"rc"`
}

// eastmoneyRankResponse mirrors the fflow/rank payload.
//
// Data.Diff is a slice of field maps; f12=symbol, f14=name, f62=net buy,
// f55/f56 ≈ buy/sell, f57/f58 ≈ ratios. Eastmoney's schema is loose, so
// we use toFloat64Safe / stringField to tolerate missing keys.
type eastmoneyRankResponse struct {
	Data struct {
		Total int                       `json:"total"`
		Diff  []map[string]interface{}   `json:"diff"`
	} `json:"data"`
	RC int `json:"rc"`
}

// ----------------------------------------------------------------------------
// Parsers
// ----------------------------------------------------------------------------

// findKamtRowByDate returns the klines row whose first CSV field matches
// `date` (YYYY-MM-DD). Returns ("", false) if no row matches.
func findKamtRowByDate(klines []string, date time.Time) (string, bool) {
	want := date.Format("2006-01-02")
	for _, line := range klines {
		parts := strings.SplitN(line, ",", 2)
		if len(parts) == 0 {
			continue
		}
		if strings.TrimSpace(parts[0]) == want {
			return line, true
		}
	}
	return "", false
}

// parseKamtRow turns a kamt.kline CSV row into a NorthboundFlow.
//
// Column order (1-indexed):
//  1=date 2:sh_net 3:sz_net 4:sh_buy 5:sh_sell 6:sz_buy 7:sz_sell
//  8:total_net 9:total_buy 10:total_sell
//
// Eastmoney reports sh_net/sz_net/total_net in 亿元 (100M CNY) and the
// buy/sell aggregates in 万元 (10K CNY); we preserve those units as
// documented on NorthboundFlow.
func parseKamtRow(line string) *NorthboundFlow {
	parts := strings.Split(line, ",")
	flow := &NorthboundFlow{}
	if len(parts) > 1 {
		flow.SHConnectNetBuy = parseFloatStr(parts[1])
	}
	if len(parts) > 2 {
		flow.SZConnectNetBuy = parseFloatStr(parts[2])
	}
	if len(parts) > 7 {
		flow.TotalNetBuy = parseFloatStr(parts[7])
	} else {
		// Fallback: total = sh + sz. Useful when the upstream omits
		// the total column (some legacy klt values do).
		flow.TotalNetBuy = flow.SHConnectNetBuy + flow.SZConnectNetBuy
	}
	if len(parts) > 8 {
		flow.TotalBuy = parseFloatStr(parts[8])
	}
	if len(parts) > 9 {
		flow.TotalSell = parseFloatStr(parts[9])
	}
	return flow
}

// parseStockFlowKLines turns the per-stock fflow klines into StockFlow
// records. Column order:
//  1:date 2:close 3:change_pct 4:main_net 5:main_buy 6:main_sell
//  7:main_net_ratio ...
//
// We map main_net → NetBuy, main_buy → BuyAmount, main_sell → SellAmount,
// and main_net_ratio → HoldingRatio (the latter is a loose mapping: the
// true northbound holding ratio is only available via the hsgt endpoint,
// but main_net_ratio is the closest proxy on the push2 surface).
func parseStockFlowKLines(klines []string, symbol, name string) []StockFlow {
	rows := make([]StockFlow, 0, len(klines))
	for _, line := range klines {
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		date, err := time.Parse("2006-01-02", strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		row := StockFlow{
			Symbol:     symbol,
			Name:       name,
			Date:       date,
			NetBuy:     parseFloatStr(parts[3]),
		}
		if len(parts) > 4 {
			row.BuyAmount = parseFloatStr(parts[4])
		}
		if len(parts) > 5 {
			row.SellAmount = parseFloatStr(parts[5])
		}
		if len(parts) > 6 {
			row.HoldingRatio = parseFloatStr(parts[6])
		}
		rows = append(rows, row)
	}
	return rows
}

// parseRankDiff turns the fflow/rank diff slice into StockFlow records.
// f12=symbol, f14=name, f62=net buy, f55=buy, f56=sell, f57=holding ratio.
func parseRankDiff(diff []map[string]interface{}, date time.Time) []StockFlow {
	rows := make([]StockFlow, 0, len(diff))
	for _, row := range diff {
		sym := stringField(row, "f12")
		name := stringField(row, "f14")
		if sym == "" {
			continue
		}
		sym = normalizeSymbol(sym)
		rows = append(rows, StockFlow{
			Symbol:       sym,
			Name:         name,
			NetBuy:       toFloat64Safe(row["f62"]),
			BuyAmount:    toFloat64Safe(row["f55"]),
			SellAmount:   toFloat64Safe(row["f56"]),
			HoldingRatio: toFloat64Safe(row["f57"]),
			Date:         date,
		})
	}
	return rows
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// symbolToEastmoneySecid converts a project symbol ("600519.SH") to
// Eastmoney's secid form ("1.600519"). Mirrors the logic in the
// sibling source package so hkex stays self-contained.
func symbolToEastmoneySecid(sym string) (string, error) {
	if len(sym) < 3 {
		return "", fmt.Errorf("invalid symbol %q", sym)
	}
	switch sym[len(sym)-2:] {
	case "SH":
		return "1." + sym[:len(sym)-3], nil
	case "SZ":
		return "0." + sym[:len(sym)-3], nil
	default:
		switch sym[0] {
		case '6', '9':
			return "1." + sym, nil
		default:
			return "0." + sym, nil
		}
	}
}

// normalizeSymbol appends the .SH / .SZ suffix to a bare 6-digit code.
func normalizeSymbol(sym string) string {
	if len(sym) == 6 && !strings.Contains(sym, ".") {
		switch sym[0] {
		case '6', '9':
			return sym + ".SH"
		default:
			return sym + ".SZ"
		}
	}
	return sym
}

// normalizeDate truncates a time to midnight UTC so two timestamps that
// differ only in their wall-clock component compare equal.
func normalizeDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// eastmoneyStockFlowLmt computes the minimum lmt to cover [start, end].
// Mirrors the source package's eastmoneyCapitalFlowLmt but kept local
// to avoid an import cycle.
func eastmoneyStockFlowLmt(start, end time.Time) int {
	const (
		headroom = 1.2
		maxLmt   = 8000
		fallback = 1000
	)
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return fallback
	}
	days := int(end.Sub(start).Hours()/24) + 1
	lmt := int(float64(days)*headroom) + 1
	if lmt > maxLmt {
		return maxLmt
	}
	if lmt < 1 {
		return 1
	}
	return lmt
}

// parseFloatStr parses a float, returning 0 on error. Trimmed because
// Eastmoney occasionally pads CSV fields with spaces.
func parseFloatStr(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// toFloat64Safe coerces an interface{} (from a JSON map) to float64.
// Handles float64, int, json.Number, and numeric strings.
func toFloat64Safe(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		return parseFloatStr(x)
	default:
		return 0
	}
}

// stringField returns the string value of key in m, or "" if absent.
func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// ----------------------------------------------------------------------------
// ExchangeRateConverter
// ----------------------------------------------------------------------------

// defaultHKDCNYRate is the fallback HKD→CNY rate used when no live FX
// source is configured. ~0.91 is the 2024-2026 band; callers should
// override via SetRate rather than editing this constant.
const defaultHKDCNYRate = 0.91

// ExchangeRateConverter converts between HKD and CNY.
//
// The default implementation is a stub: FetchRate returns a configured
// constant. A live implementation can be added later by embedding this
// struct and overriding FetchRate (or by wiring an FX adapter into the
// constructor — left as a Phase 5 task per ODR-011).
type ExchangeRateConverter struct {
	// HKDCNY is the HKD→CNY rate. Must be > 0.
	HKDCNY float64
	// logger is kept so a future live implementation can log fetches.
	logger zerolog.Logger
}

// NewExchangeRateConverter constructs a converter with the default rate.
func NewExchangeRateConverter() *ExchangeRateConverter {
	return &ExchangeRateConverter{
		HKDCNY: defaultHKDCNYRate,
		logger: zerolog.Nop(),
	}
}

// SetRate overrides the HKD→CNY rate. Returns an error if rate <= 0
// so a misconfigured upstream cannot silently zero out conversions.
func (c *ExchangeRateConverter) SetRate(rate float64) error {
	if rate <= 0 {
		return fmt.Errorf("hkex: exchange rate must be > 0, got %v", rate)
	}
	c.HKDCNY = rate
	return nil
}

// SetLogger wires a zerolog.Logger.
func (c *ExchangeRateConverter) SetLogger(l zerolog.Logger) {
	c.logger = l.With().Str("component", "hkex.fx").Logger()
}

// FetchRate returns the current HKD→CNY rate. The mock implementation
// just returns the configured rate; a live implementation would hit an
// FX API here. The ctx is accepted for interface conformance and to
// make a future live implementation drop-in compatible.
func (c *ExchangeRateConverter) FetchRate(ctx context.Context) (float64, error) {
	if c.HKDCNY <= 0 {
		return 0, fmt.Errorf("hkex: exchange rate not configured")
	}
	// Respect cancellation even in the mock so callers can't hang on
	// a future live implementation that ignores ctx.
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	return c.HKDCNY, nil
}

// HKDToCNY converts an HKD amount to CNY using the current rate.
func (c *ExchangeRateConverter) HKDToCNY(hkd float64) float64 {
	return hkd * c.HKDCNY
}

// CNYToHKD converts a CNY amount to HKD using the current rate.
// Returns 0 (not +Inf) when the rate is unset to avoid poisoning
// downstream factor math with Inf/NaN.
func (c *ExchangeRateConverter) CNYToHKD(cny float64) float64 {
	if c.HKDCNY == 0 {
		return 0
	}
	return cny / c.HKDCNY
}
