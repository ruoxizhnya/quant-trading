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
	Server      ServerConfig `mapstructure:"server"`
	Redis       RedisConfig  `mapstructure:"redis"`
	DataService DataServiceConfig `mapstructure:"data_service"`
	Logging     LoggingConfig `mapstructure:"logging"`
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
	StockPool    []string                       `json:"stock_pool" binding:"required"`
	Date         string                         `json:"date" binding:"required"`
	LookbackDays int                            `json:"lookback_days"`
	MarketData   map[string][]domain.OHLCV     `json:"market_data"`
}

// SignalResponse represents the response for signal generation.
type SignalResponse struct {
	Strategy string           `json:"strategy"`
	Date     string           `json:"date"`
	Signals  []SignalDetail   `json:"signals"`
	Count    int              `json:"count"`
}

// SignalDetail represents detailed signal information.
type SignalDetail struct {
	Symbol          string             `json:"symbol"`
	Date            time.Time          `json:"date"`
	Direction       domain.Direction   `json:"direction"`
	Strength        float64            `json:"strength"`
	CompositeScore  float64            `json:"composite_score"`
	Factors         map[string]float64 `json:"factors,omitempty"`
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
	strategy.Init(logger)

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
	// Register momentum strategy
	strategy.Register("momentum", func() domain.Strategy {
		s := examples.NewMomentumStrategy()
		defaultConfig := map[string]any{
			"lookback_days":        20,
			"long_threshold":        0.0,
			"short_threshold":       0.0,
			"top_n":                5,
			"max_positions":        5,
			"rebalance_frequency":  "weekly",
		}
		if err := s.Configure(defaultConfig); err != nil {
			logger.Error().Err(err).Msg("failed to configure momentum strategy")
		}
		return s
	})

	// Register value_momentum strategy
	strategy.Register("value_momentum", func() domain.Strategy {
		s := examples.NewValueMomentumStrategy()
		// Apply default configuration
		defaultConfig := map[string]any{
			"factors": map[string]any{
				"pe": map[string]any{
					"weight":                0.25,
					"percentile_threshold":  0.3,
				},
				"pb": map[string]any{
					"weight":                0.25,
					"percentile_threshold":  0.3,
				},
				"momentum": map[string]any{
					"weight":       0.25,
					"lookback_days": 20,
				},
				"quality": map[string]any{
					"weight":        0.25,
					"roe_threshold": 0.15,
				},
			},
			"filter": map[string]any{
				"top_mcap_percentile":  0.8,
				"require_positive_pe":  true,
				"require_positive_roe": true,
			},
			"signal": map[string]any{
				"long_threshold":  0.3,
				"short_threshold": -0.3,
				"max_positions":   20,
			},
		}
		if err := s.Configure(defaultConfig); err != nil {
			logger.Error().Err(err).Msg("failed to configure value_momentum strategy")
		}
		return s
	})

	logger.Info().Strs("strategies", strategy.ListStrategies()).Msg("strategies registered")
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

			// Get strategy
			s, err := strategy.GetStrategy(name)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": fmt.Sprintf("strategy not found: %s", name),
				})
				return
			}

			// Parse request
			var req SignalRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("invalid request: %v", err),
				})
				return
			}

			// Parse date
			date, err := time.Parse("2006-01-02", req.Date)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("invalid date format, expected YYYY-MM-DD: %v", err),
				})
				return
			}

			// Create context with timeout
			ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
			defer cancel()

			// Build stock list from pool
			stocks := make([]domain.Stock, 0, len(req.StockPool))
			for _, symbol := range req.StockPool {
				stocks = append(stocks, domain.Stock{
					Symbol:    symbol,
					MarketCap: 1_000_000_000, // Placeholder - would come from data service
				})
			}

			// Use market_data from request if provided, otherwise empty
			ohlcv := req.MarketData
			if ohlcv == nil {
				ohlcv = make(map[string][]domain.OHLCV)
			}
			fundamental := make(map[string][]domain.Fundamental)

			// Override lookback days if provided in request
			if req.LookbackDays > 0 {
				overrideConfig := map[string]any{"lookback_days": req.LookbackDays}
				if err := s.Configure(overrideConfig); err != nil {
					logger.Warn().Err(err).Msg("failed to override lookback days")
				}
			}

			// Generate signals
			signals, err := s.Signals(ctx, stocks, ohlcv, fundamental, date)
			if err != nil {
				logger.Error().Err(err).Str("strategy", name).Msg("signal generation failed")
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("signal generation failed: %v", err),
				})
				return
			}

			// Convert signals to response format
			signalDetails := make([]SignalDetail, 0, len(signals))
			for _, sig := range signals {
				signalDetails = append(signalDetails, SignalDetail{
					Symbol:         sig.Symbol,
					Date:           sig.Date,
					Direction:      sig.Direction,
					Strength:       sig.Strength,
					CompositeScore: sig.CompositeScore,
					Factors:        sig.Factors,
				})
			}

			c.JSON(http.StatusOK, SignalResponse{
				Strategy: name,
				Date:     req.Date,
				Signals:  signalDetails,
				Count:    len(signalDetails),
			})
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
