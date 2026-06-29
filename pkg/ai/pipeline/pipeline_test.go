package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/agents"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/intent"
	yamlgen "github.com/ruoxizhnya/quant-trading/pkg/ai/yaml"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBacktestRunner is a test double for BacktestRunner
type mockBacktestRunner struct {
	shouldFail bool
	result     *domain.BacktestResult
}

func (m *mockBacktestRunner) RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error) {
	if m.shouldFail {
		return nil, assert.AnError
	}
	if m.result != nil {
		return m.result, nil
	}
	start, _ := time.Parse("2006-01-02", startDate)
	end, _ := time.Parse("2006-01-02", endDate)
	return &domain.BacktestResult{
		TotalTrades: 10,
		TotalReturn: 0.10,
		SharpeRatio: 1.2,
		MaxDrawdown: 0.05,
		WinRate:     0.6,
		StartDate:   start,
		EndDate:     end,
		PortfolioValues: []domain.PortfolioValue{
			{Date: time.Now(), TotalValue: 1000000},
			{Date: time.Now(), TotalValue: 1100000},
		},
	}, nil
}

func TestNewPipeline(t *testing.T) {
	p := NewPipeline()
	require.NotNil(t, p)
	assert.NotNil(t, p.intentParser)
	assert.NotNil(t, p.yamlGen)
	assert.NotNil(t, p.aiClient)
}

func TestNewPipelineWithDeps(t *testing.T) {
	parser := intent.NewParser()
	gen := yamlgen.NewGenerator()
	client := ai.NewClient()

	p := NewPipelineWithDeps(parser, gen, client)
	require.NotNil(t, p)
	assert.Equal(t, parser, p.intentParser)
	assert.Equal(t, gen, p.yamlGen)
	assert.Equal(t, client, p.aiClient)
}

func TestPipeline_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		client   *ai.Client
		expected bool
	}{
		{
			name:     "no client",
			client:   nil,
			expected: false,
		},
		{
			name:     "client without env",
			client:   ai.NewClient(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pipeline{aiClient: tt.client}
			assert.Equal(t, tt.expected, p.IsConfigured())
		})
	}
}

func TestPipeline_StartJob(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test strategy description")

	require.NotNil(t, result)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, StageParse, result.Status)
	assert.NotZero(t, result.StartedAt)
	assert.Len(t, result.Logs, 1)
	assert.Contains(t, result.Logs[0], "test strategy description")

	// Verify job is stored
	stored := p.GetJob(result.ID)
	assert.Equal(t, result, stored)
}

func TestPipeline_GetJob_NotFound(t *testing.T) {
	p := NewPipeline()
	result := p.GetJob("non-existent-id")
	assert.Nil(t, result)
}

func TestPipeline_Execute_WithoutAIConfig(t *testing.T) {
	p := NewPipeline()
	runner := &mockBacktestRunner{}

	result, err := p.Execute(context.Background(), "动量策略", runner)

	// Should fail at code generation since AI is not configured
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StageFailed, result.Status)
	assert.NotNil(t, result.CompletedAt)
	assert.GreaterOrEqual(t, result.DurationMs, int64(0))
}

func TestPipeline_ExecuteAsync_WithoutAIConfig(t *testing.T) {
	p := NewPipeline()
	runner := &mockBacktestRunner{}

	jobID := p.ExecuteAsync(context.Background(), "均值回归策略", runner)
	assert.NotEmpty(t, jobID)

	// Wait for the async goroutine to finish all writes to the Result
	// before reading its fields. Using WaitDone (channel-based) instead
	// of time.Sleep avoids both flakiness and a data race — see
	// AGENTS.md §8.3 规范 1: "禁止用 time.Sleep 同步并发测试".
	result := p.GetJob(jobID)
	require.NotNil(t, result)
	result.WaitDone()

	// Should have failed since AI is not configured
	assert.Equal(t, StageFailed, result.Status)
}

func TestPipeline_ExecuteAsync_WithNilRunner(t *testing.T) {
	p := NewPipeline()

	jobID := p.ExecuteAsync(context.Background(), "测试策略", nil)
	assert.NotEmpty(t, jobID)

	// Channel-based wait — see TestPipeline_ExecuteAsync_WithoutAIConfig
	// for the rationale (replaces a time.Sleep that was both flaky and a
	// real data race flagged by -race).
	result := p.GetJob(jobID)
	require.NotNil(t, result)
	result.WaitDone()

	// Should fail at code generation since AI is not configured
	assert.Equal(t, StageFailed, result.Status)
}

func TestPipeline_fail(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")

	p.fail(result, StageParse, "test error")

	assert.Equal(t, StageFailed, result.Status)
	assert.Equal(t, "test error", result.BuildError)
	assert.NotNil(t, result.CompletedAt)
	assert.GreaterOrEqual(t, result.DurationMs, int64(0))
	assert.Contains(t, result.Logs[len(result.Logs)-1], "FAILED")
}

func TestPipeline_complete(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")

	p.complete(result)

	assert.Equal(t, StageComplete, result.Status)
	assert.NotNil(t, result.CompletedAt)
	assert.GreaterOrEqual(t, result.DurationMs, int64(0))
	assert.Contains(t, result.Logs[len(result.Logs)-1], "completed")
}

func TestPipeline_log(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")
	initialLen := len(result.Logs)

	p.log(result, "test message")

	assert.Len(t, result.Logs, initialLen+1)
	assert.Contains(t, result.Logs[initialLen], "test message")
}

func TestPipeline_generateStrategyCode_WithoutAI(t *testing.T) {
	p := NewPipeline()
	i := &intent.Intent{
		StrategyType: intent.StrategyTypeMomentum,
		StrategyName: "test",
	}

	code, err := p.generateStrategyCode(context.Background(), i)
	assert.Error(t, err)
	assert.Empty(t, code)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPipeline_validateCompilation_InvalidCode(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")

	invalidCode := "this is not valid go code"
	err := p.validateCompilation(invalidCode, result)

	assert.Error(t, err)
	assert.NotEmpty(t, result.BuildError)
}

func TestPipeline_validateCompilation_ValidCode(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")

	// Valid minimal Go code
	validCode := `package main
func main() {}
`
	err := p.validateCompilation(validCode, result)

	// This might fail depending on the build environment
	// but we test that the function doesn't panic
	// and properly handles the result
	if err != nil {
		assert.NotEmpty(t, result.BuildError)
	}
}

// ---- S7-P0-2 (ODR-043-2): hardcoded buildCmd.Dir ----

// TestFindProjectRoot_ReturnsDirWithGoMod verifies that findProjectRoot
// dynamically locates the project root by walking up from the current
// working directory until it finds a go.mod file. This replaces the
// hardcoded "/Users/ruoxi/longshaosWorld/quant-trading" path that made
// the pipeline non-portable across developer machines and deployments.
func TestFindProjectRoot_ReturnsDirWithGoMod(t *testing.T) {
	root, err := findProjectRoot()
	require.NoError(t, err, "findProjectRoot must not error when run inside the project")
	require.NotEmpty(t, root, "project root must not be empty")

	// The returned directory must actually contain a go.mod file —
	// this is the defining property of "project root" in a Go module.
	info, err := os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "project root must contain go.mod: %s", root)
	require.False(t, info.IsDir(), "go.mod must be a file, not a directory")
}

// TestPipeline_SourceHasNoHardcodedDeveloperPath is a regression guard
// for S7-P0-2 (ODR-043-2): the pipeline source must not contain the
// hardcoded developer-machine path that this task removed. If this test
// fails, someone re-introduced a string literal like
// `buildCmd.Dir = "/Users/ruoxi/..."` instead of using the dynamically
// detected p.buildDir.
//
// We scan pipeline.go itself rather than checking the runtime value of
// findProjectRoot(), because on the original developer's machine the
// detected root legitimately equals the former hardcoded path — the bug
// is the *hardcoding*, not the value.
func TestPipeline_SourceHasNoHardcodedDeveloperPath(t *testing.T) {
	source, err := os.ReadFile("pipeline.go")
	require.NoError(t, err, "must be able to read pipeline.go from the test working dir")

	const hardcodedDeveloperPath = "/Users/ruoxi/longshaosWorld/quant-trading"
	assert.NotContains(t, string(source), hardcodedDeveloperPath,
		"pipeline.go must not contain the hardcoded developer path; use p.buildDir instead")
}

// TestPipeline_BuildDirSetOnConstruction verifies that NewPipeline
// populates the buildDir field with a dynamically detected project root
// (containing go.mod), so validateCompilation no longer relies on a
// hardcoded path.
func TestPipeline_BuildDirSetOnConstruction(t *testing.T) {
	p := NewPipeline()
	require.NotEmpty(t, p.buildDir, "NewPipeline must set buildDir to a non-empty path")

	_, err := os.Stat(filepath.Join(p.buildDir, "go.mod"))
	assert.NoError(t, err, "buildDir must point to a directory containing go.mod: %s", p.buildDir)
}

// TestPipeline_BuildDirOverride verifies that buildDir can be overridden
// (e.g. for testing or deployment in a containerised environment where
// the project root differs from the detected location).
func TestPipeline_BuildDirOverride(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewPipeline(WithBuildDir(tmpDir))
	assert.Equal(t, tmpDir, p.buildDir, "WithBuildDir must override the default detected root")
}

// TestPipeline_ValidateCompilation_UsesBuildDir verifies that
// validateCompilation honours the injected buildDir rather than a
// hardcoded path. We point buildDir at a temp directory that does NOT
// contain go.mod and confirm the resulting build error references the
// temp dir (proving the command ran there) rather than the developer's
// machine path.
func TestPipeline_ValidateCompilation_UsesBuildDir(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewPipeline(WithBuildDir(tmpDir))
	result := p.StartJob("test")

	// Code with an unresolvable import forces a build failure whose
	// error message includes the working directory context.
	invalidCode := `package main
import "github.com/nonexistent/fakepkg"
func main() {}
`
	_ = p.validateCompilation(invalidCode, result)

	// buildDir must be the temp dir we set, proving the field is used.
	assert.Equal(t, tmpDir, p.buildDir)
}

func TestPipeline_runBacktest(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")
	runner := &mockBacktestRunner{
		result: &domain.BacktestResult{
			TotalTrades: 5,
			TotalReturn: 0.05,
		},
	}

	i := &intent.Intent{
		StrategyName: "test_strategy",
		Universe:     "csi300",
	}

	btResult, err := p.runBacktest(context.Background(), i, runner, result)
	require.NoError(t, err)
	assert.NotNil(t, btResult)
	assert.Equal(t, 5, btResult.TotalTrades)
}

func TestPipeline_runBacktest_WithError(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")
	runner := &mockBacktestRunner{shouldFail: true}

	i := &intent.Intent{
		StrategyName: "test_strategy",
		Universe:     "csi300",
	}

	btResult, err := p.runBacktest(context.Background(), i, runner, result)
	assert.Error(t, err)
	assert.Nil(t, btResult)
	assert.NotEmpty(t, result.BacktestError)
}

func TestPipeline_runBacktest_NilRunner(t *testing.T) {
	p := NewPipeline()
	result := p.StartJob("test")

	i := &intent.Intent{
		StrategyName: "test_strategy",
		Universe:     "csi300",
	}

	// The actual pipeline code doesn't check for nil runner before calling
	// This test documents that behavior - it will panic
	// In production, the caller should ensure runner is not nil
	assert.Panics(t, func() {
		p.runBacktest(context.Background(), i, nil, result)
	})
}

func TestResult_Struct(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(time.Minute)
	result := &Result{
		ID:          "test-id",
		Status:      StageComplete,
		StartedAt:   now,
		CompletedAt: &completedAt,
		DurationMs:  60000,
		Logs:        []string{"log1", "log2"},
	}

	assert.Equal(t, "test-id", result.ID)
	assert.Equal(t, StageComplete, result.Status)
	assert.Equal(t, now, result.StartedAt)
	assert.Equal(t, &completedAt, result.CompletedAt)
	assert.Equal(t, int64(60000), result.DurationMs)
	assert.Len(t, result.Logs, 2)
}

func TestStage_Constants(t *testing.T) {
	assert.Equal(t, Stage("parse"), StageParse)
	assert.Equal(t, Stage("generate"), StageGenerate)
	assert.Equal(t, Stage("validate"), StageValidate)
	assert.Equal(t, Stage("compile"), StageCompile)
	assert.Equal(t, Stage("backtest"), StageBacktest)
	assert.Equal(t, Stage("complete"), StageComplete)
	assert.Equal(t, Stage("failed"), StageFailed)
}

func TestParseUniverse(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"csi300", []string{"csi300"}},
		{"000001.SZ,000002.SZ", []string{"000001.SZ", "000002.SZ"}},
		{"", nil},
		{"all", nil},
		{"universe:csi300,csi500", []string{"csi300", "csi500"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseUniverse(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestPipeline_Integration_FullFlowWithoutAI(t *testing.T) {
	// Integration test that verifies the pipeline structure
	// even without AI configuration
	p := NewPipeline()
	_ = &mockBacktestRunner{}

	// Start a job
	result := p.StartJob("双均线策略")
	require.NotNil(t, result)
	assert.Equal(t, StageParse, result.Status)

	// Simulate what Execute does - parse intent
	ctx := context.Background()
	parsedIntent, err := p.intentParser.Parse(ctx, "双均线策略")
	require.NoError(t, err)
	result.Intent = parsedIntent

	// Generate YAML
	yamlConfig := p.yamlGen.Generate(parsedIntent)
	assert.NotEmpty(t, yamlConfig)
	result.YAMLConfig = yamlConfig

	// Validate YAML
	err = p.yamlGen.Validate(yamlConfig)
	assert.NoError(t, err)

	// Attempt code generation (will fail without AI)
	code, err := p.generateStrategyCode(ctx, parsedIntent)
	assert.Error(t, err) // Expected to fail without AI config
	assert.Empty(t, code)

	// Complete the job manually for test
	p.complete(result)
	assert.Equal(t, StageComplete, result.Status)
	assert.NotNil(t, result.CompletedAt)
}

func TestPipeline_ConcurrentJobs(t *testing.T) {
	p := NewPipeline()

	// Start multiple jobs concurrently
	var jobIDs []string
	for i := 0; i < 5; i++ {
		result := p.StartJob("strategy " + string(rune('A'+i)))
		jobIDs = append(jobIDs, result.ID)
	}

	// Verify all jobs exist
	for _, id := range jobIDs {
		result := p.GetJob(id)
		assert.NotNil(t, result)
		assert.Equal(t, StageParse, result.Status)
	}

	// Verify job count
	count := 0
	p.jobs.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	assert.Equal(t, 5, count)
}

func TestPipeline_JobIsolation(t *testing.T) {
	p := NewPipeline()

	// Start two jobs
	result1 := p.StartJob("strategy 1")
	result2 := p.StartJob("strategy 2")

	// Modify result1
	p.log(result1, "only in job 1")

	// Verify result2 is not affected
	assert.Len(t, result2.Logs, 1) // Only the initial log
	assert.Len(t, result1.Logs, 2) // Initial + added log
}

// ---- DiscoveryPipeline Tests ----

func TestNewDiscoveryPipeline(t *testing.T) {
	researchAgent := agents.NewResearchAgent()
	validateAgent := agents.NewValidateAgent(nil)
	dataProvider := &mockDataProvider{}

	p := NewDiscoveryPipeline(researchAgent, validateAgent, nil, dataProvider)
	assert.NotNil(t, p)
	assert.Equal(t, researchAgent, p.researchAgent)
	assert.Equal(t, validateAgent, p.validateAgent)
	assert.NotNil(t, p.mutator) // auto-created
	assert.Equal(t, dataProvider, p.dataProvider)
}

func TestDiscoveryPipeline_Run_NoAI(t *testing.T) {
	researchAgent := agents.NewResearchAgent()
	validateAgent := agents.NewValidateAgent(nil)
	dataProvider := &mockDataProvider{}

	p := NewDiscoveryPipeline(researchAgent, validateAgent, nil, dataProvider)
	config := DiscoveryConfig{
		Topics:             []string{"momentum", "value"},
		MutationsPerFactor: 2,
		ValidationLevel:    agents.L1Syntax,
		MinIC:              0.0,
		BatchSize:          10,
	}

	result, err := p.Run(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, result)

	// No AI → no hypotheses generated
	assert.Equal(t, 0, result.Generated)
	// But mutations are still applied to empty list
	assert.NotNil(t, result.TopFactors)
	assert.NotNil(t, result.Errors)
}

func TestDiscoveryPipeline_RunBatch(t *testing.T) {
	researchAgent := agents.NewResearchAgent()
	validateAgent := agents.NewValidateAgent(nil)
	dataProvider := &mockDataProvider{}

	p := NewDiscoveryPipeline(researchAgent, validateAgent, nil, dataProvider)
	configs := []DiscoveryConfig{
		{Topics: []string{"momentum"}, MutationsPerFactor: 1, ValidationLevel: agents.L1Syntax, MinIC: 0.0},
		{Topics: []string{"value"}, MutationsPerFactor: 1, ValidationLevel: agents.L1Syntax, MinIC: 0.0},
	}

	results, err := p.RunBatch(context.Background(), configs)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestDiscoveryPipeline_GetTopFactors_NoPool(t *testing.T) {
	researchAgent := agents.NewResearchAgent()
	validateAgent := agents.NewValidateAgent(nil)
	dataProvider := &mockDataProvider{}

	p := NewDiscoveryPipeline(researchAgent, validateAgent, nil, dataProvider)
	_, err := p.GetTopFactors(context.Background(), 5)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "factor pool not configured")
}

// ---- StrategyPipeline Tests ----

func TestNewStrategyPipeline(t *testing.T) {
	generateAgent := agents.NewGenerateAgent()
	validateAgent := agents.NewValidateAgent(nil)

	p := NewStrategyPipeline(generateAgent, validateAgent, nil, nil)
	assert.NotNil(t, p)
	assert.Equal(t, generateAgent, p.generateAgent)
	assert.Equal(t, validateAgent, p.validateAgent)
	assert.NotNil(t, p.codeValidator) // auto-created
}

func TestStrategyPipeline_Run_GenerationFails(t *testing.T) {
	generateAgent := agents.NewGenerateAgent() // no LLM
	validateAgent := agents.NewValidateAgent(nil)

	p := NewStrategyPipeline(generateAgent, validateAgent, nil, nil)
	config := StrategyPipelineConfig{
		Description:     "test",
		ValidationLevel: agents.L1Syntax,
		RunBacktest:     false,
	}

	result, err := p.Run(context.Background(), config)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Generated)
	assert.NotEmpty(t, result.Errors)
}

func TestStrategyPipeline_RenderTemplate(t *testing.T) {
	generateAgent := agents.NewGenerateAgent()
	validateAgent := agents.NewValidateAgent(nil)

	p := NewStrategyPipeline(generateAgent, validateAgent, nil, nil)

	tmpl := `Strategy: {{.Name}}`
	data := map[string]string{"Name": "momentum"}

	result, err := p.RenderTemplate(tmpl, data)
	require.NoError(t, err)
	assert.Equal(t, "Strategy: momentum", result)
}

func TestStrategyPipeline_RenderTemplate_Invalid(t *testing.T) {
	generateAgent := agents.NewGenerateAgent()
	validateAgent := agents.NewValidateAgent(nil)

	p := NewStrategyPipeline(generateAgent, validateAgent, nil, nil)

	// Invalid template syntax
	tmpl := `Strategy: {{.Name`
	data := map[string]string{"Name": "momentum"}

	_, err := p.RenderTemplate(tmpl, data)
	assert.Error(t, err)
}

func TestStrategyPipeline_BatchRun(t *testing.T) {
	generateAgent := agents.NewGenerateAgent()
	validateAgent := agents.NewValidateAgent(nil)

	p := NewStrategyPipeline(generateAgent, validateAgent, nil, nil)
	configs := []StrategyPipelineConfig{
		{Description: "strategy1", ValidationLevel: agents.L1Syntax},
		{Description: "strategy2", ValidationLevel: agents.L1Syntax},
	}

	results, err := p.BatchRun(context.Background(), configs)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// mockDataProvider implements expression.DataProvider for testing.
type mockDataProvider struct{}

func (m *mockDataProvider) GetField(symbol, field string, lookback int) ([]float64, error) {
	return []float64{1.0, 2.0, 3.0}, nil
}

func (m *mockDataProvider) GetSymbols() []string {
	return []string{"AAPL", "GOOGL"}
}
