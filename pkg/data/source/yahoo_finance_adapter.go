package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// YahooFinanceAdapter implements DataSourceAdapter for Yahoo Finance
// (query1.finance.yahoo.com) daily OHLCV.
//
// The official Go SDK (yfinance) is not imported to keep dependencies
// minimal; we hit the public chart endpoint directly:
//   GET https://query1.finance.yahoo.com/v8/finance/chart/{symbol}
//     ?range=1y&interval=1d&events=history
//
// This is the same surface yfinance wraps internally.
type YahooFinanceAdapter struct {
	AdapterBase
	httpClient *http.Client
	baseURL    string
}

// NewYahooFinanceAdapter constructs a YahooFinanceAdapter.
func NewYahooFinanceAdapter() *YahooFinanceAdapter {
	return &YahooFinanceAdapter{
		AdapterBase: NewAdapterBase("yahoo_finance", true),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		baseURL:     "https://query1.finance.yahoo.com",
	}
}

// Type implements DataSourceAdapter.Type.
func (a *YahooFinanceAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *YahooFinanceAdapter) SupportedTypes() []string {
	return []string{
		DataTypeGlobalOHLCV,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *YahooFinanceAdapter) Schema(dataType string) (DataSchema, error) {
	if dataType == DataTypeGlobalOHLCV {
		return DataSchema{
			DataType: DataTypeGlobalOHLCV,
			Fields: []SchemaField{
				{Name: "open", Type: "float", Required: true, Unit: "usd"},
				{Name: "high", Type: "float", Required: true, Unit: "usd"},
				{Name: "low", Type: "float", Required: true, Unit: "usd"},
				{Name: "close", Type: "float", Required: true, Unit: "usd"},
				{Name: "volume", Type: "float", Required: false},
				{Name: "adj_close", Type: "float", Required: false, Unit: "usd"},
			},
		}, nil
	}
	return DataSchema{}, fmt.Errorf("%w: yahoo_finance does not serve %s", ErrUnsupported, dataType)
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *YahooFinanceAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	switch req.DataType {
	case DataTypeGlobalOHLCV:
		return a.fetchDaily(ctx, req)
	default:
		return nil, fmt.Errorf("%w: yahoo_finance: %s", ErrUnsupported, req.DataType)
	}
}

// yahooChartResponse is the response shape of /v8/finance/chart/{symbol}.
type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol     string `json:"symbol"`
				Range      string `json:"range"`
				Currency   string `json:"currency"`
				Exchange   string `json:"fullExchangeName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []float64 `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`
		Error interface{} `json:"error"`
	} `json:"chart"`
}

func (a *YahooFinanceAdapter) fetchDaily(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("yahoo_finance daily: symbols required")
	}
	range_ := "1y"
	if !req.StartDate.IsZero() {
		// Approximate range from the start date.
		days := int(time.Since(req.StartDate).Hours() / 24)
		switch {
		case days <= 5:
			range_ = "5d"
		case days <= 30:
			range_ = "1mo"
		case days <= 90:
			range_ = "3mo"
		case days <= 180:
			range_ = "6mo"
		case days <= 365:
			range_ = "1y"
		case days <= 730:
			range_ = "2y"
		case days <= 1825:
			range_ = "5y"
		default:
			range_ = "max"
		}
	}
	items := make([]DataItem, 0)
	for _, sym := range req.Symbols {
		rows, err := a.fetchDailyForSymbol(ctx, sym, range_, req.StartDate, req.EndDate)
		if err != nil {
			return nil, fmt.Errorf("yahoo_finance %s: %w", sym, err)
		}
		items = append(items, rows...)
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (a *YahooFinanceAdapter) fetchDailyForSymbol(ctx context.Context, sym, range_ string, start, end time.Time) ([]DataItem, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u := a.baseURL + "/v8/finance/chart/" + sym + "?range=" + range_ + "&interval=1d&events=history"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (quant-trading)")
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo_finance: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: yahoo_finance %d: %s", ErrUpstreamUnavailable, resp.StatusCode, string(body))
	}
	var payload yahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("yahoo_finance decode: %w", err)
	}
	if len(payload.Chart.Result) == 0 {
		return nil, fmt.Errorf("%w: yahoo_finance: empty result", ErrUpstreamUnavailable)
	}
	result := payload.Chart.Result[0]
	if len(result.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("%w: yahoo_finance: empty quote", ErrUpstreamUnavailable)
	}
	quote := result.Indicators.Quote[0]
	var adjClose []float64
	if len(result.Indicators.AdjClose) > 0 {
		adjClose = result.Indicators.AdjClose[0].AdjClose
	}
	items := make([]DataItem, 0, len(result.Timestamp))
	for i, ts := range result.Timestamp {
		if i >= len(quote.Open) {
			break
		}
		date := time.Unix(ts, 0).UTC()
		if !start.IsZero() && date.Before(start) {
			continue
		}
		if !end.IsZero() && date.After(end) {
			continue
		}
		var adj float64
		if i < len(adjClose) {
			adj = adjClose[i]
		}
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: date,
			Data: map[string]interface{}{
				"open":      quote.Open[i],
				"high":      quote.High[i],
				"low":       quote.Low[i],
				"close":     quote.Close[i],
				"volume":    quote.Volume[i],
				"adj_close": adj,
			},
		})
	}
	return items, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
func (a *YahooFinanceAdapter) HealthCheck(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		a.baseURL+"/v8/finance/chart/AAPL?range=1d&interval=1d", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (quant-trading)")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: yahoo_finance: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: yahoo_finance: status %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *YahooFinanceAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}
}

// String renders a debug-friendly summary.
func (a *YahooFinanceAdapter) String() string {
	return "YahooFinanceAdapter{name=" + a.Name() + "}"
}

// yahooToFloat64 is a small helper that converts a possibly-nil JSON
// number to float64 without panicking. Currently unused; kept for
// future schema variations.
func yahooToFloat64(_ interface{}) float64 {
	return 0
}

// silence "imported and not used" for io if all references are moved
// into helper functions in future refactors.
var _ = io.Discard

// silence strconv when future debugging needs it.
var _ = strconv.Itoa
