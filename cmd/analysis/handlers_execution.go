package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// P1-15 (Sprint 6, ODR-021): execution service merged into analysis
// service. The endpoints below mirror the original
// cmd/execution/main.go routes (HTTP method, path, request body,
// response shape) so existing clients only need to retarget at
// `analysis-service:8085/api/execution/*` instead of
// `execution-service:8084/*`. Path prefix `/api` is added; the
// rest of the surface is byte-compatible.
//
// The handler wraps a *live.MockTrader (or any live.LiveTrader
// implementation) so the runtime path is zero-HOP — the same
// in-memory instance that the backtest engine bridges to via
// `WithLiveTrader` is also exposed over HTTP for operator-driven
// paper trading.

type ExecutionHandler struct {
	trader live.LiveTrader
	logger zerolog.Logger

	// emergencyToken is the bearer token required to invoke
	// EmergencyFlatten via the HTTP endpoint. Empty token means
	// the endpoint is disabled. P2-3 (ODR-026): the token is
	// configured via `trading.emergency_token` in the analysis
	// service config; see config/analysis-service.yaml.
	emergencyToken string

	mu     sync.RWMutex
	orders map[string]*live.OrderResult
}

// NewExecutionHandler constructs an ExecutionHandler. The trader is
// typically a MockTrader; the same instance should also be passed
// to the backtest engine via WithLiveTrader so HTTP-driven paper
// orders are visible to backtest bridge (and vice versa).
//
// emergencyToken is the bearer token required for the
// emergency-flatten endpoint. Pass "" to disable the endpoint
// entirely (returns 404). Tokens are compared using
// crypto/subtle.ConstantTimeCompare to prevent timing attacks.
func NewExecutionHandler(trader live.LiveTrader, logger zerolog.Logger, emergencyToken string) *ExecutionHandler {
	return &ExecutionHandler{
		trader:         trader,
		logger:         logger.With().Str("component", "execution_handler").Logger(),
		emergencyToken: emergencyToken,
		orders:         make(map[string]*live.OrderResult),
	}
}

// RegisterRoutes wires the execution endpoints under /api/execution
// on the supplied router. Legacy root-level paths
// (/orders, /orders/:id, /orders/:id/cancel, /positions, /account)
// are also registered for backward compat.
func (h *ExecutionHandler) RegisterRoutes(router *gin.Engine) {
	execGroup := router.Group("/api/execution")
	{
		execGroup.POST("/orders", h.createOrder)
		execGroup.GET("/orders", h.listOrders)
		execGroup.GET("/orders/:id", h.getOrder)
		execGroup.POST("/orders/:id/cancel", h.cancelOrder)
		execGroup.GET("/positions", h.getPositions)
		execGroup.GET("/account", h.getAccount)
		// P2-3 (ODR-026): kill-switch endpoint. The token is
		// checked inline (see emergencyFlattenHandler); the route
		// is registered even when the token is empty so a
		// misconfigured token returns 503 instead of 404.
		execGroup.POST("/emergency-flatten", h.emergencyFlattenHandler)
	}

	// Legacy compatibility routes (no /api/execution prefix).
	router.POST("/orders", h.createOrder)
	router.GET("/orders", h.listOrders)
	router.GET("/orders/:id", h.getOrder)
	router.POST("/orders/:id/cancel", h.cancelOrder)
	router.GET("/positions", h.getPositions)
	router.GET("/account", h.getAccount)
}

// createOrderRequest mirrors the original cmd/execution request body.
type createOrderRequest struct {
	Symbol    string           `json:"symbol" binding:"required"`
	Direction domain.Direction `json:"side" binding:"required"`
	OrderType domain.OrderType `json:"type"`
	Quantity  float64          `json:"quantity" binding:"required,gt=0"`
	Price     float64          `json:"price"`
}

func (h *ExecutionHandler) createOrder(c *gin.Context) {
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.OrderType == "" {
		req.OrderType = domain.OrderTypeMarket
	}
	if req.Direction == "" {
		req.Direction = domain.DirectionLong
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := h.trader.SubmitOrder(ctx, req.Symbol, req.Direction, req.OrderType, req.Quantity, req.Price)
	if err != nil {
		h.logger.Warn().Err(err).Str("symbol", req.Symbol).Msg("submit order rejected")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  err.Error(),
			"status": "rejected",
		})
		return
	}

	h.mu.Lock()
	h.orders[result.OrderID] = result
	h.mu.Unlock()

	c.JSON(http.StatusCreated, result)
}

func (h *ExecutionHandler) listOrders(c *gin.Context) {
	h.mu.RLock()
	orders := make([]*live.OrderResult, 0, len(h.orders))
	for _, o := range h.orders {
		orders = append(orders, o)
	}
	h.mu.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"orders": orders,
		"count":  len(orders),
	})
}

func (h *ExecutionHandler) getOrder(c *gin.Context) {
	id := c.Param("id")

	h.mu.RLock()
	cached, ok := h.orders[id]
	h.mu.RUnlock()

	if ok {
		c.JSON(http.StatusOK, cached)
		return
	}

	// Fallback: ask the trader directly. Useful for orders
	// that arrived through another path (e.g. engine bridge) and
	// weren't cached by the handler.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	result, err := h.trader.GetOrder(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ExecutionHandler) cancelOrder(c *gin.Context) {
	id := c.Param("id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := h.trader.CancelOrder(ctx, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	if o, ok := h.orders[id]; ok {
		o.Status = "cancelled"
	}
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"order_id": id,
		"status":   "cancelled",
	})
}

func (h *ExecutionHandler) getPositions(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	positions, err := h.trader.GetPositions(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"positions": positions,
		"count":     len(positions),
	})
}

func (h *ExecutionHandler) getAccount(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	account, err := h.trader.GetAccount(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, account)
}

// emergencyFlattenRequest is the body of POST /api/execution/emergency-flatten.
// Reason is mandatory for audit (the operator must type a justification).
// ConfirmationToken must match the server-side configured token — the
// second factor is to prevent accidental button-presses from killing
// the portfolio.
type emergencyFlattenRequest struct {
	Reason           string `json:"reason" binding:"required"`
	ConfirmationToken string `json:"confirmation_token" binding:"required"`
}

// emergencyFlattenResponse mirrors live.EmergencyFlattenResult with
// the audit fields added.
type emergencyFlattenResponse struct {
	Sold         []live.EmergencyFlattenOrder `json:"sold"`
	Skipped      []live.EmergencyFlattenSkip  `json:"skipped"`
	SoldTotal    float64                      `json:"sold_total"`
	StartedAt    time.Time                    `json:"started_at"`
	CompletedAt  time.Time                    `json:"completed_at"`
	Reason       string                       `json:"reason"`
	LatencyMS    int64                        `json:"latency_ms"`
}

// emergencyFlattenHandler implements the kill-switch endpoint
// (P2-3, ODR-026). It requires:
//
//  1. A non-empty server-side `emergencyToken` (configured via
//     `trading.emergency_token`); otherwise the endpoint is
//     disabled and returns 503.
//  2. A bearer token in the `Authorization: Bearer <token>` header
//     that matches the server-side token (constant-time compare
//     via crypto/subtle).
//  3. A JSON body with `reason` (audit) and `confirmation_token`
//     (the operator must type the same token again — defence in
//     depth against accidental button presses).
//
// On success, returns 200 with the result. On auth failure,
// returns 401/403. On trader failure, returns 500.
//
// The handler is intentionally permissive about the trader's
// per-symbol failures: the trader returns a structured
// EmergencyFlattenResult that already separates Sold from Skipped;
// the handler just relays it.
func (h *ExecutionHandler) emergencyFlattenHandler(c *gin.Context) {
	if h.emergencyToken == "" {
		// Endpoint disabled by configuration. Return 503 (not
		// 404) so the operator knows the server is up but the
		// kill switch is intentionally not wired.
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "emergency flatten endpoint disabled (trading.emergency_token not configured)",
			"detail": "set trading.emergency_token in config/analysis-service.yaml to enable",
		})
		return
	}

	// Header bearer check.
	authHeader := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		c.Header("WWW-Authenticate", `Bearer realm="emergency-flatten"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed Authorization header"})
		return
	}
	gotToken := strings.TrimPrefix(authHeader, prefix)
	if subtle.ConstantTimeCompare([]byte(gotToken), []byte(h.emergencyToken)) != 1 {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid bearer token"})
		return
	}

	// Body parse.
	var req emergencyFlattenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Confirmation token check (defence in depth — operator must
	// re-type the token to prevent accidental triggers).
	if subtle.ConstantTimeCompare([]byte(req.ConfirmationToken), []byte(h.emergencyToken)) != 1 {
		c.JSON(http.StatusForbidden, gin.H{"error": "confirmation_token mismatch"})
		return
	}

	if req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required for audit"})
		return
	}

	// Hard cap on the call duration. Emergency flatten itself
	// should complete in < 1s for a typical portfolio; 30s is a
	// safety net for pathological cases (large portfolio, slow
	// broker).
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	result, err := h.trader.EmergencyFlatten(ctx, req.Reason)
	if err != nil {
		h.logger.Error().Err(err).Msg("emergency flatten: trader error")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "trader failed to flatten",
			"detail": err.Error(),
		})
		return
	}

	resp := emergencyFlattenResponse{
		Sold:        result.Sold,
		Skipped:     result.Skipped,
		SoldTotal:   result.SoldTotal,
		StartedAt:   result.StartedAt,
		CompletedAt: result.CompletedAt,
		Reason:      result.Reason,
		LatencyMS:   result.CompletedAt.Sub(result.StartedAt).Milliseconds(),
	}
	c.JSON(http.StatusOK, resp)
}
