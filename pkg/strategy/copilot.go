package strategy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// BacktestRunner runs a backtest for the copilot.
// It is implemented by cmd/analysis via a local adapter.
type BacktestRunner interface {
	RunBacktest(ctx context.Context, strategyName string, stockPool []string, startDate, endDate string) (*domain.BacktestResult, error)
}

// CopilotService generates Go strategy code from natural-language descriptions
// and optionally runs a backtest against the generated strategy.
type CopilotService struct {
	aiClient *ai.Client

	generated  int64 // total generated (LLM called)
	buildable  int64 // build succeeded
	backtested int64 // backtest produced valid result (≥1 trade)

	jobs sync.Map // jobID string -> *JobResult
}

// JobResult holds the outcome of a copilot generation job.
type JobResult struct {
	sync.Mutex
	JobID          string                 `json:"job_id"`
	Status         string                 `json:"status"`
	Code           string                 `json:"generated_code,omitempty"`
	BuildErr       string                 `json:"build_error,omitempty"`
	StrategyName   string                 `json:"strategy_name,omitempty"`
	BacktestResult *domain.BacktestResult `json:"backtest_result,omitempty"`
	BacktestErr    string                 `json:"backtest_error,omitempty"`
}

// GenerateParams are the input parameters for strategy generation.
type GenerateParams struct {
	Description string `json:"description"`
	Universe   string `json:"universe"`   // csi300, csi500, csi800, all
	StartDate  string `json:"start_date"` // YYYY-MM-DD
	EndDate    string `json:"end_date"`   // YYYY-MM-DD
}

// NewCopilotService creates a new CopilotService.
func NewCopilotService() *CopilotService {
	return &CopilotService{
		aiClient: ai.NewClient(),
	}
}

// IsConfigured returns true if AI client is configured.
func (s *CopilotService) IsConfigured() bool {
	return s.aiClient != nil && s.aiClient.IsConfigured()
}

// Generate starts a strategy generation job and returns immediately.
// Callers poll via GetJob.
func (s *CopilotService) Generate(ctx context.Context, params GenerateParams, runner BacktestRunner) *JobResult {
	jobID := uuid.New().String()
	result := &JobResult{JobID: jobID, Status: "pending"}
	s.jobs.Store(jobID, result)
	atomic.AddInt64(&s.generated, 1)

	go func() {
		s.run(context.Background(), jobID, params, runner)
	}()

	return result
}

// GetJob retrieves a job result by ID.
func (s *CopilotService) GetJob(jobID string) *JobResult {
	val, ok := s.jobs.Load(jobID)
	if !ok {
		return nil
	}
	return val.(*JobResult)
}

// Stats returns acceptance statistics.
func (s *CopilotService) Stats() (generated, buildable, backtested int64) {
	return atomic.LoadInt64(&s.generated),
		atomic.LoadInt64(&s.buildable),
		atomic.LoadInt64(&s.backtested)
}

// AcceptanceRate returns the fraction of generated strategies that produced valid backtests.
func (s *CopilotService) AcceptanceRate() float64 {
	g := atomic.LoadInt64(&s.generated)
	if g == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&s.backtested)) / float64(g)
}

func (s *CopilotService) run(ctx context.Context, jobID string, params GenerateParams, runner BacktestRunner) {
	result := s.GetJob(jobID)
	if result == nil {
		return
	}

	code, err := s.aiClient.GenerateStrategyCode(ctx, params.Description)
	if err != nil {
		result.Lock()
		result.Status = "llm_failed"
		result.BuildErr = err.Error()
		result.Unlock()
		return
	}

	result.Lock()
	result.Code = code
	result.Unlock()

	tmpDir, err := os.MkdirTemp("", "copilot-*")
	if err != nil {
		result.Lock()
		result.Status = "build_failed"
		result.BuildErr = "failed to create temp dir: " + err.Error()
		result.Unlock()
		return
	}
	defer os.RemoveAll(tmpDir)

	code = strings.TrimPrefix(code, "```go")
	code = strings.TrimPrefix(code, "```")
	code = strings.TrimSuffix(code, "```")
	code = strings.TrimSpace(code)

	const maxRetries = 2
	strategyName := ""

	for attempt := 0; attempt <= maxRetries; attempt++ {
		outFile := filepath.Join(tmpDir, fmt.Sprintf("strategy_v%d.go", attempt))
		if err := os.WriteFile(outFile, []byte(code), 0600); err != nil {
			result.Lock()
			result.Status = "build_failed"
			result.BuildErr = "failed to write file: " + err.Error()
			result.Unlock()
			return
		}

		var stderr bytes.Buffer
		buildCmd := exec.Command("go", "build", "-o", filepath.Join(tmpDir, fmt.Sprintf("strategy_v%d", attempt)), outFile)
		buildCmd.Dir = "/Users/ruoxi/longshaosWorld/quant-trading"
		buildCmd.Stderr = &stderr
		if err := buildCmd.Run(); err != nil {
			buildErr := stderr.String()
			if attempt < maxRetries && s.aiClient.IsConfigured() {
				fixedCode, fixErr := s.aiClient.FixStrategyCode(ctx, code, buildErr)
				if fixErr == nil && fixedCode != "" {
					code = fixedCode
					result.Lock()
					result.Code = code
					result.Unlock()
					continue
				}
			}
			result.Lock()
			result.Status = "build_failed"
			result.BuildErr = buildErr
			result.Unlock()
			return
		}

		atomic.AddInt64(&s.buildable, 1)
		strategyName, _ = s.loadPluginSymbol(outFile)
		break
	}

	if strategyName == "" {
		strategyName = "copilot-generated"
	}

	result.Lock()
	result.Status = "generated"
	result.StrategyName = strategyName
	result.Unlock()

	if runner != nil {
		universe := parseUniverse(params.Universe)
		backtestResult, err := runner.RunBacktest(ctx, strategyName, universe, params.StartDate, params.EndDate)
		if err != nil {
			result.Lock()
			result.Status = "backtest_failed"
			result.BacktestErr = err.Error()
			result.Unlock()
			return
		}
		if backtestResult != nil && backtestResult.TotalTrades > 0 {
			atomic.AddInt64(&s.backtested, 1)
		}
		result.Lock()
		result.Status = "backtest_complete"
		result.BacktestResult = backtestResult
		result.Unlock()
	}
}

// loadPluginSymbol compiles the strategy as a plugin and extracts the strategy name.
// Falls back to default name if plugin loading not supported.
func (s *CopilotService) loadPluginSymbol(file string) (string, error) {
	// Try plugin approach first (Linux/macOS)
	p, err := plugin.Open(file)
	if err == nil {
		sym, err := p.Lookup("StrategyName")
		if err == nil {
			if name, ok := sym.(*string); ok {
				return *name, nil
			}
		}
	}

	// Fallback: try exec compile approach — just use "generated-strategy" as name
	// This avoids the plugin dependency issues on Darwin
	return "generated-strategy", nil
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
