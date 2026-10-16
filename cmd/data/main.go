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

	// AUD-29: gin's run mode comes from server.gin_mode and is applied
	// exactly once, here, before the router exists.
	applyGinMode(logger)

	router := buildRouter()
	syncHandler := registerRoutes(router, store, cache, tushareClient, dataCache, buildEquityDeepDictionary(logger))
	registerRegistryRoutes(router, newRegistryHandler(dataSourceRegistry))

	// SYNC-01: the scheduler must be started here. Before this it was constructed and
	// given its 7 job types, but nothing ever called Start() — so active schedules never
	// fired and /api/sync/scheduler/stats read 0/0 forever.
	//
	// A failure is logged loudly and the service keeps serving: at this point the store is
	// already reachable (routes are live and loadAndScheduleAll already tolerates a bad
	// cron row per schedule with a warning), so a store-level read error here would break
	// HTTP anyway — killing the process for a scheduler that cannot load would turn a
	// partial failure into a total one. The failure is *not* silent: `startHTTPServer`
	// below serves, and stats stay truthful at 0 entries / empty next_runs.
	if err := syncHandler.StartScheduler(); err != nil {
		logger.Error().Err(err).Msg("sync scheduler disabled: failed to load active schedules — scheduled syncs will NOT run")
	}

	srv := startHTTPServer(router, logger)

	sig := waitForShutdown()
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received; beginning graceful drain")

	gracefulShutdown(srv, logger)
	// Drain order: HTTP first, then the scheduler, so cron cannot create new jobs into a
	// queue whose workers are already being torn down; the worker pool goes last
	// (SYNC-02: its Stop had no caller, so in-flight jobs were simply killed with the
	// process). Stop() cancels the pool context, which reaches in-flight executors
	// (per-job ctx derives from the pool ctx); their rows stay `running` and are reaped
	// by CleanupStaleRunning on the next start (AUD-50), so an interrupted job stays
	// recoverable instead of silently lost.
	syncHandler.StopScheduler()
	syncHandler.StopWorkerPool()
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
func registerRoutes(r *gin.Engine, store *storage.PostgresStore, cache storage.Cache, tc *data.TushareClient, dc *data.DataCache, equityDeepDict *equitydeep.Dictionary) *SyncHandler {
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
	// 这个 POST 不需要 store：落库在 TushareClient 内部（setup.go:165 注入，
	// tushare.go 的 FetchIndexConstituents 调 SaveIndexConstituentBatch，
	// DATA-06 后其 error 上抛 ⇒ handler 回 5xx）。上一条 GET 要 store 是因为它
	// 读库，两者形状不同 —— bdd1949 把 store 加到这里时只改了调用点没改签名，
	// cmd/data 自 21:38 起编译失败。
	r.POST("/sync/index-constituents/:index_code", syncIndexConstituentsHandler(tc))

	// Sectors（DATA-01）：由 stocks.industry 派生，不走外网
	r.POST("/sync/sectors", syncSectorsHandler(store))

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
		api.POST("/jobs", syncHandler.createJobHandler) // typed create door (ODR-062)
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

	// Returned so the composition root can drive its lifecycle (SYNC-01: Start/Stop of
	// the scheduler had no caller while it stayed trapped in here).
	return syncHandler
}
