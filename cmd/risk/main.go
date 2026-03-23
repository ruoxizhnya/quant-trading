package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
)

// Config holds the application configuration.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Risk     RiskConfig      `mapstructure:"risk"`
	Volatility VolatilityConfig `mapstructure:"volatility"`
	Regime   RegimeConfig    `mapstructure:"regime"`
	Redis    RedisConfig     `mapstructure:"redis"`
	DataService DataServiceConfig `mapstructure:"data_service"`
	Logging  LoggingConfig   `mapstructure:"logging"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type RiskConfig struct {
	TargetVolatility  float64         `mapstructure:"target_volatility"`
	MaxPositionWeight float64         `mapstructure:"max_position_weight"`
	MinPositionWeight float64         `mapstructure:"min_position_weight"`
	Stoploss          StoplossConfig  `mapstructure:"stoploss"`
	TakeProfit        TakeProfitConfig `mapstructure:"take_profit"`
}

type StoplossConfig struct {
	ATRPeriod          int     `mapstructure:"atr_period"`
	BaseMultiplier     float64 `mapstructure:"base_multiplier"`
	BullMultiplier     float64 `mapstructure:"bull_multiplier"`
	BearMultiplier     float64 `mapstructure:"bear_multiplier"`
	SidewaysMultiplier float64 `mapstructure:"sideways_multiplier"`
}

type TakeProfitConfig struct {
	ATRMultiplier float64 `mapstructure:"atr_multiplier"`
}

type VolatilityConfig struct {
	LookbackDays        int     `mapstructure:"lookback_days"`
	AnnualizationFactor float64 `mapstructure:"annualization_factor"`
}

type RegimeConfig struct {
	FastMAPeriod  int `mapstructure:"fast_ma_period"`
	SlowMAPeriod  int `mapstructure:"slow_ma_period"`
	VolLookback   int `mapstructure:"vol_lookback"`
}

type RedisConfig struct {
	URL string `mapstructure:"url"`
}

type DataServiceConfig struct {
	URL string `mapstructure:"url"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

var (
	riskManager *risk.RiskManager
	config     Config
)

func main() {
	// Initialize logger
	initLogger()

	// Load configuration
	if err := loadConfig(); err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Initialize risk manager
	if err := initRiskManager(); err != nil {
		log.Fatal().Err(err).Msg("failed to initialize risk manager")
	}

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// Register routes
	registerRoutes(router)

	// Start server
	addr := fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Info().Msg("shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("server shutdown error")
		}
	}()

	log.Info().Str("address", addr).Msg("starting risk service")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server failed")
	}

	log.Info().Msg("server stopped")
}

func initLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
}

func loadConfig() error {
	viper.SetConfigName("risk-service")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("../config")
	viper.AddConfigPath("../../config")

	// Environment variable support
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if err := viper.Unmarshal(&config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Configure zerolog
	level, err := zerolog.ParseLevel(config.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if config.Logging.Format == "json" {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()
	} else {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
			With().Timestamp().Caller().Logger()
	}

	return nil
}

func initRiskManager() error {
	cfg := risk.RiskManagerConfig{
		TargetVolatility:     config.Risk.TargetVolatility,
		MaxPositionWeight:    config.Risk.MaxPositionWeight,
		MinPositionWeight:    config.Risk.MinPositionWeight,
		ATRPeriod:            config.Risk.Stoploss.ATRPeriod,
		BaseMultiplier:       config.Risk.Stoploss.BaseMultiplier,
		BullMultiplier:       config.Risk.Stoploss.BullMultiplier,
		BearMultiplier:       config.Risk.Stoploss.BearMultiplier,
		SidewaysMultiplier:  config.Risk.Stoploss.SidewaysMultiplier,
		TakeProfitMult:       config.Risk.TakeProfit.ATRMultiplier,
		VolLookbackDays:      config.Volatility.LookbackDays,
		AnnualizationFactor:  config.Volatility.AnnualizationFactor,
		FastMAPeriod:         config.Regime.FastMAPeriod,
		SlowMAPeriod:         config.Regime.SlowMAPeriod,
		RegimeVolLookback:    config.Regime.VolLookback,
	}

	rm, err := risk.NewRiskManager(cfg, log.Logger)
	if err != nil {
		return err
	}

	riskManager = rm
	return nil
}

func registerRoutes(r *gin.Engine) {
	r.GET("/health", healthHandler)
	r.POST("/calculate_position", calculatePositionHandler)
	r.POST("/detect_regime", detectRegimeHandler)
	r.POST("/check_stoploss", checkStopLossHandler)
	r.GET("/risk_metrics", riskMetricsHandler)
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		log.Info().
			Str("method", method).
			Str("path", path).
			Int("status", status).
			Dur("latency", latency).
			Msg("request completed")
	}
}

// Health handler
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"service":   "risk-service",
		"timestamp": time.Now().UTC(),
	})
}

// CalculatePosition request/response types
type CalculatePositionRequest struct {
	Signal       domain.Signal       `json:"signal"`
	Portfolio    domain.Portfolio     `json:"portfolio"`
	Regime       *domain.MarketRegime `json:"regime"`
	CurrentPrice float64             `json:"current_price"`
}

type CalculatePositionResponse struct {
	Size       float64 `json:"size"`
	Weight     float64 `json:"weight"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
	RiskScore  float64 `json:"risk_score"`
}

func calculatePositionHandler(c *gin.Context) {
	var req CalculatePositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Msg("invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	positionSize, err := riskManager.CalculatePosition(ctx, req.Signal, &req.Portfolio, req.Regime, req.CurrentPrice)
	if err != nil {
		log.Error().Err(err).Msg("position calculation failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate stop loss and take profit if we have ATR data
	// For now, using regime-based defaults
	if req.Regime != nil {
		atrMultiplier := config.Risk.Stoploss.BaseMultiplier
		switch req.Regime.Trend {
		case "bull":
			atrMultiplier = config.Risk.Stoploss.BullMultiplier
		case "bear":
			atrMultiplier = config.Risk.Stoploss.BearMultiplier
		}
		// Estimate entry price from portfolio
		// In production, this would come from actual entry data
		entryPrice := 100.0 // Placeholder
		positionSize.StopLoss = entryPrice - (atrMultiplier * entryPrice * 0.02)
		positionSize.TakeProfit = entryPrice + (config.Risk.TakeProfit.ATRMultiplier * entryPrice * 0.02)
	}

	resp := CalculatePositionResponse{
		Size:       positionSize.Size,
		Weight:     positionSize.Weight,
		StopLoss:   positionSize.StopLoss,
		TakeProfit: positionSize.TakeProfit,
		RiskScore:  positionSize.RiskScore,
	}

	c.JSON(http.StatusOK, resp)
}

// DetectRegime request/response types
type DetectRegimeRequest struct {
	Symbol       string `json:"symbol" binding:"required"`
	LookbackDays int    `json:"lookback_days"`
}

// Alternative format: direct OHLCV data passed from backtest engine
type DetectRegimeDataRequest struct {
	Data []domain.OHLCV `json:"data"`
}

func detectRegimeHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Try new format first (from backtest engine)
	var dataReq DetectRegimeDataRequest
	if err := c.ShouldBindJSON(&dataReq); err == nil && len(dataReq.Data) > 0 {
		regime, err := riskManager.DetectRegime(ctx, dataReq.Data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, regime)
		return
	}

	// Fall back to original format
	var req DetectRegimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Msg("invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	lookbackDays := req.LookbackDays
	if lookbackDays == 0 {
		lookbackDays = 200
	}

	ohlcv := generateMockOHLCV(req.Symbol, lookbackDays)

	regime, err := riskManager.DetectRegime(ctx, ohlcv)
	if err != nil {
		log.Error().Err(err).Str("symbol", req.Symbol).Msg("regime detection failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, regime)
}

// CheckStopLoss request/response types
type CheckStopLossRequest struct {
	Positions []domain.Position          `json:"positions"`
	Prices    map[string]float64         `json:"prices"`
}

func checkStopLossHandler(c *gin.Context) {
	var req CheckStopLossRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Msg("invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	events, err := riskManager.CheckStopLoss(ctx, req.Positions, req.Prices)
	if err != nil {
		log.Error().Err(err).Msg("stop loss check failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
	})
}

// RiskMetrics response
type RiskMetricsResponse struct {
	PortfolioVolatility float64 `json:"portfolio_volatility"`
	TargetVolatility    float64 `json:"target_volatility"`
	MaxPositionWeight   float64 `json:"max_position_weight"`
	MinPositionWeight   float64 `json:"min_position_weight"`
	Timestamp           time.Time `json:"timestamp"`
}

func riskMetricsHandler(c *gin.Context) {
	cfg := riskManager.GetConfig()

	resp := RiskMetricsResponse{
		PortfolioVolatility: cfg.TargetVolatility, // Would calculate from actual portfolio
		TargetVolatility:    cfg.TargetVolatility,
		MaxPositionWeight:   cfg.MaxPositionWeight,
		MinPositionWeight:   cfg.MinPositionWeight,
		Timestamp:           time.Now().UTC(),
	}

	c.JSON(http.StatusOK, resp)
}

// generateMockOHLCV generates mock OHLCV data for testing
func generateMockOHLCV(symbol string, days int) []domain.OHLCV {
	ohlcv := make([]domain.OHLCV, days)
	basePrice := 100.0
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -(days - i - 1))
		
		// Generate somewhat realistic price movements
		trend := float64(i) * 0.1 // Slight upward trend
		noise := (float64(i%7) - 3) * 0.5
		
		closePrice := basePrice + trend + noise
		openPrice := closePrice - 0.5 + float64(i%3)*0.2
		highPrice := mathMax(openPrice, closePrice) + 0.3
		lowPrice := mathMin(openPrice, closePrice) - 0.3

		ohlcv[i] = domain.OHLCV{
			Symbol:    symbol,
			Date:      date,
			Open:      openPrice,
			High:      highPrice,
			Low:       lowPrice,
			Close:     closePrice,
			Volume:    1000000,
			Turnover:  100000000,
			TradeDays: 1,
		}
	}

	return ohlcv
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
