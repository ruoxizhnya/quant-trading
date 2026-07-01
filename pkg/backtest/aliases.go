package backtest

import (
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
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

// Compile-time use of zerolog to keep the import alive even before the
// sibling-extraction commits add constructor wrappers that need it.
// Without this, `goimports` would strip the import on the first
// commit. Once Commit 3 (execution/) lands, the import is used by
// the NewExecutionBridge / NewLiveBridge wrappers below (added then).
var _ zerolog.Logger
