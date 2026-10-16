package source

// TushareDailyBasicAdapter serves the per-stock daily market activity
// layer (DataTypeDailyBasic, D5) from the Tushare daily_basic API:
// turnover_rate / turnover_rate_f / volume_ratio, persisted in
// market_activity (Migration 056).
//
// Why a separate adapter name ("tushare_daily_basic") instead of
// extending the market-data TushareAdapter's SupportedTypes:
//   - the door gets its own kill switch (DATA_DISABLE_TUSHARE_DAILY_BASIC)
//     — operators can stop activity imports without touching the OHLCV chain;
//   - Registry.Register replaces same-named adapters, so a distinct name
//     keeps both doors registered side by side.
//
// It reuses the same *data.TushareClient (and therefore the same
// TUSHARE_TOKEN — 0 new keys). A nil client (token not configured)
// registers the adapter as disabled, matching the degraded-mode posture
// of buildDataSourceRegistry.
//
// D5 contract note: this adapter is the blueprint §4.2 high-risk PIT
// scenario's first real exam. Turnover rate is computed from the FULL
// trading day — it does not exist before the close. The declaration
// (declarations.go) is therefore VisibleAfterClose with the trade_date
// anchor; the D5 PIT sabotage test pins exactly that (widening it to
// intraday-visible must turn the test red).

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
)

// dailyBasicMaxSpanDays 是单次 Fetch 允许的日历日跨度上限。
//
// 理由（我们的配额保护，不是上游文档化限额）：全市场单日约 5.5–5.6 千行，
// 一次请求一日；跨度越大请求数越多（365 天 = 365 次）。样板场景（同步/
// 回补一个窗口）400 天足够；更长的历史由调用方分片多次 Fetch。与 D1
// Router 的跨度上限（REV-01，366 天）同型——无界大范围请求会把一次
// ETL 拖成小时级配额消耗，且中途失败无法续传。
const dailyBasicMaxSpanDays = 400

// TushareDailyBasicAdapter wraps the TushareClient's daily_basic import
// face (pkg/data/tushare_daily_basic.go) as a DataSourceAdapter.
type TushareDailyBasicAdapter struct {
	AdapterBase
	client *data.TushareClient
}

// NewTushareDailyBasicAdapter constructs a TushareDailyBasicAdapter. A nil
// client (TUSHARE_TOKEN not configured) yields a disabled adapter: the
// Registry skips it at Fetch time instead of failing at startup.
func NewTushareDailyBasicAdapter(client *data.TushareClient) *TushareDailyBasicAdapter {
	return &TushareDailyBasicAdapter{
		AdapterBase: NewAdapterBase("tushare_daily_basic", client != nil),
		client:      client,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *TushareDailyBasicAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *TushareDailyBasicAdapter) SupportedTypes() []string {
	return []string{DataTypeDailyBasic}
}

// Schema implements DataSourceAdapter.Schema.
//
// trade_date 是可见时间锚点（after_close 声明的载体，类型 date）；
// ts_code 是身份列（同时承载在 DataItem.Symbol）；三个数值列可空
// （NULL = 上游未发布，缺失≠0）。
func (a *TushareDailyBasicAdapter) Schema(dataType string) (DataSchema, error) {
	if dataType != DataTypeDailyBasic {
		return DataSchema{}, fmt.Errorf("%w: tushare_daily_basic does not serve %s", ErrUnsupported, dataType)
	}
	return DataSchema{
		DataType: DataTypeDailyBasic,
		Fields: []SchemaField{
			{Name: "ts_code", Type: "string", Required: true},
			{Name: "trade_date", Type: "date", Required: true},
			{Name: "turnover_rate", Type: "float", Required: false, Unit: "percent"},
			{Name: "turnover_rate_f", Type: "float", Required: false, Unit: "percent"},
			{Name: "volume_ratio", Type: "float", Required: false},
		},
	}, nil
}

// Fetch implements DataSourceAdapter.Fetch.
//
// Scope is a calendar date range [StartDate, EndDate] (inclusive); each
// date is one upstream request (whole-market, ~5.5k rows — official
// per-call ceiling is 6000). Symbols must be empty: the D5 sample is the
// market-wide activity layer; per-symbol history is explicitly out of
// scope (fail-loud rather than silently ignoring a scope the caller
// asked for). Zero range → error (no implicit "latest" — that would be
// a hidden default).
//
// The response items mirror the persisted rows (same contract as the
// macro adapter): missing values are visible as nil in Data, never 0.
//
// ⚠️ 多日逐日落库的语义（如实声明，别当原子性）：范围内的**每一天**独立
// 走「取数 → normalize → 落库」，落库以日为事务边界。因此中途某天失败时
// 报错返回，但**当天之前已成功的天已经入库**（不是全有或全无）。这是有意
// 的选择：① 逐日 upsert 幂等（同键重导刷新），失败后重跑整个范围安全；
// ② 把 400 天读成一个大事务会让失败重来的代价从「补缺口」变成「全量重导」。
// 调用方应把「返回 error」理解为「该范围未完整导入」而非「什么都没写」。
func (a *TushareDailyBasicAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if req.DataType != DataTypeDailyBasic {
		return nil, fmt.Errorf("%w: tushare_daily_basic: %s", ErrUnsupported, req.DataType)
	}
	if a.client == nil {
		return nil, fmt.Errorf("tushare_daily_basic adapter: nil client")
	}
	if len(req.Symbols) > 0 {
		return nil, fmt.Errorf(
			"tushare_daily_basic: Symbols must be empty (market-wide activity layer; per-symbol history is out of scope for the D5 sample)")
	}

	start, end, err := dailyBasicDateRange(req)
	if err != nil {
		return nil, err
	}

	items := make([]DataItem, 0, 1024)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		rows, err := a.client.FetchDailyBasic(ctx, d.Format("20060102"))
		if err != nil {
			return nil, fmt.Errorf("tushare_daily_basic fetch %s: %w", d.Format("20060102"), err)
		}
		for _, r := range rows {
			items = append(items, DataItem{
				Symbol:    r.Symbol,
				TradeTime: r.TradeDate,
				Data: map[string]interface{}{
					"ts_code":         r.Symbol,
					"trade_date":      r.TradeDate,
					"turnover_rate":   derefOrNil(r.TurnoverRate),
					"turnover_rate_f": derefOrNil(r.TurnoverRateF),
					"volume_ratio":    derefOrNil(r.VolumeRatio),
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

// dailyBasicDateRange 解析并校验 Fetch 的日期范围：起止必填（零值即缺省
// 不行——隐式「最新一日」是隐藏默认值），跨度 ≤ dailyBasicMaxSpanDays。
func dailyBasicDateRange(req FetchRequest) (time.Time, time.Time, error) {
	start, end := req.StartDate, req.EndDate
	if start.IsZero() && end.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"tushare_daily_basic: StartDate/EndDate required (date range scope; no implicit latest)")
	}
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"tushare_daily_basic: EndDate %s before StartDate %s",
			end.Format("2006-01-02"), start.Format("2006-01-02"))
	}
	if int(end.Sub(start).Hours()/24)+1 > dailyBasicMaxSpanDays {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"tushare_daily_basic: range %s..%s exceeds %d calendar days (shard the request)",
			start.Format("2006-01-02"), end.Format("2006-01-02"), dailyBasicMaxSpanDays)
	}
	// 归一化到自然日零点：按日迭代不依赖调用方传入的时分秒。
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC),
		time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC), nil
}

// derefOrNil 把可空数值列转成 wire 值：nil → nil（缺失可见），非 nil →
// float64。缺失≠0 在 adapter 出口也要可见——不拿 0 顶包。
func derefOrNil(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
//
// Probes one calendar day (today, UTC) with a fetch-only call: the
// upstream answers with either rows or an empty item set (holiday) —
// both prove connectivity. A failed probe returns
// ErrUpstreamUnavailable so the Registry can fall back.
func (a *TushareDailyBasicAdapter) HealthCheck(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("tushare_daily_basic adapter: nil client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.client.ProbeDailyBasic(probeCtx, time.Now().UTC().Format("20060102")); err != nil {
		return fmt.Errorf("%w: tushare_daily_basic: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *TushareDailyBasicAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 200, // same ceiling as the tushare market adapter
		Burst:             10,
	}
}

// String renders a debug-friendly summary.
func (a *TushareDailyBasicAdapter) String() string {
	return "TushareDailyBasicAdapter{name=" + a.Name() + ", enabled=" + strconv.FormatBool(a.Enabled()) + "}"
}
