// Package main is the entry point for the Analysis Service.
package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
)

func main() {
	// Initialize logger
	logger := initLogger()

	// Load configuration
	v := viper.New()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/analysis-service.yaml"
	}
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to read config file")
	}

	// Configure zerolog from config
	logLevel := v.GetString("logging.level")
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Initialize backtest engine
	engine, err := backtest.NewEngine(v, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize backtest engine")
	}

	// Setup Gin router
	if v.GetString("logging.format") == "json" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))

	// Register routes
	registerRoutes(router, engine, logger)

	// Get server config
	host := v.GetString("server.host")
	port := v.GetInt("server.port")
	addr := fmt.Sprintf("%s:%d", host, port)

	// Create HTTP server
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info().
			Str("address", addr).
			Msg("Analysis Service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Server failed")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	logger.Info().Msg("Server exited")
}

// initLogger initializes the zerolog logger.
func initLogger() zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	return log.Output(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05.000",
	})
}

// requestLogger returns a Gin middleware for logging requests.
func requestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		event := logger.Info()
		if status >= 400 && status < 500 {
			event = logger.Warn()
		} else if status >= 500 {
			event = logger.Error()
		}

		event.
			Str("method", c.Request.Method).
			Str("path", path).
			Str("query", query).
			Int("status", status).
			Dur("latency", latency).
			Str("client_ip", c.ClientIP()).
			Int("body_size", c.Writer.Size()).
			Msg("Request")
	}
}

// registerRoutes registers all HTTP routes.
func registerRoutes(router *gin.Engine, engine *backtest.Engine, logger zerolog.Logger) {
	// Serve UI
	router.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./static/index.html")
	})

	router.GET("/screen", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./static/screen.html")
	})

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "analysis-service",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// OHLCV proxy for UI (avoids CORS issues)
	router.GET("/ohlcv/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		startDate := c.Query("start_date")
		endDate := c.Query("end_date")
		if startDate == "" || endDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date required (YYYYMMDD)"})
			return
		}
		// Proxy to data service
		dataURL := fmt.Sprintf("http://data-service:8081/ohlcv/%s?start_date=%s&end_date=%s", symbol, startDate, endDate)
		resp, err := http.Get(dataURL)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	})

	// Screen proxy for UI (proxies to data service)
	router.POST("/screen", func(c *gin.Context) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(c.Request.Body).Decode(&reqBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		bodyBytes, _ := json.Marshal(reqBody)
		dataURL := "http://data-service:8081/screen"
		resp, err := http.Post(dataURL, "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	})

	// Strategy list endpoint
	router.GET("/api/strategies", func(c *gin.Context) {
		strategies := strategy.DefaultRegistry.ListWithInfo()
		c.JSON(http.StatusOK, gin.H{"strategies": strategies})
	})

	// Backtest endpoints
	api := router.Group("/backtest")
	{
		// Run a backtest
		api.POST("", func(c *gin.Context) {
			var req backtest.BacktestRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "invalid request body",
					"details": err.Error(),
				})
				return
			}

			logger.Info().
				Str("strategy", req.Strategy).
				Str("start_date", req.StartDate).
				Str("end_date", req.EndDate).
				Int("stock_count", len(req.StockPool)).
				Msg("Starting backtest")

			ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
			defer cancel()

			result, err := engine.RunBacktest(ctx, req)
			if err != nil {
				logger.Error().Err(err).Msg("Backtest failed")
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "backtest failed",
					"details": err.Error(),
				})
				return
			}

			logger.Info().
				Str("backtest_id", result.ID).
				Float64("total_return", result.TotalReturn).
				Float64("sharpe_ratio", result.SharpeRatio).
				Msg("Backtest completed")

			c.JSON(http.StatusOK, result)
		})

		// Get backtest report
		api.GET("/:id/report", func(c *gin.Context) {
			backtestID := c.Param("id")

			status, err := engine.GetBacktestStatus(backtestID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": err.Error(),
				})
				return
			}

			if status != "completed" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":  "backtest not completed",
					"status": status,
				})
				return
			}

			result, err := engine.GetBacktestResult(backtestID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, result)
		})

		// Get backtest trades
		api.GET("/:id/trades", func(c *gin.Context) {
			backtestID := c.Param("id")

			trades, err := engine.GetBacktestTrades(backtestID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"backtest_id": backtestID,
				"total":       len(trades),
				"trades":      trades,
			})
		})

		// Get backtest equity curve
		api.GET("/:id/equity", func(c *gin.Context) {
			backtestID := c.Param("id")

			equity, err := engine.GetBacktestEquity(backtestID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"backtest_id": backtestID,
				"total_points": len(equity),
				"equity_curve": equity,
			})
		})
	}

	// API info
	router.GET("/api/v1", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "analysis-service",
			"version": "1.0.0",
			"endpoints": []string{
				"GET  /health",
				"POST /backtest",
				"GET  /backtest/:id/report",
				"GET  /backtest/:id/trades",
				"GET  /backtest/:id/equity",
			},
		})
	})
}
