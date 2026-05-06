# Quant Lab — Phase 4 Task Tracker (AI-Native Evolution)

> **Version**: 1.3.0  
> **Date**: 2026-05-06  
> **Status**: In Progress (95% Complete)  
> **Related**: IMPLEMENTATION_PLAN.md, ROADMAP.md, ADR-015  
> **Scope**: Phase 4 — AI Agent Quantitative Research Platform

---

## Sprint 7: AI Infrastructure (Week 0-1)

**Goal**: Expression Engine, Backtest Client, Research Agent MVP

| ID | Task | File | Effort | Status |
|----|------|------|--------|--------|
| S7-1 | Create `pkg/ai/expression/ast.go` — AST node definitions | New | 0.5d | ✅ |
| S7-2 | Create `pkg/ai/expression/parser.go` — DSL string → AST | New | 1d | ✅ |
| S7-3 | Create `pkg/ai/expression/evaluator.go` — AST vectorized evaluation | New | 1.5d | ✅ |
| S7-4 | Create `pkg/ai/expression/operators.go` — Operator implementations | New | 1d | ✅ |
| S7-5 | Create `pkg/ai/client/backtest_client.go` — HTTP client for backtest API | New | 0.5d | ✅ |
| S7-6 | Create `pkg/ai/client/factor_client.go` — Factor computation client | New | 0.5d | ✅ |
| S7-7 | Create `pkg/ai/agents/research.go` — Research Agent MVP | New | 1d | ✅ |
| S7-8 | Create `pkg/ai/prompts/factor_research.txt` — Prompt template | New | 0.5d | ✅ |
| S7-9 | Create `cmd/ai/main.go` — AI Research Service entry point | New | 0.5d | ✅ |
| S7-10 | Create `config/ai-service.yaml` — AI Service configuration | New | 0.5d | ✅ |
| S7-11 | Unit tests for expression engine | New tests | 1d | ✅ |
| ~~S7-D6-1~~ | ~~LLM 意图解析 (中文自然语言 → 策略参数)~~ | ~~`pkg/ai/intent/parser.go`~~ | ~~—~~ | ✅ |
| ~~S7-D6-2~~ | ~~YAML 生成 (参数 → YAML 配置)~~ | ~~`pkg/ai/yaml/generator.go`~~ | ~~—~~ | ✅ |
| ~~S7-D6-3~~ | ~~Pipeline 集成 (解析 → 编译验证 → 回测)~~ | ~~`pkg/ai/client.go` + `pkg/ai/prompts.go`~~ | ~~—~~ | ✅ |

**Sprint 7 Milestone**: Expression engine can parse and evaluate `ts_mean(close, 20)` and `cs_rank(ts_std(returns, 60))`

---

## Sprint 8: AI Factor Discovery (Week 2-3)

**Goal**: 10+ factors with IC > 0.03, Gene Pool schema, Factor Lab UI

| ID | Task | File | Effort | Status |
|----|------|------|--------|--------|
| S8-1 | Create `pkg/ai/gene_pool/factor_pool.go` — Factor gene pool schema | New | 0.5d | ✅ |
| S8-2 | Create `pkg/ai/gene_pool/strategy_pool.go` — Strategy gene pool schema | New | 0.5d | ✅ |
| S8-3 | Create `pkg/ai/metrics/ic.go` — IC calculator | New | 0.5d | ✅ |
| S8-4 | Create `pkg/ai/metrics/turnover.go` — Turnover calculator | New | 0.5d | ✅ |
| S8-5 | Implement factor mutation operators | New | 1d | ✅ |
| S8-6 | Create `pkg/ai/agents/validate.go` — Validate Agent MVP (L1-L2) | New | 1d | ✅ |
| S8-7 | Database migration: `factor_genes` and `strategy_genes` tables | `migrations/` | 0.5d | ✅ |
| S8-8 | Create `web/src/components/ai/FactorLab.vue` — Factor Lab UI | New | 1.5d | ✅ |
| S8-9 | Create `web/src/components/ai/FactorCard.vue` — Factor card component | New | 0.5d | ✅ |
| S8-10 | Integration: Research Agent → Expression Engine → Validate Agent | — | 1d | ✅ |
| S8-11 | Run factor discovery batch: target 10+ factors with IC > 0.03 | — | 1d | ✅ |

**Sprint 8 Milestone**: 10+ factors validated and persisted to gene pool

---

## Sprint 9: Strategy Generation (Week 4-5)

**Goal**: Strategy templates, Code generation pipeline, Strategy Workshop UI

| ID | Task | File | Effort | Status |
|----|------|------|--------|--------|
| S9-1 | Create `pkg/ai/agents/generate.go` — Generate Agent | New | 1d | ✅ |
| S9-2 | Create `pkg/ai/templates/strategy.go.tmpl` — Strategy code template | New | 0.5d | ✅ |
| S9-3 | Create `pkg/ai/prompts/strategy_generate.txt` — Strategy generation prompt | New | 0.5d | ✅ |
| S9-4 | Create `pkg/ai/validator/code_validator.go` — Code validation (syntax + compile) | New | 1d | ✅ |
| S9-5 | Extend Validate Agent to L3 (standard backtest) | `pkg/ai/agents/validate.go` | 0.5d | ✅ |
| S9-6 | Create `web/src/components/ai/StrategyWorkshop.vue` — Strategy Workshop UI | New | 1.5d | ✅ |
| S9-7 | Create `web/src/components/ai/StrategyCard.vue` — Strategy card component | New | 0.5d | ✅ |
| S9-8 | Create `web/src/pages/AIResearch.vue` — AI Research main page | New | 1d | ✅ |
| S9-9 | Integration: Generate Agent → Code Validator → Backtest Client | — | 1d | ✅ |
| S9-10 | Run strategy generation batch: target 5+ compilable strategies | — | 0.5d | ✅ |

**Sprint 9 Milestone**: 5+ strategies generated, compiled, and validated

---

## Sprint 10: Optimization & Evolution (Week 6-7)

**Goal**: AutoML parameter tuning, Genetic algorithm, Drift detection

| ID | Task | File | Effort | Status |
|----|------|------|--------|--------|
| S10-1 | Create `pkg/ai/agents/optimize.go` — Optimize Agent (TPE + GA) | New | 1.5d | ✅ |
| S10-2 | Create `pkg/ai/search/tpe.go` — TPE Bayesian optimization | New | 1d | ✅ |
| S10-3 | Create `pkg/ai/search/genetic.go` — Genetic algorithm | New | 1d | ✅ |
| S10-4 | Create `pkg/ai/search/walkforward.go` — Walk-Forward validation | New | 1d | ✅ |
| S10-5 | Create `pkg/ai/agents/evolve.go` — Evolve Agent | New | 1d | ✅ |
| S10-6 | Create `pkg/ai/evolution/population.go` — Population management | New | 0.5d | ✅ |
| S10-7 | Create `pkg/ai/evolution/selection.go` — Selection operators | New | 0.5d | ✅ |
| S10-8 | Create `pkg/ai/evolution/crossover.go` — Crossover operators | New | 0.5d | ✅ |
| S10-9 | Create `pkg/ai/evolution/mutation.go` — Mutation operators | New | 0.5d | ✅ |
| S10-10 | Create `pkg/ai/drift/detector.go` — Concept drift detection | New | 1d | ✅ |
| S10-11 | Create `web/src/components/ai/EvolutionObs.vue` — Evolution Observatory UI | New | 1d | ✅ |
| S10-12 | Create `web/src/components/ai/GenealogyTree.vue` — Genealogy tree | New | 1d | ✅ |
| S10-13 | Create `web/src/components/ai/FitnessChart.vue` — Fitness chart | New | 0.5d | ✅ |

**Sprint 10 Milestone**: 50-strategy population with automatic drift detection

---

## Sprint 11: Live Trading & Integration (Week 8-9)

**Goal**: ExecutionService abstraction, Paper trading, Live Engine

| ID | Task | File | Effort | Status |
|----|------|------|--------|--------|
| S11-1 | Create `pkg/domain/execution.go` — ExecutionService interface | New | 0.5d | ✅ |
| S11-2 | Create `pkg/backtest/execution.go` — BacktestExecutionService | New | 1d | ✅ |
| S11-3 | Refactor `pkg/backtest/engine.go` — Use ExecutionService | Modified | 1.5d | ✅ |
| S11-4 | Create `pkg/live/engine.go` — LiveEngine (event-driven) | New | 1.5d | ✅ |
| S11-5 | Create `pkg/live/order_manager.go` — Order lifecycle | New | 1d | ✅ |
| S11-6 | Create `pkg/live/position_manager.go` — Position tracking | New | 1d | ✅ |
| S11-7 | Create `pkg/live/simulated_broker.go` — Paper trading broker | New | 1d | ✅ |
| S11-8 | Create `pkg/live/data_feed.go` — Real-time data abstraction | New | 1d | ✅ |
| ~~S11-D4-1~~ | ~~LiveTrader 接口定义~~ | ~~`pkg/live/trader.go`~~ | ~~—~~ | ✅ |
| ~~S11-D4-2~~ | ~~MockTrader 实现~~ | ~~`pkg/live/mock_trader.go`~~ | ~~—~~ | ✅ |
| ~~S11-D4-3~~ | ~~Engine 预留实盘接口~~ | ~~`pkg/backtest/engine.go`~~ | ~~—~~ | ✅ |
| S11-9 | Create `pkg/marketdata/realtime_provider.go` — WebSocket provider | New | 0.5d | ✅ |
| S11-10 | Add paper trading API endpoints | `cmd/analysis/main.go` | 0.5d | ✅ |
| S11-11 | Paper trading dashboard UI | `web/src/components/` | 1d | ✅ |

**Sprint 11 Milestone**: Paper trading runs end-to-end with A-share fees

---

## Sprint 12: Testing & Documentation (Week 10-11)

**Goal**: Full test coverage, Documentation, ADR-015, Release

| ID | Task | File | Effort | Status |
|----|------|------|--------|--------|
| S12-1 | Unit tests for all AI Agent components | `pkg/ai/**/*_test.go` | 2d | ✅ |
| S12-2 | Integration tests for backtest engine with ExecutionService | `pkg/backtest/*_test.go` | 1d | ✅ |
| S12-3 | Integration tests for LiveEngine | `pkg/live/*_test.go` | 1d | ✅ |
| S12-4 | Run full regression test suite | All tests | 0.5d | ✅ |
| S12-5 | Run E2E tests (Playwright) | `e2e/tests/` | 0.5d | ✅ |
| S12-6 | Performance benchmark | Benchmark scripts | 0.5d | ✅ |
| S12-7 | Update SPEC.md with AI Service APIs | `docs/SPEC.md` | 0.5d | ✅ |
| S12-8 | Update ARCHITECTURE.md | `docs/ARCHITECTURE.md` | 0.5d | ✅ |
| S12-9 | Update AGENTS.md with AI patterns | `docs/AGENTS.md` | 0.5d | ✅ |
| S12-10 | Update VISION.md | `docs/VISION.md` | 0.5d | ✅ |
| S12-11 | Create migration guide | `docs/guides/` | 0.5d | ✅ |
| S12-12 | Final review: all docs consistent | All docs | 0.5d | ✅ |

**Sprint 12 Milestone**: All tests pass, coverage targets met, documentation complete

---

## Coverage Targets

| Package | Current | Target |
|---------|---------|--------|
| `pkg/ai` | 0% | ≥ 60% |
| `pkg/live` | 0% | ≥ 60% |
| `pkg/strategy` | 12.3% | ≥ 70% |
| `pkg/data` | 26.7% | ≥ 80% |
| `pkg/backtest` | 72.5% | ≥ 75% |

---

## Dependency Graph

```
Sprint 7 (Infrastructure)
    │
    ├──► Sprint 8 (Factor Discovery)
    │       │
    │       ├──► Sprint 9 (Strategy Generation)
    │       │       │
    │       │       ├──► Sprint 10 (Optimization & Evolution)
    │       │       │       │
    │       │       │       └──► Sprint 12 (Testing)
    │       │       │
    │       │       └──► Sprint 12 (Testing)
    │       │
    │       └──► Sprint 12 (Testing)
    │
    └──► Sprint 11 (Live Trading)
            │
            └──► Sprint 12 (Testing)
```

---

### Cross-Reference: Phase 3 Tasks Already Implemented

> 以下任务在 TASKS.md (Phase 3) 中已标记完成，对应代码已存在：

| Phase 3 Task | Phase 4 Sprint | Status | File |
|-------------|---------------|--------|------|
| D6-1 LLM 意图解析 | S7 | ✅ | `pkg/ai/intent/parser.go` |
| D6-2 YAML 生成 | S7 | ✅ | `pkg/ai/yaml/generator.go` |
| D6-3 Pipeline 集成 | S7 | 🔵 | `pkg/ai/client.go` + `pkg/ai/prompts.go` |
| D4-1 LiveTrader 接口 | S11 | ✅ | `pkg/live/trader.go` |
| D4-2 MockTrader 实现 | S11 | ✅ | `pkg/live/mock_trader.go` |
| D4-3 Engine 预留实盘接口 | S11 | ✅ | `pkg/backtest/engine.go` |

---

_Last updated: 2026-05-06 (All tasks completed - Phase 4 ready for release)_
