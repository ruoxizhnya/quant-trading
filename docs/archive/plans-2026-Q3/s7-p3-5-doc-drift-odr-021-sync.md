# S7-P3-5: 修复文档漂移 — 同步 ODR-021 服务合并

> **任务来源**: ODR-043-7 已知问题 + ODR-043 P3 任务行
> **任务追踪**: [docs/TASKS.md](../../TASKS.md) — S7-P3-5
> **执行规范**: AGENTS.md §8 — 文档任务（一致性审查 + 原子提交）

---

## 1. Context (背景)

**问题**: ODR-021 (2026-06-12, P1-15) 将 `risk-service (8083)` 和
`execution-service (8084)` **合并入** `analysis-service (8085)` 作为 in-process
组件（`risk.RiskManager` + `live.MockTrader`）。docker-compose 服务数从 7 → 5。

但文档未同步更新 — `ARCHITECTURE.md` / `VISION.md` / `SPEC.md` / `TEST.md` /
`AGENTS.md` 仍将 risk/execution 描述为独立运行的服务。此外，已归档的
`NEXT_STEPS.md` 和 `IMPLEMENTATION_PLAN.md` 仍被多处引用为活跃文档。

**本任务范围**: 修复全部文档漂移，**除** ADR 状态漂移（ADR-014/019/020 状态
更新属于 S7-P3-6）。

**Canonical 真相来源**:
- [ODR-021](../odr/odr-021-p1-15-service-merge-risk-execution.md) — 合并决策
- `docker-compose.yml` — 实际 5 个服务（postgres/redis/data/strategy/analysis）
- `cmd/analysis/handlers_risk.go` + `handlers_execution.go` — 实际路由注册
- `cmd/analysis/deps.go` — `RiskManager` + `ExecutionTrader` in-process 注入

---

## 2. Drift Inventory (漂移清单)

### Batch A: 服务拓扑漂移（ODR-021 同步）— Commit 1

| # | 文件 | 行 | 漂移内容 |
|---|------|----|---------|
| A1 | `AGENTS.md` | 199 | `docker compose up -d # Start all 7 services` → 应为 5 |
| A2 | `docs/ARCHITECTURE.md` | 76-79 | 拓扑图显示 Risk(:8083)/Execution(:8084) 为独立服务 |
| A3 | `docs/ARCHITECTURE.md` | 115-116 | 服务端口表列 risk-service/execution-service 为 "✅ 运行中" |
| A4 | `docs/SPEC.md` | 759-778 | "Risk Service (port 8083) ⚠️ Planned" 章节 + 端点 |
| A5 | `docs/SPEC.md` | 780+ | "Execution Service (port 8084)" 章节 |
| A6 | `docs/SPEC.md` | 1220 | "Calculate positions via risk service" 散文 |
| A7 | `docs/SPEC.md` | 1311-1322 | config.yaml 示例含 `risk: port: 8083` / `execution: port: 8084` |
| A8 | `docs/VISION.md` | 403-404 | "Risk service (8083)" / "Execution service (8084)" |
| A9 | `docs/VISION.md` | 421 | docker-compose 列表含 risk-service |
| A10 | `docs/TEST.md` | 83 | "analysis-service → risk-service" 跨服务 HTTP 路径 |

### Batch B: 归档文档引用漂移 — Commit 2

| # | 文件 | 行 | 漂移内容 |
|---|------|----|---------|
| B1 | `AGENTS.md` | 456, 596, 673, 719 | 4 处引用 `docs/NEXT_STEPS.md` 为活跃文档（实际在 `archive/`） |
| B2 | `docs/AGENTS_TEMPLATE.md` | 330 | 模板行引用 NEXT_STEPS.md |
| B3 | `AGENTS.md` | 727, 735 | 2 处引用 `docs/IMPLEMENTATION_PLAN.md` 为活跃文档 |
| B4 | `docs/ROADMAP.md` | 262 | 引用 `IMPLEMENTATION_PLAN.md` |
| B5 | `docs/archive/tasks-phase-2.md` | 6 | Related 行引用 IMPLEMENTATION_PLAN.md |
| B6 | `docs/archive/migration-phase3-to-phase4.md` | 7 | Related 行引用 IMPLEMENTATION_PLAN.md |

### 不在范围（S7-P3-6）

- `docs/ADR.md` line 33: ADR-019 状态 "Proposed" → "Accepted"
- ADR-014 标记 Superseded、ADR-020 状态更新

### 不修改（归档/历史记录，per AGENTS.md §4 禁区）

- `docs/archive/**` — 归档文档不可变
- `docs/odr/**` — ODR 是不可变决策记录
- `docs/adr/adr-008*`, `adr-019*` — ADR 内引用 risk-service 是历史上下文

---

## 3. Proposed Changes (实施方案)

采用 **2 个原子 commit**:

### Commit 1: 服务拓扑漂移修复（ODR-021 同步）

#### A1. `AGENTS.md` L199
```diff
- docker compose up -d                    # Start all 7 services
+ docker compose up -d                    # Start all 5 services (ODR-021: risk+exec merged into analysis)
```

#### A2 + A3. `docs/ARCHITECTURE.md` 拓扑图 + 服务表

**拓扑图 (L76-79)**: 移除 Risk Service / Execution Svc 两个独立 box，替换为
analysis-service 下方的 in-process 注记。新拓扑图：

```
         ▼
┌──────────────┐
│ Data Service │
│   (:8081)    │
└──────┬───────┘
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│              PostgreSQL (:5432)                              │
│  ...                                                         │
└─────────────────────────────────────────────────────────────┘
```

并在 analysis-service box 内/旁注明：`risk.RiskManager + live.MockTrader (in-process, ODR-021)`

**服务表 (L115-116)**: 删除 risk-service / execution-service 两行，替换为单行注释：
```
> **ODR-021 (P1-15)**: risk-service(8083) + execution-service(8084) 已合并入
> analysis-service 作为 in-process 组件。端点暴露在 /api/risk/* 和 /api/execution/*。
> docker-compose 服务数 7 → 5。
```

#### A4 + A5. `docs/SPEC.md` Risk/Execution 章节

**Risk Service 章节 (L759)**: 重写 header + status + endpoints
- Header: `### 3. Risk API (in-process, /api/risk/* on :8085) ✅ *Implemented — P1-15 (ODR-021)*`
- Status: 改为 "Implemented as in-process `risk.RiskManager` in analysis-service"
- Endpoints: 更新为实际注册的 `/api/risk/calculate_position`, `/api/risk/detect_regime`,
  `/api/risk/check_stoploss`, `/api/risk/metrics` + legacy aliases

**Execution Service 章节 (L780)**: 同样重写
- Header: `### 4. Execution API (in-process, /api/execution/* on :8085) ✅ *Implemented — P1-15 (ODR-021)*`
- Endpoints: `/api/execution/orders` (POST/GET), `/api/execution/orders/:id` (GET),
  `/api/execution/orders/:id/cancel` (POST), `/api/execution/positions` (GET),
  `/api/execution/account` (GET) + legacy aliases

#### A6. `docs/SPEC.md` L1220 散文
```diff
-    - Calculate positions via risk service
+    - Calculate positions via in-process risk.RiskManager (ODR-021)
```

#### A7. `docs/SPEC.md` L1311-1322 config.yaml 示例
移除 `risk:` 和 `execution:` service 块，或更新为注释说明它们现在是 in-process
（无独立端口）。推荐：删除这两个块，加注释 `# risk + execution are in-process
under analysis-service (ODR-021), no separate service config needed`

#### A8. `docs/VISION.md` L403-404 API 设计
```diff
- - Risk service (8083): position sizing, VaR/CVaR, regime detection, stop-loss
- - Execution service (8084): order management (stub for v1, real integration later)
+ - Risk API (/api/risk/* on :8085, in-process): position sizing, VaR/CVaR, regime detection, stop-loss (ODR-021)
+ - Execution API (/api/execution/* on :8085, in-process): order management, MockTrader implemented, real broker integration planned for Phase 4
```

#### A9. `docs/VISION.md` L421 docker-compose 列表
```diff
- docker-compose.yml defines all services: postgres (TimescaleDB image), redis, analysis-service, data-service, strategy-service, risk-service, **ai-research-service**
+ docker-compose.yml defines 5 services (ODR-021): postgres (TimescaleDB image), redis, analysis-service (incl. in-process risk + execution), data-service, strategy-service. AI research service runs separately.
```

#### A10. `docs/TEST.md` L83 服务通信
```diff
- - analysis-service → risk-service: signal weight adjustment returns within timeout
+ - analysis-service in-process risk.RiskManager: signal weight adjustment (no cross-service HTTP, ODR-021)
```

#### Commit 1 验证
- 人工审阅所有改动点确认与 ODR-021 + 代码一致
- 检查 markdown 渲染（表格、代码块格式）
- `grep -rn "8083\|8084\|risk-service\|execution-service" docs/ARCHITECTURE.md docs/VISION.md docs/SPEC.md docs/TEST.md AGENTS.md` — 应无残留（历史 changelog 注释除外）

---

### Commit 2: 归档文档引用修复

#### B1. `AGENTS.md` NEXT_STEPS.md 引用（4 处）

策略：将 `docs/NEXT_STEPS.md` 改为 `docs/archive/NEXT_STEPS.md`，并在首次出现处
加 `(archived)` 标注。对于"快速启动清单"等指向 NEXT_STEPS 作为"待办事项"的引用，
替换为指向 `docs/TASKS.md`（当前活跃的任务追踪文档）。

具体：
- L456 (文档分类表): `ROADMAP.md, NEXT_STEPS.md` → `ROADMAP.md, TASKS.md`（NEXT_STEPS 已归档）
- L596 (Reference 表行): 更新链接为 `archive/NEXT_STEPS.md` + 标注 `(archived)`
- L673 (技术债段): "见 NEXT_STEPS.md" → "见 docs/TASKS.md（NEXT_STEPS 已归档）"
- L719 (快速清单): "了解待办事项 → NEXT_STEPS.md" → "了解待办事项 → TASKS.md"

#### B2. `docs/AGENTS_TEMPLATE.md` L330
模板行更新为指向 TASKS.md（模板应反映当前最佳实践，而非已归档文档）

#### B3. `AGENTS.md` IMPLEMENTATION_PLAN.md 引用（2 处）
- L727 (快速清单): "Phase 4 实施计划 → IMPLEMENTATION_PLAN.md" → 改为指向
  `docs/archive/tasks-phase-2.md`（当前活跃的 Phase 4 任务追踪）+ 注释 IMPLEMENTATION_PLAN 已归档
- L735 (footer): 更新引用

#### B4. `docs/ROADMAP.md` L262
```diff
- 详见 [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md) Phase 0-5.
+ 详见 [archive/tasks-phase-2.md](../tasks-phase-2.md) (IMPLEMENTATION_PLAN.md 已归档至 archive/)。
```

#### B5. `docs/archive/tasks-phase-2.md` L6
```diff
- > **Related**: IMPLEMENTATION_PLAN.md, ROADMAP.md, ADR-015
+ > **Related**: ROADMAP.md, ADR-015 (IMPLEMENTATION_PLAN.md 已归档至 archive/)
```

#### B6. `docs/archive/migration-phase3-to-phase4.md` L7
```diff
- > **Related**: [VISION.md](../../VISION.md), [ADR-015](../../adr/adr-015-ai-agent-architecture.md), [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md)
+ > **Related**: [VISION.md](../../VISION.md), [ADR-015](../../adr/adr-015-ai-agent-architecture.md), [archive/tasks-phase-2.md](../tasks-phase-2.md) (IMPLEMENTATION_PLAN.md 已归档)
```

#### Commit 2 验证
- `grep -rn "docs/NEXT_STEPS.md\|docs/IMPLEMENTATION_PLAN.md" docs/ AGENTS.md` —
  应无残留指向活跃路径的引用（archive/ 内部自引用除外）

---

## 4. Files Affected (受影响文件)

### Commit 1 (服务拓扑漂移)
| 文件 | 改动类型 |
|------|---------|
| `AGENTS.md` | 1 处注释（7→5 services） |
| `docs/ARCHITECTURE.md` | 拓扑图 + 服务表 |
| `docs/SPEC.md` | 2 章节 header + endpoints + 1 行散文 + config.yaml 块 |
| `docs/VISION.md` | 2 处散文 |
| `docs/TEST.md` | 1 处散文 |

### Commit 2 (归档引用漂移)
| 文件 | 改动类型 |
|------|---------|
| `AGENTS.md` | 6 处引用更新 |
| `docs/AGENTS_TEMPLATE.md` | 1 处模板行 |
| `docs/ROADMAP.md` | 1 处引用 |
| `docs/archive/tasks-phase-2.md` | 1 处 Related 行 |
| `docs/archive/migration-phase3-to-phase4.md` | 1 处 Related 行 |

### 文档更新（Commit 2 内）
| 文件 | 改动 |
|------|------|
| `docs/TASKS.md` | S7-P3-5 状态 `⬜` → `✅` |
| `AGENTS.md` Known Issues | 移除 ODR-043-7 行（或更新为仅剩 ADR 状态漂移） |

---

## 5. Assumptions & Decisions (假设与决策)

### D1: S7-P3-5 范围 = 服务拓扑 + 归档引用；ADR 状态 = S7-P3-6
**理由**: 任务名"同步 ODR-021 服务合并"聚焦服务拓扑。但"修复全部文档漂移"
包含归档引用。ADR 状态更新是独立的 governance 动作，单独列为 S7-P3-6。

### D2: NEXT_STEPS 引用替换为 TASKS.md
**理由**: NEXT_STEPS.md 已归档至 `docs/archive/`（per ODR-008）。当前活跃的
任务追踪文档是 `docs/TASKS.md`（Phase 3）和 `docs/archive/tasks-phase-2.md`（Phase 4）。
引用应指向活跃文档。

### D3: IMPLEMENTATION_PLAN 引用替换为 archive/tasks-phase-2.md
**理由**: IMPLEMENTATION_PLAN.md 已归档。Phase 4 任务追踪在 archive/tasks-phase-2.md。

### D4: 归档文档内部自引用不修改
**理由**: AGENTS.md §4 禁区规定 `docs/archive/` 不可变。归档文档内部引用其他
归档文档是历史快照，保持原样。

### D5: 2 个原子 commit
- Commit 1: 服务拓扑（一个逻辑主题 — ODR-021 同步）
- Commit 2: 归档引用（另一个逻辑主题 — 引用卫生）
- 拆分便于回滚 + 清晰的 commit history

---

## 6. Verification (验证步骤)

### Commit 1 完成后
```bash
# 确认无残留的独立服务引用（历史 changelog 注释除外）
grep -rn "8083\|8084" docs/ARCHITECTURE.md docs/VISION.md docs/SPEC.md docs/TEST.md AGENTS.md
grep -rn "risk-service\|execution-service" docs/ARCHITECTURE.md docs/VISION.md docs/SPEC.md docs/TEST.md AGENTS.md
# 期望：仅历史 changelog 段落（如 AGENTS.md §1 ODR-021 注记）保留，活跃描述无残留
```

### Commit 2 完成后
```bash
# 确认无指向活跃路径的归档文档引用
grep -rn "docs/NEXT_STEPS.md\|docs/IMPLEMENTATION_PLAN.md" docs/ AGENTS.md
# 期望：仅 archive/ 内部 + ODR 历史记录
```

### 最终
- 人工通读修改后的章节确认语义正确
- 检查 markdown 链接有效性（相对路径正确）
- 确认 TASKS.md S7-P3-5 标记 ✅

---

## 7. Commit Message Format

### Commit 1
```
docs: sync service topology to ODR-021 merge (S7-P3-5)

Updates ARCHITECTURE.md, VISION.md, SPEC.md, TEST.md, and AGENTS.md
to reflect that risk-service(8083) + execution-service(8084) were
merged into analysis-service(8085) as in-process components per
ODR-021 (P1-15). docker-compose service count 7 → 5.

Changes:
- ARCHITECTURE.md: remove standalone risk/execution boxes from
  topology diagram; remove their rows from service port table
- SPEC.md: rewrite Risk/Execution sections as /api/risk/* and
  /api/execution/* in-process endpoints on :8085; fix config.yaml
  example; fix backtest flow prose
- VISION.md: update API design + docker-compose service list
- TEST.md: update service communication description
- AGENTS.md: fix "Start all 7 services" → "Start all 5 services"

Refs: S7-P3-5
Reviewed: cross-referenced with ODR-021 + cmd/analysis/handlers_*.go
```

### Commit 2
```
docs: fix stale references to archived NEXT_STEPS/IMPLEMENTATION_PLAN (S7-P3-5)

NEXT_STEPS.md and IMPLEMENTATION_PLAN.md were archived to docs/archive/
(per ODR-008) but still cited as active documents in AGENTS.md,
ROADMAP.md, archive/tasks-phase-2.md, and the migration guide.

Replaces stale references with current active docs:
- NEXT_STEPS.md → TASKS.md (active Phase 3 task tracker)
- IMPLEMENTATION_PLAN.md → archive/tasks-phase-2.md (active Phase 4 tracker)

Refs: S7-P3-5
Reviewed: grep verified no remaining active-path references
```

---

## 8. Out of Scope (不在范围)

- **ADR 状态更新** (ADR-014 Superseded, ADR-019/020 Accepted) — S7-P3-6
- **归档文档内部修改** — AGENTS.md §4 禁区
- **ODR/ADR 历史记录修改** — 不可变决策记录
