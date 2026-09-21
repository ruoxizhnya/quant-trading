package main

// This file contains builder functions extracted from main() to keep
// main() a thin orchestration layer (S7-P2-3, ODR-043). Each builder
// constructs a single service or group of related services from viper
// config and is independently testable.

import (
	"context"
	"fmt"
	"net"
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
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/client"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/contracts"
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
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
	"github.com/ruoxizhnya/quant-trading/pkg/tools/builtin"
	"github.com/spf13/viper"
)

// initMetrics constructs the four ADR-017 §1 core metrics, registers
// them with the Prometheus default registry, attaches Go runtime
// collectors, and wires the metrics into the package-level httpClient
// transport so outbound calls record http_client_requests_total.
func initMetrics(logger zerolog.Logger) *observability.Metrics {
	m := observability.NewMetrics()
	m.Register()
	m.RegisterCollectors(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	// Wire the metrics into the httpClient transport so every
	// outbound call records http_client_requests_total.
	if t, ok := httpClient.Transport.(*observability.HTTPTransport); ok {
		t.Metrics = m
	}
	logger.Info().Msg("observability: 4 core metrics registered (ADR-017 §1)")
	return m
}

// loadConfig reads the analysis-service YAML config. The path is taken
// from CONFIG_PATH env var, defaulting to config/analysis-service.yaml.
// It also sets the global zerolog level from the config.
func loadConfig(logger zerolog.Logger) *viper.Viper {
	v := viper.New()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/analysis-service.yaml"
	}
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	// P0-4: 密钥只允许从 env 注入（YAML 是入库的）。两个名字都认，
	// JWT_SECRET 是历史用法，AUTH_JWT_SECRET 与 AutomaticEnv 的键名一致。
	_ = v.BindEnv("auth.jwt_secret", "JWT_SECRET", "AUTH_JWT_SECRET")
	_ = v.BindEnv("auth.allow_insecure", "AUTH_INSECURE")

	if err := v.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to read config file")
	}

	logLevel := v.GetString("logging.level")
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	return v
}

// buildBacktestEngine constructs the core backtest engine and the HTTP
// data provider. The HTTP provider is returned separately because it's
// also used to build the DataAdapter later in buildDataServices.
func buildBacktestEngine(v *viper.Viper, logger zerolog.Logger) (*backtest.Engine, marketdata.Provider) {
	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
	}
	httpProvider := marketdata.NewHTTPProvider(dataServiceURL, logger)
	engine, err := backtest.NewEngine(v, httpProvider, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize backtest engine")
	}
	return engine, httpProvider
}

// buildRiskManager constructs the in-process risk manager (P1-15,
// ODR-021) from viper config keys under risk_manager.*.
func buildRiskManager(v *viper.Viper, logger zerolog.Logger) *risk.RiskManager {
	riskCfg := risk.RiskManagerConfig{
		TargetVolatility:    v.GetFloat64("risk_manager.target_volatility"),
		MaxPositionWeight:   v.GetFloat64("risk_manager.max_position_weight"),
		MinPositionWeight:   v.GetFloat64("risk_manager.min_position_weight"),
		ATRPeriod:           v.GetInt("risk_manager.stoploss.atr_period"),
		BaseMultiplier:      v.GetFloat64("risk_manager.stoploss.base_multiplier"),
		BullMultiplier:      v.GetFloat64("risk_manager.stoploss.bull_multiplier"),
		BearMultiplier:      v.GetFloat64("risk_manager.stoploss.bear_multiplier"),
		SidewaysMultiplier:  v.GetFloat64("risk_manager.stoploss.sideways_multiplier"),
		TakeProfitMult:      v.GetFloat64("risk_manager.take_profit.atr_multiplier"),
		VolLookbackDays:     v.GetInt("risk_manager.volatility.lookback_days"),
		AnnualizationFactor: v.GetFloat64("risk_manager.volatility.annualization_factor"),
		FastMAPeriod:        v.GetInt("risk_manager.regime.fast_ma_period"),
		SlowMAPeriod:        v.GetInt("risk_manager.regime.slow_ma_period"),
		RegimeVolLookback:   v.GetInt("risk_manager.regime.vol_lookback"),
	}
	rm, err := risk.NewRiskManager(riskCfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize in-process risk manager (P1-15)")
	}
	logger.Info().Msg("risk manager attached to backtest engine in-process (P1-15)")
	return rm
}

// buildExecutionTrader constructs the in-process MockTrader (P1-15,
// ODR-021) from viper config. The returned LiveTrader is injected
// into the backtest engine and exposed over HTTP.
func buildExecutionTrader(v *viper.Viper, logger zerolog.Logger) live.LiveTrader {
	execConfig := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: v.GetFloat64("backtest.commission_rate"),
		MinCommission:  v.GetFloat64("trading.min_commission"),
		InitialCapital: v.GetFloat64("backtest.initial_capital"),
	}
	trader := live.NewMockTrader(live.MockTraderConfig{
		InitialCash:    execConfig.InitialCapital,
		CommissionRate: execConfig.CommissionRate,
		StampTaxRate:   v.GetFloat64("trading.stamp_tax_rate"),
		SlippageRate:   v.GetFloat64("backtest.slippage_rate"),
	}, logger)
	logger.Info().Msg("execution trader attached to backtest engine in-process (P1-15)")
	return trader
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

// initStore constructs the PostgreSQL store from viper config. If
// database.url is empty or contains unresolved placeholders, it's
// assembled from individual database.* keys.
func initStore(v *viper.Viper, logger zerolog.Logger) *storage.PostgresStore {
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
	return store
}

// authStartup 是启动期对鉴权配置的裁决结果。做成纯值是为了可测 ——
// 真正的 os.Exit 只在 initAuth 里发生一次，测试测 decideAuthStartup 即可。
type authStartup struct {
	secret   []byte // 空 = open-access
	insecure bool   // 明确处于「无鉴权」模式
	refuse   bool   // 拒绝启动
	reason   string // 拒绝原因（必须给出可操作的修复指引）
}

// decideAuthStartup 裁决启动期的鉴权配置（P0-4）。
//
// 契约：**没有密钥就拒绝启动**。此前密钥为空会静默进入 open-access —
// 任何能访问网络的人都能触发回测、创建订单。修成 fail-closed 后，
// 唯一豁免是「显式声明不安全的本地模式」：
//
//	auth.allow_insecure=true（env: AUTH_INSECURE）且 server.host 是 loopback
//
// 非 loopback 监听时即使声明了豁免也照样拒绝 —— 0.0.0.0 上的
// open-access 等于把下单接口开给整个局域网。
func decideAuthStartup(secret string, allowInsecure bool, bindHost string) authStartup {
	if s := strings.TrimSpace(secret); s != "" {
		return authStartup{secret: []byte(s)}
	}
	if !allowInsecure {
		return authStartup{
			refuse: true,
			reason: "auth: JWT secret missing — refusing to start in open-access mode. " +
				"Set JWT_SECRET (or auth.jwt_secret) to a strong random value. " +
				"Local dev only: set AUTH_INSECURE=true AND server.host=127.0.0.1",
		}
	}
	if !isLoopbackHost(bindHost) {
		return authStartup{
			refuse: true,
			reason: fmt.Sprintf("auth: AUTH_INSECURE=true but server.host=%q is not loopback — "+
				"open-access would be reachable from other hosts. "+
				"Set server.host=127.0.0.1, or configure JWT_SECRET instead", bindHost),
		}
	}
	return authStartup{insecure: true}
}

// isLoopbackHost 报告 host 是否只监听本机。空 host 视为非 loopback —
// gin 绑 ":port" 等价于 0.0.0.0，fail closed 更安全。
func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// initAuth constructs the JWT + RBAC auth service (P1-2, ADR-017 §2)
// and enforces the P0-4 startup gate: no secret, no start (unless the
// operator explicitly opts into loopback-only open access).
//
// 密钥来源（按优先级）：JWT_SECRET env → AUTH_JWT_SECRET env →
// auth.jwt_secret 配置项。YAML 是入库的，生产一律走 env。
func initAuth(v *viper.Viper, store *storage.PostgresStore, logger zerolog.Logger) *auth.Service {
	d := decideAuthStartup(
		v.GetString("auth.jwt_secret"),
		v.GetBool("auth.allow_insecure"),
		v.GetString("server.host"),
	)
	if d.refuse {
		logger.Fatal().Msg(d.reason)
	}

	authSvc := auth.NewService(store.DB(), auth.Config{
		JWTSecret:       d.secret,
		AccessTokenTTL:  v.GetDuration("auth.access_token_ttl"),
		RefreshTokenTTL: v.GetDuration("auth.refresh_token_ttl"),
		Issuer:          v.GetString("auth.issuer"),
	})
	if authSvc.Enabled() {
		logger.Info().
			Int("access_ttl_sec", int(authSvc.AccessTTL().Seconds())).
			Msg("auth: JWT enabled (P1-2)")
	} else {
		logger.Warn().Msg("auth: INSECURE open-access mode — NO authentication, loopback only, do not use outside local dev")
	}
	return authSvc
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

// buildCopilot constructs the Copilot service (S7-P1-2, ODR-043) with
// LLM client, code checker, and build executor injected at the
// composition root. Returns the service and a BacktestRunner adapter
// that delegates to the backtest engine.
func buildCopilot(v *viper.Viper, engine *backtest.Engine, logger zerolog.Logger) (*strategy.CopilotService, strategy.BacktestRunner) {
	// S7-P1-2 (ODR-043): wire the LLM client, code checker, and build
	// executor at the composition root. Previously NewCopilotService()
	// called ai.NewClient() internally and run() called
	// staticcheck.CheckOrError() / sandboxrunner.New() inline — all of
	// which created strategy → ai / strategy → internal/sandbox reverse
	// dependencies. The DI pattern moves those imports to main.go (the
	// composition root) where they belong.
	copilotService := strategy.NewCopilotService().
		WithLLMClient(ai.NewClient()).
		WithCodeChecker(staticCheckAdapter{}).
		WithBuildExecutor(newSandboxRunnerAdapter(logger)).
		WithLogger(logger.With().Str("component", "copilot").Logger()).
		WithWorkingDir(v.GetString("copilot.working_dir"))
	logger.Info().
		Bool("ai_configured", copilotService.IsConfigured()).
		Str("working_dir", copilotService.WorkingDir()).
		Msg("Copilot service initialized")
	copilotRunner := &strategyEngineAdapter{engine: engine}
	return copilotService, copilotRunner
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

// buildToolsRegistry constructs the Tools Registry (S7-P3-3, ODR-043)
// and registers all builtin tool groups. The registry is then exposed
// over /api/tools/* by ToolsHandler, enabling external agent services
// (e.g. Hermes Agent) to discover and invoke platform capabilities
// without reading SPEC.md.
//
// S7-P3-4 (Hermes Phase 1.7 + Phase 2.2-2.3): the registry now hosts 19 tools
// across 10 groups:
//   - backtest.run          (S7-P3-3, L3 gate)
//   - factor.compute        (S7-P3-3, L2 gate)
//   - factor.evaluate       (S7-P3-3)
//   - data.ohlcv / .stocks / .fundamentals  (S7-P3-3)
//   - strategy.list / .get                 (S7-P3-3)
//   - validate_factor        (Hermes Phase 1.1, L1 gate)
//   - compute_factor_ic      (Hermes Phase 1.2, L2 gate)
//   - walk_forward_validate  (Hermes Phase 1.3, L4 gate)
//   - list_factors / save_factor           (Hermes Phase 1.4, gene pool)
//   - list_strategies / save_strategy     (Hermes Phase 1.5, gene pool)
//   - summarize_backtest     (Hermes Phase 1.6)
//   - get_strategy_lineage    (Hermes Phase 2.2, gene pool lineage)
//   - get_market_regime       (Hermes Phase 2.3, market regime detection)
//   - research.profile        (EQD-P2-1, bridge B2, EquityDeep research archive)
//
// Wiring notes:
//   - BacktestTool reuses the same contracts.BacktestRunner (copilotRunner)
//     already wired into the AI pipeline — zero duplication.
//   - FactorTool uses an HTTP client pointed at this same service's
//     /api/factor/* endpoints (the analysis-service proxies to itself;
//     the factor endpoints are registered in registerFactorRoutes).
//   - DataFetchTool uses the shared httpClient (observability + X-Request-ID)
//     pointed at the data-service URL from viper config.
//   - StrategyRegistryTool reads from the package-level strategy.DefaultRegistry,
//     so no wiring is needed.
//   - WalkForwardValidateTool takes a builtin.WalkForwardRunner, satisfied
//     by walkForwardEngineAdapter (defined in main.go) wrapping ds.WFEngine.
//   - Gene Pool tools (list_factors / save_factor / list_strategies /
//     save_strategy) take narrow interfaces satisfied by *gene_pool.FactorPool
//     and *gene_pool.StrategyPool constructed from store.DB().
//   - GetStrategyLineageTool (Phase 2.2) reuses the same StrategyPoolClient.
//   - GetMarketRegimeTool (Phase 2.3) takes a builtin.RegimeDetectorClient
//     (satisfied by *risk.RiskManager) and reuses the dataClient constructed
//     below for OHLCV fetching.
//   - ResearchProfileTool (EQD-P2-1) takes a builtin.ResearchProfileClient
//     (satisfied by *storage.PostgresStore) and reads the contract C2 vault
//     mirror root from equitydeep.vault_path (env EQUITYDEEP_VAULT_PATH).
//     An empty path is valid: it disables the mirror fallback, leaving the
//     tool answering from the research.* projection alone.
//   - ValidateFactor / ComputeFactorIC / SummarizeBacktest have no DI.
func buildToolsRegistry(
	v *viper.Viper,
	runner contracts.BacktestRunner,
	wfRunner builtin.WalkForwardRunner,
	factorPool builtin.FactorPoolClient,
	strategyPool builtin.StrategyPoolClient,
	regimeDetector builtin.RegimeDetectorClient,
	researchProfile builtin.ResearchProfileClient,
	hypothesisStore *storage.PostgresStore,
	logger zerolog.Logger,
) *tools.Registry {
	reg := tools.NewRegistry()

	// ── Group 1: Backtest (S7-P3-3) ──────────────────────────────────
	if err := reg.Register(builtin.NewBacktestTool(runner)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register backtest.run tool")
	}

	// ── Group 2: Factor — HTTP + expression-based (S7-P3-3 + Hermes 1.1/1.2) ─
	analysisURL := fmt.Sprintf("http://localhost:%d", v.GetInt("server.port"))
	if v.GetInt("server.port") == 0 {
		analysisURL = "http://localhost:8085"
	}
	factorClient := client.NewFactorClient(analysisURL)
	if err := reg.Register(builtin.NewFactorComputeTool(factorClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register factor.compute tool")
	}
	if err := reg.Register(builtin.NewFactorEvaluateTool(factorClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register factor.evaluate tool")
	}
	// Hermes Phase 1.1: L1 syntax gate — no DI, creates fresh parser per Execute.
	if err := reg.Register(builtin.NewValidateFactorTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register validate_factor tool")
	}
	// Hermes Phase 1.2: L2 quick IC gate — reuses the same factor HTTP client.
	if err := reg.Register(builtin.NewComputeFactorICTool(factorClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register compute_factor_ic tool")
	}

	// ── Group 3: Data fetch (S7-P3-3) ───────────────────────────────
	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
	}
	dataClient := builtin.NewDataSourceClient(dataServiceURL, httpClient)
	if err := reg.Register(builtin.NewDataOHLCVTool(dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register data.ohlcv tool")
	}
	if err := reg.Register(builtin.NewDataStocksTool(dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register data.stocks tool")
	}
	if err := reg.Register(builtin.NewDataFundamentalsTool(dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register data.fundamentals tool")
	}

	// ── Group 4: Strategy registry (S7-P3-3) ─────────────────────────
	if err := reg.Register(builtin.NewStrategyListTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register strategy.list tool")
	}
	if err := reg.Register(builtin.NewStrategyGetTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register strategy.get tool")
	}

	// ── Group 5: Walk-forward validation (Hermes Phase 1.3, L4 gate) ─
	if err := reg.Register(builtin.NewWalkForwardValidateTool(wfRunner)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register walk_forward_validate tool")
	}

	// ── Group 6: Gene Pool — factor + strategy CRUD (Hermes Phase 1.4/1.5) ─
	if err := reg.Register(builtin.NewListFactorsTool(factorPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register list_factors tool")
	}
	if err := reg.Register(builtin.NewSaveFactorTool(factorPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register save_factor tool")
	}
	if err := reg.Register(builtin.NewListStrategiesTool(strategyPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register list_strategies tool")
	}
	if err := reg.Register(builtin.NewSaveStrategyTool(strategyPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register save_strategy tool")
	}

	// ── Group 7: Summarization (Hermes Phase 1.6) ───────────────────
	if err := reg.Register(builtin.NewSummarizeBacktestTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register summarize_backtest tool")
	}

	// ── Group 8: Lineage (Hermes Phase 2.2, gene pool lineage) ─────
	if err := reg.Register(builtin.NewGetStrategyLineageTool(strategyPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register get_strategy_lineage tool")
	}

	// ── Group 9: Market regime (Hermes Phase 2.3) ──────────────────
	// Reuses the dataClient from Group 3 for OHLCV fetching.
	if err := reg.Register(builtin.NewGetMarketRegimeTool(regimeDetector, dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register get_market_regime tool")
	}

	// ── Group 10: Research profile (EQD-P2-1, bridge B2) ───────────
	// Reads the EquityDeep research archive, preferring the research.*
	// projection and falling back to the contract C2 mirror under
	// equitydeep.vault_path (env EQUITYDEEP_VAULT_PATH). The vault is
	// mounted read-only; an empty path simply disables the fallback.
	if err := reg.Register(builtin.NewResearchProfileTool(researchProfile, v.GetString("equitydeep.vault_path"))); err != nil {
		logger.Fatal().Err(err).Msg("failed to register research.profile tool")
	}

	// ── Group 11: Factor hypothesis (P2-3) ─────────────────────────
	// 「这个因子凭什么有效」—— 采用一个因子之前先看它的机制来自哪里。
	//
	// 显式判空而不是直接传 hypothesisStore：*PostgresStore 的 nil 塞进
	// 接口会变成非 nil 的接口值，工具会拿着空指针去查库（P1-1b 的老坑）。
	// 没连库时工具仍可用 —— 它回退到 domain 里的内置假设表。
	var hs builtin.FactorHypothesisStore
	if hypothesisStore != nil {
		hs = hypothesisStore
	}
	if err := reg.Register(builtin.NewFactorHypothesisTool(hs)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register factor.hypothesis tool")
	}

	// ── Group 12: Strategy health (P2-6) ───────────────────────────
	// 「这个策略是不是开始不行了」—— 滚动指标 + 概念漂移检测。
	// 接上之前 pkg/ai/drift 与 pkg/strategy/monitor 是两个零调用方的孤儿包，
	// 实现完整却没人消费。无状态：每次调用新建 monitor 喂完整段序列。
	if err := reg.Register(builtin.NewStrategyHealthTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register monitor.strategy_health tool")
	}

	logger.Info().
		Int("tool_count", len(reg.List())).
		Msg("Tools Registry initialized (S7-P3-3 + Hermes Phase 1.7 + Phase 2.2-2.3 + EQD-P2-1): 19 tools exposed at /api/tools/* — backtest/factor/data/strategy/gene-pool/walk-forward/summarize/lineage/regime/research")
	return reg
}

// rateLimitPerMinute returns the gateway rate limit (requests per
// ClientIP per minute window) from rate_limit.per_minute, defaulting
// to 100. Env-overridable via RATE_LIMIT_PER_MINUTE through viper
// AutomaticEnv, mirroring the AI_RATE_LIMIT_PER_MIN pattern (ODR-013):
// e2e/load scenarios crank it up, incident response drops it down.
func rateLimitPerMinute(v *viper.Viper) int {
	if n := v.GetInt("rate_limit.per_minute"); n > 0 {
		return n
	}
	return 100
}

// buildRouter creates the gin router with recovery, CORS, rate-limiting,
// request logging, and auth middleware (when enabled).
func buildRouter(authSvc *auth.Service, v *viper.Viper, logger zerolog.Logger) *gin.Engine {
	if v.GetString("logging.format") == "json" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	// P0-4: CORS 按白名单回显，白名单来自 server.cors.allowed_origins。
	// 未配置 = 不回显任何 ACAO（fail closed），不再是硬编码的 `*`。
	router.Use(httpserver.CORS(httpserver.AllowedOrigins(v)))
	router.Use(newRateLimiter(rateLimitPerMinute(v), time.Minute).middleware())
	router.Use(requestLogger(logger))
	// P1-2: JWT auth middleware (no-op when auth is disabled) + audit
	// log middleware. Both run before route registration so the
	// handlers can rely on the context values being set.
	if authSvc.Enabled() {
		router.Use(authSvc.Middleware())
		router.Use(authSvc.AuditMiddleware())
	}
	return router
}

// startHTTPServer creates and starts the HTTP server in a goroutine.
// Returns the *http.Server so the caller can perform graceful shutdown.
func startHTTPServer(router *gin.Engine, v *viper.Viper, logger zerolog.Logger) *http.Server {
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
	return srv
}

// waitForShutdown blocks until a SIGINT or SIGTERM is received.
func waitForShutdown() os.Signal {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return <-quit
}

// gracefulShutdown performs the ordered shutdown sequence
// (Sprint 6 P0-8, ODR-013):
//  1. JobService.Shutdown — reject new jobs, cancel in-flight contexts
//     (must happen BEFORE srv.Shutdown so running backtests see ctx
//     cancelled and write "failed" status themselves).
//  2. AlertManager.Close — stop the webhook delivery goroutine.
//  3. srv.Shutdown — stop accepting new HTTP requests, wait for
//     in-flight handlers to return.
//  4. JobService.CleanupStaleRunning — safety net for any rows that
//     goroutines didn't get to update.
//  5. store.Close — release the DB connection.
//
// The total budget is a 30s parent context; phases 1-3 share it.
// Phase 4 gets a fresh 5s ctx so a stuck DB doesn't hold shutdown open.
func gracefulShutdown(srv *http.Server, jobService *backtest.JobService, alertManager *alert.AlertManager, store *storage.PostgresStore, logger zerolog.Logger) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := jobService.Shutdown(shutdownCtx); err != nil {
		logger.Warn().Err(err).Msg("JobService.Shutdown did not drain cleanly; will run CleanupStaleRunning")
	}

	// P2 alert (ODR-025): close the AlertManager. This stops the
	// in-process Webhook delivery goroutine (if any) and the recorder
	// channel. The PeriodicAlertLoop's Start() goroutine is bound to
	// context.Background() so it does not observe this ctx cancel
	// directly; instead, we close the manager and rely on the next
	// tick's Evaluate failing fast due to closed channels.
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
