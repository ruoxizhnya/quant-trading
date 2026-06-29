// Package main provides the entry point for the strategy service.
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
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
)

// Config holds the service configuration.
type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Redis       RedisConfig       `mapstructure:"redis"`
	DataService DataServiceConfig `mapstructure:"data_service"`
	Logging     LoggingConfig     `mapstructure:"logging"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// RedisConfig holds Redis configuration.
type RedisConfig struct {
	URL string `mapstructure:"url"`
}

// DataServiceConfig holds data service configuration.
type DataServiceConfig struct {
	URL string `mapstructure:"url"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// SignalRequest represents the request body for generating signals.
type SignalRequest struct {
	StockPool    []string                  `json:"stock_pool" binding:"required"`
	Date         string                    `json:"date" binding:"required"`
	LookbackDays int                       `json:"lookback_days"`
	MarketData   map[string][]domain.OHLCV `json:"market_data"`
}

// SignalResponse represents the response for signal generation.
type SignalResponse struct {
	Strategy string         `json:"strategy"`
	Date     string         `json:"date"`
	Signals  []SignalDetail `json:"signals"`
	Count    int            `json:"count"`
}

// SignalDetail represents detailed signal information.
type SignalDetail struct {
	Symbol         string             `json:"symbol"`
	Date           time.Time          `json:"date"`
	Direction      domain.Direction   `json:"direction"`
	Strength       float64            `json:"strength"`
	CompositeScore float64            `json:"composite_score"`
	Factors        map[string]float64 `json:"factors,omitempty"`
}

func main() {
	// Initialize logger
	logger := initLogger()

	// Load configuration
	config, err := loadConfig(logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Initialize strategy registry
	strategy.OldInit(logger)

	// Register strategies
	registerStrategies(logger)

	logger.Info().
		Str("host", config.Server.Host).
		Int("port", config.Server.Port).
		Msg("starting strategy service")

	// Create Gin router
	if config.Logging.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))

	// Register routes
	registerRoutes(router, logger)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info().Str("address", addr).Msg("HTTP server started")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("server error")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("server forced to shutdown")
	}

	logger.Info().Msg("server exited")
}

// initLogger initializes zerolog logger.
func initLogger() zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339
	return log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})
}

// loadConfig loads configuration from YAML file.
func loadConfig(logger zerolog.Logger) (*Config, error) {
	viper.SetConfigName("strategy-service")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("../config")
	viper.AddConfigPath("../../config")

	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8082)
	viper.SetDefault("redis.url", "redis://localhost:6379")
	viper.SetDefault("data_service.url", "http://localhost:8081")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply logging configuration
	level, err := zerolog.ParseLevel(config.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if config.Logging.Format == "json" {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	logger.Debug().Interface("config", config).Msg("configuration loaded")

	return &config, nil
}

// registerStrategies registers all available strategies.
func registerStrategies(logger zerolog.Logger) {
	// The cmd/strategy service is in standby per ADR-012. ODR-013 P1-25
	// removed the legacy `strategy.Register` / `domain.Strategy` plumbing;
	// we now use the canonical `strategy.GlobalRegister` API.
	_ = logger
	_ = examples.NewMomentumStrategy
	_ = examples.NewValueMomentumStrategy
	logger.Info().Msg("strategy service is in standby (ADR-012) — no strategies auto-registered")
}

// registerRoutes registers all HTTP routes.
func registerRoutes(router *gin.Engine, logger zerolog.Logger) {
	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "strategy-service",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Strategy routes
	strategies := router.Group("/strategies")
	{
		// List all strategies
		strategies.GET("", func(c *gin.Context) {
			names := strategy.ListStrategies()
			infos := make([]gin.H, 0, len(names))

			for _, name := range names {
				info, err := strategy.GetStrategyInfo(name)
				if err != nil {
					continue
				}
				infos = append(infos, gin.H{
					"name":        info.Name,
					"description": info.Description,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"strategies": infos,
				"count":      len(infos),
			})
		})

		// Get strategy info
		strategies.GET("/:name", func(c *gin.Context) {
			name := c.Param("name")

			info, err := strategy.GetStrategyInfo(name)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": fmt.Sprintf("strategy not found: %s", name),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"name":        info.Name,
				"description": info.Description,
			})
		})

		// Generate signals
		strategies.POST("/:name/signals", func(c *gin.Context) {
			name := c.Param("name")
			logger.Debug().Str("strategy", name).Msg("signal generation requested")

			// cmd/strategy is in standby per ADR-012 — return 503 with a
			// descriptive message. The canonical strategy.Strategy interface
			// (used by analysis-service backtest loops) is the active path.
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "strategy service is in standby (ADR-012); use analysis-service backtest API",
			})
			_ = name
		})

		// Hot-reload strategies
		strategies.POST("/reload", func(c *gin.Context) {
			logger.Info().Msg("hot-reload requested")

			if err := strategy.ReloadAllStrategies(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("reload failed: %v", err),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status":     "reloaded",
				"strategies": strategy.ListStrategies(),
			})
		})
	}
}

// requestLogger returns a Gin middleware for request logging.
func requestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		event := logger.Info()
		if status >= 400 {
			event = logger.Warn()
		}
		if status >= 500 {
			event = logger.Error()
		}

		event.
			Str("method", c.Request.Method).
			Str("path", path).
			Str("query", query).
			Int("status", status).
			Dur("latency", latency).
			Str("client_ip", c.ClientIP()).
			Msg("request")
	}
}
