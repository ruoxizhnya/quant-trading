package source

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
)

// EastmoneyClient is a minimal HTTP client for the Eastmoney push2 /
// datacenter APIs. It is exported so adapters can be unit-tested with
// a fake transport.
type EastmoneyClient struct {
	// BaseURL is the root of the push2 endpoint (https://push2.eastmoney.com).
	BaseURL string
	// DataCenterURL is the root of the datacenter endpoint
	// (https://datacenter-web.eastmoney.com).
	DataCenterURL string
	// HTTPClient is the underlying HTTP client. If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// NewEastmoneyClient constructs a default client.
func NewEastmoneyClient() *EastmoneyClient {
	return &EastmoneyClient{
		BaseURL:       "https://push2.eastmoney.com",
		DataCenterURL: "https://datacenter-web.eastmoney.com",
		HTTPClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// GetJSON performs a GET request and decodes the JSON response into v.
// All parameters in q are added as URL query parameters.
func (c *EastmoneyClient) GetJSON(ctx context.Context, base string, path string, q url.Values, v interface{}) error {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	u := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("eastmoney: build request: %w", err)
	}
	req.Header.Set("User-Agent", "quant-trading/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("eastmoney: %w", err)
	}
	defer resp.Body.Close()
	// CR-52 (ODR-012): Eastmoney signals throttling via 429; surface it as
	// ErrRateLimited so the Registry's fallback chain skips to the next
	// adapter instead of treating it as a generic upstream outage (which
	// the user would see as "Eastmoney is down" — misleading because the
	// real cause is rate-limit, not unavailability). 5xx remains
	// ErrUpstreamUnavailable: those are real outages and deserve a
	// different operator response.
	if resp.StatusCode == http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: eastmoney 429: %s", ErrRateLimited, string(body))
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: eastmoney %d: %s", ErrUpstreamUnavailable, resp.StatusCode, string(body))
	}
	if v == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("eastmoney: decode: %w", err)
	}
	return nil
}

// EastmoneyAdapter implements DataSourceAdapter for the Eastmoney push2
// capital flow endpoint (and extensible to other Eastmoney APIs).
//
// Sprint 1 scope: DataTypeCapitalFlow only.
// Sprint 2+ will add sector lists (slist), top_list (龙虎榜), and
// limit_up_pool (涨停池) by adding more methods to the transport and
// routing in Fetch.
type EastmoneyAdapter struct {
	AdapterBase
	client *EastmoneyClient
}

// NewEastmoneyAdapter constructs an EastmoneyAdapter.
func NewEastmoneyAdapter(client *EastmoneyClient) *EastmoneyAdapter {
	return &EastmoneyAdapter{
		AdapterBase: NewAdapterBase("eastmoney", client != nil),
		client:      client,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *EastmoneyAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
//
// Only the data types whose Fetch implementation exists are listed here.
// Sectors, top list, and limit-up pool are served by dedicated adapters
// (EastmoneySectorsAdapter, EastmoneyTopListAdapter) so they are NOT
// included in this adapter's SupportedTypes — listing them would
// mislead the registry into routing requests here.
func (a *EastmoneyAdapter) SupportedTypes() []string {
	return []string{
		DataTypeCapitalFlow,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *EastmoneyAdapter) Schema(dataType string) (DataSchema, error) {
	switch dataType {
	case DataTypeCapitalFlow:
		return DataSchema{
			DataType: DataTypeCapitalFlow,
			Fields: []SchemaField{
				{Name: "main_net", Type: "float", Required: true, Unit: "yuan"},
				{Name: "super_net", Type: "float", Required: false, Unit: "yuan"},
				{Name: "large_net", Type: "float", Required: false, Unit: "yuan"},
				{Name: "medium_net", Type: "float", Required: false, Unit: "yuan"},
				{Name: "small_net", Type: "float", Required: false, Unit: "yuan"},
				{Name: "main_net_ratio", Type: "float", Required: false},
				{Name: "retail_net", Type: "float", Required: false, Unit: "yuan"},
				{Name: "retail_net_ratio", Type: "float", Required: false},
				{Name: "close_price", Type: "float", Required: false, Unit: "yuan"},
				{Name: "change_pct", Type: "float", Required: false, Unit: "percent"},
			},
		}, nil
	case DataTypeSectors:
		return DataSchema{
			DataType: DataTypeSectors,
			Fields: []SchemaField{
				{Name: "sector_name", Type: "string", Required: true},
				{Name: "change_pct", Type: "float", Required: false, Unit: "percent"},
				{Name: "leading_symbol", Type: "string", Required: false},
			},
		}, nil
	case DataTypeTopList:
		return DataSchema{
			DataType: DataTypeTopList,
			Fields: []SchemaField{
				{Name: "name", Type: "string", Required: true},
				{Name: "net_buy", Type: "float", Required: true, Unit: "yuan"},
				{Name: "buy_amount", Type: "float", Required: false, Unit: "yuan"},
				{Name: "sell_amount", Type: "float", Required: false, Unit: "yuan"},
				{Name: "turnover", Type: "float", Required: false, Unit: "yuan"},
				{Name: "reason", Type: "string", Required: false},
			},
		}, nil
	case DataTypeLimitUpPool:
		return DataSchema{
			DataType: DataTypeLimitUpPool,
			Fields: []SchemaField{
				{Name: "name", Type: "string", Required: true},
				{Name: "limit_price", Type: "float", Required: true, Unit: "yuan"},
				{Name: "first_time", Type: "datetime", Required: false},
				{Name: "last_time", Type: "datetime", Required: false},
				{Name: "limit_times", Type: "int", Required: false},
				{Name: "continuous", Type: "int", Required: false},
				{Name: "industry", Type: "string", Required: false},
			},
		}, nil
	default:
		return DataSchema{}, fmt.Errorf("%w: eastmoney does not serve %s", ErrUnsupported, dataType)
	}
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *EastmoneyAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if a.client == nil {
		return nil, fmt.Errorf("eastmoney adapter: nil client")
	}
	switch req.DataType {
	case DataTypeCapitalFlow:
		return a.fetchCapitalFlow(ctx, req)
	default:
		return nil, fmt.Errorf("%w: eastmoney: %s (Sprint 1 supports only capital_flow)", ErrUnsupported, req.DataType)
	}
}

// fetchCapitalFlow calls the push2 capital flow endpoint.
//
// URL pattern (push2):
//
//	/api/qt/stock/kline/get?secid=1.600519&fields1=...&fields2=...&klt=1&fqt=1&beg=...&end=...
//
// For "individual stock capital flow" we use:
//
//	/api/qt/stock/fflow/daykline/get?secid=1.600519&fields1=...&fields2=...&klt=1&fqt=1&beg=...&end=...
//
// Period mapping:
//   - "1d" / "daily"  →  period column = "daily"
//   - "5d"            →  period column = "5d"
//   - "10d"           →  period column = "10d"
//   - "60d"           →  period column = "60d"
func (a *EastmoneyAdapter) fetchCapitalFlow(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("eastmoney capital_flow: symbols required")
	}
	period := req.Period
	if period == "" {
		period = "daily"
	}
	items := make([]DataItem, 0)
	for _, sym := range req.Symbols {
		rows, err := a.fetchCapitalFlowForSymbol(ctx, sym, period, req.StartDate, req.EndDate)
		if err != nil {
			return nil, fmt.Errorf("eastmoney capital_flow %s: %w", sym, err)
		}
		items = append(items, rows...)
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// eastmoneyPush2Response is the generic response wrapper. The actual
// data is in Data.KLines (a slice of strings) or Data.Diff.
type eastmoneyPush2Response struct {
	Data struct {
		Code   string        `json:"code"`
		Name   string        `json:"name"`
		KLines []string      `json:"klines"`
		Total  int           `json:"total"`
		Diff   []interface{} `json:"diff"`
	} `json:"data"`
	RC int `json:"rc"`
}

// eastmoneyCapitalFlowRow is the parsed row from a capital flow response.
// The push2 endpoint returns the row as a CSV string: "date,main_net,...".
type eastmoneyCapitalFlowRow struct {
	Date         time.Time
	MainNet      float64
	SuperNet     float64
	LargeNet     float64
	MediumNet    float64
	SmallNet     float64
	MainNetRatio float64
	RetailNet    float64
	RetailRatio  float64
	ClosePrice   float64
	ChangePct    float64
}

func (a *EastmoneyAdapter) fetchCapitalFlowForSymbol(ctx context.Context, sym, period string, start, end time.Time) ([]DataItem, error) {
	secid, err := symbolToEastmoneySecid(sym)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("secid", secid)
	q.Set("fields1", "f1,f2,f3,f4")
	q.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61,f62,f63,f64,f65")
	klt := 1 // daily
	q.Set("klt", strconv.Itoa(klt))
	q.Set("fqt", "1")
	if !start.IsZero() {
		q.Set("beg", start.Format("20060102"))
	}
	if !end.IsZero() {
		q.Set("end", end.Format("20060102"))
	}
	// CR-41 (ODR-012): lmt must match the requested time window. The
	// previous hard-coded 1000 silently truncated anything beyond ~4
	// years of daily bars (≈1000 trading days). Compute the minimum
	// lmt that covers the window, with a sensible cap.
	q.Set("lmt", strconv.Itoa(eastmoneyCapitalFlowLmt(klt, start, end)))
	q.Set("klines", "0")

	var resp eastmoneyPush2Response
	if err := a.client.GetJSON(ctx, a.client.BaseURL, "/api/qt/stock/fflow/daykline/get", q, &resp); err != nil {
		return nil, err
	}
	rows, err := parseEastmoneyCapitalFlowKLines(resp.Data.KLines)
	if err != nil {
		return nil, fmt.Errorf("eastmoney: parse %s: %w", sym, err)
	}
	items := make([]DataItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: r.Date,
			Data: map[string]interface{}{
				"period":           period,
				"main_net":         r.MainNet,
				"super_net":        r.SuperNet,
				"large_net":        r.LargeNet,
				"medium_net":       r.MediumNet,
				"small_net":        r.SmallNet,
				"main_net_ratio":   r.MainNetRatio,
				"retail_net":       r.RetailNet,
				"retail_net_ratio": r.RetailRatio,
				"close_price":      r.ClosePrice,
				"change_pct":       r.ChangePct,
			},
		})
	}
	return items, nil
}

// eastmoneyCapitalFlowLmt computes the minimum `lmt` (max K-line
// points) the Eastmoney capital-flow endpoint must return to fully
// cover the [start, end] window. CR-41 (ODR-012): the previous
// hard-coded `lmt=1000` silently truncated any window wider than ~4
// years of daily bars — the upstream caps results at lmt and we had
// no signal that data was missing.
//
// The conversion factor per `klt` (K-line type) is approximate
// (Chinese A-share calendar ≈ 244 trading days/year, with weeks and
// months computed from that). We always add a small headroom of 20%
// to absorb months with extra trading days and we clamp the result
// to a sensible upper bound so a 50-year window can't blow up the
// request.
func eastmoneyCapitalFlowLmt(klt int, start, end time.Time) int {
	const (
		headroom     = 1.2  // 20% safety margin
		maxLmt       = 8000 // ~33 years of daily bars; above this, paginate
		defaultDaily = 1000 // upstream default — used when no window is given
	)
	if start.IsZero() || end.IsZero() || !end.After(start) {
		// No window (or invalid): defer to upstream default. CR-41
		// regression: the previous code also used 1000 in this case,
		// so we preserve that behaviour.
		return defaultDaily
	}
	// Approximate period length per klt value (Eastmoney convention):
	//   1  → 1 day,  5 → 5 min, 15 → 15 min, 30 → 30 min, 60 → 60 min,
	//   101 → 1 week, 102 → 1 month
	daysPerPeriod := kltToDays(klt)
	if daysPerPeriod <= 0 {
		daysPerPeriod = 1
	}
	windowDays := int(end.Sub(start).Hours()/24) + 1
	periods := int(float64(windowDays)/float64(daysPerPeriod)*headroom) + 1
	if periods > maxLmt {
		return maxLmt
	}
	if periods < 1 {
		return 1
	}
	return periods
}

// kltToDays maps Eastmoney's klt (K-line type) parameter to an
// approximate number of calendar days per period. Used by
// eastmoneyCapitalFlowLmt to translate the requested time window
// into a row count.
//
// CR-41: previously, lmt was hard-coded to 1000 regardless of
// klt, so weekly/monthly requests also got truncated at 1000
// periods (≈19 years for weekly, ≈83 years for monthly — the
// latter accidentally worked; the former didn't).
func kltToDays(klt int) int {
	switch klt {
	case 1:
		return 1 // 1 day
	case 5, 15, 30, 60:
		return 1 // intraday — one trading day, multiple bars
	case 101:
		return 7 // 1 week
	case 102:
		return 30 // 1 month
	default:
		return 1
	}
}

// parseEastmoneyCapitalFlowKLines parses the comma-separated rows from
// the push2 capital-flow endpoint. Field order (per the api docs):
//
//	date, close, change_pct, main_net, main_buy, main_sell, main_net_ratio,
//	super_buy, super_sell, super_net,
//	large_buy, large_sell, large_net,
//	medium_buy, medium_sell, medium_net,
//	small_buy, small_sell, small_net
func parseEastmoneyCapitalFlowKLines(lines []string) ([]eastmoneyCapitalFlowRow, error) {
	rows := make([]eastmoneyCapitalFlowRow, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		date, err := time.Parse("2006-01-02", parts[0])
		if err != nil {
			continue
		}
		row := eastmoneyCapitalFlowRow{
			Date:       date,
			MainNet:    parseFloat(parts[3]),
			ClosePrice: parseFloat(parts[1]),
			ChangePct:  parseFloat(parts[2]),
		}
		if len(parts) > 6 {
			row.MainNetRatio = parseFloat(parts[6])
		}
		// super_buy, super_sell, super_net
		if len(parts) > 9 {
			row.SuperNet = parseFloat(parts[9])
		}
		// large_buy, large_sell, large_net
		if len(parts) > 12 {
			row.LargeNet = parseFloat(parts[12])
		}
		// medium_buy, medium_sell, medium_net
		if len(parts) > 15 {
			row.MediumNet = parseFloat(parts[15])
		}
		// small_buy, small_sell, small_net
		if len(parts) > 18 {
			row.SmallNet = parseFloat(parts[18])
		}
		// retail_net is conventionally the negative of (super + large + medium + small)
		row.RetailNet = -(row.SuperNet + row.LargeNet + row.MediumNet + row.SmallNet)
		// CR-03 (ODR-012): RetailRatio is retail's share of (main + retail) net flow.
		// Previous formula `-100 * (1 - MainNetRatio/100)` was unrelated to retail
		// and polluted downstream CapitalFlowFactor/SentimentFactor. Total turnover
		// per bar is not exposed by this endpoint, so we use |MainNet|+|RetailNet|
		// as the denominator (zero-guard for first bar of a session).
		total := row.MainNet + row.RetailNet
		if row.RetailNet != 0 && total != 0 {
			row.RetailRatio = row.RetailNet / total * 100
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// symbolToEastmoneySecid converts a project symbol to Eastmoney's
// "secid" form: "1.600519" for SH, "0.000001" for SZ.
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

// parseFloat is a small helper that returns 0 on error.
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
// Eastmoney has no dedicated health endpoint; we hit a tiny static
// resource (a recent capital-flow record for SH index 000001).
func (a *EastmoneyAdapter) HealthCheck(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("eastmoney adapter: nil client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("secid", "1.000001")
	q.Set("fields1", "f1")
	q.Set("fields2", "f51")
	q.Set("klt", "1")
	q.Set("fqt", "1")
	var resp eastmoneyPush2Response
	if err := a.client.GetJSON(probeCtx, a.client.BaseURL, "/api/qt/stock/fflow/daykline/get", q, &resp); err != nil {
		return fmt.Errorf("%w: eastmoney: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *EastmoneyAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 120,
		Burst:             10,
	}
}

// String renders a debug-friendly summary.
func (a *EastmoneyAdapter) String() string {
	return "EastmoneyAdapter{name=" + a.Name() + ", enabled=" + strconv.FormatBool(a.Enabled()) + "}"
}
