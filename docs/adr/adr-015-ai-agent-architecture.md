# ADR-015: AI Agent Quantitative Research Architecture

> **Status**: Proposed  
> **Date**: 2026-05-04  
> **Category**: Architecture  
> **Related ADRs**: ADR-014 (Strategy Framework Refactor), ADR-007 (AI Sandbox)  
> **Author**: AI Assistant

---

## Context

Following the completion of Phase 3 (Integration & Scale), the project has established a solid foundation:
- Robust backtest engine with A-share specific rules (T+1, limit up/down)
- Multi-factor framework with 3 implemented factors (Momentum, Value, Quality)
- Strategy plugin system with hot-swap capability
- Vue 3 frontend with comprehensive backtest visualization

The next strategic evolution is to transform Quant Lab from a manual quantitative research tool into an AI-native platform where AI acts as a senior quantitative researcher — autonomously discovering alpha factors, generating trading strategies, and validating hypotheses through the existing backtest infrastructure.

This ADR defines the architecture for the AI Agent system, building upon the existing foundation rather than replacing it.

## Decision

### 1. AI as Augmentation Layer, Not Replacement

The AI Agent system is designed as an **augmentation layer** that sits alongside the existing services:

```
┌─────────────────────────────────────────────────────────────────┐
│                    AI Research Service (:8086)                   │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │  Research   │  │  Generate   │  │  Validate   │            │
│  │   Agent     │  │   Agent     │  │   Agent     │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ (HTTP API calls)
┌─────────────────────────────────────────────────────────────────┐
│                 Analysis Service (:8085) — Existing              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │   Backtest  │  │   Batch     │  │   Factor    │            │
│  │   Engine    │  │   Engine    │  │  Analyzer   │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

**Rationale**:
- The existing backtest engine is the core differentiator (A-share rules, performance)
- AI Agents call the backtest service for validation, ensuring consistency
- Human researchers retain final decision authority (compliance, risk management)

### 2. Multi-Agent Architecture

Four specialized agents collaborate through a shared gene pool:

| Agent | Responsibility | Input | Output |
|-------|---------------|-------|--------|
| **Research** | Factor hypothesis generation | Natural language prompt, market data | FactorExpression (DSL) |
| **Generate** | Strategy code generation | FactorExpression, strategy template | Go code (Strategy interface) |
| **Validate** | Multi-layer validation | FactorExpression or Go code | ValidationResult (IC, Sharpe, etc.) |
| **Evolve** | Population evolution | Strategy population, drift signals | New generation of strategies |

### 3. Factor Expression DSL

AI generates **factor expressions** rather than raw Go code:

```go
type FactorExpression struct {
    ID       string
    Formula  string      // e.g., "ts_corr(close, volume, 20) / ts_std(returns, 60)"
    AST      *ExprNode   // Parsed AST for evaluation
    Inputs   []string    // Required raw data fields
    Category string      // "momentum" | "value" | "quality" | "custom"
}
```

**Rationale**:
- Expressions are more concise and less error-prone than full code
- Expressions can be validated for future-function freedom
- Expressions enable vectorized computation (performance)
- Expressions are composable (factor combinations)

### 4. Layered Validation Pipeline

Every AI-generated artifact must pass through a validation funnel:

| Layer | Purpose | Duration | Gate |
|-------|---------|----------|------|
| L1 | Syntax validation (DSL parser) | < 1s | Expression well-formed |
| L2 | Quick backtest (1yr/100 stocks) | < 10s | IC > 0.02 |
| L3 | Standard backtest (3yr/500 stocks) | < 2min | Sharpe > 0.5 |
| L4 | Walk-Forward validation | < 10min | Out-of-sample IC > 0.015 |
| L5 | Human review | — | Final approval |

### 5. Gene Pool for Persistence

All factors and strategies are persisted to a gene pool (PostgreSQL JSONB):

```sql
CREATE TABLE factor_genes (
    id VARCHAR(64) PRIMARY KEY,
    expression JSONB NOT NULL,      -- FactorExpression
    performance JSONB,              -- IC history, Sharpe, etc.
    metadata JSONB,                 -- Category, creation date, parent IDs
    status VARCHAR(20) DEFAULT 'pending', -- pending | validated | archived
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE strategy_genes (
    id VARCHAR(64) PRIMARY KEY,
    code TEXT NOT NULL,             -- Go source code
    params JSONB,                   -- Optimized parameters
    factors JSONB,                  -- Referenced factor IDs
    performance JSONB,              -- Backtest metrics history
    genealogy JSONB,                -- Parent IDs, generation number
    status VARCHAR(20) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

### 6. LLM Provider Strategy

| Provider | Use Case | Fallback Chain |
|----------|----------|---------------|
| Claude 3.5 Sonnet | Primary for code generation (highest accuracy) | — |
| GPT-4o | Fast tasks (summarization, simple prompts) | Claude 3.5 |
| DeepSeek-Coder | Offline/local inference (data privacy) | GPT-4o |

## Consequences

### Positive

- **Leverages existing assets**: Backtest engine, data pipeline, strategy framework remain central
- **Scalable research**: AI can explore factor combinations exponentially faster than humans
- **Consistent validation**: Same backtest engine for manual and AI-generated strategies
- **Audit trail**: Gene pool provides complete lineage of factor/strategy evolution
- **Human oversight**: Layered validation ensures human review before production

### Negative

- **LLM costs**: API calls for factor discovery and strategy generation incur ongoing costs
- **Complexity**: Multi-agent system adds operational complexity
- **Hallucination risk**: LLMs may generate mathematically unsound expressions
- **Latency**: AI generation + validation pipeline takes minutes per iteration

## Migration Plan

1. **Week 1-2**: Build Expression Engine + Research Agent MVP
2. **Week 3-4**: Integrate with existing backtest engine via HTTP client
3. **Week 5-6**: Add Generate Agent + strategy templates
4. **Week 7-8**: Implement Validate Agent with layered pipeline
5. **Week 9-10**: Build Evolve Agent + gene pool
6. **Week 11-12**: Frontend UI + documentation + release

## Artifacts

- New service: `cmd/ai/main.go` (AI Research Service)
- New packages: `pkg/ai/agents/`, `pkg/ai/expression/`, `pkg/ai/gene_pool/`, `pkg/ai/client/`
- New frontend: `web/src/components/ai/`, `web/src/pages/AIResearch.vue`
- New tables: `factor_genes`, `strategy_genes`
- New ADR: This document

## Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Factor discovery rate | 10+ factors/week with IC > 0.03 | Automated validation |
| Strategy generation success | > 80% compile on first attempt | Build success rate |
| Validation pipeline time | < 15 min end-to-end | Timer logs |
| LLM cost per factor | < $1 | API billing |
| Human review rate | 100% before production | Workflow enforcement |

---

_Last updated: 2026-05-04_
