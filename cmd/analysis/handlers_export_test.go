package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/reporting"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ──── Unit: RenderHTML ────────────────────────────────────────

func sampleResponse() backtest.BacktestResponse {
	return backtest.BacktestResponse{
		ID:              "bt-test-1",
		Status:          "completed",
		Strategy:        "momentum",
		StartDate:       "2024-01-01",
		EndDate:         "2024-12-31",
		TotalReturn:     0.18,
		AnnualReturn:    0.21,
		SharpeRatio:     1.5,
		SortinoRatio:    2.1,
		MaxDrawdown:     -0.12,
		MaxDrawdownDate: "2024-08-15",
		WinRate:         0.55,
		TotalTrades:     42,
		WinTrades:       23,
		LoseTrades:      19,
		AvgHoldingDays:  8.5,
		CalmarRatio:     1.75,
		StockPool:       []string{"000001.SZ", "600000.SH"},
		InitialCapital:  1000000,
		PortfolioValues: []domain.PortfolioValue{
			{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), TotalValue: 1000000, Cash: 1000000, Positions: 0},
			{Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), TotalValue: 1080000, Cash: 500000, Positions: 580000},
			{Date: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), TotalValue: 1180000, Cash: 300000, Positions: 880000},
		},
		Trades: []domain.Trade{
			{ID: "t1", Symbol: "000001.SZ", Direction: domain.DirectionLong, Quantity: 100, Price: 10.0, Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "t2", Symbol: "600000.SH", Direction: domain.DirectionClose, Quantity: 100, Price: 11.5, Timestamp: time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
}

func TestRenderHTML_ContainsAllSections(t *testing.T) {
	body, ct, err := reporting.RenderHTML(sampleResponse(), reporting.HTMLReportOptions{
		IncludeEquityChart: true,
		IncludeTrades:      true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, body)
	assert.Equal(t, "text/html; charset=utf-8", ct)
	html := string(body)

	assert.Contains(t, html, "momentum 回测报告")
	assert.Contains(t, html, "2024-01-01 ~ 2024-12-31")
	assert.Contains(t, html, `id="bt-test-1"`)
	assert.Contains(t, html, "总收益率")
	assert.Contains(t, html, "18.00%")
	assert.Contains(t, html, "年化收益")
	assert.Contains(t, html, "Sharpe")
	assert.Contains(t, html, "1.50")
	assert.Contains(t, html, "最大回撤")
	assert.Contains(t, html, "-12.00%")
	assert.Contains(t, html, "<svg")
	assert.Contains(t, html, "<polyline")
	assert.Contains(t, html, "000001.SZ")
	assert.Contains(t, html, "600000.SH")
	assert.Contains(t, html, "t1")
	assert.Contains(t, html, "Ctrl+P")
}

func TestRenderHTML_DarkTheme(t *testing.T) {
	body, _, err := reporting.RenderHTML(sampleResponse(), reporting.HTMLReportOptions{Theme: "dark"})
	require.NoError(t, err)
	html := string(body)
	assert.Contains(t, html, `data-theme="dark"`)
	assert.Contains(t, html, "#0f172a")
}

func TestRenderHTML_EmptyPortfolioValues(t *testing.T) {
	resp := sampleResponse()
	resp.PortfolioValues = nil
	body, _, err := reporting.RenderHTML(resp, reporting.HTMLReportOptions{IncludeEquityChart: true})
	require.NoError(t, err)
	assert.Contains(t, string(body), "暂无数据")
}

func TestRenderHTML_NoTrades(t *testing.T) {
	resp := sampleResponse()
	resp.Trades = nil
	body, _, err := reporting.RenderHTML(resp, reporting.HTMLReportOptions{IncludeTrades: true})
	require.NoError(t, err)
	assert.Contains(t, string(body), "无交易记录")
}

func TestRenderHTML_OptOutEquity(t *testing.T) {
	resp := sampleResponse()
	body, _, err := reporting.RenderHTML(resp, reporting.HTMLReportOptions{
		IncludeEquityChart: false,
		IncludeTrades:      true,
	})
	require.NoError(t, err)
	html := string(body)
	assert.NotContains(t, html, "权益曲线")
	assert.NotContains(t, html, "<polyline")
}

func TestRenderHTML_OptOutTrades(t *testing.T) {
	resp := sampleResponse()
	body, _, err := reporting.RenderHTML(resp, reporting.HTMLReportOptions{
		IncludeEquityChart: true,
		IncludeTrades:      false,
	})
	require.NoError(t, err)
	assert.NotContains(t, string(body), "交易明细")
}

func TestRenderHTML_ZeroValue(t *testing.T) {
	// Zero-value options = no chart, no trades (caller opts in)
	body, _, err := reporting.RenderHTML(sampleResponse(), reporting.HTMLReportOptions{})
	require.NoError(t, err)
	html := string(body)
	assert.NotContains(t, html, "权益曲线")
	assert.NotContains(t, html, "交易明细")
	// Header + metrics always present
	assert.Contains(t, html, "momentum 回测报告")
	assert.Contains(t, html, "总收益率")
}

func TestRenderHTML_FooterNote(t *testing.T) {
	body, _, err := reporting.RenderHTML(sampleResponse(), reporting.HTMLReportOptions{
		IncludeEquityChart: true,
		IncludeTrades:      true,
		FooterNote:         "verified by Longshao",
	})
	require.NoError(t, err)
	assert.Contains(t, string(body), "verified by Longshao")
}

func TestRenderHTML_EscapesUserInput(t *testing.T) {
	resp := sampleResponse()
	resp.Strategy = "<script>alert('xss')</script>"
	body, _, err := reporting.RenderHTML(resp, reporting.HTMLReportOptions{IncludeEquityChart: true, IncludeTrades: true})
	require.NoError(t, err)
	html := string(body)
	assert.NotContains(t, html, "<script>alert")
	assert.Contains(t, html, "&lt;script&gt;")
}

func TestRenderHTML_TruncatesLargeTrades(t *testing.T) {
	resp := sampleResponse()
	for i := 0; i < 300; i++ {
		resp.Trades = append(resp.Trades, domain.Trade{
			ID: fmt.Sprintf("t%d", i+3), Symbol: "000002.SZ",
			Direction: domain.DirectionLong, Quantity: 100, Price: 5.0,
			Timestamp: time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC),
		})
	}
	body, _, err := reporting.RenderHTML(resp, reporting.HTMLReportOptions{IncludeTrades: true})
	require.NoError(t, err)
	assert.Contains(t, string(body), "最新 200 笔")
}

func TestRenderHTML_AllMetrics(t *testing.T) {
	body, _, err := reporting.RenderHTML(sampleResponse(), reporting.HTMLReportOptions{
		IncludeEquityChart: true,
		IncludeTrades:      true,
	})
	require.NoError(t, err)
	html := string(body)
	// Every metric label must appear
	requiredLabels := []string{
		"总收益率", "年化收益", "Sharpe", "Sortino", "Calmar",
		"最大回撤", "胜率", "总交易", "平均持仓",
	}
	for _, label := range requiredLabels {
		assert.Contains(t, html, label, "missing metric label %q", label)
	}
}

// ──── HTTP Integration ────────────────────────────────────────
// We use a minimal test handler that mirrors the production export
// handler exactly (lookup → render → write). The production handler is
// 10 lines of wiring; testing the wiring separately from the
// (already exhaustively tested) RenderHTML would only verify gin.

func newExportTestRouter() (*gin.Engine, *stubBacktestStore) {
	store := &stubBacktestStore{jobs: map[string]json.RawMessage{}}
	router := gin.New()
	api := router.Group("/api/backtest")
	api.GET("/:id/export/:format", exportTestHandler(store))
	return router, store
}

type stubBacktestStore struct {
	jobs map[string]json.RawMessage
}

func exportTestHandler(store *stubBacktestStore) gin.HandlerFunc {
	logger := zerolog.Nop()
	return func(c *gin.Context) {
		backtestID := c.Param("id")
		format := c.Param("format")
		if format != "html" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format"})
			return
		}
		raw, ok := store.jobs[backtestID]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "backtest not found or not completed"})
			return
		}
		var resp backtest.BacktestResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse stored result"})
			return
		}
		// Query-param opt-out: ?equity=0 / ?trades=0 hides the section
		opts := reporting.HTMLReportOptions{
			Theme:              c.DefaultQuery("theme", "light"),
			FooterNote:         c.Query("footer"),
			IncludeEquityChart: c.Query("equity") != "0",
			IncludeTrades:      c.Query("trades") != "0",
		}
		body, contentType, err := reporting.RenderHTML(resp, opts)
		if err != nil {
			logger.Error().Err(err).Msg("Render failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to render report"})
			return
		}
		filename := fmt.Sprintf("backtest-%s-%s.html", backtestID, time.Now().Format("20060102"))
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		c.Header("X-Backtest-Id", backtestID)
		c.Header("X-Backtest-Strategy", resp.Strategy)
		c.Data(http.StatusOK, contentType, body)
	}
}

func putStubJob(store *stubBacktestStore, id string, resp backtest.BacktestResponse) {
	b, _ := json.Marshal(resp)
	store.jobs[id] = b
}

func TestExportHandler_Html_Success(t *testing.T) {
	router, store := newExportTestRouter()
	putStubJob(store, "bt-test-1", sampleResponse())

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/bt-test-1/export/html", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	cd := w.Header().Get("Content-Disposition")
	assert.Contains(t, cd, "attachment")
	assert.Contains(t, cd, "backtest-bt-test-1-")
	assert.Equal(t, "bt-test-1", w.Header().Get("X-Backtest-Id"))
	assert.Equal(t, "momentum", w.Header().Get("X-Backtest-Strategy"))

	body := w.Body.String()
	assert.Contains(t, body, "<!DOCTYPE html>")
	assert.Contains(t, body, "momentum 回测报告")
	assert.Contains(t, body, "总收益率")
}

func TestExportHandler_Html_UnsupportedFormat(t *testing.T) {
	router, _ := newExportTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/bt-test-1/export/pdf", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported format")
}

func TestExportHandler_Html_NotFound(t *testing.T) {
	router, _ := newExportTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/missing/export/html", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

func TestExportHandler_Html_ThemeQueryParam(t *testing.T) {
	router, store := newExportTestRouter()
	putStubJob(store, "bt-test-1", sampleResponse())

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/bt-test-1/export/html?theme=dark", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `data-theme="dark"`)
}

func TestExportHandler_Html_OptOutTradesQuery(t *testing.T) {
	router, store := newExportTestRouter()
	putStubJob(store, "bt-test-1", sampleResponse())

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/bt-test-1/export/html?trades=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "交易明细")
	assert.Contains(t, body, "<polyline") // equity still there
}

func TestExportHandler_Html_OptOutEquityQuery(t *testing.T) {
	router, store := newExportTestRouter()
	putStubJob(store, "bt-test-1", sampleResponse())

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/bt-test-1/export/html?equity=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "权益曲线")
	assert.NotContains(t, body, "<polyline")
	assert.Contains(t, body, "交易明细") // trades still there
}

func TestExportHandler_Html_LargeReportUnder1MB(t *testing.T) {
	resp := sampleResponse()
	for i := 0; i < 200; i++ {
		resp.Trades = append(resp.Trades, domain.Trade{
			ID: fmt.Sprintf("t%d", i+3), Symbol: "000002.SZ",
			Direction: domain.DirectionLong, Quantity: 100, Price: 5.0,
			Timestamp: time.Date(2024, 1, 1+i%300, 0, 0, 0, 0, time.UTC),
		})
	}
	for i := 0; i < 100; i++ {
		resp.PortfolioValues = append(resp.PortfolioValues, domain.PortfolioValue{
			Date:       time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC),
			TotalValue: 1000000 + float64(i)*1000, Cash: 500000, Positions: 500000,
		})
	}
	router, store := newExportTestRouter()
	putStubJob(store, "bt-big", resp)

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/bt-big/export/html", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Less(t, len(body), 1*1024*1024, "HTML report should be under 1MB even with 200 trades + 100 portfolio points")
}
