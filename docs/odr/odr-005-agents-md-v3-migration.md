# ODR-005: AGENTS.md v3.0 Migration to Template v2.0

> **Status**: Completed
> **Date**: 2026-04-11
> **Category**: Migration
> **Related ADRs**: None
> **Supersedes**: None

## Context

项目存在两份 AGENTS 文件：
1. `AGENTS.md` (quant-trading) — 操作手册型，命令密集，580 行
2. `AGENTS copy.md` (Claudeer) — 治理规范型，流程密集，320 行

用户希望：
1. 审查两份文件的优缺点
2. 总结通用模板
3. 将 quant-trading 项目文档迁移到新模板

## Decision

采用融合方案，创建 **AGENTS Template v2.0**，包含 14 个标准章节：

| 章节 | 来源 | 核心价值 |
|------|------|---------|
| 1. 项目概述 | Claudeer | 快速理解上下文（5 秒） |
| 2. 架构概览 | quant-trading | ASCII 图 + ADR 索引 |
| 3. 目录结构 | quant-trading | 关键目录说明 |
| 4. 角色与边界 | quant-trading | 工作范围 + 禁区 |
| 5. 命令参考 | quant-trading | 即拿即用 |
| 6. 代码规范 | quant-trading | 实际示例 |
| 7. 数据流 | quant-trading | 拓扑图 |
| **8. 工作流规范** | **Claudeer** ⭐ | **类型分类 + 前置/后置动作** |
| 9. 行为边界 | quant-trading | Always/Ask/Never |
| **10. 文档维护** | **两者融合** ⭐⭐ | **6 条规则 + ODR 触发器** |
| 11. 文档导航 | Claudeer | 按用途分类（Explanation/Reference/How-to） |
| 12. 会话管理 | quant-trading | Standup 格式 |
| 13. 当前状态 | Claudeer | 健康仪表盘 |
| 14. 已知问题 | quant-trading | 变通方案表 |

**关键创新**（来自 Claudeer）：
- **工作流类型系统**：设计/审计/测试/实现/文档/其他 — 每种有判定规则和前置/后置动作
- **文档生命周期管理**：Active → Stale → Archived → Purged
- **ODR 创建触发器**：自动化运营决策记录

## Consequences

### Positive
✅ 统一的文档格式，降低认知负荷
✅ 可复用模板，适用于其他项目
✅ 自动化文档维护规则（Update-on-Change triggers）
✅ 工作流规范化，AI 行为可预测可审计
✅ 文档导航按用途分类，查找效率提升

### Negative
⚠️ AGENTS.md 从 580 行增长到 ~576 行（基本持平）
⚠️ 需要团队学习新规范（一次性成本）
⚠️ 首次迁移需要手动更新 6 份核心文档头部

## Artifacts

### Files Created
- `docs/AGENTS_TEMPLATE.md` — 通用模板（~350 行）

### Files Modified
- `AGENTS.md` — v2.0 → v3.0（完全重写为 14 章结构）
- `docs/SPEC.md` — v1.2 → v1.3（添加元数据头部）
- `docs/ARCHITECTURE.md` — v1.0 → v2.0（添加元数据头部 + changelog）
- `docs/VISION.md` — v1.2 → v1.3（添加 Related 链接）
- `docs/ROADMAP.md` — v1.0 → v1.1（添加 PHASE3-PLAN 链接）
- `docs/TEST.md` — v1.0 → v1.1（统一链接格式）
- `docs/NEXT_STEPS.md` — v1.0 → v1.1（添加 Related 链接）

### Files Unchanged
- `docs/ADR.md` — 待更新 ODR Index（下一步）
- `docs/adr/*` — 10 条 ADR 无需变更
- `docs/odr/*` — 4 条现有 ODR 无需变更

## Metrics

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| AGENTS.md 行数 | ~580 | ~576 | -0.7% |
| 标准化文档数 | 0 | 7 | +7 |
| 元数据头部覆盖率 | 0% | 100% (core docs) | +100% |
| 文档间交叉引用 | 不统一 | 统一格式 [doc](path) | ✅ |
| 工作流规范 | 无 | 6 类型完整定义 | ✅ |

## Lessons Learned

1. **模板设计原则**：从两个实际项目中提取模式，比理论设计更实用
2. **渐进式迁移**：先改头部元数据，再重构内容，降低风险
3. **保留有效内容**：quant-trading 的命令参考和代码示例非常实用，必须保留
4. **创新点明确标注**：工作流规范和文档维护规则是核心创新，应重点推广
5. **版本号策略**：使用语义化版本（v1.2 → v1.3），便于追踪变更

## Next Steps (Recommended)

1. **更新 `docs/ADR.md`**：在 ODR Index 表中添加 odr-005 条目
2. **团队培训**：向团队成员介绍新模板的 14 章结构
3. **应用到其他项目**：将模板用于 Claudeer 或其他项目的 AGENTS.md
4. **工具支持**（可选）：开发 linter 自动检查文档是否符合模板规范
5. **持续优化**：收集使用反馈，2-3 个月后迭代模板 v2.1

---
_ODR created as part of AGENTS Template v2.0 migration_
_See: [AGENTS_TEMPLATE.md](../AGENTS_TEMPLATE.md) for the reusable template_
