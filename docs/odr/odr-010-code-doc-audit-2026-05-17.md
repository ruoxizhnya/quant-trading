# ODR-010: 2026-05-17 全项目代码与文档一致性审查

> **Status**: Completed
> **Date**: 2026-05-17
> **Category**: Audit
> **Related ADRs**: ADR-015 (AI Agent Architecture), ADR-013 (Data Sync)
> **Supersedes**: N/A
> **Author**: AI Agent (Trae IDE)
> **审查范围**: docs/ + pkg/ + cmd/ + web/src/ + docker-compose + PostgreSQL 实际状态

---

## Context

用户请求对 Quant Lab 项目进行双维度审查：
1. **进度审查** — 从代码和文档两个角度评估项目当前状态
2. **一致度审查** — 对比代码实现与文档描述，识别偏差

**触发动机**:
- 在进行上一轮数据库清理时发现 24 个表中有 10 个未被代码引用
- 这引发对"项目其他部分是否存在类似文档-代码偏差"的疑虑
- 用户希望建立定期审计机制，将发现系统化记录

**审查方法**:
1. 阅读核心文档（VISION/SPEC/ARCHITECTURE/ROADMAP/TASKS/tasks-phase-2）
2. 实测 docker compose ps 验证服务运行状态
3. 实测 go vet + go test -cover 验证代码健康度
4. 交叉对比：服务端口、表名、覆盖率、接口签名、文件存在性

---

## Decision

将本次审查的所有发现系统化为可执行任务，追加至 `TASKS.md`（项目唯一活跃任务追踪源），并按 ODR 规范记录本次审计过程。

### 审查结果

#### ✅ 一致性达标的部分

| 维度 | 一致度 | 验证方法 |
|------|-------|---------|
| Strategy 接口签名 | 100% | SPEC.md ↔ ARCHITECTURE.md ↔ AGENTS.md ↔ 11 个插件实现 |
| Signal 结构体 | 100% | SPEC.md ↔ `pkg/strategy/strategy.go` |
| 服务端口 | 100% | ARCHITECTURE.md ↔ docker-compose ↔ AGENTS.md ↔ 实测 |
| 数据库迁移 | 100% | migrations/ ↔ `pkg/storage/postgres.go` 内联迁移 |
| 服务运行 | 100% | 7 个核心服务全部 healthy |
| 项目整体进度 | 准确 | Phase 1-3 完成，Phase 4 ~95% |

#### ⚠️ 发现的不一致项

| # | 类别 | 偏差描述 | 严重度 | 追踪 |
|---|------|---------|-------|------|
| 1 | 测试失败 | `TestNewPostgresStore` 失败 → 阻塞覆盖率统计 | 🔴 P0 | TASKS P0-7 |
| 2 | 测试失败 | `TestScreenCache_Eviction` 失败 → 间歇性 | 🔴 P0 | TASKS P0-8 |
| 3 | 表名错位 | 10 个文档使用 `backtest_runs`，实际是 `backtest_jobs` | 🟠 P1 | TASKS P1-19 |
| 4 | 覆盖率失真 | AGENTS.md 声明 `pkg/ai` 75%+，实测顶层 0%/子包 30-95% | 🟠 P1 | TASKS P1-20 |
| 5 | 覆盖率失真 | `pkg/live` 文档 0%，实测 52.3% | 🟠 P1 | TASKS P1-21 |
| 6 | 覆盖率失真 | `pkg/backtest` 文档 72.5%，实测 67.8% | 🟠 P1 | TASKS P1-22 |
| 7 | 覆盖率失真 | `pkg/storage` 文档 36.8%，测试失败导致 2.4% | 🟠 P1 | TASKS P1-23 |
| 8 | 状态歧义 | strategy-service 文档说"备用"，实际仍运行占 8082 | 🟠 P1 | TASKS P1-24 |
| 9 | 文档滞后 | ARCHITECTURE.md 数据模型未反映表清理（24→14） | 🟡 P2 | TASKS P2-19 |
| 10 | 验收缺口 | Phase 4 完成度未对照 ADR-015 7 项验收标准 | 🟡 P2 | TASKS P2-20 |

### 服务运行实测（2026-05-17）

```
NAME                                STATUS                PORTS
quant-trading-analysis-service-1    Up 5 days (healthy)   0.0.0.0:8085->8085
quant-trading-data-service-1        Up 5 days (healthy)   0.0.0.0:8081->8081
quant-trading-execution-service-1   Up 5 days (healthy)   0.0.0.0:8084->8084
quant-trading-postgres-1            Up 5 days (healthy)   0.0.0.0:5432->5432
quant-trading-redis-1               Up 5 days (healthy)   0.0.0.0:6379->6379
quant-trading-risk-service-1        Up 5 days (healthy)   0.0.0.0:8083->8083
quant-trading-strategy-service-1    Up 5 days (healthy)   0.0.0.0:8082->8082
```

### 测试覆盖率实测（2026-05-17）

| 包 | 文档声明 | 实测 | 差异 |
|---|---------|------|------|
| `pkg/ai` (汇总) | 75%+ | 0% (顶层) | ❌ |
| `pkg/ai/agents` | — | 16.2% | ⚠️ |
| `pkg/ai/client` | — | 78.4% | ✅ |
| `pkg/ai/drift` | — | 90.3% | ✅ |
| `pkg/ai/evolution` | — | 87.5% | ✅ |
| `pkg/ai/expression` | — | 75.2% | ✅ |
| `pkg/ai/gene_pool` | — | 30.6% | ⚠️ |
| `pkg/ai/intent` | — | 83.6% | ✅ |
| `pkg/ai/metrics` | — | 95.7% | ✅ |
| `pkg/ai/pipeline` | — | 34.1% | ⚠️ |
| `pkg/ai/search` | — | 95.6% | ✅ |
| `pkg/ai/validator` | — | 74.2% | ✅ |
| `pkg/ai/yaml` | — | 94.4% | ✅ |
| `pkg/backtest` | 72.5% | 67.8% | ⚠️ |
| `pkg/data` | 70.6% | 70.6% | ✅ |
| `pkg/strategy/plugins` | 80.3% | 80.3% | ✅ |
| `pkg/strategy` (汇总) | 73.4% | 39.4% | ❌ |
| `pkg/storage` | 36.8% | 2.4% (测试失败) | ❌ |
| `pkg/live` | 0% | 52.3% | ⚠️ |
| `pkg/marketdata` | — | 77.8% | ✅ |
| `pkg/risk` | — | 59.8% | ✅ |

---

## Consequences

### 正面影响

- **集中可追溯**: 10 项新发现已系统化记录到 TASKS.md，不会随会话丢失
- **优先级明确**: 2 项 P0 阻塞性问题（测试失败）被前置
- **可重复审计**: 建立了"双维度审查"标准流程，未来可定期执行

### 负面影响

- **TASKS.md 待办项增加**: 0 → 10 项待处理
- **文档修正工作量**: 至少 11 个文档文件需要更新（10 处 backtest_runs + 1 个 ARCHITECTURE 数据模型）

### 关联工作

本次审查还触发了上一轮数据库清理任务（从 24 表减到 14 表），但未单独建 ODR，因属于"清理操作"而非"审计决策"。

---

## Artifacts

### 创建/修改的文件

| 文件 | 操作 | 说明 |
|------|------|------|
| `docs/TASKS.md` | 修改 | 追加 10 项 P0/P1/P2 任务 + 版本升至 v3.1.0 + 变更日志 |
| `docs/odr/odr-010-code-doc-audit-2026-05-17.md` | 新建 | 本 ODR 文件 |
| `docs/ADR.md` | 修改 | ODR 索引添加 ODR-008、ODR-009、ODR-010 |

### 数据库清理（关联操作）

| 文件 | 操作 | 说明 |
|------|------|------|
| PostgreSQL | 删除 10 张表 | backtest_data, new_share, stk_managers, stk_rewards, stock_company, trade_calendar, daily, trade_cal, market_data, stock_basic |

---

## Metrics

| 指标 | 数值 |
|------|------|
| 审查范围 | 8 个核心文档 + 17 个 pkg/ 子目录 + 5 个 cmd/ 服务 + 7 个 web/src/ 组件目录 |
| 审查耗时 | 单次会话完成 |
| 发现总问题 | 10 项 |
| 严重度分布 | P0: 2 / P1: 6 / P2: 2 |
| 文档失真比例 | 7/10 (70%) 与文档相关问题涉及文档失真 |
| 代码健康度 | `go vet` 通过；`go test` 2 个包失败（已记录） |
| 服务可用性 | 7/7 healthy (100%) |
| 数据库表清理 | 24 → 14 (-42%) |

---

## Lessons Learned

1. **覆盖率统计口径需统一**: "包级别"vs"子包平均"vs"汇总"是常见歧义点。AGENTS.md 后续应明确"按子包分别列出，不汇总"。

2. **命名错位累积危害**: `backtest_runs` 在 10 个文档中固化，但实际实现用 `backtest_jobs`。建议建立"代码即真相"原则 — 当文档与代码冲突时，以代码为准并更新文档。

3. **测试失败需立即修复**: `TestNewPostgresStore` 失败导致 `pkg/storage` 覆盖率从 36.8% 暴跌至 2.4%，影响所有依赖该数据的报告。

4. **定期审查机制缺失**: ODR-009 之后约 10 天就出现新偏差。建议在 ODR 中明确"每 Sprint 一次轻量审查"作为流程标准。

5. **策略-service 状态需决策**: 占 8082 端口但文档说"备用"。需 ADR/ODR 明确：(a) 移除 (b) 激活 (c) 永久备用 — 三选一。

---

## Recommended Follow-ups

| 优先级 | 任务 | 关联 |
|-------|------|------|
| P0 | 修复 2 个失败测试 | TASKS P0-7, P0-8 |
| P1 | 统一 backtest_runs 命名 | TASKS P1-19 |
| P1 | 更新 4 项覆盖率数据 | TASKS P1-20~P1-23 |
| P1 | 决策 strategy-service 状态 | TASKS P1-24 |
| P2 | 同步 ARCHITECTURE.md 数据模型 | TASKS P2-19 |
| P2 | Phase 4 验收对照 | TASKS P2-20 |

---

_本次审查由用户主动请求触发，作为项目健康度基线记录。后续可按 ODR-009 → ODR-010 模式定期复审。_
