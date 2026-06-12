package main

import (
	"context"
	"net/http"
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

	mu     sync.RWMutex
	orders map[string]*live.OrderResult
}

// NewExecutionHandler constructs an ExecutionHandler. The trader is
// typically a MockTrader; the same instance should also be passed
// to the backtest engine via WithLiveTrader so HTTP-driven paper
// orders are visible to backtest bridge (and vice versa).
func NewExecutionHandler(trader live.LiveTrader, logger zerolog.Logger) *ExecutionHandler {
	return &ExecutionHandler{
		trader: trader,
		logger: logger.With().Str("component", "execution_handler").Logger(),
		orders: make(map[string]*live.OrderResult),
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
