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

// ============================================================
// P2-7: /api/compliance/divestment/*
// ============================================================

func TestHandler_DivestmentCheck_Allowed(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "POST", "/api/compliance/divestment/check", map[string]any{
		"profile": map[string]any{
			"user_id":        "u-1",
			"symbol":         "000001.SZ",
			"holder_type":    "controlling",
			"holdings_pct":   10.0,
			"holdings_share": 10_000_000.0,
		},
		"plan": map[string]any{
			"symbol":   "000001.SZ",
			"method":   "auction",
			"quantity": 500_000.0,
			"at":       time.Now().Format(time.RFC3339Nano),
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp compliance.DivestmentCheckResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Allowed {
		t.Fatalf("expected allowed, got %v", resp.Reasons)
	}
	if resp.ApprovedQty != 500_000 {
		t.Fatalf("expected approved 500000, got %.0f", resp.ApprovedQty)
	}
}

func TestHandler_DivestmentCheck_Rejected(t *testing.T) {
	h := newTestComplianceHandler(t)
	// Pre-IPO 股东 + 无 lockup 条目 → 引擎硬拒
	w := doRequest(h, "POST", "/api/compliance/divestment/check", map[string]any{
		"profile": map[string]any{
			"user_id":        "u-1",
			"symbol":         "000001.SZ",
			"holder_type":    "pre_ipo",
			"holdings_pct":   8.0,
			"holdings_share": 8_000_000.0,
		},
		"plan": map[string]any{
			"symbol":   "000001.SZ",
			"method":   "auction",
			"quantity": 100.0,
			"at":       time.Now().Format(time.RFC3339Nano),
		},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp compliance.DivestmentCheckResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Allowed {
		t.Fatal("expected reject")
	}
	if len(resp.Reasons) == 0 {
		t.Fatal("expected non-empty reasons on reject")
	}
}

func TestHandler_DivestmentCheck_BadBody(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "POST", "/api/compliance/divestment/check", map[string]any{
		"profile": "not-an-object",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_DivestmentCheck_DirectorAnnualCap(t *testing.T) {
	h := newTestComplianceHandler(t)
	// 董监高: 一年内已减 24%, 拟再减 2% → 累计 26% > 25% → 拒
	now := time.Now()
	w := doRequest(h, "POST", "/api/compliance/divestment/check", map[string]any{
		"profile": map[string]any{
			"user_id":        "u-dir",
			"symbol":         "000001.SZ",
			"holder_type":    "director",
			"holdings_pct":   30.0,
			"holdings_share": 3_000_000.0,
		},
		"plan": map[string]any{
			"symbol":   "000001.SZ",
			"method":   "auction",
			"quantity": 200_000.0,
			"at":       now.Format(time.RFC3339Nano),
		},
		"recent": []map[string]any{{
			"symbol":   "000001.SZ",
			"method":   "auction",
			"quantity": 2_400_000.0, // 24%
			"price":    10.0,
			"at":       now.AddDate(0, -3, 0).Format(time.RFC3339Nano),
		}},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 (annual cap), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_DivestmentHolderTypes(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "GET", "/api/compliance/divestment/holder-types", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		HolderTypes []gin.H `json:"holder_types"`
		Count       int     `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 5 {
		t.Fatalf("expected 5 holder types, got %d", resp.Count)
	}
	if len(resp.HolderTypes) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(resp.HolderTypes))
	}
	// Spot-check: controlling 应有中文 label
	var foundCtrl bool
	for _, ht := range resp.HolderTypes {
		if id, _ := ht["id"].(string); id == "controlling" {
			if label, _ := ht["label"].(string); strings.Contains(label, "控股") {
				foundCtrl = true
			}
		}
	}
	if !foundCtrl {
		t.Fatal("expected controlling with 控股 label")
	}
}

func TestHandler_DivestmentMethods(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "GET", "/api/compliance/divestment/methods", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Methods []gin.H `json:"methods"`
		Count   int     `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 3 {
		t.Fatalf("expected 3 methods, got %d", resp.Count)
	}
}

func TestHandler_DivestmentRules(t *testing.T) {
	h := newTestComplianceHandler(t)
	w := doRequest(h, "GET", "/api/compliance/divestment/rules", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Rules       map[string]compliance.DivestmentRule `json:"rules"`
		GeneratedAt time.Time                            `json:"generated_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rules) != 5 {
		t.Fatalf("expected 5 rules, got %d", len(resp.Rules))
	}
	// Spot-check: controlling 集中竞价 cap = 1.0
	if got := resp.Rules["controlling"].AuctionWindowCapPct; got != 1.0 {
		t.Fatalf("expected controlling auction cap=1.0, got %.4f", got)
	}
}
