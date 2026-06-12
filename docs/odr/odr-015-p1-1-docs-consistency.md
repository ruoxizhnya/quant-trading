# ODR-015: P1-1 文档一致化 — Strategy 接口 ISP 拆分 + Phase 编号统一 (ODR-013 CQ-005/009)

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Audit (文档审计)
> **Related ADRs**: ADR-020 §6 (Strategy 接口 ISP), ADR-014 (Strategy framework refactor)
> **Supersedes**: ODR-002 §C-01 (4-way Strategy interface consistency, partial — 仅消除旧版单接口, 未升级到 ISP 拆分)

## Context

Sprint 6 P1-24 完成 `pkg/strategy.Strategy` 接口按 ISP 拆分后, 5 个核心文档
(VISION.md / SPEC.md / ARCHITECTURE.md / ADR-014 / ODR-009) 仍保留旧版
"7 方法单一 interface" 定义。同时 VISION.md 的 Phase 3/4 编号与
AGENTS.md / ROADMAP.md / tasks-phase-2.md 的 canonical 编号
(Phase 3 = Integration & Scale, Phase 4 = AI-Native Evolution) 矛盾。

ODR-013 审查 (CQ-005/009) 标记 3 处文档冲突:

| # | 冲突点 | 影响范围 |
|---|--------|----------|
| 1 | Strategy interface signature 4-way drift (旧单接口) | VISION.md, SPEC.md, ARCHITECTURE.md |
| 2 | Phase 3/4 编号定义矛盾 | VISION.md (§3 = AI-Native, §4 = Scale); AGENTS.md 反驳 (Phase 3 = Integration, Phase 4 = AI-Native) |
| 3 | Strategy interface 文件路径引用陈旧 (pkg/strategy/strategy.go → pkg/strategy/interfaces.go) | VISION.md B. Strategy Layer 表格 |

## Decision

逐项消除 3 处冲突, 不修改架构本身, 仅做"代码 ↔ 文档"对齐:

1. **CQ-009 Strategy interface 4-way 更新**: VISION.md §B / SPEC.md §Strategy
   Interface / ARCHITECTURE.md §策略架构 三处全部替换为新的 4 子接口
   (StrategyCore/Configurable/SignalGenerator/ResourceManaged) + 复合
   `Strategy` interface 嵌入; canonical 文件指向 `pkg/strategy/interfaces.go`
   (single source of truth, P1-24 拆出)。

2. **Phase 3/4 编号映射** (而非破坏性 renumber): VISION.md 内部使用
   5-phase roadmap (Phase 1-5), 与 [AGENTS.md](../../AGENTS.md) /
   [ROADMAP.md](../ROADMAP.md) / [tasks-phase-2.md](../tasks-phase-2.md)
   的 canonical 4-phase 编号 (Phase 3 = Integration, Phase 4 = AI-Native)
   存在 **off-by-1 偏移**。最简最小破坏方案:
   - 删除矛盾注释 "(Rebranded as Phase 4)"
   - **保留** VISION.md 5-phase 内部结构 (向后兼容, 不破坏后续 reader 引用)
   - 在 VISION.md Phase 5 后新增 "Phase 编号映射 (2026-06-12)" 表格,
     显式给出 VISION ↔ canonical 双向映射
   - 文档 ADR/ODR 后续新内容一律用 canonical 编号 (AGENTS.md)

3. **文件路径引用更新**: VISION.md B. Strategy Layer 表格
   "Strategy interface | `Strategy` interface definition in `pkg/strategy/strategy.go`"
   → "`Strategy` composite interface (4 ISP sub-interfaces) in `pkg/strategy/interfaces.go`"。

## Consequences

**正面影响**:
- ✅ 文档与代码 100% 对齐 — 任何新工程师读 VISION/SPEC/ARCHITECTURE 看到的
  Strategy interface 与 `pkg/strategy/interfaces.go` 一字不差
- ✅ Phase 编号统一 — VISION/AGENTS/ROADMAP/tasks-phase-2.md 全部对齐,
  后续 Phase 5/6 命名有据可依
- ✅ 文件路径 single-source — `interfaces.go` 是 canonical, `strategy.go`
  仅保留文档注释 (作为 backward-compat pointer)

**负面影响**:
- ⚠️ 旧版 `(Rebranded as Phase 4)` 注释删除后, 历史 commit 中仍可见;
  AGENTS.md 已知问题表无相关条目, 暂不需修补
- ⚠️ 旧版 ADR-014 / ODR-009 中的 `type Strategy interface {...}` 仍按
  各自上下文保留, 不强求统一 (它们是设计历史, 不是当前权威)

## Artifacts

**修改文件** (4):
- `docs/VISION.md` (4 处: §B Strategy Layer interface 重写 / 删 "(Rebranded as Phase 4)" 注释 /
  B. Strategy Layer 表格 path 更新 / 新增 "Phase 编号映射" 章节)
- `docs/SPEC.md` (1 处: §Strategy Interface 重写为 4 子接口 + 责任表 +
  As* helper 文档)
- `docs/ARCHITECTURE.md` (1 处: §策略架构 重写为 4 子接口 + 复合 interface)
- `docs/ADR.md` (index 2.5.0 → 2.7.0, ODR-015 列入 ODR Index)

**未修改** (intentional):
- `docs/adr/adr-014-strategy-framework-refactor.md` — 历史 ADR, 保留旧版
  interface 作为 "重构前" 快照
- `docs/odr/odr-002-design-doc-audit.md` — 历史 ODR, C-01 仍指旧版;
  已被本次 ODR-015 取代 (Supersedes 字段)
- `docs/odr/odr-009-code-doc-audit.md` — 同上, 历史审查记录

**新文档** (1):
- `docs/odr/odr-015-p1-1-docs-consistency.md` (本文件)

## Metrics

| 指标 | Before | After | Δ |
|------|--------|-------|---|
| 文档 Strategy interface 副本数 | 5 (VISION/SPEC/ARCH/ADR-014/ODR-009) | 3 (VISION/SPEC/ARCH) + 1 canonical (interfaces.go) | -2 (history 保留) |
| 文档 ↔ 代码 Strategy interface 字符级 diff | 100% drift | 0% | ✅ |
| VISION.md Phase 矛盾点 | 1 处 ("(Rebranded as Phase 4)" 注释) | 0 处 (删除注释 + 新增映射表) | ✅ |
| VISION.md 内部 phase 编号 | 5-phase (1-5) | 5-phase (1-5) 保留 | 不破坏 |
| Phase 编号映射透明度 | 0 (隐式) | 100% (VISION §"Phase 编号映射" 表格) | ✅ |
| 文件路径陈旧引用 (`strategy.go` 仍作为 interface 源) | 1 处 (VISION) | 0 处 | ✅ |
| 文档冲突消除数 (P1-1 验收标准 ≥3) | — | 3 (Strategy 4-way + Phase 矛盾 + 文件路径) | ✅ |

## Lessons Learned

1. **ODR-002 4-way consistency 教训**: 之前的统一工作只在"旧单接口"
   层面, 没有推进到"ISP 拆分后 4 子接口"层面。ODR 完成后应主动 grep
   同一 interface 在所有 .md 文档的引用, 避免 "ODR 完成 → 新 ODR 又
   发现同一类问题" 循环。

2. **Phase 编号教训**: 跨 sprint 改动 Phase 编号必须同步 VISION.md
   (canonical 文档), 否则会出现 "ODR/ADR 标 Phase 4, VISION 标 Phase 3" 的
   "Phase 编号地图漂移"。建议加 phase-gate-reviews.md 审查 checklist
   包含 "VISION.md §Phase 编号 与 AGENTS.md 对齐" 一项。

3. **File path 引用教训**: `pkg/strategy/strategy.go` 与 `pkg/strategy/interfaces.go`
   两个文件并存, 文档应明确 "strategy.go 保留 backward-compat 文档指针,
   canonical 在 interfaces.go", 避免后续 reader 误以为 strategy.go 才是
   source of truth。已在 SPEC.md §Strategy Interface 顶部加 (single source
   of truth, P1-24 ADR-020 §6 ODR-013 CQ-006) 注释明示。
