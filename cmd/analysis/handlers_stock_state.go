package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// P2-13 (Sprint 6 ODR-030): Stock state / 退市 handler.
//
// 监管依据: 上交所/深交所/北交所 《股票上市规则》 (2024) 退市整理期
// 强制清仓义务 + 中国证券业协会 客户端告知 + 限制买入 (allow_sell_only).
//
// StockStateHandler exposes the in-process StockStateRegistry + ForcedLiquidator
// over HTTP so operators can:
//
//	GET    /api/stock-state/list                       — 列出所有状态 (按 delisted_date 排序)
//	GET    /api/stock-state/list?state=delisting       — 按状态过滤
//	GET    /api/stock-state/:symbol                    — 查询单只
//	POST   /api/stock-state/:symbol/set                 — 设置状态 (Listed/Suspended/Delisting/Delisted)
//	DELETE /api/stock-state/:symbol                    — 删除记录 (e.g. *ST 申诉成功)
//	POST   /api/stock-state/scan                       — 立即扫描强制清仓 (返回 LiquidationResult)
//
// The handler is intentionally a thin layer over the registry/liquidator;
// all business rules (legal transitions / delisting window) live in
// pkg/live/stock_state.go (P2-13) and are unit-tested there.
type StockStateHandler struct {
	registry   *live.StockStateRegistry
	liquidator *live.ForcedLiquidator
	logger     zerolog.Logger
}

// NewStockStateHandler wires the handler. liquidator may be nil if the
// feature is disabled (LiquidationWindow < 0). The handler returns 503
// for /scan in that case.
func NewStockStateHandler(registry *live.StockStateRegistry, liquidator *live.ForcedLiquidator, logger zerolog.Logger) *StockStateHandler {
	return &StockStateHandler{
		registry:   registry,
		liquidator: liquidator,
		logger:     logger.With().Str("component", "stock_state_handler").Logger(),
	}
}

// RegisterRoutes wires the stock-state endpoints on the supplied router.
func (h *StockStateHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/stock-state")
	{
		group.GET("/list", h.list)
		group.GET("/scan", h.scanGET)  // for browser convenience
		group.POST("/scan", h.scan)
		group.GET("/:symbol", h.get)
		group.POST("/:symbol/set", h.set)
		group.DELETE("/:symbol", h.delete)
	}
}

// list returns all records (or filtered by ?state=...).
func (h *StockStateHandler) list(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stock state registry is not enabled"})
		return
	}
	state := live.StockState(c.Query("state"))
	if state != "" {
		switch state {
		case live.StockStateListed, live.StockStateSuspended,
			live.StockStateDelisting, live.StockStateDelisted:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state filter"})
			return
		}
	}
	recs := h.registry.ListByState(state)
	c.JSON(http.StatusOK, gin.H{
		"count":    len(recs),
		"records":  recs,
		"registry_count": h.registry.Count(),
	})
}

// get returns a single record by symbol.
func (h *StockStateHandler) get(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stock state registry is not enabled"})
		return
	}
	symbol := c.Param("symbol")
	rec, ok := h.registry.GetState(symbol)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "symbol not found: " + symbol})
		return
	}
	c.JSON(http.StatusOK, rec)
}

// setRequest is the JSON body for POST /api/stock-state/:symbol/set.
type setRequest struct {
	State        string    `json:"state"`
	Reason       string    `json:"reason"`
	DelistedDate time.Time `json:"delisted_date"`
}

// set applies a state transition. Body:
//
//	{ "state": "delisting", "reason": "财务类退市", "delisted_date": "2026-07-22T15:00:00Z" }
//
// Returns 400 on illegal transition / invalid state, 200 on success.
func (h *StockStateHandler) set(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stock state registry is not enabled"})
		return
	}
	symbol := c.Param("symbol")
	var req setRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	state := live.StockState(req.State)
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}
	if err := h.registry.SetState(symbol, state, req.Reason, req.DelistedDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rec, _ := h.registry.GetState(symbol)
	c.JSON(http.StatusOK, rec)
}

// delete removes a record.
func (h *StockStateHandler) delete(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stock state registry is not enabled"})
		return
	}
	symbol := c.Param("symbol")
	h.registry.Delete(symbol)
	c.JSON(http.StatusOK, gin.H{"deleted": symbol})
}

// scanGET is a browser-friendly alias (some operators want a quick link).
func (h *StockStateHandler) scanGET(c *gin.Context) {
	h.scan(c)
}

// scan triggers an immediate forced-liquidation scan.
//
// Body (optional): { "trader": "mock" } — currently we always use the
// in-process mock trader (P1-26 ODR-022). In production this would
// dispatch to the active broker. For the handler, we accept the request
// and report a "no_trader_wired" placeholder — the periodic loop (added
// later) will use the live trader.
//
// This endpoint exists primarily for the unit-test / manual-fire path:
// when a *ST is announced, the operator can call /scan to ensure the
// registry is wired correctly and dry-run the actions.
func (h *StockStateHandler) scan(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stock state registry is not enabled"})
		return
	}
	if h.liquidator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "forced liquidator is not enabled (LiquidationWindow < 0?)"})
		return
	}
	// The forced liquidator requires a LiveTrader instance. The handler
	// does not own the trader (lives in main.go's wiring), so we
	// return the planned actions in "dry-run" form: enumerate
	// delisting records and report what *would* be force-liquidated.
	// This is consistent with the kill-switch audit pattern — the
	// actual flatten happens via the periodic scan or a manual
	// emergency-flatten call.
	now := time.Now()
	recs := h.registry.ListByState(live.StockStateDelisting)
	actions := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		actions = append(actions, map[string]any{
			"symbol":        r.Symbol,
			"state":         r.State,
			"delisted_date": r.DelistedDate,
			"in_window":     r.IsInDelistingWindow(now, 5*24*time.Hour),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"scanned_at": now,
		"actions":    actions,
		"count":      len(actions),
		"note":       "dry-run: actions are advisory; actual flatten requires a LiveTrader wired into main.go",
	})
	_ = context.Background()
}
