# ODR-048: 顶层产品重定义 — 统一研究平台

> **Status**: Accepted
> **Date**: 2026-09-15
> **Category**: Refactor
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（本操作的架构决策）, [ADR-021](../superseded-adr/adr-021-equitydeep-research-layer.md)（被取代）
> **Supersedes**: —（本 ODR 记录一次重构，不取代其他 ODR）
> **Related ODRs**: [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计）, [ODR-046](odr-046-hermes-agent-integration-decision.md)（Hermes Agent）
> **Author**: AI Assistant

---

## Context

### 触发条件

1. **起点**：[ODR-047](odr-047-equitydeep-integration-audit.md) + [ADR-021](../superseded-adr/adr-021-equitydeep-research-layer.md) 完成了 EquityDeep 与 Quant Lab 的定位复核，结论为「横截面层与纵向层正交 → **补充非改变**」，并决定"两仓独立、只锁数据契约"。
2. **用户反馈（2026-09-15）**：该结论在架构上自洽，但未回答产品层的问题。用户提出四点修正：
   - 战略方向应为 **Quant Lab 做 infra（底座）、EquityDeep 做高层产品（或高层产品之一）**；
   - 当前状态：**Quant Lab 尚未完成、EquityDeep 处于启动状态**，因此**需要先定义顶层产品，再分步骤执行**；
   - 允许修改 EquityDeep（**可以有 DB、可以有 Docker**）；
   - **不希望重复存储数据**；
   - 目标是把两者"**有机地结合、揉合成一个完整的产品**"。

### 问题本质

ADR-021 的"两仓独立 + 契约桥"方案存在三个未解决问题：

| # | 问题 | 证据 |
|---|---|---|
| 1 | **无法回答"合起来是什么"** | ADR-021 只定义了"两层如何正交"，没有顶层产品定义文档 |
| 2 | **同一份数据双写** | 财报同时存于 EquityDeep `snapshots/*.json` 与 Quant Lab `fundamentals_detail` 表 |
| 3 | **护城河确权不清** | 价值到底在 Quant Lab 的 Go 性能，还是在 EquityDeep 的研究资产上，方案中无结论 |

---

## Decision

**将顶层产品重新定义为"统一研究平台"：单一数据面 + 两个对等工作面 + 一个飞轮闭环。**

### 1. 层级重构

| 实体 | 变更前（ADR-021） | 变更后（ADR-022） |
|---|---|---|
| Quant Lab | **产品**（横截面量化研究平台） | **共享底座**（L0 数据面 + L1 计算面 + L2 编排面） |
| EquityDeep | 补充层（独立工具，无 DB/无 Docker） | **工作面 1**（纵向深研，首要高层工作面） |
| 横截面能力 | Quant Lab 本体 | **工作面 2**（与工作面 1 对等，本期纳入规划） |
| 两者关系 | 正交互补的两个层 | **一个产品的两个工作面**，共享同一数据与证据链 |

### 2. 不重复存储的机制（本决策的核心）

**按数据性质分区，每一类数据物理上只有一个权威位置**：

| 类别 | 唯一权威位置 | 其他侧副本 |
|---|---|---|
| A 原始源响应 | PG `ingest.raw`（`content_hash` 唯一键） | ❌ 仅持 `content_hash` |
| B 规范化数据 | PG `market.*` | ❌ 只读证据 API |
| C 派生计算结果 | PG `quant.*` + Redis `factor_cache` | ❌ 只读计算 API |
| D 研究叙事 | **Vault markdown**（事实源） | — 本身即事实源 |
| E 研究结构化状态 | PG `research.*`（markdown 的**确定性投影**，可 DROP 重建） | 权威在 D |

判据：**可重建的数据物理唯一于 PG（EquityDeep 零副本）；不可重建的判断物理唯一于 markdown；两者之间只有可重建投影，没有双写。**

### 3. EquityDeep 形态变更（用户明确允许）

- **DB**：接入共享 PostgreSQL 的 `research` schema（**不新建独立实例**）；
- **Docker**：作为 `equitydeep-research` worker 容器加入 docker-compose；
- **取数**：严格单一入口 —— 经 L0 只读证据 API，不再自抓 akshare（adapter 归入 L0）；
- **原始快照**：vault 内 `snapshots/*.json` → PG `ingest.raw`，vault 只留 `{content_hash, pointer}`；
- **不变**：Python 3.11 / 7-stage 固定流程 / 逐数溯源 / 三硬承诺 / 非目标红线 / vault markdown 作为叙事事实源。

### 4. 证据服务升级为平台能力

Citation 从"文件路径 + 模糊字符串"升级为**不可变内容坐标** `{source, dataset, key, as_of, content_hash}`，`GET /api/evidence/{content_hash}` 返回唯一原始记录。

**附带效果**：ODR-047 发现的 P0 缺陷（回查脚本全局子串匹配 → 假阳性）在架构层面被消除 —— 校验对象从"文本"变为"声明（citation 元组）+ JSON Pointer 精确解析"。

### 5. 执行路线（先定义，再分步）

| 阶段 | 内容 | 出口判据 |
|---|---|---|
| **P0** | 顶层定义：`PRODUCT.md` + `ADR-022` + 本 ODR | 顶层文档体系自洽 |
| **P1** | 底座契约：单一摄取入口 + `ingest.raw` + Evidence API + `contracts/` + `research` schema DDL + 抽检脚本泛化 | 任意组件可经 Evidence API 取到唯一原始记录 |
| **P2** | 工作面 1 跑通：EquityDeep 接 `research` schema + 容器化 + 走 Evidence API + 回查脚本修复 → M1 通过 | M1 通过 + 快照零本地副本 |
| **P3** | 计算面补齐：`fundamentals_detail` + 5 纵向因子 + PIT | 纵向结论可转因子 |
| **P4** | 飞轮打通：疑点 → 假设 → 因子 → 回测 → 回流档案 | ≥1 圈端到端可复现 |
| **P5** | 横截面工作面 v2：存量对齐新架构 | 无平行数据路径 |

---

## Consequences

### 正面

- 产品定位从"两个正交层"升级为"一个产品、两个对等工作面"，可对外表达、可规划、可分步执行；
- 每类数据只有一个权威位置，口径漂移在结构上被排除（直接回应用户"不重复存储"约束）；
- 证据溯源升级为平台级能力，跨工作面共享；
- 护城河确权为"研究资产累积"（档案 + 因子 + 对应关系），不再押注单点技术；
- EquityDeep 获得统一部署与运维面（容器化 + 共享 PG），不再产生第二个数据孤岛。

### 负面 / 代价

- EquityDeep 从"零依赖本地工具"变为"依赖平台在线的研究 worker"，需提供离线降级路径（`equitydeep evidence --cache`）；
- 需新建 `ingest.raw` 与 `research` schema，P1 阶段有迁移与 ETL 改造工作量；
- vault 不再自带原始快照，档案脱离平台后无法自证数字（未决问题 Q-1）；
- 跨仓契约同步风险仍在（`contracts/` 版本化 + 契约测试缓解）。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 飞轮闭环无实际转数 | **高** | 成功指标"≥3 个纵向结论完成 IC 评估"作为产品验证门禁 |
| `ingest.raw` 存储膨胀 | 中 | P1 阶段定冷热分层策略（PRODUCT.md Q-2） |
| ADR-021 既有引用大面积失效 | 低 | ADR-021 保留并标记 Superseded；`archive/RESEARCH-equitydeep-legacy.md` 降级为"工作面 1 详案"并加 ADR-022 待修订说明块（已完成） |

---

## Artifacts

| 动作 | 文件 |
|---|---|
| **新建** | `docs/PRODUCT.md` — 顶层产品定义（canonical，与 VISION.md 同级） |
| **新建** | `docs/archive/superseded-adr/adr-022-unified-research-platform.md` — 架构决策 |
| **新建** | `docs/archive/odr/odr-048-top-level-product-redefinition.md` — 本文件 |
| **修改** | `docs/archive/superseded-adr/adr-021-equitydeep-research-layer.md` — Status → Superseded by ADR-022 + 取代说明 |
| **修改** | `docs/ADR.md` — 新增 ADR-022 / ODR-048 索引行；ADR-021 状态更新；版本 3.4.0 → 3.5.0；变更日志 |
| **修改** | `AGENTS.md` — 产品定位 / 文档导航 / 当前状态 / 已知问题 |
| **修改** | `docs/archive/RESEARCH-equitydeep-legacy.md` — 降级为"工作面 1 详案"（v0.2.0）；头部加 ADR-022 待修订说明块（6 项），§1/§2/§3 有效内容保留，顶层定位改由 PRODUCT.md 承担 |
| **修改** | `docs/TASKS.md` — Sprint 8 按 ADR-022 执行路线 P1~P5 重排（收尾状态列） |
| **修改** | `docs/design/equitydeep/EquityDeep_Product_Specification.md` — v1.0 → v1.1（DB / Docker / 取数 / 快照归属 + 溯源改 content_hash） |
| **修改** | `docs/design/equitydeep/EquityDeep_Technical_Specification.md` — v1.0 → v1.1（§1.2 存储决策 / §2.2 `ingest.raw` / §3.1 回查改 citation+JSON Pointer / §4 选型 / §6 结构 / §8 风险 / §9 取数路径） |
| **修改** | `docs/VISION.md` — 2.0.0：新增 §1.0 顶层定位（底座 + 双工作面），Appendix 降级为"设计原则与特性清单" |
| **修改** | `docs/ARCHITECTURE.md` — 2.3.0：四层架构（L0-L3）+ `ingest.raw` / `research` schema + ADR-021 整节重写为"统一研究平台架构" |

---

## Metrics

| 指标 | 值 |
|---|---|
| 新建文档 | 3（PRODUCT.md / ADR-022 / ODR-048） |
| 修改文档 | 10（ADR-021 / ADR.md / AGENTS.md / archive/RESEARCH-equitydeep-legacy.md / TASKS.md / PRODUCT Spec / TECHNICAL Spec / VISION.md / ARCHITECTURE.md + 本文件） |
| ADR 累计 | 22（其中 2 条 Superseded：ADR-014 / ADR-021） |
| ODR 累计 | 48 |
| 架构层次 | 4 层（L0 数据 / L1 计算 / L2 编排 / L3 体验） |
| 数据权威位置 | 5 类，每类 1 个（消除 ADR-021 方案中的 1 处双写：财报快照） |

---

## Lessons Learned

1. **"正交"不等于"一个产品"** —— 两个研究对象正交只说明它们不冲突，不说明它们构成产品。判断"一个产品"的标准应是**是否存在闭环的价值流动**（本方案中的飞轮），而非"是否互补"。
2. **"不重复存储"要靠物理归属，不能靠约定** —— ADR-021 也声明了"单一写者"，但因为没按数据性质分区，财报快照仍然双写。应先问"这类数据能否重建"，再决定权威位置。
3. **上游约束要区分"产品红线"与"实现偏好"** —— EquityDeep 的"无 DB / 无 Docker"是**实现偏好**（服务于"简洁"），而"逐数溯源 / 固定流程 / 三硬承诺"才是**产品红线**。ADR-021 把两者一起继承，导致可优化项被当作不可动项。
4. **顶层定义缺失会让下层方案反复返工** —— 缺少 `PRODUCT.md` 时，ADR/RESEARCH 只能回答局部问题（"两层如何正交"），每次用户提出产品层诉求都会触发架构返工。
5. **缺陷修复要考虑"架构级消除"** —— ODR-047 的 P0 假阳性缺陷若只打补丁（改子串匹配为 JSON Pointer），问题仍会在下次证据格式变更时复现；升级为内容坐标证据后，该类缺陷在机制上不可能发生。

---

_本记录为 2026-09-15 顶层产品重定义操作。后续阶段（P1~P5）的完成情况应记入新的 ODR，不在本文件追加。_