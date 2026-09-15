package main

// ServerDeps bundles all service dependencies that registerRoutes
// (and the HTTP handlers it wires) need. Replacing 16 positional
// parameters with a struct makes the dependency graph explicit,
// keeps call sites readable, and lets new dependencies be added
// without changing every caller (S7-P2-4, ODR-043).
//
// Construction happens in main() via the builder functions in
// setup.go; the struct is then passed by pointer to registerRoutes.

import (
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
	"github.com/spf13/viper"
)

type ServerDeps struct {
	// Core backtest engine — serves /backtest, /backtest/:id/report, etc.
	Engine *backtest.Engine

	// JobService — async backtest job management (POST /backtest).
	JobService *backtest.JobService

	// WFEngine — walk-forward analysis (POST /walkforward).
	WFEngine *backtest.WalkForwardEngine

	// BatchEngine — batch backtests (POST /batch/backtest).
	BatchEngine *backtest.BatchEngine

	// StrategyDB — strategy CRUD + DB-backed list (GET /strategies).
	StrategyDB *strategy.StrategyDB

	// CopilotService — LLM-driven strategy generation (POST /api/copilot/*).
	CopilotService *strategy.CopilotService

	// CopilotRunner — executes backtests for the AI pipeline
	// (POST /api/pipeline/*). Same adapter instance wired into CopilotService.
	CopilotRunner strategy.BacktestRunner

	// FactorAttributor — factor attribution analysis (GET /api/factors/*).
	FactorAttributor *data.FactorAttributor

	// PluginLoader — strategy plugin hot-swap (GET /plugins, POST /plugins/reload).
	PluginLoader *strategy.PluginLoader

	// AuthSvc — JWT + RBAC (POST /api/auth/*). nil-safe: when JWT is
	// not configured, the service runs in "disabled" mode.
	AuthSvc *auth.Service

	// RiskManager — in-process risk manager (P1-15, /api/risk/*).
	RiskManager *risk.RiskManager

	// ExecutionTrader — in-process MockTrader (P1-15, /api/execution/*).
	ExecutionTrader live.LiveTrader

	// EmergencyToken — bearer token for the kill-switch endpoint
	// (POST /api/execution/emergency-flatten). Empty disables the endpoint.
	EmergencyToken string

	// Metrics — the four ADR-017 §1 core metrics, exposed at /metrics.
	Metrics *observability.Metrics

	// Logger — structured logger shared across all handlers.
	Logger zerolog.Logger

	// Viper — config reader, used by registerRoutes to load the
	// default suitability profile and large-trade reporter config.
	Viper *viper.Viper

	// ToolsRegistry — S7-P3-3 (ODR-043): the Tools Registry exposes
	// backtest/factor/data/strategy capabilities as discoverable Tools
	// over /api/tools/*. Enables external agent services to call this
	// platform without reading SPEC.md or hand-crafting HTTP requests.
	ToolsRegistry *tools.Registry

	// Store — L0-3 (ADR-022 §5): the Postgres store backs the read-only
	// Evidence API (GET /api/evidence/:content_hash), which resolves a
	// citation's content_hash to its unique archived source response in
	// `ingest.raw`. Work faces read data through this door instead of
	// keeping their own copy.
	Store *storage.PostgresStore
}
