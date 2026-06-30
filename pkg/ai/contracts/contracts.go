// Package contracts holds shared behavioral interfaces that are
// implemented by multiple higher-layer packages and consumed by
// multiple lower-layer packages. S7-P1-3 (ODR-043): extracting these
// here eliminates duplicate interface definitions across
// pkg/strategy and pkg/ai/pipeline.
//
// This is a LEAF package: it imports only pkg/domain (for the
// BacktestResult return type) and the standard library. It does NOT
// import pkg/ai, pkg/strategy, or any other behavioral package, so
// importing pkg/ai/contracts does NOT create a reverse dependency.
//
// Aliasing convention: packages that previously defined their own
// BacktestRunner should replace the local interface definition with:
//
//	type BacktestRunner = contracts.BacktestRunner
//
// This is a zero-cost type alias (not a re-declaration), so all
// existing code that used the local interface continues to work
// unchanged — including adapters, mocks, and compile-time assertions.
package contracts

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// BacktestRunner is the canonical contract for running a backtest
// against a named strategy over a stock pool and date range.
//
// S7-P1-3 (ODR-043): previously defined identically in both
// pkg/strategy/copilot.go and pkg/ai/pipeline/pipeline.go. The third
// definition in pkg/ai/agents/validate_l4.go is intentionally NOT
// consolidated here — it returns BacktestMetrics (a narrow local
// struct) rather than *domain.BacktestResult, and is kept narrow so
// that mocks are trivial (see validate_l4.go:84).
//
// Implementations:
//   - *strategyEngineAdapter (cmd/analysis/main.go) — production
//   - *mockBacktestRunner (pkg/ai/pipeline/pipeline_test.go) — tests
//   - *stubBacktestRunner (cmd/analysis/handlers_pipeline_test.go) — tests
type BacktestRunner interface {
	RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error)
}
