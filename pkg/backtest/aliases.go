package backtest

import (
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/cache"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/execution"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/state"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/walkforward"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// S7-P2-1 (ODR-043): this file re-exports the canonical types,
// constants, and the EngineRunner interface from the leaf package
// pkg/backtest/contracts so that every existing caller of
// `backtest.BacktestRequest` / `backtest.TradingConfig` / etc. keeps
// compiling unchanged after the DTOs move into contracts/.
//
// Type aliases (`type X = contracts.X`) are zero-cost (the compiler
// inlines them to the canonical type), so there is no wrapper overhead
// and no behavior change. Constants are re-exported with `const X =
// contracts.X`, which is likewise a compile-time operation.
//
// Go has no function alias, so the constructor helpers below are thin
// wrappers that delegate to the subpackage constructors. Callers stay
// unchanged.

// Type aliases (zero-cost, backward compatible).
type (
	TradingConfig    = contracts.TradingConfig
	PriceLimitConfig = contracts.PriceLimitConfig
	BacktestRequest  = contracts.BacktestRequest
	BacktestResponse = contracts.BacktestResponse
)

// Const aliases. The three fee constants are double-aliased
// (contracts -> fees), preserving the single-source-of-truth invariant
// from S7-P1-4.
const (
	DefaultStampTaxRate     = contracts.DefaultStampTaxRate
	DefaultMinCommission    = contracts.DefaultMinCommission
	DefaultTransferFeeRate  = contracts.DefaultTransferFeeRate
	DefaultPriceLimitNormal = contracts.DefaultPriceLimitNormal
	DefaultPriceLimitST     = contracts.DefaultPriceLimitST
	DefaultPriceLimitNew    = contracts.DefaultPriceLimitNew
	DefaultNewStockDays     = contracts.DefaultNewStockDays
	DefaultShortSellingRate = contracts.DefaultShortSellingRate
	TradingDaysPerYear      = contracts.TradingDaysPerYear
)

// EngineRunner is re-exported so parent-package callers (and future
// subpackage wrappers in this same file) can reference it as
// `backtest.EngineRunner` without importing contracts directly.
type EngineRunner = contracts.EngineRunner

// --- cache/ subpackage re-exports (S7-P2-1 Commit 2) ---

type (
	CacheManager        = cache.CacheManager
	FactorCacheAccessor = cache.FactorCacheAccessor
	FactorStore         = cache.FactorStore
)

// ErrQuintileNotFound re-exported from cache/.
var ErrQuintileNotFound = cache.ErrQuintileNotFound

// --- execution/ subpackage re-exports (S7-P2-1 Commit 3) ---

type (
	ExecutionService         = execution.ExecutionService
	Quote                    = execution.Quote
	BacktestExecutionService = execution.BacktestExecutionService
	ExecutionBridge          = execution.ExecutionBridge
	LiveBridge               = execution.LiveBridge
)

// Constructor wrappers (Go has no function alias, so we delegate).
// Callers that used backtest.NewBacktestExecutionService / NewExecutionBridge
// / NewLiveBridge keep working unchanged.

func NewBacktestExecutionService(config domain.ExecutionConfig) *BacktestExecutionService {
	return execution.NewBacktestExecutionService(config)
}

func NewExecutionBridge(logger zerolog.Logger) *ExecutionBridge {
	return execution.NewExecutionBridge(logger)
}

func NewLiveBridge(logger zerolog.Logger) *LiveBridge {
	return execution.NewLiveBridge(logger)
}

// --- tracker/ subpackage re-exports (S7-P2-1 Commit 4) ---

type (
	Tracker            = tracker.Tracker
	OrderLog           = tracker.OrderLog
	OrderExecutionOpts = tracker.OrderExecutionOpts
)

// NewTracker wrapper — delegates to tracker.NewTracker. The trading
// param resolves through the TradingConfig alias (= contracts.TradingConfig).
func NewTracker(initialCapital, commissionRate, slippageRate float64, trading TradingConfig, logger zerolog.Logger) *Tracker {
	return tracker.NewTracker(initialCapital, commissionRate, slippageRate, trading, logger)
}

// --- state/ subpackage re-exports (S7-P2-1 Commit 5) ---

type (
	BacktestState         = state.BacktestState
	BacktestStateSnapshot = state.BacktestStateSnapshot
	StateStore            = state.StateStore
	LRUStateStore         = state.LRUStateStore
	NoopStateStore        = state.NoopStateStore
	DiskStateStore        = state.DiskStateStore
)

// DefaultStateStoreCapacity re-exported from state/.
const DefaultStateStoreCapacity = state.DefaultStateStoreCapacity

var (
	ErrStateNotFound  = state.ErrStateNotFound
	ErrInvalidStateID = state.ErrInvalidStateID
)

// Constructor wrappers (Go has no function alias, so we delegate).
func NewLRUStateStore(capacity int) *LRUStateStore { return state.NewLRUStateStore(capacity) }
func NewNoopStateStore() *NoopStateStore           { return state.NewNoopStateStore() }
func NewDiskStateStore(dir string) (*DiskStateStore, error) {
	return state.NewDiskStateStore(dir)
}

// --- walkforward/ subpackage re-exports (S7-P2-1 Commit 6) ---

type (
	WalkForwardEngine  = walkforward.WalkForwardEngine
	WalkForwardRequest = walkforward.WalkForwardRequest
)

// NewWalkForwardEngine wrapper preserves the original signature
// (engine *Engine, store) by extracting engine.logger internally,
// so cmd/analysis/setup.go and all external callers compile unchanged.
func NewWalkForwardEngine(engine *Engine, store *storage.PostgresStore) *WalkForwardEngine {
	return walkforward.NewWalkForwardEngine(engine, store, engine.logger)
}

// Compile-time assertion: *Engine satisfies contracts.EngineRunner.
// If the RunBacktest signature ever drifts from the interface, the
// build fails here rather than at a distant call site.
var _ contracts.EngineRunner = (*Engine)(nil)

// defaultTradingConfig wraps the exported contracts.DefaultTradingConfig
// to keep the two existing call sites in engine.go (NewEngine and
// NewEngineWithOptions) unchanged. Kept unexported because only the
// parent package should call it; sibling subpackages reach for
// contracts.DefaultTradingConfig() directly.
func defaultTradingConfig() TradingConfig {
	return contracts.DefaultTradingConfig()
}
