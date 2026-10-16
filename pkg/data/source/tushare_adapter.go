package source

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// TushareAdapter wraps the existing pkg/data.TushareClient so it satisfies
// the DataSourceAdapter interface. The wrapper preserves the legacy code
// path for daily OHLCV, fundamentals, and stock metadata; the new
// DataSourceAdapter surface is layered on top without modifying the
// underlying client.
//
// Backward compatibility:
//   - All existing callers of *data.TushareClient continue to work.
//   - Normalizers emit the same data shapes the legacy code produced.
//
// This is intentionally a thin adapter: no parallel Tushare SDK is
// introduced. Future PRs may consolidate the implementation, but for now
// "wrap, don't replace" keeps the diff small and the risk low.
type TushareAdapter struct {
	AdapterBase
	client *data.TushareClient
}

// NewTushareAdapter constructs a TushareAdapter. The client must already
// be configured (token, store, cache).
func NewTushareAdapter(client *data.TushareClient) *TushareAdapter {
	return &TushareAdapter{
		AdapterBase: NewAdapterBase("tushare", client != nil),
		client:      client,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *TushareAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *TushareAdapter) SupportedTypes() []string {
	return []string{
		DataTypeOHLCDaily,
		DataTypeFundamental,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *TushareAdapter) Schema(dataType string) (DataSchema, error) {
	switch dataType {
	case DataTypeOHLCDaily:
		return DataSchema{
			DataType: DataTypeOHLCDaily,
			Fields: []SchemaField{
				{Name: "open", Type: "float", Required: true, Unit: "yuan"},
				{Name: "high", Type: "float", Required: true, Unit: "yuan"},
				{Name: "low", Type: "float", Required: true, Unit: "yuan"},
				{Name: "close", Type: "float", Required: true, Unit: "yuan"},
				{Name: "volume", Type: "float", Required: true, Unit: "share"},
				{Name: "turnover", Type: "float", Required: false, Unit: "yuan"},
				{Name: "trade_days", Type: "int", Required: false},
			},
		}, nil
	case DataTypeFundamental:
		return DataSchema{
			DataType: DataTypeFundamental,
			Fields: []SchemaField{
				{Name: "pe", Type: "float", Required: false},
				{Name: "pb", Type: "float", Required: false},
				{Name: "ps", Type: "float", Required: false},
				{Name: "roe", Type: "float", Required: false},
				{Name: "roa", Type: "float", Required: false},
				{Name: "debt_to_equity", Type: "float", Required: false},
				{Name: "gross_margin", Type: "float", Required: false},
				{Name: "net_margin", Type: "float", Required: false},
				{Name: "revenue", Type: "float", Required: false, Unit: "yuan"},
				{Name: "net_profit", Type: "float", Required: false, Unit: "yuan"},
				{Name: "total_assets", Type: "float", Required: false, Unit: "yuan"},
				{Name: "total_liab", Type: "float", Required: false, Unit: "yuan"},
				// ann_date 是**披露日**（= 可见时间锚点）。D2 时代锚点只以保留
				// 记号存在（语义承载在规范层 market.Fundamental.AnnDate），wire
				// schema 里没有 ann_date，于是声明「悬空、不可实测」；D2-R1 起
				// 把它投影进 wire schema，锚点有了实测载体。非必填——上游缺披露
				// 日时留空（缺失≠0），不编一个日期顶上。
				{Name: "ann_date", Type: "date", Required: false},
			},
		}, nil
	default:
		return DataSchema{}, fmt.Errorf("%w: tushare does not serve %s", ErrUnsupported, dataType)
	}
}

// Fetch implements DataSourceAdapter.Fetch.
//
// For DataTypeOHLCDaily:
//   - Symbols scoped: fetches daily bars for the listed tickers.
//   - Time window: inclusive [StartDate, EndDate].
//
// For DataTypeFundamental:
//   - Fetches fundamentals for each symbol on StartDate (or the latest
//     available before EndDate if StartDate is zero).
func (a *TushareAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	switch req.DataType {
	case DataTypeOHLCDaily:
		return a.fetchDaily(ctx, req)
	case DataTypeFundamental:
		return a.fetchFundamentals(ctx, req)
	default:
		return nil, fmt.Errorf("%w: tushare: %s", ErrUnsupported, req.DataType)
	}
}

func (a *TushareAdapter) fetchDaily(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if a.client == nil {
		return nil, fmt.Errorf("tushare adapter: nil client")
	}
	items := make([]DataItem, 0)
	startStr := req.StartDate.Format("20060102")
	endStr := req.EndDate.Format("20060102")
	for _, sym := range req.Symbols {
		bars, err := a.client.FetchDailyOHLCV(ctx, sym, startStr, endStr)
		if err != nil {
			return nil, fmt.Errorf("tushare fetch %s: %w", sym, err)
		}
		for _, b := range bars {
			items = append(items, DataItem{
				Symbol:    b.Symbol,
				TradeTime: b.Date,
				Data: map[string]interface{}{
					"open":       b.Open,
					"high":       b.High,
					"low":        b.Low,
					"close":      b.Close,
					"volume":     b.Volume,
					"turnover":   b.Turnover,
					"trade_days": b.TradeDays,
				},
			})
		}
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (a *TushareAdapter) fetchFundamentals(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if a.client == nil {
		return nil, fmt.Errorf("tushare adapter: nil client")
	}
	items := make([]DataItem, 0)
	dateStr := req.StartDate.Format("20060102")
	if req.StartDate.IsZero() {
		dateStr = req.EndDate.Format("20060102")
	}
	for _, sym := range req.Symbols {
		fundamentals, err := a.client.FetchFundamentals(ctx, sym, dateStr)
		if err != nil {
			return nil, fmt.Errorf("tushare fundamentals %s: %w", sym, err)
		}
		for _, f := range fundamentals {
			items = append(items, DataItem{
				Symbol: f.Symbol,
				// TradeTime 承载**报告期截止日**（end_date），**不是**可见锚点。
				// 财报的可见时刻是披露日，承载在 Data["ann_date"]
				// （规范层 market.Fundamental.AnnDate；D2 契约保留记号
				// VisibleFieldAnnDate）。两者不可互替：拿 TradeTime 当可见点，
				// 三季报（9/30 截止、10/25 披露）在 9/30 就可交易——前视洞。
				TradeTime: f.Date,
				Data:      fundamentalToMap(f),
			})
		}
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// fundamentalToMap is a small helper that flattens a domain.Fundamental
// into the Data map shape used by storage.BulkInsert.
func fundamentalToMap(f domain.Fundamental) map[string]interface{} {
	return map[string]interface{}{
		"ts_code":        f.Symbol,
		"pe":             f.PE,
		"pb":             f.PB,
		"ps":             f.PS,
		"roe":            f.ROE,
		"roa":            f.ROA,
		"debt_to_equity": f.DebtToEquity,
		"gross_margin":   f.GrossMargin,
		"net_margin":     f.NetMargin,
		"revenue":        f.Revenue,
		"net_profit":     f.NetProfit,
		"total_assets":   f.TotalAssets,
		"total_liab":     f.TotalLiab,
		// ann_date：披露日（可见时间锚点）在 wire 上的投影（D2-R1）。
		// 直接透传 domain.Fundamental.AnnDate 的指针——同本 map 里 PE/PB 等
		// 可空字段的口径：nil = 上游未披露 → **留空为 nil**，绝不用零值时间
		// 或 0 顶上（缺失≠0）；锚点语义以规范层为准，本键是它的可选载体。
		"ann_date": f.AnnDate,
	}
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
//
// Tushare has no public health endpoint; we treat the cheap FetchStocks
// call as a probe. A failed probe returns ErrUpstreamUnavailable so the
// Registry can fall back.
func (a *TushareAdapter) HealthCheck(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("tushare adapter: nil client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// FetchStocks is the cheapest Tushare call; it returns ~5000 rows
	// for the SSE listing and completes well under 5 seconds.
	_, err := a.client.FetchStocks(probeCtx, "SSE", "L")
	if err != nil {
		return fmt.Errorf("%w: tushare: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *TushareAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 200, // free tier ceiling
		Burst:             10,
	}
}

// String renders a debug-friendly summary.
func (a *TushareAdapter) String() string {
	return "TushareAdapter{name=" + a.Name() + ", enabled=" + strconv.FormatBool(a.Enabled()) + "}"
}
