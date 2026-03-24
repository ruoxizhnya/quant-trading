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
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
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
	tushareToken := viper.GetString("tushare.token")
	if tushareToken == "" {
		logger.Warn().Msg("TUSHARE_TOKEN is not set; sync endpoints will fail")
	}
	tushareClient := data.NewTushareClient(
		tushareToken,
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
	r.POST("/sync/calendar", syncCalendarHandler(tc, store))

	// Sync endpoints
	r.POST("/sync/stocks", syncStocksHandler(tc, store))
	r.POST("/sync/ohlcv", syncOHLCVHandler(tc, store))
	r.POST("/sync/ohlcv/all", syncAllOHLCVHandler(tc, store))
	r.POST("/sync/fundamental", syncFundamentalHandler(tc))
	r.POST("/sync/fundamentals", syncFundamentalsHandler(tc, store))

	// Fundamental data endpoints (stock_fundamentals table)
	r.GET("/fundamentals/:symbol", getFundamentalsHandler(store))
	r.GET("/fundamentals/:symbol/history", getFundamentalsHistoryHandler(store))
	r.POST("/screen", screenStocksHandler(store))
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

func syncStocksHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncStocksRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			req.ListStatus = "L"
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

func syncOHLCVHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncOHLCVRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.Symbols) == 0 {
			// Fall back to all stocks from DB
			allStocks, err := store.GetAllStocks(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
				return
			}
			for _, s := range allStocks {
				req.Symbols = append(req.Symbols, s.Symbol)
			}
			logging.Logger.Info().Int("count", len(req.Symbols)).Msg("No symbols provided, fetched all from DB")
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

type syncAllOHLCVRequest struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	BatchSize   int    `json:"batch_size"`
	SkipExisting bool  `json:"skip_existing"`
}

// syncAllOHLCVHandler reads all stocks from DB and syncs OHLCV in batches.
// POST /sync/ohlcv/all
// Runs asynchronously — returns immediately and processes in background.
func syncAllOHLCVHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req syncAllOHLCVRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Defaults
		if req.BatchSize <= 0 {
			req.BatchSize = 10
		}
		if req.EndDate == "" {
			req.EndDate = time.Now().Format("20060102")
		}
		if req.StartDate == "" {
			req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		}

		// Fetch all stocks from DB (non-blocking context)
		ctx := context.Background()
		stocks, err := store.GetAllStocks(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks: " + err.Error()})
			return
		}

		if len(stocks) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no stocks found in DB. Run POST /sync/stocks first."})
			return
		}

		logging.Logger.Info().
			Int("total_stocks", len(stocks)).
			Int("batch_size", req.BatchSize).
			Str("start_date", req.StartDate).
			Str("end_date", req.EndDate).
			Bool("skip_existing", req.SkipExisting).
			Msg("Bulk OHLCV sync started in background")

		// Return immediately — process in background with independent context
		c.JSON(http.StatusAccepted, gin.H{
			"message":      "bulk OHLCV sync started",
			"total_stocks": len(stocks),
		})

		// Background processing
		go func() {
			bgCtx := context.Background()
			totalSynced := 0
			totalSkipped := 0
			totalFailed := 0

			for i := 0; i < len(stocks); i += req.BatchSize {
				end := i + req.BatchSize
				if end > len(stocks) {
					end = len(stocks)
				}
				batch := stocks[i:end]

				for _, stock := range batch {
					if req.SkipExisting {
						hasData, err := store.HasOHLCVData(bgCtx, stock.Symbol)
						if err != nil {
							logging.Logger.Warn().Err(err).Str("symbol", stock.Symbol).Msg("Error checking OHLCV data")
						}
						if hasData {
							totalSkipped++
							logging.Logger.Debug().Str("symbol", stock.Symbol).Msg("Skipping - already has data")
							continue
						}
					}

					logging.Logger.Info().Str("symbol", stock.Symbol).Str("start", req.StartDate).Str("end", req.EndDate).Msg("fetching OHLCV")
					ohlcv, err := tc.FetchDailyOHLCV(bgCtx, stock.Symbol, req.StartDate, req.EndDate)
					if err != nil {
						totalFailed++
						logging.Logger.Warn().Err(err).Str("symbol", stock.Symbol).Msg("Failed to sync OHLCV")
						continue
					}
					totalSynced += len(ohlcv)
					logging.Logger.Info().Str("symbol", stock.Symbol).Int("count", len(ohlcv)).Msg("OHLCV synced")
				}

				logging.Logger.Info().
					Int("batch", (i/req.BatchSize)+1).
					Int("progress", end).
					Int("total", len(stocks)).
					Int("synced", totalSynced).
					Int("skipped", totalSkipped).
					Int("failed", totalFailed).
					Msg("Batch complete")
			}

			logging.Logger.Info().
				Int("total_stocks", len(stocks)).
				Int("records_synced", totalSynced).
				Int("skipped", totalSkipped).
				Int("failed", totalFailed).
				Msg("Bulk OHLCV sync completed")
		}()
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

// ---- Fundamental Data Handlers (stock_fundamentals) ----

type syncFundamentalsRequest struct {
	Symbols []string `json:"symbols"`
	Date    string   `json:"date"` // YYYYMMDD - if provided, fetch for that specific date; otherwise fetch recent
}

func syncFundamentalsHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncFundamentalsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// If no body, use empty request (sync all stocks)
			req = syncFundamentalsRequest{}
		}

		var symbols []string
		if len(req.Symbols) > 0 {
			symbols = req.Symbols
		} else {
			// Fetch all stocks from DB
			allStocks, err := store.GetAllStocks(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
				return
			}
			for _, s := range allStocks {
				symbols = append(symbols, s.Symbol)
			}
			logging.Logger.Info().Int("count", len(symbols)).Msg("No symbols provided, fetched all from DB")
		}

		if len(symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no stocks found in DB. Run POST /sync/stocks first."})
			return
		}

		totalCount := 0
		totalSynced := 0
		totalFailed := 0

		// Process in batches of 10 to respect rate limits
		batchSize := 10
		for i := 0; i < len(symbols); i += batchSize {
			end := i + batchSize
			if end > len(symbols) {
				end = len(symbols)
			}
			batch := symbols[i:end]

			for _, symbol := range batch {
				records, err := tc.FetchFundamentalsData(ctx, symbol, req.Date, req.Date)
				if err != nil {
					totalFailed++
					logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync fundamentals data")
					continue
				}
				totalSynced++
				totalCount += len(records)
			}

			logging.Logger.Info().
				Int("batch", (i/batchSize)+1).
				Int("progress", end).
				Int("total", len(symbols)).
				Msg("Fundamentals batch complete")
		}

		c.JSON(http.StatusOK, gin.H{
			"message":        "Fundamentals synced successfully",
			"stocks_synced":  totalSynced,
			"records_saved":  totalCount,
			"failed_stocks": totalFailed,
		})
	}
}

func getFundamentalsHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")

		fundamental, err := store.GetFundamentalDataLatest(ctx, symbol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if fundamental == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "fundamental data not found for symbol"})
			return
		}

		c.JSON(http.StatusOK, fundamental)
	}
}

func getFundamentalsHistoryHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")
		startStr := c.Query("start_date")
		endStr := c.Query("end_date")

		var startDate, endDate *time.Time
		if startStr != "" {
			t, err := time.Parse("20060102", startStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
				return
			}
			startDate = &t
		}
		if endStr != "" {
			t, err := time.Parse("20060102", endStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
				return
			}
			endDate = &t
		}

		history, err := store.GetFundamentalDataHistory(ctx, symbol, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"history": history})
	}
}

func screenStocksHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req domain.ScreenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var date *time.Time
		if req.Date != "" {
			t, err := time.Parse("20060102", req.Date)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYYMMDD"})
				return
			}
			date = &t
		}

		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}

		results, err := store.ScreenFundamentals(ctx, req.Filters, date, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"count":   len(results),
			"results": results,
		})
	}
}

// ---- End Fundamental Data Handlers ----

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

		days, err := store.GetTradingDates(ctx, startDate, endDate)
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

type syncCalendarRequest struct {
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
	Exchange  string   `json:"exchange"` // "SSE", "SZSE", or "both" (default: "both")
}

func syncCalendarHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req syncCalendarRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// Use defaults if no body provided
			req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
			req.EndDate = time.Now().Format("20060102")
		}

		if req.Exchange == "" {
			req.Exchange = "both"
		}
		if req.StartDate == "" {
			req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		}
		if req.EndDate == "" {
			req.EndDate = time.Now().Format("20060102")
		}

		// Convert YYYYMMDD to YYYY-MM-DD for the handler
		startFormatted := fmt.Sprintf("%s-%s-%s", req.StartDate[:4], req.StartDate[4:6], req.StartDate[6:8])
		endFormatted := fmt.Sprintf("%s-%s-%s", req.EndDate[:4], req.EndDate[4:6], req.EndDate[6:8])

		if _, err := time.Parse("20060102", req.StartDate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		if _, err := time.Parse("20060102", req.EndDate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		ctx := c.Request.Context()

		// Determine which exchanges to sync
		exchanges := []string{req.Exchange}
		if req.Exchange == "both" {
			exchanges = []string{"SSE", "SZSE"}
		}

		var allEntries []storage.TradingCalendarEntry
		exchangeResults := make(map[string]struct {
			count    int
			trading  int
			holidays int
		})

		for _, exchange := range exchanges {
			entries, err := tc.FetchTradingCalendar(ctx, exchange, startFormatted, endFormatted)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch %s calendar from Tushare: %v", exchange, err)})
				return
			}

			tradingCount := 0
			for _, e := range entries {
				if e.IsTradingDay {
					tradingCount++
				}
			}

			exchangeResults[exchange] = struct {
				count    int
				trading  int
				holidays int
			}{
				count:    len(entries),
				trading:  tradingCount,
				holidays: len(entries) - tradingCount,
			}

			allEntries = append(allEntries, entries...)
		}

		if len(allEntries) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "no calendar entries returned from Tushare", "count": 0})
			return
		}

		// Save all entries to database in one batch
		domainEntries := make([]*storage.TradingCalendarEntry, len(allEntries))
		for i := range allEntries {
			domainEntries[i] = &allEntries[i]
		}
		if err := store.SaveTradingCalendarBatch(ctx, domainEntries); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save calendar: " + err.Error()})
			return
		}

		totalCount := 0
		totalTrading := 0
		for _, r := range exchangeResults {
			totalCount += r.count
			totalTrading += r.trading
		}

		c.JSON(http.StatusOK, gin.H{
			"message":          "calendar synced successfully",
			"count":            totalCount,
			"trading_days":     totalTrading,
			"holidays":         totalCount - totalTrading,
			"start_date":       startFormatted,
			"end_date":         endFormatted,
			"exchanges_synced": exchanges,
			"by_exchange":      exchangeResults,
		})
	}
}
