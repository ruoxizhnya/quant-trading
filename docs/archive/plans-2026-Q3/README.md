# S7 执行计划归档 (2026-Q3)

> **归档日期**: 2026-07-01
> **来源**: `.trae/documents/` — Trae IDE Plan Mode 生成的任务执行计划
> **关联 ODR**: [ODR-044](../../odr/odr-044-s7-plan-archive-and-pattern-extraction.md)

## 背景

这 14 个文件是 Sprint 7 (ODR-043 综合审计改进任务) 执行期间，通过 Trae IDE 的 Plan Mode 生成的详细实施计划。每个文件对应一个 S7-P* 任务，包含：现状分析、设计决策、原子提交步骤、验证清单、风险分析。

这些计划**不是设计文档**（设计决策已记录在 ADR/ODR 和 ARCHITECTURE.md/SPEC.md 中），而是**执行工件** — 记录了"如何做"的过程细节。它们的价值在于：

1. **可复现性** — 相同的重构手法可应用于其他 God Package
2. **风险分析** — 记录了 HIGH-risk 决策点及其缓解方案
3. **历史上下文** — 理解为什么代码是现在这个样子

## 文件清单

| 文件 | 对应任务 | 核心模式 |
|------|---------|---------|
| `s7-p1-4-fee-rate-unification.md` | S7-P1-4 | const aliasing 单一来源 |
| `s7-p2-1-backtest-leaf-extraction.md` | S7-P2-1 (总览) | leaf + aliases 提取手法 |
| `s7-p2-1-backtest-leaf-extraction-commits-4-8.md` | S7-P2-1 (Commits 4-8) | leaf + aliases 提取手法 |
| `s7-p2-1-commit8-job-extraction.md` | S7-P2-1 (Commit 8) | fakeRunner stub 断循环 |
| `s7-p3-1-expression-engine-expansion.md` | S7-P3-1 | 表达式引擎分层架构 |
| `s7-p3-1-expression-engine-finishing-phases.md` | S7-P3-1 (收尾) | 表达式引擎分层架构 |
| `s7-p3-2-yaml-expression-loader.md` | S7-P3-2 | YAML→Strategy 加载器 |
| `s7-p3-2-phase4-execute-from-yaml.md` | S7-P3-2 (Phase 4) | YAML→Strategy 加载器 |
| `s7-p3-3-tools-registry.md` | S7-P3-3 | 服务即工具提供方 |
| `s7-p3-3-phase5-6-completion.md` | S7-P3-3 (收尾) | 服务即工具提供方 |
| `s7-p3-4-domain-market-soft-layering.md` | S7-P3-4 | 软分层 + type alias view |
| `s7-p3-5-doc-drift-odr-021-sync.md` | S7-P3-5 | 文档漂移修复 |
| `s7-p3-5-continuation-resume.md` | S7-P3-5 (续) | 文档漂移修复 |
| `s7-p3-6-adr-status-sync.md` | S7-P3-6 | ADR 状态同步 |

## 提取的关键模式

详见 [ODR-044](../../odr/odr-044-s7-plan-archive-and-pattern-extraction.md) §Decision，其中记录了 5 个可复用的工程模式：

1. **Leaf + Aliases 提取** — God Package 拆分的零破坏手法
2. **Narrow Interface 解耦** — `EngineRunner` 单方法接口断循环
3. **Fake Stub 测试** — `fakeRunner` 模式断开测试中的 import cycle
4. **Soft Layering** — type alias view 实现增量迁移
5. **Test-First + Atomic Commit** — 每个任务 = 一个可独立构建的 commit
