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

// jsonDecode is a small wrapper around json.NewDecoder for readability.
func jsonDecode(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

// XueqiuAdapter implements DataSourceAdapter for xueqiu.com hot search
// (热搜榜) and stock news feeds.
type XueqiuAdapter struct {
	AdapterBase
	httpClient *http.Client
	baseURL    string
}

// NewXueqiuAdapter constructs a XueqiuAdapter.
func NewXueqiuAdapter() *XueqiuAdapter {
	return &XueqiuAdapter{
		AdapterBase: NewAdapterBase("xueqiu", true),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		baseURL:     "https://xueqiu.com",
	}
}

// Type implements DataSourceAdapter.Type.
func (a *XueqiuAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *XueqiuAdapter) SupportedTypes() []string {
	return []string{
		DataTypeHotSearch,
		DataTypeNews,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *XueqiuAdapter) Schema(dataType string) (DataSchema, error) {
	switch dataType {
	case DataTypeHotSearch:
		return DataSchema{
			DataType: DataTypeHotSearch,
			Fields: []SchemaField{
				{Name: "rank", Type: "int", Required: true},
				{Name: "keyword", Type: "string", Required: true},
				{Name: "heat", Type: "float", Required: false},
			},
		}, nil
	case DataTypeNews:
		return DataSchema{
			DataType: DataTypeNews,
			Fields: []SchemaField{
				{Name: "title", Type: "string", Required: true},
				{Name: "content", Type: "string", Required: false},
				{Name: "publish_time", Type: "datetime", Required: true},
				{Name: "url", Type: "string", Required: false},
			},
		}, nil
	}
	return DataSchema{}, fmt.Errorf("%w: xueqiu does not serve %s", ErrUnsupported, dataType)
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *XueqiuAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	switch req.DataType {
	case DataTypeHotSearch:
		return a.fetchHotSearch(ctx, req)
	case DataTypeNews:
		return a.fetchNews(ctx, req)
	default:
		return nil, fmt.Errorf("%w: xueqiu: %s", ErrUnsupported, req.DataType)
	}
}

// xueqiuHotSearchResponse mirrors the response shape of the xueqiu hot
// search endpoint.
type xueqiuHotSearchResponse struct {
	Data []xueqiuHotSearchItem `json:"data"`
}

type xueqiuHotSearchItem struct {
	Rank    int     `json:"rank"`
	Name    string  `json:"name"`
	Hot     float64 `json:"hot"`
	HotRank int     `json:"hot_rank"`
}

func (a *XueqiuAdapter) fetchHotSearch(ctx context.Context, _ FetchRequest) (*FetchResponse, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		a.baseURL+"/query/v1/search/status?count=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (quant-trading)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://xueqiu.com/")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xueqiu hot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%w: xueqiu hot %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	var payload xueqiuHotSearchResponse
	if err := jsonDecode(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("xueqiu hot decode: %w", err)
	}
	now := time.Now().UTC()
	items := make([]DataItem, 0, len(payload.Data))
	for _, it := range payload.Data {
		if it.Name == "" {
			continue
		}
		items = append(items, DataItem{
			Symbol:    strconv.Itoa(it.Rank),
			TradeTime: now,
			Data: map[string]interface{}{
				"rank":          it.Rank,
				"keyword":       it.Name,
				"snapshot_time": now,
				"heat":          it.Hot,
			},
		})
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// xueqiuNewsResponse is the per-symbol news feed response.
type xueqiuNewsResponse struct {
	Items []xueqiuNewsItem `json:"items"`
}

type xueqiuNewsItem struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url"`
	User      struct {
		ScreenName string `json:"screen_name"`
	} `json:"user"`
}

func (a *XueqiuAdapter) fetchNews(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("xueqiu news: symbols required")
	}
	items := make([]DataItem, 0)
	for _, sym := range req.Symbols {
		code := sym
		if len(sym) > 3 && (sym[len(sym)-2:] == "SH" || sym[len(sym)-2:] == "SZ") {
			code = sym[:len(sym)-3]
		}
		rows, err := a.fetchNewsForSymbol(ctx, code, req.StartDate, req.EndDate)
		if err != nil {
			return nil, fmt.Errorf("xueqiu news %s: %w", sym, err)
		}
		// Re-attribute to the project canonical symbol.
		for _, r := range rows {
			r.Symbol = sym
			items = append(items, r)
		}
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (a *XueqiuAdapter) fetchNewsForSymbol(ctx context.Context, code string, start, end time.Time) ([]DataItem, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u := a.baseURL + "/v4/statuses/user_timeline.json?user_id=&symbol=" + code + "&count=20&source=announcement"
	if !start.IsZero() {
		u += "&since=" + strconv.FormatInt(start.Unix(), 10)
	}
	if !end.IsZero() {
		u += "&until=" + strconv.FormatInt(end.Unix(), 10)
	}
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (quant-trading)")
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xueqiu news fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%w: xueqiu news %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	var payload xueqiuNewsResponse
	if err := jsonDecode(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("xueqiu news decode: %w", err)
	}
	items := make([]DataItem, 0, len(payload.Items))
	for _, it := range payload.Items {
		items = append(items, DataItem{
			Symbol:    code,
			TradeTime: it.CreatedAt,
			Data: map[string]interface{}{
				"news_id":      strconv.FormatInt(it.ID, 10),
				"title":        it.Title,
				"content":      it.Text,
				"publish_time": it.CreatedAt,
				"url":          it.URL,
				"source_name":  it.User.ScreenName,
			},
		})
	}
	return items, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
func (a *XueqiuAdapter) HealthCheck(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		a.baseURL+"/", nil)
	if err != nil {
		return err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: xueqiu: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: xueqiu: status %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *XueqiuAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}
}

// String renders a debug-friendly summary.
func (a *XueqiuAdapter) String() string {
	return "XueqiuAdapter{name=" + a.Name() + "}"
}
