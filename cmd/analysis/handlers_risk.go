package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
)

// P1-15 (Sprint 6, ODR-021): risk service merged into analysis
// service. The endpoints below are 1:1 byte-compatible with the
// original cmd/risk/main.go routes (HTTP method, path, request body
// shape, response shape). Any existing client that talks to
// `risk-service:8083/{calculate_position,detect_regime,check_stoploss,risk_metrics}`
// can be retargeted at `analysis-service:8085/api/risk/*` with zero
// code change on the client side (paths add an `/api` prefix and
// the path is the same).

// RiskHandler serves risk-related HTTP endpoints from the analysis
// service, backed by an in-process *risk.RiskManager. The risk
// manager is also injected into the backtest engine via
// WithRiskManager (see cmd/analysis/main.go) so the engine never
// makes an HTTP call to itself.
type RiskHandler struct {
	manager *risk.RiskManager
	logger  zerolog.Logger
}

// NewRiskHandler constructs a RiskHandler. The manager MUST be the
// same instance that was injected into the backtest engine so the
// HTTP layer and the engine layer stay in lockstep.
func NewRiskHandler(manager *risk.RiskManager, logger zerolog.Logger) *RiskHandler {
	return &RiskHandler{
		manager: manager,
		logger:  logger.With().Str("component", "risk_handler").Logger(),
	}
}

// RegisterRoutes wires the risk endpoints under /api/risk on the
// supplied router group. The legacy root-level paths
// (/calculate_position, /detect_regime, /check_stoploss, /risk_metrics)
// are also registered for backward compat — a client retargeted to
// the analysis service at the same port but with the old paths will
// still work.
func (h *RiskHandler) RegisterRoutes(router *gin.Engine) {
	riskGroup := router.Group("/api/risk")
	{
		riskGroup.POST("/calculate_position", h.calculatePosition)
		riskGroup.POST("/detect_regime", h.detectRegime)
		riskGroup.POST("/check_stoploss", h.checkStopLoss)
		riskGroup.GET("/metrics", h.riskMetrics)
	}

	// Legacy compatibility routes — original cmd/risk/main.go
	// registered them at the root, not under /api/risk.
	router.POST("/calculate_position", h.calculatePosition)
	router.POST("/detect_regime", h.detectRegime)
	router.POST("/check_stoploss", h.checkStopLoss)
	router.GET("/risk_metrics", h.riskMetrics)
}

// calculatePositionRequest mirrors the original cmd/risk request
// shape. Signal / Portfolio / Regime / OHLCV are passed through
// to risk.RiskManager.CalculatePosition unchanged.
type calculatePositionRequest struct {
	Signal       domain.Signal       `json:"signal"`
	Portfolio    domain.Portfolio    `json:"portfolio"`
	Regime       *domain.MarketRegime `json:"regime"`
	CurrentPrice float64             `json:"current_price"`
	OHLCV        []domain.OHLCV      `json:"ohlcv,omitempty"`
}

type calculatePositionResponse struct {
	Size       float64 `json:"size"`
	Weight     float64 `json:"weight"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
	RiskScore  float64 `json:"risk_score"`
}

// calculatePosition computes position size for a single signal.
// Equivalent to POST /calculate_position on the legacy risk service.
func (h *RiskHandler) calculatePosition(c *gin.Context) {
	var req calculatePositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	ps, err := h.manager.CalculatePosition(ctx, req.Signal, &req.Portfolio, req.Regime, req.CurrentPrice, req.OHLCV)
	if err != nil {
		h.logger.Warn().Err(err).Str("symbol", req.Signal.Symbol).Msg("calculate_position failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, calculatePositionResponse{
		Size:       ps.Size,
		Weight:     ps.Weight,
		StopLoss:   ps.StopLoss,
		TakeProfit: ps.TakeProfit,
		RiskScore:  ps.RiskScore,
	})
}

// detectRegimeRequestData is the new format used by the backtest
// engine — passes OHLCV directly so the risk manager can compute
// the regime without re-fetching from data-service.
type detectRegimeRequestData struct {
	Data []domain.OHLCV `json:"data"`
}

func (h *RiskHandler) detectRegime(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	var dataReq detectRegimeRequestData
	if err := c.ShouldBindJSON(&dataReq); err == nil && len(dataReq.Data) > 0 {
		regime, err := h.manager.DetectRegime(ctx, dataReq.Data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, regime)
		return
	}

	// Legacy fallback — old format accepted symbol + lookback_days
	// and risk-service generated mock OHLCV. The merged service
	// intentionally does NOT keep the mock generator: callers
	// should send real OHLCV. We return 400 to signal the schema
	// change is now strict.
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "data field is required (legacy mock-OHLCV generator removed in P1-15)",
	})
}

// checkStopLossRequest mirrors the original cmd/risk request shape.
type checkStopLossRequest struct {
	Positions []domain.Position  `json:"positions"`
	Prices    map[string]float64 `json:"prices"`
}

func (h *RiskHandler) checkStopLoss(c *gin.Context) {
	var req checkStopLossRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	events, err := h.manager.CheckStopLoss(ctx, req.Positions, req.Prices)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
	})
}

type riskMetricsResponse struct {
	PortfolioVolatility float64   `json:"portfolio_volatility"`
	TargetVolatility    float64   `json:"target_volatility"`
	MaxPositionWeight   float64   `json:"max_position_weight"`
	MinPositionWeight   float64   `json:"min_position_weight"`
	Timestamp           time.Time `json:"timestamp"`
}

func (h *RiskHandler) riskMetrics(c *gin.Context) {
	cfg := h.manager.GetConfig()
	c.JSON(http.StatusOK, riskMetricsResponse{
		PortfolioVolatility: cfg.TargetVolatility,
		TargetVolatility:    cfg.TargetVolatility,
		MaxPositionWeight:   cfg.MaxPositionWeight,
		MinPositionWeight:   cfg.MinPositionWeight,
		Timestamp:           time.Now().UTC(),
	})
}
