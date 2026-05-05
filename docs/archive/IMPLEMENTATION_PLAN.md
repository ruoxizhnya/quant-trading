# Quant Lab — Comprehensive Implementation Plan

> **Version**: 2.0.0  
> **Date**: 2026-05-04  
> **Status**: Proposed  
> **Related**: ADR-014 (Strategy Refactor), ADR-015 (AI Agent Architecture), TASKS.md, tasks-phase-2.md  
> **Author**: AI Assistant

---

## Executive Summary

This implementation plan consolidates all findings from the 2026-05-04 comprehensive code review, the ADR-014 strategy framework refactor decision, the live trading extensibility analysis, and the AI Agent quantitative research system design into a single, actionable roadmap.

**Strategic Goal**: Transform Quant Lab into an AI-native quantitative trading platform that autonomously discovers alpha factors, generates and evolves trading strategies, supports seamless backtest-to-live deployment, and continuously self-improves through multi-agent collaboration.

**Key Design Principle**: AI is a "senior quantitative researcher" that uses the existing backtest service to validate hypotheses — not a replacement for the core system, but an augmentation layer.

---

## Phase Overview

| Phase | Focus | Duration | Priority | Dependencies |
|-------|-------|----------|----------|-------------|
| **Phase 0** | AI Agent Factor Mining & Strategy Evolution | 3 weeks | P0 | None |
| **Phase 1** | ExecutionService Abstraction & Backtest Decoupling | 2 weeks | P0 | Phase 0 (partial) |
| **Phase 2** | Strategy Framework Refactor | 2 weeks | P0 | Phase 1 |
| **Phase 3** | Factor System Extension | 1 week | P1 | Phase 2 |
| **Phase 4** | Live Trading Infrastructure | 2 weeks | P1 | Phase 1 |
| **Phase 5** | Testing, Validation & Documentation | 1 week | P0 | Phase 2-4 |

**Total Estimated Duration**: 11 weeks (~2.5 months)  
**Parallel Workstreams**: 
- Phase 0 runs independently and feeds into all other phases
- Phase 3 and Phase 4 can execute in parallel after Phase 1 completes

---

## Phase 0: AI Agent Factor Mining & Strategy Evolution

> **Objective**: Build an AI Agent-driven system that acts as a senior quantitative researcher — discovering factors, generating strategies, optimizing parameters, and evolving the best performers through genetic algorithms and self-learning.

### 0.1 Motivation

Current state: All factors (Momentum/Value/Quality/Size/Volatility/Growth) and strategies are hand-crafted. Factor discovery relies on human domain expertise, strategy development requires manual coding, and optimization is limited to manual parameter tuning.

Target state: AI Agents autonomously discover alpha factors, generate executable strategies from natural language descriptions, optimize them via AutoML, and evolve a population of strategies through genetic algorithms with walk-forward validation.

**Reference**: Microsoft RD-Agent(Q) demonstrated that LLM-driven factor discovery achieves 2× ARR at $10 cost with 70% fewer factors than human-designed equivalents.

### 0.2 Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    AI Agent Factor Mining & Evolution                    │
├─────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌─────────────┐ │
│  │   Research   │  │   Generate   │  │   Validate   │  │   Evolve    │ │
│  │    Agent     │  │    Agent     │  │    Agent     │  │    Agent    │ │
│  │  (Factor     │  │  (Strategy   │  │  (Backtest   │  │  (Genetic   │ │
│  │   Discovery) │  │   Code Gen)  │  │   Validate)  │  │   Algo)     │ │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └─────┬───────┘ │
│         │                 │                 │                │         │
│         └─────────────────┴─────────────────┘                │         │
│                                   │                          │         │
│                    ┌──────────────┴──────────────┐    ┌──────┴──────┐ │
│                    │      Expression Engine      │    │   Gene      │ │
│                    │      (DSL + AST)            │    │   Pool      │ │
│                    └──────────────┬──────────────┘    │  (PG+JSONB) │ │
│                                   │                   └─────────────┘ │
│         ┌─────────────────────────┼─────────────────────────┐         │
│         ▼                         ▼                         ▼         │
│  ┌─────────────┐           ┌─────────────┐           ┌─────────────┐ │
│  │   Backtest  │           │   Factor    │           │   Live      │ │
│  │   Engine    │◄─────────►│   Analyzer  │◄─────────►│   Monitor   │ │
│  │  (Phase 1)  │           │  (IC/IRR)   │           │  (Drift)    │ │
│  └─────────────┘           └─────────────┘           └─────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

### 0.3 Task Breakdown

#### P0-A: Research Agent — Factor Discovery (Week 0-1)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P0-A1 | Create `pkg/ai/agents/research.go` — LLM-driven factor hypothesis generation | New | 1d |
| P0-A2 | Implement factor expression engine (DSL for combining raw data into factors) | New | 1.5d |
| P0-A3 | Build factor validator — compute IC, Sharpe, turnover for candidate factors | New | 1d |
| P0-A4 | Create factor gene pool schema (factor DNA: expression + metadata + performance) | `pkg/ai/gene_pool.go` | 0.5d |
| P0-A5 | Implement factor mutation operators (crossover, subtree mutation, pruning) | New | 1d |
| P0-A6 | Unit tests for factor expression engine and mutation operators | New tests | 1d |

**Factor Expression DSL Example**:
```go
// pkg/ai/expression.go

type FactorExpression struct {
    ID       string
    Formula  string                    // e.g., "log(close) - delay(log(close), 20)"
    AST      *ExpressionNode           // Parsed AST for evaluation
    Inputs   []string                  // Required raw data fields
    Metadata FactorMetadata
}

// Raw data fields: open, high, low, close, volume, turnover, market_cap, etc.
// Operators: +, -, *, /, log, sqrt, rank, delay, ts_mean, ts_std, correlation, etc.

// Example: Low Volatility factor
expr := FactorExpression{
    Formula: "-ts_std(returns(close, 1), 20)",  // Negative 20-day return volatility
    Inputs:  []string{"close"},
}
```

#### P0-B: Generate Agent — Strategy Code Generation (Week 1)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P0-B1 | Create `pkg/ai/agents/generate.go` — natural language to strategy code | New | 1d |
| P0-B2 | Build strategy template library (momentum, mean-reversion, multi-factor, etc.) | New | 0.5d |
| P0-B3 | Implement LLM prompt engineering pipeline (CoT + few-shot + template filling) | New | 1d |
| P0-B4 | Create code validator — syntax check + compile + sandbox execution | New | 1d |
| P0-B5 | Build strategy gene pool (strategy DNA: code + params + fitness score) | `pkg/ai/gene_pool.go` | 0.5d |
| P0-B6 | Unit tests for code generation and validation | New tests | 0.5d |

**Strategy Generation Pipeline**:
```go
// pkg/ai/agents/generate.go

type GenerateAgent struct {
    llm        LLMClient          // Claude 3.5 / GPT-4o
    templates  StrategyTemplateDB // Pre-defined strategy templates
    validator  CodeValidator      // go vet + compile + sandbox
}

func (a *GenerateAgent) GenerateStrategy(
    ctx context.Context,
    prompt string,              // "Create a strategy that buys stocks with high earnings surprise"
    constraints Constraints,    // Max positions, turnover limits, etc.
) (*GeneratedStrategy, error) {
    // 1. Intent analysis → strategy type classification
    // 2. Template selection + factor injection (from Research Agent)
    // 3. LLM generates Go code with structured prompt
    // 4. Syntax validation + compilation
    // 5. Sandbox backtest (quick L1 validation)
    // 6. Return generated strategy with fitness preview
}
```

#### P0-C: Validate Agent — Backtest Validation & IC Analysis (Week 1-2)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P0-C1 | Create `pkg/ai/agents/validate.go` — backtest validation orchestrator | New | 1d |
| P0-C2 | Implement layered validation: L1 syntax → L2 quick backtest → L3 standard → L4 Walk-Forward | New | 1d |
| P0-C3 | Build IC (Information Coefficient) calculator | New | 0.5d |
| P0-C4 | Build factor turnover and stability metrics | New | 0.5d |
| P0-C5 | Implement multi-objective scoring (IC + Sharpe + Turnover + MaxDD) | New | 0.5d |
| P0-C6 | Unit tests for validation pipeline | New tests | 0.5d |

**Validation Pipeline**:
```go
// pkg/ai/agents/validate.go

type ValidateAgent struct {
    backtester BacktestClient
    metrics    MetricsCalculator
}

func (a *ValidateAgent) ValidateFactor(ctx context.Context, expr *FactorExpression) (*ValidationResult, error) {
    // L1: Syntax validation (expression engine)
    // L2: Quick backtest (1yr/100 stocks, < 10s)
    // L3: Standard backtest (3yr/500 stocks, < 2min)
    // L4: Walk-Forward validation (5yr/full market, < 10min)
    // Return: IC, Sharpe, Turnover, MaxDD, Score, Passed
}
```

#### P0-D: Evolve Agent — Self-Learning & Drift Detection (Week 2-3)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P0-D1 | Create `pkg/ai/agents/evolve.go` — genetic algorithm orchestrator | New | 1d |
| P0-D2 | Implement strategy population management (selection, crossover, mutation) | New | 1d |
| P0-D3 | Build concept drift detector (ADWIN + Page-Hinkley + IC decay) | New | 1d |
| P0-D4 | Implement automatic retraining trigger (performance degradation → evolve) | New | 0.5d |
| P0-D5 | Create strategy performance dashboard (genealogy tree, fitness history) | `web/src/components/ai/` | 1d |
| P0-D6 | Unit tests for evolution and drift detection | New tests | 0.5d |

**Evolution Loop**:
```go
// pkg/ai/agents/evolve.go

type EvolveAgent struct {
    population   []StrategyGene    // Current population
    genePool     *GenePool         // Historical archive
    driftDetector ConceptDriftDetector
}

func (a *EvolveAgent) Evolve(ctx context.Context) error {
    // 1. Evaluate fitness of current population (backtest all)
    // 2. Check for concept drift (IC decay, Sharpe drop)
    // 3. If drift detected or scheduled:
    //    a. Selection: tournament selection on fitness
    //    b. Crossover: combine factor weights from top performers
    //    c. Mutation: random param perturbation + factor replacement
    //    d. Validation: Walk-Forward on offspring
    // 4. Replace weakest with offspring
    // 5. Archive generation to gene pool
}

type StrategyGene struct {
    ID          string
    ParentIDs   []string        // Genealogy tracking
    Generation  int
    Chromosome  StrategyChromosome  // Encoded strategy params + factors
    Fitness     FitnessScore        // Multi-objective fitness
    Performance PerformanceHistory  // Time-series of backtest metrics
}
```

### 0.4 Key Design Decisions

**Decision 1: Multi-Agent vs Single Agent**
- **Choice**: 4 specialized agents (Research/Generate/Validate/Evolve) with shared gene pool
- **Rationale**: Separation of concerns aligns with Microsoft RD-Agent(Q) architecture; each agent can be developed/tested independently

**Decision 2: LLM Provider**
- **Primary**: Claude 3.5 Sonnet (best code generation accuracy)
- **Fallback**: GPT-4o (faster, cheaper for simple tasks)
- **Local**: DeepSeek-Coder (offline capability, data privacy)

**Decision 3: Factor Expression Engine**
- **Choice**: Custom DSL with AST evaluation
- **Rationale**: More flexible than Qlib's expression engine; can be extended with A-share specific operators (limit-up/down handling, T+1 constraints)

**Decision 4: Gene Pool Storage**
- **Choice**: PostgreSQL with JSONB columns for flexible schema
- **Rationale**: Existing infrastructure; supports complex queries (find all strategies using momentum factor with Sharpe > 1.5)

### 0.5 Integration with Existing System

| AI Agent Output | Consumer | Integration Point |
|----------------|----------|-------------------|
| Discovered Factor | FactorComputer | `pkg/data/factor.go` — auto-register new factor computation |
| Generated Strategy | Registry | `pkg/strategy/registry.go` — auto-register validated strategies |
| Optimized Params | BacktestEngine | `pkg/backtest/engine.go` — run batch optimization jobs |
| Evolution Trigger | LiveEngine | `pkg/live/engine.go` — swap to new strategy version on drift |

### 0.6 Milestone

**M0.1: AI Agent System Operational** (End of Week 3)

- [ ] Research Agent discovers 10+ novel factors with IC > 0.03
- [ ] Generate Agent produces compilable strategy from natural language (success rate > 80%)
- [ ] Validate Agent validates factors through L1-L4 pipeline
- [ ] Evolve Agent maintains population of 50 strategies with automatic drift detection
- [ ] Gene Pool persists 100+ factor definitions and 50+ strategy generations
- [ ] Web UI shows factor discovery progress, strategy genealogy tree, and fitness evolution
- [ ] Cost per factor discovery < $1 (LLM API cost)
- [ ] Cost per strategy generation < $0.5

---

## Phase 1: ExecutionService Abstraction & Backtest Decoupling

> **Objective**: Introduce the `ExecutionService` interface as the foundational abstraction layer that enables the same strategy code to run in backtest, paper, and live modes without modification.

### 1.1 Motivation

Current state: `BacktestEngine` directly calls `Tracker.ClosePosition()` / `Tracker.OpenPosition()`. This hard-couples the engine to backtest simulation logic, making live trading extension impossible without significant refactoring.

Target state: `BacktestEngine` → `ExecutionService` → `BacktestExecutionService` (wraps Tracker) or `LiveExecutionService` (calls broker API).

### 1.2 Task Breakdown

| Task ID | Task | Files | Effort | Owner |
|---------|------|-------|--------|-------|
| P1-1 | Define `ExecutionService` interface in `pkg/domain/execution.go` | New | 0.5d | Backend |
| P1-2 | Define supporting types: `OrderResult`, `FillEvent`, `FillEventHandler`, `Account` | New | 0.5d | Backend |
| P1-3 | Implement `BacktestExecutionService` wrapping existing Tracker logic | New + `pkg/backtest/` | 1d | Backend |
| P1-4 | Refactor `Engine` to use `ExecutionService` instead of direct Tracker calls | `pkg/backtest/engine.go` | 1.5d | Backend |
| P1-5 | Add `EngineMode` enum (`backtest`/`paper`/`live`) to Engine config | `pkg/backtest/engine.go` | 0.5d | Backend |
| P1-6 | Extract price resolution logic from engine into `PriceResolver` abstraction | New + `pkg/backtest/` | 1d | Backend |
| P1-7 | Add `RealTimeProvider` interface extending `MarketDataProvider` | `pkg/marketdata/` | 0.5d | Backend |
| P1-8 | Unit tests for `BacktestExecutionService` | New test files | 1d | Backend |
| P1-9 | Verify all existing backtest tests pass after refactoring | All backtest tests | 0.5d | QA |

### 1.3 Key Design Decisions

```go
// pkg/domain/execution.go

type ExecutionService interface {
    SubmitOrder(ctx context.Context, order Order) (*OrderResult, error)
    CancelOrder(ctx context.Context, orderID string) error
    QueryOrder(ctx context.Context, orderID string) (*Order, error)
    GetPositions(ctx context.Context) (map[string]Position, error)
    GetAccount(ctx context.Context) (*Account, error)
    SubscribeFillEvents(handler FillEventHandler) error
    Close() error
}

type Account struct {
    TotalValue    float64 `json:"total_value"`
    Cash          float64 `json:"cash"`
    MarginUsed    float64 `json:"margin_used"`
    BuyingPower   float64 `json:"buying_power"`
    DailyPnL      float64 `json:"daily_pnl"`
    Timestamp     time.Time `json:"timestamp"`
}
```

### 1.4 Milestone

**M1.1: Execution Abstraction Complete** (End of Week 5)

- [ ] `ExecutionService` interface defined and reviewed
- [ ] `BacktestExecutionService` implements all methods using existing Tracker
- [ ] `Engine` uses `ExecutionService` exclusively (no direct Tracker calls)
- [ ] All 55+ existing backtest tests pass without modification
- [ ] Backtest performance unchanged (±5% tolerance)

---

## Phase 2: Strategy Framework Refactor

> **Objective**: Eliminate duplicate code, unify dual interfaces, enhance thread safety, and establish a clean foundation for strategy development.

### 2.1 Motivation

Current state (from ADR-014):
- 50+ duplicate code blocks across strategy files
- `domain.Strategy` (old) and `strategy.Strategy` (new) interfaces coexist
- `Configure()` methods lack mutex protection
- `value_quality.go` violates single responsibility (contains 2 strategies)

### 2.2 Task Breakdown

#### P2-A: Extract Utilities & Base Classes (Week 6)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P2-A1 | Create `pkg/strategy/utils.go` with `SortOHLCV`, `SortedCopy`, `LatestPrice` | New | 0.5d |
| P2-A2 | Add `ParseIntParam`, `ParseFloatParam`, `ParseStringParam` to utils | New | 0.5d |
| P2-A3 | Create `pkg/strategy/base.go` with `ConfigurableBase` (mutex-protected) | New | 0.5d |
| P2-A4 | Create `pkg/strategy/errors.go` with `SignalError` type | New | 0.5d |
| P2-A5 | Add comprehensive unit tests for utils and base | New tests | 1d |

#### P2-B: Migrate Strategy Implementations (Week 6-7)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P2-B1 | Refactor `momentum.go` to use `ConfigurableBase` + utils | `plugins/momentum.go` | 0.5d |
| P2-B2 | Refactor `mean_reversion.go` to use `ConfigurableBase` + utils | `plugins/mean_reversion.go` | 0.5d |
| P2-B3 | Refactor `multi_factor.go` to use `ConfigurableBase` + utils | `plugins/multi_factor.go` | 0.5d |
| P2-B4 | Refactor `value_screen.go` to use `ConfigurableBase` + utils | `plugins/value_screen.go` | 0.5d |
| P2-B5 | Split `value_quality.go` → `value.go` + `quality.go` | `plugins/value_quality.go` | 0.5d |
| P2-B6 | Refactor new `value.go` to use `ConfigurableBase` + utils | `plugins/value.go` | 0.5d |
| P2-B7 | Refactor new `quality.go` to use `ConfigurableBase` + utils | `plugins/quality.go` | 0.5d |
| P2-B8 | Migrate `examples/momentum.go` → `plugins/example_momentum.go` | `examples/`, `plugins/` | 0.5d |
| P2-B9 | Migrate `examples/value_momentum.go` → `plugins/example_value_momentum.go` | `examples/`, `plugins/` | 0.5d |
| P2-B10 | Add parameter validation to all strategies (range checks) | All `plugins/*.go` | 0.5d |

#### P2-C: Interface Unification & Registry Cleanup (Week 7)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P2-C1 | Mark `domain.Strategy` as deprecated with migration guide | `pkg/domain/strategy.go` | 0.5d |
| P2-C2 | Update `Registry` to remove `oldRegistry` dependency | `pkg/strategy/registry.go` | 1d |
| P2-C3 | Update `cmd/strategy` service to use new `strategy.Strategy` interface | `cmd/strategy/` | 1d |
| P2-C4 | Simplify `getSignals` in backtest engine (remove old interface mapping) | `pkg/backtest/engine.go` | 0.5d |
| P2-C5 | Mark `examples/` directory as deprecated | `pkg/strategy/examples/README.md` | 0.5d |
| P2-C6 | Update `GlobalRegister` to use mutex-protected registration | `pkg/strategy/registry.go` | 0.5d |

### 2.3 Key Design Decisions

```go
// pkg/strategy/base.go

type ConfigurableBase struct {
    mu     sync.RWMutex
    params map[string]any
}

func (c *ConfigurableBase) Configure(params map[string]any) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.params = params
    return nil
}

func (c *ConfigurableBase) GetInt(key string, def int) int {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return ParseIntParam(c.params, key, def)
}

// Usage in strategy:
type momentumStrategy struct {
    ConfigurableBase
    lookbackDays int
}

func (s *momentumStrategy) Configure(params map[string]any) error {
    if err := s.ConfigurableBase.Configure(params); err != nil {
        return err
    }
    s.lookbackDays = s.GetInt("lookback_days", 20)
    if s.lookbackDays < 5 || s.lookbackDays > 250 {
        return fmt.Errorf("lookback_days must be in [5, 250], got %d", s.lookbackDays)
    }
    return nil
}
```

### 2.4 Milestone

**M2.1: Strategy Framework Refactored** (End of Week 7)

- [ ] All strategies use `ConfigurableBase` (thread-safe)
- [ ] All strategies use `Parse*Param` from utils (no duplicate type switches)
- [ ] All strategies use `SortedCopy` (no duplicate sort logic)
- [ ] `value_quality.go` split into two files
- [ ] `domain.Strategy` marked deprecated, `strategy.Strategy` is sole interface
- [ ] `oldRegistry` removed, all services use unified `Registry`
- [ ] `pkg/strategy` test coverage ≥ 50% (from 12.3%)

---

## Phase 3: Factor System Extension

> **Objective**: Complete the factor system by implementing missing factor types (Size, Volatility, Growth) and adding neutralization + IC analysis capabilities.

### 3.1 Motivation

Current state: 6 factor types defined in `domain.FactorType`, but only 3 have computation logic (`Momentum`, `Value`, `Quality`).

Target state: All 6 factors computable, with industry/market-cap neutralization and IC (Information Coefficient) analysis.

### 3.2 Task Breakdown

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P3-1 | Implement `ComputeSizeFactor` (log market cap inverse Z-Score) | `pkg/data/factor.go` | 0.5d |
| P3-2 | Implement `ComputeVolatilityFactor` (N-day return stddev, low = high score) | `pkg/data/factor.go` | 0.5d |
| P3-3 | Implement `ComputeGrowthFactor` (YoY revenue/earnings growth) | `pkg/data/factor.go` | 0.5d |
| P3-4 | Parallelize `ComputeAllFactors` using goroutines | `pkg/data/factor.go` | 0.5d |
| P3-5 | Create `pkg/data/factor_neutral.go` with `Neutralize` function | New | 1d |
| P3-6 | Create `pkg/data/factor_ic.go` with IC calculation | New | 0.5d |
| P3-7 | Update `FactorType` enum and `ParseFactorType` | `pkg/domain/factor.go` | 0.5d |
| P3-8 | Add database migration for new factor cache columns | `migrations/` | 0.5d |
| P3-9 | Unit tests for all new factor computations | New tests | 1d |
| P3-10 | Benchmark tests for factor computation performance | New tests | 0.5d |

### 3.3 Key Design Decisions

```go
// pkg/data/factor_neutral.go

// Neutralize performs industry and market-cap neutralization.
// For each group (e.g., industry), subtract the group mean from factor scores.
func Neutralize(
    scores map[string]float64,
    groups map[string]string,      // symbol -> industry group
    marketCaps map[string]float64, // symbol -> market cap (for size neutralization)
) map[string]float64 {
    // 1. Group by industry
    // 2. For each group, compute weighted mean (weighted by market cap)
    // 3. Subtract group mean from each symbol's score
    // 4. Return neutralized scores
}

// pkg/data/factor_ic.go

// ComputeIC calculates Spearman rank correlation between factor scores
// and forward returns (typically 1-month forward).
func ComputeIC(
    factorScores map[string]float64,
    forwardReturns map[string]float64,
) (ic float64, pValue float64) {
    // Spearman rank correlation
}

// ComputeICSeries calculates IC for each date in a time series.
func ComputeICSeries(
    factorHistory map[time.Time]map[string]float64,
    returnsHistory map[time.Time]map[string]float64,
) []ICEntry {
    // Returns time series of IC values for stability analysis
}
```

### 3.4 Milestone

**M3.1: Factor System Complete** (End of Week 8)

- [ ] All 6 factor types have computation implementations
- [ ] `ComputeAllFactors` runs in parallel (3× speedup)
- [ ] `Neutralize` supports industry and market-cap neutralization
- [ ] `ComputeIC` and `ComputeICSeries` implemented
- [ ] `pkg/data` test coverage ≥ 60% (from 26.7%)
- [ ] Factor computation benchmark baseline established

---

## Phase 4: Live Trading Infrastructure

> **Objective**: Build the live trading infrastructure including paper trading simulation, event-driven engine, and order/position management.

### 4.1 Motivation

Current state: `pkg/live/trader.go` exists but is a minimal stub. No `LiveEngine`, no order management, no real-time data feed integration.

Target state: Complete paper trading capability with event-driven architecture, ready for broker adapter integration.

### 4.2 Task Breakdown

#### P4-A: Core Live Trading Components (Week 8-9)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P4-A1 | Create `pkg/live/engine.go` — event-driven live engine | New | 1.5d |
| P4-A2 | Create `pkg/live/order_manager.go` — order lifecycle management | New | 1d |
| P4-A3 | Create `pkg/live/position_manager.go` — real-time position tracking | New | 1d |
| P4-A4 | Implement `SimulatedBroker` (for paper trading) | New | 1d |
| P4-A5 | Create `pkg/live/data_feed.go` — WebSocket real-time data abstraction | New | 1d |
| P4-A6 | Integrate `RiskManager` into live engine | `pkg/live/engine.go` | 0.5d |

#### P4-B: Data Feed & Market Data (Week 9)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P4-B1 | Implement `WebSocketDataFeed` for real-time ticks | New | 1d |
| P4-B2 | Implement `Quote` type and L1 order book handling | `pkg/domain/` | 0.5d |
| P4-B3 | Add tick-to-OHLCV aggregation for strategy consumption | New | 0.5d |
| P4-B4 | Create `RealTimeProvider` implementation | `pkg/marketdata/` | 0.5d |

#### P4-C: Paper Trading & Simulation (Week 9-10)

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P4-C1 | Implement slippage model in `SimulatedBroker` | `pkg/live/` | 0.5d |
| P4-C2 | Implement commission model (A-share specific: 0.025% + stamp tax) | `pkg/live/` | 0.5d |
| P4-C3 | Implement market impact model for large orders | `pkg/live/` | 0.5d |
| P4-C4 | Paper trading API endpoints | `cmd/analysis/main.go` | 0.5d |
| P4-C5 | Paper trading dashboard UI components | `web/src/components/` | 1d |

### 4.3 Key Design Decisions

```go
// pkg/live/engine.go

type LiveEngine struct {
    strategy    strategy.Strategy
    execution   domain.ExecutionService
    dataFeed    RealTimeDataFeed
    riskMgr     domain.RiskManager
    orderMgr    *OrderManager
    positionMgr *PositionManager
    portfolio   *domain.Portfolio
    mode        EngineMode // paper or live
}

func (e *LiveEngine) Run(ctx context.Context) error {
    // 1. Initialize: load positions, subscribe to data
    // 2. Event loop:
    //    a. Receive tick/bar event
    //    b. Update prices, check stop losses
    //    c. Generate signals from strategy
    //    d. Apply risk management
    //    e. Submit orders via execution service
    //    f. Handle fill events
    // 3. Graceful shutdown on context cancellation
}

// pkg/live/order_manager.go

type OrderManager struct {
    orders      map[string]*domain.Order
    pending     map[string]*domain.Order
    mu          sync.RWMutex
    subscribers []FillEventHandler
}

func (om *OrderManager) Submit(order domain.Order) (*domain.OrderResult, error)
func (om *OrderManager) HandleFill(event FillEvent) error
func (om *OrderManager) GetOpenOrders() []domain.Order
```

### 4.4 Milestone

**M4.1: Paper Trading Ready** (End of Week 10)

- [ ] `LiveEngine` runs event-driven paper trading loop
- [ ] `SimulatedBroker` implements realistic A-share commission/slippage
- [ ] `OrderManager` handles order lifecycle (submit → partial fill → complete)
- [ ] `PositionManager` tracks real-time P&L
- [ ] WebSocket data feed receives and processes real-time ticks
- [ ] Paper trading API endpoint accepts strategy + symbols + duration
- [ ] Basic UI shows paper trading status and P&L

---

## Phase 5: Testing, Validation & Documentation

> **Objective**: Ensure all changes are thoroughly tested, documented, and validated against existing functionality.

### 5.1 Task Breakdown

| Task ID | Task | Files | Effort |
|---------|------|-------|--------|
| P5-1 | Write unit tests for all refactored strategies (≥ 3 per strategy) | `pkg/strategy/plugins/*_test.go` | 2d |
| P5-2 | Write integration tests for backtest engine with `ExecutionService` | `pkg/backtest/*_test.go` | 1d |
| P5-3 | Write integration tests for `LiveEngine` paper trading | `pkg/live/*_test.go` | 1d |
| P5-4 | Write unit tests for AI Agent components | `pkg/ai/**/*_test.go` | 2d |
| P5-5 | Run full regression test suite (all Go tests) | All `*_test.go` | 0.5d |
| P5-6 | Run E2E tests (Playwright) | `e2e/tests/` | 0.5d |
| P5-7 | Performance benchmark: backtest speed vs baseline | Benchmark scripts | 0.5d |
| P5-8 | Update SPEC.md with new interfaces and APIs | `docs/SPEC.md` | 0.5d |
| P5-9 | Update ARCHITECTURE.md with live trading module | `docs/ARCHITECTURE.md` | 0.5d |
| P5-10 | Update AGENTS.md with new patterns and conventions | `docs/AGENTS.md` | 0.5d |
| P5-11 | Create migration guide for strategy developers | `docs/guides/` | 0.5d |
| P5-12 | Create ADR-015: Live Trading Architecture | `docs/adr/` | 0.5d |
| P5-13 | Update TASKS.md and tasks-phase-2.md — mark completed tasks | `docs/TASKS.md`, `docs/tasks-phase-2.md` | 0.5d |

### 5.2 Success Criteria

**M5.1: Release Ready** (End of Week 11)

- [ ] `go vet ./...` passes with zero warnings
- [ ] `go test ./...` passes with ≥ 80% overall coverage
- [ ] `pkg/strategy` coverage ≥ 70% (from 12.3%)
- [ ] `pkg/data` coverage ≥ 80% (from 26.7%)
- [ ] `pkg/backtest` coverage ≥ 75% (from 72.5%)
- [ ] `pkg/ai` coverage ≥ 60% (new module)
- [ ] `pkg/live` coverage ≥ 60% (new module)
- [ ] E2E tests pass (Chrome project)
- [ ] Backtest performance within ±5% of baseline
- [ ] All documentation updated and cross-referenced
- [ ] ADR-014 status updated to "Accepted", ADR-015 created

---

## Resource Allocation

### Human Resources

| Role | Allocation | Responsibilities |
|------|-----------|-----------------|
| **Backend Lead** | 100% (11 weeks) | Architecture decisions, code review, complex refactors |
| **Backend Dev 1** | 100% (11 weeks) | Phase 0 (AI Agent) + Phase 1 + Phase 4 |
| **Backend Dev 2** | 100% (8 weeks) | Phase 2 + Phase 3 (strategy refactor + factors) |
| **ML Engineer** | 100% (3 weeks) | Phase 0: LLM integration, prompt engineering, factor DSL |
| **Frontend Dev** | 50% (6 weeks) | AI dashboard, paper trading UI, strategy genealogy |
| **QA Engineer** | 75% (8 weeks) | Test writing, regression testing, benchmark |
| **Tech Writer** | 25% (3 weeks) | Documentation updates, migration guides |

### Infrastructure

| Resource | Purpose | Duration |
|---------|---------|----------|
| **CI/CD Runner** (8 vCPU, 16GB) | Parallel test execution | 11 weeks |
| **Staging DB** (PostgreSQL) | Integration testing + gene pool | 11 weeks |
| **Redis Cache** | Factor cache + AI job queue | 11 weeks |
| **LLM API Budget** | Claude/GPT-4o for agent tasks | 3 weeks (Phase 0) |
| **GPU Instance** (optional) | Local LLM inference (DeepSeek) | 3 weeks (Phase 0) |
| **Paper Trading Account** | Simulated broker testing | 4 weeks (Phase 4) |

---

## Dependency Graph

```
Phase 0 (AI Agent)
    │
    ├──► Phase 1 (ExecutionService)
    │       │
    │       ├──► Phase 2 (Strategy Refactor) ──► Phase 3 (Factor Extension)
    │       │                                        │
    │       │                                        ▼
    │       │                                   Phase 5 (Testing)
    │       │
    │       └──► Phase 4 (Live Trading) ─────────────► Phase 5 (Testing)
    │
    └──► Phase 5 (Documentation updates)
```

**Critical Path**: Phase 0 → Phase 1 → Phase 2 → Phase 5 (7 weeks minimum)  
**Parallel Path**: Phase 0 → Phase 1 → Phase 4 → Phase 5 (can run alongside Phase 2-3)

---

## Risk Register

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|-----------|
| LLM-generated code quality inconsistent | Medium | High | Multi-layer validation: syntax → compile → sandbox backtest → human review |
| AI Agent factor discovery produces spurious factors | Medium | High | Strict IC threshold (> 0.03), out-of-sample validation, human curation layer |
| LLM API costs exceed budget | Medium | Medium | Cache prompts/responses, use local LLM (DeepSeek) for simple tasks, rate limiting |
| Backtest regression after ExecutionService refactor | Medium | High | Comprehensive test suite before/after comparison |
| `oldRegistry` removal breaks external plugins | Medium | Medium | Deprecation period + compatibility shim |
| Live data feed latency unacceptable | Low | High | Start with polling fallback, upgrade to WebSocket |
| Factor computation performance degradation | Low | Medium | Benchmark before/after, keep serial fallback |
| Strategy mutex causes deadlock | Low | High | Timeout on lock acquisition, deadlock detection |

---

## Appendix A: File Change Summary

### New Files (AI Agent — Phase 0)

```
pkg/ai/agents/research.go         # LLM-driven factor hypothesis generation
pkg/ai/agents/generate.go         # Natural language to strategy code
pkg/ai/agents/validate.go         # Backtest validation orchestrator
pkg/ai/agents/evolve.go           # Genetic algorithm orchestrator
pkg/ai/expression/engine.go       # Factor expression DSL + AST evaluator
pkg/ai/expression/parser.go       # DSL string → AST
pkg/ai/expression/evaluator.go    # AST vectorized evaluation
pkg/ai/expression/operators.go    # Operator implementations
pkg/ai/gene_pool/factor_pool.go   # Factor gene pool (PostgreSQL)
pkg/ai/gene_pool/strategy_pool.go # Strategy gene pool (PostgreSQL)
pkg/ai/client/backtest_client.go  # Backtest HTTP client
pkg/ai/client/factor_client.go    # Factor computation client
pkg/ai/metrics/ic.go              # IC calculation
pkg/ai/metrics/turnover.go        # Turnover calculation
pkg/ai/prompts/factor_research.txt   # Factor research prompt template
pkg/ai/prompts/strategy_generate.txt # Strategy generation prompt template
pkg/ai/validator/code_validator.go   # Code validation (syntax + compile + sandbox)
pkg/ai/drift/detector.go          # Concept drift detection (ADWIN + PH)
pkg/ai/evolution/population.go    # Population management
pkg/ai/evolution/selection.go     # Selection operators
pkg/ai/evolution/crossover.go     # Crossover operators
pkg/ai/evolution/mutation.go      # Mutation operators
web/src/components/ai/FactorLab.vue       # Factor laboratory UI
web/src/components/ai/StrategyWorkshop.vue # Strategy workshop UI
web/src/components/ai/EvolutionObs.vue    # Evolution observatory UI
web/src/components/ai/FactorCard.vue      # Factor card component
web/src/components/ai/StrategyCard.vue    # Strategy card component
web/src/components/ai/GenealogyTree.vue   # Genealogy tree visualization
web/src/components/ai/FitnessChart.vue    # Fitness evolution chart
web/src/pages/AIResearch.vue              # AI Research main page
cmd/ai/main.go                    # AI Research Service entry point
config/ai-service.yaml            # AI Service configuration
```

### New Files (Infrastructure — Phases 1-4)

```
pkg/domain/execution.go          # ExecutionService interface + types
pkg/strategy/utils.go            # SortOHLCV, Parse*Param utilities
pkg/strategy/base.go             # ConfigurableBase with mutex
pkg/strategy/errors.go           # SignalError type
pkg/data/factor_neutral.go       # Neutralization logic
pkg/data/factor_ic.go            # IC calculation
pkg/live/engine.go               # Event-driven live engine
pkg/live/order_manager.go        # Order lifecycle management
pkg/live/position_manager.go     # Real-time position tracking
pkg/live/simulated_broker.go     # Paper trading broker
pkg/live/data_feed.go            # Real-time data abstraction
pkg/marketdata/realtime_provider.go  # WebSocket data provider
```

### Modified Files

```
pkg/backtest/engine.go           # Use ExecutionService, add EngineMode
pkg/backtest/engine_daily.go     # Decouple from direct Tracker calls
pkg/backtest/tracker.go          # Extract execution logic
pkg/strategy/strategy.go         # Keep as sole interface
pkg/strategy/registry.go         # Remove oldRegistry
pkg/strategy/plugins/*.go        # Use ConfigurableBase + utils
pkg/data/factor.go               # Add Size/Volatility/Growth + parallelization
pkg/domain/factor.go             # Update FactorType enum
pkg/domain/types.go              # Add Quote, RealTimeProvider types
cmd/analysis/main.go             # Add paper trading API endpoints
```

### Deleted Files

```
pkg/strategy/examples/           # Migrate to plugins/ then remove
```

---

## Appendix B: Weekly Sprint Schedule

| Week | Focus | Key Deliverables |
|------|-------|-----------------|
| **W0** | P0-A: Research Agent + Factor Expression DSL | Factor validator operational, 5+ candidate factors |
| **W1** | P0-B: Generate Agent + P0-C: Validate Agent starts | Strategy generation pipeline, validation framework |
| **W2** | P0-C: Validate completes + P0-D: Evolve Agent | Walk-Forward validation, drift detection, gene pool |
| **W3** | P0-D: Evolve Agent + AI Dashboard + M0.1 validation | **M0.1 complete**: 10+ factors, 80%+ gen success rate |
| **W4** | P1: ExecutionService abstraction + backtest decoupling | **M1.1 complete** |
| **W5** | P2-A: Utilities + base classes; P2-B: Strategy migration starts | M2.1 partially |
| **W6** | P2-B: Strategy migration completes; P2-C: Interface unification | **M2.1 complete** |
| **W7** | P3: Factor extension + P4-A: Live engine core | **M3.1** + M4.1 partially |
| **W8** | P4-B: Data feed + P4-C: Paper trading simulation | M4.1 partially |
| **W9** | P4-C: Paper trading UI + API + integration | **M4.1 complete** |
| **W10** | P5: Testing + documentation + release prep | **M5.1 complete** |
| **W11** | Buffer week: bug fixes, performance tuning, final validation | Release ready |

---

_Last updated: 2026-05-04_
_Status: Proposed — awaiting approval to begin implementation_
