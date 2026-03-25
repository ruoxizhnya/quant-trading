package data

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/httpclient"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/rs/zerolog"
)

const (
	tushareRateLimit     = 200 // requests per minute on free tier
	tushareRateLimitDur  = time.Minute
)

// TushareClient wraps the tushare.pro HTTP API.
type TushareClient struct {
	httpClient *httpclient.Client
	token     string
	logger    zerolog.Logger
	store     *storage.PostgresStore
	cache     *storage.Cache

	mu           sync.Mutex
	lastRequest  time.Time
	requestCount int
}

// NewTushareClient creates a new Tushare API client.
func NewTushareClient(token, baseURL string, maxRetries int, store *storage.PostgresStore, cache *storage.Cache) *TushareClient {
	return &TushareClient{
		httpClient: httpclient.New(baseURL, 30*time.Second, maxRetries),
		token:     token,
		logger:    logging.WithContext(map[string]any{"component": "tushare_client"}),
		store:     store,
		cache:     cache,
	}
}

// TushareRequest represents a tushare API request payload.
type TushareRequest struct {
	APIName  string                 `json:"api_name"`
	Token    string                 `json:"token"`
	Params   map[string]interface{} `json:"params,omitempty"`
	Fields   string                 `json:"fields,omitempty"`
}

// TushareResponse represents a tushare API response.
type TushareResponse struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Request TushareRequestMeta `json:"request"`
	Data    TushareData     `json:"data"`
}

// TushareRequestMeta contains metadata about the request.
type TushareRequestMeta struct {
	API      string `json:"api"`
	Token    string `json:"token"`
	Params   any    `json:"params"`
	Fields   string `json:"fields"`
	TS       int64  `json:"ts"`
}

// TushareData contains the response data.
type TushareData struct {
	Fields []string        `json:"fields"`
	Items  [][]any         `json:"items"`
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

	c.logger.Debug().Interface("data_fields", tushareResp.Data.Fields).Int("items_count", len(tushareResp.Data.Items)).Msg("tushare response received")

	return &tushareResp, nil
}

// FetchStocks retrieves stock list from tushare and saves to database.
func (c *TushareClient) FetchStocks(ctx context.Context, exchange string, listStatus string) ([]domain.Stock, error) {
	params := map[string]interface{}{
		"exchange":   exchange,
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

// FetchDailyOHLCV retrieves daily OHLCV data from tushare using stk_factor_pro API with 前复权 (qfq) adjustment.
func (c *TushareClient) FetchDailyOHLCV(ctx context.Context, symbol string, startDate, endDate string) ([]domain.OHLCV, error) {
	params := map[string]interface{}{
		"ts_code":   symbol,
		"start_date": formatDate(startDate),
		"end_date":   formatDate(endDate),
	}

	resp, err := c.call(ctx, "stk_factor_pro", params, "ts_code,trade_date,open_qfq,high_qfq,low_qfq,close_qfq,vol,amount")
	if err != nil {
		return nil, err
	}

	records := c.normalizeDailyOHLCV(resp, symbol)
	if len(records) == 0 {
		return nil, nil
	}

	// Save to database
	domainRecords := make([]*domain.OHLCV, len(records))
	for i := range records {
		domainRecords[i] = &records[i]
	}
	if err := c.store.SaveOHLCVBatch(ctx, domainRecords); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to batch save OHLCV")
	}

	c.logger.Info().Str("symbol", symbol).Int("count", len(records)).Msg("OHLCV (qfq) fetched and saved")
	return records, nil
}

// normalizeDailyOHLCV converts tushare stk_factor_pro response to domain.OHLCV.
// stk_factor_pro fields: ts_code, trade_date, open_qfq, high_qfq, low_qfq, close_qfq, vol, amount
func (c *TushareClient) normalizeDailyOHLCV(resp *TushareResponse, symbol string) []domain.OHLCV {
	var records []domain.OHLCV
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

		ohlcv := domain.OHLCV{
			Symbol:    symbol,
			Date:      t,
			Open:      c.fieldFloat(item, 2),   // open_qfq
			High:      c.fieldFloat(item, 3),   // high_qfq
			Low:       c.fieldFloat(item, 4),   // low_qfq
			Close:     c.fieldFloat(item, 5),   // close_qfq
			Volume:    c.fieldFloat(item, 6),   // vol
			Turnover:  c.fieldFloat(item, 7),   // amount
			TradeDays: 0,                       // not available from stk_factor_pro
		}
		records = append(records, ohlcv)
	}
	return records
}

// FetchFundamentals retrieves financial data from tushare.
func (c *TushareClient) FetchFundamentals(ctx context.Context, symbol string, date string) ([]domain.Fundamental, error) {
	params := map[string]interface{}{
		"ts_code": symbol,
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

	domainRecords := make([]*domain.Fundamental, len(records))
	for i := range records {
		domainRecords[i] = &records[i]
	}
	if err := c.store.SaveFundamentalBatch(ctx, domainRecords); err != nil {
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

		fund := domain.Fundamental{
			Symbol:       symbol,
			Date:         t,
			PE:           c.fieldFloat(item, 3),
			PB:           c.fieldFloat(item, 4),
			PS:           c.fieldFloat(item, 5),
			ROE:          c.fieldFloat(item, 6),
			ROA:          c.fieldFloat(item, 7),
			DebtToEquity: c.fieldFloat(item, 8),
			GrossMargin:  c.fieldFloat(item, 9),
			NetMargin:    c.fieldFloat(item, 10),
			Revenue:      c.fieldFloat(item, 11),
			NetProfit:    c.fieldFloat(item, 12),
			TotalAssets:  c.fieldFloat(item, 13),
			TotalLiab:    c.fieldFloat(item, 14),
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
			TsCode:    tsCode,
			TradeDate: endDate,
			AnnDate:   annDate,
			EndDate:   endDate,
			PE:        c.fieldFloatPtr(item, 3),
			PB:        c.fieldFloatPtr(item, 4),
			PS:        c.fieldFloatPtr(item, 5),
			ROE:       c.fieldFloatPtr(item, 6),
			ROA:       c.fieldFloatPtr(item, 7),
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
		suffix := tsCode[len(tsCode)-2:]
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
			TradeDate:      t,
			Exchange:       exchange,
			IsTradingDay:   isTradingDay,
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
			DivAmt:    c.fieldFloat(item, 4),  // div_amnt — cash dividend per share
			StkDiv:    c.fieldFloat(item, 5),  // stk_div — stock dividend per share
			StkRatio:  c.fieldFloat(item, 6),  // stk_ratio — stock split ratio
			CashRatio: c.fieldFloat(item, 7),  // cash_ratio — cash dividend ratio
		}
		records = append(records, record)
	}
	return records
}
