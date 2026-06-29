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

// AlphaVantageAdapter implements DataSourceAdapter for alphavantage.co.
//
// API endpoints (selected; the upstream service exposes 50+):
//   - TIME_SERIES_DAILY_ADJUSTED: daily OHLCV with adjusted close
//   - OVERVIEW: company profile
//   - NEWS_SENTIMENT: tickers + time window
//   - SMA / EMA / MACD / RSI / BBANDS / ATR: technical indicators
//
// This adapter focuses on TIME_SERIES_DAILY_ADJUSTED → DataTypeGlobalOHLCV.
// Fundamentals and technical indicators are out of scope for Sprint 4.
type AlphaVantageAdapter struct {
	AdapterBase
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewAlphaVantageAdapter constructs an AlphaVantageAdapter.
// apiKey may be empty; in that case Enabled() returns false.
func NewAlphaVantageAdapter(apiKey string) *AlphaVantageAdapter {
	return &AlphaVantageAdapter{
		AdapterBase: NewAdapterBase("alpha_vantage", apiKey != ""),
		apiKey:      apiKey,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		baseURL:     "https://www.alphavantage.co",
	}
}

// Type implements DataSourceAdapter.Type.
func (a *AlphaVantageAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *AlphaVantageAdapter) SupportedTypes() []string {
	return []string{
		DataTypeGlobalOHLCV,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *AlphaVantageAdapter) Schema(dataType string) (DataSchema, error) {
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
	return DataSchema{}, fmt.Errorf("%w: alpha_vantage does not serve %s", ErrUnsupported, dataType)
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *AlphaVantageAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if a.apiKey == "" {
		return nil, fmt.Errorf("alpha_vantage adapter: apiKey not configured")
	}
	switch req.DataType {
	case DataTypeGlobalOHLCV:
		return a.fetchDaily(ctx, req)
	default:
		return nil, fmt.Errorf("%w: alpha_vantage: %s", ErrUnsupported, req.DataType)
	}
}

// alphaVantageTimeSeriesResponse is the JSON response shape for
// TIME_SERIES_DAILY_ADJUSTED.
type alphaVantageTimeSeriesResponse struct {
	MetaData map[string]string            `json:"Meta Data"`
	Series   map[string]map[string]string `json:"Time Series (Daily)"`
}

func (a *AlphaVantageAdapter) fetchDaily(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("alpha_vantage daily: symbols required")
	}
	items := make([]DataItem, 0)
	for _, sym := range req.Symbols {
		rows, err := a.fetchDailyForSymbol(ctx, sym, req.StartDate, req.EndDate)
		if err != nil {
			return nil, fmt.Errorf("alpha_vantage %s: %w", sym, err)
		}
		items = append(items, rows...)
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (a *AlphaVantageAdapter) fetchDailyForSymbol(ctx context.Context, sym string, start, end time.Time) ([]DataItem, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("function", "TIME_SERIES_DAILY_ADJUSTED")
	q.Set("symbol", sym)
	q.Set("outputsize", "full")
	q.Set("apikey", a.apiKey)
	q.Set("datatype", "json")
	u := a.baseURL + "/query?" + q.Encode()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alpha_vantage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: alpha_vantage %d: %s", ErrUpstreamUnavailable, resp.StatusCode, string(body))
	}
	var payload alphaVantageTimeSeriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("alpha_vantage decode: %w", err)
	}
	if len(payload.Series) == 0 {
		// AlphaVantage returns a JSON with "Note" / "Information" fields
		// when rate limited or invalid key. Surface that as a retryable error.
		return nil, fmt.Errorf("%w: alpha_vantage returned no series (rate limit?)", ErrUpstreamUnavailable)
	}
	items := make([]DataItem, 0, len(payload.Series))
	for dateStr, row := range payload.Series {
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if !start.IsZero() && date.Before(start) {
			continue
		}
		if !end.IsZero() && date.After(end) {
			continue
		}
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: date,
			Data: map[string]interface{}{
				"open":      parseFloat(row["1. open"]),
				"high":      parseFloat(row["2. high"]),
				"low":       parseFloat(row["3. low"]),
				"close":     parseFloat(row["4. close"]),
				"volume":    parseFloat(row["6. volume"]),
				"adj_close": parseFloat(row["5. adjusted close"]),
			},
		})
	}
	return items, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
// AlphaVantage has no dedicated health endpoint; we hit the listing endpoint.
func (a *AlphaVantageAdapter) HealthCheck(ctx context.Context) error {
	if a.apiKey == "" {
		return fmt.Errorf("alpha_vantage: apiKey not configured")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("function", "GLOBAL_QUOTE")
	q.Set("symbol", "AAPL")
	q.Set("apikey", a.apiKey)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		a.baseURL+"/query?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: alpha_vantage: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: alpha_vantage: status %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
// AlphaVantage free tier: 5 req/min, 500/day. We use 5 req/min to
// prevent hitting the throttle; production keys may use a higher ceiling.
func (a *AlphaVantageAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 5,
		Burst:             1,
	}
}

// String renders a debug-friendly summary.
func (a *AlphaVantageAdapter) String() string {
	return "AlphaVantageAdapter{name=" + a.Name() + ", enabled=" + strconv.FormatBool(a.Enabled()) + "}"
}
