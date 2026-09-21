package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/internal/sandbox/runner"
	"github.com/ruoxizhnya/quant-trading/internal/sandbox/staticcheck"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/gene_pool"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/compliance"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
	"github.com/spf13/viper"
)

// httpClient wraps the outbound data-service / strategy-service calls.
// （原先还列了 ai-service，该服务于 2026-09-18 删除，见 TASKS P2-5。） Sprint 6 P0-3: HTTPTransport propagates the
// per-request X-Request-ID from the inbound request context to
// downstream calls AND records an observation in
// http_client_requests_total{service="data",status=...}.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &observability.HTTPTransport{
		Service: "data",
	},
}

type strategyEngineAdapter struct {
	engine *backtest.Engine
}

// staticCheckAdapter implements strategy.CodeChecker by delegating to
// internal/sandbox/staticcheck. S7-P1-2 (ODR-043): defined HERE in the
// composition root (cmd/analysis) so pkg/strategy doesn't import
// internal/sandbox/staticcheck — breaking the reverse dependency.
type staticCheckAdapter struct{}

func (staticCheckAdapter) CheckOrError(code string) error {
	return staticcheck.CheckOrError(code)
}

// sandboxRunnerAdapter implements strategy.BuildExecutor by delegating
// to internal/sandbox/runner. S7-P1-2 (ODR-043): defined HERE in the
// composition root so pkg/strategy doesn't import internal/sandbox/runner.
//
// The runner is constructed once with the same 30s timeout + 1GiB
// memory cap that the old inline code used (Sprint 6 P1-11 / ODR-020)
// and reused across build attempts — Runner is stateless beyond its
// config, so reuse is safe.
type sandboxRunnerAdapter struct {
	r *runner.Runner
}

func newSandboxRunnerAdapter() *sandboxRunnerAdapter {
	return &sandboxRunnerAdapter{
		r: runner.New(
			runner.WithTimeout(30*time.Second),
			runner.WithLimits(runner.Limits{
				MemoryBytes: 1 << 30, // 1 GiB
				CPUSeconds:  25,
				OpenFiles:   256,
			}),
		),
	}
}

func (a *sandboxRunnerAdapter) Run(ctx context.Context, name string, args []string, workingDir string) (*bytes.Buffer, *bytes.Buffer, error) {
	return a.r.Run(ctx, name, args, runner.Options{Dir: workingDir})
}

func (a *sandboxRunnerAdapter) IsTimeout(err error) bool {
	return errors.Is(err, runner.ErrTimeout)
}

func (a *strategyEngineAdapter) RunBacktest(
	ctx context.Context,
	strategyName string,
	stockPool []string,
	startDate, endDate string,
) (*domain.BacktestResult, error) {
	req := backtest.BacktestRequest{
		Strategy:  strategyName,
		StockPool: stockPool,
		StartDate: startDate,
		EndDate:   endDate,
	}
	resp, err := a.engine.RunBacktest(ctx, req)
	if err != nil {
		return nil, err
	}
	return &domain.BacktestResult{
		TotalReturn:    resp.TotalReturn,
		AnnualReturn:   resp.AnnualReturn,
		SharpeRatio:    resp.SharpeRatio,
		SortinoRatio:   resp.SortinoRatio,
		MaxDrawdown:    resp.MaxDrawdown,
		WinRate:        resp.WinRate,
		TotalTrades:    resp.TotalTrades,
		WinTrades:      resp.WinTrades,
		LoseTrades:     resp.LoseTrades,
		AvgHoldingDays: resp.AvgHoldingDays,
		CalmarRatio:    resp.CalmarRatio,
	}, nil
}

// walkForwardEngineAdapter wraps *backtest.WalkForwardEngine (=
// *walkforward.WalkForwardEngine) to satisfy builtin.WalkForwardRunner.
//
// The concrete engine's RunWalkForward takes a WalkForwardRequest struct,
// while the narrow Tool interface takes individual params. This adapter
// bridges the two, assembling the struct at the composition root so the
// Tool layer stays decoupled from the engine's request DTO.
//
// S7-P3-4 (Hermes Phase 1.7): defined HERE in the composition root
// (cmd/analysis) following the strategyEngineAdapter pattern — the
// builtin package defines the interface, the adapter implements it
// structurally without importing builtin.
type walkForwardEngineAdapter struct {
	engine *backtest.WalkForwardEngine
}

func (a *walkForwardEngineAdapter) RunWalkForward(
	ctx context.Context,
	strategyName string,
	stockPool []string,
	startDate, endDate string,
	params domain.WalkForwardParams,
) (*domain.WalkForwardReport, error) {
	req := backtest.WalkForwardRequest{
		Strategy:          strategyName,
		StockPool:         stockPool,
		StartDate:         startDate,
		EndDate:           endDate,
		WalkForwardParams: params,
	}
	return a.engine.RunWalkForward(ctx, req)
}

// main is the composition root for the analysis service. It wires
// together all services via the builder functions in setup.go and
// orchestrates startup + graceful shutdown. S7-P2-3 (ODR-043): the
// 381-line main() was split into focused builders so this function is
// now a thin orchestrator (~35 lines).
func main() {
	logger := initLogger()
	m := initMetrics(logger)
	v := loadConfig(logger)

	engine, httpProvider := buildBacktestEngine(v, logger)
	riskManager := buildRiskManager(v, logger)
	engine.SetRiskManager(riskManager)

	executionTrader := buildExecutionTrader(v, logger)
	engine.SetLiveTrader(executionTrader)

	alertManager, alertLoop := buildAlertSystem(v, executionTrader, riskManager, logger)

	store := initStore(v, logger)
	engine.SetStore(store)

	authSvc := initAuth(v, store, logger)

	// walk-forward 的每个窗口必须拿到**独立**的 Engine 实例：引擎级缓存
	// （OHLCV / 因子 / 基本面 / 上市日历）是 per-instance 的，共享单例会让并发
	// 窗口互相污染 —— 因子缓存是「整体替换」语义，窗口 B 的 Warm 会直接覆盖
	// 窗口 A 正在读取的缓存。这里复用主 engine 的全部装配参数，只换掉实例。
	newEngine := func() (*backtest.Engine, error) {
		eng, err := backtest.NewEngine(v, httpProvider, logger)
		if err != nil {
			return nil, err
		}
		eng.SetRiskManager(riskManager)
		eng.SetLiveTrader(executionTrader)
		eng.SetStore(store)
		return eng, nil
	}

	ds := buildDataServices(store, engine, httpProvider, logger, newEngine)
	copilotService, copilotRunner := buildCopilot(v, engine, logger)
	strategyDB, pluginLoader := initStrategyAndPlugins(v, store, logger)

	// S7-P3-3 (ODR-043): build the Tools Registry after copilotRunner
	// is available (BacktestTool delegates to it) and after strategies
	// are seeded (StrategyRegistryTool reads from the global registry).
	//
	// S7-P3-4 (Hermes Phase 1.7): the registry now also wires:
	//   - WalkForwardValidateTool (via walkForwardEngineAdapter wrapping ds.WFEngine)
	//   - ListFactors/SaveFactor tools (via gene_pool.NewFactorPool(store.DB()))
	//   - ListStrategies/SaveStrategy tools (via gene_pool.NewStrategyPool(store.DB()))
	//   - ValidateFactor / ComputeFactorIC / SummarizeBacktest tools (no DI)
	//   - ResearchProfileTool (EQD-P2-1) via *storage.PostgresStore (research.*
	//     projection) + equitydeep.vault_path (contract C2 mirror fallback)
	factorPool := gene_pool.NewFactorPool(store.DB())
	strategyPool := gene_pool.NewStrategyPool(store.DB())
	wfRunner := &walkForwardEngineAdapter{engine: ds.WFEngine}
	toolsRegistry := buildToolsRegistry(v, copilotRunner, wfRunner, factorPool, strategyPool, riskManager, store, store, logger)

	deps := &ServerDeps{
		Engine:           engine,
		JobService:       ds.JobService,
		WFEngine:         ds.WFEngine,
		BatchEngine:      ds.BatchEngine,
		StrategyDB:       strategyDB,
		CopilotService:   copilotService,
		CopilotRunner:    copilotRunner,
		FactorAttributor: ds.FactorAttributor,
		PluginLoader:     pluginLoader,
		AuthSvc:          authSvc,
		RiskManager:      riskManager,
		ExecutionTrader:  executionTrader,
		EmergencyToken:   v.GetString("trading.emergency_token"),
		Metrics:          m,
		Logger:           logger,
		Viper:            v,
		ToolsRegistry:    toolsRegistry,
		Store:            store,
	}

	router := buildRouter(authSvc, v, logger)
	registerRoutes(router, deps)
	registerAlertRoutes(router, alertLoop)
	go alertLoop.Start(context.Background())

	srv := startHTTPServer(router, v, logger)

	sig := waitForShutdown()
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received; beginning graceful drain")
	gracefulShutdown(srv, ds.JobService, alertManager, store, logger)
}

func initLogger() zerolog.Logger {
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

func requestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info().
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Dur("latency", time.Since(start)).
			Msg("request")
	}
}

func registerRoutes(router *gin.Engine, deps *ServerDeps) {

	router.Static("/static", "./cmd/analysis/static")

	router.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/index.html")
	})
	router.GET("/screen", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/screen.html")
	})
	router.GET("/screen.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/screen.html")
	})
	router.GET("/dashboard", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/dashboard.html")
	})
	router.GET("/dashboard.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/dashboard.html")
	})
	router.GET("/copilot", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/copilot.html")
	})
	router.GET("/copilot.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/copilot.html")
	})
	router.GET("/strategy-selector", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/strategy-selector.html")
	})
	router.GET("/strategy-selector.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/strategy-selector.html")
	})
	router.GET("/index.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/index.html")
	})

	healthHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "analysis-service",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
	router.GET("/health", healthHandler)
	// /api/health 是同一个探针的 /api 版本。它必须存在：Dockerfile 的
	// HEALTHCHECK 打的就是这个路径，而在此之前后端只注册了 /health ——
	// 探针一直 404，容器从启动起就被判 unhealthy。
	// 另外前端在 dev 下只代理 /api（vite proxy），不挂这个别名前端也探不到。
	router.GET("/api/health", healthHandler)

	// Sprint 6 P0-3: /metrics endpoint exposing the four ADR-017 §1
	// core metrics + Go runtime collectors. Unauthenticated by
	// design — the metrics scraper runs on the same network and
	// ADR-017 §2 (P1-2) will add an authn boundary separately.
	router.GET("/metrics", observability.Handler(deps.Metrics))

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

	// P2-17: OpenAPI 3.0 spec + Swagger UI. The spec is embedded
	// from docs/openapi.yaml (via docs/embed.go) and served at
	// /api/openapi.yaml; the Swagger UI is served at /api/docs.
	registerOpenAPIRoutes(router)

	registerProxyRoutes(router, httpClient, deps.Viper, deps.Logger)
	registerBacktestRoutes(router, deps.Engine, deps.JobService, deps.Logger)
	registerWalkForwardRoutes(router, deps.WFEngine, deps.Logger)
	registerBatchRoutes(router, deps.BatchEngine, deps.Logger)
	registerStrategyRoutes(router, deps.StrategyDB)
	registerCopilotRoutes(router, deps.CopilotService, deps.CopilotRunner)
	registerDatasourceRoutes(router, deps.Engine)
	registerFactorRoutes(router, deps.FactorAttributor, deps.Logger)
	registerPluginRoutes(router, deps.PluginLoader)
	// S7-P0-1 (ODR-043-1): inject copilotRunner so the AI pipeline can
	// execute the backtest stage end-to-end instead of silently skipping
	// it. copilotRunner is the same *strategyEngineAdapter already wired
	// into /api/copilot above.
	// P1-1b：把实验日志落点接进 pipeline。
	// 显式判空而不是直接传 deps.Store —— *PostgresStore 的 nil 塞进接口会
	// 变成一个非 nil 的接口值，pipeline 会拿着空指针去写日志（这个坑在
	// P0-5 的 aiClient 上踩过一次）。
	var expSink pipeline.ExperimentSink
	if deps.Store != nil {
		expSink = deps.Store
	}
	registerPipelineRoutes(router, deps.CopilotRunner, expSink)
	// P1-2：探索的 HTTP 入口。与上面共用同一个 sink，所以每一轮探索的
	// 每一次尝试都会落进 experiments 表。
	registerExploreRoutes(router, deps.CopilotRunner, expSink, deps.Engine)
	registerAuthRoutes(router, deps.AuthSvc, deps.Logger)

	// P1-15 (Sprint 6, ODR-021): risk + execution endpoints
	// absorbed from cmd/risk/main.go + cmd/execution/main.go.
	// Both backends are in-process (risk.RiskManager and
	// live.MockTrader) so the HTTP layer is a thin shim — no
	// service-to-service hop.
	NewRiskHandler(deps.RiskManager, deps.Logger).RegisterRoutes(router)
	// P2-3 (ODR-026): pass the emergency-flatten bearer token
	// through to the execution handler. Empty token disables the
	// kill-switch endpoint (returns 503 instead of 404).
	NewExecutionHandler(deps.ExecutionTrader, deps.Logger, deps.EmergencyToken).RegisterRoutes(router)

	// P2-4 (ODR-028): investor suitability (compliance) endpoints.
	// The handler is read-only — it does not block order submission
	// in the execution path; the frontend does the precheck before
	// calling POST /api/execution/orders. The default profile is
	// loaded from `trading.default_user_profile.*` in the analysis
	// config; in production this is replaced by a JWT-driven DB
	// lookup (P1-2 + a future `users` table column set).
	defaultProfile := loadDefaultSuitabilityProfile(deps.Viper)
	// P2-6 (ODR-028): large-transaction reporter config from
	// `compliance.reporter.*` viper keys. Defaults are regulatory
	// (2M / 5M) but the operator can override per environment.
	reporterCfg := compliance.LargeTradeConfig{
		SingleThresholdCNY:     deps.Viper.GetFloat64("compliance.reporter.single_threshold_cny"),
		CumulativeThresholdCNY: deps.Viper.GetFloat64("compliance.reporter.cumulative_threshold_cny"),
		OutputPath:             deps.Viper.GetString("compliance.reporter.output_path"),
		AccountWhitelist:       map[string]bool{},
	}
	NewComplianceHandler(deps.Logger, defaultProfile, reporterCfg).RegisterRoutes(router)

	// S7-P3-3 (ODR-043): Tools Registry endpoints. Exposes backtest /
	// factor / data / strategy capabilities as discoverable Tools over
	// /api/tools/* so external agent services can call without reading
	// SPEC.md.
	NewToolsHandler(deps.ToolsRegistry, deps.Logger).RegisterRoutes(router)

	// L0-3 (ADR-022 §5): read-only Evidence API. Resolves a citation's
	// content_hash to its unique archived source response in `ingest.raw`
	// (404 = not ingested). This is the platform's single evidence
	// coordinate — work faces read through it instead of storing copies.
	NewEvidenceHandler(deps.Store, deps.Logger).RegisterRoutes(router)
}

// loadDefaultSuitabilityProfile reads the suitability profile from
// the analysis-service viper config under `trading.default_user_profile.*`.
// Missing keys resolve to zero values — a zero-valued profile
// represents the most conservative "default-reject" stance (no asset,
// no experience, no risk level → nothing passes for restricted boards).
func loadDefaultSuitabilityProfile(v *viper.Viper) compliance.SuitabilityProfile {
	p := compliance.SuitabilityProfile{
		UserID:           v.GetString("trading.default_user_profile.user_id"),
		AssetDailyAvgCNY: v.GetFloat64("trading.default_user_profile.asset_daily_avg_cny"),
		RiskLevel:        compliance.RiskLevel(v.GetInt("trading.default_user_profile.risk_level")),
		BoardsEnabled:    v.GetStringSlice("trading.default_user_profile.boards_enabled"),
	}
	if firstTrade := v.GetString("trading.default_user_profile.first_trade_at"); firstTrade != "" {
		if t, err := time.Parse(time.RFC3339, firstTrade); err == nil {
			p.FirstTradeAt = t
		}
	}
	if rte := v.GetString("trading.default_user_profile.risk_test_expired_at"); rte != "" {
		if t, err := time.Parse(time.RFC3339, rte); err == nil {
			p.RiskTestExpiredAt = t
		}
	}
	return p
}
