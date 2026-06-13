package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/compliance"
)

func init() { gin.SetMode(gin.TestMode) }

// newTestComplianceHandler constructs a ComplianceHandler for handler
// tests. The `t.TempDir()` output path keeps the on-disk report
// isolated from other tests.
func newTestComplianceHandler(t *testing.T) *ComplianceHandler {
	t.Helper()
	logger := zerolog.Nop()
	defaultProfile := compliance.SuitabilityProfile{
		UserID:           "test-user",
		AssetDailyAvgCNY: 2_000_000,
		RiskLevel:        compliance.RiskLevelAggressive,
	}
	cfg := compliance.LargeTradeConfig{
		SingleThresholdCNY:     2_000_000,
		CumulativeThresholdCNY: 5_000_000,
		OutputPath:             t.TempDir(),
		AccountWhitelist:       map[string]bool{},
	}
	return NewComplianceHandler(logger, defaultProfile, cfg)
}

func doRequest(handler *ComplianceHandler, method, path string, body any) *httptest.ResponseRecorder {
	r := gin.New()
	handler.RegisterRoutes(r)
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ============================================================
// P2-4: /api/compliance/check
// ============================================================

func TestHandler_Check_AllowsMainBoard(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "POST", "/api/compliance/check", map[string]any{"symbol": "000001.SZ"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp checkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Allowed {
		t.Fatalf("expected allowed for main board, got reasons %v", resp.Reasons)
	}
}

func TestHandler_Check_RejectsChiNext(t *testing.T) {
	h := newTestComplianceHandler(t)
	// 50k < 100k ChiNext threshold → reject
	w := doRequest(h, "POST", "/api/compliance/check", map[string]any{
		"symbol":               "300750.SZ",
		"asset_daily_avg_cny":  50_000,
		"first_trade_at":       time.Now().AddDate(0, -36, 0).Format(time.RFC3339),
		"risk_level":           4,
		"boards_enabled":       []string{"chinext"},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 (compliance rejected), got %d: %s", w.Code, w.Body.String())
	}
	var resp checkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Allowed {
		t.Fatal("expected not allowed")
	}
	if len(resp.Reasons) == 0 {
		t.Fatal("expected at least one reason")
	}
}

func TestHandler_Check_MissingSymbol(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "POST", "/api/compliance/check", map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (missing symbol), got %d", w.Code)
	}
}

func TestHandler_Requirements_ReturnsThree(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "GET", "/api/compliance/requirements", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Requirements []compliance.BoardRequirement `json:"requirements"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Requirements) != 3 {
		t.Fatalf("expected 3 boards, got %d", len(resp.Requirements))
	}
}

func TestHandler_Boards_ReturnsThree(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "GET", "/api/compliance/boards", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "chinext") {
		t.Fatal("expected chinext in boards list")
	}
}

// ============================================================
// P2-5: /api/compliance/abnormal/run
// ============================================================

func TestHandler_AbnormalRun_EmptyInput(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "POST", "/api/compliance/abnormal/run",
		map[string]any{"account_id": "test"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp abnormalRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 0 {
		t.Fatalf("expected 0 alerts, got %d", resp.Count)
	}
}

func TestHandler_AbnormalRun_FiresFrequentCancel(t *testing.T) {
	h := newTestComplianceHandler(t)
	// Default FrequentCancel.Window = 1 minute — orders must fall
	// inside that window for the detector to consider them. Anchor
	// the orders at "now - 10s" with successive 1s offsets.
	base := time.Now().Add(-10 * time.Second)
	orders := []map[string]any{}
	for i := 0; i < 3; i++ {
		orders = append(orders, map[string]any{
			"order_id":     "ord-" + string(rune('a'+i)),
			"symbol":       "000001.SZ",
			"direction":    "buy",
			"quantity":     1000.0,
			"price":        10.0,
			"filled_qty":   0.0,
			"status":       "cancelled",
			"submitted_at": base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			"updated_at":   base.Add(time.Duration(i)*time.Second + 100*time.Millisecond).Format(time.RFC3339Nano),
		})
	}
	w := doRequest(h, "POST", "/api/compliance/abnormal/run", map[string]any{
		"account_id": "test-acct",
		"orders":     orders,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp abnormalRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// We have 3 cancels within 1 minute with 100% rate, so this
	// fires FrequentCancel. It also fires Spoofing because the
	// latency is < 500ms — accept either or both.
	if resp.Count < 1 {
		t.Fatalf("expected ≥1 alert, got %d", len(resp.Alerts))
	}
	hasFrequentCancel := false
	for _, a := range resp.Alerts {
		if a.Category == compliance.CategoryFrequentCancel {
			hasFrequentCancel = true
			break
		}
	}
	if !hasFrequentCancel {
		t.Fatalf("expected a frequent_cancel alert, got %+v", resp.Alerts)
	}
}

// ============================================================
// P2-6: /api/compliance/report/daily
// ============================================================

func TestHandler_ReportDaily_BasicFlow(t *testing.T) {
	h := newTestComplianceHandler(t)
	dayStart := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []map[string]any{
		{
			"trade_id":   "trd-1",
			"order_id":   "acct-A:ord-1",
			"symbol":     "000001.SZ",
			"direction":  "buy",
			"quantity":   100_000.0,
			"price":      20.0,
			"fee":        600.0,
			"trade_time": dayStart.Add(1 * time.Hour).Format(time.RFC3339Nano),
		},
	}
	w := doRequest(h, "POST", "/api/compliance/report/daily", map[string]any{
		"trading_date": "2026-06-15",
		"trades":       trades,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp reportDailyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(resp.Path, "large-trades-2026-06-15.json") {
		t.Fatalf("expected file ending with large-trades-2026-06-15.json, got %s", resp.Path)
	}
	if len(resp.Report.LargeTrades) != 1 {
		t.Fatalf("expected 1 large trade, got %d", len(resp.Report.LargeTrades))
	}
}

func TestHandler_ReportDaily_BadDateFormat(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "POST", "/api/compliance/report/daily", map[string]any{
		"trading_date": "not-a-date",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (bad date), got %d", w.Code)
	}
}
