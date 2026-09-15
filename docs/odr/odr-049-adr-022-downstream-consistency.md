# ODR-049: ADR-022 下游一致性收口 — 文档体系完整性审计

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Audit
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（本次收口的对象）, [ADR-021](../adr/adr-021-equitydeep-research-layer.md)（被取代，其引用为本次修复来源）
> **Supersedes**: —
> **Related ODRs**: [ODR-048](odr-048-top-level-product-redefinition.md)（顶层产品重定义，本次收口的前序动作）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计）
> **Author**: AI Assistant

---

## Context

### 触发条件

用户请求「检查当前文档体系是否完整」。此前 [ODR-048](odr-048-top-level-product-redefinition.md) 完成了顶层重定义（新建 `PRODUCT.md` / `ADR-022`，`ADR-021` 标记 Superseded），但**下游文档的 ADR-021 引用未同步收口**。

### 审计范围与方法

- 范围：`AGENTS.md`、`docs/**`（含 `ADR.md` / `TASKS.md` / `RESEARCH.md` / `ROADMAP.md` / `SPEC.md` / `PRODUCT.md` / `ARCHITECTURE.md` / `design/index.md` / `adr/`、`odr/`）。
- 方法：全文检索 ADR-021 引用 + 逐文件核对版本号/状态标注/交叉引用 + 校验 ODR 索引与实际文件一致性。

### 审计结论

**结构完整、索引齐全**（22 ADR / 48 ODR 与实际文件一一对应，`ADR.md` 索引无缺失），但 **ADR-021 → ADR-022 迁移未收口，存在 12 处一致性缺口**，分三级：

| 级别 | 定义 | 数量 |
|---|---|---|
| **P0** | 语义矛盾（文档自相矛盾或与 ADR-022 直接冲突） | 3 |
| **P1** | 过时引用（仍指向被取代的 ADR-021 或旧定位） | 8 |
| **P2** | 陈旧标注（声明与实际状态不符） | 1 组（4 处） |

---

## Decision

**全部修复（P0 + P1 + P2），按"以 ADR-022 为唯一顶层事实源"原则统一收口。**

### 缺口清单与修复动作

#### P0 — 语义矛盾

| # | 位置 | 问题 | 修复 |
|---|---|---|---|
| 1 | `AGENTS.md` §13 当前状态 | 仍写「EquityDeep 保持独立 Python 仓库，仅锁数据契约」，与 ADR-022「接入共享 PG + worker 容器」直接冲突 | 改写为「**统一研究平台**」条目，说明 Quant Lab 降维为共享底座、EquityDeep 升级为工作面 1、横截面升为工作面 2，并载明 DB/Docker 形态变更 |
| 2 | `AGENTS.md` 尾注 | 仅记录到 ODR-048，未反映本次收口 | 新增 `_ADR-022 Consistency Update_` 行，指向本 ODR |
| 3 | `docs/TASKS.md` L4 版本号 | 头部 `Version: 3.22.0` 与文内 changelog `3.23.0` 自相矛盾 | 统一为 `3.24.0` 并更新条目描述 |

#### P1 — 过时引用

| # | 位置 | 问题 | 修复 |
|---|---|---|---|
| 4 | `AGENTS.md` §10 文档分类表 | `adr-001 ~ adr-021`、`odr-001 ~ odr-047` | → `adr-001 ~ adr-022`、`odr-001 ~ odr-049` |
| 5 | `AGENTS.md` §14 已知问题 | 「基本面深度不足（ADR-021 契约 C1）」 | → 改引 ADR-022 计算面（原契约 C1） |
| 6 | `AGENTS.md` 关键文件速查 | 以 ADR-021 为 EquityDeep 主入口 | → 改为 PRODUCT.md（顶层）→ ADR-022（决策）→ RESEARCH.md（详案）链路 |
| 7 | `docs/TASKS.md` L610 / 文末文档表 | 任务来源仍标 ADR-021 | → 标注「后由 ADR-022 取代」；文档表补 PRODUCT.md / RESEARCH.md / ADR-022 三行 |
| 8 | `docs/RESEARCH.md` L328 / L372 | §3.3 契约原则与参考表仍引 ADR-021 | → 改引 ADR-022 §5 证据服务原则并注明取代关系 |
| 9 | `docs/design/index.md` L8 | EquityDeep 目录说明未标 ADR-021 Superseded | → 补 PRODUCT.md 上位定义 + ADR-022 决策 + 取代标注 |
| 10 | `docs/ROADMAP.md` L256 | Sprint 8 仍为「AI Factor Discovery」（与 TASKS.md 重排后的 Sprint 8 不符） | → 改为「统一研究平台落地 (ADR-022)」，指向 TASKS.md Sprint 8 P1~P5 |
| 11 | `docs/SPEC.md` 正文 | changelog 声明「新增 §Unified Research Platform + Evidence API 草案」，但正文实际缺失 | → 补写该 section（四层架构 / 双工作面 / A-E 数据归属 / Evidence API 草案） |

#### P2 — 陈旧标注

| # | 位置 | 问题 | 修复 |
|---|---|---|---|
| 12 | `adr-022` L174、`PRODUCT.md` L322 | 标注上游规格「待按 ADR-022 修订」，实际 ODR-048 已完成 v1.0 → v1.1 | → 改为「已按 ADR-022 修订至 v1.1」 |

### 附带确认项（非缺口，无需修改）

| 项 | 结论 |
|---|---|
| `docs/INDEX.md` 不存在 | 非缺陷 —— 文档索引由 `ADR.md` 承担（符合 AGENTS.md §11 导航约定） |
| `docs/migrations/` 与根 `migrations/` 疑似重复 | 历史遗留，属 P1 底座契约（ADR-022 阶段 P1）范围，本次不动；已由 TASKS.md 追踪 |
| `ARCHITECTURE.md` L452 引用 `migrations/012_*.sql` | 该迁移尚未创建（`fundamentals_detail` 属阶段 P3，任务 EQD-P1-1），为**前置引用**而非错误 |

---

## Consequences

### 正面

- ADR-021 的残留引用全部收口，文档体系在「顶层定义 → 架构决策 → 详案 → 任务」链路上自洽；
- 消除 3 处 P0 语义矛盾（尤其 `AGENTS.md` 中与 ADR-022 直接冲突的"独立仓库"表述），避免后续会话按过时上下文行动；
- `SPEC.md` changelog 与正文恢复一致，不再声明未落盘的内容；
- ODR 索引（`ADR.md`）与实际文件严格对齐，累计数可信。

### 负面 / 代价

- 本轮为纯文档收口，不改变任何实现状态；12 处修复均为引用/标注层，未触及架构；
- ADR-021 文件本身保留（标记 Superseded），历史引用仍会在归档文档中出现 —— 属预期。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 后续新增文档再次引入 ADR-021 引用 | 中 | AGENTS.md §10「Update-on-Change Triggers」已覆盖；`ADR.md` 索引中 ADR-021 行显式标注 Superseded |
| SPEC.md 新增 section 与 PRODUCT.md 后续演进漂移 | 低 | 该 section 已声明「冲突时以 PRODUCT.md / ADR-022 为准」，且仅摘录有约束力的部分 |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `docs/odr/odr-049-adr-022-downstream-consistency.md` | 本文件 |
| **修改** | `AGENTS.md` | 5 处：§10 索引范围 / §13 当前状态 / §14 已知问题 / 速查表 / 尾注 |
| **修改** | `docs/TASKS.md` | 4 处：版本号 3.22.0 → 3.24.0 / L610 来源标注 / 文末文档表 / 新增 changelog 节 |
| **修改** | `docs/RESEARCH.md` | 2 处：§3.3 契约原则引用 / 参考文档表 |
| **修改** | `docs/design/index.md` | 1 处：EquityDeep 目录语义边界说明 |
| **修改** | `docs/ROADMAP.md` | 1 处：Sprint 8 条目对齐统一研究平台 |
| **修改** | `docs/SPEC.md` | 2 处：头部元数据（v1.4.2 → v1.5.0）+ 正文新增 §Unified Research Platform |
| **修改** | `docs/adr/adr-022-unified-research-platform.md` | 1 处：上游规格标注 |
| **修改** | `docs/PRODUCT.md` | 1 处：上游规格标注 |
| **修改** | `docs/ADR.md` | 新增 ODR-049 索引行 + 版本 3.5.0 → 3.6.0 + 变更日志 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 审计文件数 | 12+（AGENTS.md + docs/ 全部核心文档 + ADR/ODR 目录） |
| 发现缺口 | 12 处（P0 3 / P1 8 / P2 1 组） |
| 修复缺口 | 12 / 12（100%） |
| 修改文档 | 9（AGENTS.md / TASKS.md / RESEARCH.md / SPEC.md / ROADMAP.md / PRODUCT.md / adr-022 / design/index.md / ADR.md） |
| 新建文档 | 1（本文件） |
| ADR 累计 | 22（其中 2 条 Superseded：ADR-014 / ADR-021） |
| ODR 累计 | 49 |
| 任务增减 | 0（TASKS.md 统计不变：233 项） |

---

## Lessons Learned

1. **Superseded 必须做"引用收口"，不能只改状态位** —— ODR-048 已把 ADR-021 标记 Superseded，但下游 8 处引用仍指向它。**"标记取代"与"完成取代"是两件事**；前者改一个文件，后者要全文检索并逐处确认语义是否仍成立。
2. **P0 的矛盾往往藏在"状态类"文档里** —— `AGENTS.md §13 当前状态` 这类文档最易被忽略，却恰恰是每个新会话的入口，过时表述会直接污染后续推理。
3. **changelog 是承诺，必须与正文同时落盘** —— `SPEC.md` 一度出现"changelog 声明新增 section、正文却没有"的情况。**changelog 与正文应在同一次编辑内完成**，避免产生"文档声称已做"的假象。
4. **版本号自相矛盾是低成本的信号** —— `TASKS.md` 头部 3.22.0 与文内 3.23.0 不一致，暴露了"改了一处、漏了另一处"的编辑模式；此类字段适合在收尾时统一 grep 校验。
5. **审计要区分"缺口"与"前置引用"** —— `ARCHITECTURE.md` 引用尚未创建的 `migrations/012_*.sql` 看似断链，实为阶段 P3 的前置声明。**审计结论应区分"错误"与"尚未发生的计划"**，否则会误改正确的文档。

---

_本记录为 2026-09-15 ADR-022 下游一致性收口操作。后续阶段（P1~P5）的实施情况应记入新的 ODR，不在本文件追加。_