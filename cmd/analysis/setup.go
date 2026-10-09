package main

// This file keeps the analysis-only builders and thin delegation shims
// to internal/bootstrap (AI 拆仓阶段 1：共享装配上收为 bootstrap 包，
// AI 部分随阶段 2 迁往 quant-trading-agent 仓）。
//
// 委托 shim 的理由：main.go / deps.go / 测试对这批函数的调用点与断言
// **零改动**（沿用 K1 切片 2「现有装配块一行不改」的同一裁决精神）。
// shim 体内的转发是唯一允许的改动；任何行为差异都必须发生在
// bootstrap 包里并被其测试覆盖。

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/internal/bootstrap"
	"github.com/ruoxizhnya/quant-trading/pkg/alert"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/spf13/viper"
)

// ─── 委托 shim（共享 builder 已上收 internal/bootstrap）────────────────

func initMetrics(logger zerolog.Logger) *observability.Metrics {
	return bootstrap.InitMetrics(logger)
}

// loadConfig reads the analysis-service YAML config (bootstrap.LoadConfig
// with the analysis default path).
func loadConfig(logger zerolog.Logger) *viper.Viper {
	return bootstrap.LoadConfig(logger, "config/analysis-service.yaml")
}

func buildBacktestEngine(v *viper.Viper, logger zerolog.Logger) (*backtest.Engine, marketdata.Provider) {
	return bootstrap.BuildBacktestEngine(v, logger)
}

func buildRiskManager(v *viper.Viper, logger zerolog.Logger) *risk.RiskManager {
	return bootstrap.BuildRiskManager(v, logger)
}

func initStore(v *viper.Viper, logger zerolog.Logger) *storage.PostgresStore {
	return bootstrap.InitStore(v, logger)
}

// InsecureExposureLoopbackPublished 别名保留（取值唯一权威在 bootstrap）。
const InsecureExposureLoopbackPublished = bootstrap.InsecureExposureLoopbackPublished

func authExposureOK(exposure string) bool { return bootstrap.AuthExposureOK(exposure) }

func decideAuthStartup(secret string, allowInsecure bool, bindHost string, insecureExposure string) bootstrap.AuthStartup {
	return bootstrap.DecideAuthStartup(secret, allowInsecure, bindHost, insecureExposure)
}

func isLoopbackHost(host string) bool { return bootstrap.IsLoopbackHost(host) }

func initAuth(v *viper.Viper, store *storage.PostgresStore, logger zerolog.Logger) *auth.Service {
	return bootstrap.InitAuth(v, store, logger)
}

func rateLimitPerMinute(v *viper.Viper) int { return bootstrap.RateLimitPerMinute(v) }

func applyGinMode(v *viper.Viper, logger zerolog.Logger) { bootstrap.ApplyGinMode(v, logger) }

func buildRouter(authSvc *auth.Service, v *viper.Viper, logger zerolog.Logger) *gin.Engine {
	return bootstrap.BuildRouter(authSvc, v, logger)
}

func startHTTPServer(router *gin.Engine, v *viper.Viper, logger zerolog.Logger) *http.Server {
	return bootstrap.StartHTTPServer(router, v, logger, "Analysis Service")
}

func waitForShutdown() os.Signal { return bootstrap.WaitForShutdown() }

func gracefulShutdown(srv *http.Server, jobService *backtest.JobService, alertManager *alert.AlertManager, store *storage.PostgresStore, logger zerolog.Logger) {
	bootstrap.GracefulShutdown(srv, jobService, alertManager, store, logger)
}

func newRateLimiter(rate int, window time.Duration) *bootstrap.RateLimiter {
	return bootstrap.NewRateLimiter(rate, window)
}

var rateLimitExemptPaths = bootstrap.RateLimitExemptPaths

func isRateLimitExempt(path string) bool { return bootstrap.IsRateLimitExempt(path) }

func initLogger() zerolog.Logger { return bootstrap.InitLogger() }

// ─── analysis 独有 builder（不随 AI 拆仓迁移）─────────────────────────

// buildExecutionTrader constructs the in-process MockTrader (P1-15,
// ODR-021) from viper config. The returned LiveTrader is injected
// into the backtest engine and exposed over HTTP.
func buildExecutionTrader(v *viper.Viper, logger zerolog.Logger) live.LiveTrader {
	return bootstrap.BuildExecutionTrader(v, logger)
}

// buildAlertSystem constructs the AlertManager + PeriodicAlertLoop
// (P2 alert, ODR-025) from viper config under alert.*.
func buildAlertSystem(v *viper.Viper, executionTrader live.LiveTrader, riskManager *risk.RiskManager, logger zerolog.Logger) (*alert.AlertManager, *PeriodicAlertLoop) {
	alertCfg := alert.AlertManagerConfig{
		MaxPositionWeight: v.GetFloat64("alert.max_position_weight"),
		MaxSectorWeight:   v.GetFloat64("alert.max_sector_weight"),
		MaxDrawdown:       v.GetFloat64("alert.max_drawdown"),
		DailyLossLimit:    v.GetFloat64("alert.daily_loss_limit"),
		FailureRateLimit:  v.GetFloat64("alert.failure_rate_limit"),
		WebhookURL:        v.GetString("alert.webhook_url"),
		WebhookTimeout:    time.Duration(v.GetInt("alert.webhook_timeout_sec")) * time.Second,
	}
	if alertCfg.WebhookTimeout == 0 {
		alertCfg.WebhookTimeout = 5 * time.Second
	}
	recorder := alert.NewRecorderChannel(v.GetInt("alert.recorder_capacity"))
	alertManager := alert.NewAlertManager(alertCfg, logger)
	alertManager.AddChannel(recorder)

	alertLoopCfg := PeriodicAlertConfig{
		Interval:     time.Duration(v.GetInt("alert.interval_sec")) * time.Second,
		HistoryLimit: v.GetInt("alert.history_limit"),
		Enabled:      v.GetBool("alert.enabled"),
	}
	if alertLoopCfg.Interval == 0 {
		alertLoopCfg.Interval = 5 * time.Minute
	}
	if alertLoopCfg.HistoryLimit == 0 {
		alertLoopCfg.HistoryLimit = 100
	}
	alertHistory := NewAlertHistory(alertLoopCfg.HistoryLimit)
	alertLoop := NewPeriodicAlertLoop(alertLoopCfg, alertManager, executionTrader, riskManager, alertHistory, logger)
	logger.Info().
		Bool("enabled", alertLoopCfg.Enabled).
		Dur("interval", alertLoopCfg.Interval).
		Int("history_limit", alertLoopCfg.HistoryLimit).
		Int("recorder_capacity", recorder.Len()).
		Msg("AlertManager + PeriodicAlertLoop attached in-process (P2 alert)")
	return alertManager, alertLoop
}

// dataServices bundles the data-layer services that depend on the
// Postgres store and backtest engine.
type dataServices struct {
	DataAdapter      *marketdata.DataAdapter
	JobService       *backtest.JobService
	WFEngine         *backtest.WalkForwardEngine
	BatchEngine      *backtest.BatchEngine
	FactorAttributor *data.FactorAttributor
}

// buildDataServices constructs the data adapter, job/walk-forward/batch
// engines, and factor attributor from the store and engine. The HTTP
// provider is reused for the DataAdapter fallback path.
//
// newEngine 为每个 walk-forward 窗口构造一个独立的 Engine。引擎级缓存
// （OHLCV / 因子 / 基本面 / 上市日历）是 per-instance 的，共享单例会让并发
// 窗口互相污染 —— 因子缓存是「整体替换」语义，窗口 B 的 Warm 会覆盖窗口 A
// 正在读取的缓存。传 nil 则退回共享 engine（仅应急，不推荐）。
func buildDataServices(
	store *storage.PostgresStore,
	engine *backtest.Engine,
	httpProvider marketdata.Provider,
	logger zerolog.Logger,
	newEngine func() (*backtest.Engine, error),
) *dataServices {
	pgProvider := marketdata.NewPostgresProvider(store, logger)
	dataAdapter := marketdata.NewDataAdapter(nil, pgProvider, httpProvider, logger)
	engine.SetDataAdapter(dataAdapter)

	jobService := backtest.NewJobService(store, engine)
	logger.Info().Msg("Job service initialized")

	// AUD-50: repair backtest rows left in `running` by a previous process.
	//
	// The same repair already runs on the way down (gracefulShutdown), but that
	// only covers an orderly SIGTERM. CleanupStaleRunning's own doc comment has
	// always said it is "also useful as a recovery tool after a hard process
	// crash (kill -9, OOM, etc.) — call it on startup to repair stale rows from
	// the previous run" — and no startup caller ever existed, so the
	// recommended path was never taken. This is that call.
	//
	// Safe here because nothing is running yet: the HTTP server has not started
	// and no job has been accepted, so a `running` row can only be a leftover.
	// Unlike the sync queue this cannot be made structural by hanging it off the
	// worker pool — backtest jobs run on their own goroutines, there is no
	// central dequeue to hook — so the "startup only" precondition is written
	// down instead of enforced. Calling it while jobs are in flight would fail
	// rows that are still working.
	//
	// internal/repoguard pins this call site to
	// cmd/analysis/setup.go:buildDataServices — moving it breaks the guard.
	if n, err := jobService.CleanupStaleRunning(context.Background()); err != nil {
		logger.Error().Err(err).
			Msg("Stale-running cleanup failed; an interrupted backtest may stay 'running'")
	} else if n > 0 {
		logger.Warn().Int("recovered", n).
			Msg("Recovered backtest jobs left 'running' by a previous process")
	}

	wfEngine := backtest.NewWalkForwardEngine(func() (*backtest.Engine, error) {
		if newEngine == nil {
			return engine, nil
		}
		eng, err := newEngine()
		if err != nil {
			return nil, err
		}
		// 新引擎必须挂上 dataAdapter，否则多源回退路径不生效。
		eng.SetDataAdapter(dataAdapter)
		eng.SetStore(store)
		return eng, nil
	}, store, logger)
	logger.Info().Msg("Walk-forward engine initialized")

	batchEngine := backtest.NewBatchEngine(engine, wfEngine, backtest.DefaultBatchConfig(), logger)
	logger.Info().Msg("Batch engine initialized")

	factorAttributor := data.NewFactorAttributor(store)
	logger.Info().Msg("Factor attribution service initialized")

	return &dataServices{
		DataAdapter:      dataAdapter,
		JobService:       jobService,
		WFEngine:         wfEngine,
		BatchEngine:      batchEngine,
		FactorAttributor: factorAttributor,
	}
}

// initStrategyAndPlugins constructs the StrategyDB and PluginLoader,
// seeds built-in strategies, and auto-loads plugins from the configured
// directory if plugins.directory is set.
func initStrategyAndPlugins(v *viper.Viper, store *storage.PostgresStore, logger zerolog.Logger) (*strategy.StrategyDB, *strategy.PluginLoader) {
	strategyDB := strategy.NewStrategyDB(store)
	if err := store.SeedStrategies(context.Background()); err != nil {
		logger.Warn().Err(err).Msg("failed to seed built-in strategies")
	} else {
		logger.Info().Msg("strategy DB seeded")
	}

	strategy.InitPluginLoader(strategy.DefaultRegistry, logger)
	pluginLoader := strategy.GlobalPluginLoader
	logger.Info().Msg("Plugin loader initialized")

	pluginDir := v.GetString("plugins.directory")
	if pluginDir != "" {
		if err := pluginLoader.SetWatchDir(pluginDir); err != nil {
			logger.Warn().Err(err).Str("dir", pluginDir).Msg("Failed to set plugin watch directory")
		} else {
			loaded, errs := pluginLoader.LoadAll()
			if len(errs) > 0 {
				logger.Warn().Int("errors", len(errs)).Msg("Some plugins failed to load")
			}
			logger.Info().Int("count", len(loaded)).Str("dir", pluginDir).Msg("Plugins auto-loaded")
		}
	}
	return strategyDB, pluginLoader
}
