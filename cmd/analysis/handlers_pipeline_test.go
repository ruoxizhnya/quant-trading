package main

import (
	"context"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBacktestRunner is a test double implementing pipeline.BacktestRunner.
// It records that RunBacktest was invoked, proving the handler wires the
// runner through to the pipeline instead of passing nil.
type stubBacktestRunner struct {
	called       bool
	strategyName string
}

func (s *stubBacktestRunner) RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error) {
	s.called = true
	s.strategyName = strategyName
	return &domain.BacktestResult{}, nil
}

// TestNewPipelineHandler_DefaultHasNoRunner verifies backward compatibility:
// when no runner option is supplied, handler.runner stays nil and the pipeline
// will skip the backtest stage (the legacy behaviour).
func TestNewPipelineHandler_DefaultHasNoRunner(t *testing.T) {
	handler := NewPipelineHandler()
	assert.Nil(t, handler.runner, "default handler should have nil runner for backward compat")
}

// TestNewPipelineHandler_WithRunner verifies the S7-P0-1 fix: the handler
// must accept a BacktestRunner dependency via functional option so that
// main.go can inject copilotRunner instead of leaving it nil.
//
// Before the fix, NewPipelineHandler accepted no arguments and RunPipeline
// hardcoded nil as the runner, causing Stage 5 (backtest) to be silently
// skipped on every request — the pipeline was never end-to-end runnable.
func TestNewPipelineHandler_WithRunner(t *testing.T) {
	runner := &stubBacktestRunner{}
	handler := NewPipelineHandler(WithBacktestRunner(runner))

	require.NotNil(t, handler.runner, "handler must hold the injected runner")
	assert.Same(t, runner, handler.runner, "handler.runner must be the exact instance injected")
}

// TestNewPipelineHandler_WithRunner_NilStaysNil verifies that explicitly
// passing a nil runner via the option is equivalent to not passing it.
func TestNewPipelineHandler_WithRunner_NilStaysNil(t *testing.T) {
	handler := NewPipelineHandler(WithBacktestRunner(nil))
	assert.Nil(t, handler.runner)
}

// TestPipelineHandler_ImplementsRunnerContract is a compile-time check that
// stubBacktestRunner satisfies the pipeline.BacktestRunner interface.
func TestPipelineHandler_ImplementsRunnerContract(t *testing.T) {
	var _ pipeline.BacktestRunner = (*stubBacktestRunner)(nil)
}
