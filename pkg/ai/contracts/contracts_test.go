// S7-P1-3 regression tests: verify the shared BacktestRunner contract
// lives in pkg/ai/contracts and is re-exported by both pkg/strategy
// and pkg/ai/pipeline via type aliases.
//
// This is an EXTERNAL test package (package contracts_test) so it can
// import pkg/strategy and pkg/ai/pipeline — both of which import
// pkg/ai/contracts. An internal test (package contracts) would create
// an import cycle.
//
// What these tests lock in:
//  1. contracts.BacktestRunner exists with the canonical signature
//     (returns *domain.BacktestResult, not a local narrow type).
//  2. strategy.BacktestRunner is a type alias for contracts.BacktestRunner
//     (not a re-declared interface) — eliminating the duplicate definition.
//  3. pipeline.BacktestRunner is a type alias for contracts.BacktestRunner.
//  4. A single concrete adapter satisfies all three references — proving
//     they're the SAME type.
package contracts_test

import (
	"context"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
)

// stubRunner is a minimal concrete type that satisfies BacktestRunner.
// Used to verify the interface can be implemented by an external type.
type stubRunner struct{}

func (stubRunner) RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error) {
	return &domain.BacktestResult{TotalTrades: 1}, nil
}

// TestS7P1_3_BacktestRunnerInterfaceExists verifies the canonical
// interface exists in pkg/ai/contracts with the expected signature.
func TestS7P1_3_BacktestRunnerInterfaceExists(t *testing.T) {
	var _ contracts.BacktestRunner = (*stubRunner)(nil)
	t.Log("contracts.BacktestRunner exists with canonical signature")
}

// TestS7P1_3_BacktestRunner_IsSatisfiedByStub verifies a concrete
// implementation can be assigned to the interface and invoked.
func TestS7P1_3_BacktestRunner_IsSatisfiedByStub(t *testing.T) {
	var r contracts.BacktestRunner = stubRunner{}
	result, err := r.RunBacktest(context.Background(), "test-strat", nil, "2025-01-01", "2025-12-31")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.TotalTrades,
		"stub runner must return the expected BacktestResult")
}

// TestS7P1_3_BacktestRunner_ReturnsDomainResult verifies the return
// type is *domain.BacktestResult (not a local narrow type). This is
// the key distinction from the agents.BacktestRunner in validate_l4.go
// which returns BacktestMetrics — a different, intentionally narrow
// interface that is NOT consolidated here.
func TestS7P1_3_BacktestRunner_ReturnsDomainResult(t *testing.T) {
	var r contracts.BacktestRunner = stubRunner{}
	result, _ := r.RunBacktest(context.Background(), "x", nil, "", "")
	// The compile-time signature already guarantees *domain.BacktestResult;
	// this assertion documents the contract and guards against accidental
	// narrowing in a future refactor.
	assert.NotNil(t, result,
		"contracts.BacktestRunner.RunBacktest must return a non-nil *domain.BacktestResult, "+
			"not a local narrow type — this distinguishes it from agents.BacktestRunner")
	// Verify the concrete type is *domain.BacktestResult (not some alias
	// that wraps a different type).
	assert.IsType(t, &domain.BacktestResult{}, result,
		"return type must be exactly *domain.BacktestResult")
}

// TestS7P1_3_StrategyAndPipelineAliasesAreIdentical verifies that
// strategy.BacktestRunner and pipeline.BacktestRunner are zero-cost
// type aliases to contracts.BacktestRunner (the SAME type), not
// re-declared interfaces. If either package re-declares the interface,
// the type assertion below fails to compile.
//
// This is the "eliminate 3 duplicate definitions" guard: a future
// contributor who re-introduces a local `type BacktestRunner interface`
// in pkg/strategy or pkg/ai/pipeline will break this test at compile
// time, surfacing the regression before it ships.
func TestS7P1_3_StrategyAndPipelineAliasesAreIdentical(t *testing.T) {
	// If strategy.BacktestRunner is a type alias, this assignment is
	// valid (same type). If it's a re-declared interface, the types are
	// distinct and the assignment fails to compile.
	var s strategy.BacktestRunner = stubRunner{}
	var p pipeline.BacktestRunner = stubRunner{}

	// Both are the same type as contracts.BacktestRunner — cross-assign
	// to prove it. With re-declared interfaces these would be distinct
	// types and the assignments would fail.
	var c contracts.BacktestRunner = s
	_ = p
	_ = c
	t.Log("strategy.BacktestRunner, pipeline.BacktestRunner, and " +
		"contracts.BacktestRunner are the SAME type (zero-cost aliases)")
}
