package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/intent"
	yamlgen "github.com/ruoxizhnya/quant-trading/pkg/ai/yaml"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Stage represents a pipeline stage
type Stage string

const (
	StageParse    Stage = "parse"
	StageGenerate Stage = "generate"
	StageValidate Stage = "validate"
	StageCompile  Stage = "compile"
	StageBacktest Stage = "backtest"
	StageComplete Stage = "complete"
	StageFailed   Stage = "failed"
)

// Result holds the outcome of a pipeline execution
type Result struct {
	ID             string                 `json:"id"`
	Status         Stage                  `json:"status"`
	Intent         *intent.Intent         `json:"intent,omitempty"`
	YAMLConfig     string                 `json:"yaml_config,omitempty"`
	GeneratedCode  string                 `json:"generated_code,omitempty"`
	BuildError     string                 `json:"build_error,omitempty"`
	BacktestResult *domain.BacktestResult `json:"backtest_result,omitempty"`
	BacktestError  string                 `json:"backtest_error,omitempty"`
	StartedAt      time.Time              `json:"started_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	DurationMs     int64                  `json:"duration_ms"`
	Logs           []string               `json:"logs,omitempty"`
}

// BacktestRunner is the interface for running backtests
type BacktestRunner interface {
	RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error)
}

// Pipeline orchestrates the full strategy generation and validation flow
type Pipeline struct {
	intentParser *intent.Parser
	yamlGen      *yamlgen.Generator
	aiClient     *ai.Client
	jobs         sync.Map // jobID -> *Result
}

// NewPipeline creates a new pipeline instance
func NewPipeline() *Pipeline {
	return &Pipeline{
		intentParser: intent.NewParser(),
		yamlGen:      yamlgen.NewGenerator(),
		aiClient:     ai.NewClient(),
	}
}

// NewPipelineWithDeps creates a pipeline with specific dependencies
func NewPipelineWithDeps(parser *intent.Parser, gen *yamlgen.Generator, client *ai.Client) *Pipeline {
	return &Pipeline{
		intentParser: parser,
		yamlGen:      gen,
		aiClient:     client,
	}
}

// IsConfigured returns true if the pipeline can execute (AI client configured)
func (p *Pipeline) IsConfigured() bool {
	return p.aiClient != nil && p.aiClient.IsConfigured()
}

// Execute runs the full pipeline synchronously and returns the result
func (p *Pipeline) Execute(ctx context.Context, description string, runner BacktestRunner) (*Result, error) {
	result := p.StartJob(description)

	// Stage 1: Parse intent
	p.log(result, "Stage 1/5: Parsing intent...")
	parsedIntent, err := p.intentParser.Parse(ctx, description)
	if err != nil {
		p.fail(result, StageParse, fmt.Sprintf("Intent parsing failed: %v", err))
		return result, err
	}
	result.Intent = parsedIntent
	p.log(result, fmt.Sprintf("Parsed intent: type=%s, name=%s", parsedIntent.StrategyType, parsedIntent.StrategyName))

	// Stage 2: Generate YAML
	p.log(result, "Stage 2/5: Generating YAML configuration...")
	yamlConfig := p.yamlGen.Generate(parsedIntent)
	if yamlConfig == "" {
		p.fail(result, StageGenerate, "YAML generation failed: empty output")
		return result, fmt.Errorf("yaml generation failed")
	}
	result.YAMLConfig = yamlConfig
	p.log(result, "YAML configuration generated successfully")

	// Stage 3: Generate strategy code via LLM
	p.log(result, "Stage 3/5: Generating strategy code...")
	code, err := p.generateStrategyCode(ctx, parsedIntent)
	if err != nil {
		p.fail(result, StageGenerate, fmt.Sprintf("Code generation failed: %v", err))
		return result, err
	}
	result.GeneratedCode = code
	p.log(result, "Strategy code generated successfully")

	// Stage 4: Compile validation
	p.log(result, "Stage 4/5: Validating compilation...")
	compileErr := p.validateCompilation(code, result)
	if compileErr != nil {
		p.fail(result, StageCompile, fmt.Sprintf("Compilation failed: %v", compileErr))
		return result, compileErr
	}
	p.log(result, "Compilation validation passed")

	// Stage 5: Backtest (if runner provided)
	if runner != nil {
		p.log(result, "Stage 5/5: Running backtest...")
		btResult, err := p.runBacktest(ctx, parsedIntent, runner, result)
		if err != nil {
			p.fail(result, StageBacktest, fmt.Sprintf("Backtest failed: %v", err))
			return result, err
		}
		result.BacktestResult = btResult
		p.log(result, "Backtest completed successfully")
	} else {
		p.log(result, "Stage 5/5: Skipping backtest (no runner provided)")
	}

	// Complete
	p.complete(result)
	return result, nil
}

// ExecuteAsync starts the pipeline asynchronously and returns the job ID
func (p *Pipeline) ExecuteAsync(ctx context.Context, description string, runner BacktestRunner) string {
	result := p.StartJob(description)

	go func() {
		// Stage 1: Parse intent
		p.log(result, "Stage 1/5: Parsing intent...")
		parsedIntent, err := p.intentParser.Parse(ctx, description)
		if err != nil {
			p.fail(result, StageParse, fmt.Sprintf("Intent parsing failed: %v", err))
			return
		}
		result.Intent = parsedIntent
		p.log(result, fmt.Sprintf("Parsed intent: type=%s, name=%s", parsedIntent.StrategyType, parsedIntent.StrategyName))

		// Stage 2: Generate YAML
		p.log(result, "Stage 2/5: Generating YAML configuration...")
		yamlConfig := p.yamlGen.Generate(parsedIntent)
		if yamlConfig == "" {
			p.fail(result, StageGenerate, "YAML generation failed: empty output")
			return
		}
		result.YAMLConfig = yamlConfig
		p.log(result, "YAML configuration generated successfully")

		// Stage 3: Generate strategy code
		p.log(result, "Stage 3/5: Generating strategy code...")
		code, err := p.generateStrategyCode(ctx, parsedIntent)
		if err != nil {
			p.fail(result, StageGenerate, fmt.Sprintf("Code generation failed: %v", err))
			return
		}
		result.GeneratedCode = code
		p.log(result, "Strategy code generated successfully")

		// Stage 4: Compile validation
		p.log(result, "Stage 4/5: Validating compilation...")
		compileErr := p.validateCompilation(code, result)
		if compileErr != nil {
			p.fail(result, StageCompile, fmt.Sprintf("Compilation failed: %v", compileErr))
			return
		}
		p.log(result, "Compilation validation passed")

		// Stage 5: Backtest
		if runner != nil {
			p.log(result, "Stage 5/5: Running backtest...")
			btResult, err := p.runBacktest(ctx, parsedIntent, runner, result)
			if err != nil {
				p.fail(result, StageBacktest, fmt.Sprintf("Backtest failed: %v", err))
				return
			}
			result.BacktestResult = btResult
			p.log(result, "Backtest completed successfully")
		} else {
			p.log(result, "Stage 5/5: Skipping backtest (no runner provided)")
		}

		p.complete(result)
	}()

	return result.ID
}

// StartJob creates a new pipeline job
func (p *Pipeline) StartJob(description string) *Result {
	jobID := uuid.New().String()
	now := time.Now()
	result := &Result{
		ID:        jobID,
		Status:    StageParse,
		StartedAt: now,
		Logs:      []string{fmt.Sprintf("Pipeline started for: %s", description)},
	}
	p.jobs.Store(jobID, result)
	return result
}

// GetJob retrieves a job result by ID
func (p *Pipeline) GetJob(jobID string) *Result {
	val, ok := p.jobs.Load(jobID)
	if !ok {
		return nil
	}
	return val.(*Result)
}

// generateStrategyCode generates Go strategy code from intent
func (p *Pipeline) generateStrategyCode(ctx context.Context, i *intent.Intent) (string, error) {
	if p.aiClient == nil || !p.aiClient.IsConfigured() {
		return "", fmt.Errorf("AI client not configured")
	}

	prompt := fmt.Sprintf(`Generate a complete Go trading strategy implementing the strategy.Strategy interface.

Strategy Type: %s
Strategy Name: %s
Description: %s
Indicators: %v
Parameters: %v
Universe: %s
Timeframe: %s

Requirements:
1. Package must be "plugins"
2. Implement all Strategy interface methods
3. Use domain.OHLCV for price data
4. Return []strategy.Signal with Action "buy" or "sell"
5. Include init() function that calls strategy.GlobalRegister()
6. No fmt.Print, log.Print, or side effects
7. Keep under 100 lines

Output ONLY the Go source code, no explanations.`, i.StrategyType, i.StrategyName, i.Description, i.Indicators, i.Parameters, i.Universe, i.Timeframe)

	messages := []ai.ChatMessage{
		{Role: "system", Content: "You are an expert quantitative trading strategy developer for A-share market."},
		{Role: "user", Content: prompt},
	}

	resp, err := p.aiClient.Chat(ctx, messages)
	if err != nil {
		return "", err
	}

	// Clean up markdown fences
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```go")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	return strings.TrimSpace(resp), nil
}

// validateCompilation compiles the generated code in a temp directory
func (p *Pipeline) validateCompilation(code string, result *Result) error {
	tmpDir, err := os.MkdirTemp("", "pipeline-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outFile := filepath.Join(tmpDir, "strategy.go")
	if err := os.WriteFile(outFile, []byte(code), 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	var stderr bytes.Buffer
	buildCmd := exec.Command("go", "build", "-o", filepath.Join(tmpDir, "strategy"), outFile)
	buildCmd.Dir = "/Users/ruoxi/longshaosWorld/quant-trading"
	buildCmd.Stderr = &stderr
	if err := buildCmd.Run(); err != nil {
		buildErr := stderr.String()
		result.BuildError = buildErr
		return fmt.Errorf("compilation failed: %s", buildErr)
	}

	return nil
}

// runBacktest executes a backtest with the generated strategy
func (p *Pipeline) runBacktest(ctx context.Context, i *intent.Intent, runner BacktestRunner, result *Result) (*domain.BacktestResult, error) {
	universe := parseUniverse(i.Universe)
	startDate := "2022-01-01"
	endDate := "2024-01-01"

	btResult, err := runner.RunBacktest(ctx, i.StrategyName, universe, startDate, endDate)
	if err != nil {
		result.BacktestError = err.Error()
		return nil, err
	}

	return btResult, nil
}

// fail marks a pipeline job as failed
func (p *Pipeline) fail(result *Result, stage Stage, message string) {
	result.Status = StageFailed
	result.BuildError = message
	now := time.Now()
	result.CompletedAt = &now
	result.DurationMs = now.Sub(result.StartedAt).Milliseconds()
	p.log(result, fmt.Sprintf("FAILED at stage %s: %s", stage, message))
}

// complete marks a pipeline job as completed
func (p *Pipeline) complete(result *Result) {
	result.Status = StageComplete
	now := time.Now()
	result.CompletedAt = &now
	result.DurationMs = now.Sub(result.StartedAt).Milliseconds()
	p.log(result, "Pipeline completed successfully")
}

// log adds a log entry to the result
func (p *Pipeline) log(result *Result, message string) {
	result.Logs = append(result.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message))
}

func parseUniverse(universe string) []string {
	if universe == "" || universe == "all" {
		return nil
	}
	if strings.HasPrefix(universe, "universe:") {
		universe = strings.TrimPrefix(universe, "universe:")
	}
	parts := strings.Split(universe, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		result = append(result, strings.TrimSpace(p))
	}
	return result
}
