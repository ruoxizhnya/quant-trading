package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/alert"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
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

func main() {
	logger := initLogger()

	// Sprint 6 P0-3: construct the four ADR-017 §1 core metrics and
	// expose them via /metrics. The Go runtime collectors are
	// attached for memory/CPU/Goroutine visibility.
	metrics = observability.NewMetrics()
	metrics.Register()
	metrics.RegisterCollectors(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	// Wire the metrics into the httpClient transport so every
	// outbound call records http_client_requests_total.
	if t, ok := httpClient.Transport.(*observability.HTTPTransport); ok {
		t.Metrics = metrics
	}
	logger.Info().Msg("observability: 4 core metrics registered (ADR-017 §1)")

	v := viper.New()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/analysis-service.yaml"
	}
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to read config file")
	}

	logLevel := v.GetString("logging.level")
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
	}
	httpProvider := marketdata.NewHTTPProvider(dataServiceURL, logger)

	engine, err := backtest.NewEngine(v, httpProvider, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize backtest engine")
	}

	// P1-15 (Sprint 6, ODR-021): in-process risk manager.
	// Constructed once from viper config and injected into the
	// backtest engine so position sizing / regime detection /
	// stop-loss checks happen in-process (zero HTTP latency). The
	// same instance is also exposed over HTTP via RiskHandler.
	riskCfg := risk.RiskManagerConfig{
		TargetVolatility:     v.GetFloat64("risk_manager.target_volatility"),
		MaxPositionWeight:    v.GetFloat64("risk_manager.max_position_weight"),
		MinPositionWeight:    v.GetFloat64("risk_manager.min_position_weight"),
		ATRPeriod:            v.GetInt("risk_manager.stoploss.atr_period"),
		BaseMultiplier:       v.GetFloat64("risk_manager.stoploss.base_multiplier"),
		BullMultiplier:       v.GetFloat64("risk_manager.stoploss.bull_multiplier"),
		BearMultiplier:       v.GetFloat64("risk_manager.stoploss.bear_multiplier"),
		SidewaysMultiplier:   v.GetFloat64("risk_manager.stoploss.sideways_multiplier"),
		TakeProfitMult:       v.GetFloat64("risk_manager.take_profit.atr_multiplier"),
		VolLookbackDays:      v.GetInt("risk_manager.volatility.lookback_days"),
		AnnualizationFactor:  v.GetFloat64("risk_manager.volatility.annualization_factor"),
		FastMAPeriod:         v.GetInt("risk_manager.regime.fast_ma_period"),
		SlowMAPeriod:         v.GetInt("risk_manager.regime.slow_ma_period"),
		RegimeVolLookback:    v.GetInt("risk_manager.regime.vol_lookback"),
	}
	riskManager, err := risk.NewRiskManager(riskCfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize in-process risk manager (P1-15)")
	}
	engine.SetRiskManager(riskManager)
	logger.Info().Msg("risk manager attached to backtest engine in-process (P1-15)")

	// P1-15 (Sprint 6, ODR-021): in-process execution trader.
	// A MockTrader with the A-share trading rules from the
	// analysis config is created once. The same instance is
	// injected into the backtest engine via WithLiveTrader so
	// backtest signal bridges to live/paper trading without an
	// HTTP call, AND exposed over HTTP via ExecutionHandler so
	// operators can submit / list / cancel orders through REST.
	execConfig := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: v.GetFloat64("backtest.commission_rate"),
		MinCommission:  v.GetFloat64("trading.min_commission"),
		InitialCapital: v.GetFloat64("backtest.initial_capital"),
	}
	executionTrader := live.NewMockTrader(live.MockTraderConfig{
		InitialCash:    execConfig.InitialCapital,
		CommissionRate: execConfig.CommissionRate,
		StampTaxRate:   v.GetFloat64("trading.stamp_tax_rate"),
		SlippageRate:   v.GetFloat64("backtest.slippage_rate"),
	}, logger)
	engine.SetLiveTrader(executionTrader)
	logger.Info().Msg("execution trader attached to backtest engine in-process (P1-15)")

	// P2 alert (ODR-025): in-process AlertManager + PeriodicAlertLoop.
	// Wires the alert.AlertManager (P1-29) into the analysis
	// process so the running portfolio is continuously evaluated
	// against the 6 P0 risk rules. The manager owns its channels
	// (LogChannel always-on + WebhookChannel if URL configured +
	// RecorderChannel for HTTP exposure). The loop ticks every
	// alert.interval seconds; alerts are pushed to the history
	// ring buffer for /api/alerts/history.
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

	dbURL := v.GetString("database.url")
	if dbURL == "" || strings.Contains(dbURL, "${") {
		dbUser := v.GetString("database.user")
		dbPassword := v.GetString("database.password")
		dbHost := v.GetString("database.host")
		dbPort := v.GetInt("database.port")
		dbName := v.GetString("database.database")
		dbSSLMode := v.GetString("database.sslmode")
		if dbHost == "" {
			dbHost = "localhost"
		}
		if dbPort == 0 {
			dbPort = 5432
		}
		if dbName == "" {
			dbName = "quant_trading"
		}
		if dbSSLMode == "" {
			dbSSLMode = "disable"
		}
		dbURL = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			url.PathEscape(dbUser), url.PathEscape(dbPassword), dbHost, dbPort, dbName, dbSSLMode)
	}
	store, err := storage.NewPostgresStore(context.Background(), dbURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize postgres store")
	}
	engine.SetStore(store)

	// P1-2: JWT + RBAC + audit (ADR-017 §2). If `auth.jwt_secret` is
	// unset, the service runs in "disabled" mode and the middleware is
	// a no-op so dev / test environments continue to work. The secret
	// is read from env (JWT_SECRET) or, failing that, from the YAML
	// config — but YAML is checked in, so prefer env in production.
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		jwtSecret = []byte(v.GetString("auth.jwt_secret"))
	}
	authSvc := auth.NewService(store.DB(), auth.Config{
		JWTSecret:       jwtSecret,
		AccessTokenTTL:  v.GetDuration("auth.access_token_ttl"),
		RefreshTokenTTL: v.GetDuration("auth.refresh_token_ttl"),
		Issuer:          v.GetString("auth.issuer"),
	})
	if authSvc.Enabled() {
		logger.Info().
			Int("access_ttl_sec", int(authSvc.AccessTTL().Seconds())).
			Msg("auth: JWT enabled (P1-2)")
	} else {
		logger.Warn().Msg("auth: JWT secret not configured — running in open-access mode (dev only)")
	}
	_ = authSvc // referenced via registerAuthRoutes below

	pgProvider := marketdata.NewPostgresProvider(store, logger)
	dataAdapter := marketdata.NewDataAdapter(nil, pgProvider, httpProvider, logger)
	engine.SetDataAdapter(dataAdapter)

	jobService := backtest.NewJobService(store, engine)
	logger.Info().Msg("Job service initialized")

	wfEngine := backtest.NewWalkForwardEngine(engine, store)
	logger.Info().Msg("Walk-forward engine initialized")

	batchEngine := backtest.NewBatchEngine(engine, wfEngine, backtest.DefaultBatchConfig(), logger)
	logger.Info().Msg("Batch engine initialized")

	factorAttributor := data.NewFactorAttributor(store)
	logger.Info().Msg("Factor attribution service initialized")

	copilotService := strategy.NewCopilotService().
		WithLogger(logger.With().Str("component", "copilot").Logger()).
		WithWorkingDir(v.GetString("copilot.working_dir"))
	logger.Info().
		Bool("ai_configured", copilotService.IsConfigured()).
		Str("working_dir", copilotService.WorkingDir()).
		Msg("Copilot service initialized")

	copilotRunner := &strategyEngineAdapter{engine: engine}

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

	if v.GetString("logging.format") == "json" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())
	router.Use(newRateLimiter(100, time.Minute).middleware())
	router.Use(requestLogger(logger))
	// P1-2: JWT auth middleware (no-op when auth is disabled) + audit
	// log middleware. Both run before route registration so the
	// handlers can rely on the context values being set.
	if authSvc.Enabled() {
		router.Use(authSvc.Middleware())
		router.Use(authSvc.AuditMiddleware())
	}

	registerRoutes(router, engine, jobService, wfEngine, batchEngine, strategyDB, copilotService, copilotRunner, factorAttributor, pluginLoader, authSvc, riskManager, executionTrader, v.GetString("trading.emergency_token"), logger)

	// P2 alert (ODR-025): register the alert HTTP endpoints and
	// start the periodic evaluation loop. The loop is a single
	// goroutine that runs until ctx is cancelled by the shutdown
	// sequence below.
	registerAlertRoutes(router, alertLoop)
	go alertLoop.Start(context.Background())

	host := v.GetString("server.host")
	port := v.GetInt("server.port")
	addr := fmt.Sprintf("%s:%d", host, port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info().
			Str("address", addr).
			Msg("Analysis Service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received; beginning graceful drain")

	// Sprint 6 P0-8 (ODR-013): graceful shutdown sequence.
	//
	// The order matters and is documented in ADR-017 §1 / ODR-013:
	//
	//   1. JobService.Shutdown — REJECT new jobs, CANCEL in-flight
	//      contexts, WAIT for goroutines to settle (up to 30s). This
	//      must happen BEFORE srv.Shutdown so that requests currently
	//      running a backtest see their ctx cancelled and can write
	//      the "failed" status themselves (rather than being torn
	//      out mid-write by the HTTP server close).
	//   2. srv.Shutdown — stop accepting new HTTP requests and wait
	//      for in-flight handlers to return. The 30s timeout is
	//      shared with step 1.
	//   3. JobService.CleanupStaleRunning — safety net for any rows
	//      that the goroutines didn't get to update (e.g. a hard
	//      freeze on the DB write, or the wait deadline elapsed).
	//   4. Close the storage connection and stop the plugin loader's
	//      file watcher so we don't leak FDs after process exit.
	//
	// The total budget is a 30s parent context; each phase gets a
	// slice of it. Phases 1 and 2 share the same ctx so that the
	// overall wall-clock stays under 30s even in the worst case.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := jobService.Shutdown(shutdownCtx); err != nil {
		logger.Warn().Err(err).Msg("JobService.Shutdown did not drain cleanly; will run CleanupStaleRunning")
	}

	// P2 alert (ODR-025): close the AlertManager. This stops the
	// in-process Webhook delivery goroutine (if any) and the
	// recorder channel. The PeriodicAlertLoop's Start() goroutine
	// is bound to context.Background() above so it does not
	// observe this ctx cancel directly; instead, we close the
	// manager and rely on the next tick's Evaluate failing fast
	// due to closed channels. Acceptable trade-off: the loop
	// goroutine is killed when the process exits.
	alertManager.Close()
	logger.Info().Msg("AlertManager closed (P2 alert)")

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("HTTP server forced to shutdown")
	} else {
		logger.Info().Msg("HTTP server stopped accepting new requests")
	}

	// Phase 3: sweep any rows still stuck in 'running'. We do this
	// with a fresh, short ctx so a stuck DB doesn't hold the whole
	// shutdown open past the budget.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	transitioned, cleanupErr := jobService.CleanupStaleRunning(cleanupCtx)
	if cleanupErr != nil {
		logger.Error().Err(cleanupErr).Msg("CleanupStaleRunning failed; some jobs may still appear as 'running' in DB")
	} else if transitioned > 0 {
		logger.Info().Int("transitioned", transitioned).Msg("Stale 'running' jobs transitioned to 'failed'")
	} else {
		logger.Info().Msg("No stale 'running' jobs found; DB state is clean")
	}

	// Phase 4: close remaining resources. The PluginLoader's Watch
	// loop is context-driven and exits on its own; we don't need to
	// explicitly stop it. The store gets an explicit Close so the
	// underlying *sql.DB is released and FDs don't leak after exit.
	if store != nil {
		store.Close()
		logger.Info().Msg("Postgres store closed")
	}

	logger.Info().Msg("Server exited")
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

func registerRoutes(router *gin.Engine, engine *backtest.Engine, jobService *backtest.JobService, wfEngine *backtest.WalkForwardEngine, batchEngine *backtest.BatchEngine, strategyDB *strategy.StrategyDB, copilotService *strategy.CopilotService, copilotRunner strategy.BacktestRunner, factorAttributor *data.FactorAttributor, pluginLoader *strategy.PluginLoader, authSvc *auth.Service, riskManager *risk.RiskManager, executionTrader live.LiveTrader, emergencyToken string, logger zerolog.Logger) {
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

	registerProxyRoutes(router, httpClient, logger)
	registerBacktestRoutes(router, engine, jobService, logger)
	registerWalkForwardRoutes(router, wfEngine, logger)
	registerBatchRoutes(router, batchEngine, logger)
	registerStrategyRoutes(router, strategyDB)
	registerCopilotRoutes(router, copilotService, copilotRunner)
	registerDatasourceRoutes(router, engine, logger)
	registerFactorRoutes(router, factorAttributor, logger)
	registerPluginRoutes(router, pluginLoader)
	registerPipelineRoutes(router)
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
}
