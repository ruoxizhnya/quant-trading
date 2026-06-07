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

## ✅ Phase 4 验收对照 (2026-05-17)

> 关联任务: TASKS P2-20 | ODR-010

### 维度 1: AI 作为增强层（非替代）

| 验收项 | 状态 | 证据 |
|-------|-----|------|
| AI Service 独立部署 (:8086) | ✅ | `cmd/ai/main.go` 已实现并运行 |
| 通过 HTTP 调用 backtest engine | ✅ | `pkg/ai/client/backtest_client.go` |
| 不修改 analysis-service 核心 | ✅ | `cmd/analysis/main.go` 无 AI 相关代码 |
| 人类决策保留 | ✅ | 5 层验证管道 L5 (Human Review) |

### 维度 2: 多 Agent 架构

| Agent | 责任 | 实现状态 | 文件 |
|-------|-----|---------|------|
| Research | 因子假设生成 | ✅ | `pkg/ai/agents/research_agent.go` |
| Generate | 策略代码生成 | ✅ | `pkg/ai/agents/generate_agent.go` |
| Validate | 多层验证 | ✅ | `pkg/ai/agents/validate_agent.go` |
| Evolve | 种群进化 | ✅ | `pkg/ai/agents/evolve_agent.go` |

### 维度 3: Factor Expression DSL

| 验收项 | 状态 | 证据 |
|-------|-----|------|
| FactorExpression struct | ✅ | `pkg/ai/expression/types.go` |
| AST 解析器 | ✅ | `pkg/ai/expression/parser.go` (coverage 75.2%) |
| DSL 函数集 (ts_corr/ts_std 等) | ✅ | `pkg/ai/expression/builtins.go` |

### 维度 4: 分层验证管道

| Layer | 用途 | 状态 | 验证标准 |
|-------|------|-----|---------|
| L1 | 语法验证 (DSL parser) | ✅ | Expression well-formed (< 1s) |
| L2 | 快速回测 (1yr/100 stocks) | ✅ | IC > 0.02 (< 10s) |
| L3 | 标准回测 (3yr/500 stocks) | ✅ | Sharpe > 0.5 (< 2min) |
| L4 | Walk-Forward 验证 | ✅ | Out-of-sample IC > 0.015 (< 10min) |
| L5 | 人工审查 | ⚠️ | UI 集成中 (PipelineDashboard.vue 已实现) |

### 维度 5: Gene Pool 持久化

| 验收项 | 状态 | 证据 |
|-------|-----|------|
| `factor_genes` 表 | ✅ | `migrations/012_add_gene_pool_tables.sql` + `pkg/ai/gene_pool/factor_pool.go` |
| `strategy_genes` 表 | ✅ | `migrations/012_add_gene_pool_tables.sql` + `pkg/ai/gene_pool/strategy_pool.go` |
| 系谱追踪 (genealogy JSONB) | ✅ | `pkg/ai/gene_pool/genealogy.go` |
| 状态机 (pending → validated → archived) | ✅ | `pkg/ai/gene_pool/*_pool.go` |

### 综合评分

| 维度 | 完成度 | 备注 |
|-----|-------|------|
| 1. AI 增强层架构 | 100% | ✅ 5/5 |
| 2. 多 Agent 架构 | 100% | ✅ 4/4 agents |
| 3. Factor DSL | 100% | ✅ 解析 + 求值 + 内置函数 |
| 4. 分层验证 | 90% | ⚠️ L5 人工审查 UI 待完善 |
| 5. Gene Pool | 100% | ✅ 因子 + 策略表完整 |
| **综合** | **98%** | Phase 4 核心交付完成 |

### 验收遗留项

1. **P2-20-a**: L5 人工审查 UI 完善 (PipelineDashboard.vue 增补)
2. **P2-20-b**: 指标追踪（ADR-015 §Metrics）建立 dashboard
3. **P2-20-c**: LLM 成本监控接入

---
