# S7-P3-5 续接：完成剩余 SPEC.md 漂移修复 + Commit 2

> **任务来源**: S7-P3-5 (ODR-043-7 文档漂移修复)
> **前置计划**: [s7-p3-5-doc-drift-odr-021-sync.md](s7-p3-5-doc-drift-odr-021-sync.md) (已批准, 部分执行)
> **执行规范**: AGENTS.md §8 — 文档任务（一致性审查 + 原子提交）

---

## 1. Context (背景与当前状态)

### 原计划回顾
S7-P3-5 计划分 2 个原子 commit 修复文档漂移:
- **Commit 1**: 服务拓扑漂移 (ODR-021 同步) — 10 个 drift points (A1-A10)
- **Commit 2**: 归档文档引用漂移 — 6 个 drift points (B1-B6)

### 当前实际状态 (2026-07-02 验证)

**分支**: `feat/s7-p3-5-doc-drift-odr-021-sync` (已检出, 未提交)

**关键发现**: Read 工具显示的是缓存内容, 与磁盘实际内容不一致。
**必须使用 `sed`/`grep` (而非 Read) 验证编辑是否真正持久化。**

#### Commit 1 drift points 实际状态

| # | 文件 | 状态 | 证据 (sed/grep) |
|---|------|------|-----------------|
| A1 | AGENTS.md L199 | ✅ 已修复 | `# Start all 5 services (ODR-021: risk+exec merged into analysis)` |
| A2 | ARCHITECTURE.md 拓扑图 | ✅ 已修复 | 拓扑图显示 `/api/risk/*` + `/api/execution/*` in-process |
| A3 | ARCHITECTURE.md 服务表 | ✅ 已修复 | 表无 risk/execution 行; 有 ODR-021 注记块 |
| A4 | SPEC.md Risk 章节 (L759) | ✅ 已修复 | `### 3. Risk API (in-process, /api/risk/* on :8085)` |
| **A5** | **SPEC.md Execution 章节 (L788-829)** | **❌ 未修复** | **L788 仍为 `### 4. Execution Service (port 8084)`; L819-829 仍为旧端点** |
| **A6** | **SPEC.md 散文 (L1228)** | **❌ 未修复** | **L1228 仍为 `Calculate positions via risk service`** |
| **A7** | **SPEC.md config.yaml (L1319-1328)** | **❌ 未修复** | **L1325 `port: 8083`, L1327 `port: 8084` 仍存在** |
| A8 | VISION.md L403-404 | ✅ 已修复 | `Risk API (/api/risk/* on :8085, in-process)` |
| A9 | VISION.md L421 | ✅ 已修复 | `docker-compose.yml defines 5 services (ODR-021)` |
| A10 | TEST.md L83 | ✅ 已修复 | `analysis-service in-process risk.RiskManager` |

**小结**: 10 个 drift points 中 7 个已修复, **3 个未修复 (均在 SPEC.md)**。

#### Commit 2 drift points 实际状态
全部未开始。grep 验证的残留引用:
- `AGENTS.md`: L456, L596, L673, L719 (NEXT_STEPS.md), L727, L735 (IMPLEMENTATION_PLAN.md)
- `README.md`: L60 (NEXT_STEPS.md) — **原计划遗漏, 新增**
- `docs/ROADMAP.md`: L262 (IMPLEMENTATION_PLAN.md)
- `docs/archive/migration-phase3-to-phase4.md`: L7 (IMPLEMENTATION_PLAN.md)
- `docs/AGENTS_TEMPLATE.md`: L330 (NEXT_STEPS.md) — 需验证
- `docs/archive/tasks-phase-2.md`: L6 (IMPLEMENTATION_PLAN.md) — 需验证

### 测试死锁说明 (不阻塞本任务)
后台 job `job-32a3b93bcd5f45809dbe9c20adf5e300` 运行 `go test` 时在
`TestExpressionStrategy_Configure_ChangesFilter` 上超时 (10min panic)。
但 stack trace 显示调用 `BaseStrategy.Configure` (已删除的代码路径) —
当前 `strategy.go:147` 已用 inline helpers 修复, 不调用 BaseStrategy.Configure。
**该失败来自旧代码构建, 不影响当前文档任务。** 验证步骤见 §5。

---

## 2. Remaining Work (剩余工作)

### Phase A: 完成 Commit 1 — 修复 SPEC.md 3 个未持久化的 drift points

#### A5: SPEC.md Execution 章节 (L788-829)

**当前内容** (sed 验证):
```
788: ### 4. Execution Service (port 8084) ✅ *Implemented — Phase 3*
789: > **Status**: LiveTrader interface defined, MockTrader implemented with A-share rules, AdvancedTrader with batch operations and quote streaming. Real broker integration planned for Phase 4.
...
819: **Endpoints**:
820: ```
821: GET  /health                      - Health check
822: POST /orders                      - Create new order
823:      {"symbol": "000001.SZ", "quantity": 100, "side": "buy", "type": "market"}
824: GET  /orders                      - List orders
825: GET  /orders/:id                  - Get order status
826: POST /orders/:id/cancel            - Cancel order
827: GET  /positions                   - Get current positions
828: GET  /account                     - Get account summary
829: ```
```

**目标内容** (匹配 `cmd/analysis/handlers_execution.go:68-76` 实际路由):
```
### 4. Execution API (in-process, /api/execution/* on :8085) ✅ *Implemented — P1-15 (ODR-021)*
> **Status**: LiveTrader interface defined, MockTrader implemented with A-share rules, AdvancedTrader with batch operations and quote streaming. Real broker integration planned for Phase 4. Merged from standalone execution-service(8084) per ODR-021 (P1-15, 2026-06-12).
...
**Endpoints** (all on analysis-service :8085):
```
POST /api/execution/orders            - Create new order
     {"symbol": "000001.SZ", "quantity": 100, "side": "buy", "type": "market"}
GET  /api/execution/orders            - List orders
GET  /api/execution/orders/:id        - Get order status
POST /api/execution/orders/:id/cancel - Cancel order
GET  /api/execution/positions         - Get current positions
GET  /api/execution/account          - Get account summary

# Legacy aliases (root-level, backward compat):
POST /orders                          - legacy alias
GET  /orders                          - legacy alias
GET  /orders/:id                      - legacy alias
POST /orders/:id/cancel               - legacy alias
GET  /positions                       - legacy alias
GET  /account                        - legacy alias
```

> **Source**: `cmd/analysis/handlers_execution.go` — `ExecutionHandler.RegisterRoutes`
```

**编辑策略**: 由于 Edit 工具之前未能持久化, 改用 **逐段精确替换**:
1. 替换 header 行 (L788)
2. 替换 status 行 (L789)
3. 替换 endpoints 块 (L819-829)
4. 每次替换后立即用 `sed -n '<line>p'` 验证

如果 Edit 仍不持久化, 回退到 **Write 整个 SPEC.md** (先 sed 备份全文, 修改, 写回)。

#### A6: SPEC.md 散文 L1228

```diff
-   - Calculate positions via risk service
+   - Calculate positions via in-process risk.RiskManager (ODR-021)
```

#### A7: SPEC.md config.yaml L1319-1328

**当前内容** (sed 验证):
```
services:
  data:
    port: 8081
  strategy:
    port: 8082
  risk:
    port: 8083
  execution:
    port: 8084
  analysis:
    port: 8085
```

**目标内容**:
```
services:
  data:
    port: 8081
  strategy:
    port: 8082
  # ODR-021 (P1-15): risk + execution are in-process under analysis-service,
  # no separate service config / port needed.
  analysis:
    port: 8085
```

### Phase B: 提交 Commit 1

```bash
# 验证无残留
grep -rn "8083\|8084" docs/SPEC.md | grep -v "ODR-021\|Merged from\|standalone\|archived\|historical"
grep -rn "Execution Service (port" docs/SPEC.md
# 期望: 无输出 (或仅有 ODR-021 历史注记)

# 暂存 + 提交
git add AGENTS.md docs/ARCHITECTURE.md docs/SPEC.md docs/TEST.md docs/VISION.md
git commit -m "docs: sync service topology to ODR-021 merge (S7-P3-5)

Updates ARCHITECTURE.md, VISION.md, SPEC.md, TEST.md, and AGENTS.md
to reflect that risk-service(8083) + execution-service(8084) were
merged into analysis-service(8085) as in-process components per
ODR-021 (P1-15). docker-compose service count 7 → 5.

Refs: S7-P3-5
Reviewed: cross-referenced with ODR-021 + cmd/analysis/handlers_*.go"
```

### Phase C: 完成 Commit 2 — 归档文档引用修复

按原计划 [s7-p3-5-doc-drift-odr-021-sync.md](s7-p3-5-doc-drift-odr-021-sync.md) §3 Batch B 执行,
**新增** README.md L60 修复:

#### B1-B6: 原计划 6 处 (详见原计划文件)
- AGENTS.md: 4 处 NEXT_STEPS.md → TASKS.md/archive/NEXT_STEPS.md
- AGENTS.md: 2 处 IMPLEMENTATION_PLAN.md → archive/tasks-phase-2.md
- docs/AGENTS_TEMPLATE.md: L330
- docs/ROADMAP.md: L262
- docs/archive/tasks-phase-2.md: L6
- docs/archive/migration-phase3-to-phase4.md: L7

#### B7 (新增): README.md L60
```diff
- | [NEXT_STEPS](../NEXT_STEPS.md) | 审计报告、问题清单、下一步计划 |
+ | [NEXT_STEPS](../NEXT_STEPS.md) | 审计报告、问题清单、下一步计划 (archived) |
```

#### B8: 更新 docs/TASKS.md
S7-P3-5 状态 `⬜` → `✅`

#### B9: 更新 AGENTS.md Known Issues
ODR-043-7 行 — 移除"服务拓扑/归档引用"部分, 仅保留"ADR 状态"部分 (S7-P3-6 范围)

#### 提交 Commit 2
```bash
git add AGENTS.md README.md docs/AGENTS_TEMPLATE.md docs/ROADMAP.md docs/archive/tasks-phase-2.md docs/archive/migration-phase3-to-phase4.md docs/TASKS.md
git commit -m "docs: fix stale references to archived NEXT_STEPS/IMPLEMENTATION_PLAN (S7-P3-5)

NEXT_STEPS.md and IMPLEMENTATION_PLAN.md were archived to docs/archive/
(per ODR-008) but still cited as active documents in AGENTS.md, README.md,
ROADMAP.md, archive/tasks-phase-2.md, and the migration guide.

Replaces stale references with current active docs:
- NEXT_STEPS.md → TASKS.md (active Phase 3 task tracker)
- IMPLEMENTATION_PLAN.md → archive/tasks-phase-2.md (active Phase 4 tracker)

Refs: S7-P3-5
Reviewed: grep verified no remaining active-path references"
```

### Phase D: 合并到 main

```bash
git checkout main
git merge --no-ff feat/s7-p3-5-doc-drift-odr-021-sync
# 验证合并无冲突
git log --oneline -3
```

---

## 3. Verification (验证步骤)

### Commit 1 验证
```bash
# 1. 确认 SPEC.md Execution 章节已更新
sed -n '788p' docs/SPEC.md
# 期望: ### 4. Execution API (in-process, /api/execution/* on :8085)

# 2. 确认 config.yaml 已更新
sed -n '1319,1328p' docs/SPEC.md
# 期望: 无 risk: port: 8083 / execution: port: 8084

# 3. 确认散文已更新
grep -n "via risk service\|via in-process risk" docs/SPEC.md
# 期望: 仅 "via in-process risk.RiskManager (ODR-021)"

# 4. 全局残留检查 (排除历史注记)
grep -rn "8083\|8084" docs/ARCHITECTURE.md docs/VISION.md docs/SPEC.md docs/TEST.md AGENTS.md | \
  grep -v "ODR-021\|Merged from\|standalone\|archived\|historical\|CR-36\|were added\|have been merged"
# 期望: 无输出
```

### Commit 2 验证
```bash
grep -rn "docs/NEXT_STEPS.md\|docs/IMPLEMENTATION_PLAN.md" docs/ AGENTS.md README.md | \
  grep -v "archive/\|ODR-008\|已归档\|archived"
# 期望: 无输出 (仅 archive/ 内部 + ODR 历史记录)
```

### 测试验证 (非阻塞, 但推荐)
```bash
# 验证 expression strategy 死锁已修复 (当前代码应通过)
go test ./pkg/strategy/expression/... -run TestExpressionStrategy_Configure -count=1 -timeout 30s
# 期望: PASS (后台 job 的失败来自旧代码)
```

---

## 4. Risk Mitigation (风险缓解)

### R1: Edit 工具不持久化
**问题**: 之前 Edit 报告成功但实际未写入磁盘 (Read 显示缓存内容)。
**缓解**: 
- 每次编辑后立即用 `sed -n '<line>p' <file>` 验证 (不用 Read)
- 如果 Edit 仍不持久化, 用 `sed -i` 直接修改, 或 Write 整个文件
- 提交前用 `git diff` 确认所有改动在磁盘上

### R2: README.md 额外引用
**问题**: 原计划未包含 README.md L60 的 NEXT_STEPS.md 引用。
**缓解**: 已加入 Commit 2 作为 B7。提交前 grep 确认无其他遗漏。

### R3: 后台 job 仍在运行
**问题**: `job-32a3b93bcd5f45809dbe9c20adf5e300` 可能仍在运行旧测试。
**缓解**: 不重新运行同一命令。本任务是纯文档修改, 不影响 Go 代码, 测试不受影响。
如需验证测试, 单独运行 `go test ./pkg/strategy/expression/... -run Configure -count=1`。

---

## 5. Files Affected (受影响文件汇总)

### Commit 1 (补完 SPEC.md 3 处)
| 文件 | 改动 |
|------|------|
| `docs/SPEC.md` | A5 Execution header+status+endpoints, A6 散文, A7 config.yaml |

### Commit 2 (归档引用)
| 文件 | 改动 |
|------|------|
| `AGENTS.md` | B1 (4处 NEXT_STEPS), B3 (2处 IMPLEMENTATION_PLAN), B9 (Known Issues) |
| `README.md` | B7 (L60 NEXT_STEPS) |
| `docs/AGENTS_TEMPLATE.md` | B2 (L330) |
| `docs/ROADMAP.md` | B4 (L262) |
| `docs/archive/tasks-phase-2.md` | B5 (L6) |
| `docs/archive/migration-phase3-to-phase4.md` | B6 (L7) |
| `docs/TASKS.md` | B8 (S7-P3-5 ✅) |

---

## 6. Execution Order (执行顺序)

1. [ ] **A5**: 编辑 SPEC.md Execution header (L788) → sed 验证
2. [ ] **A5**: 编辑 SPEC.md Execution status (L789) → sed 验证
3. [ ] **A5**: 编辑 SPEC.md Execution endpoints (L819-829) → sed 验证
4. [ ] **A6**: 编辑 SPEC.md 散文 (L1228) → sed 验证
5. [ ] **A7**: 编辑 SPEC.md config.yaml (L1319-1328) → sed 验证
6. [ ] 全局 grep 验证 Commit 1 无残留
7. [ ] `git add` + Commit 1
8. [ ] **B1-B6**: 按原计划修复归档引用 (逐文件编辑+验证)
9. [ ] **B7**: 修复 README.md L60
10. [ ] **B8**: 更新 TASKS.md S7-P3-5 → ✅
11. [ ] **B9**: 更新 AGENTS.md Known Issues
12. [ ] 全局 grep 验证 Commit 2 无残留
13. [ ] `git add` + Commit 2
14. [ ] 合并到 main
15. [ ] 最终验证: `git log --oneline -5` + grep 全局确认
