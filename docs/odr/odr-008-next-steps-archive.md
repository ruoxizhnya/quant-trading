# ODR-008: NEXT_STEPS.md Archival to docs/archive/

> **Status**: Completed
> **Date**: 2026-05-03
> **Category**: Cleanup
> **Related ADRs**: N/A
> **Supersedes**: N/A
> **Author**: AI Assistant

---

## Context

在 ODR-007 (Task Consolidation) 中，NEXT_STEPS.md 的职责已被重新定位为"审查发现归档"，所有可执行任务迁移至 TASKS.md。然而该文件仍位于 `docs/` 根目录，与活跃文档并列，造成以下问题：

1. **视觉混淆**：用户打开 `docs/` 目录时，无法一眼区分活跃文档与归档文档
2. **导航成本**：新团队成员可能误将 NEXT_STEPS.md 当作当前任务来源
3. **违反归档策略**：AGENTS.md 规定归档文档应移至 `docs/archive/`，而 NEXT_STEPS.md 已被标记为 Archived 却仍留在根目录

## Decision

将 `docs/NEXT_STEPS.md` 物理迁移至 `docs/archive/NEXT_STEPS.md`，并更新所有引用路径。

### 迁移细节

| 项目 | 原位置 | 新位置 |
|------|--------|--------|
| 文件路径 | `docs/NEXT_STEPS.md` | `docs/archive/NEXT_STEPS.md` |
| 文档状态 | Archived (in-place) | Archived (proper location) |
| 引用更新 | 4 处相对路径 | 4 处更新为 `archive/NEXT_STEPS.md` |

### 引用更新清单

| 文件 | 原引用 | 更新后 |
|------|--------|--------|
| `docs/ROADMAP.md` | `[NEXT_STEPS.md](NEXT_STEPS.md)` | `[archive/NEXT_STEPS.md](archive/NEXT_STEPS.md)` |
| `docs/TASKS.md` (Related) | `[NEXT_STEPS.md](NEXT_STEPS.md)` | `[archive/NEXT_STEPS.md](archive/NEXT_STEPS.md)` |
| `docs/TASKS.md` (指南) | `[NEXT_STEPS.md](NEXT_STEPS.md)` | `[archive/NEXT_STEPS.md](archive/NEXT_STEPS.md)` |
| `docs/TASKS.md` (底部) | `[NEXT_STEPS.md](NEXT_STEPS.md)` | `[archive/NEXT_STEPS.md](archive/NEXT_STEPS.md)` |
| `docs/VISION.md` | ``NEXT_STEPS.md`` | ``archive/NEXT_STEPS.md`` |

**不更新的引用**：
- `docs/odr/odr-00{1,5,6,7}.md` — 历史 ODR 中提及 NEXT_STEPS.md 的上下文为历史记录，保持原样以保留决策时点的真实性
- `docs/archive/*` 中的文档 — 归档文档之间的引用保持原样

## Consequences

### Positive

1. **目录结构清晰**：`docs/` 根目录仅保留活跃文档，归档文档统一在 `docs/archive/`
2. **降低误用风险**：新用户不会误将归档文档当作当前任务来源
3. **符合归档策略**：与 AGENTS.md 中 "归档文档移至 `docs/archive/`" 的规定一致
4. **保留历史可追溯**：文件内容完整保留，仅物理位置变更

### Negative

1. **外部链接可能失效**：如果存在项目外部的书签或链接指向 `docs/NEXT_STEPS.md`，将返回 404
2. **Git 历史中断**：`git log -- docs/NEXT_STEPS.md` 不再追踪新变更（但 `git log --follow` 仍可追踪）

### Neutral

- 文件内容未做任何修改
- 文档的 Archived 状态未改变

## Artifacts

| 操作 | 文件 |
|------|------|
| 移动 | `docs/NEXT_STEPS.md` → `docs/archive/NEXT_STEPS.md` |
| 更新 | `docs/ROADMAP.md` (1 处引用) |
| 更新 | `docs/TASKS.md` (3 处引用) |
| 更新 | `docs/VISION.md` (1 处引用) |
| 新增 | `docs/odr/odr-008-next-steps-archive.md` (本文档) |
| 更新 | `docs/ADR.md` (ODR Index) |
| 更新 | `docs/archive/README.md` (目录清单) |

## Metrics

| 指标 | 数值 |
|------|------|
| 归档文件数 | 1 |
| 引用更新数 | 5 处（跨 4 个文件） |
| 历史 ODR 引用保留数 | 4 处（保持历史真实性） |
| 用户可见的 docs/ 根目录文件数 | -1 |

## Lessons Learned

1. **归档应及时物理迁移**：ODR-007 在 2026-04-11 已将 NEXT_STEPS.md 标记为归档，但直到 2026-05-03 才执行物理迁移，期间存在 22 天的"逻辑归档但物理未归档"窗口
2. **引用更新应系统化**：使用 `grep -r "NEXT_STEPS.md" docs/` 确保不遗漏任何引用
3. **历史文档引用应保持不变**：ODR 等历史决策记录中的引用不应回溯修改，以保留决策时点的上下文

---

_文档版本: 1.0_
_创建日期: 2026-05-03_
_状态: Completed_
