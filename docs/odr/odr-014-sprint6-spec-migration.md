# ODR-014: Sprint 6 对齐审查 Spec 文件迁移 + 合并回长效文档

> **Status**: Completed
> **Date**: 2026-06-11
> **Category**: Migration
> **Related ADRs**: 无 (与 ADR-017~020 同源 Sprint 6 启动)
> **Supersedes**: 无

## Context

Sprint 6 启动后，AI Assistant 通过 4 个子代理在 ~5 分钟内完成 ODR-013 全项目 4 维度综合审查，并生成了 73 项任务。审查过程中：

1. **临时工作目录**: Trae IDE 自动创建 `.trae/specs/review-sprint6-alignment/` 用于 spec-driven coding 工作流，存放 `spec.md` / `tasks.md` / `checklist.md` 三个相互关联的文档
2. **文档关系**: 这 3 个文件是 Sprint 6 对齐审查的完整 spec 单元，必须保持逻辑结构与文件关系不破坏
3. **集中化原则**: 项目 `AGENTS.md §3 目录结构` 明确所有文档统一在 `docs/` 目录下，`.trae/` 是临时工作区，最终交付物应迁回 `docs/`

如果保留 `.trae/specs/` 目录会导致：
- 文档分散在两个目录，新人 onboarding 时不知道看 `docs/` 还是 `.trae/`
- `AGENTS.md` 文档索引不完整（缺失 `.trae/` 引用）
- 临时工作区与正式文档混存，违反 AGENTS.md §10 文档维护规范

**续 (2026-06-11 同日追加)**: 完成 `.trae/` → `docs/specs/` 迁移后，进一步审视 `docs/specs/review-sprint6-alignment/` 3 个文件的内容分散性：

- `spec.md` 的 8 类验证通过 + 4 类已修正 + 6 类待校核 — 审计结果，本应归属 ODR-013
- `spec.md` 的"路径引用规范"原则 — 新发现的 VISION 原则
- `tasks.md` 的 5 任务 — 全部已 ✅，但内容是修改 TASKS.md/ADR-019/ADR-020 等（重叠）
- `checklist.md` 6 章节验证 — 全部 [x]，是 ODR-013 复核的产物

为避免临时 spec 单元长期占用 `docs/specs/` 目录，决定将内容**合并回长效文档**（ODR-013 / VISION.md / TASKS.md），删除 `docs/specs/` 目录。

## Decision

**分两阶段执行**：

### 阶段 1：`.trae/specs/` → `docs/specs/` 迁移（已完成，2026-06-11）

将 `.trae/specs/review-sprint6-alignment/` 整个目录迁至 `docs/specs/review-sprint6-alignment/`，保持原文件结构与命名不变。

### 阶段 2：`docs/specs/review-sprint6-alignment/` 内容合并回长效文档（已完成，2026-06-11 同日）

将 3 个 spec 单元文件的内容按性质合并：

| 来源文件 | 内容 | 合并到 | 形式 |
|---------|------|------|------|
| `spec.md` §"对齐审计复核" 8+4+6 验证结果 | ODR-013 §对齐审计复核 | 完整章节（保留 8 验证 + 4 修正 + 6 校核） |
| `spec.md` §"路径引用规范" | VISION.md §Principle 8 | 新增原则（Documentation-Path Consistency） |
| `spec.md` 6 类待校核项 | TASKS.md §Sprint 6 启动期 待校核项 | 新章节（含校核操作清单） |
| `tasks.md` 5 任务 (全部 ✅) | — | **不合并**（重叠 — 5 任务内容是修改 TASKS.md 本身，已在 TASKS.md 体现） |
| `checklist.md` 6 章节 (全部 [x]) | — | **不合并**（仅是 ODR-013 复核的验证表，成果已合入 ODR-013） |

迁移结构：

```
.trae/specs/review-sprint6-alignment/   →   docs/specs/review-sprint6-alignment/
├── spec.md         (3 章节/126 行)         ├── spec.md         (保持原结构)
├── tasks.md        (5 任务/96 行)          ├── tasks.md        (保持原结构)
└── checklist.md    (6 章节/74 行)          └── checklist.md    (保持原结构)

阶段 2 后：docs/specs/ 内容 → ODR-013 / VISION.md / TASKS.md
docs/specs/ 目录 → 删除
```

### 同步修正

| 文件 | 修改内容 |
|------|---------|
| `docs/odr/odr-013-comprehensive-audit-2026-06-11.md` | 新增"对齐审计复核 (2026-06-11 同日)"章节；更新"未来 ODR 计划" |
| `docs/VISION.md` | 新增 Principle 8: Documentation-Path Consistency |
| `docs/TASKS.md` | 新增 §Sprint 6 启动期 待校核项 (6 项)；Sprint 6 顶部注记链接更新 |
| `docs/ADR.md` | ODR-014 标题更新为"+ 合并回长效文档"；index version 2.6.0→2.7.0 |

### 目标位置选择理由

- **`docs/specs/`** （阶段 1 临时）：作为 spec 单元集合的过渡目录
- **`ODR-013`**（阶段 2 永久）：审计结果 + 复核发现的自然归宿（audit→audit follow-up）
- **`VISION.md §Principle 8`**（阶段 2 永久）：路径引用规范是设计原则，归属 VISION
- **`TASKS.md §Sprint 6 启动期`**（阶段 2 永久）：待校核项是活跃任务，归属 TASKS

不创建新的 ODR / ADR / SPEC 单元，避免文档碎片化。

## Consequences

### Positive
- ✅ **集中化**: 所有项目文档统一在 `docs/`，新人 onboarding 路径单一
- ✅ **AGENTS.md 完整索引**: 未来 `AGENTS.md` 文档导航表只需引用 `docs/` 下的目录
- ✅ **可追溯**: ODR-014 完整记录迁移+合并历史，未来若需回查 `.trae/` 内容可从 git log 找回
- ✅ **审计闭环**: ODR-013 现在包含"主审查 + 同日复核"完整记录，符合 ODR 单一可追溯原则
- ✅ **新增 VISION 原则**: Principle 8 提升到 VISION 级别，所有未来 ADR/ODR/TASKS 文档应遵守

### Negative
- ⚠️ **历史链接**: 任何已发布的外部链接指向 `.trae/specs/review-sprint6-alignment/` 或 `docs/specs/review-sprint6-alignment/` 的会 404（本次未发现外部链接，git 提交 hash 仍可通过 git log 找回）
- ⚠️ **新原则需普及**: Principle 8 刚加入 VISION，团队成员需在下次任务中实际应用（已纳入 Sprint 6 P1-1 文档一致化任务跟踪）

## Artifacts

### 阶段 1 创建 (在 `docs/specs/review-sprint6-alignment/`，后已删除)
- `spec.md` — Sprint 6 对齐审查规格
- `tasks.md` — 5 任务执行追踪
- `checklist.md` — 6 章节验证清单

### 阶段 2 修改 (长效文档)
- `docs/odr/odr-013-comprehensive-audit-2026-06-11.md` — 新增"对齐审计复核"章节（含 8+4+6 findings + 修正落地位置 + 复核结论）
- `docs/VISION.md` — 新增 Principle 8 (Documentation-Path Consistency)
- `docs/TASKS.md` — 新增 §Sprint 6 启动期 待校核项 (6 项) + Sprint 6 顶部注记链接更新
- `docs/ADR.md` — ODR-014 标题更新；index version 2.7.0

### 阶段 1+2 删除
- `.trae/specs/review-sprint6-alignment/` (3 files, 阶段 1 完成后)
- `docs/specs/review-sprint6-alignment/` (3 files, 阶段 2 完成后)
- `.trae/` 目录 (阶段 1)
- `docs/specs/` 目录 (阶段 2)

## Metrics

- **阶段 1 净变化**: docs/specs/ +3, .trae/ -3 (净零)
- **阶段 2 净变化**: 
  - ODR-013 +~70 行 (复核章节)
  - VISION.md +~20 行 (Principle 8)
  - TASKS.md +~40 行 (待校核项章节)
  - docs/specs/ -3 文件 + 目录删除
  - **净 +130 行长效文档 / -3 文件临时目录**
- **总迁移 + 合并耗时**: < 15 min (阶段 1 ~5 min + 阶段 2 ~10 min)
- **新增 VISION 原则**: 1 个 (Principle 8)

## Lessons Learned

1. **临时目录应在交付前清理并合并**: 阶段 1 迁到 `docs/specs/` 后立即发现内容应该合并到长效文档；阶段 2 完成彻底整合。**未来**: spec-driven coding 工作流应在 spec 完成后**一次性**合并到长效文档，避免二次操作
2. **ODR 即时记录**: 本 ODR 完整记录阶段 1 + 阶段 2，是 AGENTS.md §10 Rule 2 的标准实践
3. **新原则应早纳入 VISION**: 路径引用规范是审计过程发现的原则性问题，应立刻升级为 VISION 原则（不是 ad-hoc 经验），影响未来所有文档
4. **AI 工具生成的临时路径应在文档中标注**: Trae IDE 使用 `.trae/specs/`，VS Code spec-kit 使用 `.github/specs/`，项目应保持 IDE 无关的最终目录结构
5. **审计结果属于 ODR**: spec.md 中的 findings 本质是 audit follow-up 数据，合并到 ODR-013 是最自然的选择（不是创建 ODR-015）

---

_本 ODR 标记为 Completed，迁移 + 内容合并均已完成，ODR-014 已纳入 ADR.md 索引；ODR-013/VISION.md/TASKS.md 包含完整长效内容。_
