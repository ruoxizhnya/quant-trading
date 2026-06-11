# Quant Lab — 统一任务追踪

> **Status**: Active (Long-Live Task Tracker)
> **Version:** 3.9.0 (Sprint 5 P2 pickup #6 — F1-new mutation 偶发)
> **Last Updated:** 2026-06-10
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [ROADMAP.md](ROADMAP.md) (sprint progress), [archive/NEXT_STEPS.md](archive/NEXT_STEPS.md) (audit archive)
>
> **Purpose**: 本文件是 Quant Lab 项目的**唯一活跃任务追踪源**。所有可执行任务必须在此记录，不得散落在其他文档中。
>
> **文档使用指南**:
> | 需求 | 应查阅文档 | 说明 |
> |------|-----------|------|
> | **查看当前待办任务** | **本文档** | 唯一活跃的任务追踪源，含 P0-P3 + D1-D7 |
> | **了解 Sprint 里程碑** | [ROADMAP.md](ROADMAP.md) | Phase/Sprint 级别进度和验收标准 |
> | **查看历史审查发现** | [archive/NEXT_STEPS.md](archive/NEXT_STEPS.md) | 2026-04-09 审查的只读归档 |
>
> **整合来源**: CODE\_REVIEW\_REPORT.md + NEXT\_STEPS.md + PHASE3-PLAN.md + AGENTS.md

***

## 任务状态说明

| 状态  | 图标 | 含义       |
| --- | -- | -------- |
| 待处理 | ⬜  | 未开始      |
| 进行中 | 🔵 | 正在执行     |
| 已完成 | ✅  | 已验证通过    |
| 已阻塞 | 🔴 | 有依赖或外部阻塞 |
| 已取消 | ⚫  | 不再需要执行   |

***

## 🔴 P0 — 必须立即修复（安全/数据完整性风险）

> **来源**: CODE\_REVIEW\_REPORT.md (2026-04-10)

| ID   | 任务                                         | 文件                                         | 状态 | 来源                   |
| ---- | ------------------------------------------ | ------------------------------------------ | -- | -------------------- |
| P0-1 | 修复 Copilot.vue XSS 漏洞 — 使用 DOMPurify 或文本插值 | `web/src/pages/Copilot.vue:7`              | ✅  | CODE\_REVIEW\_REPORT |
| P0-2 | 为批量 DB 操作添加事务保护                            | `pkg/storage/ohlcv.go`, `cache.go`         | ✅  | CODE\_REVIEW\_REPORT |
| P0-3 | 创建带超时的 HTTP 客户端 (30s timeout)              | `cmd/analysis/main.go`, `cmd/data/main.go` | ✅  | CODE\_REVIEW\_REPORT |
| P0-4 | 修复 syncCalendarHandler panic — 验证字符串长度     | `cmd/data/main.go:1023`                    | ✅  | CODE\_REVIEW\_REPORT |
| P0-5 | 添加 CORS + 速率限制中间件                          | `cmd/analysis/main.go`, `cmd/data/main.go` | ✅  | CODE\_REVIEW\_REPORT |
| P0-6 | 配置文件移除明文密码 — 改用环境变量                        | `config/analysis-service.yaml`             | ✅  | CODE\_REVIEW\_REPORT |

***

## 🟠 P1 — 尽快修复（代码质量/可维护性）

### 测试覆盖

| ID   | 任务                                | 目标           | 状态 | 来源                   |
| ---- | --------------------------------- | ------------ | -- | -------------------- |
| P1-1 | 提升 `pkg/data` 测试覆盖率               | 26.7% → 70%+ | ✅ | AGENTS.md            |
| P1-2 | 提升 `pkg/storage` 测试覆盖率            | 36.8% → 70%+ | ✅  | AGENTS.md            |
| P1-3 | 提升 `pkg/strategy` 测试覆盖率           | 12.3% → 70%+ | ✅  | NEXT\_STEPS          |
| P1-4 | 编写 `performance_test.go` — 绩效指标测试 | 新增测试文件       | ✅  | CODE\_REVIEW\_REPORT |
| P1-5 | 编写 `tracker_test.go` — 交易执行测试     | 新增测试文件       | ✅  | CODE\_REVIEW\_REPORT |
| P1-6 | 补充 9 项关键缺失 E2E 测试 (T-01\~T-09)    | e2e/tests/   | ✅  | NEXT\_STEPS          |

### 代码质量

| ID    | 任务                                    | 文件                               | 状态 | 来源                   |
| ----- | ------------------------------------- | -------------------------------- | -- | -------------------- |
| P1-7  | 拆分 "上帝文件" main.go                     | `cmd/analysis/main.go` (\~1347行) | ✅  | CODE\_REVIEW\_REPORT |
| P1-8  | 提取硬编码的服务 URL 和配置到配置文件                 | 多处                               | ✅  | CODE\_REVIEW\_REPORT |
| P1-9  | 修复 data-service 反向依赖 analysis-service | `cmd/data/main.go:1634`          | ✅  | CODE\_REVIEW\_REPORT |
| P1-10 | 明确 strategy-service 去留决策              | docker-compose, 架构文档             | ✅  | CODE\_REVIEW\_REPORT |
| P1-11 | 统一格式化函数 — 消除重复 fmtPercent             | `web/src/utils/format.ts`        | ✅  | NEXT\_STEPS          |
| P1-12 | API Client 统一错误处理 + AbortController   | `web/src/api/client.ts`          | ✅  | NEXT\_STEPS          |

### 前端重构（已完成）

| ID    | 任务                             | 文件                               | 状态 | 来源          |
| ----- | ------------------------------ | -------------------------------- | -- | ----------- |
| P1-13 | 拆分 BacktestEngine.vue 为子组件     | `web/src/components/backtest/*`  | ✅  | NEXT\_STEPS |
| P1-14 | 拆分 Dashboard.vue 为子组件          | `web/src/components/dashboard/*` | ✅  | NEXT\_STEPS |
| P1-15 | 回测结果持久化 — POST /backtest 写入 DB | `pkg/backtest/engine.go`         | ✅  | NEXT\_STEPS |

### 性能优化

| ID    | 任务                                              | 文件                                    | 状态 | 来源          |
| ----- | ----------------------------------------------- | ------------------------------------- | -- | ----------- |
| P1-16 | 批量化 regime/stoploss 调用 (1,260 serial → batched) | `pkg/risk/`, `pkg/backtest/engine.go` | ✅  | PHASE\_GATE |
| P1-17 | 向量化逐日处理 — 优化回测主循环性能                             | `pkg/backtest/engine.go`              | ✅  | PHASE\_GATE |

***

## 🟡 P2 — 计划修复（架构/文档改进）

### 文档同步

| ID   | 任务                                   | 文件                     | 状态 | 来源                   |
| ---- | ------------------------------------ | ---------------------- | -- | -------------------- |
| P2-1 | 统一 Strategy 接口定义 (VISION/SPEC/代码)    | 3 处                    | ✅  | NEXT\_STEPS          |
| P2-2 | 更新 SPEC.md 同步实际 API 端点               | `docs/SPEC.md`         | ✅  | CODE\_REVIEW\_REPORT |
| P2-3 | 更新 ARCHITECTURE.md 服务状态 + 前端架构       | `docs/ARCHITECTURE.md` | ✅  | NEXT\_STEPS          |
| P2-4 | 标注 risk/execution 服务为 Planned        | `docs/SPEC.md`         | ✅  | NEXT\_STEPS          |
| P2-5 | 修正 ROADMAP.md Phase 状态标记             | `docs/ROADMAP.md`      | ✅  | NEXT\_STEPS          |
| P2-6 | VISION.md 增加前端架构章节 (Vue SPA)         | `docs/VISION.md`       | ✅  | NEXT\_STEPS          |
| P2-7 | 创建 ADR-011: 前端架构决策 (HTML vs Vue SPA) | `docs/adr/`            | ✅  | NEXT\_STEPS          |

### 架构改进

| ID    | 任务                                | 描述                            | 状态 | 来源                   |
| ----- | --------------------------------- | ----------------------------- | -- | -------------------- |
| P2-8  | 精简 domain/types.go                | 拆分为 4 文件                      | ✅  | CODE\_REVIEW\_REPORT |
| P2-9  | 支持回测引擎水平扩展                        | `currentBacktest` 改为 map      | ✅  | CODE\_REVIEW\_REPORT |
| P2-10 | 提取代理端点通用函数                        | `proxyRequest()`              | ✅  | CODE\_REVIEW\_REPORT |
| P2-11 | 添加前端 404 路由                       | `web/src/router/`             | ✅  | CODE\_REVIEW\_REPORT |
| P2-12 | 移除前端生产环境调试代码                      | 37 处 console.log              | ✅  | CODE\_REVIEW\_REPORT |
| P2-13 | 修复 E2E 测试无效断言                     | `backtest-engine.spec.ts:382` | ✅  | CODE\_REVIEW\_REPORT |
| P2-14 | 修复 Dashboard static file path 不一致 | 确认路径一致，无需修复                   | ✅  | PHASE\_GATE          |

### 测试质量

| ID    | 任务                                | 描述                           | 状态 | 来源          |
| ----- | --------------------------------- | ---------------------------- | -- | ----------- |
| P2-14 | 增加 E2E 负向测试 (400/500 错误)          | e2e/tests/                   | ✅  | NEXT\_STEPS |
| P2-15 | 测试隔离 — 每个测试前清空 localStorage/store | e2e/tests/                   | ✅  | NEXT\_STEPS |
| P2-16 | 替换硬编码等待为智能等待                      | e2e/tests/                   | ✅  | NEXT\_STEPS |
| P2-17 | 增加回测结果对比测试 (run twice → same id?) | e2e/tests/                   | ✅  | NEXT\_STEPS |
| P2-18 | Dashboard HTML 存根标记 deprecated    | `cmd/analysis/static/*.html` | ✅  | PHASE\_GATE |

***

## 🟢 P3 — 持续改进

### 代码重构

| ID   | 任务                         | 描述                         | 状态 | 来源                   |
| ---- | -------------------------- | -------------------------- | -- | -------------------- |
| P3-1 | 提取 `isRebalanceDay()` 到公共包 | 3 个策略文件重复                  | ✅  | CODE\_REVIEW\_REPORT |
| P3-2 | 提取 `callScreenAPI()` 到公共包  | 2 个策略文件重复                  | ✅  | CODE\_REVIEW\_REPORT |
| P3-3 | 提取 `sampleData()` 到 utils  | 前端重复                       | ✅  | CODE\_REVIEW\_REPORT |
| P3-4 | 统一前端类型定义                   | `HistoryEntry`/`TradeInfo` | ✅  | CODE\_REVIEW\_REPORT |
| P3-5 | 前端 icon 组件使用 `markRaw()`   | 2 处遗漏                      | ✅  | CODE\_REVIEW\_REPORT |
| P3-6 | 修复 catch 子句类型              | `e: any` → `e: unknown`    | ✅  | CODE\_REVIEW\_REPORT |
| P3-7 | 提取 Magic Numbers 为命名常量     | 多处                         | ✅  | NEXT\_STEPS          |
| P3-8 | 减少 TypeScript `any` 类型使用   | 定义严格接口                     | ✅  | AGENTS.md            |
| P3-9 | 重构 engine.go 提取子方法降低复杂度    | `pkg/backtest/engine.go`   | ✅  | AGENTS.md            |

### 规范化

| ID    | 任务                                | 描述                               | 状态    | 来源                   |
| ----- | --------------------------------- | -------------------------------- | ----- | -------------------- |
| P3-10 | 统一 API 路径前缀                       | `/api/strategies` vs `/backtest` | ✅     | CODE\_REVIEW\_REPORT |
| P3-11 | 重命名 `doScreen` 并独立为 `screener.ts` | 命名不规范                            | ✅     | CODE\_REVIEW\_REPORT |
| P3-12 | 修复 Copilot prompt 中 Strategy 接口   | 缺少 3 个方法                         | ✅     | CODE\_REVIEW\_REPORT |
| P3-13 | 替换废弃的 `rand.Seed()`               | Go 1.20+ 废弃                      | ✅     | CODE\_REVIEW\_REPORT |
| P3-14 | 移除 `registry.go` 中的 `panic()`     | 改为返回 error                       | ✅     | CODE\_REVIEW\_REPORT |
| P3-15 | execution-service 订单持久化           | 内存 map → Redis/PG                | ✅     | CODE\_REVIEW\_REPORT |
| P3-16 | 引入 golang-migrate 工具              | 替代硬编码迁移                          | ✅     | CODE\_REVIEW\_REPORT |
| P3-17 | 为每个服务创建独立 Dockerfile              | 替代单一 Dockerfile                  | ✅     | CODE\_REVIEW\_REPORT |
| P3-18 | 完善 `pkg/live/` 实盘接口集成             | 接口预留                             | ✅     | AGENTS.md            |
| P3-19 | vnpy drift 对比验证（需要 vnpy 环境）       | 回测结果准确性验证                        | 🔴 阻塞 | PHASE\_GATE          |

***

## � Phase 3 实施任务

> **来源**: PHASE3-PLAN.md (2026-04-08)
> **状态**: 已批准，待实施

### D1: 多数据源适配器框架 (Week 1-2)

| ID    | 任务                                                         | 文件                                    | 状态 | 预估   |
| ----- | ---------------------------------------------------------- | ------------------------------------- | -- | ---- |
| D1-1  | 实现 DataEventBus (pub/sub)                                  | `pkg/marketdata/eventbus.go`          | ✅  | 0.5d |
| D1-2  | 增强 Provider 接口 (Name/CheckConnectivity/GetTradingCalendar) | `pkg/marketdata/provider.go`          | ✅  | 0.5d |
| D1-3  | TushareProvider 重构                                         | `pkg/marketdata/tushare_provider.go`  | ✅  | 0.5d |
| D1-4  | PostgresProvider 新增 (零网络延迟)                                | `pkg/marketdata/postgres_provider.go` | ✅  | 1d   |
| D1-5  | AkShareProvider 新增 (免费备选)                                  | `pkg/marketdata/akshare_provider.go`  | ✅  | 0.5d |
| D1-6  | HttpProvider 新增 (通用 HTTP 适配)                               | `pkg/marketdata/http_provider.go`     | ✅  | 0.5d |
| D1-7  | CachedProvider 装饰器 (Redis 缓存)                              | `pkg/marketdata/cached_provider.go`   | ✅  | 0.5d |
| D1-8  | DataAdapter 实现 (整合三层)                                      | `pkg/marketdata/adapter.go`           | ✅  | 1d   |
| D1-9  | Engine 集成 DataAdapter                                      | `pkg/backtest/engine.go`              | ✅  | 0.5d |
| D1-10 | Config + API (数据源切换)                                       | `config.yaml`, API handlers           | ✅  | 0.5d |

**D1 验收标准**:

- [ ] 切换到 postgres provider 后，500股回测 < 5s
- [ ] Tushare 不可用时自动 fallback 到 akshare
- [ ] 所有 55+ 现有测试通过
- [ ] API 可以在运行时切换数据源

### D2: 批量回测框架 (Week 2-3)

| ID   | 任务                           | 文件                             | 状态 | 预估 |
| ---- | ---------------------------- | ------------------------------ | -- | -- |
| D2-1 | 类型定义 (BatchTask/BatchResult) | `pkg/backtest/batch.go`        | ✅  | —  |
| D2-2 | CSV 任务解析                     | `pkg/backtest/batch_csv.go`    | ✅  | —  |
| D2-3 | BatchEngine (goroutine pool) | `pkg/backtest/batch.go`        | ✅  | —  |
| D2-4 | Scorer (评级 + OverfitScore)   | `pkg/backtest/batch_scorer.go` | ✅  | —  |
| D2-5 | Walk-Forward 集成              | `pkg/backtest/batch.go`        | ✅  | —  |
| D2-6 | 汇总报告生成                       | `pkg/backtest/batch.go`        | ✅  | —  |
| D2-7 | API 端点                       | `cmd/analysis/main.go`         | ✅  | —  |

**D2 验收标准**:

- [ ] 100 任务 (10股票×4策略×区间池) < 30s 完成
- [ ] 输出含评级 + OverfitScore + StabilityScore
- [ ] CSV 兼容金策格式 + 我们的扩展格式

### D3: Go Plugin 策略热加载 (Week 3-4)

| ID   | 任务                     | 文件                       | 状态 | 预估 |
| ---- | ---------------------- | ------------------------ | -- | -- |
| D3-1 | PluginLoader 实现        | `pkg/strategy/loader.go` | ✅  | —  |
| D3-2 | Load/Unload/Reload API | `pkg/strategy/loader.go` | ✅  | —  |
| D3-3 | 示例插件                   | `pkg/strategy/plugins/`  | ✅  | —  |
| D3-4 | API 端点                 | `cmd/analysis/main.go`   | ✅  | —  |
| D3-5 | 文档更新                   | `docs/`                  | ✅  | —  |

**D3 验收标准**:

- [ ] 动态加载 .so 策略，立即生效
- [ ] Reload 后新代码生效
- [ ] 提供 Makefile 一键编译插件

### D4: 实盘交易接口预留 (Week 4)

| ID   | 任务              | 文件                        | 状态 | 预估 |
| ---- | --------------- | ------------------------- | -- | -- |
| D4-1 | LiveTrader 接口定义 | `pkg/live/trader.go`      | ✅  | —  |
| D4-2 | MockTrader 实现   | `pkg/live/mock_trader.go` | ✅  | —  |
| D4-3 | Engine 预留实盘接口   | `pkg/backtest/engine.go`  | ✅  | —  |
| D4-4 | 文档更新            | `docs/`                   | ✅  | —  |

**D4 验收标准**:

- [ ] LiveTrader 接口编译通过
- [ ] MockTrader 可用于测试/Paper Trading
- [ ] 文档清楚描述接入规范

### D5: 更多实战策略插件 (Week 5-6)

| ID   | 任务                       | 描述                               | 状态 | 预估   |
| ---- | ------------------------ | -------------------------------- | -- | ---- |
| D5-1 | TD Sequential (神奇九转)     | 价格序列模式计数                         | ✅  | ≤ 3s |
| D5-2 | Bollinger Mean Reversion | BB位置 + RSI                       | ✅  | ≤ 3s |
| D5-3 | Volume-Price Trend       | 量价配合度 + MA共振                     | ✅  | ≤ 3s |
| D5-4 | Volatility Breakout      | ATR突破 + 方向过滤                     | ✅  | ≤ 3s |
| D5-5 | 单元测试 (每个策略 ≥ 3 个)        | `pkg/strategy/plugins/*_test.go` | ✅  | —    |

**D5 验收标准**:

- [ ] 4 个新策略注册到 GlobalRegistry
- [ ] 每个策略 ≥ 3 个单元测试
- [ ] FactorCache 加速生效

### D6: AI Copilot 深度集成 (Week 6-7)

| ID   | 任务           | 描述             | 状态 | 预估 |
| ---- | ------------ | -------------- | -- | -- |
| D6-1 | LLM 意图解析     | 中文自然语言 → 策略参数  | ✅  | 2026-05-05 |
| D6-2 | YAML 生成      | 参数 → YAML 配置   | ✅  | 2026-05-05 |
| D6-3 | Pipeline 集成  | 解析 → 编译验证 → 回测 | ✅  | 2026-05-05 |
| D6-4 | Dashboard 集成 | 前端 UI 更新       | ✅  | 2026-05-05 |

**D6 验收标准**:

- [ ] 中文描述 → 30s 内得到回测结果
- [ ] ≥ 5 种策略描述正确解析

### D7: 数据同步增强 (ADR-013) (Week 7-9)

> **依赖**: ADR-003 (Background Worker), ADR-006 (Job Queue)
> **设计文档**: [docs/design/pages/data-sync.md](../design/pages/data-sync.md)
> **ADR**: [docs/adr/adr-013-data-sync-enhancement.md](../adr/adr-013-data-sync-enhancement.md)

#### Phase 1: 后端任务队列 (Week 7)

| ID    | 任务                                                         | 文件                                    | 状态 | 预估   | 依赖 |
| ----- | ---------------------------------------------------------- | ------------------------------------- | -- | ---- | -- |
| D7-1  | 创建 `sync_jobs` 表迁移脚本                                    | `migrations/012_add_sync_jobs_table.sql` | ✅  | 0.5d | —  |
| D7-2  | 创建 `sync_schedules` 表迁移脚本                               | `migrations/013_add_sync_schedules_table.sql` | ✅  | 0.5d | D7-1 |
| D7-3  | 实现 `pkg/sync/job.go` — 任务模型和状态机                        | `pkg/sync/job.go`                     | ✅  | 0.5d | D7-1 |
| D7-4  | 实现 `pkg/sync/queue.go` — PostgreSQL 队列管理                  | `pkg/sync/queue.go`                   | ✅  | 1d   | D7-3 |
| D7-5  | 实现 `pkg/sync/worker.go` — Worker goroutine pool              | `pkg/sync/worker.go`                  | ✅  | 1d   | D7-4 |
| D7-6  | 改造现有 `/sync/*` 端点为任务创建模式（保持向后兼容）                  | `cmd/data/main.go`                    | ✅  | 1d   | D7-5 |
| D7-7  | 新增 `/api/sync/*` REST API 端点                             | `cmd/data/sync_handlers.go` (新建)     | ✅  | 1d   | D7-6 |
| D7-8  | 实现 SSE 进度推送端点 `/api/sync/stream`                        | `cmd/data/sync_handlers.go`           | ✅  | 0.5d | D7-7 |
| D7-9  | 后端单元测试 (job/queue/worker 覆盖率 ≥ 70%)                     | `pkg/sync/*_test.go`                  | ✅  | 1d   | D7-5 |

#### Phase 2: 定时调度器 (Week 8)

| ID    | 任务                                                         | 文件                                    | 状态 | 预估   | 依赖 |
| ----- | ---------------------------------------------------------- | ------------------------------------- | -- | ---- | -- |
| D7-10 | 集成 `robfig/cron/v3` 库                                    | `go.mod`                              | ✅  | 0.5d | —  |
| D7-11 | 实现 `pkg/sync/scheduler.go` — 定时调度器核心                   | `pkg/sync/scheduler.go`               | ✅  | 1d   | D7-10 |
| D7-12 | 实现调度配置 CRUD API (`/api/sync/schedules`)                  | `cmd/data/sync_handlers.go`           | ✅  | 0.5d | D7-11 |
| D7-13 | 调度器与任务队列集成 (创建任务时关联 schedule_id)                  | `pkg/sync/scheduler.go`               | ✅  | 0.5d | D7-11 |
| D7-14 | 调度器持久化与恢复 (服务重启后恢复定时任务)                        | `pkg/sync/scheduler.go`               | ✅  | 0.5d | D7-13 |
| D7-15 | 调度器单元测试                                               | `pkg/sync/scheduler_test.go`          | ✅  | 0.5d | D7-11 |

#### Phase 3: 前端 UI (Week 8-9)

| ID    | 任务                                                         | 文件                                    | 状态 | 预估   | 依赖 |
| ----- | ---------------------------------------------------------- | ------------------------------------- | -- | ---- | -- |
| D7-16 | 创建 `web/src/types/sync.ts` — 同步相关 TypeScript 类型        | `web/src/types/sync.ts`               | ✅  | 0.5d | —  |
| D7-17 | 创建 `web/src/api/sync.ts` — 同步 API 客户端                  | `web/src/api/sync.ts`                 | ✅  | 0.5d | D7-16 |
| D7-18 | 创建 `web/src/stores/sync.ts` — Pinia Store                  | `web/src/stores/sync.ts`              | ✅  | 0.5d | D7-17 |
| D7-19 | 创建 `SyncOverviewCards.vue` — 数据概览卡片                    | `web/src/components/sync/SyncOverviewCards.vue` | ✅  | 0.5d | D7-18 |
| D7-20 | 创建 `SyncControlPanel.vue` — 同步控制面板                     | `web/src/components/sync/SyncControlPanel.vue` | ✅  | 1d   | D7-19 |
| D7-21 | 创建 `SyncJobQueue.vue` — 同步任务队列                         | `web/src/components/sync/SyncJobQueue.vue` | ✅  | 1d   | D7-20 |
| D7-22 | 创建 `SyncLogViewer.vue` — 同步日志查看器                      | `web/src/components/sync/SyncLogViewer.vue` | ✅  | 0.5d | D7-21 |
| D7-23 | 创建 `DataQualityDashboard.vue` — 数据质量仪表盘                | `web/src/components/sync/DataQualityDashboard.vue` | ✅  | 1d   | D7-22 |
| D7-24 | 创建 `pages/DataSync.vue` — 数据同步管理页面                   | `web/src/pages/DataSync.vue`          | ✅  | 1d   | D7-19~D7-23 |
| D7-25 | 添加路由 `/data-sync` 和侧边栏导航入口                          | `web/src/router/index.ts`, `AppSidebar.vue` | ✅  | 0.5d | D7-24 |
| D7-26 | 集成 SSE 实时进度推送                                        | `web/src/stores/sync.ts`              | ✅  | 0.5d | D7-24 |
| D7-27 | 前端 Vitest 单元测试 (组件覆盖率 ≥ 60%)                         | `web/src/components/sync/*.spec.ts`   | ✅  | 1d   | D7-24 |

#### Phase 4: 集成测试与文档 (Week 9)

| ID    | 任务                                                         | 文件                                    | 状态 | 预估   | 依赖 |
| ----- | ---------------------------------------------------------- | ------------------------------------- | -- | ---- | -- |
| D7-28 | E2E 测试：完整同步流程 (创建 → 执行 → 完成 → 验证)                | `e2e/tests/data-sync.spec.ts`         | ✅  | 1d   | D7-24 |
| D7-29 | E2E 测试：定时任务配置与触发验证                                  | `e2e/tests/data-sync-schedule.spec.ts` | ✅  | 0.5d | D7-28 |
| D7-30 | E2E 测试：失败重试与错误处理                                     | `e2e/tests/data-sync-error.spec.ts`   | ✅  | 0.5d | D7-28 |
| D7-31 | 性能测试：批量同步 5000+ 股票 OHLCV                           | `pkg/sync/bench_test.go`              | ✅  | 0.5d | D7-5  |
| D7-32 | 故障注入测试：网络中断、Tushare 限流                            | `pkg/sync/fault_test.go`              | ✅  | 0.5d | D7-5  |
| D7-33 | 更新 SPEC.md 新增 API 文档                                    | `docs/SPEC.md`                        | ✅  | 0.5d | D7-7  |
| D7-34 | 更新 AGENTS.md 数据流架构图                                   | `AGENTS.md`                           | ✅  | 0.5d | D7-7  |
| D7-35 | 运行 `go vet ./... && go test ./...` 确保后端质量              | ✅  | 0.5d | D7-9  |
| D7-36 | 运行 `npm run lint && npm run typecheck` 确保前端质量          | ✅  | 0.5d | D7-27 |

**D7 验收标准**:

- [ ] 定时同步每日 09:00 自动执行 OHLCV 增量同步
- [ ] 前端 Data Sync 页面可查看所有数据类型的覆盖度和同步状态
- [ ] 同步任务队列支持创建/取消/重试操作
- [ ] SSE 实时推送进度，前端进度条平滑更新
- [ ] 同步成功率 > 99%，失败任务可自动重试(最多3次)
- [ ] `sync_jobs` 表自动归档(保留30天)，不影响查询性能
- [ ] 所有新增代码通过 lint + typecheck + 单元测试
- [ ] E2E 测试覆盖完整同步流程、定时任务、错误恢复

***

## 🔴 P0 — 必须立即修复（2026-05-17 代码与文档审查发现）

> **来源**: ODR-010 (2026-05-17 全项目代码+文档一致性审查)

| ID   | 任务                                          | 文件                                                  | 状态 | 来源       |
| ---- | ------------------------------------------- | --------------------------------------------------- | -- | -------- |
| P0-7 | 修复 `TestNewPostgresStore` 测试失败 — 连接或 fixture 问题 | `pkg/storage/postgres_test.go`                      | ✅  | ODR-010  |
| P0-8 | 修复 `TestScreenCache_Eviction` 测试失败 — 断言逻辑      | `pkg/strategy/utils_test.go:105`                    | ✅  | ODR-010  |

***

## 🟠 P1 — 尽快修复（2026-05-17 审查发现）

### 文档-代码命名统一

| ID   | 任务                            | 影响文件                                                                                                                                          | 状态 | 来源       |
| ---- | ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -- | -------- |
| P1-19 | 修正 `backtest_runs` → `backtest_jobs`（文档 10 处引用） | `AGENTS.md`, `docs/VISION.md`, `docs/ROADMAP.md`, `docs/TEST.md`, `docs/adr/adr-003-background-worker.md`, `docs/adr/adr-006-job-queue.md`, `docs/adr/adr-013-data-sync-enhancement.md` | ✅ | ODR-010 |

### 测试覆盖率数据校准

| ID    | 任务                                  | 现状文档           | 实测值                  | 状态 | 来源       |
| ----- | ----------------------------------- | -------------- | -------------------- | -- | -------- |
| P1-20 | 更新 `pkg/ai` 覆盖率口径（子包分别 30-95%）     | AGENTS.md 75%+ | 顶层 0%/子包平均 ~67%      | ✅ | ODR-010  |
| P1-21 | 更新 `pkg/live` 覆盖率声明                | AGENTS.md 0%   | 实测 52.3%              | ✅ | ODR-010  |
| P1-22 | 更新 `pkg/backtest` 覆盖率声明             | AGENTS.md 72.5% | 实测 67.8%              | ✅ | ODR-010  |
| P1-23 | 更新 `pkg/storage` 覆盖率声明              | AGENTS.md 36.8% | 测试失败导致 2.4%        | ✅ | ODR-010  |

### 服务状态澄清

| ID    | 任务                              | 文件                | 状态 | 来源       |
| ----- | ------------------------------- | ----------------- | -- | -------- |
| P1-24 | 明确 strategy-service 状态（"备用" vs 实际运行） | `docs/ARCHITECTURE.md`, `AGENTS.md` | ✅ | ODR-010  |

***

## 🟡 P2 — 计划修复（2026-05-17 审查发现）

### 数据库文档同步

| ID   | 任务                              | 文件                  | 状态 | 来源       |
| ---- | ------------------------------- | ------------------- | -- | -------- |
| P2-19 | 同步 ARCHITECTURE.md 数据模型（24 → 18 表） | `docs/ARCHITECTURE.md` 第 295-400 行 | ✅ | ODR-010  |

### Phase 4 验收

| ID   | 任务                                | 文件                                | 状态 | 来源       |
| ---- | --------------------------------- | --------------------------------- | -- | -------- |
| P2-20 | 对照 ADR-015 5 项验收标准逐项核验 Phase 4 完成度 (98%) | `docs/adr/adr-015-ai-agent-architecture.md` | ✅ | ODR-010  |

***

## 🔴 Sprint 1 P0 — Multi-Source Data Integration (2026-05-17 → 2026-06-08 ✅ Completed)

> **来源**: ODR-011 + ADR-016 | **关联项目**: `../Ashare-data-source-fetchers` (SKILL.md V3.2.2)
> **目标**: 引入 mootdx 实时 + 东财 push2 资金流

| ID    | 任务                                  | 文件                                  | 状态 | 来源       |
| ----- | ----------------------------------- | ----------------------------------- | -- | -------- |
| MS-1  | 迁移 014: 给所有数据表加 source/ingest_time 列 | `migrations/014_add_source_columns.sql` | ✅ | ODR-011 |
| MS-2  | 迁移 015: realtime_quote + ohlcv_minute + capital_flow hypertable | `migrations/015_add_realtime_and_capital_flow.sql` | ✅ | ODR-011 |
| MS-3  | 定义 DataSourceAdapter 接口           | `pkg/data/source/adapter.go`          | ✅ | ODR-011 |
| MS-4  | 实现 Registry + 降级链管理            | `pkg/data/source/registry.go`        | ✅ | ODR-011 |
| MS-5  | ETL Pipeline (Normalize→Validate→Persist) | `pkg/data/source/etl.go`             | ✅ | ODR-011 |
| MS-6  | UnifiedDataPoint 数据模型             | `pkg/data/source/unified.go`         | ✅ | ODR-011 |
| MS-7  | 重构 TushareClient 为 TushareAdapter   | `pkg/data/source/tushare_adapter.go` | ✅ | ODR-011 |
| MS-8  | 实现 mootdx SDK 适配器 (实时/五档/逐笔)  | `pkg/data/source/mootdx_adapter.go`  | ✅ | ODR-011 |
| MS-9  | 实现东财 push2 资金流适配器 (分钟级)     | `pkg/data/source/eastmoney_adapter.go` | ✅ | ODR-011 |
| MS-10 | storage 层新增 BulkInsert             | `pkg/storage/bulk_insert.go`         | ✅ | ODR-011 |
| MS-11 | cmd/data 初始化 Registry              | `cmd/data/main.go` + `cmd/data/registry_init.go` | ✅ | ODR-011 |

***

## 🟠 Sprint 2 P1 — 板块 + 龙虎榜 (✅ Completed)

| ID    | 任务                                  | 文件                                  | 状态 | 来源       |
| ----- | ----------------------------------- | ----------------------------------- | -- | -------- |
| MS-12 | 迁移 016: sectors, top_list, limit_up_pool | `migrations/016_add_sectors_and_toplist.sql` | ✅ | ODR-011 |
| MS-13 | 东财 slist 概念板块适配器              | `pkg/data/source/eastmoney_sectors_adapter.go` | ✅ | ODR-011 |
| MS-14 | 东财龙虎榜/涨停池适配器                | `pkg/data/source/eastmoney_sectors_adapter.go` (EastmoneyTopListAdapter) | ✅ | ODR-011 |

***

## 🟡 Sprint 3 P1 — 公告 + 舆情 (✅ Completed)

| ID    | 任务                                  | 文件                                  | 状态 | 来源       |
| ----- | ----------------------------------- | ----------------------------------- | -- | -------- |
| MS-15 | 迁移 017: announcements, news, hot_search | `migrations/017_add_announcements_news_hotsearch.sql` | ✅ | ODR-011 |
| MS-16 | 巨潮公告适配器 (orgId 动态获取)        | `pkg/data/source/juchao_adapter.go`  | ✅ | ODR-011 |
| MS-17 | 雪球热搜适配器                         | `pkg/data/source/xueqiu_adapter.go`  | ✅ | ODR-011 |

***

## 🟢 Sprint 4 P3 — 全球扩展 (✅ Completed)

| ID    | 任务                                  | 文件                                  | 状态 | 来源       |
| ----- | ----------------------------------- | ----------------------------------- | -- | -------- |
| MS-18 | Alpha Vantage 适配器 (TIME_SERIES_DAILY_ADJUSTED) | `pkg/data/source/alpha_vantage_adapter.go` | ✅ | ODR-011 |
| MS-19 | Yahoo Finance 适配器 (chart 端点)    | `pkg/data/source/yahoo_finance_adapter.go` | ✅ | ODR-011 |
| MS-19b | 迁移 018: global_ohlcv hypertable   | `migrations/018_add_global_ohlcv.sql` | ✅ | ODR-011 |

***

## 🧪 验证与测试 (✅ Completed)

| ID    | 任务                                  | 文件                                  | 状态 | 来源       |
| ----- | ----------------------------------- | ----------------------------------- | -- | -------- |
| MS-20 | L1 单元测试：validate / IsRetryable / AdapterBase / 接口合规 | `pkg/data/source/adapter_test.go`   | ✅ | ODR-011 |
| MS-20b | L1/L2 transport 行为测试 (mockAdapter) | `pkg/data/source/source_test.go`    | ✅ | ODR-011 |
| MS-21 | L2 集成测试：Adapter → ETL → DB      | `pkg/data/source/etl_test.go`       | ✅ | ODR-011 |
| MS-22 | L3 多源一致性 IC 测试 (capital_flow / sector_rotation / hot_search) | `pkg/data/source/ic_test.go` | ✅ | ODR-011 |
| MS-23 | L4 资金流因子 + 单元测试                  | `pkg/ai/factor/capital_flow.go` + `_test.go` | ✅ | ODR-011 |
| MS-24 | L4 板块轮动因子 + 单元测试 (含 as-of 过滤)   | `pkg/ai/factor/sector_rotation.go` + `_test.go` | ✅ | ODR-011 |
| MS-25 | L4 舆情因子 + 单元测试 (含时间衰减)         | `pkg/ai/factor/sentiment.go` + `_test.go` | ✅ | ODR-011 |

***

## 🌐 HTTP 端点 (✅ Completed)

| ID    | 任务                                  | 文件                                  | 状态 | 来源       |
| ----- | ----------------------------------- | ----------------------------------- | -- | -------- |
| MS-26 | `/api/datasource/registry/{status,health,chains}` | `cmd/data/registry_handlers.go` | ✅ | ODR-011 |

***

## 🔴 Sprint 5 P0 — 全项目综合代码审查 (2026-06-08 ⏳ Discovered)

> **来源**: 用户请求 (代码质量 + 测试 + 文档一致性 + 任务记录 4 维度)
> **方法**: 3 个子代理并行审查 (后端 Go / 前端 Vue / 文档一致性),交叉验证关键发现
> **结果**: 发现 **53 项** 高置信度问题,其中 **P0×16, P1×20, P2×14, P3×4**
> **配套**: 待创建 ODR-012 综合代码审查

### P0 Critical — 16 项 (2026-06-08 全部修复 ✅)

> 修复验证: `go vet`/`go build`/`go test ./pkg/storage/... ./pkg/data/source/... ./cmd/data/...` 全通过;
> `vue-tsc --noEmit` 无错; `npm test` 78/78 通过; `npm run build` 成功。

| ID    | 任务                                       | 文件                                                                                  | 状态 | 来源       |
| ----- | ---------------------------------------- | ----------------------------------------------------------------------------------- | -- | -------- |
| CR-01 | `BulkInsert` 结果循环使用 `len(valid)` 而实际 batch 较短,导致错误/计数错位            | [pkg/storage/bulk_insert.go:253](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/storage/bulk_insert.go#L253) | ✅ | B-001 |
| CR-02 | `snapshotStatus` 持锁跨越 `HealthCheck` 网络 I/O — 修复未生效              | [cmd/data/registry_handlers.go:60-71](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/data/registry_handlers.go#L60) | ✅ | B-002 |
| CR-03 | `RetailRatio` 公式无意义 (`-100 * (1 - MainNetRatio/100)` 与 retail 无关) | [pkg/data/source/eastmoney_adapter.go:342-344](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_adapter.go#L342) | ✅ | B-003 |
| CR-04 | `api/backtest.ts` 双函数 POST 同一端点但 schema 不同                  | [web/src/api/backtest.ts:20-25](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/backtest.ts#L20) | ✅ | F-001 |
| CR-05 | `BacktestResultCard.vue` 重复定义 `formatPercent` / `formatNumber`    | [components/ai/BacktestResultCard.vue:36-44](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/BacktestResultCard.vue#L36) | ✅ | F-002 |
| CR-06 | `FactorCard.vue` 重复定义 `formatMetric` / `formatPercent` (行为不一致) | [components/ai/FactorCard.vue:129-137](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/FactorCard.vue#L129) | ✅ | F-003 |
| CR-07 | `PaperTrading.vue` 显式 `DataTableColumns<any>` + 3 个 `catch (error: any)` | [pages/PaperTrading.vue:247](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/PaperTrading.vue#L247) | ✅ | F-004, F-005 |
| CR-08 | `types/api.ts` `BacktestJob.params: Record<string, any>` (公开类型)     | [web/src/types/api.ts:67](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/types/api.ts#L67) | ✅ | F-006 |
| CR-09 | `PipelineDashboard.vue` `jobHistory` 累积无上限 (潜在内存泄漏)        | [components/ai/PipelineDashboard.vue:308](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/PipelineDashboard.vue#L308) | ✅ | F-007 |
| CR-10 | ADR.md 索引缺失 6 条已存在的 ADR (ADR-011~016)                       | [docs/ADR.md:13-25](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/ADR.md#L13) | ✅ | D-001 |
| CR-11 | SPEC.md Backtest API 路径缺少 `/api` 前缀 (与实际 `/api/backtest` 不一致) | [docs/SPEC.md:543-560](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L543) | ✅ | D-002 |
| CR-12 | SPEC.md 把 `/api/datasource/*` 错误归在 AI Service,实际 Analysis + Data | [docs/SPEC.md:711-715](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L711) | ✅ | D-003 |
| CR-13 | Data Service 实际 30+ 端点未在 SPEC.md 记录 (sync/factor/screen 全部)   | [docs/SPEC.md:395-435](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L395) | ✅ | D-004 |
| CR-14 | ARCHITECTURE.md "6 张核心表" 与内部 "18 张活跃表" 自相矛盾              | [docs/ARCHITECTURE.md:297](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/ARCHITECTURE.md#L297) | ✅ | D-005 |
| CR-15 | ODR-011 Sprint 1-4 新增的 13 张表 (realtime_quote/capital_flow/sectors/...) 未在 ARCHITECTURE.md | [docs/ARCHITECTURE.md:387-398](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/ARCHITECTURE.md#L387) | ✅ | D-006 |
| CR-16 | SPEC.md AI Research Service 章节列出 35+ 端点但实际只注册 3 个             | [docs/SPEC.md:647-723](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L647) | ✅ | D-011 |

### P1 High — 20 项 ✅ 2026-06-10 完成 (ODR-012)

| ID    | 任务                                       | 文件                                                                                  | 状态 | 来源       |
| ----- | ---------------------------------------- | ----------------------------------------------------------------------------------- | -- | -------- |
| CR-17 | `mootdx.fetchRealtime` 注释承诺"按市场批量"但实际 N 次串行调用            | [pkg/data/source/mootdx_adapter.go:212-234](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/mootdx_adapter.go#L212) | ✅ | B-004 |
| CR-18 | `pkg/storage/bulk_insert.go` (13KB) 0 单元测试覆盖 — 新写入路径无保护      | [pkg/storage/bulk_insert.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/storage/bulk_insert.go) | ✅ | B-005 |
| CR-19 | `EastmoneyTopListAdapter.fetchLimitUpPool` 4 字段硬编码 1,数据真实性归零  | [pkg/data/source/eastmoney_sectors_adapter.go:463-475](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_sectors_adapter.go#L463) | ✅ | B-006 |
| CR-20 | `Registry.HealthCheck` 串行执行,7 adapter × 5s = 最坏 35s 阻塞        | [pkg/data/source/registry.go:191-212](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/registry.go#L191) | ✅ | B-007 |
| CR-21 | `etl_test.go` stubStore 接口签名与真实 `PostgresStore.BulkInsert` 不兼容,集成测试零覆盖 | [pkg/data/source/etl_test.go:23-30](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/etl_test.go#L23) | ✅ | B-008 |
| CR-22 | `DetailMetrics.vue` `props` 声明但从未使用 (ESLint 警告)              | [components/backtest/DetailMetrics.vue:15-19](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/backtest/DetailMetrics.vue#L15) | ✅ | F-008 |
| CR-23 | `FitnessChart.vue` `Math.max(...arr)` 在大数组栈溢出 (>200 代)       | [components/ai/FitnessChart.vue:55-60](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/FitnessChart.vue#L55) | ✅ | F-009 |
| CR-24 | `GenealogyTree.vue` 同样 `Math.max(...arr)` 栈溢出风险                  | [components/ai/GenealogyTree.vue:69-70](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/GenealogyTree.vue#L69) | ✅ | F-010 |
| CR-25 | `api/client.ts` 每次请求都 `addEventListener('pagehide')` 但从不 remove | [api/client.ts:57](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/client.ts#L57) | ✅ | F-011 |
| CR-26 | `stores/sync.ts` SSE 重连时旧连接未 close (泄漏)                    | [stores/sync.ts:117-127](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/stores/sync.ts#L117) | ✅ | F-012 |
| CR-27 | `FitnessChart.vue` resize 监听器无清理 (组件销毁后仍执行)             | [components/ai/FitnessChart.vue:200-203](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/FitnessChart.vue#L200) | ✅ | F-013 |
| CR-28 | `TradeTable.vue` (用户核心组件) + `useAsyncBacktest.ts` 0 测试覆盖   | [components/backtest/TradeTable.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/backtest/TradeTable.vue) | ✅ | Test Gap |
| CR-29 | SPEC.md Analysis Service 章节遗漏 30+ 实际 plugin/paper trading 端点 | [docs/SPEC.md:535-600](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L535) | ✅ | D-007 |
| CR-30 | ADR-015 引用 `*_agent.go` 实际文件无后缀 (`research.go` 等)         | [docs/adr/adr-015-ai-agent-architecture.md:193-196](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/adr/adr-015-ai-agent-architecture.md#L193) | ✅ | D-008 |
| CR-31 | ADR-016 迁移文件名引用错位 (off-by-one,全部小 1)                     | [docs/adr/adr-016-multi-source-data-architecture.md:351-355](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/adr/adr-016-multi-source-data-architecture.md#L351) | ✅ | D-009 |
| CR-32 | ODR-011 声称 "8 个新数据源" 实际注册 9 个 adapter (含 Eastmoney 3 slot) | [docs/odr/odr-011-multi-source-integration.md:158-159](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/odr/odr-011-multi-source-integration.md#L158) | ✅ | D-010 |
| CR-33 | Strategy 接口 Signal 类型三处文档 (SPEC/VISION/AGENTS) 未指定包名     | [docs/SPEC.md:160-172](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L160) | ✅ | D-012 |
| CR-34 | SPEC.md/VISION.md 声称 "ai 覆盖率 ≥75%" 实际 0% 顶层,16-95% 子包     | [docs/SPEC.md:30-37](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L30) | ✅ | D-013 |
| CR-35 | ADR-015/016 Status 仍为 "Proposed" 但实施已 98% 完成                  | [docs/adr/adr-015-ai-agent-architecture.md:3](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/adr/adr-015-ai-agent-architecture.md#L3) | ✅ | D-014 |
| CR-36 | AGENTS.md 技术栈表缺失 risk-service(8083)/execution-service(8084) | [AGENTS.md:23-32](file:///Users/ruoxi/longshaosWorld/quant-trading/AGENTS.md#L23) | ✅ | D-015 |

### P2 Medium — 14 项 (Backlog)

| ID    | 任务                                       | 文件                                                                                  | 状态 | 来源       |
| ----- | ---------------------------------------- | ----------------------------------------------------------------------------------- | -- | -------- |
| CR-37 | 多文件 `var _ = io.Discard` 占位语句 (死代码)                  | [pkg/data/source/eastmoney_sectors_adapter.go:610-611](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_sectors_adapter.go#L610) | ✅ | B-009 |
| CR-38 | `fetchStockSectors` 只读 f100/f102,未含 f101/f103 概念/地域    | [pkg/data/source/eastmoney_sectors_adapter.go:217-222](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_sectors_adapter.go#L217) | ✅ | B-010 |
| CR-39 | `Registry.Fetch` fallback 链无日志,可观测性差                  | [pkg/data/source/registry.go:129-187](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/registry.go#L129) | ✅ | B-011 |
| CR-40 | `Registry.Fetch` "adapter 未注册" 与 "上游全炸" 错误未区分       | [pkg/data/source/registry.go:138-152](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/registry.go#L138) | ✅ | B-012 |
| CR-41 | `EastmoneyAdapter` 强制 `lmt=1000` 与时间窗口不一致被截断     | [pkg/data/source/eastmoney_adapter.go:264](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_adapter.go#L264) | ✅ | B-013 |
| CR-42 | `CapitalFlowFactor` 窗口内停牌日处理未文档化                            | [pkg/ai/factor/capital_flow.go:107-122](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/factor/capital_flow.go#L107) | ✅ | B-014 |
| CR-43 | `BacktestEngine.vue` 冗余 `triggerRef(result)` 调用             | [pages/BacktestEngine.vue:140](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/BacktestEngine.vue#L140) | ✅ | F-014 |
| CR-44 | `useAsyncBacktest.ts` 进度 90→100 跳跃                          | [composables/useAsyncBacktest.ts:103-109](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/composables/useAsyncBacktest.ts#L103) | ✅ | F-015 |
| CR-45 | `BacktestEngine.vue` `strategiesCache` 类型 `string[]` 污染    | [pages/BacktestEngine.vue:211](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/BacktestEngine.vue#L211) | ✅ | F-016 |
| CR-46 | `api/client.ts` retry 退避公式不直观                              | [api/client.ts:92-95](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/client.ts#L92) | ✅ | F-017 |
| CR-47 | AGENTS.md 文档导航说 "6 张表" 与 ARCHITECTURE.md "18 张" 不一致  | [AGENTS.md:492](file:///Users/ruoxi/longshaosWorld/quant-trading/AGENTS.md#L492) | ✅ | D-016 |
| CR-48 | AGENTS.md 已知问题表未反映 ODR-011 引入的 5 个新风险 (mootdx SDK/反爬/对账)  | [AGENTS.md:581-587](file:///Users/ruoxi/longshaosWorld/quant-trading/AGENTS.md#L581) | ✅ | D-017 |
| CR-49 | SPEC.md §6.4 `SetLiveTrader` 等方法名需对照代码验证 (未直接验证)   | [docs/SPEC.md:856-877](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L856) | ✅ | D-018 |
| CR-50 | `api/client.ts` 单元测试缺失 (超时/retry/abort 关键路径)              | [api/client.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/client.ts) | ✅ | Test Gap |

### P3 Low — 4 项 (Backlog)

| ID    | 任务                                       | 文件                                                                                  | 状态 | 来源       |
| ----- | ---------------------------------------- | ----------------------------------------------------------------------------------- | -- | -------- |
| CR-51 | `BulkInsert` `defaultTableMapper` 并发风险 (未来加 Register 需加锁)        | [pkg/storage/bulk_insert.go:425-427](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/storage/bulk_insert.go#L425) | ✅ | B-015 |
| CR-52 | `EastmoneyClient.GetJSON` 429 应返回 `ErrRateLimited` 而非 Upstream  | [pkg/data/source/eastmoney_adapter.go:39-69](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_adapter.go#L39) | ✅ | B-016 |
| CR-53 | `sector_rotation_test.go` / `sentiment_test.go` 缺 NaN/Inf 容错测试 | [pkg/ai/factor/sector_rotation_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/factor/sector_rotation_test.go) | ✅ | B-017 |
| CR-54 | `cmd/data/registry_init.go` env/viper key 来源优先级无日志告警 | [cmd/data/registry_init.go:90-95](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/data/registry_init.go#L90) | ✅ | B-018 |
| **F1-new** | **`mutation.go:69` `Intn(5)-2` 1/5 概率产 0 delta, 偶发测试失败** | [pkg/ai/evolution/mutation.go:69](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/evolution/mutation.go#L69) | ✅ | B-019 |
| **F2-new** | **vitest `toBe(expected, message)` 误用 2 处 + 缺 CI lint rule** | [web/scripts/lint-tests.mjs](file:///Users/ruoxi/longshaosWorld/quant-trading/web/scripts/lint-tests.mjs) | ✅ | F-018 |

### 后续行动建议

| 优先级 | 立即 (本周) | 下个 Sprint | Backlog |
| --- | --- | --- | --- |
| P0 | ~~CR-01 ~ CR-16 (16 项)~~ ✅ 2026-06-08 完成 | — | — |
| P1 | ~~CR-17 ~ CR-36 (20 项)~~ ✅ 2026-06-10 完成 | — | — |
| P2 | ~~CR-37 ~ CR-50 (14 项)~~ ✅ 2026-06-10 完成 | — | — |
| P3 | ~~CR-51 ~ CR-54 (4 项)~~ ✅ 2026-06-10 完成 + F1/F2-new | — | — |

***

## 📊 统计

| 优先级             | 待处理    | 进行中   | 已完成    | 已阻塞   | 已取消   | 总计     |
| --------------- | ------ | ----- | ------ | ----- | ----- | ------ |
| P0              | 0      | 0     | 8      | 0     | 0     | 8      |
| P1              | 0      | 0     | 20     | 0     | 0     | 20     |
| P2              | 0      | 0     | 19     | 0     | 0     | 19     |
| P3              | 0      | 0     | 19     | 1     | 0     | 19     |
| Phase 3 (D1-D7) | 0      | 0     | 53     | 0     | 0     | 53     |
| MS (Sprint 1-4 + 验证) | 0  | 0     | 25     | 0     | 0     | 25     |
| **CR (Sprint 5 — 综合审查 + 新发现)** | **0** | **0** | **56** | **0** | **0** | **56** | (含 F1/F2-new, 全部完成) |
| **总计**          | **0** | **0** | **200** | **1** | **0** | **200** |

***

## 📝 任务变更日志

### 2026-06-10 (v3.9.2) — Sprint 5 P2 pickup #8: CR-38/CR-41 收尾 + TASKS 表全部 ✅

- **触发**: v3.9.1 后 CR-38/CR-41 仍标 ⬜, 与已完成但未入表的 CR-39/40/44/45/49/51/52/53/54 一并收尾
- **过程**:
  - ✅ **CR-38** [pkg/data/source/eastmoney_sectors_adapter.go:168-303](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_sectors_adapter.go#L168) — `fetchStockSectors` field list 扩到 `f100,f101,f102,f103`, 提取 `buildStockSectorItems` + `stringField` 辅助函数, 每条 DataItem 加 `category` 标签 (`industry`/`concept`), Schema 同步加 `category` 字段。 新增 [TestBuildStockSectorItems](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/source_test.go) (8 子测试) + [TestEastmoneySectors_FetchStockSectors_HTTP](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/source_test.go) (HTTP 集成, 验证 `fields=f100,f101,f102,f103`)。
  - ✅ **CR-41** [pkg/data/source/eastmoney_adapter.go:266-378](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_adapter.go#L266) — `lmt=1000` 硬编码改为 `eastmoneyCapitalFlowLmt(klt, start, end)`, 窗口 + klt 联动计算 (20% headroom, 8000 上限, 单 klt→days 映射表)。 新增 [TestEastmoneyCapitalFlowLmt](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/source_test.go) (10 子测试覆盖 1y/2y/4y/10y/50y/weekly/monthly/unknown klt) + [TestEastmoneyAdapter_CapitalFlow_LmtScalesWithWindow](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/source_test.go) (5 年窗口 HTTP 集成, lmt 实际 ≥ 2193)。
  - ✅ **任务表 56 项全部 ✅**: CR-37~54 全部置为已完成, 后续行动建议 4 行全划掉, 统计表中 CR 待处理 10 → 0 (-10), 总待处理 20 → 0 (-20), 总完成 179 → 200 (+21, 含 F1/F2-new 收尾)
- **验证**:
  - `go vet ./...` exit 0
  - `go build ./...` exit 0
  - `go test ./pkg/... ./cmd/...` 全绿 (含 `pkg/data/source` 新增 ~13 个子测试)
  - `npm test` 9 文件 / 139 测试全绿
  - `npm run build` 成功 (vue-tsc + vite)
- **影响**: Sprint 5 P2/P3 + 新发现 F1/F2-new 56/56 全部完成, 综合代码审查 backlog 清零

### 2026-06-10 (v3.9.1) — Sprint 5 P2 pickup #7: F2-new CI lint rule 落地

- **触发**: F2-new 任务表中标记 ⬜, 此前仅修了 2 处误用, 缺自动化防护
- **过程**:
  - ✅ **新建 [web/scripts/lint-tests.mjs](file:///Users/ruoxi/longshaosWorld/quant-trading/web/scripts/lint-tests.mjs)**:
    独立 Node 脚本, 与 `src/test-lint.test.ts` 共享同一 regex
    `expect\([^)]*\)\.toBe\([^)]*,\s*['"`]`, 扫描 7 个 `*.test.ts`。
    自排除 `test-lint.test.ts` 避免自匹配。
  - ✅ **[web/package.json](file:///Users/ruoxi/longshaosWorld/quant-trading/web/package.json) 串联**:
    - 新增 `"lint:tests": "node scripts/lint-tests.mjs"`
    - `test` 改为 `"npm run lint:tests && vitest run"`,
      CI 跑 `npm test` 时 lint 必先于 vitest 跑 (fails fast, <100ms)
  - ✅ **更新 [web/src/test-lint.test.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/test-lint.test.ts) 注释**:
    明确这是"runtime half of a two-layer guard", 强调 regex 须与脚本保持同步
  - 📋 **ODR-012 §F2-new 状态**: 误用 2 处已修 + CI lint rule 已落地, F2-new 关闭
- **为什么不用 ESLint plugin**: 项目未引入 ESLint (package.json 无 eslint 依赖),
  新增依赖属 AGENTS.md "Ask First" 项; 独立 Node 脚本零依赖、可被
  pre-commit hook 复用, 与现有 `vue-tsc + vitest` CI 流程无缝集成
- **验证**:
  - 正向 (无 misuse): `npm run lint:tests` → exit 0
  - 反向 (canary 引入 misuse): `npm run lint:tests` → exit 1 + 精确行号定位
  - 完整 `npm test`: 9 文件 / 137 测试全绿, lint 阶段 7 文件全过
  - `go vet ./...` exit 0
- **总待处理**: 21 → 20 (-1: F2-new)
- **总完成数**: 178 → 179 (+1: F2-new)

### 2026-06-10 (v3.9.0) — Sprint 5 P2 pickup #6: F1-new mutation 偶发 + 新发现跟踪

- **触发**: 在 CR-42 验证 `go test ./pkg/ai/...` 时, 已登记的 F1-new 偶发测试失败被触发。
  顺手 fix, 并正式将 F1/F2-new 加入 CR 任务表
- **过程**:
  - ✅ **F1-new**: [pkg/ai/evolution/mutation.go:67-79](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/evolution/mutation.go#L67) 原 `delta := m.rng.Intn(5) - 2`
    在 seed 42 下 1/5 概率产 0, 导致 `TestMutation_MutateParams` 偶发失败。
    改为 `delta := m.rng.Intn(3) + 1; if m.rng.Intn(2) == 0 { delta = -delta }`,
    delta ∈ {-3, -2, -1, 1, 2, 3} (6 值, 永不 0)
  - 📋 **F1/F2-new 入表**: 此前 F1/F2-new 仅在 ODR-012 中提及, 正式入 CR 表追踪 (B-019/F-018)
- **验证**: 
  - `go test ./pkg/ai/evolution/... -count=50`  50/50 pass (消除偶发)
  - `go test ./pkg/ai/... -count=1`  13 packages all pass (整体绿)
- **总任务数**: 198 → 200 (+2: F1/F2-new)
- **总完成数**: 177 → 178 (+1: F1-new)
- **总待处理**: 20 → 21 (+1: F2-new)

### 2026-06-10 (v3.8.0) — Sprint 5 P2 pickup #5: CR-42 停牌日语义文档化 + 真实 bug 修复

- **触发**: Sprint 5 P2 继续;挑选 ⭐ 易改项: 文档可读性 (CR-42)
- **过程**:
  - 📚 **CR-42 docs**: [capital_flow.go:74-109](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/factor/capital_flow.go#L74) 新增 26 行 "Suspended-day (停牌) semantics" 注释块,
    显式说明两种上游行为 (omit / zero-fill) 的影响,以及为何不做日历 gap-fill (避免假 zero-flow 与真实 zero-flow 混淆)
  - 🐛 **CR-42 bonus bug fix**: 原 `if closeRef == 0 { closeRef = r.ClosePrice }` 模式有真 bug:
    当 most recent day close=0 (停牌),第二个 row 的 close 会**静默覆盖** 0,
    导致 (a) "closeRef <= 0" guard 永远不触发, (b) 用 stale price 做归一化。
    改为 `haveClose bool` 显式追踪, 配合 CR-42 测试 "suspended as most recent day → symbol dropped" 锁定行为
  - 🧪 **测试**: [capital_flow_test.go:112-160](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/factor/capital_flow_test.go#L112) 新增 `TestCapitalFlowFactor_SuspendedDaySemantics` 3 子测试:
    1. 停牌为最新日 → 整个 symbol drop
    2. 停牌在窗口中间 → 当 zero sum, factor 保留
    3. 上游 omit 停牌日 → 无 gap-fill, 仅 sum 实际有 row
- **验证**: `go test ./pkg/ai/factor -v -run TestCapitalFlow` 9/9 pass
  (注: `go test ./pkg/ai/...` 整体有 1 个失败 — `TestMutation_MutateParams`,
   是已登记的 F1-new (mutation.go:51 `Intn(5)-2` 可能产 0 delta),
   与本任务无关, 下一轮 fix)
- **总任务数**: 198 → 198 (1 项状态变更: CR-42)
- **总完成数**: 176 → 177 (+1)
- **总待处理**: 21 → 20 (-1)

### 2026-06-10 (v3.7.0) — Sprint 5 P2 pickup #4: CR-46 retry 退避公式

- **触发**: Sprint 5 P2 继续;挑选 ⭐ 易改项: 文档可读性 (CR-46)
- **过程**:
  - ✅ **CR-46**: [api/client.ts:124](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/client.ts#L124) 原 `API_RETRY_DELAY * (4 - retry)` 魔数 `4` 来源不明 (实际是 `API_MAX_RETRIES + 1`)。
    重构为 `API_RETRY_DELAY * (API_MAX_RETRIES + 1 - remainingRetries)`,
    引入 `API_MAX_RETRIES` import, 提取 `remainingRetries` 局部变量。
    附 8 行注释,显式列出退避 schedule:
      remainingRetries=3 (默认) -> 1s
      remainingRetries=2          -> 2s
      remainingRetries=1          -> 3s
  - **行为不变**: 退避时长与原公式完全一致 (API_MAX_RETRIES=3 时);
    CR-50 测试中 2000ms/3000ms 期望值无需修改
- **验证**: vitest 24/24 ✅, vue-tsc build ✅
- **总任务数**: 198 → 198 (1 项状态变更: CR-46)
- **总完成数**: 175 → 176 (+1)
- **总待处理**: 22 → 21 (-1)

### 2026-06-10 (v3.6.0) — Sprint 5 P2 pickup #3: CR-37/43 死代码 + 冗余 triggerRef

- **触发**: Sprint 5 P2 继续;挑选 ⭐⭐⭐ 项: 死代码清理 (CR-37) + 冗余 triggerRef (CR-43)
- **过程**:
  - ✅ **CR-37**: 删除 [eastmoney_sectors_adapter.go:610-611](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/data/source/eastmoney_sectors_adapter.go#L610) 死代码
    `var _ = io.Discard` / `var _ = http.MethodGet` 及其占位注释。
    连带删除 import 块中的 `io` 和 `net/http` 两个未使用 import。
    `go vet` + `go test ./pkg/data/source/...` 全绿
  - ✅ **CR-43**: 删除 [BacktestEngine.vue:141](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/BacktestEngine.vue#L141) 冗余 `triggerRef(result)`。
    `result` 是 shallowRef,赋值 `result.value = newResult` 已触发响应式;`triggerRef` 仅在 mutate 嵌套属性时需要。
    `triggerRef` 仍在 3 处其他位置使用 (lines 224/250/287),保留 import。
    `npm test` 129/129 ✅, `npm run build` (vue-tsc) ✅
- **总任务数**: 198 → 198 (2 项状态变更: CR-37/43)
- **总完成数**: 173 → 175 (+2)
- **总待处理**: 24 → 22 (-2)

### 2026-06-10 (v3.5.0) — Sprint 5 P2 pickup #2: CR-47/48 文档一致性

- **触发**: Sprint 5 P2 继续;挑选文档一致性 2 项 (CR-47/48, 纯 Markdown, ~3 分钟)
- **过程**:
  - ✅ **CR-47**: AGENTS.md 文档导航表第 500 行 "DB schema（6 张表）" → "（18 张表）",对齐 ARCHITECTURE.md:305。Count verified by `grep CREATE TABLE` across migrations/ (18) + pkg/storage/postgres.go (14)。附 CR-47 注释 (5 行) 说明 canonical 来源
  - ✅ **CR-48**: AGENTS.md 已知问题表 (§14) 追加 5 项 ODR-011 多源集成风险 (mootdx SDK/反爬/数值仲裁/实时背压/HealthCheck CI)
- **总任务数**: 198 → 198 (3 项状态变更: CR-47/48/50)
- **总完成数**: 170 → 173 (+3)
- **总待处理**: 27 → 24 (-3)

### 2026-06-10 (v3.4.0) — Sprint 5 P2 first pickup: CR-50 (api/client.ts 测试 + isTimeout bug fix)

- **触发**: ODR-012 P2 Sprint 启动;优先挑选 P2 性价比最高项 (CR-50: api/client.ts 测试覆盖)
- **过程**:
  - 🐛 **附带真实 bug 修复**: `ApiError.isTimeout` 原实现 `message.includes('abort')` 与实际抛出的中文消息 `'请求已取消'` 不匹配 → 永远返回 `false`,导致 `createCancellableRequest` (client.ts:142) 和 `useAsyncBacktest` 的 abort 分支判断失效。改为显式 `isAbort` flag
  - ✅ **CR-50**: 新增 `web/src/api/client.test.ts` (24 测试) — 覆盖 ApiError 5 个 getter、GET/POST/DELETE 短方法、绝对 URL 透传、4xx/5xx 错误映射、timeout 触发 AbortController、手动 AbortSignal 中断、retry 重试预算/不重试 4xx/5xx/transient 恢复、`createCancellableRequest` abort 取消、pagehide 监听器存在性
  - 📋 **vitest config**: `src/api/**` 加入 coverage include
- **验证**: `npm test` 8 files, 129 tests ✅ (其中 24 为本次新增) / `npm run build` (vue-tsc) ✅
- **总任务数**: 198 → 198 (CR-50 状态变更)
- **总完成数**: 169 → 170 (+1)
- **总待处理**: 28 → 27 (-1)

### 2026-06-10 (v3.3.0) — ODR-012 P1 综合审查 20 项修复

- **触发**: 用户确认整批 P1 (20 项) — 紧接 ODR-012 P0 (16 项, 2026-06-08) 之后
- **过程**: 后端 5 项 + 前端 7 项 + 文档 8 项同步修复
  - 🔧 **后端 (CR-17~21)**: mootdx 按市场批量 / bulk_insert 单元测试 / TopList 4 字段去硬编码 / Registry.HealthCheck 并行化 / etl_test stubStore 接口签名对齐
  - 🔧 **前端 (CR-22~28)**: DetailMetrics 未用 props / FitnessChart Math.max+resize 清理 / GenealogyTree Math.max / api/client pagehide 单注册 / sync.ts SSE 关闭 / pairTrades 提取 + 18 测试 / useAsyncBacktest 16 测试
  - 🔧 **文档 (CR-29~36)**: SPEC Analysis Service +19 端点 / ADR-015 `*_agent.go` → 裸名 / ADR-016 migration 014→018 off-by-one / ODR-011 数据源 7→9 / 三处 `Signal` → `domain.Signal` / VISION+SPEC ai 75% → 0%/avg 67% / ADR-015/016 Status → Accepted / AGENTS.md services
- **配套**: ODR-012 追加 P1 Completion Update + P1 Artifacts 章节
- **验证**: `go vet ./...` ✅ / `go build ./...` ✅ / `go test ./pkg/data/source/... ./pkg/storage/... ./cmd/...` ✅ / `vue-tsc --noEmit` ✅ / `npx vitest run` 7 files, 105 tests ✅ / `npm run build` ✅
- **总任务数**: 144 → 198 (无变化,本批修改状态)
- **总完成数**: 133 → 169 (+36, P0 + P1)
- **总待处理**: 64 → 28 (-36)

### 2026-06-08 (v3.2.0) — ODR-011 多源数据集成完成

- **触发**: ODR-011 (Multi-Source Data Integration) 全部 4 个 Sprint 实施完毕
- **过程**: 集成 ashare-data-source-fetchers (SKILL.md V3.2.2) 的 8 个外部数据源
- **结果**: 追加 **25 项 MS 任务** (MS-1 ~ MS-26，含 1 项 sub-id)
  - 🔴 **Sprint 1 (MS-1~MS-11)**: 实时行情 + 资金流 (mootdx / eastmoney push2)
  - 🟠 **Sprint 2 (MS-12~MS-14)**: 板块 + 龙虎榜 (eastmoney slist / top_list)
  - 🟡 **Sprint 3 (MS-15~MS-17)**: 公告 + 舆情 (juchao / xueqiu)
  - 🟢 **Sprint 4 (MS-18~MS-19b)**: 全球扩展 (alpha_vantage / yahoo_finance)
  - 🧪 **验证 (MS-20~MS-25)**: L1-L4 测试 + 3 个新因子
  - 🌐 **HTTP 端点 (MS-26)**: `/api/datasource/registry/{status,health,chains}`
- **配套**: 创建 ODR-011 + ADR-016 (Multi-Source Architecture)
- **5 项代码审查 Bug 修复**:
  1. Eastmoney 适配器命名冲突 (3 个 slot)
  2. EastmoneyAdapter.SupportedTypes 越权声明
  3. SectorRotationFactor as-of 过滤 (避免 forward-looking)
  4. snapshotStatus 持锁跨越网络 I/O
  5. Gin 路由空路径歧义
- **总任务数**: 119 → 144 (+25)
- **总完成数**: 108 → 133 (+25)
- **总待处理**: 10 (无变化，本次新增项全部完成)

### 2026-05-17 (v3.1.0) — 全项目代码+文档一致性审查

- **触发**: 用户请求对项目进度和文档-代码一致度进行双维度审查
- **过程**: 全面扫描 docs/ + pkg/ + cmd/ + web/src/ + docker-compose + 数据库实际状态
- **结果**: 追加 **10 项新任务** (P0-7~P0-8, P1-19~P1-24, P2-19~P2-20)
  - 🔴 **P0 (2 项)**: 2 个测试包失败 — 阻塞覆盖率统计准确性
  - 🟠 **P1 (6 项)**: 表名错位（10 处引用）+ 4 项覆盖率数据校准 + 1 项服务状态澄清
  - 🟡 **P2 (2 项)**: 数据库文档同步 + Phase 4 验收对照
- **配套**: 创建 ODR-010 记录审查过程，添加 ADR.md 索引
- **总任务数**: 109 → 119 (+10)
- **总完成数**: 108 → 108 (新增项均为待处理)
- **新增统计**: 10 待处理 / 108 已完成 / 1 阻塞 / 119 总计

### 2026-05-05 (v3.0.0) — 一致性检验与统计修正

- **一致性检验**: 全面扫描 TASKS.md 任务状态与代码库实际完成情况
  - 发现统计数据严重错误: 文档声称 12 个待处理任务，实际所有列出任务均已完成
  - 实际任务总数: 109 项 (原为 124 项，虚增 15 项)
  - 实际完成: 108 项，阻塞: 1 项 (P3-19 vnpy drift)，待处理: 0 项
  - 修正 P1/P2/P3/D1-D7 各分类统计数字以匹配实际任务数量
- **完成**: D6-1~D6-4 (AI Copilot 深度集成测试)
  - `pkg/ai/intent/parser_test.go` — 26 个测试用例
  - `pkg/ai/yaml/generator_test.go` — 20 个测试用例
  - `pkg/ai/pipeline/pipeline_test.go` — 23 个测试用例
  - `web/src/components/ai/__tests__/PipelineDashboard.spec.ts` — 12 个测试用例
  - 新增 `BacktestResultCard.vue` 组件

### 2026-05-05 (v2.9.0)

- **完成**: D7-28~D7-30 (E2E 测试 — 数据同步/定时任务/错误处理)
  - `e2e/tests/data-sync.spec.ts` — 7 个测试用例 (创建/执行/完成/验证/UI/SSE)
  - `e2e/tests/data-sync-schedule.spec.ts` — 5 个测试用例 (CRUD/触发/切换)
  - `e2e/tests/data-sync-error.spec.ts` — 10 个测试用例 (错误/重试/并发)
- **完成**: D4-4 (实盘接口文档 — `docs/live-trading.md`)
- ~~更新统计: 124 项任务 (12 待处理, 0 进行中, 111 已完成, 1 阻塞)~~ → 修正为 v3.0.0

### 2026-05-05 (v2.8.0)

- **完成**: D7-31 (性能测试 — `pkg/sync/bench_test.go` 6 个 benchmark)
- **完成**: D7-32 (故障注入测试 — `pkg/sync/fault_test.go` 6 个测试用例)
- **完成**: D7-33 (SPEC.md API 文档 — 新增数据同步/Batch/Walk-Forward API)
- **完成**: D7-34 (AGENTS.md 架构图 — 更新数据流和测试覆盖率)
- **修复**: `pkg/sync/worker.go` — 添加 panic recovery 防止 worker 崩溃
- 更新统计: 124 项任务 (16 待处理, 0 进行中, 107 已完成, 1 阻塞)

### 2026-05-05 (v2.7.0)

- **状态修正**: 基于代码审查结果，更新 P1-1、D1-D5、D7-16~D7-36 任务状态以匹配实际代码实现
- **完成**: P1-1 (`pkg/data` 测试覆盖率 70.6%，已有 14 个测试文件)
- **完成**: D1-10 (数据源切换 API — `cmd/analysis/handlers_datasource.go`)
- **完成**: D2-2~D2-7 (CSV 解析/Walk-Forward/汇总报告/Batch API — 均已实现)
- **完成**: D3-4~D3-5 (Plugin Loader API + ADR-001 文档)
- **完成**: D5-5 (策略插件单元测试 — 11 个测试文件，覆盖率 80.3%)
- **完成**: D7-16~D7-27 (数据同步前端 — types/api/store/components/page/router/SSE/测试)
- **完成**: D7-35~D7-36 (质量验证 — go vet + go test + npm run build 全部通过)
- **修复**: `pkg/sync/job_test.go` + `worker_test.go` 并发测试 race condition (添加 mutex + Clone)
- **新增**: `pkg/sync/job.go` — Job.Clone() 深拷贝方法
- 更新统计: 124 项任务 (20 待处理, 0 进行中, 103 已完成, 1 阻塞)

### 2026-05-05 (v2.6.0)

- **完成**: D7-4~D7-9 (数据同步队列/Worker/Handler/SSE/单元测试)
  - `pkg/sync/queue.go` — PostgreSQL 队列管理 (Enqueue/Dequeue/Complete/Fail/Retry)
  - `pkg/sync/worker.go` — Worker goroutine pool (RegisterExecutor/Start/Stop/ProcessJob)
  - `cmd/data/sync_handlers.go` — REST API + SSE 进度推送端点
  - `pkg/sync/*_test.go` — 35+ 单元测试，覆盖 job/queue/worker/scheduler
- **完成**: D7-10~D7-15 (定时调度器实现 + 测试)
  - `pkg/sync/scheduler.go` — cron 定时调度器 (Create/Update/Delete/Toggle/RunNow)
  - `pkg/sync/schedule.go` — Schedule 模型和 ScheduleStore 接口
  - 调度器单元测试覆盖 CRUD/触发/统计
- **修复**: `pkg/storage/ohlcv.go` + `cache.go` pgx batch `conn busy` 错误
- **修复**: `pkg/storage/postgres_test.go` GetLatestOHLCVDate 测试稳定性
- **修复**: `pkg/strategy/loader.go` 添加 WatchDir() + SetPluginForTesting() 公共方法
- **修复**: `cmd/analysis/handlers_plugin_test.go` 访问未导出字段问题
- 更新统计: 124 项任务 (35 待处理, 0 进行中, 88 已完成, 1 阻塞)

### 2026-05-05 (v2.5.0)

- **状态修正**: 基于代码审查结果，批量更新 D1-D5、D7 任务状态以匹配实际代码实现
- **完成**: D1-1~D1-3, D1-5~D1-7 (多数据源适配器框架 — eventbus/provider/akshare/http/cached)
- **完成**: D2-1, D2-3, D2-4 (批量回测框架 — 类型定义/BatchEngine/Scorer)
- **完成**: D3-1~D3-3 (Go Plugin 热加载 — loader + plugins)
- **完成**: D4-1~D4-3 (实盘接口预留 — LiveTrader/MockTrader/Engine集成)
- **完成**: D5-1~D5-4 (实战策略插件 — TD Sequential/Bollinger/VPT/Volatility Breakout)
- **完成**: D7-1~D7-3 (数据同步增强 — 迁移脚本 + job.go 完整实现)
- **进行中**: D7-4~D7-7, D7-10~D7-11 (queue/worker/scheduler 骨架 + handlers)
- **回退**: P1-1 从 🔵 改为 ⬜ (pkg/data 实际无测试文件，覆盖率 0%)
- 更新统计: 124 项任务 (50 待处理, 8 进行中, 65 已完成, 1 阻塞)

### 2026-05-03 (v2.4.0)

- **新增**: ADR-013 (Data Synchronization Enhancement) — 数据同步增强架构决策
- **新增**: D7 数据同步增强实施任务 (36 项, Week 7-9)
- **新增**: `docs/design/pages/data-sync.md` — 数据同步管理页面 UI 设计规范
- **更新**: `docs/ARCHITECTURE.md` — 新增数据同步架构章节 (ADR-013)
- **更新**: `docs/ADR.md` — 添加 ADR-013 到索引
- 更新统计: 124 项任务 (67 待处理, 1 进行中, 55 已完成, 1 阻塞)

### 2026-04-12 (v2.3.0)

- **完成**: P1-2 (storage 测试覆盖率 — 新增 backtest\_jobs\_test.go + strategies\_test.go + cache 补充, 共 27 测试)
- **完成**: P1-3 (strategy 测试覆盖率 — 新增 utils\_test.go, 16 测试覆盖 IsRebalanceDay + ScreenCache)
- **完成**: P1-16 (批量化 regime/stoploss — CalculatePositionsBatch + checkStopLossesWithATR + 预计算 ATR)
- **完成**: P2-15 (E2E 测试隔离 — isolation.ts helper + beforeEach hooks in 4 describe blocks)
- **完成**: P2-17 (回测结果对比测试 — 5 个对比场景: 幂等性/策略差异/日期范围/费率影响/本金比例)
- **完成**: D1-4 (PostgresProvider 集成到 main.go 作为主数据源)
- **完成**: D1-8/D1-9 (DataAdapter 集成到引擎, PG为主HTTP为备)
- **修复**: 回测"内部错误"根因 — Tushare 429 导致级联崩溃, 通过 DataAdapter PG 直查解决
- **修复**: backtests map 并发安全 — 添加 btMu sync.RWMutex 专用锁
- **完成**: P1-17 (向量化逐日处理 — processSignalsAndExecuteTrades 使用 calculatePositionsBatch + fallback)
- **完成**: P1-6 (9 项关键 E2E 测试 T-01\~T-09 — 结果渲染/交易可视化/错误处理/表单验证/导航高亮/NaN防护/响应式)
- 更新统计: 88 项任务 (32 待处理, 1 进行中, 54 已完成, 1 阻塞)

### 2026-04-11 (v2.2.0)

- **完成**: P1-9 (data-service 反向依赖修复), P1-10 (strategy-service 去留决策)
- **完成**: P2-16 (E2E 智能等待), P3-9 (engine.go 重构), P3-10 (API 路径前缀统一)
- **修复**: 重复 P2 ID (P2-14, P2-15 各出现两次)，合并为 P2-18
- **新增**: ADR-012 (strategy-service standby), handlers\_walkforward.go, engine\_daily.go
- 更新统计: 88 项任务 (43 待处理, 1 进行中, 43 已完成, 1 阻塞)

### 2026-04-11 (v2.1.0)

- **新增**: 整合 phase-gate-reviews.md 的可执行任务
- 新增 P1 性能优化任务 (P1-16, P1-17)
- 新增 P2 前端/配置任务 (P2-14, P2-15)
- 新增 P3 阻塞任务 (P3-19: vnpy drift 对比)
- 更新统计: 91 项任务 (86 待处理, 1 进行中, 3 已完成, 1 阻塞)
- **重大更新**: 整合 PHASE3-PLAN.md、NEXT\_STEPS.md、AGENTS.md 的所有任务
- 新增 Phase 3 实施任务 (D1-D6, 共 28 项)
- 新增 P2 测试质量任务 (P2-14 \~ P2-17)
- 标记已完成任务: P1-13, P1-14, P1-15 (前端重构 + 持久化)
- 更新统计: 85 项任务 (81 待处理, 1 进行中, 3 已完成)

### 2026-04-11 (v1.0.0)

- 创建 TASKS.md，整合 CODE\_REVIEW\_REPORT.md 和 NEXT\_STEPS.md 的可执行任务
- P1-1 (提升 `pkg/data` 测试覆盖率) 标记为进行中 🔵

### 2026-04-10

- CODE\_REVIEW\_REPORT.md 发现 47 个问题，按优先级分类

### 2026-04-09

- NEXT\_STEPS.md 审查发现测试覆盖和文档同步问题

***

## 🚀 Sprint 6: ODR-013 综合审查闭环 (2026-06-11 → 2026-07-02)

> **来源**: [ODR-013 全项目 4 维度综合审查](odr/odr-013-comprehensive-audit-2026-06-11.md) (2026-06-11)
> **综合评分**: 59/100（业务 68 + 架构 62 + 代码 72 + 测试 47）
> **总任务数**: **73 项** (P0: 10 + P1: 30 + P2: 33)
> **关联 ADR**: [ADR-007](adr/adr-007-ai-sandbox.md) (Accepted) + [ADR-008](adr/adr-008-inter-service-comm.md) (Accepted) + [ADR-017](adr/adr-017-observability-and-auth.md) + [ADR-018](adr/018-test-and-async-safety.md) + [ADR-019](adr/019-service-merge-ai-copilot.md) + [ADR-020](adr/020-engine-decomposition.md)
> **Owner**: 龙少 (Longshao) — AI Assistant
> **Sprint 周期**: 3 周 (2026-06-11 → 2026-07-02)
> **CI Gate**: `go test -race -count=1 ./...` 0 panic + `pkg/storage` 覆盖率 ≥ 60% + `go vet ./...` 0 issue
> **对齐审计**: 2026-06-11 完成；详见 [ODR-013 §对齐审计复核 (2026-06-11)](odr/odr-013-comprehensive-audit-2026-06-11.md#对齐审计复核-2026-06-11-同日) — 路径/行号/描述对齐项目实际状态后完成 4 类修正（P1-15 路径、P0-2 lock-during-I/O、P1-19 setter 数量、P1-2/8 migrations 格式）；剩余 6 类"待校核项"见 [§Sprint 6 启动期 待校核项](#-sprint-6-启动期-待校核项-6-项)

### 🔴 Sprint 6 P0 — 必须立即修复 (10 项) [CI 阻断]

> **验收**: Sprint 6 第 1 周末 (2026-06-18) 前全部完成。CI 任何 P0 未完成 → 阻断部署。

| ID | 任务 | 关联问题 | 文件 | Owner | 估时 | 验收标准 | 状态 |
|---|---|---|---|---|---|---|---|
| **P0-1** | LLMClient interface 化 | TQ-003, CQ-003 | `pkg/ai/client.go`, `pkg/strategy/copilot.go` | TBD | 1d | `pkg/strategy` 测试套件 `go test` 0 panic | ⬜ |
| **P0-2** | 修复 `pkg/live/engine.go::Stop` 持锁跨越 `Unsubscribe`/`Disconnect` 网络 I/O（类似 CR-02 ODR-012 模式） | CQ-009 | `pkg/live/engine.go:Stop` | TBD | 1d | 锁在 `close(stopCh)` 后立即释放；`TestLiveEngine_Stop_Concurrent` 1000 次并发无 deadlock | ⬜ |
| **P0-3** | 引入 OpenTelemetry + Prometheus + `/metrics` + request_id 透传 | AR-001, ADR-017 §1 | `cmd/analysis/main.go` | TBD | 2d | `/metrics` 端点暴露 4 类核心 metric；trace_id 跨服务透传 | ⬜ |
| **P0-4** | Copilot `WorkingDir` 配置化 + 静态分析闸 (sandbox 阶段 1) | AR-003, ADR-007/019 §2 | `pkg/strategy/copilot.go`, `internal/sandbox/staticcheck/` | TBD | 1d | 硬编码路径消除；危险模式 (`os.RemoveAll`/`exec.Command`) 拒绝 | ⬜ |
| **P0-5** | `engine.go::rand.NewSource` 返回值保留 + 确定性重放 | AR-007, CQ-001 | `pkg/backtest/engine.go:244-246` | TBD | 2d | `TestEngine_DeterministicReplay` byte-level 通过 | ⬜ |
| **P0-6** | `pkg/backtest` 并发 map panic 修复 (RWMutex + race test) | CQ-019, TQ-012 | `pkg/backtest/engine.go`, `pkg/backtest/job.go` | TBD | 2d | `go test -race ./pkg/backtest/...` 0 竞态 | ⬜ |
| **P0-7** | `pkg/storage` dockertest 集成测试 | TQ-011, AR-013 | `pkg/storage/integration_test.go` (新建) | TBD | 3d | 集成测试覆盖率 8.2% → ≥ 60% | ⬜ |
| **P0-8** | `cmd/analysis` 优雅停机 (WaitGroup + stale 'running' cleanup) | AR-005, TQ-008 | `cmd/analysis/main.go`, `pkg/backtest/job.go` | TBD | 2d | SIGTERM 后 DB status 全部 'completed'/'failed' | ⬜ |
| **P0-9** | AI service token bucket 限流 (10 req/min/user) | AR-004, AR-008, ADR-017 §2 | `cmd/ai/main.go` | TBD | 1d | 限流生效；超限返 429 | ⬜ |
| **P0-10** | 删除 8 处手写 `max`/`min`，改用 Go 1.21+ 内建 | CQ-003 | `pkg/live/mock_trader.go:337`, `pkg/backtest/execution.go:147,154`, `pkg/ai/evolution/mutation.go:137,144`, `pkg/ai/metrics/turnover.go:112,126`, `pkg/ai/expression/evaluator.go:345` | TBD | 0.5d | `go vet` + 编译通过；测试通过 | ⬜ |

### 🟠 Sprint 6 P1 — 1 周内修复 (30 项)

> **验收**: Sprint 6 第 2 周末 (2026-06-25) 前全部完成。

| ID | 任务 | 关联问题 | 文件 | Owner | 估时 | 验收标准 | 状态 |
|---|---|---|---|---|---|---|---|
| **P1-1** | 文档一致化 (VISION/SPEC/AGENTS 覆盖率 + Phase 状态对齐) | BR-001/008, TQ-009 | `docs/VISION.md`, `docs/SPEC.md`, `docs/AGENTS.md` | TBD | 2d | ODR-014 文档一致化创建；3 处状态冲突消除 | ⬜ |
| **P1-2** | RBAC + JWT auth + audit_logs 表 + bcrypt login | AR-004, BR-002, ADR-017 §2 | `cmd/analysis/main.go`, migrations/019_*.sql | TBD | 1w | `POST /api/auth/login` 工作；mutating 端点需 token | ⬜ |
| **P1-3** | LiveEngine 限价单实现 (Limit / Stop / Trailing) | AR-015 | `pkg/live/engine.go:tryFillOrder` | TBD | 3d | tryFillOrder 接受 OrderTypeLimit；价格匹配逻辑测试 | ⬜ |
| **P1-4** | A 股券商真实对接 (中泰 XTP 推荐) | BR-003, BR-005 | `pkg/live/broker/xtp/` (新建) | TBD | 2w | 模拟账户下单/撤单/查询 working | ⬜ |
| **P1-5** | A 股价格笼子校验 (沪深/创/科/北 4 套) | BR-004 | `pkg/live/price_cage.go` (新建) | TBD | 1w | 4 套笼子规则测试 + 主板 ±2% 模拟 | ⬜ |
| **P1-6** | 集合竞价撮合 (9:15-9:25 + 14:57-15:00) | BR-004, BR-017 | `pkg/backtest/auction.go` (新建) | TBD | 1w | 开盘集合 + 收盘集合状态机测试 | ⬜ |
| **P1-7** | 4 黄金 fixture 补全 (momentum/value/T+1/zhangting) | TQ-009, TEST.md §2.3 | `pkg/backtest/testdata/` | TBD | 2d | 4 fixture 存在；`TestGolden_*` 容差 ±0.01 | ⬜ |
| **P1-8** | users/audit_logs 表 + JWT middleware + login endpoint | AR-004, ADR-017 | `migrations/019_add_auth_tables.sql` (扁平命名), `cmd/analysis/auth/` | TBD | 1w | 同 P1-2 (合并) | ⬜ |
| **P1-9** | testing/quick property-based 5 个 invariant | TQ-014, TEST.md §2.4 | `pkg/backtest/property_test.go` (新建) | TBD | 3d | 1000 次随机序列不违反 5 个 property | ⬜ |
| **P1-10** | research_batch_test.go fail-gate (10+ factors IC>0.03) | TQ-007, TQ-015 | `pkg/ai/agents/research_batch_test.go` | TBD | 1d | `assert.GreaterOrEqual(highICFactors, 10)` 通过 | ⬜ |
| **P1-11** | AI Copilot 进程隔离 sandbox (Phase 2) | AR-003, ADR-007/019 | `internal/sandbox/runner/` (新建) | TBD | 1w | subprocess + rlimit + 5s timeout working | ⬜ |
| **P1-12** | L4 validate 实际 walk-forward 实现 (非 placeholder) | TQ-007 | `pkg/ai/agents/validate.go:validateL4` | TBD | 3d | L4 真实跑 walk-forward；Score 不再恒 4.0 | ⬜ |
| **P1-13** | AI Pipeline L5 人工审查 UI (Approve/Reject/Edit) | BR-013 | `web/src/components/ai/ReviewActions.vue` (新建) | TBD | 3d | PipelineDashboard 有 3 按钮 + POST /api/pipeline/jobs/:id/review | ⬜ |
| **P1-14** | AI service httpclient 加固 (timeout/retry/rate/cost) | AR-008, AR-017 | `pkg/ai/client.go` | TBD | 3d | OTel trace；token bucket；cost table 写入 | ⬜ |
| **P1-15** | risk/execution service 合并到 analysis (7→3 服务) | AR-002, ADR-008/019 | `cmd/risk/`, `cmd/execution/` (服务名 risk-service/execution-service 在 docker-compose 中) | TBD | 1w | Docker compose 3 服务；`engine.go` 0 HTTP 调用 | ⬜ |
| **P1-16** | Engine 拆 CacheManager + FactorCacheAccessor | CQ-001, ADR-020 | `pkg/backtest/cache.go`, `pkg/backtest/factor_cache.go` | TBD | 3d | 2 子包独立测试；Engine 减少 ~300 行 | ⬜ |
| **P1-17** | Engine 拆 LiveBridge + ExecutionBridge | CQ-001, ADR-020 | `pkg/backtest/live_bridge.go`, `pkg/backtest/execution_bridge.go` | TBD | 3d | 2 子包独立测试；Engine 减少 ~200 行 | ⬜ |
| **P1-18** | StateStore interface + LRU/持久化 | CQ-008, AR-012, ADR-020 | `pkg/backtest/state_store.go` (新建) | TBD | 2d | LRU 1000 条 + 落 PG；backtests map 内存不再泄漏 | ⬜ |
| **P1-19** | EngineOption 函数式注入 + backward-compat shim | CQ-005, ADR-020 | `pkg/backtest/engine.go` | TBD | 3d | `NewEngine(cfg, prov, opts...)` working；旧 5 个 engine setter（SetDataAdapter/SetStore/SetRiskManager/SetLiveTrader/SetExecutionService）+ 1 个 strategy SetFactorCache 共 6 个 setter 保留 6 个月 backward-compat | ⬜ |
| **P1-20** | BacktestState 内部锁 + Freeze 模式 | AR-014, ADR-020 | `pkg/backtest/engine.go` (BacktestState struct) | TBD | 2d | race detector 0 issue；回测完成冻结 | ⬜ |
| **P1-21** | `pkg/statistics/` 包抽取 (mean/std/slope/volatility) | CQ-004 | `pkg/statistics/` (新建) | TBD | 2d | 6+ 处重复消除；单包覆盖率 ≥ 80% | ⬜ |
| **P1-22** | `pkg/fees/ashare.go` 费率常量统一 | CQ-005 | `pkg/fees/ashare.go` (新建) | TBD | 1d | 4 处硬编码消除；单包测试 | ⬜ |
| **P1-23** | `pkg/id/order.go` UUID v7 统一 | CQ-007 | `pkg/id/order.go` (新建) | TBD | 1d | 3 处订单号生成统一；测试 | ⬜ |
| **P1-24** | Strategy 接口拆分 (StrategyCore/Configurable/ResourceManaged) | CQ-006, ISP | `pkg/strategy/strategy.go` | TBD | 2d | 3 个可组合 interface；现有 strategy 适配 | ⬜ |
| **P1-25** | domain.Strategy Deprecated 删除 | CQ-014 | `pkg/domain/types.go` | TBD | 0.5d | 旧接口移除；pkg/strategy.Strategy 唯一源 | ⬜ |
| **P1-26** | 4 套执行实体合并 (LiveEngine/OrderManager/...) | CQ-010, YAGNI | `pkg/live/` | TBD | 1w | 5 套 → 2 套 (LiveEngine + MockTrader) | ⬜ |
| **P1-27** | `pkg/strategy/plugins/utils.go` 删除手写 `itoa`/`ftoa`/`joinStrings` | CQ-006 | `pkg/strategy/plugins/utils.go` | TBD | 0.5d | 标准库替换；测试通过 | ⬜ |
| **P1-28** | Redis 缓存 key namespace 化 (`quantlab:` 前缀) | AR-021 | `pkg/storage/redis.go` | TBD | 1d | 全部 key 加前缀；InvalidateOHLCV 限定 pattern | ⬜ |
| **P1-29** | 持仓超限/行业集中度/回撤告警 (AlertManager) | BR-015, ADR-017 | `pkg/alert/manager.go` (新建) | TBD | 1w | 6 类 P0 风险告警；webhook 渠道 | ⬜ |
| **P1-30** | E2E AI Copilot 端到端 + SSE 进度 | TQ-016, BR-014 | `e2e/tests/ai-copilot-e2e.spec.ts` | TBD | 3d | Playwright 自然语言 → 回测 → 展示 | ⬜ |

### 🟢 Sprint 6 P2 — Backlog (33 项)

> **验收**: Sprint 6 第 3 周末 (2026-07-02) 前 P0/P1 优先；P2 按 ROI 决定。

| ID | 任务 | 关联问题 | 文件 | Owner | 估时 | 验收标准 | 状态 |
|---|---|---|---|---|---|---|---|
| **P2-1** | backtest 报告 PDF/HTML 导出 | BR-014 | `pkg/backtest/export.go` (新建) | TBD | 3d | `/api/backtest/:id/export/{pdf,html}` | ⬜ |
| **P2-2** | 多策略对比 UI (`/backtest/compare`) | BR-014 | `web/src/pages/BacktestCompare.vue` (新建) | TBD | 3d | 2-N 曲线叠加 | ⬜ |
| **P2-3** | 远程紧急平仓 (EMERGENCY FLATTEN 按钮) | BR-018 | `pkg/live/trader.go:EmergencyStop`, `web/src/components/EmergencyButton.vue` | TBD | 2d | 双重身份验证 + 短信告警 | ⬜ |
| **P2-4** | 投资者适当性 (创业板/科创板/北交所) | BR-005, BR-011 | `pkg/compliance/appropriateness.go` (新建) | TBD | 1w | 10/50/100 万 + 24 月验证 | ⬜ |
| **P2-5** | 异常交易监控 (6 类) | BR-011 | `pkg/compliance/abnormal_trade.go` (新建) | TBD | 1w | 频繁撤单/自成交/对倒/洗售/虚假申报/拉抬打压 检测 | ⬜ |
| **P2-6** | 大额交易报告 (单笔 ≥200万 / 累计 ≥500万) | BR-011 | `pkg/compliance/reporter.go` (新建) | TBD | 3d | 日终 reporter 生成 report.json | ⬜ |
| **P2-7** | 减持规则引擎 (控股股东 ≤3月 ≤1%) | BR-011 | `pkg/compliance/divestment.go` (新建) | TBD | 1w | 3 类股东减持规则 | ⬜ |
| **P2-8** | 券资金对账 Worker (每 15min) | BR-012 | `pkg/live/reconciliation.go` (新建) | TBD | 1w | 偏差 > 阈值报警 | ⬜ |
| **P2-9** | 融资融券 + 做空 | BR-005, BR-007 | `pkg/live/margin.go` (新建) | TBD | 2w | MarginAccount + ShortableList | ⬜ |
| **P2-10** | 可转债策略 | BR-005 | `pkg/strategy/plugins/convertible_bond.go` (新建) | TBD | 1w | 转股价值/纯债价值/赎回回售 | ⬜ |
| **P2-11** | 期权定价 + Greeks | BR-005 | `pkg/strategy/options/` (新建) | TBD | 2w | Black-Scholes + Binomial | ⬜ |
| **P2-12** | 港股通/北向因子 | BR-005 | `pkg/data/source/hkex/` (新建) | TBD | 1w | 陆股通净流入 + 汇率换算 | ⬜ |
| **P2-13** | 退市 + 北交所 30% 涨跌停 | BR-005, BR-006 | `pkg/live/stock_state.go` (新建) | TBD | 3d | delisted_date 字段 + 强制清仓逻辑 | ⬜ |
| **P2-14** | 止盈/移动止盈/分批止盈 | BR-007 | `pkg/risk/take_profit.go` (新建) | TBD | 1w | TakeProfitRule 接口 + 3 种实现 | ⬜ |
| **P2-15** | 分红/送股/拆股/配股/增发 | BR-006 | `pkg/domain/corporate_action.go` (新建) | TBD | 1w | CorporateAction 接口 + 5 种行为 | ⬜ |
| **P2-16** | API 版本化 (`/api/v1` 强制) | AR-020 | `cmd/analysis/main.go` | TBD | 1d | legacy 路由 301 → v1 | ⬜ |
| **P2-17** | OpenAPI 3.0 spec 自动生成 | AR-020 | `docs/openapi.yaml` (新建) | TBD | 2d | swagger 端点可用 | ⬜ |
| **P2-18** | pkg/data/source ETL 真实集成测试 (dockertest) | TQ-016 | `pkg/data/source/integration_test.go` (新建) | TBD | 2d | 9 个 adapter 真实测试 | ⬜ |
| **P2-19** | pkg/ai/gene_pool 持久化测试 (覆盖 41.3%→60%) | TQ-011 | `pkg/ai/gene_pool/integration_test.go` (新建) | TBD | 2d | 覆盖率 ≥ 60% | ⬜ |
| **P2-20** | pkg/risk 边界测试 (60%→70%) | TQ-016 | `pkg/risk/*_test.go` | TBD | 2d | stoploss/regime/volatility 边界 | ⬜ |
| **P2-21** | pkg/ai/pipeline 端到端测试 (57%→70%) | TQ-016 | `pkg/ai/pipeline/e2e_test.go` (新建) | TBD | 3d | Intent → YAML → Code → Compile → Backtest | ⬜ |
| **P2-22** | pkg/domain 类型边界测试 (0%→80%) | TQ-016 | `pkg/domain/types_test.go` (新建) | TBD | 1d | OHLCV/Portfolio/Signal zero value + JSON | ⬜ |
| **P2-23** | pkg/httpclient 测试 (0%→80%) | TQ-016 | `pkg/httpclient/*_test.go` (新建) | TBD | 1d | timeout/retry/backoff 测试 | ⬜ |
| **P2-24** | pkg/logging 日志脱敏 (0%→80%) | TQ-016 | `pkg/logging/*_test.go` (新建) | TBD | 1d | API key/账号脱敏测试 | ⬜ |
| **P2-25** | pkg/ai/client LLMClient interface 测试 (78%→90%) | ADR-018 | `pkg/ai/client_test.go` (扩展) | TBD | 1d | 所有 interface 方法 mock 覆盖 | ⬜ |
| **P2-26** | E2E 视觉回归 (Playwright 截图对比) | TQ-016 | `e2e/tests/visual-regression.spec.ts` | TBD | 2d | 关键页面截图 baseline | ⬜ |
| **P2-27** | WASM sandbox (wazero) | AR-003, ADR-007/019 Phase 3 | `internal/sandbox/wasm/` (新建) | TBD | 1mo | 完全内存隔离 | ⬜ |
| **P2-28** | EventBus backpressure (drop-oldest 策略) | AR-018 | `pkg/marketdata/eventbus.go` | TBD | 2d | 5000+ 标的实时不阻塞 | ⬜ |
| **P2-29** | 跨日状态持久化测试 (Quant 状态) | TQ-016 | `pkg/backtest/persistence_test.go` (新建) | TBD | 2d | 服务重启不丢历史 | ⬜ |
| **P2-30** | 数值精度 (float64 vs Decimal) | TQ-016 | `pkg/decimal/` (新建) | TBD | 1w | 累计误差 < 0.01 CNY | ⬜ |
| **P2-31** | 拆分 3 个长函数 (getSignals/SubmitOrder/CalculatePosition) | CQ-011 | 3 个文件 | TBD | 2d | 每个函数 < 50 行 | ⬜ |
| **P2-32** | 删除 4 处 `Test*_Cleanup` 空测试 | TQ-002 | `pkg/strategy/plugins/coverage_test.go` | TBD | 0.5d | 删除或补 assert | ⬜ |
| **P2-33** | 删除 `assert.True(t, true)` placeholder | TQ-001 | `pkg/storage/postgres_screen_test.go:235` | TBD | 0.5d | pgxmock 真实 SQL 验证 | ⬜ |

### Sprint 6 验收 Gate (2026-07-02)

| Gate | 命令/标准 | 通过条件 |
|---|---|---|
| **G1** | `go test -race -count=1 ./...` | 0 panic, 0 race condition |
| **G2** | `go vet ./...` | 0 issue |
| **G3** | `pkg/storage` 覆盖率 | ≥ 60% |
| **G4** | `pkg/ai/agents` 覆盖率 | ≥ 60% |
| **P5** | `pkg/ai/pipeline` 覆盖率 | ≥ 70% |
| **G6** | 73 项任务完成率 | ≥ 80% (59/73) |
| **G7** | Docker compose 服务数 | ≤ 3 (analysis/data/ai) |
| **G8** | ADR/ODR 索引同步 | 20 ADR + 13 ODR |

### Sprint 6 任务分布

| Owner Role | 任务数 | 工作日估算 |
|---|---|---|
| Backend Go | 38 项 | ~25d |
| Frontend Vue | 8 项 | ~5d |
| Database/SQL | 5 项 | ~3d |
| Infrastructure/DevOps | 6 项 | ~4d |
| Documentation | 4 项 | ~2d |
| E2E Tests | 5 项 | ~3d |
| Cross-cutting (Auth/Logging) | 7 项 | ~5d |
| **Total** | **73 项** | **~47 工作日 (3 人 × 3 周)** |

### Sprint 6 Top 10 ROI 排序 (与 ODR-013 Top 10 一致)

1. **P0-1** LLMClient interface 化 (1d, CI panic fix)
2. **P0-6** pkg/backtest 并发 map 修复 (2d, 消除 critical 竞态)
3. **P0-7** pkg/storage dockertest (3d, 8.2%→60%)
4. **P0-3** OTel + Prometheus (2d, 可观测性 25→70)
5. **P1-15** Service 合并 (1w, 部署 -50%)
6. **P1-2** JWT + RBAC (1w, 实盘前置)
7. **P1-4** 中泰 XTP 对接 (2w, 真正具备实盘能力)
8. **P0-10** + **P1-21/22/23** Go 现代化 (1w, 7.2→8.5)
9. **P1-12** L4 validate 实际实现 (3d, Phase 4 P0 fail gate)
10. **P1-16~20** Engine 拆分 (2w, 1408 行→300 行)

---

## 🔍 Sprint 6 启动期 待校核项 (6 项)

> **来源**: [ODR-013 §对齐审计复核 (2026-06-11)](odr/odr-013-comprehensive-audit-2026-06-11.md#对齐审计复核-2026-06-11-同日)
> **状态**: 路径引用为 (新建) — 任务执行时精确校核
> **原则**: [VISION.md §Principle 8](VISION.md#principle-8-documentation-path-consistency)

ODR-013 综合审查生成的 73 项 Sprint 6 任务中，有 6 项涉及"待新建文件"或"待引入依赖"，无法在 Sprint 6 启动时（2026-06-11）做精确路径/行号对照。这些项在对应任务执行时再做最终校核。

| 任务 | 待校核内容 | 校核触发点 | 校核 Owner |
|------|----------|----------|-----------|
| **P0-7** | `pkg/storage/integration_test.go` (新建) | P0-7 实施时确认 dockertest 引入 + integration_test.go 文件创建 | TBD |
| **P0-3** | OpenTelemetry/Prometheus go.mod 依赖 | P0-3 实施时 `go.mod` 增补（搜索 `go.opentelemetry.io`、`github.com/prometheus/client_golang`） | TBD |
| **P1-2 / P1-8** | `migrations/019_add_auth_tables.sql` (新建) | P1-2 实施时创建；格式遵循扁平式（与 `012_add_sync_jobs_table.sql` 一致） | TBD |
| **P1-11** | `internal/sandbox/runner/` (新建) | P1-11 实施时创建（Phase 2 sandbox 进程隔离 + rlimit + timeout） | TBD |
| **P1-21/22/23** | `pkg/statistics/`, `pkg/fees/`, `pkg/id/` (新建) | 对应任务实施时创建（Go 现代化拆分） | TBD |
| **P1-29** | `pkg/alert/` (新建) | P1-29 实施时创建（AlertManager 告警框架） | TBD |

### 校核操作清单 (Checklist)

实施上述任一任务时，须执行：

```bash
# 1. 验证路径是否已存在
ls -la pkg/storage/integration_test.go 2>/dev/null
ls -la internal/sandbox/runner/ 2>/dev/null

# 2. 验证依赖是否已引入
grep -E "opentelemetry|client_golang" go.mod

# 3. 验证 migrations 序号下一个可用
ls migrations/ | sort

# 4. 若不匹配，更新任务描述
edit docs/TASKS.md  # 修正路径/依赖声明
```

### 校核完成判定

- [ ] 6 项全部校核完成（Sprint 6 第 1 周末前）
- [ ] 校核结果填入 ODR-013 §对齐审计复核 状态表
- [ ] 任何与原始描述不符的项，更新 TASKS.md 任务描述（不创建新任务）

---

## 🔗 相关文档

| 文档                               | 用途             |
| -------------------------------- | -------------- |
| [ROADMAP.md](ROADMAP.md)         | Sprint 进度和里程碑  |
| [PHASE3-PLAN.md](PHASE3-PLAN.md) | Phase 3 实施计划详情 |
| [archive/NEXT\_STEPS.md](archive/NEXT_STEPS.md)  | 审查发现详情         |
| [TEST.md](TEST.md)               | 测试策略和覆盖率目标     |
| [ODR-013](odr/odr-013-comprehensive-audit-2026-06-11.md) | Sprint 6 综合审查记录 |
| [ADR-017](adr/adr-017-observability-and-auth.md) ~ [ADR-020](adr/adr-020-engine-decomposition.md) | Sprint 6 架构决策 |

***

_Last updated: 2026-06-11 (v3.10.0) — Sprint 6 (ODR-013) 73 项任务全量入库：P0×10 + P1×30 + P2×33；ADR-007/008 Accepted + ADR-017~020 Proposed；ODR-013 审计 + 6 跨维度系统性问题_
_Source: 整合自 CODE\_REVIEW\_REPORT.md + NEXT\_STEPS.md + PHASE3-PLAN.md + AGENTS.md + ODR-011 + Sprint 5 综合审查 + Sprint 6 (ODR-013) 综合审查_

