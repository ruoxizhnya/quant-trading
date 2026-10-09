package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/internal/sandbox/runner"
	"github.com/ruoxizhnya/quant-trading/internal/sandbox/staticcheck"
	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/spf13/viper"
)

// StrategyEngineAdapter 把 *backtest.Engine 适配成 strategy.BacktestRunner
// （窄接口）。copilot / pipeline / explore 三族 handler 与 tools 门面的
// backtest.run 都消费它 —— 零重复。
type StrategyEngineAdapter struct {
	engine *backtest.Engine
}

// NewStrategyEngineAdapter 构造引擎适配器。
func NewStrategyEngineAdapter(engine *backtest.Engine) *StrategyEngineAdapter {
	return &StrategyEngineAdapter{engine: engine}
}

// RunBacktest 实现 strategy.BacktestRunner。
func (a *StrategyEngineAdapter) RunBacktest(
	ctx context.Context,
	strategyName string,
	stockPool []string,
	startDate, endDate string,
) (*domain.BacktestResult, error) {
	req := backtest.BacktestRequest{
		Strategy:  strategyName,
		StockPool: stockPool,
		StartDate: startDate,
		EndDate:   endDate,
	}
	resp, err := a.engine.RunBacktest(ctx, req)
	if err != nil {
		return nil, err
	}
	return &domain.BacktestResult{
		TotalReturn:    resp.TotalReturn,
		AnnualReturn:   resp.AnnualReturn,
		SharpeRatio:    resp.SharpeRatio,
		SortinoRatio:   resp.SortinoRatio,
		MaxDrawdown:    resp.MaxDrawdown,
		WinRate:        resp.WinRate,
		TotalTrades:    resp.TotalTrades,
		WinTrades:      resp.WinTrades,
		LoseTrades:     resp.LoseTrades,
		AvgHoldingDays: resp.AvgHoldingDays,
		CalmarRatio:    resp.CalmarRatio,
	}, nil
}

// StaticCheckAdapter 实现 strategy.CodeChecker，委托给
// internal/sandbox/staticcheck。S7-P1-2（ODR-043）：定义在装配根，
// 使 pkg/strategy 不 import internal/sandbox —— 反向依赖在这里断。
type StaticCheckAdapter struct{}

// CheckOrError 实现 strategy.CodeChecker。
func (StaticCheckAdapter) CheckOrError(code string) error {
	return staticcheck.CheckOrError(code)
}

// SandboxRunnerAdapter 实现 strategy.BuildExecutor，委托给
// internal/sandbox/runner。Runner 以 30s 超时 + 1GiB 内存上限构造一次并
// 复用（Sprint 6 P1-11 / ODR-020）；Runner 除配置外无状态，复用安全。
type SandboxRunnerAdapter struct {
	r *runner.Runner
}

// envAllowUnenforcedSandboxLimits 是在无法强制资源限额的平台（当前是
// Windows，没有 setrlimit(2)）上运行 copilot build 沙箱的逃生舱。
//
// 没有它 runner 会 FAIL CLOSED：要了下面限额的构建直接被拒，而不是无界
// 运行。要点在于 —— 一个静默无上限的子进程比没有子进程更糟，因为下游
// 没人知道保护缺了。
//
// 部署环境绝不许设它。POSIX 上没有必要：限额在子进程内部强制。
const envAllowUnenforcedSandboxLimits = "SANDBOX_ALLOW_UNENFORCED_LIMITS"

// NewSandboxRunnerAdapter 构造沙箱 runner 适配器。
func NewSandboxRunnerAdapter(logger zerolog.Logger) *SandboxRunnerAdapter {
	opts := []runner.Option{
		runner.WithTimeout(30 * time.Second),
		runner.WithLimits(runner.Limits{
			MemoryBytes: 1 << 30, // 1 GiB
			CPUSeconds:  25,
			OpenFiles:   256,
		}),
	}

	if os.Getenv(envAllowUnenforcedSandboxLimits) != "" {
		logger.Warn().
			Str("env", envAllowUnenforcedSandboxLimits).
			Msg("sandbox resource limits will NOT be enforced on this platform; " +
				"unset this variable outside local development")
		opts = append(opts,
			runner.WithAllowUnenforcedLimits(),
			runner.WithOnUnenforcedLimits(func(argv []string, unenforced runner.Limits) {
				logger.Warn().
					Strs("argv", argv).
					Str("unenforced", unenforced.Describe()).
					Msg("sandbox build ran with some resource limits NOT enforced")
			}),
		)
	}

	return &SandboxRunnerAdapter{r: runner.New(opts...)}
}

// Run 实现 strategy.BuildExecutor。
func (a *SandboxRunnerAdapter) Run(ctx context.Context, name string, args []string, workingDir string) (*bytes.Buffer, *bytes.Buffer, error) {
	return a.r.Run(ctx, name, args, runner.Options{Dir: workingDir})
}

// IsTimeout 实现 strategy.BuildExecutor。
func (a *SandboxRunnerAdapter) IsTimeout(err error) bool {
	return errors.Is(err, runner.ErrTimeout)
}

// BuildCopilot 构造 Copilot service（S7-P1-2, ODR-043）：LLM client、
// code checker、build executor 全部在装配根注入。返回 service 与委托给
// 回测引擎的 BacktestRunner 适配器。
func BuildCopilot(v *viper.Viper, engine *backtest.Engine, logger zerolog.Logger) (*strategy.CopilotService, strategy.BacktestRunner) {
	copilotService := strategy.NewCopilotService().
		WithLLMClient(ai.NewClient()).
		WithCodeChecker(StaticCheckAdapter{}).
		WithBuildExecutor(NewSandboxRunnerAdapter(logger)).
		WithLogger(logger.With().Str("component", "copilot").Logger()).
		WithWorkingDir(v.GetString("copilot.working_dir"))
	logger.Info().
		Bool("ai_configured", copilotService.IsConfigured()).
		Str("working_dir", copilotService.WorkingDir()).
		Msg("Copilot service initialized")
	copilotRunner := NewStrategyEngineAdapter(engine)
	return copilotService, copilotRunner
}

// WalkForwardEngineAdapter 把 *backtest.WalkForwardEngine 适配成
// builtin.WalkForwardRunner（窄接口）。具体引擎的 RunWalkForward 收
// Request struct，工具接口收散参 —— 桥接在装配根完成，工具层与引擎
// DTO 解耦（S7-P3-4，Hermes Phase 1.7）。
type WalkForwardEngineAdapter struct {
	engine *backtest.WalkForwardEngine
}

// NewWalkForwardEngineAdapter 构造 walk-forward 适配器。
func NewWalkForwardEngineAdapter(engine *backtest.WalkForwardEngine) *WalkForwardEngineAdapter {
	return &WalkForwardEngineAdapter{engine: engine}
}

// RunWalkForward 实现 builtin.WalkForwardRunner。
func (a *WalkForwardEngineAdapter) RunWalkForward(
	ctx context.Context,
	strategyName string,
	stockPool []string,
	startDate, endDate string,
	params domain.WalkForwardParams,
) (*domain.WalkForwardReport, error) {
	req := backtest.WalkForwardRequest{
		Strategy:          strategyName,
		StockPool:         stockPool,
		StartDate:         startDate,
		EndDate:           endDate,
		WalkForwardParams: params,
	}
	return a.engine.RunWalkForward(ctx, req)
}
