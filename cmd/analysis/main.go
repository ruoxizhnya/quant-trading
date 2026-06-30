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
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/compliance"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
	"github.com/spf13/viper"
)

// httpClient wraps the outbound data-service / strategy-service /
// ai-service calls. Sprint 6 P0-3: HTTPTransport propagates the
// per-request X-Request-ID from the inbound request context to
// downstream calls AND records an observation in
// http_client_requests_total{service="data",status=...}.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &observability.HTTPTransport{
		Service: "data",
	},
}

// metrics holds the four ADR-017 §1 core metrics. Constructed in
// main() and shared into the httpClient transport (records
// http_client_requests_total), the /metrics handler, and any
// backtest/LLM observation call sites.
var metrics *observability.Metrics

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

// main is the composition root for the analysis service. It wires
// together all services via the builder functions in setup.go and
// orchestrates startup + graceful shutdown. S7-P2-3 (ODR-043): the
// 381-line main() was split into focused builders so this function is
// now a thin orchestrator (~35 lines).
func main() {
	logger := initLogger()
	metrics = initMetrics(logger)
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

	ds := buildDataServices(store, engine, httpProvider, logger)
	copilotService, copilotRunner := buildCopilot(v, engine, logger)
	strategyDB, pluginLoader := initStrategyAndPlugins(v, store, logger)

	router := buildRouter(authSvc, v, logger)
	registerRoutes(router, engine, ds.JobService, ds.WFEngine, ds.BatchEngine, strategyDB, copilotService, copilotRunner, ds.FactorAttributor, pluginLoader, authSvc, riskManager, executionTrader, v.GetString("trading.emergency_token"), logger, v)
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

func registerRoutes(router *gin.Engine, engine *backtest.Engine, jobService *backtest.JobService, wfEngine *backtest.WalkForwardEngine, batchEngine *backtest.BatchEngine, strategyDB *strategy.StrategyDB, copilotService *strategy.CopilotService, copilotRunner strategy.BacktestRunner, factorAttributor *data.FactorAttributor, pluginLoader *strategy.PluginLoader, authSvc *auth.Service, riskManager *risk.RiskManager, executionTrader live.LiveTrader, emergencyToken string, logger zerolog.Logger, v *viper.Viper) {

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

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "analysis-service",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// Sprint 6 P0-3: /metrics endpoint exposing the four ADR-017 §1
	// core metrics + Go runtime collectors. Unauthenticated by
	// design — the metrics scraper runs on the same network and
	// ADR-017 §2 (P1-2) will add an authn boundary separately.
	router.GET("/metrics", observability.Handler(metrics))

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

	registerProxyRoutes(router, httpClient, logger)
	registerBacktestRoutes(router, engine, jobService, logger)
	registerWalkForwardRoutes(router, wfEngine, logger)
	registerBatchRoutes(router, batchEngine, logger)
	registerStrategyRoutes(router, strategyDB)
	registerCopilotRoutes(router, copilotService, copilotRunner)
	registerDatasourceRoutes(router, engine, logger)
	registerFactorRoutes(router, factorAttributor, logger)
	registerPluginRoutes(router, pluginLoader)
	// S7-P0-1 (ODR-043-1): inject copilotRunner so the AI pipeline can
	// execute the backtest stage end-to-end instead of silently skipping
	// it. copilotRunner is the same *strategyEngineAdapter already wired
	// into /api/copilot above.
	registerPipelineRoutes(router, copilotRunner)
	registerAuthRoutes(router, authSvc, logger)

	// P1-15 (Sprint 6, ODR-021): risk + execution endpoints
	// absorbed from cmd/risk/main.go + cmd/execution/main.go.
	// Both backends are in-process (risk.RiskManager and
	// live.MockTrader) so the HTTP layer is a thin shim — no
	// service-to-service hop.
	NewRiskHandler(riskManager, logger).RegisterRoutes(router)
	// P2-3 (ODR-026): pass the emergency-flatten bearer token
	// through to the execution handler. Empty token disables the
	// kill-switch endpoint (returns 503 instead of 404).
	NewExecutionHandler(executionTrader, logger, emergencyToken).RegisterRoutes(router)

	// P2-4 (ODR-028): investor suitability (compliance) endpoints.
	// The handler is read-only — it does not block order submission
	// in the execution path; the frontend does the precheck before
	// calling POST /api/execution/orders. The default profile is
	// loaded from `trading.default_user_profile.*` in the analysis
	// config; in production this is replaced by a JWT-driven DB
	// lookup (P1-2 + a future `users` table column set).
	defaultProfile := loadDefaultSuitabilityProfile(v)
	// P2-6 (ODR-028): large-transaction reporter config from
	// `compliance.reporter.*` viper keys. Defaults are regulatory
	// (2M / 5M) but the operator can override per environment.
	reporterCfg := compliance.LargeTradeConfig{
		SingleThresholdCNY:     v.GetFloat64("compliance.reporter.single_threshold_cny"),
		CumulativeThresholdCNY: v.GetFloat64("compliance.reporter.cumulative_threshold_cny"),
		OutputPath:             v.GetString("compliance.reporter.output_path"),
		AccountWhitelist:       map[string]bool{},
	}
	NewComplianceHandler(logger, defaultProfile, reporterCfg).RegisterRoutes(router)
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
