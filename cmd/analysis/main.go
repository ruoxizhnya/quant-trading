package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/bootstrap"
	"github.com/ruoxizhnya/quant-trading/pkg/compliance"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
	"github.com/spf13/viper"
)

// httpClient wraps the outbound data-service / strategy-service calls.
// （原先还列了 ai-service，该服务于 2026-09-18 删除，见 TASKS P2-5。） Sprint 6 P0-3: HTTPTransport propagates the
// per-request X-Request-ID from the inbound request context to
// downstream calls AND records an observation in
// http_client_requests_total{service="data",status=...}.
//
// AI 拆仓阶段 1：client 本体上收 internal/bootstrap（cmd/ai 共享同一份）。
var httpClient = bootstrap.HTTPClient

// main is the composition root for the analysis service. It wires
// together all services via the builder functions in setup.go and
// orchestrates startup + graceful shutdown. S7-P2-3 (ODR-043): the
// 381-line main() was split into focused builders so this function is
// now a thin orchestrator (~35 lines).
func main() {
	logger := initLogger()
	m := initMetrics(logger)
	v := loadConfig(logger)

	// store 在 Boot 前构造：eventstore 模块复用它的 pool（一个进程一个池）。
	store := initStore(v, logger)

	// ─── 内核接管装配（K1 切片 3）─────────────────────────────────────
	// engine / riskManager / executionTrader / dataAdapter 的构造与注入
	// 顺序由 BootOrder 决定（eventstore→clock→data-engine→portfolio→
	// risk-engine→exec-engine→strategy-runtime→indicators→exec-algo→msgbus，
	// 见 pkg/kernel/interfaces.go 的冻结契约）；接管前的硬编码顺序
	// （buildBacktestEngine → buildRiskManager → buildExecutionTrader →
	// initStore → Set* 散装注入）就此退役。失败 fail-fast：内核是承重
	// 组件，装配不起来就不该对外提供服务。
	ak, err := assembleKernel(v, store, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("kernel assembly failed")
	}
	engine := ak.Wiring.Engine
	httpProvider := ak.Wiring.Provider
	riskManager := ak.Wiring.RiskManager
	executionTrader := ak.Wiring.ExecutionTrader

	alertManager, alertLoop := buildAlertSystem(v, executionTrader, riskManager, logger)

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

	// DataAdapter 由 data-engine 模块构造（Boot 第 3 位）—— 传进来复用，
	// 不在此重建（否则同一进程两条 adapter 实例 = 双真相）。
	ds := buildDataServices(store, engine, ak.Wiring.Adapter, logger, newEngine)
	strategyDB, pluginLoader := initStrategyAndPlugins(v, store, logger)

	// AI 拆仓阶段 1（ADR-027 §5 第 8 步前置切片）：Copilot / pipeline /
	// explore / tools 四族 handler 随 ai-service 迁出，本服务对这四族
	// 路由改 HTTP 反代（见 handlers_ai_proxy.go），前端零改动。装配侧
	// 相应移除 buildCopilot / buildToolsRegistry / 各 adapter 与
	// gene_pool pools —— 它们现在住在 cmd/ai。

	deps := &ServerDeps{
		Engine:           engine,
		JobService:       ds.JobService,
		WFEngine:         ds.WFEngine,
		BatchEngine:      ds.BatchEngine,
		StrategyDB:       strategyDB,
		FactorAttributor: ds.FactorAttributor,
		PluginLoader:     pluginLoader,
		AuthSvc:          authSvc,
		RiskManager:      riskManager,
		ExecutionTrader:  executionTrader,
		EmergencyToken:   v.GetString("trading.emergency_token"),
		Metrics:          m,
		Logger:           logger,
		Viper:            v,
		Store:            store,
	}

	// AUD-29: gin's run mode comes from server.gin_mode and is applied
	// exactly once, here, before the router exists.
	applyGinMode(v, logger)

	// 内核已在上方 Boot（接管装配）——影子启动随切片 3 删除。

	router := buildRouter(authSvc, v, logger)
	registerRoutes(router, deps)
	registerAlertRoutes(router, alertLoop)
	go alertLoop.Start(context.Background())

	srv := startHTTPServer(router, v, logger)

	sig := waitForShutdown()
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received; beginning graceful drain")

	// ─── 内核关停（K1 切片 3；时序裁决继承切片 2）─────────────────────
	// 必须早于 gracefulShutdown：后者 phase 4 会 store.Close() 关掉连接池，
	// 而 kernel.shutdown 要落 audit.message_log —— 落库时 pool 必须还活着。
	// 内核内部顺序：先发 kernel.shutdown（msgbus/eventstore 仍运行），再按
	// BootOrder 逆序停（msgbus 最先停、eventstore 最后停）。见 kernel.go 的
	// 「时序裁决」注释与其单测 TestShutdownPublishesBeforeStoppingMsgBus。
	shutdownKernel(ak.Kernel, logger)

	gracefulShutdown(srv, ds.JobService, alertManager, store, logger)
}

func registerRoutes(router *gin.Engine, deps *ServerDeps) {

	// Legacy HTML UI (cmd/analysis/static/) retired in AUD-33 (2026-09-22,
	// the ③ stage of the AUD-18 staged-retirement ruling). web/ is the only
	// frontend now: nginx serves it on host port 8080 (AUD-32) and it reaches
	// this service only through /api/*.
	//
	// Do NOT re-add a catch-all "/" or a /static mount here. This service is
	// an API; if it also answers "/" with HTML, the two frontends silently
	// diverge again -- and deps_test.go's mustNotHave guard will fire.

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
	registerDatasourceRoutes(router, deps.Engine)
	registerFactorRoutes(router, deps.FactorAttributor, deps.Logger)
	registerPluginRoutes(router, deps.PluginLoader)
	// AI 拆仓阶段 1（ADR-027 §5 第 8 步前置切片）：copilot / pipeline /
	// explore / tools 四族路由改反代到 ai-service。原进程内注册（registerCopilotRoutes
	// / registerPipelineRoutes / registerExploreRoutes / NewToolsHandler）
	// 已随 handler 文件迁往 cmd/ai。前端与 openapi 契约不变 —— 路径、
	// 宿主、端口全部维持原样。
	registerAIProxyRoutes(router, deps.Viper, deps.Logger)
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
	//
	// AUD-02 (ODR-065 H5): WithExecutionAuth gates the order-mutating
	// endpoints (POST /orders, POST /orders/:id/cancel) behind
	// RequireRole(trader, admin) — on both /api/execution/* and the
	// legacy root paths.
	NewExecutionHandler(deps.ExecutionTrader, deps.Logger, deps.EmergencyToken,
		WithExecutionAuth(deps.AuthSvc)).RegisterRoutes(router)

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
