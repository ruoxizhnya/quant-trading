# Architecture Decision Records (ADR) & Operational Decision Records (ODR)

> **Location:** `docs/adr/` — architectural ADR files | `docs/odr/` — operational ODR files
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Version:** 3.1.0
> **Created:** 2026-03-24
> **Updated:** 2026-06-13

---

## ADR Index — Architectural Decisions

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](adr/adr-001-plugin-loading.md) | Dynamic Plugin Loading vs. Compiled Strategies | Accepted | 2026-03-24 |
| [ADR-002](adr/adr-002-timescaledb.md) | TimescaleDB vs. Vanilla PostgreSQL for OHLCV Storage | Accepted | 2026-03-24 |
| [ADR-003](adr/adr-003-background-worker.md) | In-Process Backtest vs. Background Worker | Accepted | 2026-03-24 |
| [ADR-004](adr/adr-004-scoring-method.md) | Rank-Based Composite Scoring vs. Portfolio Optimization | Accepted | 2026-03-24 |
| [ADR-005](adr/adr-005-strategy-config.md) | YAML Strategy Config vs. Database-Driven Strategy Config | Accepted | 2026-03-24 |
| [ADR-006](adr/adr-006-job-queue.md) | Job Queue Technology Selection | OPEN | 2026-03-24 |
| [ADR-007](adr/adr-007-ai-sandbox.md) | AI Evolution Layer — Sandbox & Safety | **Accepted** (2026-06-11) | 2026-03-24 |
| [ADR-008](adr/adr-008-inter-service-comm.md) | Synchronous vs. Async Inter-Service Communication | **Accepted** (2026-06-11) | 2026-03-24 |
| [ADR-009](adr/adr-009-speed-target-revision.md) | Speed Target Revision — Phase 1 Exit Criteria | Decided | 2026-03-25 |
| [ADR-010](adr/adr-010-speed-architecture.md) | Speed Optimization Architecture — Phase 2 | Draft | 2026-03-25 |
| [ADR-011](adr/adr-011-vue-spa-frontend.md) | Vue 3 SPA as Official Frontend (Replacing Legacy HTML) | Accepted | 2026-04-11 |
| [ADR-012](adr/adr-012-strategy-service-standby.md) | Strategy-Service Standby Decision | Accepted | 2026-04-11 |
| [ADR-013](adr/adr-013-data-sync-enhancement.md) | Data Synchronization Enhancement | Proposed | 2026-05-03 |
| [ADR-014](adr/adr-014-strategy-framework-refactor.md) | Strategy Framework Refactor & Unified Interface | Proposed | 2026-05-04 |
| [ADR-015](adr/adr-015-ai-agent-architecture.md) | AI Agent Quantitative Research Architecture | Proposed | 2026-05-04 |
| [ADR-016](adr/adr-016-multi-source-data-architecture.md) | Multi-Source Data Architecture | Proposed | 2026-05-17 |
| [ADR-017](adr/adr-017-observability-and-auth.md) | Observability Stack + API Authentication (前置 Phase 4) | Proposed | 2026-06-11 |
| [ADR-018](adr/adr-018-test-and-async-safety.md) | Testing Architecture + Async Safety + Determinism | Proposed | 2026-06-11 |
| [ADR-019](adr/adr-019-service-merge-ai-copilot.md) | Service 合并 + AI Copilot Sandbox 重构 | Proposed | 2026-06-11 |
| [ADR-020](adr/adr-020-engine-decomposition.md) | Engine God Object 拆分 + 函数式依赖注入 (含 Strategy 接口 ISP §6) | **Accepted** (P1-16~20,24) | 2026-06-11 |

---

## ODR Index — Operational Decisions

| ODR | Title | Status | Category | Date |
|-----|-------|--------|----------|------|
| [ODR-001](odr/odr-001-document-cleanup.md) | Document Cleanup Operation | Completed | Cleanup | 2026-04-09 |
| [ODR-002](odr/odr-002-design-doc-audit.md) | Design Document Audit Framework | Completed | Audit | 2026-04-09 |
| [ODR-003](odr/odr-003-agents-md-adoption.md) | AGENTS.md Adoption Decision | Completed | Migration | 2026-04-09 |
| [ODR-004](odr/odr-004-verification-standard.md) | Verification Standard Definition | Completed | Process | 2026-04-09 |
| [ODR-005](odr/odr-005-agents-md-v3-migration.md) | AGENTS.md v3.0 Migration to Template v2.0 | Completed | Migration | 2026-04-11 |
| [ODR-006](odr/odr-006-document-consolidation.md) | Document Consolidation & TASKS.md Creation | Completed | Cleanup | 2026-04-11 |
| [ODR-007](odr/odr-007-task-consolidation.md) | Task Consolidation & Document Migration | Completed | Migration | 2026-04-11 |
| [ODR-008](odr/odr-008-next-steps-archive.md) | NEXT_STEPS.md Archival to docs/archive/ | Completed | Cleanup | 2026-05-03 |
| [ODR-009](odr/odr-009-code-doc-audit.md) | 代码与文档对齐全面审查 | Completed | Audit | 2026-05-06 |
| [ODR-010](odr/odr-010-code-doc-audit-2026-05-17.md) | 2026-05-17 全项目代码与文档一致性审查 | Completed | Audit | 2026-05-17 |
| [ODR-011](odr/odr-011-multi-source-integration.md) | Multi-Source Data Integration (ashare-data-source-fetchers 整合) | Completed | Migration | 2026-05-17 → 2026-06-08 |
| [ODR-012](odr/odr-012-comprehensive-code-review.md) | Sprint 5 — 全项目综合代码审查 (代码质量/测试/文档一致性 4 维度) | Completed | Audit | 2026-06-08 |
| [ODR-013](odr/odr-013-comprehensive-audit-2026-06-11.md) | Sprint 6 — 全项目 4 维度综合审查 (业务/架构/代码/测试) | Accepted | Audit | 2026-06-11 |
| [ODR-014](odr/odr-014-sprint6-spec-migration.md) | Sprint 6 对齐审查 Spec 文件迁移 + 合并回长效文档 | Completed | Migration | 2026-06-11 |
| [ODR-015](odr/odr-015-p1-1-docs-consistency.md) | P1-1 文档一致化 — Strategy 接口 ISP 拆分 + Phase 3/4 编号统一 | Completed | Audit | 2026-06-12 |
| [ODR-016](odr/odr-016-p1-3-live-limit-orders.md) | P1-3 LiveEngine 限价单 (Limit / Stop / Trailing) — 撮合语义 + HWM 跟踪 | Completed | Implementation | 2026-06-12 |
| [ODR-017](odr/odr-017-p1-13-p1-14-ai-hardening.md) | P1-13 + P1-14 AI 研究闭环加固 — httpclient 弹性 (5 件套) + L5 人工审查 UI | Completed | Implementation | 2026-06-12 |
| [ODR-018](odr/odr-018-p1-5-p1-6-ashare-microstructure.md) | P1-5 + P1-6 A 股交易所微观结构 — 价格笼子 + 集合竞价 | Completed | Implementation | 2026-06-12 |
| [ODR-019](odr/odr-019-p1-2-rbac-jwt-auth.md) | P1-2 + P1-8 RBAC + JWT auth + audit_logs (HS256 / bcrypt cost 12 / 3 角色) | Completed | Implementation | 2026-06-12 |
| [ODR-020](odr/odr-020-p1-11-copilot-sandbox.md) | P1-11 AI Copilot 进程隔离 sandbox (subprocess + rlimit + 30s timeout + setsid) | Completed | Implementation | 2026-06-12 |
| [ODR-021](odr/odr-021-p1-15-service-merge-risk-execution.md) | P1-15 risk + execution 服务合并到 analysis (7→5 服务) — in-process 注入 + legacy alias + 12 TestXxx | Completed | Implementation | 2026-06-12 |
| [ODR-022](odr/odr-022-p1-26-execution-entity-consolidation.md) | P1-26 4 套执行实体合并 (5→2) — PersistentMockTrader/AdvancedMockTrader/AdvancedTrader 合并到 MockTrader + OrderStore 字段; -743 行 net | Completed | Refactor | 2026-06-12 |
| [ODR-023](odr/odr-023-p1-29-alert-manager.md) | P1-29 AlertManager — 6 类 P0 风险告警 (position_concentration / sector_concentration / drawdown / daily_loss_limit / order_failure_rate / risk_metric_breach) + LogChannel + WebhookChannel; 1326 行 + 25 TestXxx | Completed | Implementation | 2026-06-12 |
| [ODR-024](odr/odr-024-p1-30-copilot-e2e.md) | P1-30 E2E AI Copilot 端到端 + SSE 契约 — 13 TestXxx (7 UI + 4 API + 1 SSE + 1 混合); page.route stub 模式不依赖真实 AI; tsc strict + playwright list 全过 | Completed | Implementation | 2026-06-12 |
| [ODR-025](odr/odr-025-p2-alert-integration.md) | P2 alert 接入 — PeriodicAlertLoop (5min tick) + AlertHistory (ring buffer 100) + /api/alerts/{history,force-check,stats}; RecorderChannel.Snapshot/DrainAndReset; 16 TestXxx; in-process 注入零运维开销 | Completed | Implementation | 2026-06-12 |
| [ODR-026](odr/odr-026-p2-3-emergency-flatten.md) | P2-3 远程紧急平仓 kill-switch — LiveTrader.EmergencyFlatten 绕过 T+1 (BypassedT1 审计标记) + /api/execution/emergency-flatten 三重鉴权 (Bearer + body confirmation + 浏览器 confirm) + EmergencyFlatten.vue arm-and-confirm UI + 12 TestXxx 全过 race detector | Completed | Implementation | 2026-06-12 |
| [ODR-027](odr/odr-027-p2-1-p2-2-export-compare.md) | P2-1 HTML 自包含报告导出 (RenderHTML + 17 TestXxx) + P2-2 多策略对比 (`/api/backtest/compare?ids=` + 2-8 best 高亮 + BacktestCompare.vue 路由 + localStorage 持久化选择) | Completed | Implementation | 2026-06-12 |
| [ODR-028](odr/odr-028-p2-4-p2-5-p2-6-compliance.md) | P2-4 投资者适当性 (5 道门禁 + Registry) + P2-5 异常交易监控 (6 detector Orchestrator) + P2-6 大额交易报告 (单笔/累计 + 0600 落盘) — 共享 `pkg/compliance` 包 + 60 TestXxx race-clean | Completed | Implementation | 2026-06-13 |
| [ODR-029](odr/odr-029-p2-7-divestment-engine.md) | P2-7 减持规则引擎 — 5 类股东 (controlling/director/major_5pct/pre_ipo/placement) × 3 种方式 (auction/block/agreement) = 15 情形, 90 日 1%/2% 滚动窗 + 限售期半开区间 + 协议 ≥5% + 董监高 25% 年内 + 举牌 1%/5% 告警 — 33 单元 + 7 handler = 40 TestXxx race-clean | Completed | Implementation | 2026-06-13 |
| [ODR-030](odr/odr-030-p2-13-delisting-forced-liquidation.md) | P2-13 退市整理期 + 强制清仓引擎 — StockState 4 状态机 (Listed/Suspended/Delisting/Delisted) + StockStateRegistry + ForcedLiquidator (复用 P2-3 EmergencyFlatten + BypassedT1 审计) + 15 日整理期 + 5 日 LiquidationWindow + DryRun 模式 — 23 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-031](odr/odr-031-p2-14-take-profit.md) | P2-14 止盈三件套 — FixedTakeProfit (固定阈值) + TrailingTakeProfit (移动 HWM 跟踪) + TieredTakeProfit (分批 + 100 股取整) + TakeProfitChecker (Registry 模式) + 无状态 Rule + Position.Metadata 持久化 — 29 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-032](odr/odr-032-p2-15-corporate-action.md) | P2-15 公司行为 — CashDividend + BonusShare + CorporateActionSplit + RightsIssue (两阶段 Apply: 默认放弃 + ApplyPaid) + Placement + ActionEngine (apply 顺序 Split→Bonus→Rights→Cash→Placement, 同 ex-date 多 action 排序固定) + appliedLog 幂等去重 — 22 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-033](odr/odr-033-p2-16-api-versioning.md) | P2-16 API 版本化 — `pkg/api` 新建 leaf package + APIVersionMiddleware (URL 重写 + engine.HandleContext re-dispatch, gin radix-tree 路由在 middleware 前匹配必须 re-dispatch) + DeprecationHeader (RFC 8594 / 9745 / 8288 合规: Deprecation / Sunset / Link 头) + DiscoveryHandler + 两阶段迁移 (软 deprecation → LegacyRedirect 严格重定向) + 零侵入 13 handler 不改 — 25 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-034](odr/odr-034-p2-9-margin-trading.md) | P2-9 融资融券 + 做空 — MarginAccount + ShortableList (RWMutex) + MarginCalculator 纯函数 + 4 类操作 (MarginBuy/ShortSell/BuyToCover/MarginSell) + 利息计提 + 强制平仓 (130%) — 55 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-035](odr/odr-035-p2-10-convertible-bond.md) | P2-10 可转债策略 — 转股价值/纯债价值/溢价率/Delta + 强制赎回 (15/30) + 回售 (30 连续) + Strategy 接口实现 — 59 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-036](odr/odr-036-p2-11-options-pricing.md) | P2-11 期权定价 + Greeks — Black-Scholes (欧式) + Binomial CRR (美式) + 5 Greeks + ImpliedVol (Newton-Raphson) + NormCDF (A&S 近似) — 75 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-037](odr/odr-037-p2-12-hkex-northbound.md) | P2-12 港股通/北向因子 — EastmoneyNorthboundFetcher + 5 类北向因子 (MA/动量/持股变化/排名/信号) + ExchangeRateConverter — 75 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-038](odr/odr-038-p2-17-openapi-spec.md) | P2-17 OpenAPI 3.0 spec + Swagger UI — docs/openapi.yaml + /api/docs 端点 + Go embed 静态文件 + /api/version discovery — 5 TestXxx | Completed | Implementation | 2026-06-14 |
| [ODR-039](odr/odr-039-p2-18-p2-26-test-quality.md) | P2-18~P2-26 测试质量提升 — 9 个测试文件 (数据源集成/基因池持久化/风控边界/Pipeline E2E/领域类型/HTTP客户端/日志脱敏/LLM客户端/视觉回归) + 日志脱敏功能 — ~200+ TestXxx | Completed | Implementation | 2026-06-14 |
| [ODR-040](odr/odr-040-p2-27-p2-30-infrastructure.md) | P2-27~P2-30 基础设施 — WASM sandbox (InProcessRuntime 回退) + EventBus backpressure (drop-oldest) + 跨日状态持久化 (DiskStateStore) + Decimal 定点数 — 127 TestXxx race-clean | Completed | Implementation | 2026-06-14 |
| [ODR-041](odr/odr-041-p2-31-p2-33-code-quality.md) | P2-31~P2-33 代码质量 — 拆 3 个长函数为 11 个子函数 + 删 4 个空测试 + 替换 placeholder 为 7 个 SQL 构建子测试 | Completed | Implementation | 2026-06-14 |

---

## Status Legend

### ADR Status
| Status | Meaning |
|--------|---------|
| Draft | Under discussion, not yet decided |
| OPEN | Needs decision before a specific phase |
| PARTIAL | Partially resolved, some aspects still open |
| Accepted | Decided and implemented |
| Decided | Decided but not yet implemented |
| Superseded | Replaced by a later ADR |
| Proposed | Draft submitted, awaiting review/decision (Sprint 6+ style) |

### ODR Status
| Status | Meaning |
|--------|---------|
| Proposed | Draft, pending review |
| Accepted | Approved, execution pending |
| Completed | Execution finished, outcomes verified |
| Deprecated | No longer relevant, kept for history |
| Superseded | Replaced by a later ODR (link to replacement) |

---

## Future ADRs

| ADR | Topic | Phase | Note |
|-----|-------|-------|------|
| ~~ADR-017 (API auth)~~ | ~~Phase 5~~ | **Promoted to Sprint 6 via ADR-017** | 2026-06-11 |
| ADR-018 | Docker networking → Kubernetes service discovery | Phase 5 | 待定 |

---

## ODR Creation Guide

When to create an ODR:
- After completing a document cleanup, audit, or migration operation
- When making a process/tooling decision that affects how the team works
- When archiving or restructuring project documentation

ODR template: see `docs/odr/odr-001-document-cleanup.md` for the canonical example.

---
_Last updated by: AI Assistant — 2026-06-14 (P2-9~P2-12 + P2-17~P2-33 全部完成 → ODR-034~041 新建 Completed; Sprint 6 P2 累计 33 项全部 ✅)_
_ADR 累计 20 条: 架构 16 + 业务 1 (ADR-017) + 测试 1 (ADR-018) + 服务合并 1 (ADR-019) + 重构 1 (ADR-020)_
_ODR 累计 41 条: Cleanup 3 (ODR-001/006/008) | Audit 6 (ODR-002/009/010/012/013/015) | Migration 5 (ODR-003/005/007/011/014) | Process 1 (ODR-004) | Implementation 25 (ODR-016/017/018/019/020/021/023/024/025/026/027/028/029/030/031/032/033/034/035/036/037/038/039/040/041) | Refactor 1 (ODR-022)_
_2026-06-14 状态变更 (本次): ODR-034~041 新建 (P2-9 融资融券 + P2-10 可转债 + P2-11 期权 + P2-12 港股通 + P2-17 OpenAPI + P2-18~26 测试质量 + P2-27~30 基础设施 + P2-31~33 代码质量; P1-15/P1-30 状态修正 ✅; 预存测试失败修复 2 处)_
_2026-06-13 状态变更: ODR-029 新建 (P2-7 减持规则 40 TestXxx + 5 类股东 + 3 种方式完成); ODR-028 新建 (P2-4/5/6 合规三件套 60 TestXxx 完成); Sprint 6 P2 累计 4 项全部 ✅_
_2026-06-12 状态变更: ODR-027 新建 (P2-1 HTML 导出 + P2-2 多策略对比 17 TestXxx + 路由 + 持久化完成)_
_2026-06-12 状态变更: ODR-026 新建 (P2-3 紧急平仓 kill-switch 12 TestXxx + 三重鉴权完成)_
_2026-06-12 状态变更: ODR-025 新建 (P2 alert 接入 PeriodicAlertLoop + /api/alerts + 16 TestXxx 完成)_
_2026-06-12 状态变更: ODR-024 新建 (P1-30 E2E AI Copilot 13 TestXxx + SSE 契约完成); Sprint 6 P1 全部完成_
_2026-06-12 状态变更: ODR-023 新建 (P1-29 AlertManager 6 类 P0 风险告警 + Webhook 渠道完成)_
_2026-06-12 状态变更: ODR-022 新建 (P1-26 执行实体合并 5→2 完成, -743 行 net)_
_2026-06-12 状态变更: ODR-021 新建 (P1-15 服务合并 7→5 完成)_
_2026-06-12 状态变更: ADR-020 Proposed→Accepted (P1-16~20,24 全部完成); ODR-015 新建 (P1-1 文档一致化完成)_
_2026-06-11 状态变更: ADR-007 OPEN→Accepted, ADR-008 PARTIAL→Accepted, ADR-017~020 新建 Proposed, ODR-014 新建 Completed (Sprint 6 spec 迁移 + 内容合并回长效文档: ODR-013/VISION.md/TASKS.md)_
_docs/ 新增内容: VISION.md §Principle 8 (Documentation-Path Consistency) + TASKS.md §Sprint 6 启动期 待校核项 (6 项)_
_.trae/ 临时目录: 已清空并删除 (3 个文件迁至 docs/specs/ 后已合并回 ODR-013/VISION.md/TASKS.md)_
_docs/specs/ 临时目录: 已清空并删除 (内容合并至 ODR-013 §对齐审计复核 + VISION.md §Principle 8 + TASKS.md §Sprint 6 启动期 待校核项)_
