package bootstrap

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/client"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/tools"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/tools/builtin"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
)

// BuildToolsRegistry 构造 Tools Registry（S7-P3-3, ODR-043）并注册全部
// 内置工具组。registry 经 ToolsHandler 暴露在 /api/tools/*，让外部 agent
// 服务（如 Hermes）无需读 SPEC.md 即可发现与调用平台能力。
//
// S7-P3-4（Hermes Phase 1.7 + Phase 2.2-2.3）起 hosting 19 个工具、12 组：
//   - backtest.run          (S7-P3-3, L3 gate)
//   - factor.compute/.evaluate / validate_factor / compute_factor_ic
//   - data.ohlcv/.stocks/.fundamentals
//   - strategy.list/.get
//   - walk_forward_validate (Hermes Phase 1.3, L4 gate)
//   - list_factors / save_factor / list_strategies / save_strategy（gene pool）
//   - summarize_backtest / get_strategy_lineage / get_market_regime
//   - research.profile (EQD-P2-1) / factor.hypothesis (P2-3) / strategy_health (P2-6)
//
// Wiring notes:
//   - BacktestTool 复用与 AI pipeline 相同的 contracts.BacktestRunner（零重复）。
//   - FactorTool 走 HTTP 指向 factorAPIURL —— 拆出 ai-service 后该 URL 指向
//     analysis-service（/api/factor/* 只在那里注册），不再「指向自身」。
//   - DataFetchTool 用共享 HTTPClient 指向 data-service URL。
//   - StrategyRegistryTool 读包级 strategy.DefaultRegistry，无需装配。
//   - Gene Pool tools 收窄接口，由 *gene_pool.FactorPool / StrategyPool 满足。
//   - ResearchProfileTool 收 *storage.PostgresStore + equitydeep.vault_path。
func BuildToolsRegistry(
	v *viper.Viper,
	factorAPIURL string,
	runner contracts.BacktestRunner,
	wfRunner builtin.WalkForwardRunner,
	factorPool builtin.FactorPoolClient,
	strategyPool builtin.StrategyPoolClient,
	regimeDetector builtin.RegimeDetectorClient,
	researchProfile builtin.ResearchProfileClient,
	hypothesisStore *storage.PostgresStore,
	logger zerolog.Logger,
) *tools.Registry {
	reg := tools.NewRegistry()

	// ── Group 1: Backtest (S7-P3-3) ──────────────────────────────────
	if err := reg.Register(builtin.NewBacktestTool(runner)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register backtest.run tool")
	}

	// ── Group 2: Factor — HTTP + expression-based (S7-P3-3 + Hermes 1.1/1.2) ─
	factorClient := client.NewFactorClient(factorAPIURL)
	if err := reg.Register(builtin.NewFactorComputeTool(factorClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register factor.compute tool")
	}
	if err := reg.Register(builtin.NewFactorEvaluateTool(factorClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register factor.evaluate tool")
	}
	// Hermes Phase 1.1: L1 syntax gate — no DI, creates fresh parser per Execute.
	if err := reg.Register(builtin.NewValidateFactorTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register validate_factor tool")
	}
	// Hermes Phase 1.2: L2 quick IC gate — reuses the same factor HTTP client.
	if err := reg.Register(builtin.NewComputeFactorICTool(factorClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register compute_factor_ic tool")
	}

	// ── Group 3: Data fetch (S7-P3-3) ───────────────────────────────
	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
	}
	dataClient := builtin.NewDataSourceClient(dataServiceURL, HTTPClient)
	if err := reg.Register(builtin.NewDataOHLCVTool(dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register data.ohlcv tool")
	}
	if err := reg.Register(builtin.NewDataStocksTool(dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register data.stocks tool")
	}
	if err := reg.Register(builtin.NewDataFundamentalsTool(dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register data.fundamentals tool")
	}

	// ── Group 4: Strategy registry (S7-P3-3) ─────────────────────────
	if err := reg.Register(builtin.NewStrategyListTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register strategy.list tool")
	}
	if err := reg.Register(builtin.NewStrategyGetTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register strategy.get tool")
	}

	// ── Group 5: Walk-forward validation (Hermes Phase 1.3, L4 gate) ─
	if err := reg.Register(builtin.NewWalkForwardValidateTool(wfRunner)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register walk_forward_validate tool")
	}

	// ── Group 6: Gene Pool — factor + strategy CRUD (Hermes Phase 1.4/1.5) ─
	if err := reg.Register(builtin.NewListFactorsTool(factorPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register list_factors tool")
	}
	if err := reg.Register(builtin.NewSaveFactorTool(factorPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register save_factor tool")
	}
	if err := reg.Register(builtin.NewListStrategiesTool(strategyPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register list_strategies tool")
	}
	if err := reg.Register(builtin.NewSaveStrategyTool(strategyPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register save_strategy tool")
	}

	// ── Group 7: Summarization (Hermes Phase 1.6) ───────────────────
	if err := reg.Register(builtin.NewSummarizeBacktestTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register summarize_backtest tool")
	}

	// ── Group 8: Lineage (Hermes Phase 2.2, gene pool lineage) ─────
	if err := reg.Register(builtin.NewGetStrategyLineageTool(strategyPool)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register get_strategy_lineage tool")
	}

	// ── Group 9: Market regime (Hermes Phase 2.3) ──────────────────
	// Reuses the dataClient from Group 3 for OHLCV fetching.
	if err := reg.Register(builtin.NewGetMarketRegimeTool(regimeDetector, dataClient)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register get_market_regime tool")
	}

	// ── Group 10: Research profile (EQD-P2-1, bridge B2) ───────────
	// Reads the EquityDeep research archive, preferring the research.*
	// projection and falling back to the contract C2 mirror under
	// equitydeep.vault_path (env EQUITYDEEP_VAULT_PATH). The vault is
	// mounted read-only; an empty path simply disables the fallback.
	if err := reg.Register(builtin.NewResearchProfileTool(researchProfile, v.GetString("equitydeep.vault_path"))); err != nil {
		logger.Fatal().Err(err).Msg("failed to register research.profile tool")
	}

	// ── Group 11: Factor hypothesis (P2-3) ─────────────────────────
	// 「这个因子凭什么有效」—— 采用一个因子之前先看它的机制来自哪里。
	//
	// 显式判空而不是直接传 hypothesisStore：*PostgresStore 的 nil 塞进
	// 接口会变成非 nil 的接口值，工具会拿着空指针去查库（P1-1b 的老坑）。
	// 没连库时工具仍可用 —— 它回退到 domain 里的内置假设表。
	var hs builtin.FactorHypothesisStore
	if hypothesisStore != nil {
		hs = hypothesisStore
	}
	if err := reg.Register(builtin.NewFactorHypothesisTool(hs)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register factor.hypothesis tool")
	}

	// ── Group 12: Strategy health (P2-6) ───────────────────────────
	// 「这个策略是不是开始不行了」—— 滚动指标 + 概念漂移检测。
	// 接上之前 pkg/ai/drift 与 pkg/strategy/monitor 是两个零调用方的孤儿包，
	// 实现完整却没人消费。无状态：每次调用新建 monitor 喂完整段序列。
	if err := reg.Register(builtin.NewStrategyHealthTool()); err != nil {
		logger.Fatal().Err(err).Msg("failed to register monitor.strategy_health tool")
	}

	logger.Info().
		Int("tool_count", len(reg.List())).
		Msg("Tools Registry initialized (S7-P3-3 + Hermes Phase 1.7 + Phase 2.2-2.3 + EQD-P2-1): 19 tools exposed at /api/tools/* — backtest/factor/data/strategy/gene-pool/walk-forward/summarize/lineage/regime/research")
	return reg
}

// FactorAPIURL 从 server.port 推导 analysis 侧 factor 端点基址
// （analysis 自指；ai-service 则显式传 analysis 的地址）。
func FactorAPIURL(v *viper.Viper, defaultPort int) string {
	if port := v.GetInt("server.port"); port != 0 {
		return fmt.Sprintf("http://localhost:%d", port)
	}
	return fmt.Sprintf("http://localhost:%d", defaultPort)
}
