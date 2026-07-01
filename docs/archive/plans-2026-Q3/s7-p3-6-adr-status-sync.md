# S7-P3-6: ADR 状态同步 (ODR-043-7 剩余项)

> **Task ID**: S7-P3-6 (ODR-043 Sprint 7 P3)
> **Branch**: `feat/s7-p3-6-adr-status-sync` (已创建)
> **Plan Status**: Approved (re-planning after context reset)
> **Date**: 2026-07-01

---

## Summary

修复 ODR-043-7 识别的 ADR 状态文档漂移。S7-P3-5 已修复服务拓扑漂移和归档文档引用漂移；本任务 (S7-P3-6) 修复剩余的 **ADR 状态漂移**：

1. ADR-014 状态应为 "Superseded by ADR-020 §6" (Strategy 接口 ISP 拆分已取代 ADR-014 §1 的单一接口设计)
2. ADR-015 索引状态应为 "Accepted" (文件头已正确，索引漂移)
3. ADR-019 状态应为 "Accepted" (3 个 section 全部落地: §1→ODR-021, §2→ODR-020, §3→ODR-017，均 Completed)
4. ADR-020 文件头状态应为 "Accepted" (索引已正确，文件头漂移)

完成后从 AGENTS.md Known Issues 移除 ODR-043-7 整行 (所有漂移已解决)。

---

## Current State Analysis (Phase 1 探索结果)

### 已验证的当前状态 (通过 `sed -n` 直接读盘)

| 文件 | 行号 | 当前内容 | 目标内容 | 类型 |
|------|------|----------|----------|------|
| `docs/adr/adr-014-*.md` | L3 | `> **Status**: Proposed` | `> **Status**: Superseded by [ADR-020 §6](adr-020-engine-decomposition.md)` | 文件头漂移 |
| `docs/adr/adr-014-*.md` | L7 后 | (无 Superseded By 字段) | 新增 `> **Superseded By**: [ADR-020 §6](adr-020-engine-decomposition.md) — Strategy 接口 ISP 拆分 (CQ-006)` | 模板扩展 |
| `docs/adr/adr-015-*.md` | L3 | `> **Status**: Accepted` ✅ | (无需修改) | 文件已正确 |
| `docs/adr/adr-019-*.md` | L4 | `**Status:** Proposed` | `**Status:** Accepted` | 文件头漂移 |
| `docs/adr/adr-020-*.md` | L4 | `**Status:** Proposed` | `**Status:** Accepted` | 文件头漂移 |
| `docs/ADR.md` | L28 | ADR-014 `\| Proposed \` | `\| Superseded by ADR-020 §6 \` | 索引漂移 |
| `docs/ADR.md` | L29 | ADR-015 `\| Proposed \` | `\| Accepted \` | 索引漂移 |
| `docs/ADR.md` | L33 | ADR-019 `\| Proposed \` | `\| Accepted \` | 索引漂移 |
| `docs/ADR.md` | L34 | ADR-020 `\| **Accepted** (P1-16~20,24) \` | (无需修改) ✅ | 索引已正确 |
| `docs/ADR.md` | 末行 | (docs/specs 清理记录) | 追加 S7-P3-6 状态变更日志 | 审计追踪 |
| `AGENTS.md` | L687 | ODR-043-7 Known Issues 整行 | (删除整行) | 问题已解决 |
| `docs/TASKS.md` | L1632 | S7-P3-6 `⬜` | S7-P3-6 `✅` | 任务状态更新 |

### 双向漂移确认

- **ADR-015**: 文件头 ✅ Correct vs 索引 ❌ Wrong (索引漂移)
- **ADR-020**: 文件头 ❌ Wrong vs 索引 ✅ Correct (文件头漂移)
- **ADR-014 / ADR-019**: 文件头 + 索引 双双漂移

### ADR-020 §6 取代关系证据

`docs/adr/adr-020-engine-decomposition.md` L174:
```
### §6. Strategy 接口 ISP 拆分 (CQ-006, ADR-014 precedent)
```
该 section 将 ADR-014 §1 的单一 `Strategy` 接口 (7 方法) 重构为 4 个 ISP 子接口 (StrategyCore/Configurable/SignalGenerator/ResourceManaged) 的复合接口，向后兼容。已在 `pkg/strategy/interfaces.go` 落地 (P1-24)。

### ADR-019 落地证据

ADR-019 三个 section 全部实现：
- §1 (Service 合并) → ODR-021 (Completed 2026-06-12, risk+exec 合并到 analysis)
- §2 (Engine 拆分) → ODR-020 / ADR-020 (Accepted, P1-16~20,24 全部完成)
- §3 (AI Copilot Sandbox) → ODR-017 (Completed)

---

## Proposed Changes (执行方案)

### 执行方式: Python 脚本

> **R1 (CRITICAL)**: 本项目 Edit 工具有已知缓存 bug (S7-P3-5 已验证)：
> Edit 报告成功但不写盘，Read 显示缓存内容与磁盘实际不一致。
> **所有文件修改改用 Python 脚本 `open/read/replace/write`**，每次修改后用
> `sed -n '<line>p'` (直接读盘) 验证已持久化。

### 修改清单 (7 个文件)

#### 文件 1: `docs/adr/adr-014-strategy-framework-refactor.md`

**修改 1a** — L3 状态变更:
- 旧: `> **Status**: Proposed`
- 新: `> **Status**: Superseded by [ADR-020 §6](adr-020-engine-decomposition.md)`

**修改 1b** — L7 后新增 Superseded By 字段:
- 在 `> **Supersedes**: None (enhances existing decisions)` 行之后插入:
  ```
  > **Superseded By**: [ADR-020 §6](adr-020-engine-decomposition.md) — Strategy 接口 ISP 拆分 (CQ-006)
  ```

> **说明**: ADR 模板当前只有 `Supersedes` 字段，无 `Superseded By`。本任务作为模板扩展
> 为 ADR-014 添加该字段，记录被取代关系。仅 ADR-014 添加 (它是唯一被本 Sprint 内
> ADR 取代的决策)；不为其他 ADR 添加空字段。

#### 文件 2: `docs/adr/adr-019-service-merge-ai-copilot.md`

**修改 2** — L4 状态变更:
- 旧: `**Status:** Proposed`
- 新: `**Status:** Accepted`

#### 文件 3: `docs/adr/adr-020-engine-decomposition.md`

**修改 3** — L4 状态变更:
- 旧: `**Status:** Proposed`
- 新: `**Status:** Accepted`

#### 文件 4: `docs/ADR.md` (索引)

**修改 4a** — L28 (ADR-014 行):
- 旧: `| [ADR-014](adr/adr-014-strategy-framework-refactor.md) | Strategy Framework Refactor & Unified Interface | Proposed | 2026-05-04 |`
- 新: `| [ADR-014](adr/adr-014-strategy-framework-refactor.md) | Strategy Framework Refactor & Unified Interface | Superseded by ADR-020 §6 | 2026-05-04 |`

**修改 4b** — L29 (ADR-015 行):
- 旧: `| [ADR-015](adr/adr-015-ai-agent-architecture.md) | AI Agent Quantitative Research Architecture | Proposed | 2026-05-04 |`
- 新: `| [ADR-015](adr/adr-015-ai-agent-architecture.md) | AI Agent Quantitative Research Architecture | Accepted | 2026-05-04 |`

**修改 4c** — L33 (ADR-019 行):
- 旧: `| [ADR-019](adr/adr-019-service-merge-ai-copilot.md) | Service 合并 + AI Copilot Sandbox 重构 | Proposed | 2026-06-11 |`
- 新: `| [ADR-019](adr/adr-019-service-merge-ai-copilot.md) | Service 合并 + AI Copilot Sandbox 重构 | Accepted | 2026-06-11 |`

**修改 4d** — 文件末尾追加状态变更日志:
```
_2026-07-01 状态变更: ADR-014 Proposed→Superseded by ADR-020 §6 (Strategy 接口 ISP 拆分取代单一接口); ADR-015 Proposed→Accepted (文件头早已正确，索引漂移修复); ADR-019 Proposed→Accepted (§1→ODR-021, §2→ODR-020, §3→ODR-017 全部落地); ADR-020 文件头 Proposed→Accepted (索引早已正确，文件头漂移修复) — S7-P3-6 ODR-043-7_
```

#### 文件 5: `AGENTS.md` (Known Issues)

**修改 5** — 删除 L687 ODR-043-7 整行:
- 删除: `| **ODR-043-7 文档漂移: ADR 状态 (剩余)** | ADR-014 被 ADR-020 §6 取代未标记; ADR-019 状态需更新为 Accepted — S7-P3-6 范围 — 详见 [ODR-043](docs/odr/odr-043-comprehensive-audit-2026-06-29.md) |`
- 理由: S7-P3-5 + S7-P3-6 完成后，ODR-043-7 识别的所有文档漂移已全部解决

#### 文件 6: `docs/TASKS.md`

**修改 6** — L1632 状态更新:
- 旧: `| S7-P3-6 | 标记 ADR-014 为 Superseded by ADR-020 §6 + 更新 ADR-015/019/020 状态 | \`docs/adr/adr-014*.md\` 等 | ⬜ | ODR-043 |`
- 新: `| S7-P3-6 | 标记 ADR-014 为 Superseded by ADR-020 §6 + 更新 ADR-015/019/020 状态 | \`docs/adr/adr-014*.md\` 等 | ✅ | ODR-043 |`

#### 文件 7: `AGENTS.md` 已在文件 5 中处理 (合并)

> 实际是 6 个文件修改 (AGENTS.md 一处 + ADR 4 个 + ADR.md 1 个 + TASKS.md 1 个)。

---

## Assumptions & Decisions

1. **ADR 内容不可变原则**: 仅修改 Status 字段和索引，不修改 ADR 的 Context/Decision/Consequences 正文 (per AGENTS.md §4 禁区 — ADR decision content is immutable)。
2. **Superseded By 字段为模板扩展**: 当前 ADR 模板无此字段，本任务为 ADR-014 单独添加，不为其他 ADR 添加空字段。
3. **不创建新 ODR**: 本任务是 ODR-043-7 的修复执行 (已在 ODR-043 范围内)，无需新建 ODR。S7-P3-5 已创建 ODR-043 引用，本任务延续。
4. **原子提交**: 一个 commit 包含所有 6 个文件修改 (逻辑上是一个变更: "ADR 状态同步")。
5. **Edit 工具缓存 bug 规避**: 全程使用 Python 脚本 + sed 验证，不依赖 Edit/Read 工具的缓存视图。

---

## Verification Steps (验证步骤)

执行 Python 脚本后，按以下顺序验证 (全部用 `sed`/`grep` 直接读盘):

### 步骤 1: 逐文件验证状态行已持久化

```bash
# ADR-014
sed -n '3p' docs/adr/adr-014-strategy-framework-refactor.md
# 期望: > **Status**: Superseded by [ADR-020 §6](adr-020-engine-decomposition.md)
sed -n '8p' docs/adr/adr-014-strategy-framework-refactor.md
# 期望: > **Superseded By**: [ADR-020 §6](adr-020-engine-decomposition.md) — Strategy 接口 ISP 拆分 (CQ-006)

# ADR-019
sed -n '4p' docs/adr/adr-019-service-merge-ai-copilot.md
# 期望: **Status:** Accepted

# ADR-020
sed -n '4p' docs/adr/adr-020-engine-decomposition.md
# 期望: **Status:** Accepted

# ADR.md 索引
sed -n '28p;29p;33p;34p' docs/ADR.md
# 期望: ADR-014=Superseded by ADR-020 §6, ADR-015=Accepted, ADR-019=Accepted, ADR-020=**Accepted** (P1-16~20,24)

# ADR.md 末行 (状态变更日志)
tail -1 docs/ADR.md
# 期望: 包含 "2026-07-01 状态变更" + "S7-P3-6 ODR-043-7"

# AGENTS.md L686-688 (确认 ODR-043-7 行已删除)
sed -n '686,688p' AGENTS.md
# 期望: 无 "ODR-043-7 文档漂移" 行

# TASKS.md L1632
sed -n '1632p' docs/TASKS.md
# 期望: S7-P3-6 ... ✅
```

### 步骤 2: 全局一致性检查 (文件头 ↔ 索引)

```bash
# 确认无残留 "Proposed" 状态在 ADR-014/015/019/020 文件头
grep -n "Status.*Proposed" docs/adr/adr-014*.md docs/adr/adr-015*.md docs/adr/adr-019*.md docs/adr/adr-020*.md
# 期望: 无输出

# 确认索引中 ADR-014/015/019 已更新
grep -E "ADR-01[459]" docs/ADR.md | grep -E "Proposed"
# 期望: 无输出 (ADR-013/016/017/018 仍为 Proposed 是正常的)
```

### 步骤 3: git diff 检查

```bash
git diff --stat
# 期望: 6 个文件修改
git diff
# 人工 review diff 内容
```

---

## Execution Steps (执行步骤)

1. ✅ 创建 feature branch `feat/s7-p3-6-adr-status-sync` (已完成)
2. 编写 Python 脚本 `/tmp/fix_adr_status.py` 处理 6 个文件的所有修改
3. 运行 Python 脚本
4. 按 Verification Steps 逐一验证 (sed 直读盘)
5. 全局一致性 grep 检查
6. `git add` 6 个文件 + `git commit` (atomic commit)
   - Commit message:
     ```
     docs: sync ADR status to reflect ODR-043-7 resolution (S7-P3-6)

     ADR-014: Proposed → Superseded by ADR-020 §6 (Strategy 接口 ISP
     拆分 CQ-006 取代 ADR-014 §1 单一接口设计)
     ADR-015: index Proposed → Accepted (file header was already correct)
     ADR-019: Proposed → Accepted (all 3 sections landed: §1→ODR-021,
     §2→ODR-020, §3→ODR-017)
     ADR-020: file header Proposed → Accepted (index was already correct)
     Removed ODR-043-7 row from AGENTS.md Known Issues (all drift resolved).

     Refs: S7-P3-6 (ODR-043-7)
     Reviewed: self-review + sed verification + grep consistency check
     ```
7. 切换到 main: `git checkout main`
8. 合并 feature branch: `git merge --no-ff feat/s7-p3-6-adr-status-sync`
9. (可选) 删除 feature branch: `git branch -d feat/s7-p3-6-adr-status-sync`

---

## Risk Mitigation

| 风险 | 缓解措施 |
|------|----------|
| R1: Edit 工具缓存 bug | 全程使用 Python 脚本 + sed 验证 (S7-P3-5 已验证可行) |
| R2: ADR 内容不可变违规 | 仅修改 Status 字段、Superseded By 字段、索引行、状态变更日志；不碰 Context/Decision/Consequences 正文 |
| R3: 删除 AGENTS.md Known Issues 行误删 | sed 验证 L686-688 确认仅删除目标行，前后行保持完整 |
| R4: 非原子提交 | 一个 commit 包含全部 6 文件修改 (逻辑统一: "ADR 状态同步") |
