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
	"sync"
	"time"
)

// JuchaoAdapter implements DataSourceAdapter for 巨潮资讯网 (cninfo.com.cn)
// announcement feed.
//
// V3.2.2 fix: orgId is not a uniform "gssx0{code}" format. We
// dynamically resolve it from the szse_stock.json mapping table
// and cache it for subsequent calls.
type JuchaoAdapter struct {
	AdapterBase
	httpClient *http.Client
	baseURL    string

	mu       sync.RWMutex
	orgIDMap map[string]string // code (6-digit) -> orgId
	loadedAt time.Time
}

// NewJuchaoAdapter constructs a JuchaoAdapter.
func NewJuchaoAdapter() *JuchaoAdapter {
	return &JuchaoAdapter{
		AdapterBase: NewAdapterBase("juchao", true),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		baseURL:     "http://www.cninfo.com.cn",
		orgIDMap:    make(map[string]string),
	}
}

// Type implements DataSourceAdapter.Type.
func (a *JuchaoAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *JuchaoAdapter) SupportedTypes() []string {
	return []string{
		DataTypeAnnounce,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *JuchaoAdapter) Schema(dataType string) (DataSchema, error) {
	if dataType == DataTypeAnnounce {
		return DataSchema{
			DataType: DataTypeAnnounce,
			Fields: []SchemaField{
				{Name: "ann_title", Type: "string", Required: true},
				{Name: "ann_time", Type: "datetime", Required: true},
				{Name: "ann_type", Type: "string", Required: false},
				{Name: "pdf_url", Type: "string", Required: false},
			},
		}, nil
	}
	return DataSchema{}, fmt.Errorf("%w: juchao does not serve %s", ErrUnsupported, dataType)
}

// Fetch implements DataSourceAdapter.Fetch.
func (a *JuchaoAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	switch req.DataType {
	case DataTypeAnnounce:
		return a.fetchAnnouncements(ctx, req)
	default:
		return nil, fmt.Errorf("%w: juchao: %s", ErrUnsupported, req.DataType)
	}
}

func (a *JuchaoAdapter) fetchAnnouncements(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("juchao announcements: symbols required")
	}
	// Ensure orgId map is loaded.
	if err := a.ensureOrgIDMap(ctx); err != nil {
		return nil, fmt.Errorf("juchao: load orgId map: %w", err)
	}
	items := make([]DataItem, 0)
	for _, sym := range req.Symbols {
		orgID, err := a.resolveOrgID(ctx, sym)
		if err != nil {
			return nil, fmt.Errorf("juchao: %s: %w", sym, err)
		}
		rows, err := a.queryAnnouncements(ctx, sym, orgID, req.StartDate, req.EndDate)
		if err != nil {
			return nil, fmt.Errorf("juchao query %s: %w", sym, err)
		}
		items = append(items, rows...)
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// juchaoStockMapResponse is the szse_stock.json response.
type juchaoStockMapResponse struct {
	StockList []struct {
		Code  string `json:"code"`
		OrgID string `json:"orgId"`
		Zwjc  string `json:"zwjc"`
	} `json:"stockList"`
}

// ensureOrgIDMap loads the orgId map if it has not been loaded or is
// older than 24 hours. The map is large (~6200 entries) and changes
// rarely, so daily refresh is sufficient.
func (a *JuchaoAdapter) ensureOrgIDMap(ctx context.Context) error {
	a.mu.RLock()
	fresh := !a.loadedAt.IsZero() && time.Since(a.loadedAt) < 24*time.Hour
	a.mu.RUnlock()
	if fresh {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		a.baseURL+"/new/data/szse_stock.json", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "quant-trading/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("orgId map fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: orgId map %d: %s", ErrUpstreamUnavailable, resp.StatusCode, string(body))
	}
	var payload juchaoStockMapResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("orgId map decode: %w", err)
	}
	a.mu.Lock()
	a.orgIDMap = make(map[string]string, len(payload.StockList))
	for _, s := range payload.StockList {
		if s.Code != "" && s.OrgID != "" {
			a.orgIDMap[s.Code] = s.OrgID
		}
	}
	a.loadedAt = time.Now().UTC()
	a.mu.Unlock()
	return nil
}

// resolveOrgID returns the orgId for a project-canonical symbol.
//
//	"600519.SH" → "600519" → lookup in map → "gssh0600519" (example)
func (a *JuchaoAdapter) resolveOrgID(_ context.Context, sym string) (string, error) {
	code := sym
	if len(sym) > 3 && (sym[len(sym)-2:] == "SH" || sym[len(sym)-2:] == "SZ") {
		code = sym[:len(sym)-3]
	}
	a.mu.RLock()
	orgID, ok := a.orgIDMap[code]
	a.mu.RUnlock()
	if !ok {
		// Fallback heuristic (V3.1 behavior, will miss 601xxx segment).
		if sym[len(sym)-2:] == "SH" {
			orgID = "gssh0" + code
		} else {
			orgID = "gssz0" + code
		}
	}
	return orgID, nil
}

// juchaoAnnouncementResponse is the announcement query response.
type juchaoAnnouncementResponse struct {
	TotalAnnouncement int                      `json:"totalAnnouncement"`
	Announcements     []map[string]interface{} `json:"announcements"`
}

// queryAnnouncements calls /new/hisAnnouncement/query with the
// resolved orgId.
func (a *JuchaoAdapter) queryAnnouncements(ctx context.Context, sym, orgID string, start, end time.Time) ([]DataItem, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{}
	code := sym
	if len(sym) > 3 && (sym[len(sym)-2:] == "SH" || sym[len(sym)-2:] == "SZ") {
		code = sym[:len(sym)-3]
	}
	form.Set("stock", code+","+orgID)
	form.Set("tabName", "fulltext")
	form.Set("pageSize", "30")
	form.Set("pageNum", "1")
	form.Set("column", "sse")
	form.Set("category", "")
	if !start.IsZero() && !end.IsZero() {
		form.Set("seDate", start.Format("2006-01-02")+"~"+end.Format("2006-01-02"))
	}
	form.Set("isHLtitle", "true")

	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost,
		a.baseURL+"/new/hisAnnouncement/query", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (quant-trading)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("juchao query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: juchao query %d: %s", ErrUpstreamUnavailable, resp.StatusCode, string(body))
	}
	var payload juchaoAnnouncementResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("juchao decode: %w", err)
	}
	items := make([]DataItem, 0, len(payload.Announcements))
	for _, row := range payload.Announcements {
		annID := stringFromMap(row, "announcementId")
		title := stringFromMap(row, "announcementTitle")
		if annID == "" || title == "" {
			continue
		}
		annTime := parseEastmoneyDate(row["adjunctUrl"]) // placeholder; overwritten below
		// annTime is typically in the "announcementTime" field.
		if v, ok := row["announcementTime"]; ok {
			annTime = parseEastmoneyDate(v)
		}
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: annTime,
			Data: map[string]interface{}{
				"ann_id":    annID,
				"ann_title": title,
				"ann_time":  annTime,
				"ann_type":  stringFromMap(row, "adjunctType"),
				"pdf_url":   "http://static.cninfo.com.cn/" + stringFromMap(row, "adjunctUrl"),
			},
		})
	}
	return items, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
func (a *JuchaoAdapter) HealthCheck(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		a.baseURL+"/new/data/szse_stock.json", nil)
	if err != nil {
		return err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: juchao: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: juchao: status %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *JuchaoAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}
}

// String renders a debug-friendly summary.
func (a *JuchaoAdapter) String() string {
	a.mu.RLock()
	n := len(a.orgIDMap)
	a.mu.RUnlock()
	return "JuchaoAdapter{name=" + a.Name() + ", orgIdCache=" + strconv.Itoa(n) + "}"
}
