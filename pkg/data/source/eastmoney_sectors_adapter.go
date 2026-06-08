package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// EastmoneySectorsAdapter implements DataSourceAdapter for the Eastmoney
// slist endpoint: industry/concept sector lists, sector membership
// (stock → sector mapping), and the daily sector snapshot.
//
// API reference (per cn_market_data.yaml):
//   GET https://push2.eastmoney.com/api/qt/clist/get
//     ?pn=1&pz=200&po=1&np=1&fltt=2&invt=2&fid=f3
//     &fs=m:90+t:2        # industry sectors
//     &fs=m:90+t:3        # concept sectors
//     &fields=f1,f2,f3,f4,f5,f6,f12,f14,f20
type EastmoneySectorsAdapter struct {
	AdapterBase
	client *EastmoneyClient
}

// NewEastmoneySectorsAdapter constructs the adapter.
func NewEastmoneySectorsAdapter(client *EastmoneyClient) *EastmoneySectorsAdapter {
	return &EastmoneySectorsAdapter{
		AdapterBase: NewAdapterBase("eastmoney_sectors", client != nil),
		client:      client,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *EastmoneySectorsAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *EastmoneySectorsAdapter) SupportedTypes() []string {
	return []string{
		DataTypeSectors,
		DataTypeStockSector,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *EastmoneySectorsAdapter) Schema(dataType string) (DataSchema, error) {
	switch dataType {
	case DataTypeSectors:
		return DataSchema{
			DataType: DataTypeSectors,
			Fields: []SchemaField{
				{Name: "sector_name", Type: "string", Required: true},
				{Name: "change_pct", Type: "float", Required: false, Unit: "percent"},
				{Name: "leading_symbol", Type: "string", Required: false},
			},
		}, nil
	case DataTypeStockSector:
		return DataSchema{
			DataType: DataTypeStockSector,
			Fields: []SchemaField{
				{Name: "sector_name", Type: "string", Required: true},
			},
		}, nil
	default:
		return DataSchema{}, fmt.Errorf("%w: eastmoney-sectors does not serve %s", ErrUnsupported, dataType)
	}
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *EastmoneySectorsAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if a.client == nil {
		return nil, fmt.Errorf("eastmoney-sectors adapter: nil client")
	}
	switch req.DataType {
	case DataTypeSectors:
		return a.fetchSectorList(ctx, req)
	case DataTypeStockSector:
		return a.fetchStockSectors(ctx, req)
	default:
		return nil, fmt.Errorf("%w: eastmoney-sectors: %s", ErrUnsupported, req.DataType)
	}
}

// sectorListResponse mirrors the push2 clist response shape.
type sectorListResponse struct {
	Data struct {
		Total int                       `json:"total"`
		Diff  []map[string]interface{}  `json:"diff"`
	} `json:"data"`
	RC int `json:"rc"`
}

// fetchSectorList fetches the industry/concept sector list snapshot.
// Extra["category"] = "industry" | "concept" | "style" (default: industry).
func (a *EastmoneySectorsAdapter) fetchSectorList(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	category := "industry"
	if req.Extra != nil {
		if c, ok := req.Extra["category"].(string); ok && c != "" {
			category = c
		}
	}
	fs := "m:90+t:2" // industry default
	switch category {
	case "concept":
		fs = "m:90+t:3"
	case "style":
		fs = "m:90+t:1"
	}
	q := url.Values{}
	q.Set("pn", "1")
	q.Set("pz", "200")
	q.Set("po", "1")
	q.Set("np", "1")
	q.Set("fltt", "2")
	q.Set("invt", "2")
	q.Set("fid", "f3")
	q.Set("fs", fs)
	q.Set("fields", "f1,f2,f3,f4,f5,f6,f12,f14,f20")

	var resp sectorListResponse
	if err := a.client.GetJSON(ctx, a.client.BaseURL, "/api/qt/clist/get", q, &resp); err != nil {
		return nil, err
	}
	tradeDate := time.Now().UTC()
	if !req.StartDate.IsZero() {
		tradeDate = req.StartDate
	}
	items := make([]DataItem, 0, len(resp.Data.Diff))
	for _, row := range resp.Data.Diff {
		// f12: sector code, f14: sector name, f3: change_pct,
		// f20: leading symbol, f4: leading change.
		code, _ := row["f12"].(string)
		name, _ := row["f14"].(string)
		if code == "" || name == "" {
			continue
		}
		changePct := toFloat64Safe(row["f3"])
		leading, _ := row["f20"].(string)
		items = append(items, DataItem{
			Symbol:    code, // sector code in Symbol slot
			TradeTime: tradeDate,
			Data: map[string]interface{}{
				"sector_code":     code,
				"sector_name":     name,
				"category":        category,
				"change_pct":      changePct,
				"leading_symbol":  leading,
				"leading_change":  toFloat64Safe(row["f4"]),
			},
		})
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// fetchStockSectors fetches sector membership for a list of symbols.
// Eastmoney returns sector codes per stock via
//   GET /api/qt/stock/get?secid=1.600519&fields=f100,f102,...
// but the most reliable approach is the sector list endpoint with a
// per-stock filter:
//   GET /api/qt/clist/get?fs=m:90+t:2+f:!2,m:90+t:3+f:!2&...
// (We use the simpler approach: call per symbol.)
func (a *EastmoneySectorsAdapter) fetchStockSectors(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("eastmoney-sectors stock_sector: symbols required")
	}
	items := make([]DataItem, 0)
	for _, sym := range req.Symbols {
		rows, err := a.fetchStockSectorsForSymbol(ctx, sym)
		if err != nil {
			return nil, fmt.Errorf("eastmoney-sectors %s: %w", sym, err)
		}
		items = append(items, rows...)
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// stockSectorResponse is the per-symbol sector membership response.
type stockSectorResponse struct {
	Data struct {
		Code string                   `json:"code"`
		Name string                   `json:"name"`
		Industry map[string]interface{} `json:"industry"`
	} `json:"data"`
}

func (a *EastmoneySectorsAdapter) fetchStockSectorsForSymbol(ctx context.Context, sym string) ([]DataItem, error) {
	secid, err := symbolToEastmoneySecid(sym)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("secid", secid)
	q.Set("fields", "f100,f102")
	q.Set("invt", "2")
	q.Set("fltt", "2")

	var resp stockSectorResponse
	if err := a.client.GetJSON(ctx, a.client.BaseURL, "/api/qt/stock/get", q, &resp); err != nil {
		return nil, err
	}
	// Industry field may be a string like "银行" or a list of sector codes.
	var industries []string
	if s, ok := resp.Data.Industry["f100"].(string); ok && s != "" {
		industries = append(industries, s)
	}
	if s, ok := resp.Data.Industry["f102"].(string); ok && s != "" {
		industries = append(industries, s)
	}
	items := make([]DataItem, 0, len(industries))
	now := time.Now().UTC()
	for _, ind := range industries {
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: now,
			Data: map[string]interface{}{
				"sector_code": ind,
				"sector_name": ind,
			},
		})
	}
	return items, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
func (a *EastmoneySectorsAdapter) HealthCheck(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("eastmoney-sectors adapter: nil client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("pn", "1")
	q.Set("pz", "1")
	q.Set("po", "1")
	q.Set("np", "1")
	q.Set("fltt", "2")
	q.Set("invt", "2")
	q.Set("fid", "f3")
	q.Set("fs", "m:90+t:2")
	q.Set("fields", "f1")
	var resp sectorListResponse
	if err := a.client.GetJSON(probeCtx, a.client.BaseURL, "/api/qt/clist/get", q, &resp); err != nil {
		return fmt.Errorf("%w: eastmoney-sectors: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *EastmoneySectorsAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 120,
		Burst:             10,
	}
}

// String renders a debug-friendly summary.
func (a *EastmoneySectorsAdapter) String() string {
	return "EastmoneySectorsAdapter{name=" + a.Name() + "}"
}

// ----------------------------------------------------------------------------
// Eastmoney TopList adapter (龙虎榜)
// ----------------------------------------------------------------------------

// EastmoneyTopListAdapter implements DataSourceAdapter for the Eastmoney
// 龙虎榜 (top_list) and 涨停池 (limit_up_pool) endpoints.
type EastmoneyTopListAdapter struct {
	AdapterBase
	client *EastmoneyClient
}

// NewEastmoneyTopListAdapter constructs the adapter.
func NewEastmoneyTopListAdapter(client *EastmoneyClient) *EastmoneyTopListAdapter {
	return &EastmoneyTopListAdapter{
		AdapterBase: NewAdapterBase("eastmoney_toplist", client != nil),
		client:      client,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *EastmoneyTopListAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *EastmoneyTopListAdapter) SupportedTypes() []string {
	return []string{
		DataTypeTopList,
		DataTypeLimitUpPool,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *EastmoneyTopListAdapter) Schema(dataType string) (DataSchema, error) {
	switch dataType {
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
		return DataSchema{}, fmt.Errorf("%w: eastmoney-toplist does not serve %s", ErrUnsupported, dataType)
	}
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *EastmoneyTopListAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if a.client == nil {
		return nil, fmt.Errorf("eastmoney-toplist adapter: nil client")
	}
	switch req.DataType {
	case DataTypeTopList:
		return a.fetchTopList(ctx, req)
	case DataTypeLimitUpPool:
		return a.fetchLimitUpPool(ctx, req)
	default:
		return nil, fmt.Errorf("%w: eastmoney-toplist: %s", ErrUnsupported, req.DataType)
	}
}

// topListResponse is the response for the 龙虎榜 endpoint.
type topListResponse struct {
	Result struct {
		Data []map[string]interface{} `json:"data"`
	} `json:"result"`
	RC int `json:"rc"`
}

// fetchTopList fetches the dragon-tiger list.
// API: https://datacenter-web.eastmoney.com/api/data/v1/get
//   ?reportName=RPT_DAILYBILLBOARD_DETAILS
//   &columns=ALL&filter=&pageNumber=1&pageSize=50
//   &sortColumns=TRADE_DATE&sortTypes=-1
func (a *EastmoneyTopListAdapter) fetchTopList(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	q := url.Values{}
	q.Set("reportName", "RPT_DAILYBILLBOARD_DETAILS")
	q.Set("columns", "ALL")
	if !req.StartDate.IsZero() {
		q.Set("filter", fmt.Sprintf("(TRADE_DATE>='%s')", req.StartDate.Format("2006-01-02")))
	}
	q.Set("pageNumber", "1")
	q.Set("pageSize", "100")
	q.Set("sortColumns", "TRADE_DATE")
	q.Set("sortTypes", "-1")

	var resp topListResponse
	if err := a.client.GetJSON(ctx, a.client.DataCenterURL, "/api/data/v1/get", q, &resp); err != nil {
		return nil, err
	}
	items := make([]DataItem, 0, len(resp.Result.Data))
	for _, row := range resp.Result.Data {
		sym, _ := row["SECURITY_CODE"].(string)
		if sym == "" {
			continue
		}
		// Ensure the symbol has an exchange suffix.
		if len(sym) == 6 {
			switch sym[0] {
			case '6', '9':
				sym += ".SH"
			default:
				sym += ".SZ"
			}
		}
		tradeDate := parseEastmoneyDate(row["TRADE_DATE"])
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: tradeDate,
			Data: map[string]interface{}{
				"name":         stringFromMap(row, "SECURITY_NAME_ABBR"),
				"net_buy":      toFloat64Safe(row["BILLBOARD_NET_AMT"]),
				"buy_amount":   toFloat64Safe(row["BILLBOARD_BUY_AMT"]),
				"sell_amount":  toFloat64Safe(row["BILLBOARD_SELL_AMT"]),
				"turnover":     toFloat64Safe(row["TURNOVER"]),
				"reason":       stringFromMap(row, "EXPLAIN"),
				"close_price":  toFloat64Safe(row["CLOSE_PRICE"]),
				"change_pct":   toFloat64Safe(row["CHANGE_RATE"]),
			},
		})
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// fetchLimitUpPool fetches the daily limit-up pool.
// API: https://push2.eastmoney.com/api/qt/stock/get
//   ?secid=1.000001&fields=...&mktnum=...
// For the full pool we use:
//   https://push2.eastmoney.com/api/qt/clist/get
//     ?fs=m:1+t:2,m:0+t:6  (sh+sz main boards, limit-up)
//     &fields=f1,f2,f3,f4,f5,f6,f12,f14,f20
func (a *EastmoneyTopListAdapter) fetchLimitUpPool(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	q := url.Values{}
	q.Set("pn", "1")
	q.Set("pz", "500")
	q.Set("po", "1")
	q.Set("np", "1")
	q.Set("fltt", "2")
	q.Set("invt", "2")
	q.Set("fid", "f3")
	q.Set("fs", "m:1+t:2,m:0+t:6+f:!2,m:0+t:13+f:!2,m:0+t:80+f:!2,m:1+t:23+f:!2")
	q.Set("fields", "f1,f2,f3,f4,f5,f6,f12,f14,f20,f23,f25")
	// Note: limit-up pool is a daily snapshot; we don't have a per-date filter.
	var resp sectorListResponse
	if err := a.client.GetJSON(ctx, a.client.BaseURL, "/api/qt/clist/get", q, &resp); err != nil {
		return nil, err
	}
	tradeDate := time.Now().UTC()
	items := make([]DataItem, 0, len(resp.Data.Diff))
	for _, row := range resp.Data.Diff {
		// f12: symbol, f14: name, f2: latest price, f3: change_pct
		sym, _ := row["f12"].(string)
		name, _ := row["f14"].(string)
		if sym == "" || name == "" {
			continue
		}
		if len(sym) == 6 {
			switch sym[0] {
			case '6', '9':
				sym += ".SH"
			default:
				sym += ".SZ"
			}
		}
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: tradeDate,
			Data: map[string]interface{}{
				"name":        name,
				"limit_price": toFloat64Safe(row["f2"]),
				"first_time":  tradeDate, // approximation: not exposed
				"last_time":   tradeDate,
				"limit_times": 1,
				"continuous":  1,
				"industry":    "",
			},
		})
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
func (a *EastmoneyTopListAdapter) HealthCheck(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("eastmoney-toplist adapter: nil client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("pn", "1")
	q.Set("pz", "1")
	q.Set("po", "1")
	q.Set("np", "1")
	q.Set("fltt", "2")
	q.Set("invt", "2")
	q.Set("fid", "f3")
	q.Set("fs", "m:1+t:2,m:0+t:6")
	q.Set("fields", "f1")
	var resp sectorListResponse
	if err := a.client.GetJSON(probeCtx, a.client.BaseURL, "/api/qt/clist/get", q, &resp); err != nil {
		return fmt.Errorf("%w: eastmoney-toplist: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *EastmoneyTopListAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 120,
		Burst:             10,
	}
}

// String renders a debug-friendly summary.
func (a *EastmoneyTopListAdapter) String() string {
	return "EastmoneyTopListAdapter{name=" + a.Name() + "}"
}

// ----------------------------------------------------------------------------
// shared helpers
// ----------------------------------------------------------------------------

// toFloat64Safe handles both float64 and json.Number (decoded without UseNumber).
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
		f, _ := strconv.ParseFloat(x, 64)
		return f
	default:
		return 0
	}
}

func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func parseEastmoneyDate(v interface{}) time.Time {
	switch x := v.(type) {
	case string:
		// Eastmoney typically returns dates as "2026-05-14" or "20260514".
		for _, layout := range []string{"2006-01-02", "20060102", "2006/01/02"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t
			}
		}
	case float64:
		// Some endpoints return YYYYMMDD as a number.
		s := strconv.FormatInt(int64(x), 10)
		if len(s) == 8 {
			if t, err := time.Parse("20060102", s); err == nil {
				return t
			}
		}
	}
	return time.Now().UTC()
}

// Ensure http package is imported even if io is only used in helpers.
var _ = io.Discard
var _ = http.MethodGet
