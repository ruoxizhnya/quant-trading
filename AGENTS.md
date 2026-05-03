# Quant Lab — Agentic Coding Configuration

> **版本**: v3.0 (基于 AGENTS Template v2.0 迁移)
> **最后更新**: 2026-04-11
> **适用项目**: A-share quantitative trading system (Quant Lab)
>
> 本文件为 AI 编码助手提供项目上下文。阅读本文件即可快速理解项目全貌。

---

## 1. 项目概述

**Quant Lab** 是一个专业的 A 股量化交易平台，采用 Go 后端 + Vue 3 前端 + PostgreSQL + Redis 架构。

- **语言**: Go 1.21+ (后端), TypeScript + Vue 3 (前端)
- **当前版本**: Phase 3 (Integration & Scale)
- **状态**: 核心功能已完成，进入质量优化阶段
- **入口**: `cmd/analysis/main.go` (后端), `web/src/main.ts` (前端)
- **构建**: `go build ./...` (后端), `npm run build` (前端)

### 技术栈

| 组件 | 技术 | 版本/端口 |
|------|------|----------|
| 后端 API | Go + Gin | :8085 |
| 前端 SPA | Vue 3 + Naive UI | :5173 (dev) |
| 数据服务 | Go + Gin | :8081 |
| 策略服务 | Go + Gin | :8082 (备用) |
| 数据库 | PostgreSQL | :5432 |
| 缓存 | Redis | :6377 |

---

## 2. 架构概览

采用微服务架构，Analysis Service 作为 API 网关协调 Data Service 和 Strategy Service。

```
Browser (Vue SPA :5173)
  │
  ├──► GET /health          ──► analysis-service :8085
  ├──► POST /backtest       ──► analysis-service → engine.go → postgres
  ├──► GET /ohlcv/:symbol   ──► analysis-service → data-service :8081 (proxy)
  ├──► GET /strategies      ──► analysis-service → StrategyDB (local, no proxy)
  ├──► POST /walkforward    ──► analysis-service → WalkForwardEngine → postgres
  │
  └──► Redis (:6377) ◄──── factor_cache, session store
              │
              └──► PostgreSQL (:5432) ◄── stocks, ohlcv, fundamentals, backtest_runs

  strategy-service :8082 (standby — see ADR-012)
```

### 关键架构决策

| ADR | 决策 | 核心理由 |
|-----|------|----------|
| [ADR-001](docs/adr/adr-001-plugin-loading.md) | 策略插件动态加载 | Hot-swap，无需重启 |
| [ADR-002](docs/adr/adr-002-timescaledb.md) | TimescaleDB 时序优化 | OHLCV 查询性能 |
| [ADR-003](docs/adr/adr-003-background-worker.md) | 异步任务队列 | 回测不阻塞 API |
| [ADR-004](docs/adr/adr-004-scoring-method.md) | 多因子评分框架 | 策略通用化 |
| [ADR-005](docs/adr/adr-005-strategy-config.md) | 策略配置标准化 | 统一参数接口 |

详见: [ADR.md](docs/ADR.md)

---

## 3. 目录结构

```
quant-trading/
├── cmd/                    # 服务入口
│   └── analysis/           # 主服务 (:8085)
├── pkg/                    # 业务逻辑包
│   ├── backtest/           # 回测引擎 (engine, job, batch, walkforward)
│   ├── data/               # 数据管道 (sync, marketdata, factors)
│   ├── domain/             # 领域模型 (OHLCV, Signal, Portfolio)
│   ├── storage/            # PostgreSQL 存储
│   ├── strategy/           # 策略框架 + 插件
│   └── risk/               # 风控模块 (stoploss, position sizing)
├── web/src/                # Vue 3 前端
│   ├── api/                # API 客户端
│   ├── pages/              # 页面组件
│   ├── components/         # 可复用组件
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
- **后端**: `cmd/`, `pkg/` (Go modules — backtest, data, domain, storage, strategy)
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
npm test                                # Run Vitest unit tests
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
    Weight(signal Signal, portfolioValue float64) float64
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
  ├──► GET /health          ──► analysis-service :8085
  ├──► POST /backtest       ──► analysis-service → engine.go → postgres
  ├──► GET /ohlcv/:symbol   ──► analysis-service → data-service :8081 (proxy)
  ├──► GET /strategies      ──► analysis-service → strategy-service :8082 (proxy)
  │
  └──► Redis (:6377) ◄──── factor_cache, session store
              │
              └──► PostgreSQL (:5432) ◄── stocks, ohlcv, fundamentals, backtest_runs
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
| 决策文档 | `docs/adr/` | 记录架构决策的上下文和影响 | adr-001 ~ adr-010 |
| 运营决策 | `docs/odr/` | 记录运营/流程/治理决策 | odr-001 ~ odr-006 |
| 任务文档 | `docs/TASKS.md` | 统一追踪可执行任务 | — |
| 参考文档 | `docs/` | 持续维护的状态/进度文档 | ROADMAP.md, NEXT_STEPS.md |
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
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | 服务拓扑、DB schema（6 张表）、缓存设计 | 理解系统布局、调试时 |

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
- **Phase**: 3 (Integration & Scale) — 核心完成，优化中
- **测试覆盖**: backtest 72.5% | strategy 73.4% | data 26.7% | storage 36.8%
- **关键服务**: 全部运行中 ✅

### 任务追踪
> **统一任务追踪源**: [docs/TASKS.md](docs/TASKS.md)
> 
> 所有可执行任务（P0-P3 + Phase 3 实施任务）均在 TASKS.md 中维护。
> 本文件不再维护任务列表，避免信息分散。

### 技术债
- 大部分已完成（见 [NEXT_STEPS.md](docs/NEXT_STEPS.md)）
- 主要剩余：测试覆盖率持续提升、前端交互测试补充

---

## 14. 已知问题与变通方案

| Issue | Workaround |
|-------|-----------|
| Legacy HTML UI still exists at `cmd/analysis/static/` | Deprecated; Vue SPA is the official frontend. Do not modify legacy HTML. |
| `ChatbubbleEllipsisOutline` icon name doesn't exist | Correct name is `ChatbubbleEllipsesOutline` (with 'e' before 's') |
| Trade markers may not render if portfolio_values is empty | Ensure backtest returns valid data before calling renderChart() |

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
| 使用本模板 | [AGENTS_TEMPLATE.md](docs/AGENTS_TEMPLATE.md) |

---
_Last updated: 2026-04-11_
_Source: 基于 AGENTS Template v2.0 迁移，融合 quant-trading + Claudeer 最佳实践_
_Migration ODR: odr-005-agents-md-v3-migration (pending creation)_
