# ODR-006: Document Consolidation & TASKS.md Creation

> **Status**: Completed
> **Date**: 2026-04-11
> **Category**: Cleanup + Migration
> **Related ADRs**: None
> **Supersedes**: None

## Context

项目存在以下文档管理问题：

1. **散碎文档过多**：docs/ 目录下有多个独立的 .md 文件，内容分散且部分已过时
2. **缺少统一任务追踪**：CODE_REVIEW_REPORT.md 和 NEXT_STEPS.md 都包含可执行任务，但分散在不同文档中
3. **报告类文档膨胀**：存在多个 `*_REPORT.md` 文件，违反 ODR 规则（Rule 3: No Report Files）

### 散碎文档清单

| 文件 | 内容 | 问题 |
|------|------|------|
| CACHE.md | Redis 缓存设计 | 应整合到 ARCHITECTURE.md |
| CODE_REVIEW_REPORT.md | 代码审查发现 | Report 文件，任务应迁移到 TASKS.md |
| DOC_MGMT_RESEARCH.md | 文档管理研究 | 研究已完成，结论已应用 |
| QUANT_SOFTWARE_DESIGN_ANALYSIS.md | 量化软件设计分析 | 研究性质文档 |
| REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md | ODR 治理设计 | 内容已整合到 AGENTS.md |

## Decision

### 1. 创建 TASKS.md（统一任务追踪）

创建 `docs/TASKS.md` 作为项目的**单一任务追踪源**，整合：
- CODE_REVIEW_REPORT.md 中的 47 个问题
- NEXT_STEPS.md 中的测试覆盖和文档同步任务

**任务分类**：
- P0 (6 项)：安全/数据完整性风险
- P1 (17 项)：代码质量/可维护性
- P2 (10 项)：架构/文档改进
- P3 (14 项)：持续改进

### 2. 整合 CACHE.md 到 ARCHITECTURE.md

将 Redis 缓存设计内容整合到 ARCHITECTURE.md 的"缓存设计 (Redis)"章节，包括：
- 两层 cache-aside 架构图
- Redis Key 设计表
- L1/L2 层说明

### 3. 归档散碎文档

创建 `docs/archive/research-2026-Q2/` 目录，归档以下文件：
- CACHE.md → 内容已整合
- CODE_REVIEW_REPORT.md → 任务已迁移
- DOC_MGMT_RESEARCH.md → 研究已完成
- QUANT_SOFTWARE_DESIGN_ANALYSIS.md → 研究性质
- REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md → 内容已整合

### 4. 保留必要文档

以下文档因特殊原因保留：
- FINAL_VERIFICATION_REPORT.md — 质量门禁 artifact（ODR Rule 3 例外）
- PHASE3-PLAN.md — 当前活跃计划
- phase-gate-reviews.md — Phase Gate 审查记录
- test-cases/T1_AND_ZHANGTING.md — 测试用例

## Consequences

### Positive
✅ 统一任务追踪源（TASKS.md），避免任务散落
✅ 减少文档碎片化，核心文档更聚焦
✅ 遵循 ODR 规则（No Report Files）
✅ ARCHITECTURE.md 内容更完整（含缓存设计）
✅ 归档目录结构清晰（按季度分类）

### Negative
⚠️ 需要团队习惯使用 TASKS.md 而非散碎报告
⚠️ 归档文件仍占用存储空间（但可追溯）

## Artifacts

### Files Created
- `docs/TASKS.md` — 统一任务追踪文档（~200 行）
- `docs/archive/research-2026-Q2/` — 新归档目录

### Files Modified
- `docs/ARCHITECTURE.md` — 新增"缓存设计 (Redis)"章节（+52 行）
- `docs/archive/README.md` — 更新目录结构

### Files Moved (Archived)
- `docs/CACHE.md` → `docs/archive/research-2026-Q2/`
- `docs/CODE_REVIEW_REPORT.md` → `docs/archive/research-2026-Q2/`
- `docs/DOC_MGMT_RESEARCH.md` → `docs/archive/research-2026-Q2/`
- `docs/QUANT_SOFTWARE_DESIGN_ANALYSIS.md` → `docs/archive/research-2026-Q2/`
- `docs/REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md` → `docs/archive/research-2026-Q2/`

## Metrics

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| docs/ 根目录 .md 文件数 | 17 | 12 | -5 (-29%) |
| 统一任务追踪文档 | 0 | 1 | +1 |
| 归档目录数 | 1 | 2 | +1 |
| ARCHITECTURE.md 行数 | ~370 | ~422 | +52 |
| TASKS.md 任务总数 | 0 | 47 | +47 |

## Lessons Learned

1. **任务追踪集中化**：散落在多个报告中的任务难以追踪，TASKS.md 解决了这个问题
2. **内容整合优先**：整合内容到核心文档比创建新文档更好
3. **按季度归档**：`research-2026-Q2` 命名清晰，便于历史追溯
4. **保留质量门禁**：FINAL_VERIFICATION_REPORT.md 作为例外保留，证明规则可以有合理例外
5. **更新 README**：归档后立即更新 archive/README.md，避免信息丢失

## Next Steps

1. **更新 AGENTS.md Document Index**：添加 TASKS.md 到文档导航表
2. **更新 ADR.md ODR Index**：添加 ODR-006 条目
3. **团队培训**：介绍 TASKS.md 的使用方式
4. **定期维护**：每次完成任务后更新 TASKS.md 状态

---
_ODR created as part of document consolidation operation_
_See: [TASKS.md](../TASKS.md) for the unified task tracker_
