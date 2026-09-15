# ODR-054: P1 收口 — EQD-P3-2 文档漂移 8 项（DR-1~DR-8）一致性复核

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Audit
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（本项为阶段 P1「底座契约」出口余项）
> **Supersedes**: —
> **Related ODRs**: [ODR-047](odr-047-equitydeep-integration-audit.md)（DR-1~DR-8 的来源；本记录为其收口校验且不修改原记录）, [ODR-049](odr-049-adr-022-downstream-consistency.md)（文档体系完整性审计，同类方法论）, [ODR-050](odr-050-p1-base-contract-landing.md) / [ODR-051](odr-051-l0-1-single-ingest-entry.md) / [ODR-052](odr-052-p1-contract-freeze-and-spot-check.md) / [ODR-053](odr-053-p3-fundamentals-detail-table.md)（阶段 P1/P3 其余项，本轮复核的「后续落地」背景）
> **Author**: AI Assistant

---

## Context

### 触发条件

阶段 P1「底座契约」仅余一项 `EQD-P3-2`（🔵）—— [ODR-047](odr-047-equitydeep-integration-audit.md) D5 登记的 **8 处文档漂移（DR-1~DR-8）**。这批修复在 ODR-047 审计当时逐条完成，但此后又经历了 4 轮落地（ODR-050 / 051 / 052 / 053，含迁移编号统一、活跃表数 32→39、ODR-048~053 新增）。**「当初修好了」不等于「现在还一致」**：计数类声明（ADR/ODR 条数、builtin tool 数、表数）会随每次新增记录而静默陈旧。故本项不是复述修复，而是**统一复核**。

### 复核方法

对 8 项逐条做「文档声明 ↔ 物理事实」双向取证：

- **物理事实**取自仓库本身 —— `Glob`/`Grep` 列文件、`Read` 逐行读取、`Name()` 实现逐个计数；
- **文档声明**取自 `docs/ARCHITECTURE.md` / `docs/ADR.md` / `AGENTS.md` / `docs/ROADMAP.md` / `docs/VISION.md` / `docs/design/index.md` / `docs/TASKS.md`；
- 判定标准：声明值 = 实测值，且声明**内部可验算**（拆分之和 = 总数）。

### 复核矩阵（8 项）

| DR | 原缺陷位置 | 原修复动作 | 本次复核取证（实测） | 结论 |
|----|-----------|-----------|---------------------|------|
| **DR-1** | `ARCHITECTURE.md` 称「8 个 builtin tool」 | 改为 18 个并逐一列名 | `pkg/tools/builtin/*.go` 实测 `Name()` 实现 **18 个**，与 L982 所列 18 个名字逐一对应（backtest.run / factor.compute / factor.evaluate / validate_factor / compute_factor_ic / list_factors / list_strategies / save_factor / save_strategy / get_strategy_lineage / walk_forward_validate / get_market_regime / summarize_backtest / data.ohlcv / data.stocks / data.fundamentals / strategy.list / strategy.get） | ✅ **未回退** |
| **DR-2** | Generate / Validate / Evolve Agent + Gene Pool 误标「规划中」 | 改标「已实现」+ ODR-046 deprecated 注记 | `pkg/ai/agents/{generate,validate,evolve}.go` 与 `pkg/ai/gene_pool/` **均存在**；`ARCHITECTURE.md` L1113-1115 标 ✅ 已实现（ODR-046 后 deprecated） | ✅ **未回退** |
| **DR-3** | `ADR.md` 尾注「ODR 累计 42 条」（缺 ODR-043~046） | 补齐计数 | 尾注「ODR 累计 53 条」与 `docs/odr/` 实测 **53 个文件**（odr-001~053）一致 | ⚠️ **残留**（同两行的内部枚举算术不自洽，见下） |
| **DR-4** | `AGENTS.md` §1/§13/§14 多处称 ODR 42 / ADR 15 | 补齐计数 | L123「共 22 条 ADR：ADR-001~022」与 `docs/adr/` 实测 **22 个文件**一致；L166 `ADR-001~022` 一致 | ⚠️ **残留**（L167 陈旧，见下） |
| **DR-5** | `ROADMAP.md` Phase 4 标「PROPOSED — Awaiting Approval」 | 改标 IN PROGRESS + 修正注 | L199 `Status: Phase 4 IN PROGRESS — 大部分已交付`；L201 DR-5 修正注完整（含 expression DSL / gene_pool / search TPE+GA / evolution / drift / metrics / 18 MCP 工具已实现） | ✅ **未回退** |
| **DR-6** | `VISION.md` L607 称 `optimize.go` ✅ 与 L395 自相矛盾 | 统一为 NOT IMPLEMENTED | L432 与 L644 **两处均标 NOT IMPLEMENTED**（「文件从未创建」，TPE/遗传算法在 `pkg/ai/search/`）；全文件已无 ✅ 声明 | ✅ **未回退**（自相矛盾消失） |
| **DR-7** | `fundamentals` / `stock_fundamentals` 字段重叠无任务登记 | 登记为 `EQD-P3-1` | `TASKS.md` L1804 存在 `EQD-P3-1`（目标 `docs/migrations/025_equitydeep_field_consolidation.sql`，状态 ⬜）；`ARCHITECTURE.md` L467 备注指向 ODR-047 DR-7 | ✅ **已登记** |
| **DR-8** | `docs/design/` 目录语义冲突 | 迁移至 `docs/design/equitydeep/` | 子目录存在（Product + Technical 2 文件）；`docs/design/` 根下已无 EquityDeep 规格；`design/index.md` L8 已加「目录语义边界」声明；全仓 7 处引用（ADR-021/022、RESEARCH、PRODUCT、TASKS、ODR-047/048）路径一致 | ✅ **未回退** |

### 复核发现：3 处残留漂移（均由后续落地引起）

| 编号 | 位置 | 现状 | 实测/验算 | 应为 |
|------|------|------|----------|------|
| **DR-3-R1** | `docs/ADR.md` L144 ADR 拆分 | `架构 17 (含 ADR-022) + 业务 1 (ADR-017) + 测试 1 (ADR-018) + 服务合并 1 (ADR-019) + 重构 1 (ADR-020)` | 17 + 1 + 1 + 1 + 1 = **21** ≠ 声明的 22 —— ADR-021 未落入任何一类 | 显式列出各类区间并补 ADR-021（研究层） |
| **DR-3-R2** | `docs/ADR.md` L145 Implementation 枚举 | `Implementation 30 (ODR-016~042 + ODR-050~053)` | 27 + 4 = **31** ≠ 声明的 30 —— ODR-022 已被索引表 L65 归入 **Refactor**，却仍落在 `016~042` 区间内 | `ODR-016~021 + ODR-023~042 + ODR-050~053` = 6 + 20 + 4 = 30 |
| **DR-4-R1** | `AGENTS.md` L167 目录树 | `odr/ # 运营决策记录 (ODR-001~049)` | `docs/odr/` 实测 53 个文件，本轮新增 ODR-054 后为 54 | `ODR-001~054` |

> 三处均为**同一类根因**：把「计数」写成**枚举清单**却又未随新增同步。DR-3-R2 更暴露了「分类计数」与「区间枚举」两种口径混用 —— 一个 ODR 同时属于「分类」与「区间」。

---

## Decision

### 1. 修复 3 处残留漂移（本轮）

- **DR-3-R1 / DR-3-R2**：重写 `docs/ADR.md` 尾注两行，使 ADR/ODR 拆分**枚举完整且可验算**（拆分之和 = 声明总数）：

  - ADR：`架构 17 (ADR-001~016 + ADR-022) + 业务 1 (ADR-017) + 测试 1 (ADR-018) + 服务合并 1 (ADR-019) + 重构 1 (ADR-020) + 研究层 1 (ADR-021)` = **22**；并明确取代关系（ADR-014 由 ADR-020 §6 取代 / ADR-021 由 ADR-022 取代）。
  - ODR：`Cleanup 4 + Audit 10 + Migration 7 + Process 1 + Implementation 30 + Refactor 2` = **54**；Implementation 改为 `ODR-016~021 + ODR-023~042 + ODR-050~053`。

- **DR-4-R1**：`AGENTS.md` L167 目录树改为 `ODR-001~054`（与 `docs/adr/` 22 / `docs/odr/` 54 同步）。

### 2. 复核结论作为 DR-1~DR-8 的最终状态

DR-1 / DR-2 / DR-5 / DR-6 / DR-8 **未回退**；DR-3 / DR-4 **本轮回填残留**后一致；DR-7 属**结构性欠债**（需迁移 SQL），不在文档修复范围内，继续由 `EQD-P3-1` 承接。

### 3. 不修改 ODR-047

遵循「后续实施情况记入新 ODR，不在原记录追加」的约定 —— ODR-047 的 DR 表保持审计当时的状态，本记录为其**收口校验**。

---

## Consequences

### 正面

| 项 | 说明 |
|----|------|
| 阶段 P1 可正式关闭 | `EQD-P3-2` 是 P1「底座契约」最后一项（🔵）；本记录落盘后 P1 全部 ✅，出口条件达成 |
| 消除静默陈旧源头 | 三处漂移的共同根因是「枚举式计数」；改为可验算形式后，下一轮落地若漏改会立即暴露为「和 ≠ 总数」 |
| 复核矩阵可复用 | 8 项「声明 ↔ 实测」对照表可直接作为后续同类复核的模板 |
| 零代码/零 DDL | 纯文档修复，无运行时影响 |

### 负面 / 代价

| 项 | 说明 |
|----|------|
| 计数漂移无法自动拦截 | 本轮仍靠人工取证；未引入「文档计数 vs 文件数」的自动校验脚本（同 ODR-052 的 DDL 一致性缺口，本轮**未**给出同类工程化补丁） |
| ADR-021 分类为新增口径 | 「研究层 1」是本次为凑齐可验算拆分而引入的表述，ODR-047/048 未定义过该分类名 |
| 复核本身不产生能力 | 8 项中 5 项仅确认未回退，属「防守型」工作 |

### 风险

| 风险 | 级别 | 应对 |
|------|------|------|
| 下轮新增 ODR 后尾注再次陈旧 | 中 | 尾注已改为可验算形式；建议后续把「更新 ADR.md 尾注」纳入新增 ODR 的固定动作（ODR-050~053 均已如此执行，本轮为回填历史欠账） |
| `AGENTS.md` 目录树清单仍为「区间式」 | 低 | 区间式（`ODR-001~054`）比枚举式更耐陈旧，但每次新增仍需改；已记录为已知维护成本 |
| DR-7 长期滞留 | 中 | 已登记 `EQD-P3-1`，属阶段 P3 第三项，不因 P1 关闭而丢失 |

---

## Artifacts

| 动作 | 文件 | 说明 |
|------|------|------|
| **新建** | `docs/odr/odr-054-dr-reverification.md` | 本记录 |
| **修改** | `docs/ADR.md` | 版本 3.10.0 → 3.10.1；ODR 索引新增 ODR-054 行；尾注修复 DR-3-R1 / DR-3-R2（ADR 累计 22 拆分补 ADR-021；ODR 累计 53 → 54、Audit 9 → 10、Implementation 枚举改为 `016~021 + 023~042 + 050~053`）；新增本次状态变更行 |
| **修改** | `AGENTS.md` | 修复 DR-4-R1（L167 `ODR-001~049` → `ODR-001~054`）；§1 当前版本行移除「除 EQD-P3-2 复核」（P1 已全部关闭） |
| **修改** | `docs/TASKS.md` | `EQD-P3-2` 状态 进行中 → ✅；阶段 P1 标题 进行中 → ✅；Sprint 8 统计 `9/1/7` → `9/0/8`、总计 `11/1/220` → `11/0/221`；版本 3.28.0 → 3.29.0；新增变更日志段；更新 L691 / L1819 进展注 |

---

## Metrics

| 指标 | 值 |
|------|-----|
| 复核项数 | 8（DR-1~DR-8） |
| 未回退项 | 5（DR-1 / DR-2 / DR-5 / DR-6 / DR-8） |
| 已登记项 | 1（DR-7 → `EQD-P3-1`） |
| 残留漂移修复项 | 3（DR-3-R1 / DR-3-R2 / DR-4-R1） |
| 文档声明 ↔ 实测一致率（复核前） | 8 项中 6 项一致（DR-3 / DR-4 各 1 处内部不一致） |
| 文档声明 ↔ 实测一致率（复核后） | 8 项中 8 项一致（DR-7 由任务承接） |
| 代码 / DDL 改动 | 0 / 0 |
| ODR 累计 | 54 |
| 阶段 P1 完成度 | 4/4（`EQD-P0-1` / `EQD-P0-2` / `EQD-P3-2` / `L0-1`~`L0-4` 全部 ✅） |

---

## 未做项

| 项 | 说明 |
|----|------|
| 计数一致性自动校验 | 未引入「文档声明 vs 实际文件数」的脚本/测试（类比 ODR-053 为 DDL 三副本引入的 parity 测试）；本轮仅人工取证 + 可验算改写 |
| DR-7 `fundamentals` / `stock_fundamentals` 合并 | 结构性欠债，需独立迁移 `025_*.sql` → `EQD-P3-1`（阶段 P3） |
| ODR-047 原记录回填 | 按约定不修改；本记录为其收口校验 |
| 历史归档文档的同类陈旧 | `docs/archive/**` 内的区间式清单（如 `ODR-001~004`）为历史快照，按归档语义**不改** |
| 其他计数点位全量扫描 | 本轮按 DR-1~DR-8 划定的点位取证，未做全仓计数点穷举 |

---

## Lessons Learned

1. **「修复完成」与「复核通过」是两件事** —— 8 项修复在 ODR-047 当时逐条 ✅，但若无独立复核，就无法区分「未回退」与「无人再看」；本轮 4 轮后续落地（ODR-048~053）期间，DR-3/DR-4 已静默陈旧。
2. **计数必须写成可验算形式** —— 「拆分之和 = 总数」比「一个总数」更能抵抗陈旧；DR-3-R1 缺一项、DR-3-R2 多一项，若当初写成枚举和校验即不会被漏掉。
3. **「分类计数」与「区间枚举」是两种口径，混用即漂移源** —— ODR-022 同时落在 Refactor（分类）与 `016~042`（区间）里；此类混用不会立刻报错，只会在下次改数时暴露。
4. **枚举式清单是最易陈旧的文档形态** —— `odr/ # (ODR-001~049)` 这种遍历式描述，每新增一条记录就失效一次；它恰恰落在「新增记录时不会被想到」的位置。
5. **目录迁移类修复回退风险低** —— DR-8 之所以稳，是因为引用是全仓显式写死的（7 处路径），任何一处未改都会在跳转时暴露；相比之下计数类声明没有这类「使用时自证」机制。
6. **收口项应主动认领而非被动等待** —— `EQD-P3-2` 标 🔵 的期间，事实上的漂移已在发生；把「复核」当成有工期的交付项，而不是「审计的收尾动作」，才能阻断积累。

---

_本记录为 ODR-047 DR-1~DR-8 的收口校验。阶段 P1「底座契约」至此全部关闭；后续阶段（P2 工作面 / P3 计算面 / P4 飞轮 / P5 横截面 v2）的实施情况应记入新 ODR，不在本文件追加。_