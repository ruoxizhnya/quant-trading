// Package main is the entry point for the data service.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/data/equitydeep"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// main is the composition root for the data service. S7-P2-5 (ODR-043):
// the 1713-line God File was split into focused files — setup.go
// (builders), middleware.go (CORS + rate-limiter), and handlers_*.go
// (domain-grouped HTTP handlers). main() is now a thin orchestrator
// that wires services together and manages the startup/shutdown
// lifecycle.
func main() {
	if err := loadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logging.Init(
		viper.GetString("logging.level"),
		viper.GetString("logging.format"),
	)
	logger := logging.Logger
	logger.Info().Str("service", "data-service").Msg("Starting data service")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := initStore(ctx, logger)
	defer store.Close()

	cache := initCache(logger)
	defer cache.Close()

	dataCache := buildDataCache(cache, store)
	tushareClient := buildTushareClient(store, cache, logger)

	// Multi-source data registry (ADR-016 / ODR-011). Routes that
	// read from non-Tushare sources go through Registry.Fetch.
	dataSourceRegistry := buildDataSourceRegistry(tushareClient, logger)

	router := buildRouter()
	registerRoutes(router, store, cache, tushareClient, dataCache, buildEquityDeepDictionary(logger))
	registerRegistryRoutes(router, newRegistryHandler(dataSourceRegistry))

	srv := startHTTPServer(router, logger)

	sig := waitForShutdown()
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received; beginning graceful drain")

	gracefulShutdown(srv, logger)
}

// registerRoutes wires every data-service HTTP endpoint to its handler.
// Handler functions are defined in handlers_*.go grouped by domain:
//   - handlers_stocks.go      — health, stocks, market index, screen, calendar
//   - handlers_ohlcv.go       — OHLCV get/bulk + cache warming
//   - handlers_fundamentals.go — fundamental/fundamentals READ handlers
//   - handlers_sync.go        — sync stocks/OHLCV/fundamentals/calendar/dividends/splits
//   - handlers_factor.go      — factor + attribution + IC
//   - handlers_equitydeep_ingest.go — C-3 equitydeep snapshot ingest door
//   - sync_handlers.go        — SyncHandler (async job queue + worker pool)
func registerRoutes(r *gin.Engine, store *storage.PostgresStore, cache storage.Cache, tc *data.TushareClient, dc *data.DataCache, equityDeepDict *equitydeep.Dictionary) {
	// Health check
	r.GET("/health", healthHandler(store, cache))

	// L0 ingest write door (ADR-022 §2/§3, TASKS.md L0-1).
	// The only door through which raw source responses enter ingest.raw,
	// hence the only way an external producer (the akshare side) can make
	// its data citable through the Evidence API.
	r.POST("/api/ingest/raw", ingestRawHandler(store))

	// C-3 equitydeep ingest door (TASKS.md EQD-P1-2).
	// Normalizes archived EquityDeep snapshots into fundamentals_detail.
	// The caller quotes the content_hash of the raw response it already
	// archived through the door above; rows are stamped with that hash, so
	// every stored number stays resolvable to the response it came from.
	r.POST("/api/ingest/equitydeep", equityDeepIngestHandler(store, equityDeepDict))

	// Stock endpoints
	r.GET("/stocks", listStocksHandler(store, cache))
	r.GET("/stocks/:symbol", getStockHandler(store, cache))
	r.GET("/stocks/count", stocksCountHandler(store))

	// Market index endpoint (returns sh300, sse, cyb — real-time data via Tushare or empty if unavailable)
	r.GET("/market/index", marketIndexHandler(store))

	// OHLCV endpoints
	r.GET("/ohlcv/:symbol", getOHLCVHandler(dc))
	r.POST("/api/v1/ohlcv/bulk", bulkOHLCVHandler(dc))

	// Fundamental endpoints
	r.GET("/fundamental/:symbol", getFundamentalHandler(store))

	// Index endpoints
	r.GET("/index/:code/constituents", getIndexConstituentsHandler(tc, store))
	r.POST("/sync/index-constituents/:index_code", syncIndexConstituentsHandler(tc))

	// Trading calendar
	r.GET("/api/v1/trading/calendar", getTradingCalendarHandler(store))
	r.POST("/sync/calendar", syncCalendarHandler(tc, store))

	// Initialize sync handler with job queue and worker pool
	syncHandler := NewSyncHandler(store, tc, dc)
	syncHandler.RegisterExecutors()
	syncHandler.StartWorkerPool()

	// Legacy sync endpoints (transformed to async job creation)
	r.POST("/sync/stocks", syncHandler.syncStocksHandler)
	r.POST("/sync/ohlcv", syncHandler.syncOHLCVHandler)
	r.POST("/sync/ohlcv/all", syncHandler.syncAllOHLCVHandler)
	r.POST("/sync/fundamental", syncFundamentalHandler(tc))
	r.POST("/sync/fundamentals", syncHandler.syncFundamentalsHandler)
	r.POST("/sync/dividends", syncHandler.syncDividendsHandler)
	r.POST("/sync/splits", syncHandler.syncSplitsHandler)

	// New REST API for sync job management
	api := r.Group("/api/sync")
	{
		// Job management
		api.GET("/jobs", syncHandler.listJobsHandler)
		api.GET("/jobs/:id", syncHandler.getJobHandler)
		api.POST("/jobs/:id/cancel", syncHandler.cancelJobHandler)
		api.POST("/jobs/:id/retry", syncHandler.retryJobHandler)

		// SSE progress streaming
		api.GET("/jobs/:id/progress", syncHandler.sseProgressHandler)

		// Worker stats
		api.GET("/workers", syncHandler.getWorkerStatsHandler)

		// Schedule management
		api.POST("/schedules", syncHandler.createScheduleHandler)
		api.GET("/schedules", syncHandler.listSchedulesHandler)
		api.GET("/schedules/:id", syncHandler.getScheduleHandler)
		api.PUT("/schedules/:id", syncHandler.updateScheduleHandler)
		api.DELETE("/schedules/:id", syncHandler.deleteScheduleHandler)
		api.POST("/schedules/:id/toggle", syncHandler.toggleScheduleHandler)
		api.POST("/schedules/:id/run", syncHandler.runScheduleNowHandler)
		api.GET("/scheduler/stats", syncHandler.getSchedulerStatsHandler)
	}

	// Cache warming (called by backtest engine before a run)
	r.POST("/api/v1/cache/warm", warmCacheHandler(dc))

	// Factor cache endpoints
	factorComputer := data.NewFactorComputer(store)
	r.GET("/factors/:factor_name", getFactorHandler(store))
	r.POST("/sync/factors/:factor_name", syncFactorHandler(factorComputer))
	r.POST("/sync/factors/all", syncAllFactorsHandler(factorComputer))

	// Factor attribution & IC endpoints
	factorAttributor := data.NewFactorAttributor(store)
	r.GET("/api/factors/attribution/:factor_name", getFactorAttributionHandler(factorAttributor))
	r.GET("/api/factors/ic/:factor_name", getICHandler(factorAttributor))
	r.POST("/api/sync/factor-attribution/:factor_name", syncFactorAttributionHandler(factorAttributor))
	r.POST("/api/sync/factor-ic/:factor_name", syncFactorICHandler(factorAttributor))

	// Fundamental data endpoints (stock_fundamentals table)
	r.GET("/fundamentals/:symbol", getFundamentalsHandler(store))
	r.GET("/fundamentals/:symbol/history", getFundamentalsHistoryHandler(store))
	r.POST("/screen", screenStocksHandler(store))
}
