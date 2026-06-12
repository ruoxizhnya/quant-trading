package strategy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	sandboxrunner "github.com/ruoxizhnya/quant-trading/internal/sandbox/runner"
	"github.com/ruoxizhnya/quant-trading/internal/sandbox/staticcheck"
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
	// aiClient is the LLM backend. Typed as the ai.LLMClient interface
	// (Sprint 6 P0-1) so tests can inject a deterministic *ai.MockClient
	// instead of hitting a real LLM endpoint.
	aiClient ai.LLMClient

	generated  int64 // total generated (LLM called)
	buildable  int64 // build succeeded
	backtested int64 // backtest produced valid result (≥1 trade)

	jobs sync.Map // jobID string -> *JobResult

	// Sprint 6 P0-4 (ODR-013): WorkingDir replaces the previously
	// hard-coded `buildCmd.Dir = "/Users/ruoxi/longshaosWorld/quant-trading"`.
	//
	// WorkingDir is the directory `go build` runs in when compiling
	// the LLM-generated strategy. It must contain a `go.mod` so that
	// the generated file can resolve its imports
	// (github.com/ruoxizhnya/quant-trading/pkg/domain, etc.). The
	// service does NOT auto-detect this — callers MUST set it
	// explicitly via WithWorkingDir so the configuration is
	// environment-agnostic and reviewable in code review.
	//
	// Empty WorkingDir causes the service to fail-closed at build
	// time (it returns "sandbox_rejected" with a clear error). We do
	// NOT silently fall back to os.Getwd() because the typical
	// caller is a long-running server whose working directory may
	// change between restarts.
	workingDir string

	// logger is the structured logger used by the sandbox gate and
	// retry loop. Defaults to zerolog.Nop() if the caller did not
	// set one (so unit tests don't have to thread a logger through).
	logger zerolog.Logger
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

// NewCopilotService creates a new CopilotService that reads AI credentials
// from the AI_API_KEY / AI_API_URL environment variables.
func NewCopilotService() *CopilotService {
	return &CopilotService{
		aiClient: ai.NewClient(),
		logger:   zerolog.Nop(),
	}
}

// NewCopilotServiceWithLLM creates a CopilotService with an explicit
// LLMClient. If client is nil, falls back to NewCopilotService() semantics.
// This is the constructor tests should use to inject an *ai.MockClient.
func NewCopilotServiceWithLLM(client ai.LLMClient) *CopilotService {
	if client == nil {
		client = ai.NewClient()
	}
	return &CopilotService{aiClient: client, logger: zerolog.Nop()}
}

// WithWorkingDir returns the receiver with WorkingDir set to dir.
// This is the post-construction setter used by cmd/analysis/main.go
// once it has read the value from analysis-service.yaml. The method
// returns the service so it can be chained immediately after
// construction:
//
//	svc := strategy.NewCopilotService().WithWorkingDir(v.GetString("copilot.working_dir"))
//
// dir is NOT validated at this point — the validation is deferred to
// the first Generate() call so a misconfigured server can still start
// (and report the issue via /health or /api/copilot/generate) instead
// of crashing on boot.
func (s *CopilotService) WithWorkingDir(dir string) *CopilotService {
	s.workingDir = dir
	return s
}

// WorkingDir returns the currently configured working directory.
// Exported so config-bootstrap code and tests can assert that the
// service received a working dir at all.
func (s *CopilotService) WorkingDir() string {
	return s.workingDir
}

// WithLogger attaches a structured logger to the service. Production
// callers in cmd/analysis use the root logger with a `component=copilot`
// field. Tests may leave the default zerolog.Nop() in place.
func (s *CopilotService) WithLogger(l zerolog.Logger) *CopilotService {
	if l.GetLevel() != zerolog.Disabled {
		s.logger = l
	}
	return s
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

	// Sprint 6 P0-4 (ODR-013): Stage-1 sandbox gate.
	//
	// Run the regex-based staticcheck pass BEFORE creating any temp
	// directory or invoking `go build`. If the LLM produced code
	// containing known-dangerous patterns (os.RemoveAll, exec.Command,
	// net.Dial, panic, …), reject the job with status
	// "sandbox_rejected" and a finding-bearing error message.
	//
	// This is the cheap, fail-closed filter from ADR-007 Phase 1; the
	// process-isolation sandbox (Phase 2) is tracked under Sprint 6
	// P1-11.
	if err := staticcheck.CheckOrError(code); err != nil {
		s.logger.Warn().
			Err(err).
			Str("job_id", jobID).
			Msg("Generated strategy rejected by staticcheck sandbox gate (P0-4)")
		result.Lock()
		result.Status = "sandbox_rejected"
		result.BuildErr = err.Error()
		result.Unlock()
		return
	}

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

	// Sprint 6 P0-4 (ODR-013): fail-closed if WorkingDir is not set.
	// The previous hard-coded path
	// "/Users/ruoxi/longshaosWorld/quant-trading" was the only reason
	// this code worked on a single developer's machine; in any other
	// environment (CI, Docker, a different developer's laptop) the
	// `go build` would have failed silently with a non-obvious
	// "no go.mod" error. Configuration must be explicit.
	if s.workingDir == "" {
		result.Lock()
		result.Status = "sandbox_rejected"
		result.BuildErr = "copilot.working_dir is not configured; set it in analysis-service.yaml under the `copilot:` key"
		result.Unlock()
		return
	}

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

		// Sprint 6 P1-11 (ODR-020): process-isolation sandbox. The `go build`
	// command is now executed via sandboxrunner.Runner which enforces a 30s
	// wall-clock timeout, a 1GB virtual memory cap, and runs the child
	// in its own session / process group. If the LLM produces a
	// runaway-loop / fork-bomb / mem-leak build, the runner kills it
	// before it can DoS the analysis service.
	buildRunner := sandboxrunner.New(
		sandboxrunner.WithTimeout(30*time.Second),
		sandboxrunner.WithLimits(sandboxrunner.Limits{
			MemoryBytes: 1 << 30, // 1 GiB
			CPUSeconds:  25,
			OpenFiles:   256,
		}),
	)
	var stderr bytes.Buffer
	buildOut := filepath.Join(tmpDir, fmt.Sprintf("strategy_v%d", attempt))
	_, buildStderr, err := buildRunner.Run(ctx, "go", []string{"build", "-o", buildOut, outFile}, sandboxrunner.Options{
		Dir: s.workingDir, // Sprint 6 P0-4: was a hard-coded path
	})
	// Copy the runner's stderr capture into our local buffer so the
	// rest of the loop (LLM retry logic) keeps working unchanged.
	if buildStderr != nil {
		stderr.Write(buildStderr.Bytes())
	}
	if err != nil {
		buildErr := stderr.String()
		if errors.Is(err, sandboxrunner.ErrTimeout) {
			s.logger.Warn().
				Err(err).
				Str("job_id", jobID).
				Int("attempt", attempt).
				Msg("Sandbox runner killed `go build` after timeout (P1-11)")
		}
		if attempt < maxRetries && s.aiClient.IsConfigured() {
				fixedCode, fixErr := s.aiClient.FixStrategyCode(ctx, code, buildErr)
				if fixErr == nil && fixedCode != "" {
					// Re-run the sandbox gate on the LLM's fix. A
					// self-correcting LLM that, in a retry,
					// accidentally introduces an os.RemoveAll
					// (because it copied a snippet from training
					// data) would otherwise sneak past us.
					if recheckErr := staticcheck.CheckOrError(fixedCode); recheckErr == nil {
						code = fixedCode
						result.Lock()
						result.Code = code
						result.Unlock()
						continue
					} else {
						s.logger.Warn().
							Err(recheckErr).
							Str("job_id", jobID).
							Int("attempt", attempt).
							Msg("LLM's retry code rejected by staticcheck; aborting retry loop")
					}
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
