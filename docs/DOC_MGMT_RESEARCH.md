# Agentic/Vibe Coding 文档管理深度研究报告

> **研究日期**: 2026-04-09
> **研究范围**: AI 辅助编码场景下的文档管理最佳实践、架构设计、迁移评估
> **适用项目**: quant-trading (Go + Vue 3 全栈量化交易系统)

---

## 一、研究背景与动机

### 1.1 问题定义

当前 quant-trading 项目在 Agentic Coding（AI 编码代理辅助开发）场景下面临以下文档管理挑战：

| 挑战 | 具体表现 |
|------|---------|
| **文档膨胀** | 清理后仍有 22 个 `.md` 文档 (3,908 行)，AI agent 难以快速定位关键信息 |
| **信息分散** | Strategy 接口定义散布在 VISION/SPEC/ARCHITECTURE 三处，需人工同步 |
| **静态过时** | 文档更新依赖人工审查，无法随代码变更自动演进 |
| **AI 不友好** | 当前文档面向人类读者设计，缺少 machine-readable 结构 |
| **历史 vs 实时** | ADR 提供决策历史，但缺少"当前生效规则"的实时视图 |

### 1.2 研究目标

1. 确定该场景下的**最优文档管理方法与实践**
2. 设计支持**历史记录** (ADR-like) 和**实时改进** (live improvement) 的文档架构
3. 调研并总结相关领域**最佳实现方案**
4. 对比分析最佳方案与**当前实践的差异**
5. 评估**迁移可行性与必要性**

---

## 二、行业最佳实践调研结果

### 2.1 AGENTS.md — 行业新标准 ⭐⭐⭐⭐⭐

**来源**: [AGENTS.md 规范](https://agentsmd.io/) / [Linux Foundation AAIF](https://agentic-ai.foundation/) / [GitHub 分析(2500+ repos)](https://github.blog)

#### 核心定位

> **"README is for humans. AGENTS.md is for machines."**
> — Particula Tech, AGENTS.md Explained

| 属性 | 值 |
|------|-----|
| **发起者** | OpenAI (2025-05, Codex CLI) |
| **治理方** | Linux Foundation — Agentic AI Foundation (AAIF) |
| **采用量** | **60,000+** 开源仓库 |
| **支持工具** | **25+** (Codex, Copilot, Cursor, Windsurf, Gemini CLI, Aider, Zed...) |
| **格式** | Markdown (machine-readable) |
| **位置** | 项目根目录 (可嵌套 monorepo 子目录) |

#### 标准结构 (GitHub 推荐的 4 大特征)

```markdown
# Role
You are a backend API agent for a FastAPI service.
You work exclusively in `src/api/` and `tests/api/`.
You never modify frontend code, infrastructure configs, or database migrations.

# Commands
- `pnpm install` — Install dependencies
- `pnpm dev` — Start dev server (port 3000)
- `pnpm test` — Run test suite
- `pnpm test:e2e` — Run Playwright E2E tests
- `pnpm lint` — Run ESLint + Prettier check

# Code Style
Use functional components with TypeScript. Named exports only.
<!-- GOOD -->
export function UserCard({ name, role }: UserCardProps) { ... }
<!-- BAD -->
export default class UserCard extends Component { ... }

# Boundaries
### Always Do
- Run `pnpm test` before submitting any changes
### Ask First
- Before changing database schema or migrations
- Before adding new dependencies to package.json
### Never Do
- Never run migrations without approval
- Never commit directly to main branch
```

#### 关键设计原则

| 原则 | 说明 | 来源 |
|------|------|------|
| **Commands First** | 可执行命令放在最前面，agent 可直接执行 | GitHub Analysis |
| **Code Examples > Prose** | 用代码片段而非文字描述风格 | Particula Tech |
| **Three-Tier Boundaries** | Always Do / Ask First / Never Do | OpenAI Spec |
| **Role Definition** | 明确 agent 的职责范围和限制 | GitHub Analysis |
| **Nested Overrides** | Monorepo 支持子目录 AGENTS.md 覆盖父级 | OpenAI Spec |

#### 与现有配置文件的关系

| 工具 | 原有配置 | AGENTS.md 方案 |
|------|---------|---------------|
| Claude Code | `CLAUDE.md` | `CLAUDE.md` → `@AGENTS.md` 引用 + Claude 特有内容 |
| Cursor IDE | `.cursorrules` | 符号链接 → `AGENTS.md` |
| Windsurf IDE | `.windsurfrules` | 符号链接 → `AGENTS.md` |
| Gemini CLI | `settings.json` | `"contextFileName": "AGENTS.md"` |

**参考案例**: Apache Superset ADR-002 ([GitHub](https://github.com/apache/superset/blob/main/docs/adr/002-agents-md-adoption.md)) — 采用 AGENTS.md 后配置体积减少 33%，消除 70% 内容重复。

---

### 2.2 Agentic Coding 6 原则 + 28 实践 ⭐⭐⭐⭐

**来源**: [agentic-coding.github.io](https://agentic-coding.github.io/) (Benedict Lee, CC-BY 4.0)

这是目前最系统的 Agentic Coding 方法论框架：

#### 6 大核心原则

| # | 原则 | 核心含义 |
|---|------|---------|
| P1 | **Developer Accountability** | 开发者对 AI 生成的代码负最终责任。"AI did it" 不是有效借口 |
| P2 | **Understand and Verify** | 盲目接受 AI 代码是禁止的。必须理解后再集成 |
| P3 | **Security & Confidentiality** | 不向未批准的外部 AI agent 输入敏感信息 |
| P4 | **Code Quality Standards** | 确保 AI 生成代码符合团队规范和架构模式 |
| P5 | **Human-Led Design** | 核心系统设计和关键业务逻辑必须由人主导 |
| P6 | **Recognize AI Limitations** | 了解 AI 的边界（幻觉/偏见/知识截止）|

#### 28 条实践 (与文档管理相关的关键条目)

| 类别 | # | 实践 | 文档管理启示 |
|------|-----|------|------------|
| **A. 准备** | **#1** | Setting Agent Rules (`.cursorrules`, `CLAUDE.md`) | → 应统一为 AGENTS.md SSOT |
| | **#2** | Providing Project Structure Context | → AGENTS.md 的核心内容之一 |
| **B. 策略** | **#10** | Plan First, Code Later | → plan.md 作为实时改进载体 |
| **C. 交互** | **#7** | Decompose Tasks into Manageable Units | → 文档也应模块化 |
| | **#8** | Few-Shot Prompting with Code Examples | → 文档中嵌入代码示例 |
| **D. 审查** | **#19** | Indicate AI Tool Used in PR | → 决策记录应关联 commit |
| **E. 质量** | **#20** | Ensure Generated Code Adheres to Standards | → AGENTS.md Boundaries |
| **F. 工作流** | **#24** | Use State Files for Complex Tasks (`plan.md`) | → 动态任务状态文档 |
| | **#25** | Checkpoints & Rollback | → 文档版本控制 |
| | **#27** | Team-Level Learning Sharing | → ADR 的团队学习功能 |

---

### 2.3 Cursor/Windsurf 文档工作流最佳实践 ⭐⭐⭐⭐

**来源**: [Rivet.dev "Writing Docs for AI"](https://www.rivet.dev/blog/2025-03-15-writing-docs-for-ai), [Mintlify](https://mintlify.com/blog/6-tips-every-developer-should-know-when-using-cursor-and-windsurf), [Balakumar.dev "Two File Management"](https://blog.balakumar.dev/2025/04/18/ditching-the-clutter)

#### 关键发现

##### 发现 1: LLM-Optimized Markdown 格式

```
AI tools parse these formats best:
✅ Markdown with clear headings (##, ###)
✅ Code blocks with language tags (```go, ```yaml)
✅ Tables for structured data
✅ Bullet lists for rules/options
❌ Long prose paragraphs (LLMs skip)
❌ Images (most agents can't see them)
❌ Nested HTML (confuses parsers)
```

##### 发现 2: `/docs:` 内联查询模式

Cursor/Windsurf 支持 `/docs:` 前缀直接查询文档库：
```
用户输入: /docs: 如何处理 T+1 交割？
Agent 行为: 从索引文档中检索相关段落返回，无需切换标签页
```
**前提**: 文档必须以 Markdown 发布且包含 `/llms.txt` 索引。

##### 发现 3: ai_dynamic_task.md — 实时任务状态追踪

[Balakumar.dev](https://blog.balakumar.dev/2025/04/18/ditching-the-clutter) 提出的双文件管理模式：
```
AGENTS.md          ← 静态规则 (SSOT，很少变更)
ai_dynamic_task.md ← 动态任务状态 (每次会话更新)
```

`ai_dynamic_task.md` 记录:
```markdown
## Current Task
Implement user authentication module

## State
- Step 1: ✅ Design schema (2026-04-09 14:00)
- Step 2: 🔄 Implement JWT service (in progress)
- Step 3: ⬜ Write unit tests
- Step 4: ⬜ Integration testing

## Notes
- User requested OAuth2 support in addition to JWT
- Need to check if redis is available for token blacklist
```

##### 发现 4: Standup Format 重置混乱会话

当 Agent 上下文漂移时，使用 standup 格式重置：
```
@AGENTS.md

## Standup
Since last session:
- Completed: BacktestEngine.vue chart rendering fix
- In Progress: Documentation audit
- Blocked: None
- Next: Implement trade signal visualization E2E tests

Please continue from where we left off.
```

---

### 2.4 其他相关实践

| 实践 | 来源 | 要点 |
|------|------|------|
| **ADR + Decision Log** | ThoughtWorks ADR | 决策记录不可变，但可有后续修正 ADR |
| **Living Documentation** | GitBook/Confluence 模式 | 文档即代码，PR 自动触发更新 |
| **docs/ as Package** | Rust/Go 社区惯例 | `cargo doc --open` / `pkgsite.dev` 自动生成 |
| **CHANGELOG.md** | Keep a Changelog 标准 | 版本化变更日志，机器可解析 |
| **.cursorignore** | Cursor 安全实践 | 排除敏感文件不被 AI 索引 |

---

## 三、最优文档架构设计

基于以上调研，设计适用于 quant-trading 项目的 **"双层四区"文档架构**。

### 3.1 架构总览

```
quant-trading/
│
├── AGENTS.md                    ★ 核心: AI Agent SSOT (~200行)
├── README.md                    ★ 入口: 人类开发者入口
│
├── docs/
│   ├── VISION.md                ★ 设计愿景 (稳定少变)
│   ├── SPEC.md                  ★ 技术规格 (随迭代更新)
│   ├── ARCHITECTURE.md          ★ 架构设计 (重大变更时更新)
│   │
│   ├── ROADMAP.md               ◐ 进度规划 (Sprint 级别更新)
│   ├── NEXT_STEPS.md            ◐ 待办事项 (审计/计划输出)
│   │
│   ├── adr/                     ★ 决策历史 (只追加不修改)
│   │   ├── adr-001-plugin-loading.md
│   │   ├── ...
│   │   └── adr-010-speed-architecture.md
│   │
│   └── reports/                 ◐ 审计/审查报告 (按需生成)
│       ├── DOC_AUDIT_REPORT.md
│       └── CLEANUP_REPORT.md
│
├── .session/                    ◐ 会话级动态文档 (gitignored)
│   └── task-current.md          ← 每次编码会话的任务状态
│
├── CLAUDE.md                   → @AGENTS.md 引用 (Claude Code)
├── .cursorrules                → AGENTS.md (符号链接)
└── .windsurfrules              → AGENTS.md (符号链接)
```

### 3.2 四个文档分区

| 分区 | 定位 | 更新频率 | 目标读者 | 文件数 |
|------|------|---------|---------|--------|
| **P0: Agent SSOT** | AI 编码代理的唯一事实来源 | 低 (稳定) | AI Agents | 1 (AGENTS.md) |
| **P1: Design Core** | 系统设计的权威定义 | 中 (迭代时) | 人类+AI | 3 (VISION+SPEC+ARCH) |
| **P2: Living Plans** | 进度/待办/报告 | 高 (持续) | PM/开发者 | 4+ |
| **P3: Historical** | 不可变决策历史 | 只增不减 | 架构师/审计者 | 10+ (ADR) |

### 3.3 AGENTS.md 详细设计 (推荐模板)

```markdown
# Quant Lab — Agentic Coding Configuration

## Role
You are a full-stack development agent working on an A-share quantitative trading system.
The project uses Go backend (:8085) + Vue 3 SPA frontend (:5173) + PostgreSQL + Redis.

## Scope
- **Backend**: `cmd/`, `pkg/` (Go modules)
- **Frontend**: `web/src/` (Vue 3 + TypeScript)
- **Tests**: `e2e/tests/` (Playwright E2E)
- **Docs**: `docs/` (Markdown design docs)
- **NEVER modify**: `node_modules/`, `dist/`, `.env*`, `migrations/` (auto-generated)

## Commands
```bash
# Backend
go build ./...                          # Build all packages
go test ./pkg/backtest/... -v           # Run backtest tests
go test ./pkg/data/... -v               # Run data pipeline tests

# Frontend
cd web && npm install                   # Install deps
npm run dev                             # Dev server (:5173)
npm run build                           # Production build
npm run lint                            # ESLint + Prettier
npm run typecheck                       # TypeScript check

# E2E Tests
cd e2e && npx playwright test            # Full E2E suite
npx playwright test --project=chrome    # Chrome only

# Infrastructure
docker compose up -d                    # Start all services
docker compose logs -f analysis-service  # Tail logs
```

## Code Style — Backend (Go)
- Follow Effective Go conventions
- Use `context.Context` as first parameter
- Error handling: return error, never panic in production
- Naming: PascalCase for exported, camelCase for unexported
- Package structure: domain/ → data/ → strategy/ → backtest/ → storage/

## Code Style — Frontend (Vue 3 + TS)
- Composition API with `<script setup lang="ts">`
- Naive UI component library (dark theme)
- Pinia for state management (shallowRef for large objects)
- Chart.js for data visualization
- Component naming: PascalCase (BacktestForm.vue, EquityChart.vue)

## Key Patterns
### Strategy Interface (Canonical)
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

### API Client Pattern
```typescript
// web/src/api/client.ts — centralized fetch wrapper
// All API calls go through this client with unified error handling
// Base URL: http://localhost:8085 (Vite proxy in dev mode)
```

### Data Flow
```
Browser (Vue SPA) → Vite Proxy → analysis-service (:8085)
                                    ├── POST /backtest → engine.go
                                    ├── GET /ohlcv/:symbol → data-service proxy
                                    └── GET /strategies → strategy-service proxy
```

## Boundaries

### Always Do
- Run `npm run lint && npm run typecheck` before committing frontend code
- Run `go vet ./...` before committing backend code
- Run `npx playwright test` after UI changes
- Update AGENTS.md when adding new commands or changing patterns
- Reference docs/VISION.md for design decisions, don't reinvent

### Ask First
- Before modifying database schema (add migration file)
- Before adding new npm/pip dependencies
- Before changing core interfaces (Strategy, Domain types)
- Before removing or renaming existing API endpoints

### Never Do
- NEVER hardcode API keys, secrets, or credentials
- NEVER commit to main branch directly (use PR workflow)
- NEVER modify `node_modules/`, `dist/`, or auto-generated files
- NEVER use `any` type in TypeScript without explicit justification
- NEVER generate code you cannot explain to a teammate

## Document Index
| Doc | Purpose | When to Read |
|-----|---------|-------------|
| VISION.md | Design principles, domain model | Starting new feature |
| SPEC.md | API specs, data models, interfaces | Implementing endpoints |
| ARCHITECTURE.md | Service topology, DB schema | Understanding system layout |
| ROADMAP.md | Sprint progress, milestones | Planning work |
| NEXT_STEPS.md | Audit findings, TODO list | After code review |
| adr/*.md | Architectural decision history | Questioning past decisions |

## Session Management
For multi-step tasks, maintain state in `.session/task-current.md`:
- Current objective
- Completed steps (with timestamps)
- Blocking issues
- Next actions
Reset with Standup format when context drifts.
```

### 3.4 实时改进机制 (Live Improvement)

#### 机制 1: 任务状态文件 (.session/task-current.md)

```markdown
<!-- .session/task-current.md (gitignored) -->
# Active Task: Trade Signal Visualization E2E Tests

## Objective
Add E2E tests for buy/sell markers on equity curve chart.

## Progress
- [x] T-01: Chart rendering baseline test
- [x] T-02: Buy marker (green triangle) visibility
- [ ] T-03: Sell marker (red cross) visibility
- [ ] T-04: Toggle button behavior
- [ ] T-05: Tooltip content verification

## Notes
- Chart.js Scatter dataset requires explicit `type: 'scatter'`
- Markers map to chart x-axis via date index lookup
- See BACKTEST_ENGINE_ISSUES.md for known limitations

---
Last updated: 2026-04-09 16:30 by AI Assistant
```

**生命周期**: 创建于任务开始 → 持续更新 → 完成后归档至 `docs/reports/` 或删除。

#### 机制 2: Standup 重置

当 Agent 上下文漂移时，使用以下 prompt 重置：

```
@AGENTS.md

## Standup
Since last session:
- Completed: [list finished items]
- In Progress: [current work]
- Blocked: [anything blocking?]
- Next: [immediate next step]

Context from previous session: [brief summary of key decisions made]
Please continue from where we left off.
```

#### 机制 3: 决策记录即时追加 (ADR Live)

传统 ADR 是事后记录。Agentic 场景下建议增加 **ADR-Lite** 模式：

```markdown
<!-- 在对话中做出重要决策后立即追加到 ADR -->
## ADR-011: Frontend Architecture Decision (Vue SPA vs Legacy HTML)
- **Date**: 2026-04-09
- **Status**: Proposed (pending human review)
- **Context**: Dual UI system identified during DOC_AUDIT
- **Decision**: Vue SPA is sole official frontend; legacy HTML deprecated
- **Triggered by**: NEXT_STEPS.md Q-003 finding
```

确认后转为正式 ADR。

---

## 四、对比分析：最佳方案 vs 当前实践

### 4.1 差异矩阵

| 维度 | 最佳实践 (推荐) | 当前实践 | 差距 | 严重度 |
|------|----------------|---------|------|--------|
| **AI SSOT** | AGENTS.md (单一入口, ~200行) | ❌ **不存在** | 完全缺失 | 🔴 Critical |
| **Agent Rules 分散** | 统一 AGENTS.md + 工具包装器 | ❌ 无任何 agent 配置 | 缺失 | 🔴 Critical |
| **Strategy 接口** | AGENTS.md Canonical 定义 + 3 文档引用 | ✅ 已修复 (本轮审计) | 已对齐 | ✅ |
| **三层边界** | AlwaysDo / AskFirst / NeverDo | ❌ 无明确边界定义 | 缺失 | 🟠 High |
| **命令清单** | AGENTS.md Commands 区 (可直接执行) | ❌ 散落在 README/各处 | 部分存在 | 🟠 High |
| **任务状态追踪** | .session/task-current.md (实时) | ❌ 不存在 | 缺失 | 🟠 High |
| **会话重置** | Standup format | ❌ 不存在 | 缺失 | 🟡 Medium |
| **决策历史** | ADR (10条, 不可变) | ✅ 存在且质量好 | 充足 | ✅ |
| **设计核心** | VISION+SPEC+ARCH (3文档) | ✅ 存在 | 充足 | ✅ |
| **进度规划** | ROADMAP+NEXT_STEPS | ✅ 存在 | 充足 | ✅ |
| **文档数量** | ~15 (精简目标) | 22 | 多 7 个 | 🟡 Medium |
| **LLM 友好度** | Code examples > prose, 结构化 markdown | ⚠️ 部分文档偏散文式 | 中等 | 🟡 Medium |
| **Monorepo 嵌套** | 子目录 AGENTS.md 覆盖 | N/A (单仓库) | 不适用 | — |

### 4.2 关键差距详解

#### 差距 1: 缺少 AGENTS.md (🔴 Critical)

**影响**: 每次 AI Agent (Trae/Cursor/Claude 等) 进入项目时：
- 不知道项目的构建命令 → 可能运行错误的测试框架
- 不知道代码风格 → 生成不符合规范的代码
- 不知道哪些不能改 → 可能修改敏感文件
- 不知道文档在哪里 → 无法快速查找设计依据

**量化影响**: 根据Particula Tech 的客户端数据，添加 AGENTS.md 后 Agent 错误率降低约 **60%**。

#### 差距 2: 缺少三层边界定义 (🟠 High)

**影响**: 
- Agent 可能擅自修改数据库 schema
- Agent 可能添加未经审核的依赖
- Agent 可能删除关键文件
- Agent 可能提交到 main 分支

#### 差距 3: 缺少实时任务状态 (🟠 High)

**影响**:
- 多轮对话中 Agent 丢失上下文
- 复杂任务无法跨 session 保持状态
- 无法追踪 "上次做到哪了"

---

## 五、迁移评估

### 5.1 迁移方案

| 阶段 | 操作 | 工作量 | 风险 |
|------|------|--------|------|
| **Phase 1: 创建 AGENTS.md** | 新建根目录 AGENTS.md (~200行) | 2h | 低 (纯新增) |
| **Phase 2: 工具适配** | 创建 CLAUDE.md + .cursorrules 符号链接 | 30min | 低 |
| **Phase 3: 建立 .session/** | 创建目录 + task-current.md 模板 | 15min | 低 |
| **Phase 4: 文档瘦身** | 将重复内容从 P1/P2 文档移入 AGENTS.md 引用 | 2h | 中 (需验证不影响现有文档) |
| **Phase 5: 团队培训** | 编写 ADR-011 说明新文档架构 | 1h | 低 |

**总计**: 约 **5.5 小时**，分 2-3 次完成。

### 5.2 可行性评估

| 评估维度 | 评分 | 说明 |
|---------|------|------|
| **技术可行性** | ⭐⭐⭐⭐⭐ | 纯 Markdown 文件操作，无代码改动风险 |
| **工具兼容性** | ⭐⭐⭐⭐⭐ | Trae IDE 支持 AGENTS.md; 也可通过自定义规则注入 |
| **向后兼容** | ⭐⭐⭐⭐⭐ | 新增文件不影响现有 22 个文档; 可渐进式迁移 |
| **维护成本** | ⭐⭐⭐⭐ | AGENTS.md 稳定后很少变更; task-current.md 按 session 管理 |
| **团队接受度** | ⭐⭐⭐⭐ | 符合行业标准 (60K+ repos); 降低认知负荷 |

### 5.3 必要性评估

| 评估维度 | 结论 | 理由 |
|---------|------|------|
| **是否必要?** | ✅ **高必要性** | 当前每轮 AI 编码会话都因缺少上下文而浪费大量时间在重复说明项目规则上 |
| **ROI?** | ✅ **高回报** | 投入 5.5h, 预期节省每会话 15-30min × N 会话 |
| **紧迫性?** | 🟡 **中等** | 项目处于活跃开发期，越早建立越好; 但非阻塞当前功能开发 |
| **替代方案?** | ❌ **无更好方案** | AGENTS.md 是行业共识标准; 自制方案会增加维护负担 |

### 5.4 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| AGENTS.md 过时 (与代码不同步) | 中 | 高 | 在 CI 中加入检查: 关键接口签名是否匹配 |
| .session/ 文件泄露敏感信息 | 低 | 高 | 已在 .gitignore 排除; 明确标注不含密钥 |
| 工具不支持 AGENTS.md (Trae) | 低 | 中 | Trae 可通过项目规则或 custom instructions 注入等价内容 |
| 文档冗余 (AGENTS.md 与其他文档重复) | 中 | 低 | AGENTS.md 只放 "怎么做", 设计文档放 "为什么" |

---

## 六、结论与建议

### 6.1 最终建议

**立即执行 Phase 1-3** (创建 AGENTS.md + 工具适配 + .session/ 目录):

1. **创建 `AGENTS.md`** — 这是投入产出比最高的单一改进
2. **创建 `.session/` 目录** — 解决多轮对话上下文丢失问题
3. **可选: 创建工具适配文件** — 如果团队使用 Cursor/Claude Code

**暂缓 Phase 4-5** (文档瘦身 + 团队培训):
- 当前 22 个文档体系已足够清晰
- 瘦身可在下次文档审查周期进行
- 团队培训可在 ADR-011 中顺带完成

### 6.2 长期演进路线

```
Now (v1.0):     AGENTS.md + .session/ + 现有 22 文档
Next (v1.5):   文档瘦身至 ~15 个; ADR-Lite 实时决策
Future (v2.0):  /docs: 内联查询 (/docs: 模式); 自动化文档同步 CI
Vision (v3.0):  文档即代码 — 设计变更自动触发文档 PR
```

### 6.3 核心原则总结

> **"文档应该像代码一样被对待: 有版本控制、有测试覆盖、有明确的消费者(AI/Human)、有清晰的契约(SSOT)."**

对于 Agentic Coding 场景，这意味着:
1. **一份 AGENTS.md** 给 AI Agent 看 (怎么做)
2. **一套设计文档** 给人类和 AI 共同看 (为什么)
3. **一串 ADR** 记录决策历史 (做过什么)
4. **一个 task-current.md** 追踪实时进展 (正在做什么)

这四层构成了完整的文档信息架构。

---

_研究报告完成。建议立即执行 Phase 1 (创建 AGENTS.md)，预计 2 小时内完成。_
