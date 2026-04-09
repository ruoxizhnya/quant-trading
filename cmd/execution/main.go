package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Trading  TradingConfig  `mapstructure:"trading"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type TradingConfig struct {
	InitialCapital float64 `mapstructure:"initial_capital"`
	CommissionRate float64 `mapstructure:"commission_rate"`
	StampTaxRate   float64 `mapstructure:"stamp_tax_rate"`
	SlippageRate   float64 `mapstructure:"slippage_rate"`
	MinCommission  float64 `mapstructure:"min_commission"`
	TransferFeeRate float64 `mapstructure:"transfer_fee_rate"`
}

type RedisConfig struct {
	URL string `mapstructure:"url"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

var (
	trader    live.LiveTrader
	cfg       Config
	ordersMu  sync.RWMutex
	orderStore map[string]*live.OrderResult
)

func main() {
	initLogger()

	if err := loadConfig(); err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
	}

	orderStore = make(map[string]*live.OrderResult)

	trader = live.NewMockTrader(live.MockTraderConfig{
		InitialCash:    cfg.Trading.InitialCapital,
		CommissionRate: cfg.Trading.CommissionRate,
		StampTaxRate:   cfg.Trading.StampTaxRate,
		SlippageRate:   cfg.Trading.SlippageRate,
	}, log.Logger)

	if v := viper.GetString("logging.format"); v == "json" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	registerRoutes(router)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Info().Msg("shutting down execution service...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("server shutdown error")
		}
	}()

	log.Info().Str("address", addr).Msg("starting execution service")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server failed")
	}

	log.Info().Msg("execution service stopped")
}

func initLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
}

func loadConfig() error {
	viper.SetConfigName("execution-service")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("../config")
	viper.AddConfigPath("../../config")

	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8084)
	viper.SetDefault("trading.initial_capital", 1000000.0)
	viper.SetDefault("trading.commission_rate", 0.0003)
	viper.SetDefault("trading.stamp_tax_rate", 0.001)
	viper.SetDefault("trading.slippage_rate", 0.0001)
	viper.SetDefault("trading.min_commission", 5.0)
	viper.SetDefault("trading.transfer_fee_rate", 0.00001)
	viper.SetDefault("logging.level", "info")

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Warn().Err(err).Msg("config file not found, using defaults")
	}

	if err := viper.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	level, err := zerolog.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	return nil
}

func registerRoutes(r *gin.Engine) {
	r.GET("/health", healthHandler)
	r.POST("/orders", createOrderHandler)
	r.GET("/orders", listOrdersHandler)
	r.GET("/orders/:id", getOrderHandler)
	r.POST("/orders/:id/cancel", cancelOrderHandler)
	r.GET("/positions", getPositionsHandler)
	r.GET("/account", getAccountHandler)
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

func healthHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := trader.HealthCheck(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    "unhealthy",
			"service":   "execution-service",
			"error":     err.Error(),
			"timestamp": time.Now().UTC(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"service":   "execution-service",
		"timestamp": time.Now().UTC(),
	})
}

type CreateOrderRequest struct {
	Symbol    string             `json:"symbol" binding:"required"`
	Direction domain.Direction   `json:"side" binding:"required"`
	OrderType domain.OrderType   `json:"type"`
	Quantity  float64            `json:"quantity" binding:"required,gt=0"`
	Price     float64            `json:"price"`
}

func createOrderHandler(c *gin.Context) {
	var req CreateOrderRequest
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

	result, err := trader.SubmitOrder(ctx, req.Symbol, req.Direction, req.OrderType, req.Quantity, req.Price)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   err.Error(),
			"status":  "rejected",
		})
		return
	}

	ordersMu.Lock()
	orderStore[result.OrderID] = result
	ordersMu.Unlock()

	c.JSON(http.StatusCreated, result)
}

func listOrdersHandler(c *gin.Context) {
	ordersMu.RLock()
	defer ordersMu.RUnlock()

	orders := make([]*live.OrderResult, 0, len(orderStore))
	for _, o := range orderStore {
		orders = append(orders, o)
	}

	c.JSON(http.StatusOK, gin.H{
		"orders": orders,
		"count":  len(orders),
	})
}

func getOrderHandler(c *gin.Context) {
	id := c.Param("id")

	ordersMu.RLock()
	order, ok := orderStore[id]
	ordersMu.RUnlock()

	if !ok {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		result, err := trader.GetOrder(ctx, id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}

	c.JSON(http.StatusOK, order)
}

func cancelOrderHandler(c *gin.Context) {
	id := c.Param("id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := trader.CancelOrder(ctx, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ordersMu.Lock()
	if o, ok := orderStore[id]; ok {
		o.Status = "cancelled"
	}
	ordersMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"order_id": id,
		"status":   "cancelled",
	})
}

func getPositionsHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	positions, err := trader.GetPositions(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"positions": positions,
		"count":     len(positions),
	})
}

func getAccountHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	account, err := trader.GetAccount(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, account)
}

func generateOrderID() string {
	return uuid.New().String()
}
