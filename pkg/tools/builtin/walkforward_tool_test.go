package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
)

// ─── mock WalkForwardRunner ────────────────────────────────────────────

// mockWFRunner is a test double for the WalkForwardRunner interface.
// It records the last call's arguments and returns either a canned
// report or an error.
type mockWFRunner struct {
	report *domain.WalkForwardReport
	err    error

	// Recorded call arguments.
	lastStrategy string
	lastPool     []string
	lastStart    string
	lastEnd      string
	lastParams   domain.WalkForwardParams
	calls        int
}

func (m *mockWFRunner) RunWalkForward(
	ctx context.Context,
	strategyName string,
	stockPool []string,
	startDate, endDate string,
	params domain.WalkForwardParams,
) (*domain.WalkForwardReport, error) {
	m.calls++
	m.lastStrategy = strategyName
	m.lastPool = stockPool
	m.lastStart = startDate
	m.lastEnd = endDate
	m.lastParams = params
	if m.err != nil {
		return nil, m.err
	}
	if m.report != nil {
		return m.report, nil
	}
	// Default canned report.
	return &domain.WalkForwardReport{
		StrategyID:     strategyName,
		Windows:        []*domain.WalkForwardResult{{WindowIndex: 0, TrainSharpe: 1.2, TestSharpe: 0.9, TestReturn: 0.08, TestMaxDrawdown: -0.05, OOSvsTrain: 0.75}},
		AvgTestSharpe:  0.9,
		AvgTestReturn:  0.08,
		AvgDegradation: 0.75,
		OverfitScore:   0.25,
		OverallPass:    true,
		PassRate:       1.0,
	}, nil
}

// ─── Name / Description / Parameters / OutputSchema ────────────────────

func TestWalkForwardValidateTool_Name(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	assert.Equal(t, "walk_forward_validate", tt.Name())
}

func TestWalkForwardValidateTool_Description(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	desc := tt.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "walk-forward")
	assert.Contains(t, desc, "L4 gate")
}

func TestWalkForwardValidateTool_Parameters(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	params := tt.Parameters()
	require.Len(t, params, 6)

	// First 4 are required, last 2 are optional with defaults.
	required := []string{"strategy_name", "stock_pool", "start_date", "end_date"}
	for i, name := range required {
		assert.Equal(t, name, params[i].Name, "param %d name", i)
		assert.True(t, params[i].Required, "param %q should be required", name)
	}

	assert.Equal(t, "train_days", params[4].Name)
	assert.False(t, params[4].Required)
	assert.Equal(t, 250, params[4].Default)

	assert.Equal(t, "test_days", params[5].Name)
	assert.False(t, params[5].Required)
	assert.Equal(t, 60, params[5].Default)
}

func TestWalkForwardValidateTool_OutputSchema(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	schema := tt.OutputSchema()
	assert.Equal(t, "object", schema.Type)
	require.NotEmpty(t, schema.Fields)

	expected := map[string]bool{
		"strategy_id":     true,
		"num_windows":     true,
		"avg_test_sharpe": true,
		"avg_degradation": true,
		"overfit_score":   true,
		"overall_pass":    true,
		"pass_rate":       true,
		"windows":         true,
	}
	for _, f := range schema.Fields {
		delete(expected, f.Name)
	}
	assert.Empty(t, expected, "missing fields in schema: %v", expected)
}

// ─── Execute happy path ─────────────────────────────────────────────────

func TestWalkForwardValidateTool_Execute_HappyPath(t *testing.T) {
	canned := &domain.WalkForwardReport{
		StrategyID:     "momentum",
		Windows:        []*domain.WalkForwardResult{{WindowIndex: 0, TrainSharpe: 1.5, TestSharpe: 1.1, TestReturn: 0.12, TestMaxDrawdown: -0.07, OOSvsTrain: 0.73}},
		AvgTestSharpe:  1.1,
		AvgDegradation: 0.73,
		OverfitScore:   0.2,
		OverallPass:    true,
		PassRate:       1.0,
	}
	runner := &mockWFRunner{report: canned}
	tt := NewWalkForwardValidateTool(runner)

	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ", "600000.SH"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
	}

	result, err := tt.Execute(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	summary, ok := result.(*walkForwardSummary)
	require.True(t, ok, "result should be *walkForwardSummary, got %T", result)
	assert.Equal(t, "momentum", summary.StrategyID)
	assert.Equal(t, 1, summary.NumWindows)
	assert.InDelta(t, 1.1, summary.AvgTestSharpe, 1e-9)
	assert.InDelta(t, 0.73, summary.AvgDegradation, 1e-9)
	assert.True(t, summary.OverallPass)
	assert.InDelta(t, 1.0, summary.PassRate, 1e-9)
	require.Len(t, summary.Windows, 1)
	assert.InDelta(t, 1.5, summary.Windows[0].TrainSharpe, 1e-9)
	assert.InDelta(t, 1.1, summary.Windows[0].TestSharpe, 1e-9)

	// Verify runner was called with correct args.
	assert.Equal(t, 1, runner.calls)
	assert.Equal(t, "momentum", runner.lastStrategy)
	assert.Equal(t, []string{"000001.SZ", "600000.SH"}, runner.lastPool)
	assert.Equal(t, "2020-01-01", runner.lastStart)
	assert.Equal(t, "2024-01-01", runner.lastEnd)
	// Defaults: train_days=250, test_days=60, step_days=60.
	assert.Equal(t, 250, runner.lastParams.TrainDays)
	assert.Equal(t, 60, runner.lastParams.TestDays)
	assert.Equal(t, 60, runner.lastParams.StepDays)
}

func TestWalkForwardValidateTool_Execute_CustomWindowSizes(t *testing.T) {
	runner := &mockWFRunner{}
	tt := NewWalkForwardValidateTool(runner)

	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
		"train_days":    180,
		"test_days":     45,
	}

	_, err := tt.Execute(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, 180, runner.lastParams.TrainDays)
	assert.Equal(t, 45, runner.lastParams.TestDays)
	assert.Equal(t, 45, runner.lastParams.StepDays, "step_days should equal test_days for non-overlapping windows")
}

func TestWalkForwardValidateTool_Execute_TrainDaysAsFloat(t *testing.T) {
	// JSON-decoded numbers come as float64.
	runner := &mockWFRunner{}
	tt := NewWalkForwardValidateTool(runner)

	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
		"train_days":    float64(200),
		"test_days":     float64(50),
	}

	_, err := tt.Execute(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, 200, runner.lastParams.TrainDays)
	assert.Equal(t, 50, runner.lastParams.TestDays)
}

// ─── Execute missing/wrong-typed args ───────────────────────────────────

func TestWalkForwardValidateTool_Execute_MissingStrategyName(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"stock_pool": []string{"000001.SZ"},
		"start_date": "2020-01-01",
		"end_date":   "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "strategy_name")
}

func TestWalkForwardValidateTool_Execute_MissingStockPool(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"strategy_name": "momentum",
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "stock_pool")
}

func TestWalkForwardValidateTool_Execute_MissingStartDate(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"end_date":      "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "start_date")
}

func TestWalkForwardValidateTool_Execute_MissingEndDate(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "end_date")
}

func TestWalkForwardValidateTool_Execute_ZeroTrainDays(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
		"train_days":    0,
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "train_days")
}

func TestWalkForwardValidateTool_Execute_NegativeTestDays(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
		"test_days":     -10,
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "test_days")
}

func TestWalkForwardValidateTool_Execute_WrongTypeTrainDays(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
		"train_days":    "250", // string, not int
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "train_days")
}

// ─── Execute runner error ───────────────────────────────────────────────

func TestWalkForwardValidateTool_Execute_RunnerError(t *testing.T) {
	runnerErr := errors.New("insufficient trading days: need 310, got 100")
	runner := &mockWFRunner{err: runnerErr}
	tt := NewWalkForwardValidateTool(runner)

	args := map[string]interface{}{
		"strategy_name": "momentum",
		"stock_pool":    []string{"000001.SZ"},
		"start_date":    "2020-01-01",
		"end_date":      "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	// Runner error propagates verbatim — NOT ErrInvalidArgs.
	assert.True(t, errors.Is(err, runnerErr))
	assert.False(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "walk_forward_validate")
}

// ─── summarizeWalkForwardReport unit tests ──────────────────────────────

func TestSummarizeWalkForwardReport_NilInput(t *testing.T) {
	summary := summarizeWalkForwardReport(nil)
	assert.Nil(t, summary)
}

func TestSummarizeWalkForwardReport_EmptyWindows(t *testing.T) {
	report := &domain.WalkForwardReport{
		StrategyID:    "test",
		Windows:       []*domain.WalkForwardResult{},
		AvgTestSharpe: 0.5,
		OverallPass:   false,
	}
	summary := summarizeWalkForwardReport(report)
	require.NotNil(t, summary)
	assert.Equal(t, "test", summary.StrategyID)
	assert.Equal(t, 0, summary.NumWindows)
	assert.Empty(t, summary.Windows)
	assert.InDelta(t, 0.5, summary.AvgTestSharpe, 1e-9)
}

func TestSummarizeWalkForwardReport_NilWindowsSkipped(t *testing.T) {
	report := &domain.WalkForwardReport{
		StrategyID: "test",
		Windows: []*domain.WalkForwardResult{
			{WindowIndex: 0, TrainSharpe: 1.0, TestSharpe: 0.8},
			nil, // should be skipped
			{WindowIndex: 2, TrainSharpe: 1.2, TestSharpe: 0.9},
		},
	}
	summary := summarizeWalkForwardReport(report)
	require.NotNil(t, summary)
	require.Len(t, summary.Windows, 2, "nil windows should be skipped")
	assert.Equal(t, 0, summary.Windows[0].WindowIndex)
	assert.Equal(t, 2, summary.Windows[1].WindowIndex)
}

// ─── Construction ───────────────────────────────────────────────────────

func TestNewWalkForwardValidateTool_NilRunnerPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewWalkForwardValidateTool(nil)
	})
}

// ─── Registry integration ──────────────────────────────────────────────

func TestWalkForwardValidateTool_RegisterInRegistry(t *testing.T) {
	tt := NewWalkForwardValidateTool(&mockWFRunner{})
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tt))

	got, err := reg.Get("walk_forward_validate")
	require.NoError(t, err)
	assert.Equal(t, "walk_forward_validate", got.Name())

	info := reg.List()
	require.Len(t, info, 1)
	assert.Equal(t, "walk_forward_validate", info[0].Name)
	assert.Len(t, info[0].Parameters, 6)
}
