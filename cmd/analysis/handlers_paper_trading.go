package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// PaperTradingHandler handles paper trading HTTP requests
type PaperTradingHandler struct {
	engine *live.LiveEngine
	config domain.ExecutionConfig
}

// NewPaperTradingHandler creates a new paper trading handler
func NewPaperTradingHandler(config domain.ExecutionConfig) *PaperTradingHandler {
	// Create simulated broker for paper trading
	broker := live.NewSimulatedBroker(config.InitialCapital)
	dataFeed := live.NewSimulatedDataFeed()

	engine := live.NewLiveEngine(broker, dataFeed, config)

	return &PaperTradingHandler{
		engine: engine,
		config: config,
	}
}

// registerPaperTradingRoutes registers paper trading routes with the router
func registerPaperTradingRoutes(router *gin.Engine, config domain.ExecutionConfig) {
	handler := NewPaperTradingHandler(config)
	api := router.Group("/api")
	handler.RegisterPaperTradingRoutes(api)
}

// RegisterPaperTradingRoutes registers paper trading routes
func (h *PaperTradingHandler) RegisterPaperTradingRoutes(router *gin.RouterGroup) {
	paper := router.Group("/paper")
	{
		paper.POST("/start", h.StartPaperTrading)
		paper.POST("/stop", h.StopPaperTrading)
		paper.GET("/status", h.GetPaperTradingStatus)
		paper.POST("/orders", h.SubmitOrder)
		paper.GET("/orders", h.GetOrders)
		paper.GET("/orders/:id", h.GetOrder)
		paper.DELETE("/orders/:id", h.CancelOrder)
		paper.GET("/positions", h.GetPositions)
		paper.GET("/portfolio", h.GetPortfolio)
		paper.GET("/trades", h.GetTrades)
	}
}

// StartPaperTradingRequest represents a request to start paper trading
type StartPaperTradingRequest struct {
	Symbols        []string `json:"symbols" binding:"required" example:"[\"000001.SZ\",\"600000.SH\"]"`
	InitialCapital float64  `json:"initial_capital" example:"1000000"`
}

// StartPaperTrading starts paper trading session
// @Summary      Start Paper Trading
// @Description  Start a new paper trading session with simulated broker
// @Description  启动新的模拟交易会话
// @Tags         Paper Trading
// @Accept       json
// @Produce      json
// @Param        request  body      StartPaperTradingRequest  true  "Start paper trading request"
// @Success      200      {object}  map[string]interface{}    "Paper trading started"
// @Failure      400      {object}  map[string]string         "Invalid request"
// @Failure      500      {object}  map[string]string         "Internal server error"
// @Router       /paper/start [post]
func (h *PaperTradingHandler) StartPaperTrading(c *gin.Context) {
	var req StartPaperTradingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.InitialCapital > 0 {
		h.config.InitialCapital = req.InitialCapital
	}

	ctx := context.Background()
	if err := h.engine.Start(ctx, req.Symbols); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":          "started",
		"initial_capital": h.config.InitialCapital,
		"symbols":         req.Symbols,
		"started_at":      time.Now().Format(time.RFC3339),
	})
}

// StopPaperTrading stops paper trading session
// @Summary      Stop Paper Trading
// @Description  Stop the current paper trading session
// @Description  停止当前模拟交易会话
// @Tags         Paper Trading
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Paper trading stopped"
// @Failure      500  {object}  map[string]string       "Internal server error"
// @Router       /paper/stop [post]
func (h *PaperTradingHandler) StopPaperTrading(c *gin.Context) {
	// Get current symbols from engine
	if err := h.engine.Stop([]string{}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "stopped",
		"stopped_at": time.Now().Format(time.RFC3339),
	})
}

// GetPaperTradingStatus returns paper trading status
// @Summary      Get Paper Trading Status
// @Description  Get the current status of paper trading session
// @Description  获取模拟交易会话的当前状态
// @Tags         Paper Trading
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Paper trading status"
// @Router       /paper/status [get]
func (h *PaperTradingHandler) GetPaperTradingStatus(c *gin.Context) {
	isRunning := h.engine.IsRunning()

	c.JSON(http.StatusOK, gin.H{
		"running":         isRunning,
		"initial_capital": h.config.InitialCapital,
	})
}

// SubmitOrderRequest represents a request to submit an order
type SubmitOrderRequest struct {
	Symbol     string  `json:"symbol" binding:"required" example:"000001.SZ"`
	Direction  string  `json:"direction" binding:"required" example:"long"`
	Quantity   float64 `json:"quantity" binding:"required,gt=0" example:"100"`
	OrderType  string  `json:"order_type" example:"market"`
	LimitPrice float64 `json:"limit_price,omitempty" example:"10.5"`
}

// SubmitOrder submits a new order
// @Summary      Submit Order
// @Description  Submit a new order for paper trading
// @Description  提交新的模拟交易订单
// @Tags         Paper Trading
// @Accept       json
// @Produce      json
// @Param        request  body      SubmitOrderRequest      true  "Order request"
// @Success      200      {object}  map[string]interface{}  "Order submitted"
// @Failure      400      {object}  map[string]string       "Invalid request"
// @Failure      500      {object}  map[string]string       "Internal server error"
// @Router       /paper/orders [post]
func (h *PaperTradingHandler) SubmitOrder(c *gin.Context) {
	var req SubmitOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	order := domain.Order{
		Symbol:     req.Symbol,
		Direction:  domain.Direction(req.Direction),
		Quantity:   req.Quantity,
		OrderType:  domain.OrderType(req.OrderType),
		LimitPrice: req.LimitPrice,
		Timestamp:  time.Now(),
	}

	// Use order manager directly through engine
	orderID, err := h.engine.SubmitOrder(order)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"status":   "submitted",
	})
}

// GetOrders returns all orders
// @Summary      List Orders
// @Description  Get all orders in the paper trading session
// @Description  获取模拟交易会话中的所有订单
// @Tags         Paper Trading
// @Produce      json
// @Success      200  {array}   domain.Order  "List of orders"
// @Router       /paper/orders [get]
func (h *PaperTradingHandler) GetOrders(c *gin.Context) {
	orders := h.engine.GetOrders()
	c.JSON(http.StatusOK, orders)
}

// GetOrder returns a specific order
// @Summary      Get Order
// @Description  Get a specific order by ID
// @Description  获取特定订单
// @Tags         Paper Trading
// @Produce      json
// @Param        id   path      string            true  "Order ID"
// @Success      200  {object}  domain.Order      "Order details"
// @Failure      404  {object}  map[string]string "Order not found"
// @Router       /paper/orders/{id} [get]
func (h *PaperTradingHandler) GetOrder(c *gin.Context) {
	orderID := c.Param("id")
	// Access order manager through engine
	order, found := h.engine.GetOrder(orderID)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, order)
}

// CancelOrder cancels an order
// @Summary      Cancel Order
// @Description  Cancel a pending order
// @Description  取消待处理订单
// @Tags         Paper Trading
// @Produce      json
// @Param        id   path      string                  true  "Order ID"
// @Success      200  {object}  map[string]interface{}  "Order cancelled"
// @Failure      404  {object}  map[string]string       "Order not found"
// @Failure      500  {object}  map[string]string       "Internal server error"
// @Router       /paper/orders/{id} [delete]
func (h *PaperTradingHandler) CancelOrder(c *gin.Context) {
	orderID := c.Param("id")
	if err := h.engine.CancelOrder(orderID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "order_id": orderID})
}

// GetPositions returns current positions
// @Summary      Get Positions
// @Description  Get current positions in the paper trading portfolio
// @Description  获取模拟交易组合中的当前持仓
// @Tags         Paper Trading
// @Produce      json
// @Success      200  {array}   domain.Position  "List of positions"
// @Router       /paper/positions [get]
func (h *PaperTradingHandler) GetPositions(c *gin.Context) {
	positions := h.engine.GetPositions()
	c.JSON(http.StatusOK, positions)
}

// GetPortfolio returns portfolio summary
// @Summary      Get Portfolio
// @Description  Get portfolio summary including cash, positions, and total value
// @Description  获取组合摘要，包括现金、持仓和总价值
// @Tags         Paper Trading
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Portfolio summary"
// @Router       /paper/portfolio [get]
func (h *PaperTradingHandler) GetPortfolio(c *gin.Context) {
	portfolio := h.engine.GetPortfolio()
	c.JSON(http.StatusOK, portfolio)
}

// GetTrades returns all trades
// @Summary      Get Trades
// @Description  Get all executed trades in the paper trading session
// @Description  获取模拟交易会话中的所有已执行交易
// @Tags         Paper Trading
// @Produce      json
// @Success      200  {array}   domain.Trade  "List of trades"
// @Router       /paper/trades [get]
func (h *PaperTradingHandler) GetTrades(c *gin.Context) {
	// Get trades from order manager
	trades := h.engine.GetTrades()
	c.JSON(http.StatusOK, trades)
}
