# ODR-007: Task Consolidation & Document Migration

> **Status**: Completed
> **Date**: 2026-04-11
> **Category**: Migration + Cleanup
> **Related ADRs**: None
> **Supersedes**: None

## Context

项目存在以下文档管理问题：

1. **任务分散**：任务散落在多个文档中（PHASE3-PLAN.md、NEXT_STEPS.md、AGENTS.md），难以追踪
2. **文档职责不清**：AGENTS.md 包含任务列表，违反"单一职责"原则
3. **计划文档膨胀**：PHASE3-PLAN.md 包含大量任务项，与 TASKS.md 功能重叠
4. **NEXT_STEPS.md 任务项过多**：Phase A-E 任务列表与 TASKS.md 重复

### 散落任务清单

| 文档 | 任务数量 | 问题 |
|------|---------|------|
| PHASE3-PLAN.md | 28 项 (D1-D6) | 实施计划包含大量任务项 |
| NEXT_STEPS.md | 20+ 项 (Phase A-E) | 与 TASKS.md 重复 |
| AGENTS.md 第 13 章 | 8 项 (P0-P2) | 不应在配置文件中维护任务 |

## Decision

### 1. TASKS.md 作为统一任务追踪源

升级 TASKS.md 到 v2.0.0，整合所有任务：
- **P0-P3 优先级任务**：来自 CODE_REVIEW_REPORT.md + NEXT_STEPS.md + AGENTS.md
- **Phase 3 实施任务**：来自 PHASE3-PLAN.md (D1-D6)
- **状态标注**：已完成任务标记 ✅，进行中标记 🔵

**任务统计**：
| 类别 | 数量 |
|------|------|
| P0 (安全/数据完整性) | 6 |
| P1 (代码质量/可维护性) | 16 |
| P2 (架构/文档改进) | 17 |
| P3 (持续改进) | 18 |
| Phase 3 (D1-D6) | 28 |
| **总计** | **85** |

### 2. AGENTS.md 精简

移除第 13 章的"待办事项"列表，改为指向 TASKS.md：
```markdown
### 任务追踪
> **统一任务追踪源**: [docs/TASKS.md](docs/TASKS.md)
> 
> 所有可执行任务（P0-P3 + Phase 3 实施任务）均在 TASKS.md 中维护。
> 本文件不再维护任务列表，避免信息分散。
```

### 3. NEXT_STEPS.md 精简

- **保留**：审查发现详情（设计文档审查、代码一致性、测试有效性、代码质量）
- **移除**：Phase A-E 任务列表（已迁移到 TASKS.md）
- **新增**：任务迁移映射表

### 4. PHASE3-PLAN.md 归档

- **原因**：任务已迁移到 TASKS.md，设计内容保留在原文件作为参考
- **归档位置**：`docs/archive/research-2026-Q2/PHASE3-PLAN.md`
- **替代**：TASKS.md 中的 Phase 3 实施任务章节

### 5. phase-gate-reviews.md 保留

- **原因**：质量门禁 artifact，属于历史记录
- **不迁移**：这是审查记录，不是任务

## Consequences

### Positive
✅ 统一任务追踪源（TASKS.md），避免任务散落
✅ AGENTS.md 职责清晰（AI 配置文件，非任务追踪）
✅ NEXT_STEPS.md 职责清晰（审查发现归档，非任务列表）
✅ 任务状态可视化（85 项任务，3 项已完成）
✅ 遵循 ODR 规则（No Report Files — 任务迁移到 TASKS.md）

### Negative
⚠️ 需要团队习惯使用 TASKS.md 而非散碎文档
⚠️ PHASE3-PLAN.md 归档后需要从 TASKS.md 查看任务

## Artifacts

### Files Modified
- `docs/TASKS.md` — 升级到 v2.0.0，整合 85 项任务（+131 行）
- `AGENTS.md` — 移除待办事项列表，指向 TASKS.md（-10 行）
- `docs/NEXT_STEPS.md` — 精简为审查发现归档（-57 行）
- `docs/archive/README.md` — 更新归档目录结构

### Files Moved (Archived)
- `docs/PHASE3-PLAN.md` → `docs/archive/research-2026-Q2/PHASE3-PLAN.md`

## Metrics

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| 任务追踪文档数 | 3 (散落) | 1 (统一) | -67% |
| TASKS.md 任务数 | 47 | 85 | +81% |
| AGENTS.md 行数 | ~580 | ~570 | -2% |
| NEXT_STEPS.md 行数 | 362 | 305 | -16% |
| 已完成任务数 | 0 | 3 | +3 |

## Lessons Learned

1. **单一任务源原则**：所有可执行任务必须在 TASKS.md 维护，不得散落在其他文档
2. **文档职责分离**：AGENTS.md 是配置文件，NEXT_STEPS.md 是审查发现归档，都不是任务追踪文档
3. **状态标注重要**：已完成任务必须标记 ✅，避免重复执行
4. **归档而非删除**：PHASE3-PLAN.md 归档而非删除，保留设计内容作为参考
5. **迁移映射表**：NEXT_STEPS.md 保留迁移映射表，便于追溯

## Next Steps

1. **团队培训**：介绍 TASKS.md 的使用方式
2. **定期维护**：每次完成任务后更新 TASKS.md 状态
3. **自动化**：考虑 CI/CD 集成任务状态更新

---
_ODR created as part of task consolidation operation_
_See: [TASKS.md](../TASKS.md) for the unified task tracker_
