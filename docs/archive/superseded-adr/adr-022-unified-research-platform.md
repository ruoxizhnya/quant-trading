# ADR-022: 统一研究平台 — 单一数据面 + 双工作面

> **Status**: Proposed（方向经用户确认 2026-09-15，待实施）
> **Date**: 2026-09-15
> **Category**: Architecture
> **Supersedes**: [ADR-021](adr-021-equitydeep-research-layer.md)（"两个独立代码库、只锁数据契约"的结论被本 ADR 取代）
> **Related ADRs**: [adr-015](../../adr/adr-015-ai-agent-architecture.md) (AI Agent 架构), [adr-016](../../adr/adr-016-multi-source-data-architecture.md) (多数据源), [adr-019](../../adr/adr-019-service-merge-ai-copilot.md) (服务合并), [adr-020](../../adr/adr-020-engine-decomposition.md) (Engine 分解)
> **Related ODRs**: [odr-046](../odr/odr-046-hermes-agent-integration-decision.md) (Hermes Agent), [odr-047](../odr/odr-047-equitydeep-integration-audit.md) (EquityDeep 集成审计), [odr-048](../odr/odr-048-top-level-product-redefinition.md) (本次重构记录)
> **Upstream**: [PRODUCT.md](../../PRODUCT.md)（顶层产品定义）
> **Author**: AI Assistant

---

## Context

[ADR-021](adr-021-equitydeep-research-layer.md) 确立了"横截面（Quant Lab）与纵向（EquityDeep）正交互补"，并据此决定：**两仓独立、只锁数据契约**（`contracts/`），EquityDeep 保持"文件系统即数据库、无 DB、无 Web、无 Docker"。

用户随后提出四点修正，暴露出 ADR-021 结论的不完整：

1. **产品层的诉求变了** —— 目标不是"两个正交的层"，而是"**一个完整产品**"。ADR-021 的"两仓独立"在架构上自洽，却无法回答"它俩合起来是什么"。
2. **不允许重复存储数据** —— ADR-021 方案下，同一份财报会同时存在于 EquityDeep 的 `snapshots/*.json` 与 Quant Lab 的 `fundamentals_detail` 表，属于双写。
3. **EquityDeep 可以有 DB / Docker** —— ADR-021 把"无 DB/无 Docker"当作 EquityDeep 的**设计前提**继承了下来；用户允许修改这一点。
4. **需要先定义顶层产品** —— 当前 Quant Lab 未完成、EquityDeep 未启动，缺少一个"顶层产品定义"来统一二者并指导分步执行。

同时，[ODR-047](../odr/odr-047-equitydeep-integration-audit.md) 的审计发现仍然有效（EquityDeep 回查脚本的 P0 假阳性缺陷、6 项加固建议），但它们的**修法**受本 ADR 影响：若证据坐标改为内容哈希 + JSON Pointer，P0 缺陷在架构层面被消除，而非仅打补丁。

本 ADR 决定：**这两个工作面如何构成一个产品，以及数据如何在其中不重复地流动。**

---

## Decision

### 1. 顶层定位：一个产品，两个对等工作面，一个共享底座

```
        ┌──────────────────────┐   ┌──────────────────────
        │  工作面 1：纵向深研    │   │  工作面 2：横截面选股  │
        │  1 股 × N 季度        │   │  N 股 × 1 因子        │
        │  产出：研究档案        │   │  产出：交易信号        │
        └──────────┬───────────┘   └──────────┬───────────┘
                   │      飞轮闭环 ①②↔③④      │
                   └───────────┬───────────────
                               ▼
        ┌──────────────────────────────────────────────────┐
        │  共享底座 = 原 Quant Lab 全部能力（降维为 L0-L2）   │
        └──────────────────────────────────────────────────┘
```

- **原 Quant Lab 的 Go 后端 + PG + 微服务 = 底座**（数据面 + 计算面 + 编排面）。它不再是"产品本身"，而是产品的基础设施。
- **EquityDeep = 工作面 1**（纵向深研），是本产品的**首要高层工作面**。
- **原横截面能力 = 工作面 2**，与工作面 1 **对等**，本期纳入规划（不降级、不搁置）。

> 这一条直接推翻 ADR-021 的"两仓独立"：**两仓仍然可以是两个代码库，但它们属于同一个产品的两个部分，共享同一个数据面**。

### 2. 按数据性质分区，实现"不重复存储"

这是本 ADR 的核心机制。**不重复存储不靠约定，靠物理归属**：

| 类别 | 内容 | 可重建？ | 唯一权威位置 | 其他侧副本 |
|---|---|---|---|---|
| A | 原始源响应（含中文原始字段名） | 可（重抓） | PG `ingest.raw`（`content_hash` 唯一键） | ❌ 仅持 `content_hash` |
| B | 规范化数据（OHLCV / 财报字段 / 日历 / 公司行为） | 可（从 A 重算） | PG `market.*` | ❌ 只读证据 API |
| C | 派生计算结果（因子 / 回测 / IC） | 可（从 B 重算） | PG `quant.*` + Redis `factor_cache` | ❌ 只读计算 API |
| D | 研究叙事（结论 / 疑点的自然语言正文） | **不可**（人的判断） | **Vault markdown**（事实源） | — 本身即事实源 |
| E | 研究结构化状态（结论/疑点字段 + citations） | 可（从 D 确定性投影） | PG `research.*`（**投影，非权威**） | 权威在 D；可 DROP 重建 |

**判据**："重复存储"= 同一份数据有两个都可写的位置并导致口径漂移。本方案中：

- A/B/C 物理唯一于 PG，工作面 1 **零副本**（运行期按需读 API，不做本地同步）；
- D 物理唯一于 vault markdown；
- E 是 D 的**确定性投影**（`equitydeep sync`，非 LLM 生成），单一写者、可丢弃、冲突时以 markdown 为准 —— 性质等同索引/物化视图，存在理由仅是可做跨层 SQL join。

### 3. 四层架构与单向依赖

```
L3 体验面   Obsidian Vault（工作面1） | Vue SPA（工作面2） | Hermes Agent（编排）
L2 编排面   Research Pipeline（纵向） | Research Engine（横截面） | MCP Tool Bridge
L1 计算面   因子引擎 | 回测引擎 | 验证门禁 L1-L5 | 风控·执行
L0 数据面   ingest.raw | market.* | quant.* | research.* | Evidence API   ← 唯一事实源
```

原则：**L0 唯一数据面 + 单一写者 + 单向依赖（L3→L2→L1→L0）+ 可重建性标注 + 契约优先**。

### 4. EquityDeep 形态变更（允许有 DB / Docker，但零数据副本）

| 项 | ADR-021（原） | ADR-022（本决策） |
|---|---|---|
| DB | 无（"文件系统即数据库"） | **接入共享 PostgreSQL 的 `research` schema**（不新建实例） |
| Docker | 无 | **`equitydeep-research` worker 容器**加入 docker-compose |
| 取数 | 自行调用 akshare | **严格单一入口**：经 L0 只读证据 API；akshare adapter 归入 L0 |
| 原始快照 | vault 内 `snapshots/*.json` | 迁至 PG `ingest.raw`；vault 只留 `{content_hash, pointer}` |
| 叙事 | `_profile.md` | **不变** —— 仍是事实源，Obsidian 仍是工作面 |
| 结构化状态 | `_profile.json` 镜像文件 | 升级为 PG `research.*` 投影（JSON 保留为导出格式） |
| **不变** | Python 3.11 / 7-stage 固定流程 / 逐数溯源 / 三硬承诺 / 非目标红线 | **全部保留**（产品价值本体） |

### 5. 证据服务升级为平台能力

Citation 从"文件路径 + 模糊字符串"升级为**不可变内容坐标**：

```
citation = { source, dataset, key, as_of, content_hash }
GET /api/evidence/{content_hash}  →  ingest.raw 中的唯一原始记录（不可变）
```

这条同时**在架构层面消除** ODR-047 发现的 P0 缺陷：回查脚本的校验对象从"文本子串"变为"声明（citation 元组）+ JSON Pointer 精确解析"，假阳性在机制上不可能发生。

### 6. 飞轮闭环是"一个产品"的判据

```
① 纵向深挖 → 产出可检验假设
② 横截面验证 → 假设变因子 → 全市场回测 → IC/Sharpe
③ 结果回流 → 修正/限制原结论（证伪也是收益）
④ 异常触发 → 横截面命中异常板块 → 触发纵向深挖 → 回到 ①
```

没有这个闭环，产品退化为两个独立工具；有了它，**护城河是积累起来的研究资产（档案 + 因子 + 对应关系）**，而非任何单点技术。

---

## Consequences

### 正面

| 收益 | 说明 |
|---|---|
| **产品定位清晰** | 从"两个正交层"升级为"一个产品、两个对等工作面"，可对外表达、可规划 |
| **数据主权单一** | 每类数据只有一个权威位置，口径漂移在结构上被排除 |
| **溯源能力平台化** | 内容哈希坐标使证据成为跨工作面共享的服务，而非某个工具的内部实现 |
| **护城河对准资产** | 护城河从"Go 性能"转为"研究档案 + 因子 + 二者对应关系"的累积 |
| **部署统一** | EquityDeep 容器化 + 共享 PG，运维面收敛 |
| **P0 缺陷架构级消除** | 回查脚本假阳性问题被证据坐标机制覆盖，而非打补丁 |
| **分步执行可行** | P0 定义 → P1 契约 → P2 工作面1 → P3 计算 → P4 飞轮 → P5 横截面对齐 |

### 负面 / 代价

| 代价 | 说明 | 缓解 |
|---|---|---|
| EquityDeep 形态改动大 | 从"无依赖文件工具"变为"依赖平台在线的研究 worker" | 提供 `equitydeep evidence --cache` 离线核验路径；中断平台时降级为只读研究 |
| 需新建 `ingest.raw` / `research` schema | 迁移与 ETL 改造工作量 | P1 契约冻结后先做 schema，不改存量表 |
| vault 不再自带原始快照 | 档案脱离平台后无法自证数字 | 未决问题 Q-1：保留"可移植导出"能力 |
| 两仓仍有同步风险 | 代码库分离，契约变更需双侧同步 | `contracts/` 版本化 + 契约测试（Q-3） |

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| `ingest.raw` 存储膨胀（原始 payload 长期保留） | 中 | Q-2 定冷热分层策略 |
| 飞轮闭环无实际转数（结论无法转化为有效因子） | **高** | 成功指标设为"≥3 个结论完成 IC 评估"，作为产品验证门禁 |
| EquityDeep 容器化后偏离"固定 7-stage"的简洁性 | 中 | 明确：容器化只是运行形态，不引入新架构复杂度 |
| ADR-021 的既有文档引用失效 | 低 | ADR-021 标记 Superseded 并保留；archive/RESEARCH-equitydeep-legacy.md 降级为"工作面 1 详案"待修订 |

---

## Alternatives Considered

| 方案 | 内容 | 为何不选 |
|---|---|---|
| **A. 维持 ADR-021（两仓独立 + 契约桥）** | 不改架构，只实现三桥 B1/B2/B3 | 无法回答"合起来是什么"；同一份财报双写；护城河确权不清 |
| **B. 完全合并为单仓单体** | EquityDeep 用 Go 重写，技术栈统一 | 摧毁 EquityDeep 的产品价值（Python 生态 / Obsidian 工作面 / 固定流程的简洁性）；重写成本 >> 收益 |
| **C. 保留 EquityDeep 独立数据源** | 允许 EquityDeep 自抓 akshare，Quant Lab 走 tushare，双源对账 | 违反"不重复存储"；同一指标两套口径；对账成本永久存在 |
| **D. 本方案：单一数据面 + 双工作面 + 内容坐标证据** | 见 Decision | —（被采纳） |

---

## References

| 文档 | 关系 |
|---|---|
| [PRODUCT.md](../../PRODUCT.md) | 顶层产品定义（本 ADR 的上游） |
| [archive/RESEARCH-equitydeep-legacy.md](../RESEARCH-equitydeep-legacy.md) | 工作面 1 详案（Product + Tech），部分内容受本 ADR 修订 |
| [ODR-047](../odr/odr-047-equitydeep-integration-audit.md) | 审计发现（加固项 1-6 仍有效，修法受本 ADR 影响） |
| [ODR-048](../odr/odr-048-top-level-product-redefinition.md) | 本次顶层重定义的操作记录 |
| [ADR-021](adr-021-equitydeep-research-layer.md) | Superseded by 本 ADR |
| [docs/design/equitydeep/](../design/equitydeep/) | EquityDeep 上游规格（已按本 ADR 修订至 v1.1） |

---

_决策日期: 2026-09-15 | 状态: Proposed（待实施验证）_