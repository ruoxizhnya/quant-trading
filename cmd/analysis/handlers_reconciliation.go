package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// P2-8 (Sprint 6 ODR-030): Reconciliation handler.
//
// 监管依据: 中国结算 《证券资金对账指引》 (2023) §3.2 要求券商
// 内部系统与登记结算公司的对账结果应可被审计 / 监管回溯。
//
// ReconciliationHandler exposes the in-process ReconciliationWorker
// over HTTP so operators can:
//   - GET  /api/reconciliation/latest     — 最新的对账报告
//   - GET  /api/reconciliation/history     — 最近 N 条历史报告
//   - POST /api/reconciliation/run         — 强制对账一次 (测试 / 应急)
//   - GET  /api/reconciliation/config      — 当前配置 (interval / tolerances)
//
// The handler is intentionally read-only except for /run; the
// reconciliation worker is owned by main.go's wiring layer.
type ReconciliationHandler struct {
	worker *live.ReconciliationWorker
	logger zerolog.Logger
}

// NewReconciliationHandler wires the handler to a worker. The worker
// may be nil when the feature is disabled; in that case, the handler
// returns 503 Service Unavailable.
func NewReconciliationHandler(worker *live.ReconciliationWorker, logger zerolog.Logger) *ReconciliationHandler {
	return &ReconciliationHandler{
		worker: worker,
		logger: logger.With().Str("component", "reconciliation_handler").Logger(),
	}
}

// RegisterRoutes wires the reconciliation endpoints under
// /api/reconciliation on the supplied router.
func (h *ReconciliationHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/reconciliation")
	{
		group.GET("/latest", h.latest)
		group.GET("/history", h.history)
		group.POST("/run", h.run)
		group.GET("/config", h.config)
	}
}

// latest returns the most recent reconciliation report, or 404 when
// no cycle has run yet.
func (h *ReconciliationHandler) latest(c *gin.Context) {
	if h.worker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reconciliation worker is not enabled"})
		return
	}
	rep := h.worker.History().Latest()
	if rep == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no reconciliation report yet"})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// history returns the buffered recent reports (newest first). The
// optional ?limit=N caps the response (default 20, max 100).
func (h *ReconciliationHandler) history(c *gin.Context) {
	if h.worker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reconciliation worker is not enabled"})
		return
	}
	limit := 20
	if v := c.Query("limit"); v != "" {
		if _, err := parseIntQuery(v, &limit); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	snap := h.worker.History().Snapshot()
	if len(snap) > limit {
		snap = snap[:limit]
	}
	c.JSON(http.StatusOK, gin.H{
		"count":   len(snap),
		"reports": snap,
	})
}

// runRequest accepts an optional accountID; when empty, the worker
// probes the local snapshotter for the default account.
type runRequest struct {
	AccountID string `json:"account_id"`
}

// run forces a reconciliation cycle. The cycle is synchronous —
// returns the full report when complete. Intended for tests and
// operator-triggered emergency reconciliation; the periodic loop
// also calls ReconcileOnce on its interval.
func (h *ReconciliationHandler) run(c *gin.Context) {
	if h.worker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reconciliation worker is not enabled"})
		return
	}
	var req runRequest
	_ = c.ShouldBindJSON(&req) // body is optional

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	rep, err := h.worker.ReconcileOnce(ctx, req.AccountID)
	if err != nil {
		h.logger.Warn().Err(err).Str("account_id", req.AccountID).Msg("forced reconciliation failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// config returns the worker's effective configuration. The Interval
// field is rendered as a human-readable string for UI display.
func (h *ReconciliationHandler) config(c *gin.Context) {
	if h.worker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reconciliation worker is not enabled"})
		return
	}
	cfg := h.worker.Config()
	c.JSON(http.StatusOK, gin.H{
		"interval":            cfg.Interval.String(),
		"interval_seconds":    cfg.Interval.Seconds(),
		"cash_tolerance":      cfg.CashTolerance,
		"quantity_tolerance":  cfg.QuantityTolerance,
		"market_value_tol":    cfg.MarketValueTol,
		"fee_tolerance":       cfg.FeeTolerance,
		"report_path":         cfg.ReportPath,
		"history_limit":       cfg.HistoryLimit,
		"enabled":             cfg.Enabled,
	})
}

// parseIntQuery is a tiny helper: parses a string -> int. Returns
// the parsed int via the out pointer; returns error on bad input.
func parseIntQuery(s string, out *int) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errBadInt
		}
		n = n*10 + int(r-'0')
	}
	*out = n
	return n, nil
}

// errBadInt is a sentinel for the parseIntQuery helper.
var errBadInt = errors.New("bad integer")
