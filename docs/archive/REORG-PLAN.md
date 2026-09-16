---
status: active
type: plan
created: 2026-09-16
last-verified: 2026-09-16
verified-by: 人工确认（用户已选择 ODR 全归档 / TASKS 重建 / 先出方案）
owner: 若曦
supersedes: docs/archive/reports-2026-Q2/DOC_AUDIT_REPORT.md
---

# 文档体系重组方案

> 本文件是**一次性执行方案**，执行完成后移入 `05-archive/`。

---

## 1. 目标

把 147 篇 / 2.5MB 文档从「按主题平铺 + 追加式增长」改为「按生命周期分层 + 常青演进」。

判据：改完之后，**新来的人（或半年后的你）能在 10 分钟内找到"我该读哪一份"**。

---

## 2. 方法论依据

| 框架 | 来源 | 采纳的规则 |
|---|---|---|
| **Diátaxis** | diataxis.fr | 按用户意图分四类（教程 / 指南 / 参考 / 解释），不混合 |
| **ADR 反模式** | ozimmer.ch/practices | ADR 只记「高架构显著性 + 高撤销成本」；细节外移；日志不成史诗 |
| **Docs as Code** | 行业通识 | 文档进版本控制与 CI，必须可被自动验证 |

---

## 3. 诊断：三个结构性错误

| # | 问题 | 数据 |
|---|---|---|
| 1 | **体量倒挂** | TASKS 217KB > VISION 74KB > SPEC 68KB > PRODUCT 23KB。顶层最大，应最小 |
| 2 | **生命周期混杂** | 决策（ADR 22）/ 规范（SPEC）/ 计划（TASKS）/ 记录（ODR 64）混在同一层 |
| 3 | **累积而非演进** | TASKS.md 含两组 `🔴 P0` 章节 + Sprint 1~6 全部历史，被当追加日志用 |

核心结论：**ODR 不是决策记录，是工作日志**——与 git log 重复，却占 756KB 与主导航位置。

---

## 4. 新结构

```
docs/
├── README.md                  [新建] 唯一入口：导航 + 文档规则（≤3 页）
├── 01-product/                Evergreen · 这是什么
│   ├── PRODUCT.md             [重写] ≤3 页
│   ├── VISION.md              [重写] 74KB → ≤2 页
│   └── ROADMAP.md             [精简] ≤2 页
├── 02-architecture/           Evergreen · 怎么搭
│   ├── ARCHITECTURE.md        [重写] 66KB → ≤5 页
│   ├── SPEC.md                [保留] Reference 类可长
│   ├── adr/                   [收敛] 22 → 17 条 + 索引
│   └── design/                [精简] 设计细节
├── 03-guides/                 How-to · 怎么做具体事
│   ├── testing.md             [自 TEST.md]
│   ├── test-cases/            [移入]
│   └── setup/add-datasource/run-experiment   [新建]
├── 04-current/                Active · 现在在做什么
│   ├── TASKS.md               [重建] ≤5 页，只放未完成项
│   └── ACTIVE.md              [新建] 当前 sprint，完成即归档
├── 05-archive/                Archived · 只读，不维护，不进导航
│   ├── odr/                   ← 64 个 ODR 全量
│   ├── superseded-adr/        ← ADR 009/010/012/021
│   ├── 2026-q2-reports/ 2026-q3-plans/ research-2026-q2/
│   ├── TASKS-history.md       ← 原 TASKS.md
│   ├── VISION-full.md         ← 原 VISION.md 备份
│   └── archive/RESEARCH-equitydeep-legacy.md            ← 工作面 1 旧详案（定位已变更）
└── hermes/                    [保留] agent 配置
```

---

## 5. 治理规则（写进 docs/README.md）

**R1 · Frontmatter 强制**
每份文档顶部必须标注：`status` / `last-verified` / `verified-by`。
**没有 `last-verified` 的文档 = 已腐烂。**

**R2 · 常青层字数上限**
PRODUCT ≤3 页、VISION ≤2 页、ARCHITECTURE ≤5 页、ROADMAP ≤2 页、TASKS ≤5 页。
超限 → 细节外移到 `02-architecture/design/`。

**R3 · 状态不可逆**
`evergreen` 随代码更新；`active` 完成即转 `archived`；`archived` 只读、不改、不进导航。

**R4 · 何时该写 ADR**（防止 ADR 再次膨胀）
只记「高架构显著性 + 高撤销成本」的决策。不属于此列的一律写进 git commit message。
**判据测试**：半年后如果有人问"为什么当初这么选"，答不上来才需要 ADR。

**R5 · CI 文档校验**（Docs as Code 的落地）
脚本扫描常青文档中引用的文件路径、API 路径、表名、配置项，验证其真实存在。
**当前缺 CI，这条是文档不腐烂的唯一可靠机制，优先级 P1。**

---

## 6. 执行清单

### Phase A · 归档（纯移动，零信息损失）

| 源 | 目标 |
|---|---|
| `odr/*.md`（64 个，756KB） | `05-archive/odr/` |
| `TASKS.md`（217KB） | `05-archive/TASKS-history.md` |
| `VISION.md`（74KB） | `05-archive/VISION-full.md` |
| `archive/RESEARCH-equitydeep-legacy.md` | `05-archive/` |
| `adr/adr-009,010,012,021` | `05-archive/superseded-adr/` |
| `archive/benchmark-results.md` | `05-archive/` |
| `archive/tasks-phase-2.md` | `05-archive/` |
| `guides/archive/migration-phase3-to-phase4.md` | `05-archive/` |
| `archive/reports-2026-Q2` `archive/plans-2026-Q3` `archive/research-2026-Q2` | `05-archive/` 下同名目录 |
| `archive/IMPLEMENTATION_PLAN.md` `NEXT_STEPS.md` `README.md` | `05-archive/` |

### Phase B · 结构搭建

| 动作 | 说明 |
|---|---|
| 新建 `docs/README.md` | 导航 + 五条治理规则 |
| 移动 `PRODUCT/ROADMAP/ARCHITECTURE/SPEC/TEST` | 进对应新目录 |
| 移动 `design/` → `02-architecture/design/` | 保留 backtest-engine-design 等 |
| 移动 `test-cases/` → `03-guides/test-cases/` | |
| `TEST.md` → `03-guides/testing.md` | |
| 移动 `AGENTS_TEMPLATE.md` | → 仓库根（它是模板不是文档） |

### Phase C · 重写（内容变更，需单独确认）

| 文档 | 动作 | 依据 |
|---|---|---|
| `PRODUCT.md` | **重写** | 新定位：AI 实验员 + 红队 + 异步审阅台 |
| `VISION.md` | **重写**（≤2 页） | 从 74KB 提炼设计原则 |
| `ARCHITECTURE.md` | **重写**（≤5 页） | 三层模型：AI 编排 / 能力 / 数据 |
| `adr-022` | **重写** | EquityDeep 从"工作面"降为"数据底座 + 研究洞察" |
| `TASKS.md` | **重建** | 只放未完成项 |

### 回滚

全程使用 `git mv` / `git rm`，任何一步可用 `git reset --hard HEAD` 或 `git revert` 回滚。
执行前会 `git status` 确认工作区干净。

---

## 7. 后续：三份新文档

| 文档 | 位置 | 内容 |
|---|---|---|
| 设计文档 | `01-product/PRODUCT.md` + `02-architecture/design/experiment-loop.md` | AI 实验员 / 红队 / 审阅台 / 校准 |
| 技术文档 | `02-architecture/design/experiment-loop-tech.md` | experiments 表 / 控制器接口 / 三层接口 |
| 实现 tasks | `04-current/TASKS.md` | 分阶段任务清单 |

---

_执行完成后本文件移入 `05-archive/`。_
