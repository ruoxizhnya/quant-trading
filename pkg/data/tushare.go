package data

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/httpclient"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

const (
	tushareRateLimit    = 200 // requests per minute on free tier
	tushareRateLimitDur = time.Minute
)

// TushareStore defines the interface for tushare data operations.
type TushareStore interface {
	SaveStockBatch(ctx context.Context, stocks []domain.Stock) error
	SaveOHLCVBatch(ctx context.Context, records []*domain.OHLCV) error
	SaveFundamentalBatch(ctx context.Context, records []*domain.Fundamental) error
	SaveFundamentalDataBatch(ctx context.Context, records []*domain.FundamentalData) error
	SaveIndexConstituentBatch(ctx context.Context, records []*domain.IndexConstituent) error
	GetIndexConstituents(ctx context.Context, indexCode string) ([]domain.IndexConstituent, error)
	SaveDividendBatch(ctx context.Context, records []*domain.Dividend) error
	SaveSplitBatch(ctx context.Context, records []*domain.Split) error
	// SaveRawIngest archives a class-A raw source response (ADR-022 §2,
	// ingest.raw). Declared here so the archive contract is enforced at
	// compile time rather than discovered at runtime.
	SaveRawIngest(ctx context.Context, record *storage.RawIngest) error
}

// TushareClient wraps the tushare.pro HTTP API.
type TushareClient struct {
	httpClient *httpclient.Client
	token      string
	logger     zerolog.Logger
	store      TushareStore
	cache      storage.Cache

	mu           sync.Mutex
	lastRequest  time.Time
	requestCount int
}

// NewTushareClient creates a new Tushare API client.
func NewTushareClient(token, baseURL string, maxRetries int, store TushareStore, cache storage.Cache) *TushareClient {
	logger := logging.WithContext(map[string]any{"component": "tushare_client"})
	// Ensure logger is valid (fallback to Nop if global logger not initialized)
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &TushareClient{
		httpClient: httpclient.New(baseURL, 30*time.Second, maxRetries),
		token:      token,
		logger:     logger,
		store:      store,
		cache:      cache,
	}
}

// TushareRequest represents a tushare API request payload.
type TushareRequest struct {
	APIName string                 `json:"api_name"`
	Token   string                 `json:"token"`
	Params  map[string]interface{} `json:"params,omitempty"`
	Fields  string                 `json:"fields,omitempty"`
}

// TushareResponse represents a tushare API response.
type TushareResponse struct {
	Code    int                `json:"code"`
	Msg     string             `json:"msg"`
	Request TushareRequestMeta `json:"request"`
	Data    TushareData        `json:"data"`
}

// TushareRequestMeta contains metadata about the request.
type TushareRequestMeta struct {
	API    string `json:"api"`
	Token  string `json:"token"`
	Params any    `json:"params"`
	Fields string `json:"fields"`
	TS     int64  `json:"ts"`
}

// TushareData contains the response data.
type TushareData struct {
	Fields []string `json:"fields"`
	Items  [][]any  `json:"items"`
}

// fieldToFloat safely converts an interface{} to float64.
func fieldToFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

// fieldToStr safely converts an interface{} to string.
func fieldToStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// waitForRateLimit ensures we don't exceed 200 req/min.
func (c *TushareClient) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.requestCount >= tushareRateLimit {
		elapsed := now.Sub(c.lastRequest)
		if elapsed < tushareRateLimitDur {
			sleepDur := tushareRateLimitDur - elapsed
			c.logger.Info().Dur("sleep", sleepDur).Msg("Rate limit reached, waiting")
			time.Sleep(sleepDur)
		}
		c.requestCount = 0
	}

	if c.requestCount == 0 {
		c.lastRequest = time.Now()
	}
	c.requestCount++
}

// call invokes the tushare API with rate limiting and retry.
func (c *TushareClient) call(ctx context.Context, apiName string, params map[string]interface{}, fields string) (*TushareResponse, error) {
	c.waitForRateLimit()

	req := TushareRequest{
		APIName: apiName,
		Token:   c.token,
		Params:  params,
		Fields:  fields,
	}

	resp, err := c.httpClient.Post(ctx, "", req)
	if err != nil {
		return nil, fmt.Errorf("tushare API call failed: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tushare API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var tushareResp TushareResponse
	if err := httpclient.DecodeJSON(resp.Body, &tushareResp); err != nil {
		return nil, fmt.Errorf("failed to decode tushare response: %w, body: %s", err, string(resp.Body))
	}

	if tushareResp.Code != 0 {
		return nil, fmt.Errorf("tushare API error %d: %s", tushareResp.Code, tushareResp.Msg)
	}

	// Archive the raw source response before any caller normalises it, so
	// ingest.raw stays the single evidence coordinate for this request
	// (ADR-022 §2 class A). Only successful responses are archived — an error
	// envelope is not data anyone can cite.
	c.archiveRaw(ctx, apiName, params, resp.Body)

	c.logger.Debug().Interface("data_fields", tushareResp.Data.Fields).Int("items_count", len(tushareResp.Data.Items)).Msg("tushare response received")

	return &tushareResp, nil
}

// FetchStocks retrieves stock list from tushare and saves to database.
func (c *TushareClient) FetchStocks(ctx context.Context, exchange string, listStatus string) ([]domain.Stock, error) {
	params := map[string]interface{}{
		"exchange":    exchange,
		"list_status": listStatus,
	}

	resp, err := c.call(ctx, "stock_basic", params, "ts_code,symbol,name,area,industry,market,list_date,delist_date,is_hs")
	if err != nil {
		return nil, err
	}

	stocks := c.normalizeStocks(resp)
	if len(stocks) == 0 {
		return nil, nil
	}

	if err := c.store.SaveStockBatch(ctx, stocks); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save stocks")
	}

	// Invalidate cache
	if exchange != "" {
		c.cache.InvalidateStocks(ctx, exchange)
	} else {
		c.cache.InvalidateStocks(ctx, "all")
	}

	c.logger.Info().Int("count", len(stocks)).Msg("Stocks fetched and saved")
	return stocks, nil
}

// normalizeStocks converts tushare stock_basic response to domain.Stock.
func (c *TushareClient) normalizeStocks(resp *TushareResponse) []domain.Stock {
	var stocks []domain.Stock
	for _, item := range resp.Data.Items {
		if len(item) < 9 {
			continue
		}

		tsCode := c.fieldStr(item, 0)
		symbol := c.fieldStr(item, 1)
		if tsCode == "" && symbol == "" {
			continue
		}

		// Use full ts_code (e.g. 000001.SZ) as Symbol for API compatibility
		useSymbol := tsCode
		if useSymbol == "" {
			useSymbol = symbol
		}

		stock := domain.Stock{
			Symbol:   useSymbol,
			Name:     c.fieldStr(item, 2),
			Exchange: c.extractExchange(useSymbol),
			Industry: c.fieldStr(item, 4),
			Status:   "active",
		}

		if listDate := c.fieldStr(item, 6); listDate != "" {
			if t, err := time.Parse("20060102", listDate); err == nil {
				stock.ListDate = t
			}
		}

		// delist_date（P2-4）：摘牌日。**此前这一列被整个丢弃**，退市票
		// 同步进来也被标成 active —— 于是「某日仍在市的池子」根本构造
		// 不出来，幸存者偏差只能诊断、治不了。
		//
		// Status 由摘牌日推导，不再无条件写 "active"：摘牌日已过就是
		// delisted。写死 active 会让退市票在下游被当成正常可交易标的。
		if delistDate := c.fieldStr(item, 7); delistDate != "" {
			if t, err := time.Parse("20060102", delistDate); err == nil {
				d := t
				stock.DelistDate = &d
				if !d.After(time.Now()) {
					stock.Status = "delisted"
				}
			}
		}

		stocks = append(stocks, stock)
	}
	return stocks
}

// formatDate converts YYYY-MM-DD to YYYYMMDD for tushare API.
func formatDate(s string) string {
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		return s[:4] + s[5:7] + s[8:10]
	}
	return s
}

// FetchDailyOHLCV retrieves daily OHLCV data, 前复权 (qfq) adjusted.
//
// 复权怎么来的：**不再用 stk_factor_pro**，改为 `daily`（不复权行情）+ `adj_factor`
// （复权因子）自己算。原因：stk_factor_pro 是专业版接口，权限不足时返回 40203
// 「您没有接口(stk_factor_pro)访问权限」—— 那不是 token 无效（40101 才是），而是
// 该接口没开通；本仓默认走的那条路因此从一开始就拿不到行情（同步 job 会 100% 失败，
// 5568 只股票一只都进不来）。而 `daily` 与 `adj_factor` 都在同一个 token 的权限内。
//
// 换算口径取自 Tushare 官方 `adj_factor` 接口文档的示例 4（**别凭记忆改**）：
//
//	前复权价 = 当日价格 × 当日复权因子 / 最新复权因子
//	后复权价 = 当日价格 × 当日复权因子          （基准 = 上市日，基准因子为 1）
//
// 官方文档同时点明两件事，实现里都照做了：
//  1. adj_factor 是「**累计后复权因子**」，每日 8:30-9:30 更新；
//  2. **停牌日 adj_factor 会补齐，而 daily 没有行情** —— 两个序列按日期并不对齐，
//     所以以 daily 为准逐日取因子，取不到的那天**跳过**而不是补 1。
func (c *TushareClient) FetchDailyOHLCV(ctx context.Context, symbol string, startDate, endDate string) ([]domain.OHLCV, error) {
	params := map[string]interface{}{
		"ts_code":    symbol,
		"start_date": formatDate(startDate),
		"end_date":   formatDate(endDate),
	}

	resp, err := c.call(ctx, "daily", params, "ts_code,trade_date,open,high,low,close,vol,amount")
	if err != nil {
		return nil, err
	}

	factors, err := c.fetchAdjFactors(ctx, symbol, startDate, endDate)
	if err != nil {
		return nil, err
	}

	records := c.normalizeDailyOHLCV(resp, symbol, factors)
	if len(records) == 0 {
		return nil, nil
	}

	// Save to database
	ptrRecords := make([]*domain.OHLCV, len(records))
	for i := range records {
		ptrRecords[i] = &records[i]
	}
	if err := c.store.SaveOHLCVBatch(ctx, ptrRecords); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save OHLCV")
	}

	c.logger.Info().Str("symbol", symbol).Int("count", len(records)).Msg("OHLCV (qfq) fetched and saved")
	return records, nil
}

// fetchAdjFactors returns a trade_date (YYYYMMDD) → adj_factor map for the range.
//
// adj_factor 是「累计后复权因子」，配合不复权行情算出前/后复权价（口径见
// FetchDailyOHLCV）。因子 <= 0 视为脏数据、不进 map —— 它既不是「没除权」
// （那在 1.0 附近），也不是「没有数据」（那是 key 不存在），混进来会污染基准。
func (c *TushareClient) fetchAdjFactors(ctx context.Context, symbol string, startDate, endDate string) (map[string]float64, error) {
	params := map[string]interface{}{
		"ts_code":    symbol,
		"start_date": formatDate(startDate),
		"end_date":   formatDate(endDate),
	}

	resp, err := c.call(ctx, "adj_factor", params, "ts_code,trade_date,adj_factor")
	if err != nil {
		return nil, err
	}

	out := make(map[string]float64, len(resp.Data.Items))
	for _, item := range resp.Data.Items {
		if len(item) < 3 {
			continue
		}
		d := c.fieldStr(item, 1)
		if d == "" {
			continue
		}
		f := c.fieldFloat(item, 2)
		if f <= 0 {
			continue
		}
		out[d] = f
	}
	return out, nil
}

// normalizeDailyOHLCV converts a `daily` response into 前复权 (qfq) OHLCV rows.
//
// daily fields: ts_code, trade_date, open, high, low, close, vol, amount
// 四个价格按 qfq 口径换算：raw × 当日因子 / 最新因子。成交量**不**调整 ——
// tushare 的 vol/amount 本来就是原始成交量，前复权不改它（stk_factor_pro 亦同）。
//
// 刻意严格的两处（同 P2-10 的教训：缺失值折成某个默认值 = 编数据）：
//   - 某天取不到因子 → **跳过这一行**，不补 1.0。补 1.0 等于宣称「这天没除权」，
//     在除权日附近会造出假跳空，而回测会把它当成真信号；
//   - 因子 <= 0 → 同样跳过（它不可能是合法的复权因子）。
func (c *TushareClient) normalizeDailyOHLCV(resp *TushareResponse, symbol string, factors map[string]float64) []domain.OHLCV {
	if len(factors) == 0 {
		c.logger.Warn().Str("symbol", symbol).
			Msg("no adj_factor for the range — refusing to store unadjusted prices as qfq")
		return nil
	}

	// 前复权基准 = **区间内**最新交易日的因子（不是「今天」，也不是首日）。
	// 由此带来一个必须知道的性质：qfq 是相对基准的，将来同步到更晚的日期时，
	// 历史数据的 qfq 会整体变动 —— 这是复权口径的固有性质（Tushare 官方对
	// stk_factor 也明确写了「前复权是历史快照、数据不更新」），不是本实现引入的。
	// YYYYMMDD 的字典序 == 时间序，可以直接比字符串。
	latest := ""
	for d := range factors {
		if d > latest {
			latest = d
		}
	}
	base := factors[latest]
	if base <= 0 {
		c.logger.Warn().Str("symbol", symbol).Str("latest_date", latest).
			Msg("latest adj_factor is not positive — skipping")
		return nil
	}

	var records []domain.OHLCV
	skipped := 0
	c.logger.Debug().Int("items_count", len(resp.Data.Items)).Msg("normalizeDailyOHLCV start")
	for _, item := range resp.Data.Items {
		if len(item) < 8 {
			c.logger.Debug().Int("item_len", len(item)).Msg("item skipped: too short")
			continue
		}

		tradeDate := c.fieldStr(item, 1)
		if tradeDate == "" {
			continue
		}

		t, err := time.Parse("20060102", tradeDate)
		if err != nil {
			continue
		}

		f, ok := factors[tradeDate]
		if !ok || f <= 0 {
			skipped++
			continue
		}

		scale := f / base
		ohlcv := domain.OHLCV{
			Symbol:    symbol,
			Date:      t,
			Open:      c.fieldFloat(item, 2) * scale, // open
			High:      c.fieldFloat(item, 3) * scale, // high
			Low:       c.fieldFloat(item, 4) * scale, // low
			Close:     c.fieldFloat(item, 5) * scale, // close
			Volume:    c.fieldFloat(item, 6),         // vol（不复权）
			Turnover:  c.fieldFloat(item, 7),         // amount（不复权）
			TradeDays: 0,                             // not available from daily
		}
		records = append(records, ohlcv)
	}
	if skipped > 0 {
		c.logger.Warn().Str("symbol", symbol).Int("skipped", skipped).
			Msg("rows skipped: no usable adj_factor on that trade_date " +
				"(storing them unadjusted would fake a gap)")
	}
	return records
}

// FetchFundamentals retrieves financial data from tushare.
func (c *TushareClient) FetchFundamentals(ctx context.Context, symbol string, date string) ([]domain.Fundamental, error) {
	params := map[string]interface{}{
		"ts_code":  symbol,
		"ann_date": date,
	}

	resp, err := c.call(ctx, "fina_indicator", params, "ts_code,ann_date,end_date,pe,pb,ps,roe,roa,debt_to_equity,gross_margin,net_margin,revenue,net_profit,total_assets,total_liab")
	if err != nil {
		return nil, err
	}

	records := c.normalizeFundamentals(resp)
	if len(records) == 0 {
		return nil, nil
	}

	ptrRecords := make([]*domain.Fundamental, len(records))
	for i := range records {
		ptrRecords[i] = &records[i]
	}
	if err := c.store.SaveFundamentalBatch(ctx, ptrRecords); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save fundamentals")
	}

	c.logger.Info().Str("symbol", symbol).Int("count", len(records)).Msg("Fundamentals fetched and saved")
	return records, nil
}

// normalizeFundamentals converts tushare financial_data response to domain.Fundamental.
func (c *TushareClient) normalizeFundamentals(resp *TushareResponse) []domain.Fundamental {
	var records []domain.Fundamental
	for _, item := range resp.Data.Items {
		if len(item) < 3 {
			continue
		}

		symbol := c.fieldStr(item, 0)
		if symbol == "" {
			continue
		}

		endDateStr := c.fieldStr(item, 2)
		t, _ := time.Parse("20060102", endDateStr)

		// ann_date 在响应第 2 列，**此前被整个丢弃**（P1-4）。
		//
		// 丢掉它的后果不是"少存一个字段"这么轻：只存 end_date 的话，读取侧
		// COALESCE(ann_date, trade_date) 会退化成 end_date，三季报（9/30
		// 截止、10/25 披露）在 9/30 就能被回测看见。P0-1 的 COALESCE 只是
		// 把这个洞盖住了，没补上 —— 补在这里。
		var annDate *time.Time
		if annDateStr := c.fieldStr(item, 1); annDateStr != "" {
			if a, err := time.Parse("20060102", annDateStr); err == nil {
				annDate = &a
			}
		}

		// 用 fieldFloatPtr 而不是 fieldFloat：源端缺字段时留下 nil（未知），
		// 而不是 0（会被下游读成"PE = 0，白送的股票"）。见 TASKS P2-10。
		fund := domain.Fundamental{
			Symbol:       symbol,
			Date:         t,
			AnnDate:      annDate,
			PE:           c.fieldFloatPtr(item, 3),
			PB:           c.fieldFloatPtr(item, 4),
			PS:           c.fieldFloatPtr(item, 5),
			ROE:          c.fieldFloatPtr(item, 6),
			ROA:          c.fieldFloatPtr(item, 7),
			DebtToEquity: c.fieldFloatPtr(item, 8),
			GrossMargin:  c.fieldFloatPtr(item, 9),
			NetMargin:    c.fieldFloatPtr(item, 10),
			Revenue:      c.fieldFloatPtr(item, 11),
			NetProfit:    c.fieldFloatPtr(item, 12),
			TotalAssets:  c.fieldFloatPtr(item, 13),
			TotalLiab:    c.fieldFloatPtr(item, 14),
		}
		records = append(records, fund)
	}
	return records
}

// FetchFundamentalsData retrieves financial data from tushare financial_data API
// and stores it in the stock_fundamentals table.
func (c *TushareClient) FetchFundamentalsData(ctx context.Context, symbol, startDate, endDate string) ([]domain.FundamentalData, error) {
	params := map[string]interface{}{
		"ts_code": symbol,
	}
	if startDate != "" {
		params["start_date"] = formatDate(startDate)
	}
	if endDate != "" {
		params["end_date"] = formatDate(endDate)
	}

	resp, err := c.call(ctx, "fina_indicator", params, "ts_code,ann_date,end_date,pe,pb,ps,roe,roa,debt_to_equity,gross_margin,net_margin,revenue,net_profit,total_assets,total_liab")
	if err != nil {
		return nil, err
	}

	records := c.normalizeFundamentalsData(resp)
	if len(records) == 0 {
		return nil, nil
	}

	// Save to stock_fundamentals table via store
	if c.store != nil {
		ptrs := make([]*domain.FundamentalData, len(records))
		for i := range records {
			ptrs[i] = &records[i]
		}
		if err := c.store.SaveFundamentalDataBatch(ctx, ptrs); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to batch save fundamentals data")
		}
	}

	c.logger.Info().Str("symbol", symbol).Int("count", len(records)).Msg("FundamentalsData fetched and saved")
	return records, nil
}

// normalizeFundamentalsData converts tushare financial_data response to domain.FundamentalData.
// financial_data fields: ts_code,ann_date,end_date,pe,pb,ps,roe,roa,debt_to_equity,gross_margin,net_margin,revenue,net_profit,total_assets,total_liab
func (c *TushareClient) normalizeFundamentalsData(resp *TushareResponse) []domain.FundamentalData {
	var records []domain.FundamentalData
	for _, item := range resp.Data.Items {
		if len(item) < 3 {
			continue
		}

		tsCode := c.fieldStr(item, 0)
		if tsCode == "" {
			continue
		}

		annDateStr := c.fieldStr(item, 1)
		endDateStr := c.fieldStr(item, 2)

		annDate, _ := time.Parse("20060102", annDateStr)
		endDate, _ := time.Parse("20060102", endDateStr)

		// Use end_date as trade_date for factor analysis
		fund := domain.FundamentalData{
			TsCode:       tsCode,
			TradeDate:    endDate,
			AnnDate:      annDate,
			EndDate:      endDate,
			PE:           c.fieldFloatPtr(item, 3),
			PB:           c.fieldFloatPtr(item, 4),
			PS:           c.fieldFloatPtr(item, 5),
			ROE:          c.fieldFloatPtr(item, 6),
			ROA:          c.fieldFloatPtr(item, 7),
			DebtToEquity: c.fieldFloatPtr(item, 8),
			GrossMargin:  c.fieldFloatPtr(item, 9),
			NetMargin:    c.fieldFloatPtr(item, 10),
			Revenue:      c.fieldFloatPtr(item, 11),
			NetProfit:    c.fieldFloatPtr(item, 12),
			TotalAssets:  c.fieldFloatPtr(item, 13),
			TotalLiab:    c.fieldFloatPtr(item, 14),
		}
		records = append(records, fund)
	}
	return records
}

// fieldFloatPtr returns a pointer to float64, handling nil values.
func (c *TushareClient) fieldFloatPtr(item []any, idx int) *float64 {
	if idx >= len(item) || item[idx] == nil {
		return nil
	}
	v := c.fieldFloat(item, idx)
	return &v
}

// FetchIndexConstituents retrieves index constituents from tushare and saves them to DB.
func (c *TushareClient) FetchIndexConstituents(ctx context.Context, indexCode string, date string) ([]domain.IndexConstituent, error) {
	params := map[string]interface{}{
		"index_code": indexCode,
	}
	if date != "" {
		params["trade_date"] = date
	}

	resp, err := c.call(ctx, "index_weight", params, "index_code,con_code,in_date,out_date")
	if err != nil {
		return nil, err
	}

	constituents := c.normalizeIndexConstituents(resp, indexCode)
	if len(constituents) == 0 {
		return nil, nil
	}

	// Save to database
	ptrs := make([]*domain.IndexConstituent, len(constituents))
	for i := range constituents {
		ptrs[i] = &constituents[i]
	}
	if err := c.store.SaveIndexConstituentBatch(ctx, ptrs); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save index constituents")
	}

	c.logger.Info().Str("index", indexCode).Int("count", len(constituents)).Msg("Index constituents fetched and saved")
	return constituents, nil
}

// normalizeIndexConstituents converts tushare index_weight response to domain.IndexConstituent.
// index_weight fields: index_code, con_code, in_date, out_date
func (c *TushareClient) normalizeIndexConstituents(resp *TushareResponse, indexCode string) []domain.IndexConstituent {
	var constituents []domain.IndexConstituent
	for _, item := range resp.Data.Items {
		if len(item) < 4 {
			continue
		}

		conCode := c.fieldStr(item, 1)
		if conCode == "" {
			continue
		}

		inDateStr := c.fieldStr(item, 2)
		outDateStr := c.fieldStr(item, 3)

		var inDate, outDate time.Time
		if inDateStr != "" {
			if t, err := time.Parse("20060102", inDateStr); err == nil {
				inDate = t
			}
		}
		if outDateStr != "" {
			if t, err := time.Parse("20060102", outDateStr); err == nil {
				outDate = t
			}
		}

		constituents = append(constituents, domain.IndexConstituent{
			IndexCode: indexCode,
			Symbol:    conCode,
			InDate:    inDate,
			OutDate:   outDate,
			Weight:    0, // index_weight API does not return weight field
		})
	}
	return constituents
}

// GetIndexConstituents returns the current constituents of an index from DB.
// If not found in DB, fetches from Tushare and saves.
func (c *TushareClient) GetIndexConstituents(ctx context.Context, indexCode string, date string) ([]domain.IndexConstituent, error) {
	// Try DB first
	constituents, err := c.store.GetIndexConstituents(ctx, indexCode)
	if err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get index constituents from DB, falling back to Tushare")
	}
	if len(constituents) > 0 {
		return constituents, nil
	}

	// Fetch from Tushare
	return c.FetchIndexConstituents(ctx, indexCode, date)
}

// Helper methods

func (c *TushareClient) fieldStr(item []any, idx int) string {
	if idx >= len(item) || item[idx] == nil {
		return ""
	}
	switch v := item[idx].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (c *TushareClient) fieldFloat(item []any, idx int) float64 {
	if idx >= len(item) || item[idx] == nil {
		return 0
	}
	switch v := item[idx].(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func (c *TushareClient) extractExchange(tsCode string) string {
	if len(tsCode) >= 4 {
		suffix := tsCode[len(tsCode)-3:]
		switch suffix {
		case ".SH":
			return "SSE"
		case ".SZ":
			return "SZSE"
		}
	}
	return strings.Split(tsCode, ".")[0]
}

// FetchTradingCalendar retrieves the trading calendar from Tushare trade_cal API.
// exchange: "SSE" (Shanghai Stock Exchange) or "SZSE" (Shenzhen Stock Exchange)
// startDate, endDate: format "YYYY-MM-DD"
func (c *TushareClient) FetchTradingCalendar(ctx context.Context, exchange, startDate, endDate string) ([]storage.TradingCalendarEntry, error) {
	params := map[string]interface{}{
		"exchange":   exchange,
		"start_date": formatDate(startDate),
		"end_date":   formatDate(endDate),
	}

	resp, err := c.call(ctx, "trade_cal", params, "exchange,cal_date,is_open")
	if err != nil {
		return nil, err
	}

	return c.normalizeTradingCalendar(resp, exchange)
}

// normalizeTradingCalendar converts tushare trade_cal response to storage.TradingCalendarEntry.
// trade_cal fields: exchange, cal_date, is_open
// is_open: 1 = trading day, 0 = holiday
func (c *TushareClient) normalizeTradingCalendar(resp *TushareResponse, exchange string) ([]storage.TradingCalendarEntry, error) {
	var entries []storage.TradingCalendarEntry
	for _, item := range resp.Data.Items {
		if len(item) < 3 {
			continue
		}

		calDate := c.fieldStr(item, 1)
		if calDate == "" {
			continue
		}

		// Parse date: YYYYMMDD
		t, err := time.Parse("20060102", calDate)
		if err != nil {
			c.logger.Warn().Str("cal_date", calDate).Msg("failed to parse calendar date")
			continue
		}

		// is_open: "1" = trading day, "0" = holiday/closed
		isOpenStr := c.fieldStr(item, 2)
		isTradingDay := isOpenStr == "1"

		entries = append(entries, storage.TradingCalendarEntry{
			TradeDate:    t,
			Exchange:     exchange,
			IsTradingDay: isTradingDay,
		})
	}

	c.logger.Info().Int("count", len(entries)).Str("exchange", exchange).Msg("Trading calendar normalized")
	return entries, nil
}

// FetchDividends retrieves dividend data from tushare dividend API.
// startDate, endDate: format "YYYYMMDD" (optional — pass "" to fetch all available).
func (c *TushareClient) FetchDividends(ctx context.Context, symbol string, startDate, endDate string) ([]domain.Dividend, error) {
	params := map[string]interface{}{
		"ts_code": symbol,
	}
	if startDate != "" {
		params["ann_date"] = formatDate(startDate)
	}
	if endDate != "" {
		params["end_date"] = formatDate(endDate)
	}

	resp, err := c.call(ctx, "dividend", params, "ts_code,ann_date,rec_date,pay_date,div_amnt,stk_div,stk_ratio,cash_ratio")
	if err != nil {
		return nil, err
	}

	records := c.normalizeDividends(resp)
	if len(records) == 0 {
		return nil, nil
	}

	// Save to database
	ptrs := make([]*domain.Dividend, len(records))
	for i := range records {
		ptrs[i] = &records[i]
	}
	if err := c.store.SaveDividendBatch(ctx, ptrs); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save dividends")
	}

	c.logger.Info().Str("symbol", symbol).Int("count", len(records)).Msg("Dividends fetched and saved")
	return records, nil
}

// normalizeDividends converts tushare dividend API response to domain.Dividend.
// dividend API fields: ts_code, ann_date, rec_date, pay_date, div_amnt, stk_div, stk_ratio, cash_ratio
func (c *TushareClient) normalizeDividends(resp *TushareResponse) []domain.Dividend {
	var records []domain.Dividend
	for _, item := range resp.Data.Items {
		if len(item) < 8 {
			continue
		}

		tsCode := c.fieldStr(item, 0)
		if tsCode == "" {
			continue
		}

		annDateStr := c.fieldStr(item, 1)
		recDateStr := c.fieldStr(item, 2)
		payDateStr := c.fieldStr(item, 3)

		annDate, _ := time.Parse("20060102", annDateStr)
		recDate, _ := time.Parse("20060102", recDateStr)
		payDate, _ := time.Parse("20060102", payDateStr)

		record := domain.Dividend{
			Symbol:    tsCode,
			AnnDate:   annDate,
			RecDate:   recDate,
			PayDate:   payDate,
			DivAmt:    c.fieldFloat(item, 4), // div_amnt — cash dividend per share
			StkDiv:    c.fieldFloat(item, 5), // stk_div — stock dividend per share
			StkRatio:  c.fieldFloat(item, 6), // stk_ratio — stock split ratio
			CashRatio: c.fieldFloat(item, 7), // cash_ratio — cash dividend ratio
		}
		records = append(records, record)
	}
	return records
}

// FetchSplits retrieves stock split/rights-issue data from tushare split API.
// startDate, endDate: format "YYYYMMDD" (optional — pass "" to fetch all available).
// Tushare split API fields: ts_code, trade_date, stk_div_ratio, cash_div_ratio, currency
func (c *TushareClient) FetchSplits(ctx context.Context, symbol string, startDate, endDate string) ([]domain.Split, error) {
	params := map[string]interface{}{
		"ts_code": symbol,
	}
	if startDate != "" {
		params["start_date"] = formatDate(startDate)
	}
	if endDate != "" {
		params["end_date"] = formatDate(endDate)
	}

	resp, err := c.call(ctx, "split", params, "ts_code,trade_date,stk_div_ratio,cash_div_ratio,currency")
	if err != nil {
		return nil, err
	}

	records := c.normalizeSplits(resp)
	if len(records) == 0 {
		return nil, nil
	}

	// Save to database
	ptrs := make([]*domain.Split, len(records))
	for i := range records {
		ptrs[i] = &records[i]
	}
	if err := c.store.SaveSplitBatch(ctx, ptrs); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save splits")
	}

	c.logger.Info().Str("symbol", symbol).Int("count", len(records)).Msg("Splits fetched and saved")
	return records, nil
}

// normalizeSplits converts tushare split API response to domain.Split.
func (c *TushareClient) normalizeSplits(resp *TushareResponse) []domain.Split {
	var records []domain.Split
	for _, item := range resp.Data.Items {
		if len(item) < 5 {
			continue
		}

		tsCode := c.fieldStr(item, 0)
		if tsCode == "" {
			continue
		}

		tradeDateStr := c.fieldStr(item, 1)
		tradeDate, _ := time.Parse("20060102", tradeDateStr)

		record := domain.Split{
			Symbol:       tsCode,
			TradeDate:    tradeDate,
			StkDivRatio:  c.fieldFloat(item, 2), // stk_div_ratio — stock dividend/split ratio
			CashDivRatio: c.fieldFloat(item, 3), // cash_div_ratio — cash dividend ratio
			Currency:     c.fieldStr(item, 4),   // currency
		}
		records = append(records, record)
	}
	return records
}
