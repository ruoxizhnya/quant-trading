// Package contracts holds shared behavioral interfaces that are
// implemented by multiple higher-layer packages and consumed by
// multiple lower-layer packages. S7-P1-3 (ODR-043): extracting these
// here eliminates duplicate interface definitions across
// pkg/strategy and pkg/ai/pipeline.
//
// This is a LEAF package on the core side: it imports only pkg/domain
// (for the BacktestResult return type) and the standard library. It does
// NOT import pkg/ai, pkg/strategy, or any other behavioral package, so a
// package importing pkg/backtest/contracts does NOT create a reverse
// dependency.
//
// 2026-10-09 rehoming (ADR-027 §5 step 3): this contract was originally
// parked in pkg/ai/contracts, which forced every consumer — including
// pkg/strategy — to reach back into the AI layer, an illegal reverse edge.
// But BacktestRunner is execution-carrier infrastructure, not an AI
// capability: it is a plain domain-typed port with no LLM, prompt, or
// non-determinism in it. It therefore belongs on the core side, and moved
// to pkg/backtest/contracts alongside the rest of the backtest core, so
// the dependency direction is now strictly ai → core. The S7-P1-3 /
// ODR-043 rationale below (eliminating duplicate interface definitions)
// is unchanged; only the package's home and thus the import path moved.
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
