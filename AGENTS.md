# Quant Lab — Agentic Coding Configuration

> **版本**: v3.0 (基于 AGENTS Template v2.0 迁移)
> **最后更新**: 2026-06-10 (ODR-012 P1 20 项完成, 详见 [ODR-012](docs/odr/odr-012-comprehensive-code-review.md))
> **适用项目**: A-share quantitative trading system (Quant Lab)
>
> 本文件为 AI 编码助手提供项目上下文。阅读本文件即可快速理解项目全貌。

---

## 1. 项目概述

**Quant Lab** 是一个专业的 A 股量化交易平台，采用 Go 后端 + Vue 3 前端 + PostgreSQL + Redis 架构。

- **语言**: Go 1.21+ (后端), TypeScript + Vue 3 (前端)
- **当前版本**: Phase 3 (Integration & Scale) → Phase 4 (AI-Native Evolution) 进行中
- **状态**: 核心功能已完成，AI 研究服务已上线运行
- **入口**: `cmd/analysis/main.go` (后端), `cmd/ai/main.go` (AI 服务), `web/src/main.ts` (前端)
- **构建**: `go build ./...` (后端), `npm run build` (前端)

### 技术栈

| 组件 | 技术 | 版本/端口 |
|------|------|----------|
| 后端 API | Go + Gin | :8085 |
| 前端 SPA | Vue 3 + Naive UI | :5173 (dev) |
| 数据服务 | Go + Gin | :8081 |
| **AI 研究服务** | **Go + Gin** | **:8086** |
| 策略服务 | Go + Gin | :8082 (备用 per ADR-012) |
| **风控 + 执行** | **in-process** | **合并到 analysis (per ODR-021, P1-15)** |
| 数据库 | PostgreSQL | :5432 |
| 缓存 | Redis | :6379 |

> **CR-36 (ODR-012)**: previously only analysis/data/strategy/AI were listed.
> risk-service(8083) and execution-service(8084) were added in CR-36.
>
> **ODR-021 (P1-15, 2026-06-12)**: risk-service + execution-service have
> been **merged into analysis-service** as in-process components
> (`risk.RiskManager` + `live.MockTrader`). Docker compose service
> count reduced 7 → 5. Risk endpoints exposed at `/api/risk/*` and
> execution endpoints at `/api/execution/*` (both with legacy aliases).
> See [ODR-021](docs/odr/odr-021-p1-15-service-merge-risk-execution.md).

---

## 2. 架构概览

采用微服务架构，Analysis Service 作为 API 网关协调 Data Service 和 Strategy Service。

```
Browser (Vue SPA :5173)
  │
  ├──► GET /api/health        ──► analysis-service :8085
  ├──► POST /api/backtest     ──► analysis-service → engine.go → postgres
  ├──► GET /ohlcv/:symbol     ──► analysis-service → data-service :8081 (proxy)
  ├──► GET /api/strategies    ──► analysis-service → StrategyDB (local)
  ├──► POST /api/walkforward  ──► analysis-service → WalkForwardEngine → postgres
  ├──► POST /api/batch/backtest ──► analysis-service → BatchEngine → postgres
  ├──► GET /api/sync/stream   ──► analysis-service → SSE (real-time progress)
  ├──► GET /api/sync/status   ──► analysis-service → SyncJobManager → postgres
  ├──► POST /api/sync/jobs    ──► analysis-service → SyncQueue → WorkerPool
  ├──► GET /api/datasource/status ──► analysis-service → DataAdapter
  ├──► POST /api/datasource/switch ──► analysis-service → ProviderManager
  ├──► GET /api/plugins       ──► analysis-service → PluginLoader
  │
  └──► Redis (:6379) ◄──── factor_cache, session store, sync_job_cache
              │
              └──► PostgreSQL (:5432) ◄── stocks, ohlcv, fundamentals,
                                          backtest_jobs, sync_jobs, sync_schedules

  data-service :8081 (tushare data ingestion)
  strategy-service :8082 (standby — see ADR-012)
  ai-research-service :8086 (LLM-driven strategy generation)
```

### 关键架构决策

| ADR | 决策 | 核心理由 |
|-----|------|----------|
| [ADR-001](docs/adr/adr-001-plugin-loading.md) | 策略插件动态加载 | Hot-swap，无需重启 |
| [ADR-002](docs/adr/adr-002-timescaledb.md) | TimescaleDB 时序优化 | OHLCV 查询性能 |
| [ADR-003](docs/adr/adr-003-background-worker.md) | 异步任务队列 | 回测不阻塞 API |
| [ADR-004](docs/adr/adr-004-scoring-method.md) | 多因子评分框架 | 策略通用化 |
| [ADR-005](docs/adr/adr-005-strategy-config.md) | 策略配置标准化 | 统一参数接口 |
| [ADR-014](docs/adr/adr-014-strategy-framework-refactor.md) | 策略框架重构 | 消除重复代码，统一接口 |
| [ADR-015](docs/adr/adr-015-ai-agent-architecture.md) | AI Agent 量化研究架构 | AI 作为资深量化研究员 |

详见: [ADR.md](docs/ADR.md)

---

## 3. 目录结构

```
quant-trading/
├── cmd/                    # 服务入口
│   ├── analysis/           # 主服务 (:8085)
│   └── ai/                 # AI 研究服务 (:8086) — Phase 4
├── pkg/                    # 业务逻辑包
│   ├── ai/                 # AI Agent 系统 (Phase 4)
│   │   ├── intent/         # LLM 意图解析：自然语言 → 结构化策略参数
│   │   ├── yaml/           # YAML 配置生成器：结构化意图 → 策略配置
│   │   ├── pipeline/       # 策略生成流水线：意图 → YAML → 代码 → 编译 → 回测
│   │   ├── agents/         # Research/Generate/Validate/Evolve Agents
│   │   ├── expression/     # 因子表达式 DSL + AST
│   │   ├── gene_pool/      # 因子/策略基因池
│   │   ├── client/         # 回测/因子 HTTP 客户端
│   │   ├── metrics/        # IC、换手率计算
│   │   ├── search/         # TPE + 遗传算法
│   │   ├── evolution/      # 种群管理 + 选择/交叉/变异
│   │   ├── drift/          # 概念漂移检测
│   │   ├── validator/      # 代码验证器
│   │   └── prompts/        # LLM 提示模板
│   ├── backtest/           # 回测引擎 (engine, job, batch, walkforward)
│   ├── data/               # 数据管道 (sync, marketdata, factors)
│   ├── domain/             # 领域模型 (OHLCV, Signal, Portfolio)
│   ├── storage/            # PostgreSQL 存储
│   ├── strategy/           # 策略框架 + 插件
│   ├── risk/               # 风控模块 (stoploss, position sizing)
│   └── live/               # 实盘/纸交易引擎 (Phase 4)
├── web/src/                # Vue 3 前端
│   ├── api/                # API 客户端
│   ├── pages/              # 页面组件
│   │   └── AIResearch.vue  # AI 研究主页面 — Phase 4
│   ├── components/         # 可复用组件
│   │   └── ai/             # AI 模块组件 — Phase 4
│   │       ├── PipelineDashboard.vue  # AI 策略生成流水线仪表盘
│   │       ├── FactorLab.vue
│   │       ├── StrategyWorkshop.vue
│   │       ├── EvolutionObs.vue
│   │       ├── FactorCard.vue
│   │       ├── StrategyCard.vue
│   │       ├── GenealogyTree.vue
│   │       └── FitnessChart.vue
│   └── utils/              # 工具函数
├── docs/                   # 文档 (见下方导航)
├── e2e/tests/              # Playwright E2E 测试
└── migrations/             # 数据库迁移 SQL
```

---

## 4. 角色与边界

### 你是谁
你是一个全栈开发 Agent，协助进行后端服务、前端页面、API 集成、测试和文档工作。你编写生产级代码，遵循现有模式和约定。

### 工作范围（What You Work On）
- **后端**: `cmd/`, `pkg/` (Go modules — backtest, data, domain, storage, strategy, **ai**, **live**)
- **前端**: `web/src/` (Vue 3 + TypeScript + Naive UI + Chart.js + Pinia)
- **测试**: `e2e/tests/` (Playwright E2E tests)
- **文档**: `docs/` (Markdown design documents)
- **ODR**: `docs/odr/` (Operational Decision Records — process/governance decisions)

### 禁区（What You NEVER Modify）
- `node_modules/`, `dist/`, `.vite/` — 自动生成，gitignored
- `.env*` 文件 — 包含密钥
- `migrations/` SQL 文件 — 只新增，不修改已有
- `docker-compose.yml` 基础配置 — 通过 `docker-compose.override.yml` 覆盖
- 二进制文件、编译产物或 vendor 目录
- `docs/archive/` — 归档文档不可变；只新增，不修改

---

## 5. 命令参考

### Backend (Go)
```bash
go build ./...                          # Build all packages
go vet ./...                            # Static analysis
go test ./pkg/backtest/... -v           # Run backtest engine tests
go test ./pkg/data/... -v               # Run data pipeline tests
go test ./pkg/storage/... -v            # Run PostgreSQL storage tests
```

### Frontend (Vue 3)
```bash
cd web && npm install                   # Install dependencies
npm run dev                             # Start Vite dev server (:5173)
npm run build                           # Production build to dist/
npm run lint                            # ESLint + Prettier check
npm run typecheck                       # TypeScript strict mode check
npm run lint:tests                      # Static scan for vitest toBe(expected, "message") misuse (F2-new)
npm test                                # Run Vitest unit tests (calls lint:tests first)
npm run test:watch                      # Vitest watch mode
npm run test:coverage                   # Vitest with coverage report
```

### E2E Tests (Playwright)
```bash
cd e2e && npx playwright test            # Full E2E suite (all projects)
npx playwright test --project=chrome    # Chrome browser only
npx playwright test --grep "Dashboard" # Run specific tests by name
npx playwright test --report=html      # Generate HTML report
```

### Infrastructure
```bash
docker compose up -d                    # Start all 7 services
docker compose ps                       # Check service health
docker compose logs -f analysis-service  # Tail analysis service logs
docker compose down                     # Stop all services
```

---

## 6. 代码规范

### Backend (Go)

遵循 [Effective Go](https://go.dev/doc/effective_go) 规范。

- 使用 `context.Context` 作为 I/O 函数的第一个参数
- 错误处理：返回 error，生产代码中绝不 panic
- 命名：导出符号用 PascalCase，未导出用 camelCase
- 包结构：`domain/` → `data/` → `strategy/` → `backtest/` → `storage/`
- 使用 `logrus` 或标准 `log` 包记录日志（与现有代码一致）

**Canonical Strategy Interface**（单一事实来源）：
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]domain.Signal, error)
    Weight(signal domain.Signal, portfolioValue float64) float64
    Cleanup()
}
```
参见: `pkg/strategy/strategy.go`, [SPEC.md](docs/SPEC.md), [VISION.md](docs/VISION.md)

### Frontend (Vue 3 + TypeScript)

- **Composition API** 与 `<script setup lang="ts">` 专用
- **Naive UI** 组件库（通过 NConfigProvider 使用暗色主题）
- **Pinia** 状态管理 — 大对象使用 `shallowRef`（BacktestResult），简单状态使用 `ref`
- **Chart.js 4** 数据可视化（权益曲线、交易标记）
- **Vue Router 4** SPA 导航
- 组件命名：PascalCase（如 `BacktestForm.vue`, `EquityChart.vue`, `MetricsCards.vue`）
- Composables 命名：`use*.ts`（如 `useBacktest.ts`）
- 工具函数集中在 `web/src/utils/format.ts` — 不要在组件中重复 `fmtPercent()`

**Key Frontend Patterns**:
```typescript
// API Client — 所有调用通过 web/src/api/client.ts
// Base URL: http://localhost:8085 (Vite proxy in dev mode)
import { getBacktestReport } from '@/api/backtest'
import { runBacktest as apiRunBacktest } from '@/api/backtest'

// State management — 大对象使用 shallowRef
const result = shallowRef<BacktestResult | null>(null)
triggerRef(result) // 手动触发响应式更新

// Icon components — 用 markRaw() 包装防止 Vue 响应式代理
import { markRaw } from 'vue'
const metrics = ref([{ icon: markRaw(ServerOutline), ... }])

// Chart.js 渲染 — 在状态更新后始终 await nextTick() 再访问 canvas
async function renderChart() {
  chartData.value = data
  await nextTick()
  if (!eqCanvasRef.value) return
  const ctx = eqCanvasRef.value.getContext('2d')
  // ... create chart
}
```

---

## 7. 数据流

```
Browser (Vue SPA :5173)
  │
  ├──► GET /health            ──► analysis-service :8085
  ├──► POST /backtest         ──► analysis-service → engine.go → postgres
  ├──► GET /ohlcv/:symbol     ──► analysis-service → data-service :8081 (proxy)
  ├──► GET /strategies        ──► analysis-service → StrategyDB (local)
  ├──► POST /batch/backtest   ──► analysis-service → BatchEngine → postgres
  ├──► POST /walkforward      ──► analysis-service → WalkForwardEngine → postgres
  ├──► GET /sync/stream       ──► analysis-service → SSE (real-time)
  ├──► GET /datasource/status ──► analysis-service → DataAdapter
  ├──► GET /plugins           ──► analysis-service → PluginLoader
  ├──► POST /api/risk/*       ──► analysis-service → in-process risk.RiskManager (P1-15)
  ├──► POST /api/execution/*  ──► analysis-service → in-process live.MockTrader (P1-15)
  │
  └──► Redis (:6379) ◄──── factor_cache, session store, sync_job_cache
              │
              └──► PostgreSQL (:5432) ◄── stocks, ohlcv, fundamentals,
                                          backtest_jobs, sync_jobs, sync_schedules
```

---

## 8. 工作流规范

### 工作类型分类

| 类型 | 判定关键词 | 示例 |
|------|-----------|------|
| 设计 | 设计/架构/方案/选型/ADR | 新策略模块设计 |
| 审计 | 审计/审查/Review/检查 | 代码质量审计 |
| 测试 | 测试/Test/覆盖 | 编写单元测试 |
| 实现 | 实现/开发/编写/修复/重构 | 修复回测 Bug |
| 文档 | 文档/README/API文档/指南 | 更新 SPEC.md |
| 其他 | 配置/问答/调研 | 环境配置调整 |

### 前置动作（开始前必做）

| 任务类型 | 必做事项 |
|---------|---------|
| 设计 | 研究现有架构（[ARCHITECTURE.md](docs/ARCHITECTURE.md)）、查阅 ADR、横向比较方案 |
| 审计 | 通读相关设计文档、审查变更历史、确认技术规范 |
| 测试 | 理解需求（[TEST.md](docs/TEST.md)）、分析源码识别边界条件 |
| 实现 | 理解设计文档（[SPEC.md](docs/SPEC.md)）、熟悉编码规范、检查依赖兼容性 |
| 文档 | 对照源码验证一致性、检查格式规范（ADR/ODR 模板） |
| 其他 | 无特定要求 |

### 后置动作（完成后必做）

| 任务类型 | 必做事项 |
|---------|---------|
| 设计 | 更新现有文档（优先）、编写 ADR、记录待办到 TASKS.md |
| 审计 | 输出发现（ODR 格式）、更新任务列表、修复 Critical/High 问题 |
| 测试 | 确保测试通过、审计测试质量（消除无效/低效测试）、更新覆盖率 |
| 实现 | 运行 lint + typecheck、编写/更新测试、确保构建通过、更新相关文档 |
| 文档 | 验证代码一致性、检查交叉引用、更新导航表（AGENTS.md Document Index） |
| 其他 | 无特定要求 |

### 执行规范（Implementation Discipline）

> **来源**: 2026-06-29 ODR-043 综合审计后确立 — 适用于 `docs/TASKS.md` 中所有 S7-* 系列任务及后续新增任务

每个任务在执行过程中**必须**遵循以下三条规范，缺一不可：

#### 规范 1: 测试先行（Test-First）

- **每个任务必须编写对应的测试用例**，包括但不限于：
  - Bug 修复：先写一个能复现 bug 的失败测试，再修复代码使测试通过
  - 新功能：先写测试定义预期行为，再实现功能
  - 重构：确保现有测试覆盖重构范围，重构后测试全部通过
- **测试质量要求**：
  - 必须有行为断言（禁止 `assert.True(t, true)` placeholder）
  - 必须覆盖边界条件（空输入、非法输入、并发场景）
  - 禁止用 `time.Sleep` 同步并发测试，改用 channel/`require.Eventually`/`sync.WaitGroup`
  - 测试名必须描述被测行为（如 `TestT1SellBlockedByQuantityYesterday`）
- **验证标准**: `go test ./... -count=1 -race` 必须通过；前端 `npm test` 必须通过

#### 规范 2: 代码审查（Code Review）

- **每个任务完成后必须进行代码审查**，审查维度：
  - **正确性**: 逻辑是否正确？边界条件是否处理？
  - **可维护性**: 命名是否清晰？函数是否过长（>100 行警示）？参数是否过多（>5 个警示）？
  - **规范遵循**: 是否符合 AGENTS.md §6 代码规范？gofmt 是否通过？
  - **测试质量**: 测试是否有效？是否覆盖关键路径？
  - **文档同步**: 是否需要更新 SPEC.md/ARCHITECTURE.md/AGENTS.md？
- **审查方式**:
  - 自审：提交前自己通读 diff
  - 工具审：`go vet ./...` + `gofmt -l .` + `npm run typecheck`（若可用）
  - 必要时使用 TRAE-code-review skill 执行结构化审查
- **审查记录**: 审查发现的问题要么当场修复，要么记录为新任务到 `docs/TASKS.md`

#### 规范 3: 原子提交（Atomic Commit）

- **每个任务采用 atomic commit 方式提交代码变更**，含义：
  - **一个任务 = 一个 commit**（或一组逻辑紧密关联的 commit）
  - **每个 commit 必须是可独立构建、可独立测试通过的完整变更**
  - 禁止"提交一半工作"（如修改了 A 但没改依赖的 B，导致编译失败）
  - 禁止"混合多个任务到一个 commit"（如把 bug 修复和重构混在一起）
- **Commit message 格式**:
  ```
  <type>(<scope>): <subject>

  <body — 说明 what/why，不要说明 how>

  Refs: S7-P0-1 (任务 ID from docs/TASKS.md)
  Reviewed: self-review + go vet + gofmt + tests pass
  ```
  - `type`: feat / fix / refactor / test / docs / chore
  - `scope`: 受影响的包或模块（如 `ai/pipeline` / `backtest` / `web`）
  - `Refs`: 任务 ID，便于追溯
- **提交前检查清单**:
  - [ ] `go build ./...` 通过
  - [ ] `go vet ./...` 无警告
  - [ ] `gofmt -l .` 无输出（已格式化）
  - [ ] `go test ./... -count=1 -race` 通过（或受影响包的测试通过）
  - [ ] 前端变更：`npm test` + `npm run typecheck`（若可用）通过
  - [ ] commit message 符合格式
- **分支策略**: 不直接提交到 `main`，使用 feature branch（如 `fix/s7-p0-1-pipeline-nil-runner`）

#### 规范适用范围

| 任务类型 | 测试先行 | 代码审查 | 原子提交 |
|---------|---------|---------|---------|
| Bug 修复 (P0) | ✅ 必须复现测试 | ✅ | ✅ |
| 重构 (P1/P2) | ✅ 现有测试不退化 | ✅ | ✅ |
| 新功能 (P3) | ✅ TDD 优先 | ✅ | ✅ |
| 文档同步 | N/A | ✅ 一致性审查 | ✅ 独立 commit |
| 测试补强 | ✅ 被测代码已有 | ✅ 测试质量审查 | ✅ |

#### 违规处理

- 未编写测试的任务：**不算完成**，标记为 blocked
- 未通过代码审查的提交：**不得合并**，返回修改
- 非 atomic commit：**要求拆分**为多个原子 commit 后再合并

---

## 9. 行为边界

### Always Do（必须做）
- ✅ 在提交前端变更前运行 `npm run lint && npm run typecheck`
- ✅ 在提交后端变更前运行 `go vet ./... && go test ./...`
- ✅ UI 变更后运行 `npx playwright test`
- ✅ 更改接口或架构时更新相关文档
- ✅ 参考 [VISION.md](docs/VISION.md) 进行架构决策时遵循设计原则
- ✅ 大对象使用 `shallowRef`（BacktestResult, portfolio_values 数组）
- ✅ Icon 组件使用 `markRaw()` 包装防止 Vue 响应式警告
- ✅ 状态更改后访问 DOM refs 前 `await nextTick()`
- ✅ **遵循 Document Self-Maintenance Protocol**（见第 10 章）— 保持文档实时更新

### Ask First（先问再做）
- ❓ 修改数据库 schema（必须在 `migrations/` 中添加新迁移文件）
- ❓ 添加新的 npm 或 Go 依赖
- ❓ 更改核心接口（`Strategy`, `Domain` types in `pkg/domain/`）
- ❓ 移除或重命名现有 API 端点
- ❓ 修改 `docker-compose.yml` 基础配置
- ❓ 不确定的设计决策时，检查 `docs/adr/` 是否有先前 ADR

### Never Do（绝对不做）
- ❌ **绝不在代码中硬编码 API 密钥、密码或凭据**
- ❌ **绝不直接提交到 `main` 分支** — 使用 PR 工作流
- ❌ **绝不修改 `node_modules/`, `dist/`, `.env*` 或自动生成的文件**
- ❌ **绝不使用 `any` 类型且无显式注释说明**
- ❌ **绝不生成无法解释的代码**
- ❌ **绝不使用 `ref()` 处理大对象（>50 属性或嵌套数组）— 使用 `shallowRef`**
- ❌ **绝不状态更改后立即访问 DOM refs（canvas, input）— await `nextTick()`**
- ❌ **绝不跨组件复制工具函数如 `fmtPercent()`**
- ❌ **绝不编写独立的 Report (.md) 文件 — 使用 ODR 替代**

---

## 10. 文档维护

### 文档分类

| 类型 | 目录 | 职责 | 示例 |
|------|------|------|------|
| 设计文档 | `docs/` | 解释系统设计原理和架构 | VISION.md, SPEC.md |
| 决策文档 | `docs/adr/` | 记录架构决策的上下文和影响 | adr-001 ~ adr-015 |
| 运营决策 | `docs/odr/` | 记录运营/流程/治理决策 | odr-001 ~ odr-008 |
| 任务文档 | `docs/TASKS.md` | 统一追踪可执行任务 | — |
| 参考文档 | `docs/` | 持续维护的状态/进度文档 | ROADMAP.md, NEXT_STEPS.md |
| 指南文档 | `docs/guides/` | 迁移、部署等操作指南 | migration-phase3-to-phase4.md |
| 归档文档 | `docs/archive/` | 过时但保留作历史参考 | reports-2026-Q2/ |

### 文档生命周期
```
Active → Stale → Archived → (optional) Purged
```
- **Active**: 当前准确，被引用在 AGENTS.md Document Index
- **Stale**: 含过时信息但有历史价值 → 在顶部添加 ⚠️ stale notice
- **Archived**: 移至 `docs/archive/` 并带季度标签（如 `reports-2026-Q2/`）
- **Purged**: 从归档中删除（仅限 12+ 个月后，需维护者批准）

**Staleness Detection** — 每次新会话开始时快速检查：
- AGENTS.md Known Issues 表是否仍反映现实？
- Data Flow Architecture 中的端口/服务是否正确？
- ODR index 中的条目是否已过时？

如果检测到过时内容，**主动修复** — 不要等用户提问。

### Rule 1: Update-on-Change Triggers

| 代码变更 | 文档 to Update | 更新内容 |
|---------|--------------|---------|
| 新增/修改 Go API 端点 | `docs/SPEC.md` | API section: endpoint path, method, request/response |
| 新增/修改数据库表 | `docs/ARCHITECTURE.md` | DB schema section: table name, columns, indexes |
| 新增/修改服务端口 | `docs/ARCHITECTURE.md` + AGENTS.md Data Flow | Service topology diagram |
| 更改 Strategy 接口 | `docs/SPEC.md` + `docs/VISION.md` + AGENTS.md Canonical section | Interface signature (all 3 must match) |
| 新增 Vue 页面/路由 | `docs/ARCHITECTURE.md` (frontend section) + `docs/design/pages/` | Page structure tree + Design PRD |
| 新增 npm/Go 依赖 | AGENTS.md Commands section (if adds new commands) | Build/run commands |
| 修复已知问题 | AGENTS.md Known Issues table | Remove the fixed entry |
| 发现新问题 | AGENTS.md Known Issues table | Add new entry with workaround |
| 变更 docker-compose services | `docs/ARCHITECTURE.md` + AGENTS.md Data Flow | Service list and ports |

### Rule 2: ODR Creation Triggers

| 操作 | ODR 类别 | 创建时机 |
|------|---------|---------|
| 删除/归档任何文档 | Cleanup | 操作后立即 |
| 审计/审查工作 | Audit | 完成后 48h 内 |
| 文档架构迁移 | Migration | 完成后 72h 内 |
| 变更开发工具或流程 | Tooling/Process | 上线前 |
| 归档文档到 `docs/archive/` | Cleanup | 与归档操作同会话 |

**ODR Template**（所有新 ODR 必须使用此格式）：
```markdown
# ODR-[next-number]: [Short Title]

> **Status**: [Proposed/Accepted/Completed/Deprecated]
> **Date**: [YYYY-MM-DD]
> **Category**: [Cleanup/Audit/Migration/Tooling/Process]
> **Related ADRs**: [adr-xxx] (if any)
> **Supersedes**: [odr-yyy] (if replacing an earlier ODR)

## Context
[Why was this needed? What problem triggered it?]

## Decision
[What was decided? Why this approach over alternatives?]

## Consequences
[Positive and negative impacts of this decision]

## Artifacts
[Files created/modified/deleted in this operation]

## Metrics
[Quantifiable results: doc count change, line reduction, etc.]

## Lessons Learned
[What would you do differently next time?]
```

创建 ODR 后，**必须同时更新** `docs/ADR.md` — 将新 ODR 添加到 "ODR Index" 表。

### Rule 3-6: 其他文档规则

**Rule 3: No Report Files — Use ODR Instead**

**CRITICAL**: 绝不创建新的 `*_REPORT.md` 文件。

Reports 是快照，会迅速过时。替代方案：
1. **决策类** → 在 `docs/odr/` 创建 ODR
2. **审计发现** → 创建 ODR + 直接更新受影响的文档
3. **迁移记录** → 创建 ODR + 如需要则更新 AGENTS.md

唯一例外：`FINAL_VERIFICATION_REPORT.md` 保留作为质量门禁 artifact。

**Rule 4: AGENTS.md Self-Update**

AGENTS.md 是活文档。以下情况主动更新：

| Trigger | What to Update in AGENTS.md |
|---------|----------------------------|
| 发现新构建/测试命令 | Commands section |
| 发现新已知问题 | Known Issues table |
| 已知问题解决 | Remove from Known Issues table |
| 服务端口变更 | Data Flow Architecture diagram |
| 新文档添加到 docs/ | Document Index table |
| 文档归档到 docs/archive/ | 从 Document Index 移除，在 archive README 注记 |
| 代码模式经验教训 | Key Frontend Patterns 或 Code Style sections |
| 新 "Never Do" 规则 | Boundaries → Never Do section |

**Rule 5: Session-End Document Check**

每次编码会话结束前（特别是建议 commit 前），运行此清单：

- [ ] 是否修改了接口？→ 更新 SPEC.md / VISION.md / AGENTS.md
- [ ] 是否添加/删除了文件？→ 如是结构性变更，更新 ARCHITECTURE.md 目录树
- [ ] 是否修复了已知问题？→ 从 AGENTS.md Known Issues 移除
- [ ] 是否发现了新问题？→ 添加到 AGENTS.md Known Issues
- [ ] 是否归档/删除了文档？→ 创建 ODR + 更新 ADR.md index
- [ ] 是否改变了项目约定？→ 更新 AGENTS.md Code Style / Boundaries
- [ ] 是否添加了新依赖？→ 检查 AGENTS.md Commands 是否需要更新

---

## 11. 文档导航

> AGENTS.md 是你的快速参考；下方的 docs 提供深层上下文（"why"）。

### Explanation (理解设计)

| 文档 | Purpose | When to Read |
|------|---------|-------------|
| [VISION.md](docs/VISION.md) | 设计原则（Accuracy First, Hot-Swap 等）、领域模型 | 开始新功能、质疑方法时 |
| [SPEC.md](docs/SPEC.md) | 技术规格、API 定义、数据模型、Strategy 接口 | 实现端点、编写策略时 |
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | 服务拓扑、DB schema（18 张表）、缓存设计 | 理解系统布局、调试时 |

> **CR-47 (ODR-012)**: AGENTS.md previously said "6 张表" while
> ARCHITECTURE.md:305 says 18 tables (14 in `pkg/storage/postgres.go`
> + 18 in `migrations/`; migrations win after 012_*). Count verified
> by grepping `CREATE TABLE` across both files: migrations=18,
> postgres.go=14. The 18-table figure is canonical.

### Reference (查找状态)

| 文档 | Content | Updated When |
|------|---------|-------------|
| [ROADMAP.md](docs/ROADMAP.md) | Sprint 进度、Phase 里程碑、状态跟踪 | 规划工作时、检查进度时 |
| [NEXT_STEPS.md](docs/NEXT_STEPS.md) | 审查发现、TODO list、行动项 | 代码审查后、规划下一步时 |
| [TEST.md](docs/TEST.md) | 测试策略、覆盖率目标、T+1/涨跌停规格 | 编写测试、验证正确性时 |

### How-to (执行任务)

| 文档 | 用途 |
|------|------|
| [E2E_TEST_GUIDE.md](docs/E2E_TEST_GUIDE.md) | E2E 测试指南 |
| [TASKS.md](docs/TASKS.md) | 统一任务追踪（含 Phase 3 实施任务） |
| [guides/migration-phase3-to-phase4.md](docs/guides/migration-phase3-to-phase4.md) | Phase 3 → Phase 4 迁移指南 |
| [benchmark-results.md](docs/benchmark-results.md) | 性能基准测试结果 |

### Decisions (架构决策)

| 文档 | 内容 |
|------|------|
| [ADR.md](docs/ADR.md) + `docs/adr/` + `docs/odr/` | 架构 (ADR) + 运营 (ODR) 决策记录 | 质疑过往决策、理解理由时 |

**Archived** (在 `docs/archive/`): CLEANUP_REPORT.md, DOC_AUDIT_REPORT.md, MIGRATION_REPORT.md — 见 `docs/archive/README.md`

---

## 12. 会话管理

对于多步骤任务，在 `.session/task-current.md` 中维护实时状态。这有助于跟踪进度并向用户展示工作全貌。

### 启动新任务会话
创建或更新 `.session/task-current.md`：
```markdown
# Active Task: <brief title>

## Objective
<what you're trying to accomplish>

## Progress
- [ ] Step 1: ...
- [ ] Step 2: ...

## Notes
<blockers, discoveries, decisions made>
```

### 重置上下文（Standup 格式）

当对话漂移或上下文丢失时，使用此格式重置：
```
@AGENTS.md

## Standup
Since last session:
- Completed: <list finished items>
- In Progress: <current work>
- Blocked: <anything blocking?>
- Next: <immediate next step>

Context from previous session: <brief summary of key decisions>
Please continue from where we left off.
```

---

## 13. 当前状态

### 项目健康度
- **Phase**: 3 (Integration & Scale) → 4 (AI-Native Evolution) 进行中
- **测试覆盖** (2026-06-29 实测, 总体 62.9% statement-weighted): backtest 76.0% | strategy 64.4% (顶层) / plugins 83.1% (子包) | data/source 60.9% | data/sentiment 76.8% | storage 13.2% (无 DB 时 skip) | ai 28.9% (顶层) / 子包 16-95% (avg ~67%) | live 75.8% | marketdata 79.0% | risk 85.3% | compliance 91.6% | fees 100% | expression 74.5%
- **关键服务**: Analysis ✅ | Data ✅ | **Sync ✅** | **AI Research ✅ (running)** | Strategy ⏸️ (standby per ADR-012, awaiting Phase 3 D3 activation)
- **审计状态** (2026-06-29 ODR-043): 4 维度审计完成, 12 Critical / 18 High / 10 Medium 问题点; 5 真实 bug 已识别待修复

### 任务追踪
> **Phase 3 任务追踪**: [docs/TASKS.md](docs/TASKS.md)
> **Phase 4 (AI-Native) 任务追踪**: [docs/tasks-phase-2.md](docs/tasks-phase-2.md)
>
> Phase 3 任务（P0-P3 + 实施任务）在 TASKS.md 中维护。
> Phase 4 任务（AI Agent + Live Trading）在 tasks-phase-2.md 中维护。

### 技术债
- Phase 3 大部分已完成（见 [NEXT_STEPS.md](docs/NEXT_STEPS.md)）
- Phase 4 新增技术债：AI Agent 测试覆盖、表达式引擎性能、LLM 成本优化
- LiveTrader 已实现接口和 MockTrader，但缺少真实券商对接实现

---

## 14. 已知问题与变通方案

| Issue | Workaround |
|-------|-----------|
| Legacy HTML UI still exists at `cmd/analysis/static/` | Deprecated; Vue SPA is the official frontend. Do not modify legacy HTML. |
| `ChatbubbleEllipsisOutline` icon name doesn't exist | Correct name is `ChatbubbleEllipsesOutline` (with 'e' before 's') |
| Trade markers may not render if portfolio_values is empty | Ensure backtest returns valid data before calling renderChart() |
| **ODR-011 Multi-Source Risks** (CR-48, ODR-012) | See sub-table below |
| **ODR-043-5 前端 lint/typecheck 脚本不存在** | `web/package.json` 无 `lint`/`typecheck` 脚本, 未安装 ESLint; AGENTS.md §5/§9 的命令实际无法执行 — 临时用 `npx vue-tsc --noEmit` 替代 |
| **ODR-043-7 文档漂移: 服务拓扑/归档引用/ADR 状态** | ARCHITECTURE/VISION/SPEC 仍引用 risk-service(8083)/execution-service(8084) (ODR-021 已合并); NEXT_STEPS/IMPLEMENTATION_PLAN 已归档仍被引用; ADR-014 被 ADR-020 §6 取代未标记 — 详见 [ODR-043](docs/odr/odr-043-comprehensive-audit-2026-06-29.md) |

### ODR-011 Multi-Source Integration Risks (CR-48, ODR-012)

| # | Risk | Workaround / Status |
|---|------|---------------------|
| 1 | **mootdx Go SDK 稳定性** — 反编译协议可能随时失效 | `NewMootdxAdapter(nil)` 模式保留 — SDK 未就绪时 service 仍可启动 (`enabled=false` → Fetch 跳过) |
| 2 | **东财反爬应对不完整** — 缺 User-Agent / Referer / 随机延时 | Eastmoney 抓取仅用于低频板块/榜单数据，短线无影响；高频补全见后续 PR |
| 3 | **多源冲突无数值仲裁** — 同 `DataType` 多源返回不同值时无 reconcile 策略 | 当前 "先注册者优先" 仍为 "主备优先级"；数值仲裁 (`pkg/etl/reconcile.go`) 留待 Phase 5 |
| 4 | **实时数据无背压** — 盘中实时通道挤压未处理 | ETL 同步路径 OK；盘中实时需另议背压队列 (`pkg/data/source/mootdx_adapter.go` 后续 PR) |
| 5 | **HealthCheck 慢路径易误入 CI** — `TestInterfaceCompliance` 触发 5s+ 网络调用，CI 必崩 | Health probe 仅暴露在 `/ops/health` 端点；adapter 单元测试用 stub/mock (`pkg/data/source/etl_test.go` 模式) |

---

## 附录：快速启动清单

新会话开始时，按顺序确认：
- [ ] 阅读「1. 项目概述」→ 理解技术栈和服务拓扑
- [ ] 查看「13. 当前状态」→ 了解项目健康度和待办事项
- [ ] 检查「14. 已知问题」→ 避免踩坑
- [ ] 确认「4. 角色与边界」→ 明确工作范围和禁区
- [ ] 如继续旧任务 → 查看 `.session/task-current.md`

### 关键文件速查

| 需要做什么 | 查看哪里 |
|-----------|---------|
| 了解设计原则 | [VISION.md](docs/VISION.md) |
| 查看 API 定义 | [SPEC.md](docs/SPEC.md) |
| 理解系统架构 | [ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| 检查测试策略 | [TEST.md](docs/TEST.md) |
| 查看路线图 | [ROADMAP.md](docs/ROADMAP.md) |
| 了解待办事项 | [NEXT_STEPS.md](docs/NEXT_STEPS.md) |
| 查阅架构决策 | [ADR.md](docs/ADR.md) → `docs/adr/` |
| 查阅运营决策 | [ADR.md](docs/ADR.md) → `docs/odr/` |
| **前端设计系统** | **[docs/design/index.md](docs/design/index.md)** |
| 查看页面设计规范 | [docs/design/pages/](docs/design/pages/) |
| 查看组件使用规范 | [docs/design/components.md](docs/design/components.md) |
| 查看视觉规范 | [docs/design/visual.md](docs/design/visual.md) |
| **AI 研究架构** | **[ADR-015](docs/adr/adr-015-ai-agent-architecture.md)** |
| **Phase 4 实施计划** | **[IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md)** |
| **Phase 4 任务追踪** | **[tasks-phase-2.md](docs/tasks-phase-2.md)** |
| 使用本模板 | [AGENTS_TEMPLATE.md](docs/AGENTS_TEMPLATE.md) |

---
_Last updated: 2026-05-06_
_Source: 基于 AGENTS Template v2.0 迁移，融合 quant-trading + Claudeer 最佳实践_
_Migration ODR: odr-005-agents-md-v3-migration (pending creation)_
_Phase 4 Update: AI-Native Evolution architecture documented in ADR-015, IMPLEMENTATION_PLAN.md, tasks-phase-2.md_
