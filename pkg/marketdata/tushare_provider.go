package marketdata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/ruoxizhnya/quant-trading/pkg/httpclient"
	"encoding/json"
	"github.com/rs/zerolog"
)

type tushareProvider struct {
	token     string
	baseURL   string
	client    *httpclient.Client
	logger    zerolog.Logger
	store     OHLCVStore

	mu          sync.Mutex
	lastRequest time.Time
	reqCount    int
}

type OHLCVStore interface {
	GetOHLCVForDateRange(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error)
	GetFundamentalsSnapshot(ctx context.Context, symbol string, start, end time.Time) ([]domain.Fundamental, error)
}

func NewTushareProvider(token, baseURL string, store OHLCVStore, logger zerolog.Logger) Provider {
	return &tushareProvider{
		token:   token,
		baseURL: baseURL,
		client:  httpclient.New(baseURL, 30*time.Second, 3),
		logger:  logger.With().Str("component", "tushare_provider").Logger(),
		store:   store,
	}
}

func (p *tushareProvider) Name() string {
	return "tushare"
}

func (p *tushareProvider) CheckConnectivity(ctx context.Context) error {
	resp, err := p.callAPI(ctx, "trade_cal", map[string]interface{}{
		"exchange":   "SSE",
		"start_date": "20240101",
		"end_date":   "20240105",
	}, "exchange,cal_date,is_open")
	if err != nil {
		return fmt.Errorf("tushare connectivity check failed: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("tushare returned error %d: %s", resp.Code, resp.Msg)
	}
	return nil
}

func (p *tushareProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	if p.store != nil {
		bars, err := p.store.GetOHLCVForDateRange(ctx, symbol, start, end)
		if err == nil && len(bars) > 0 {
			return bars, nil
		}
	}

	params := map[string]interface{}{
		"ts_code":    symbol,
		"start_date": start.Format("20060102"),
		"end_date":   end.Format("20060102"),
	}
	resp, err := p.callAPI(ctx, "stk_factor_pro", params, "ts_code,trade_date,open_qfq,high_qfq,low_qfq,close_qfq,vol,amount")
	if err != nil {
		return nil, err
	}
	return p.normalizeOHLCV(resp.Data, symbol), nil
}

func (p *tushareProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	if p.store != nil {
		funds, err := p.store.GetFundamentalsSnapshot(ctx, symbol, date.AddDate(0, 0, -90), date)
		if err == nil && len(funds) > 0 {
			for i := len(funds) - 1; i >= 0; i-- {
				if !funds[i].Date.After(date) {
					return &funds[i], nil
				}
			}
			return &funds[len(funds)-1], nil
		}
	}

	params := map[string]interface{}{
		"ts_code":  symbol,
		"end_date": date.Format("20060102"),
	}
	resp, err := p.callAPI(ctx, "fina_indicator", params, "ts_code,end_date,pe,pb,ps,roe,roa,debt_to_equity,gross_margin,net_margin,revenue,net_profit,total_assets,total_liab")
	if err != nil {
		return nil, err
	}
	funds := p.normalizeFundamentals(resp.Data, symbol)
	if len(funds) == 0 {
		return nil, apperrors.NotFound("fundamental", symbol)
	}
	return &funds[len(funds)-1], nil
}

func (p *tushareProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	params := map[string]interface{}{}
	if exchange != "" {
		exParam := "SSE"
		if exchange == "SZSE" {
			exParam = "SZSE"
		}
		params["exchange"] = exParam
	}
	resp, err := p.callAPI(ctx, "stock_basic", params, "ts_code,symbol,name,area,industry,market,list_date")
	if err != nil {
		return nil, err
	}
	return p.normalizeStocks(resp.Data), nil
}

func (p *tushareProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -5)
	bars, err := p.GetOHLCV(ctx, symbol, start, end)
	if err != nil {
		return 0, err
	}
	if len(bars) == 0 {
		return 0, apperrors.NotFound("OHLCV", symbol)
	}
	return bars[len(bars)-1].Close, nil
}

func (p *tushareProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	resp, err := p.callAPI(ctx, "index_weight", map[string]interface{}{
		"index_code": indexCode,
	}, "index_code,con_code,in_date,out_date")
	if err != nil {
		return nil, err
	}
	var symbols []string
	for _, item := range resp.Data.Items {
		if len(item) >= 2 {
			if s, ok := item[1].(string); ok && s != "" {
				symbols = append(symbols, s)
			}
		}
	}
	return symbols, nil
}

func (p *tushareProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	var allDays []time.Time
	for _, ex := range []string{"SSE", "SZSE"} {
		resp, err := p.callAPI(ctx, "trade_cal", map[string]interface{}{
			"exchange":   ex,
			"start_date": start.Format("20060102"),
			"end_date":   end.Format("20060102"),
		}, "exchange,cal_date,is_open")
		if err != nil {
			return nil, err
		}
		days := p.normalizeTradingDays(resp.Data)
		allDays = append(allDays, days...)
	}
	sort.Slice(allDays, func(i, j int) bool { return allDays[i].Before(allDays[j]) })
	return uniqSortedDays(allDays), nil
}

func (p *tushareProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	stocks, err := p.GetStocks(ctx, "")
	if err != nil {
		return domain.Stock{Symbol: symbol}, err
	}
	for _, s := range stocks {
		if s.Symbol == symbol {
			return s, nil
		}
	}
	return domain.Stock{Symbol: symbol}, nil
}

func (p *tushareProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	data := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars, err := p.GetOHLCV(ctx, sym, start, end)
		if err != nil {
			p.logger.Warn().Str("symbol", sym).Err(err).Msg("BulkLoadOHLCV skip symbol")
			continue
		}
		data[sym] = bars
	}
	return data, nil
}

func (p *tushareProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	days, err := p.GetTradingDays(ctx, start, end)
	if err != nil {
		return false, err
	}
	return len(days) > 0, nil
}

type tushareResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data tushareData   `json:"data"`
}

type tushareData struct {
	Fields []string `json:"fields"`
	Items  [][]any   `json:"items"`
}

func (p *tushareProvider) callAPI(ctx context.Context, apiName string, params map[string]interface{}, fields string) (*tushareResponse, error) {
	p.mu.Lock()
	now := time.Now()
	if p.reqCount >= 200 {
		elapsed := now.Sub(p.lastRequest)
		if elapsed < time.Minute {
			time.Sleep(time.Minute - elapsed)
		}
		p.reqCount = 0
	}
	if p.reqCount == 0 {
		p.lastRequest = now
	}
	p.reqCount++
	p.mu.Unlock()

	body := map[string]interface{}{
		"api_name": apiName,
		"token":    p.token,
		"params":   params,
		"fields":   fields,
	}
	resp, err := p.client.Post(ctx, "", body)
	if err != nil {
		return nil, fmt.Errorf("tushare %s call failed: %w", apiName, err)
	}

	var tr tushareResponse
	if err := json.Unmarshal(resp.Body, &tr); err != nil {
		return nil, fmt.Errorf("tushare decode failed: %w", err)
	}
	if tr.Code != 0 {
		return nil, fmt.Errorf("tushare %s error %d: %s", apiName, tr.Code, tr.Msg)
	}
	return &tr, nil
}

func (p *tushareProvider) normalizeOHLCV(data tushareData, symbol string) []domain.OHLCV {
	var bars []domain.OHLCV
	fieldMap := make(map[string]int)
	for i, f := range data.Fields {
		fieldMap[f] = i
	}
	for _, item := range data.Items {
		dateIdx, ok := fieldMap["trade_date"]
		if !ok || dateIdx >= len(item) {
			continue
		}
		dateStr := strVal(item[dateIdx])
		if dateStr == "" {
			continue
		}
		t, err := time.Parse("20060102", dateStr)
		if err != nil {
			continue
		}
		bars = append(bars, domain.OHLCV{
			Symbol:   symbol,
			Date:     t,
			Open:     floatVal(fieldGet(data, item, "open_qfq")),
			High:     floatVal(fieldGet(data, item, "high_qfq")),
			Low:      floatVal(fieldGet(data, item, "low_qfq")),
			Close:    floatVal(fieldGet(data, item, "close_qfq")),
			Volume:   floatVal(fieldGet(data, item, "vol")),
			Turnover: floatVal(fieldGet(data, item, "amount")),
		})
	}
	return bars
}

func (p *tushareProvider) normalizeFundamentals(data tushareData, symbol string) []domain.Fundamental {
	var funds []domain.Fundamental
	fieldMap := make(map[string]int)
	for i, f := range data.Fields {
		fieldMap[f] = i
	}
	for _, item := range data.Items {
		endDateStr := strVal(fieldGet(data, item, "end_date"))
		t, _ := time.Parse("20060102", endDateStr)
		funds = append(funds, domain.Fundamental{
			Symbol:       symbol,
			Date:         t,
			PE:           floatVal(fieldGet(data, item, "pe")),
			PB:           floatVal(fieldGet(data, item, "pb")),
			PS:           floatVal(fieldGet(data, item, "ps")),
			ROE:          floatVal(fieldGet(data, item, "roe")),
			ROA:          floatVal(fieldGet(data, item, "roa")),
			DebtToEquity: floatVal(fieldGet(data, item, "debt_to_equity")),
			GrossMargin:  floatVal(fieldGet(data, item, "gross_margin")),
			NetMargin:    floatVal(fieldGet(data, item, "net_margin")),
			Revenue:      floatVal(fieldGet(data, item, "revenue")),
			NetProfit:    floatVal(fieldGet(data, item, "net_profit")),
			TotalAssets:  floatVal(fieldGet(data, item, "total_assets")),
			TotalLiab:    floatVal(fieldGet(data, item, "total_liab")),
		})
	}
	return funds
}

func (p *tushareProvider) normalizeStocks(data tushareData) []domain.Stock {
	var stocks []domain.Stock
	fieldMap := make(map[string]int)
	for i, f := range data.Fields {
		fieldMap[f] = i
	}
	for _, item := range data.Items {
		tsCode := strVal(fieldGet(data, item, "ts_code"))
		symbol := tsCode
		if symbol == "" {
			symbol = strVal(fieldGet(data, item, "symbol"))
		}
		if symbol == "" {
			continue
		}
		name := strVal(fieldGet(data, item, "name"))
		listDateStr := strVal(fieldGet(data, item, "list_date"))
		var listDate time.Time
		if listDateStr != "" {
			listDate, _ = time.Parse("20060102", listDateStr)
		}
		stocks = append(stocks, domain.Stock{
			Symbol:   symbol,
			Name:     name,
			Exchange: extractExchange(symbol),
			Status:   "active",
			ListDate: listDate,
		})
	}
	return stocks
}

func (p *tushareProvider) normalizeTradingDays(data tushareData) []time.Time {
	var days []time.Time
	fieldMap := make(map[string]int)
	for i, f := range data.Fields {
		fieldMap[f] = i
	}
	for _, item := range data.Items {
		isOpen := strVal(fieldGet(data, item, "is_open"))
		if isOpen != "1" {
			continue
		}
		calDate := strVal(fieldGet(data, item, "cal_date"))
		if calDate == "" {
			continue
		}
		t, err := time.Parse("20060102", calDate)
		if err != nil {
			continue
		}
		days = append(days, t)
	}
	return days
}

func fieldGet(data tushareData, item []any, fieldName string) any {
	idx := -1
	for i, f := range data.Fields {
		if f == fieldName {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(item) {
		return nil
	}
	return item[idx]
}

func floatVal(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := parseFloat(val)
		return f
	default:
		return 0
	}
}

func strVal(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		s := fmt.Sprintf("%.0f", val)
		if len(s) >= 8 {
			return s
		}
		return s
	default:
		return fmt.Sprintf("%v", val)
	}
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func extractExchange(symbol string) string {
	if len(symbol) >= 3 {
		suffix := symbol[len(symbol)-3:]
		if suffix == ".SH" || strings.HasSuffix(symbol, ".SH") {
			return "SSE"
		}
		if suffix == ".SZ" || strings.HasSuffix(symbol, ".SZ") {
			return "SZSE"
		}
	}
	return "SSE"
}

func uniqSortedDays(days []time.Time) []time.Time {
	if len(days) <= 1 {
		return days
	}
	out := make([]time.Time, 0, len(days))
	prev := time.Time{}
	for _, d := range days {
		if d.Equal(prev) {
			continue
		}
		out = append(out, d)
		prev = d
	}
	return out
}
