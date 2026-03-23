// Package main is the entry point for the data service.
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
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func main() {
	// Load configuration
	if err := loadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	logging.Init(
		viper.GetString("logging.level"),
		viper.GetString("logging.format"),
	)

	logger := logging.Logger
	logger.Info().Str("service", "data-service").Msg("Starting data service")

	// Initialize context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize PostgreSQL store
	dbConnString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		viper.GetString("database.user"),
		viper.GetString("database.password"),
		viper.GetString("database.host"),
		viper.GetInt("database.port"),
		viper.GetString("database.database"),
		viper.GetString("database.sslmode"),
	)

	store, err := storage.NewPostgresStore(ctx, dbConnString)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to PostgreSQL")
	}
	defer store.Close()

	// Initialize Redis cache
	cache, err := storage.NewCache(viper.GetString("redis.url"))
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	defer cache.Close()

	// Initialize Tushare client
	tushareClient := data.NewTushareClient(
		viper.GetString("tushare.token"),
		viper.GetString("tushare.base_url"),
		viper.GetInt("tushare.max_retries"),
		store,
		cache,
	)

	// Setup Gin router
	if viper.GetString("logging.level") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// Register routes
	registerRoutes(router, store, cache, tushareClient)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d",
		viper.GetString("server.host"),
		viper.GetInt("server.port"),
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info().Str("addr", addr).Msg("HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("Shutting down data service...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	logger.Info().Msg("Data service stopped")
}

func loadConfig() error {
	viper.SetConfigName("data-service")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("../config")
	viper.AddConfigPath("../../config")

	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8081)
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("tushare.max_retries", 3)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	return nil
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		logger := logging.Logger
		if status >= 500 {
			logger.Error().
				Str("method", c.Request.Method).
				Str("path", path).
				Str("query", query).
				Int("status", status).
				Dur("latency", latency).
				Str("client_ip", c.ClientIP()).
				Msg("Request failed")
		} else if status >= 400 {
			logger.Warn().
				Str("method", c.Request.Method).
				Str("path", path).
				Str("query", query).
				Int("status", status).
				Dur("latency", latency).
				Str("client_ip", c.ClientIP()).
				Msg("Request error")
		} else {
			logger.Info().
				Str("method", c.Request.Method).
				Str("path", path).
				Str("query", query).
				Int("status", status).
				Dur("latency", latency).
				Str("client_ip", c.ClientIP()).
				Msg("Request")
		}
	}
}

func registerRoutes(r *gin.Engine, store *storage.PostgresStore, cache *storage.Cache, tc *data.TushareClient) {
	// Health check
	r.GET("/health", healthHandler(store, cache))

	// Stock endpoints
	r.GET("/stocks", listStocksHandler(store, cache))
	r.GET("/stocks/:symbol", getStockHandler(store, cache))

	// OHLCV endpoints
	r.GET("/ohlcv/:symbol", getOHLCVHandler(store, cache))

	// Fundamental endpoints
	r.GET("/fundamental/:symbol", getFundamentalHandler(store))

	// Index endpoints
	r.GET("/index/:code/constituents", getIndexConstituentsHandler(tc))

	// Trading calendar
	r.GET("/api/v1/trading/calendar", getTradingCalendarHandler(store))

	// Sync endpoints
	r.POST("/sync/stocks", syncStocksHandler(tc))
	r.POST("/sync/ohlcv", syncOHLCVHandler(tc))
	r.POST("/sync/fundamental", syncFundamentalHandler(tc))
}

// Handlers

func healthHandler(store *storage.PostgresStore, cache *storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		if err := store.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "database"})
			return
		}
		if err := cache.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "cache"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	}
}

func listStocksHandler(store *storage.PostgresStore, cache *storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		exchange := c.Query("exchange")

		// Check cache first
		if cached, err := cache.GetCachedStocks(ctx, exchange); err == nil && cached != nil {
			c.JSON(http.StatusOK, gin.H{"stocks": cached, "source": "cache"})
			return
		}

		stocks, err := store.GetStocks(ctx, exchange)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Cache the result
		if len(stocks) > 0 {
			cache.CacheStocks(ctx, exchange, stocks)
		}

		c.JSON(http.StatusOK, gin.H{"stocks": stocks, "source": "database"})
	}
}

func getStockHandler(store *storage.PostgresStore, cache *storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")

		// Check cache first
		if cached, err := cache.GetCachedStock(ctx, symbol); err == nil && cached != nil {
			c.JSON(http.StatusOK, gin.H{"stock": cached, "source": "cache"})
			return
		}

		stock, err := store.GetStock(ctx, symbol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if stock == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "stock not found"})
			return
		}

		// Cache the result
		cache.CacheStock(ctx, stock)

		c.JSON(http.StatusOK, gin.H{"stock": stock, "source": "database"})
	}
}

func getOHLCVHandler(store *storage.PostgresStore, cache *storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		startDate, err := time.Parse("20060102", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		endDate, err := time.Parse("20060102", endDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		// Check cache first
		if cached, err := cache.GetCachedOHLCV(ctx, symbol, startDate, endDate); err == nil && cached != nil {
			c.JSON(http.StatusOK, gin.H{"ohlcv": cached, "source": "cache"})
			return
		}

		ohlcv, err := store.GetOHLCV(ctx, symbol, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Cache the result
		if len(ohlcv) > 0 {
			cache.CacheOHLCV(ctx, symbol, startDate, endDate, ohlcv)
		}

		c.JSON(http.StatusOK, gin.H{"ohlcv": ohlcv, "source": "database"})
	}
}

func getFundamentalHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")
		dateStr := c.Query("date")

		if dateStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date query parameter required"})
			return
		}

		date, err := time.Parse("20060102", dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYYMMDD"})
			return
		}

		fundamental, err := store.GetFundamental(ctx, symbol, date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if fundamental == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "fundamental data not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"fundamental": fundamental})
	}
}

func getIndexConstituentsHandler(tc *data.TushareClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		indexCode := c.Param("code")
		date := c.Query("date")

		constituents, err := tc.FetchIndexConstituents(ctx, indexCode, date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"constituents": constituents})
	}
}

type syncStocksRequest struct {
	Exchange   string `json:"exchange"`
	ListStatus string `json:"list_status"`
}

func syncStocksHandler(tc *data.TushareClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncStocksRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			req.ListStatus = "active"
		}

		stocks, err := tc.FetchStocks(ctx, req.Exchange, req.ListStatus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "stocks synced successfully",
			"count":   len(stocks),
		})
	}
}

type syncOHLCVRequest struct {
	Symbols   []string `json:"symbols"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
}

func syncOHLCVHandler(tc *data.TushareClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncOHLCVRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.Symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbols array is required"})
			return
		}

		totalCount := 0
		for _, symbol := range req.Symbols {
			logging.Logger.Info().Str("symbol", symbol).Str("start", req.StartDate).Str("end", req.EndDate).Msg("fetching OHLCV")
			ohlcv, err := tc.FetchDailyOHLCV(ctx, symbol, req.StartDate, req.EndDate)
			logging.Logger.Info().Err(err).Str("symbol", symbol).Int("count", len(ohlcv)).Msg("OHLCV fetch result")
			if err != nil {
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync OHLCV")
				continue
			}
			totalCount += len(ohlcv)
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "OHLCV synced successfully",
			"count":   totalCount,
		})
	}
}

type syncFundamentalRequest struct {
	Symbols []string `json:"symbols"`
	Date    string   `json:"date"`
}

func syncFundamentalHandler(tc *data.TushareClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncFundamentalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.Symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbols array is required"})
			return
		}
		if req.Date == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date is required"})
			return
		}

		totalCount := 0
		for _, symbol := range req.Symbols {
			fundamentals, err := tc.FetchFundamentals(ctx, symbol, req.Date)
			if err != nil {
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync fundamentals")
				continue
			}
			totalCount += len(fundamentals)
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Fundamentals synced successfully",
			"count":   totalCount,
		})
	}
}

func getTradingCalendarHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		startStr := c.Query("start")
		endStr := c.Query("end")

		if startStr == "" || endStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start and end query params required"})
			return
		}

		startDate, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start date format, use YYYY-MM-DD"})
			return
		}
		endDate, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end date format, use YYYY-MM-DD"})
			return
		}

		days, err := store.GetTradingDays(ctx, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		dayStrs := make([]string, len(days))
		for i, d := range days {
			dayStrs[i] = d.Format("2006-01-02")
		}
		c.JSON(http.StatusOK, gin.H{"trading_days": dayStrs})
	}
}
