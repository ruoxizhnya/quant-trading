// S7-P1-3 regression tests: verify the shared BacktestRunner contract
// lives in pkg/backtest/contracts and carries the canonical signature.
//
// This is an EXTERNAL test package (package contracts_test) so it only
// exercises the exported contract — it deliberately imports NOTHING from
// pkg/ai. The contract is core-side infrastructure (ADR-027 §5 step 3):
// pkg/strategy and pkg/ai/pipeline re-export it via zero-cost type
// aliases, so those aliases can be asserted without this file depending
// on an AI package. The cross-package alias-identity check lives in
// pkg/ai/pipeline/backtest_runner_alias_test.go.
//
// What these tests lock in:
//  1. contracts.BacktestRunner exists with the canonical signature
//     (returns *domain.BacktestResult, not a local narrow type).
//  2. A concrete external type can satisfy and invoke the interface.
//  3. The return type is exactly *domain.BacktestResult.
package contracts_test

import (
	"context"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
)

// stubRunner is a minimal concrete type that satisfies BacktestRunner.
// Used to verify the interface can be implemented by an external type.
type stubRunner struct{}

func (stubRunner) RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error) {
	return &domain.BacktestResult{TotalTrades: 1}, nil
}

// TestS7P1_3_BacktestRunnerInterfaceExists verifies the canonical
// interface exists in pkg/backtest/contracts with the expected signature.
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
