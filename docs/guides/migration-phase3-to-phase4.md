# Migration Guide: Phase 3 → Phase 4 (AI-Native Evolution)

> **Version**: 1.0.0
> **Date**: 2026-05-06
> **Status**: Active
> **Scope**: Migration from Phase 3 (Integration & Scale) to Phase 4 (AI-Native Evolution)
> **Related**: [VISION.md](../VISION.md), [ADR-015](../adr/adr-015-ai-agent-architecture.md), [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md)

---

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Database Migrations](#database-migrations)
4. [Service Changes](#service-changes)
5. [Configuration Updates](#configuration-updates)
6. [API Changes](#api-changes)
7. [Frontend Changes](#frontend-changes)
8. [Testing Requirements](#testing-requirements)
9. [Rollback Procedure](#rollback-procedure)
10. [Troubleshooting](#troubleshooting)

---

## Overview

Phase 4 introduces the AI-Native Evolution platform, transforming the system from a traditional quant trading tool into an AI-assisted research platform. This migration guide covers the steps required to upgrade from Phase 3 to Phase 4.

### Key Changes

| Component | Phase 3 | Phase 4 |
|-----------|---------|---------|
| AI System | Strategy Copilot (chat-based) | Full AI Agent system (Research/Generate/Validate/Evolve) |
| Factor Discovery | Manual | LLM-driven automated discovery |
| Strategy Generation | Template-based | Natural language → Go code |
| Validation | Manual backtest | L1-L4 automated pipeline |
| Execution | Backtest only | Unified ExecutionService (backtest/paper/live) |
| Gene Pool | N/A | PostgreSQL JSONB factor/strategy genes |
| UI | Basic backtest dashboard | AI Research Platform (FactorLab, StrategyWorkshop, EvolutionObs) |
| Services | 5 services | 6 services (+ AI Research Service on :8086) |

---

## Prerequisites

### System Requirements

- **Go**: 1.21+ (unchanged)
- **Node.js**: 18+ (unchanged)
- **PostgreSQL**: 14+ with TimescaleDB 2.11+ (unchanged)
- **Redis**: 7+ (unchanged)
- **Docker**: 24+ (unchanged)
- **Docker Compose**: 2.20+ (unchanged)

### Phase 3 Completion Checklist

Before starting migration, verify:

- [ ] All Phase 3 P0 features complete (see [VISION.md](../VISION.md) Phase 3 exit criteria)
- [ ] `go test ./...` passes with ≥80% coverage on core packages
- [ ] `npm run build` succeeds in `web/` directory
- [ ] `docker compose up -d` starts all services successfully
- [ ] Database migrations up to `011_add_strategies_table.sql` applied
- [ ] Backup of PostgreSQL database completed
- [ ] Backup of Redis data completed

### Required Access

- PostgreSQL superuser access
- Redis CLI access
- Docker Compose control
- LLM API key (OpenAI/Anthropic/compatible) for AI Research Service

---

## Database Migrations

### New Tables (Phase 4)

Execute these migrations in order:

```sql
-- 012_add_factor_genes_table.sql
CREATE TABLE factor_genes (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    formula TEXT NOT NULL,
    category VARCHAR(50) NOT NULL CHECK (category IN ('momentum', 'value', 'quality', 'volatility', 'liquidity', 'custom')),
    description TEXT,
    ic_history JSONB DEFAULT '{}',
    performance JSONB DEFAULT '{}',
    genealogy JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_factor_genes_category ON factor_genes(category);
CREATE INDEX idx_factor_genes_created_at ON factor_genes(created_at);

-- 013_add_strategy_genes_table.sql
CREATE TABLE strategy_genes (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    code TEXT NOT NULL,
    params JSONB DEFAULT '{}',
    fitness JSONB DEFAULT '{}',
    genealogy JSONB DEFAULT '{}',
    generation INT DEFAULT 0,
    parent_ids INT[] DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'retired', 'under_review')),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_strategy_genes_generation ON strategy_genes(generation);
CREATE INDEX idx_strategy_genes_status ON strategy_genes(status);
CREATE INDEX idx_strategy_genes_created_at ON strategy_genes(created_at);

-- 014_add_ai_research_jobs_table.sql
CREATE TABLE ai_research_jobs (
    id SERIAL PRIMARY KEY,
    job_type VARCHAR(50) NOT NULL CHECK (job_type IN ('factor_discovery', 'strategy_generation', 'evolution_run', 'validation')),
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    params JSONB DEFAULT '{}',
    results JSONB DEFAULT '{}',
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_ai_research_jobs_status ON ai_research_jobs(status);
CREATE INDEX idx_ai_research_jobs_type ON ai_research_jobs(job_type);
```

### Migration Commands

```bash
# Apply migrations
psql -U $DB_USER -d $DB_NAME -f migrations/012_add_factor_genes_table.sql
psql -U $DB_USER -d $DB_NAME -f migrations/013_add_strategy_genes_table.sql
psql -U $DB_USER -d $DB_NAME -f migrations/014_add_ai_research_jobs_table.sql

# Verify
psql -U $DB_USER -d $DB_NAME -c "\dt"
```

---

## Service Changes

### New Service: AI Research Service

**Port**: 8086
**Entry Point**: `cmd/ai/main.go`
**Purpose**: LLM-driven strategy generation, factor discovery, evolution pipeline

#### Startup

```bash
# Development
cd cmd/ai && go run main.go

# Production (via Docker Compose)
docker compose up -d ai-research-service
```

#### Configuration

Create `config/ai-service.yaml`:

```yaml
service:
  name: ai-research-service
  port: 8086
  log_level: info

llm:
  provider: openai  # or anthropic, local
  api_key: ${LLM_API_KEY}
  model: gpt-4-turbo-preview
  timeout: 30s
  max_tokens: 4096

backtest:
  endpoint: http://analysis-service:8085
  timeout: 60s

gene_pool:
  max_factors: 1000
  max_strategies: 500
  min_ic_threshold: 0.03

evolution:
  population_size: 50
  max_generations: 100
  elite_count: 5
  crossover_rate: 0.8
  mutation_rate: 0.15
```

### Updated Services

| Service | Changes |
|---------|---------|
| Analysis Service (8085) | Added proxy routes to AI Research Service; ExecutionService integration |
| Data Service (8081) | Unchanged |
| Strategy Service (8082) | Unchanged (standby) |

### Docker Compose Updates

Add to `docker-compose.yml`:

```yaml
  ai-research-service:
    build:
      context: .
      dockerfile: Dockerfile.service
      args:
        - SERVICE=ai
    ports:
      - "8086:8086"
    environment:
      - LLM_API_KEY=${LLM_API_KEY}
      - DB_HOST=postgres
      - DB_PORT=5432
      - DB_USER=${DB_USER}
      - DB_PASSWORD=${DB_PASSWORD}
      - DB_NAME=${DB_NAME}
      - REDIS_HOST=redis
      - REDIS_PORT=6379
    volumes:
      - ./config/ai-service.yaml:/app/config/ai-service.yaml:ro
    depends_on:
      - postgres
      - redis
      - analysis-service
    networks:
      - quant-network
```

---

## Configuration Updates

### Environment Variables

Add to `.env`:

```bash
# AI Research Service
LLM_API_KEY=sk-...
LLM_PROVIDER=openai
LLM_MODEL=gpt-4-turbo-preview

# Gene Pool
GENE_POOL_MAX_FACTORS=1000
GENE_POOL_MAX_STRATEGIES=500
GENE_POOL_MIN_IC=0.03

# Evolution
EVOLUTION_POPULATION_SIZE=50
EVOLUTION_MAX_GENERATIONS=100
EVOLUTION_ELITE_COUNT=5
```

### Frontend Configuration

Update `web/.env`:

```bash
VITE_API_BASE_URL=http://localhost:8085
VITE_AI_SERVICE_URL=http://localhost:8086
```

---

## API Changes

### New Endpoints (AI Research Service)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/ai/health` | GET | Health check |
| `/api/ai/factors/discover` | POST | Discover factors from topic |
| `/api/ai/factors/validate` | POST | Validate factor (L1-L4) |
| `/api/ai/strategies/generate` | POST | Generate strategy from description |
| `/api/ai/strategies/validate` | POST | Validate strategy code |
| `/api/ai/evolution/run` | POST | Run evolution loop |
| `/api/ai/evolution/status/:id` | GET | Check evolution status |
| `/api/ai/pipeline/run` | POST | Run full pipeline |

### Request/Response Examples

#### Factor Discovery

```bash
curl -X POST http://localhost:8086/api/ai/factors/discover \
  -H "Content-Type: application/json" \
  -d '{"topic": "momentum factors for A-share market"}'
```

Response:
```json
{
  "factors": [
    {
      "id": "fact_001",
      "name": "20-day price momentum",
      "formula": "ts_mean(close, 20) / close - 1",
      "category": "momentum",
      "confidence": 0.85
    }
  ]
}
```

#### Strategy Generation

```bash
curl -X POST http://localhost:8086/api/ai/strategies/generate \
  -H "Content-Type: application/json" \
  -d '{"description": "低估值高ROE的价值股策略", "universe": "CSI300"}'
```

Response:
```json
{
  "strategy": {
    "id": "strat_001",
    "name": "ValueQualityStrategy",
    "code": "package strategy\n...",
    "params": [
      {"name": "pe_threshold", "type": "float", "default": 20}
    ]
  }
}
```

---

## Frontend Changes

### New Routes

| Route | Component | Description |
|-------|-----------|-------------|
| `/ai-research` | `AIResearch.vue` | Main AI research page |
| `/ai-research/factor-lab` | `FactorLab.vue` | Factor discovery & visualization |
| `/ai-research/strategy-workshop` | `StrategyWorkshop.vue` | Strategy generation & editing |
| `/ai-research/evolution` | `EvolutionObs.vue` | Evolution monitoring |
| `/ai-research/pipeline` | `PipelineDashboard.vue` | Pipeline visualization |

### New Components

All components located in `web/src/components/ai/`:

- `FactorLab.vue` — Factor discovery interface
- `FactorCard.vue` — Individual factor display
- `StrategyWorkshop.vue` — Strategy generation workshop
- `StrategyCard.vue` — Strategy card display
- `EvolutionObs.vue` — Evolution observatory
- `GenealogyTree.vue` — Strategy genealogy visualization
- `FitnessChart.vue` — Fitness evolution chart
- `PipelineDashboard.vue` — End-to-end pipeline dashboard
- `BacktestResultCard.vue` — Backtest result display

### Dependencies

No new dependencies required. Uses existing:
- Vue 3 + TypeScript
- Naive UI
- Chart.js
- Pinia

---

## Testing Requirements

### Unit Tests

```bash
# AI package
go test ./pkg/ai/... -v -cover

# Live trading
go test ./pkg/live/... -v -cover

# Expression engine
go test ./pkg/ai/expression/... -v -cover

# Evolution
go test ./pkg/ai/evolution/... -v -cover
```

### Integration Tests

```bash
# AI service integration
go test ./pkg/ai/client/... -v

# Live engine integration
go test ./pkg/live/... -run Integration -v
```

### E2E Tests

```bash
cd e2e && npx playwright test --grep "AI"
```

### Coverage Targets

| Package | Target | Phase 4 Status |
|---------|--------|----------------|
| `pkg/ai` | ≥ 60% | ✅ ≥75% |
| `pkg/live` | ≥ 60% | ✅ Interfaces defined |
| `pkg/ai/expression` | ≥ 80% | ✅ |
| `pkg/ai/evolution` | ≥ 70% | ✅ |

---

## Rollback Procedure

If migration fails:

1. **Stop AI Research Service**:
   ```bash
   docker compose stop ai-research-service
   ```

2. **Revert Database**:
   ```sql
   DROP TABLE IF EXISTS ai_research_jobs;
   DROP TABLE IF EXISTS strategy_genes;
   DROP TABLE IF EXISTS factor_genes;
   ```

3. **Remove Configuration**:
   ```bash
   rm config/ai-service.yaml
   ```

4. **Revert Docker Compose**:
   ```bash
   git checkout docker-compose.yml
   docker compose up -d
   ```

5. **Verify**:
   ```bash
   curl http://localhost:8085/api/health
   ```

---

## Troubleshooting

### Common Issues

#### AI Service Won't Start

**Symptom**: `cmd/ai/main.go` fails with "LLM client not configured"

**Solution**:
```bash
# Check API key
export LLM_API_KEY=sk-...

# Verify config
cat config/ai-service.yaml | grep api_key
```

#### Database Connection Failed

**Symptom**: AI service logs show "connection refused"

**Solution**:
```bash
# Check PostgreSQL
docker compose ps postgres

# Verify migrations
psql -U $DB_USER -d $DB_NAME -c "SELECT COUNT(*) FROM factor_genes;"
```

#### Frontend Can't Reach AI Service

**Symptom**: "Network Error" in FactorLab

**Solution**:
```bash
# Check Vite proxy
 cat web/vite.config.ts | grep 8086

# Verify CORS
curl -I http://localhost:8086/api/ai/health
```

#### Gene Pool Performance Issues

**Symptom**: Slow factor queries

**Solution**:
```sql
-- Add indexes if missing
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_factor_genes_category ON factor_genes(category);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_strategy_genes_generation ON strategy_genes(generation);
```

---

## Post-Migration Checklist

- [ ] AI Research Service responds on :8086
- [ ] `/api/ai/health` returns 200
- [ ] Factor discovery API works
- [ ] Strategy generation API works
- [ ] Frontend AI routes accessible
- [ ] All existing backtest functionality preserved
- [ ] Database migrations applied successfully
- [ ] `go test ./...` passes
- [ ] `npm run build` succeeds
- [ ] E2E tests pass

---

## Support

For issues not covered in this guide:

1. Check [VISION.md](../VISION.md) for design context
2. Review [ADR-015](../adr/adr-015-ai-agent-architecture.md) for architecture decisions
3. Consult [tasks-phase-2.md](../tasks-phase-2.md) for implementation status
4. Run diagnostics: `go vet ./... && go test ./...`

---

_Last updated: 2026-05-06_
_Migration version: Phase 3 → Phase 4_
