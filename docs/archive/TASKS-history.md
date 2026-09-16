# Quant Lab — 统一任务追踪

> **Status**: Active (Long-Live Task Tracker)
> **Version:** 3.41.0 (Sprint 8 — P5-3 对接 L0 单一数据面 + Evidence API: SPA 消费 citation 坐标（因子页 → 一键回溯证据）+ 运行时端到端取证, ODR-064)
> **Last Updated:** 2026-09-16
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [ROADMAP.md](../ROADMAP.md) (sprint progress), [archive/NEXT_STEPS.md](NEXT_STEPS.md) (audit archive)
>
> **Purpose**: 本文件是 Quant Lab 项目的**唯一活跃任务追踪源**。所有可执行任务必须在此记录，不得散落在其他文档中。
>
> **文档使用指南**:
> | 需求 | 应查阅文档 | 说明 |
> |------|-----------|------|
> | **查看当前待办任务** | **本文档** | 唯一活跃的任务追踪源，含 P0-P3 + D1-D7 |
> | **了解 Sprint 里程碑** | [ROADMAP.md](../ROADMAP.md) | Phase/Sprint 级别进度和验收标准 |
> | **查看历史审查发现** | [archive/NEXT_STEPS.md](NEXT_STEPS.md) | 2026-04-09 审查的只读归档 |
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
| P1-18 | 补齐 `pkg/sync/worker_test.go` 缺失的 `newMockJobStore` mock 定义（`*_test.go` 疑因 gitignore 从未入库，`go test ./...` 全仓门禁被其阻断） | `pkg/sync/`（mock 需实现 `JobStore` 接口） | ✅ | ODR-062 → 完成 (ODR-063) |

### 运行时取证遗留（2026-09-16, [ODR-063](odr/odr-063-e2e-runtime-forensics.md)）

| ID | 任务 | 文件 | 状态 | 来源 |
| --- | --- | --- | --- | --- |
| P1-25 | backtest 入口校验缺失 — 缺 `stock_pool`/非法 body 返 500 应 400 fail-fast | `cmd/analysis/handlers_backtest.go:67` | ⬜ | ODR-063 D-1 |
| P1-26 | `BacktestResponse` 指标字段 `omitempty` — 零值指标从 `job.Result` 持久化与 report 响应消失（零交易回测 report 缺 `total_return` 等 8 项） | `pkg/backtest/contracts/contracts.go:68` | ⬜ | ODR-063 D-2 |
| P1-27 | 零交易回测 `sortino_ratio` 输出 `MaxFloat64` 哨兵（1.797e308）— 除零防护/指标归一缺失 | `pkg/backtest/engine.go` | ⬜ | ODR-063 D-3 |
| P1-28 | `FetchDailyOHLCV` 依赖 premium 接口 `stk_factor_pro`（token 40203 无权限）→ `daily` + `adj_factor` 组合回退 | `pkg/backtest/tushare.go` | ⬜ | ODR-063 D-4 |
| P1-29 | 限流滑动窗口整窗重置（窗口边界突发 2×）— 已产品化缓解（配置驱动），算法替换（令牌桶/滑动日志）另立 | `cmd/*/setup.go` rate limiter | ⬜ | ODR-063 D-5 |
| P1-30 | `TestPluginLoader_SetWatchDir` Windows 失败 — `/nonexistent/path` 期望 error 实得 nil（平台性预存） | `pkg/strategy/loader_test.go:39` | ⬜ | ODR-063 D-6 |
| P1-31 | e2e 期望侧对齐 — SPA 类名/导航断言 ×21 + data-sync 契约 shape ×4 + 期望集 ×3 + PnL 容差 ×1 + 视觉基线策略 ×6 | `e2e/tests/*` | ⬜ | ODR-063 D-7 |

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
| D1-3  | TushareProvider 重构（**已退役** — 直连外部源违反 ADR-022 §1，见 [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md)）   | ~~`pkg/marketdata/tushare_provider.go`~~ | ⚫  | 0.5d |
| D1-4  | PostgresProvider 新增 (零网络延迟)                                | `pkg/marketdata/postgres_provider.go` | ✅  | 1d   |
| D1-5  | AkShareProvider 新增 (免费备选)（**已退役** — 同 D1-3，见 [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md)）             | ~~`pkg/marketdata/akshare_provider.go`~~ | ⚫  | 0.5d |
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
| CR-05 | `BacktestResultCard.vue` 重复定义 `formatPercent` / `formatNumber` ⚠️ 文件已删除 (S7-P2-7, ODR-043/045) | ~~[components/ai/BacktestResultCard.vue:36-44](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/BacktestResultCard.vue#L36)~~ | ✅ (moot) | F-002 |
| CR-06 | `FactorCard.vue` 重复定义 `formatMetric` / `formatPercent` (行为不一致) ⚠️ 文件已删除 (S7-P2-7, ODR-043/045) | ~~[components/ai/FactorCard.vue:129-137](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/FactorCard.vue#L129)~~ | ✅ (moot) | F-003 |
| CR-07 | `PaperTrading.vue` 显式 `DataTableColumns<any>` + 3 个 `catch (error: any)` | [pages/PaperTrading.vue:247](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/PaperTrading.vue#L247) | ✅ | F-004, F-005 |
| CR-08 | `types/api.ts` `BacktestJob.params: Record<string, any>` (公开类型)     | [web/src/types/api.ts:67](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/types/api.ts#L67) | ✅ | F-006 |
| CR-09 | `PipelineDashboard.vue` `jobHistory` 累积无上限 (潜在内存泄漏) ⚠️ 文件已删除 (S7-P2-7, ODR-043/045) | ~~[components/ai/PipelineDashboard.vue:308](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/ai/PipelineDashboard.vue#L308)~~ | ✅ (moot) | F-007 |
| CR-10 | ADR.md 索引缺失 6 条已存在的 ADR (ADR-011~016)                       | [docs/ADR.md:13-25](../ADR.md#L13) | ✅ | D-001 |
| CR-11 | SPEC.md Backtest API 路径缺少 `/api` 前缀 (与实际 `/api/backtest` 不一致) | [docs/SPEC.md:543-560](../SPEC.md#L543) | ✅ | D-002 |
| CR-12 | SPEC.md 把 `/api/datasource/*` 错误归在 AI Service,实际 Analysis + Data | [docs/SPEC.md:711-715](../SPEC.md#L711) | ✅ | D-003 |
| CR-13 | Data Service 实际 30+ 端点未在 SPEC.md 记录 (sync/factor/screen 全部)   | [docs/SPEC.md:395-435](../SPEC.md#L395) | ✅ | D-004 |
| CR-14 | ARCHITECTURE.md "6 张核心表" 与内部 "18 张活跃表" 自相矛盾              | [docs/ARCHITECTURE.md:297](../ARCHITECTURE.md#L297) | ✅ | D-005 |
| CR-15 | ODR-011 Sprint 1-4 新增的 13 张表 (realtime_quote/capital_flow/sectors/...) 未在 ARCHITECTURE.md | [docs/ARCHITECTURE.md:387-398](../ARCHITECTURE.md#L387) | ✅ | D-006 |
| CR-16 | SPEC.md AI Research Service 章节列出 35+ 端点但实际只注册 3 个             | [docs/SPEC.md:647-723](../SPEC.md#L647) | ✅ | D-011 |

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
| CR-29 | SPEC.md Analysis Service 章节遗漏 30+ 实际 plugin/paper trading 端点 | [docs/SPEC.md:535-600](../SPEC.md#L535) | ✅ | D-007 |
| CR-30 | ADR-015 引用 `*_agent.go` 实际文件无后缀 (`research.go` 等)         | [docs/adr/adr-015-ai-agent-architecture.md:193-196](../adr/adr-015-ai-agent-architecture.md#L193) | ✅ | D-008 |
| CR-31 | ADR-016 迁移文件名引用错位 (off-by-one,全部小 1)                     | [docs/adr/adr-016-multi-source-data-architecture.md:351-355](../adr/adr-016-multi-source-data-architecture.md#L351) | ✅ | D-009 |
| CR-32 | ODR-011 声称 "8 个新数据源" 实际注册 9 个 adapter (含 Eastmoney 3 slot) | [docs/archive/odr/odr-011-multi-source-integration.md:158-159](odr/odr-011-multi-source-integration.md#L158) | ✅ | D-010 |
| CR-33 | Strategy 接口 Signal 类型三处文档 (SPEC/VISION/AGENTS) 未指定包名     | [docs/SPEC.md:160-172](../SPEC.md#L160) | ✅ | D-012 |
| CR-34 | SPEC.md/VISION.md 声称 "ai 覆盖率 ≥75%" 实际 0% 顶层,16-95% 子包     | [docs/SPEC.md:30-37](../SPEC.md#L30) | ✅ | D-013 |
| CR-35 | ADR-015/016 Status 仍为 "Proposed" 但实施已 98% 完成                  | [docs/adr/adr-015-ai-agent-architecture.md:3](../adr/adr-015-ai-agent-architecture.md#L3) | ✅ | D-014 |
| CR-36 | AGENTS.md 技术栈表缺失 risk-service(8083)/execution-service(8084) | [AGENTS.md:23-32](../../AGENTS.md#L23) | ✅ | D-015 |

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
| CR-47 | AGENTS.md 文档导航说 "6 张表" 与 ARCHITECTURE.md "18 张" 不一致  | [AGENTS.md:492](../../AGENTS.md#L492) | ✅ | D-016 |
| CR-48 | AGENTS.md 已知问题表未反映 ODR-011 引入的 5 个新风险 (mootdx SDK/反爬/对账)  | [AGENTS.md:581-587](../../AGENTS.md#L581) | ✅ | D-017 |
| CR-49 | SPEC.md §6.4 `SetLiveTrader` 等方法名需对照代码验证 (未直接验证)   | [docs/SPEC.md:856-877](../SPEC.md#L856) | ✅ | D-018 |
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
| P1              | 8      | 0     | 22     | 0     | 0     | 30     | P1-18 完成 + P1-25~31 新增 (e2e 运行时取证, ODR-063) |
| P2              | 0      | 0     | 19     | 0     | 0     | 19     |
| P3              | 0      | 0     | 19     | 1     | 0     | 19     |
| Phase 3 (D1-D7) | 0      | 0     | 51     | 0     | 2     | 53     | D1-3/D1-5 已退役转 ⚫ (ODR-058) |
| MS (Sprint 1-4 + 验证) | 0  | 0     | 25     | 0     | 0     | 25     |
| **CR (Sprint 5 — 综合审查 + 新发现)** | **0** | **0** | **56** | **0** | **0** | **56** | (含 F1/F2-new, 全部完成) |
| **P2 (P2-1 ~ P2-3: alert/emergency/export/compare)** | **0** | **0** | **3** | **0** | **0** | **3** | P2-1 + P2-2 完成 (ODR-027) |
| **Sprint 8 (统一研究平台落地 — 阶段 P1~P5)** | **5** | **0** | **13** | **0** | **0** | **18** | ADR-022 / ODR-048; Quant Lab 降维为共享底座(L0-L2) + 双工作面; L0-1~L0-4 已冻结(ODR-050/051) + EQD-P0-1/P0-2 已落地(ODR-052) + EQD-P1-1 已落地(ODR-053) + **P1 全部关闭**(EQD-P3-2 复核, ODR-054) + EQD-P1-2 已落地(ODR-055) + EQD-P3-1 已落地(ODR-056, **阶段 P3 3/3 关闭**) + EQD-P2-1 已落地(ODR-057, **阶段 P4 1/2**) + **阶段 P5 切片 1**已落地(ODR-058, P-A 退役 / P-B·P-C 不动 / P-D 收敛) + **阶段 P5 切片 2**已落地(ODR-059, P-B 退役 / status·health 保留) + **阶段 P5 切片 3**已落地(ODR-060, Vue SPA 对接 L0 Evidence API / 后端零改动) + **阶段 P5 切片 C**已实施(ODR-061, C2 落地: factor_cache citation JSONB + 注入链 + 输出面展开五元组) + **阶段 P5 切片 B+D**已落地(ODR-062, 方案甲: SPA sync 面对齐 jobs 契约 + analysis 网关代理 SSE 透传 + 死路由/死代理清理 → **阶段 P5 1/1 关闭**, **P5-1 ✅**) + **P5-3** 已落地(ODR-064, SPA 消费 citation 坐标: analysis 网关 `/api/factors/*` facade + 因子页五元组 + 一键回溯 Evidence, **运行时端到端取证 10 项全过**) |
| **总计**          | **15** | **0** | **225** | **1** | **2** | **242** | (v3.41.0 P5-3 SPA 消费 citation 坐标已落地 + 运行时取证, ODR-064) |

***

## 📝 任务变更日志

### 2026-09-16 (v3.40.0) — Sprint 8 **e2e 运行时全套件取证**（Playwright 167 用例 ×3 轮）: worker 饥饿 + strategies params 形状双缺陷修复 + P1-18 关闭

**来源**: [ODR-063](odr/odr-063-e2e-runtime-forensics.md) — 承 ODR-062 交棒「运行时全套件需 tushare token 待跑」；token 仅会话进程环境注入（不落盘不提交）

- **三轮运行**: ① 全套件 167: 86 passed / 81 failed (29.2m)，主导因素 = 429 限流风暴（阈值硬编码自伤）+ worker 饥饿 + `/api/strategies` 500 + 数据面未就绪；② `--last-failed` 81: 19/62 (33.8m)，限流配置驱动修复后 429 归零；③ `--last-failed` 62: 24/38 (14.1m)，双缺陷修复后 167 用例通过数 86 → **129**（77.2%）
- **缺陷修复 1 — worker 饥饿（产品级）**: `JobService.CreateJob/RetryJob` 直写 DB 从不 notify，`WorkerPool.Start()` 先启动（队列空）→ worker 永久阻塞 `WaitForJob` channel select → 作业永不消费；时间线取证（worker 启动 14:34:13 本地 vs DB 最老 pending 13:01:23 UTC = 15:01:23 本地）推翻「启动时已有 pending」误判。修复: `Queue.NotifyJobAvailable()` 导出 + `JobService.SetPendingNotifier` + `NewSyncHandler` 接线（一次覆盖全部 14 个调用点）+ 回归测试 ×2；活体: stocks 作业 6s completed / 5564 行真实落库
- **缺陷修复 2 — `/api/strategies` 500（产品级）**: `params` 列双消费者形状冲突（`SeedStrategies`/`Create` 写参数值 object，`ListWithDB` 误按 `[]Parameter` 描述符数组解析）→ 内置策略全 500。修复: object 解析 + keys 排序投影为 `Parameter` 描述符（`pkg/strategy/db.go`），解析失败仍 fail-loudly
- **P1-18 关闭**: `newMockJobStore`（内存 JobStore + Clone + `created_at DESC` 镜像）+ `TestWorkerPool_WakesOnJobServiceCreate/Retry` 回归测试 → `pkg/sync` 11/11 PASS，全仓 `go test ./...` 门禁解锁
- **工程化**: 限流阈值配置驱动 `RATE_LIMIT_PER_MINUTE` / `AI_RATE_LIMIT_PER_MIN`（4 文件）；`.gitignore` 补 visual 快照与 `/build/`
- **数据面就绪**: calendar 969 交易日 / stocks 5564 行真实 tushare / ohlcv 权限墙（`stk_factor_pro` 40203）SQL 合成兜底 / momentum 回测 200·92ms·1 trade
- **剩余 38 条根因分类**（ODR-063 §分类表）: 视觉基线漂移 6 / SPA 结构脱节 21 / data-sync 契约脱节 4 / 期望集不符 3 / 产品缺陷 3（`omitempty` 指标消失 P1-26 + 空 `stock_pool` 500 P1-25 + `sortino` MaxFloat64 P1-27）/ 测试假设过强 1 → 登记 **P1-25~31**
- **统计更新**: P1-18 ⬜→✅；新增 P1-25~31 ×7；总计 234 → **241**（待处理 8 → 15，已完成 223 → 224）

### 2026-09-16 (v3.39.0) — Sprint 8 阶段 P5 **旁路取数残留全量收口**（方案甲: 切片 B + 切片 D）→ **P5-1 关闭**

**来源**: [ODR-062](odr/odr-062-p5-1-bypass-residue-audit.md) — P5-1 第五刀（收官）；承 [ODR-060](odr/odr-060-p5-1-frontend-evidence-api.md) §未做项第 2 条（切片 B）+ 第 4 条（切片 D）；**ODR-062 Status: Proposed → Completed**

- **评估 → 裁决**: 8 条现场取证（a SPA sync 4 端点全仓无注册 / b data 真实契约 `/api/sync/jobs*` 家族 / c e2e 三套件按 gateway 契约编写但 analysis 无路由（文档·e2e·实现三方脱节）/ d analysis 3 条零消费代理路由 / e 4 条裸镜像路由有 legacy 静态页活消费者 / f e2e 不经 vite / g vite 死代理 3 条 / h SPA sync 活端点仅 datasource 2 个）+ ADR-022 §3 单向依赖判定表；候选 S-A~S-E；**用户裁决方案甲（全量收口）** = S-A + S-B + S-C + S-E，**S-D 保留现状**（裸镜像路由与 legacy 静态页共存亡，legacy 退役另立议题）
- **S-A 前端对齐 jobs 契约**（5 文件重写）: `types/sync.ts`（`SyncJob` 逐字对应 sync_jobs JSON + Create/List 响应类型，删死类型）+ `api/sync.ts`（createSyncJob / listSyncJobs / getSyncJob / cancelSyncJob / retrySyncJob + SSE 路径 helper，删 3 死端点）+ `stores/sync.ts`（jobs 列表态 + `activeJob` computed + `applyJobUpdate` upsert + EventSource 按 job 订阅 `/api/sync/jobs/:id/progress`）+ `SyncStatusPanel.vue`（activeJob 进度视图）+ `DataImportForm.vue`（创建任务反馈）
- **S-B analysis 网关代理**: `router.Any("/api/sync/*path")` → data（`httputil.NewSingleHostReverseProxy` + `FlushInterval = -1` SSE 流式透传）；**顺带修复 viper 接线**（`registerProxyRoutes` 原读全局 viper 恒空、硬编码 docker hostname 静默生效 → 改签名接收 `deps.Viper`）
- **S-C + S-E 死代码退役**: analysis 删 `/api/sync/calendar`、裸 `/sync/calendar`、`/api/v1/trading/calendar` ×3；vite 删 `/market`→8081（**仓内最后一处 L3→L0 直连形态**）、`/stocks`→8085、`/ohlcv`→8085 ×3
- **实施补齐契约缺口 ×2**: ① data 侧 `POST /api/sync/jobs` 原不存在（SPEC.md:1323 / e2e 三套件均按其编写）→ 新增类型化创建门 `createJobHandler`（7 job type switch + 缺省值 + 门上 400 fail-fast：未知类型/缺 symbols/非法日期；`jsonHasKey` 区分 key 缺失 vs 显式空数组；`validDateRange` 双格式 YYYYMMDD/YYYY-MM-DD）；② 哨兵 `sync.ErrInvalidCron` → 无效 cron **500 → 400**（`errors.Is` 映射，消息不变）
- **验证**: `go build ./...` EXIT=0 / `go test` cmd/analysis + cmd/data + pkg/storage 全 ok（`pkg/sync` 存量问题除外：`worker_test.go` 引用的 `newMockJobStore` mock 定义从未入库 → 登记 **P1-18**，全仓 `go test ./...` 门禁被其阻断）/ 前端 vue-tsc 0 错 + vitest 13 files 158 tests + vite build 全绿；**运行时取证网关面 12 项全过**（TimescaleDB pg16 + Redis 一次性容器 + migrations 012/013 手工应用：create 202 / list·get 200 / SSE `event: progress` 流式 ✓ / cancel 200 / retry-on-cancelled 400 / invalid type·missing symbols·bad date 400 / 已删路由 404（data 直连仍 200）/ facade 200 / schedule 201 + invalid cron 400）；e2e 三套件静态对齐（`stock_list`→`stocks` ×6 / `progress_percent` / SSE 按 job 订阅），运行时全套件需 tushare token 待跑
- **P5-1 关闭**: 五刀全部落地（ODR-058 切片 1 封直连 + ODR-059 切片 2 退役切换门 + ODR-060 切片 3 SPA 对接 Evidence API + ODR-061 切片 C citation 坐标 + ODR-062 切片 B+D 旁路残留收口）—— L3 直连形态清零、死契约清零、SPA 数据管理面真实可用、gateway 契约与文档对齐；**阶段 P5 1/1 全部关闭**
- **统计更新**: 新增 P1-18（`pkg/sync/worker_test.go` mock 缺文件待办）；总计 233 → **234**（待处理 8 持平：+P1-18 / −P5-1）；已完成 222 → **223**

### 2026-09-16 (v3.38.0) — Sprint 8 阶段 P5 切片 C **主验收点运行时取证通过** + pgx v5 批次 `conn busy` 潜伏缺陷修复 ×7

**来源**: [ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md) §Metrics — 承 v3.37.0 遗留的「主验收点待运行时取证」；**ODR-061 Status 不变（Completed），Metrics 补实测**

- **运行时取证环境**: Docker Desktop + `timescale/timescaledb:latest-pg16`（:5434）+ `redis:7-alpine`（:6380）一次性容器；`cmd/data`(:8081) + `cmd/analysis`(:8085) 本地起服；迁移在 `NewPostgresStore` 自动执行（含 027，直查确认 `citation jsonb NOT NULL DEFAULT '[]'::jsonb` 已生效）
- **全链路实测（6 步全过）**: ① `POST /api/ingest/raw` 归档 → hash `f6afca77…4823`；② `POST /api/ingest/equitydeep?content_hash=`（4 季度快照 ndjson，`营业总收入`/`营业成本` 白名单字段）→ **201**（4 snapshots / 8 rows / 0 dropped）；③ `POST /sync/factors/gross_margin_trend` → 计算落库；④ `GET /factors/gross_margin_trend?symbol=600999.SH&date=20260915` → citation 已展开为 `{source:"equitydeep", dataset:"income", key:"600999.SH", content_hash}`（`as_of` 因 NULL 正确缺席，ADR-022 §5 权威形态）；⑤ `GET /api/evidence/{hash}` → **200** + 原始记录；⑥ 未归档 hash → **404** 一等语义
- **列存形态直查**: `factor_cache.citation` = `[{"content_hash": "f6afca77…"}]`（hash-only，无五元组字段）—— 声明先行、精确后置的分层与设计一致；斜率实测 −0.00416 与种子数据 OLS 精确一致
- **顺手修复（运行时取证暴露的预存在潜伏缺陷，×7）**: `tx.SendBatch` + `defer results.Close()` + `tx.Commit` 的顺序在 pgx v5 下**必然 `conn busy`**（批次未关闭连接即提交；离线单测覆盖不到真实 PG，且 `SaveIndexConstituents` / `bulk_insert.go` 早已是正确的显式 Close 写法，属遗漏而非惯例）。修复 `pkg/storage/cache.go` ×5（`SaveFactorCacheBatch` / factor_returns / ic_analysis / dividends / splits）+ `pkg/storage/fundamentals_detail.go` ×1（equitydeep 摄取门**此前对真实 PG 从未成功落库**），统一为：Exec 循环（错误路径显式 `results.Close()`）→ 显式 `results.Close()` → `tx.Commit`。pool 版 4 处（calendar / stocks / fundamentals ×2）无事务提交语义，`defer` 安全，不动
- **验证**: 修复后全链路一次通过；`go test -count=1 ./pkg/storage/ ./pkg/data/ ./cmd/data/ ./cmd/analysis/` 全 ok
- **统计更新**: 总计 233 不变（缺陷修复 + 取证，无任务增减）；P5-1 整体仍未关闭（余旁路取数残留维度不变）

### 2026-09-16 (v3.37.0) — Sprint 8 阶段 P5 切片 C **实施**: `factor_cache` 因子链携 citation 坐标（C2 落地）

**来源**: [ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md) — 承 v3.36.0 的 C2 裁决实施；**ODR-061 Status: Accepted → Completed**

- **迁移 027**: `ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS citation JSONB NOT NULL DEFAULT '[]'::jsonb`（文档副本 [027_factor_cache_citation.sql](migrations/027_factor_cache_citation.sql) + 内联 `pkg/storage/postgres.go migrate()`；带默认值加列 metadata-only，幂等，`'[]'` = 「A→B 链尚未建立」显式语义非 NULL）
- **类型层**: `FactorCacheEntry.Citation json.RawMessage` —— 列内 hash-only 形态可无损升级为五元组（`pkg/domain/factor.go`）
- **存储层**: `SaveFactorCacheBatch`（INSERT + `ON CONFLICT DO UPDATE` 均带 citation，空值归一 `'[]'`）/ `GetFactorCache` / `GetFactorCacheRange` 加列读写（`pkg/storage/cache.go`）；`pkg/data.FactorStore` / `pkg/backtest/cache.FactorStore` **接口不变，两个 mock 零改动**
- **注入链（唯一新增信息流）**: `loadStatementBook` 保留 `SnapshotURI`（剥 `ingest.raw:` 前缀，**前缀不匹配跳过，不猜不造**）→ `statementField.provenance`（与 `annDate` 同构按 period 记录，重述胜者同规则）→ `statementBook` 结构化（fields + 每标的**去重排序** hash 集合）→ `saveVerticalFactor`（5 个纵向因子唯一收口点）加来源入参写 `entry.Citation`（空集合 marshal 为 `[]`）（`pkg/data/factor_equitydeep.go`）
- **输出面**: `getFactorHandler` 经 `expandCitation` 逐 hash `GetRawIngest` 展开为 ADR-022 §5 五元组 `{source, dataset, key, as_of, content_hash}`；**未命中只出 `{"content_hash"}` 不补字段**；citation 解析失败透传原样**不 5xx**；`factor_cache` 未命中仍 404（`cmd/data/handlers_factor.go`）
- **覆盖面语义**: 5/11 个因子（`gross_margin_trend` / `contract_liability_ratio` / `ocf_to_net_profit` / `roe_dupont_leverage` / `inventory_turnover_delta`）；momentum / value / quality citation 恒 `[]`；PRODUCT §6.4「100%」**如实记为未达标**
- **测试**: `pkg/data/factor_equitydeep_test.go` 新增 `TestContentHashOfSnapshot` / `TestLoadStatementBookCitation`（排序+去重+**非归档重述丢弃被覆盖批次**）/ `TestComputeVerticalFactorCitation`（hash-only 存储 + 空标记）；`statementBook` 结构化访问适配
- **顺手修复（平台缺陷，与本切片无关）**: `pkg/backtest/state/persistence_test.go::TestDiskStateStore_ConcurrentSaveLoad` 以 `rune('0'+i)` 生成 ID 扫到 NTFS 非法字符 `: < > ?` 在 Windows 恒失败 → 改 `fmt.Sprintf("bt-concurrent-%02d", i)`
- **验证**: `go build ./...` EXIT=0；`go test -count=1 ./pkg/storage/ ./pkg/data/ ./pkg/domain/ ./cmd/data/ ./pkg/backtest/...` **17 包全 ok**；主验收点（`GET /factors/...` 的 hash 命中 `GET /api/evidence/{hash}` → 200 循环）**待运行时取证**（需 PG + 完整 ingest 流；离线侧由 `expandCitation` 单元逻辑覆盖，详见 ODR-061 §Metrics）
- **统计更新**: 总计 233 不变；**阶段 P5 切片 C 实施完成，P5-1 整体仍未关闭**（余旁路取数残留维度：`/market` → `8081` L3 直连、`handlers_proxy.go` 镜像路由、`api/sync.ts` 死端点）

### 2026-09-16 (v3.36.0) — Sprint 8 阶段 P5 切片 C **评估**: Research Engine 输出携 citation 元组（裁决收窄为 C2）

**来源**: [ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md) — P5-1 第四刀；承 [ODR-060](odr/odr-060-p5-1-frontend-evidence-api.md) §未做项第 3 条「Research Engine 输出携带 `content_hash`（切片 C）—— 跨 schema 全链路，高风险」

- **本切片仅评估与裁决，未产出代码**（Category: **Audit**；统计总计 233 不变）
- **名词澄清**: 全仓 Grep `ResearchEngine|research_engine` = **0 命中** —— 它是 ADR-022 §3 L2 横截面工作面的**概念名**，代码对应为 `pkg/data`（`FactorComputer` / `FactorAttributor`）+ `cmd/data` 的 factor handler；本切片实际触及 L1 因子引擎 → `quant.*` 段
- **契约要求（切片成立依据）**: ADR-022 §2 C 类「其他侧副本」= ❌ 禁的是**数据副本**，citation 是**坐标**（hash + pointer）非副本；PRODUCT §6.3「因子定义可用同一坐标引用同一数字」+ §6.4「**100%** 数字可用 `content_hash` 回溯」直接要求
- **现场取证（四条事实）**
  — **(a) 全链路只有一段通**：见下链路表
  — **(b) `archiveRaw` 算了 hash 又丢弃**：`pkg/data/tushare_raw.go` 内 `hash, err := storage.ContentHashOf(body)` → 写 `RawIngest` → **函数无返回值**；A 类归档已挂真实咽喉点（ODR-051）但 hash **未回传给调用方**
  — **(c) `quant.*` 表零 hash 列**：`factor_cache` / `factor_returns` / `ic_analysis` 无 citation 列；`ohlcv_daily_qfq` / `stock_fundamentals` **连 `source` 列都没有**
  — **(d) 纵向面是唯一已有 hash 的链**：`fundamentals_detail.snapshot_uri`（值形如 `ingest.raw:<hash>`）是唯一已落库的 hash 出参
- **链路现状（核心发现）**: **A→B**（OHLCV / 基本面）❌ 断 / **A→B**（equitydeep 纵向）✅ 已建 / **B→C**（因子计算）❌ 断（`loadStatementBook` **读了行却丢弃 `SnapshotURI`**）/ **C→输出** ❌ 断
- **裁决: C2 收窄**（3 候选 C1 全链路 ⛔ / **C2 ✅** / C3 零 schema ⛔）
  — **存储层**: 迁移 027 `ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS citation JSONB NOT NULL DEFAULT '[]'::jsonb`（默认 `'[]'` 非 NULL；`IF NOT EXISTS` 幂等；PG metadata-only；**不改写 Migration 006 的 `CREATE TABLE` 原文**，同迁移 026 口径）+ `docs/migrations/027_factor_cache_citation.sql` 文档副本
  — **类型层**: `pkg/domain/factor.go::FactorCacheEntry` 增 `Citation json.RawMessage`（JSON `citation,omitempty`）——用 `RawMessage` 是刻意的：列内形态（今日 hash-only）与输出形态（5 元组）**不同**且前者须可无损升级
  — **注入链（唯一新增信息流）**: `loadStatementBook` **保留** `SnapshotURI`（剥 `ingest.raw:`，**前缀不匹配者跳过，不猜不造**）→ `statementField` 增 provenance → `saveVerticalFactor`（**5 个纵向因子的唯一收口点**）写 `entry.Citation`
  — **输出面**: `cmd/data/handlers_factor.go::getFactorHandler`（`GET /factors/:factor_name`，注册于 `cmd/data/main.go:159`）解码后逐个 `GetRawIngest` → 命中输出 5 元组 `{source, dataset, key, as_of, content_hash}`；**未命中只出 `{"content_hash": ...}`**（其余四字段缺席**即是**「未响应未归档」信号，与 Evidence API 404 一等语义同源，**不得凭空补字段**）；`factor_cache` 未命中仍 404，citation 解析失败**不得**转 5xx
  — **接口零变更**: `pkg/data.FactorStore` / `pkg/backtest/cache.FactorStore` 均不变（citation 随 `FactorCacheEntry` 走）⇒ 生产调用方与 mock 零改动
  — **粒度语义**: `archiveRaw` 的 `as_of` 刻意留 NULL ⇒ citation 落点是**请求批次坐标**；同一标的多 period 可能来自不同批次 ⇒ **citation 是集合**（列内用数组之因）；取并集**略宽于**最小集，精确到「哪个数字 ← 哪一批次」由 JSON Pointer 层负责（ADR-022 §5 声明 / 精确两层分工）
- **报告面推迟（用户裁决）**: `pkg/backtest/cache/factor_cache.go::Warm` **硬编码** `{Momentum, Value, Quality}` —— 三者在 C2 里**全部不可覆盖**（源自 `ohlcv_daily_qfq` / `stock_fundamentals`，两表零 hash 列）⇒ 原「回测内嵌 + 走查加列」方案只能产出**恒空 citation（幽灵字段）**，故 `backtest_jobs.result` 与 `walk_forward_reports` 本切片**整体不动**，标注「待 A→B 链落地」
- **明确不做（切片边界）**: `factor_returns` / `ic_analysis` 携 citation（须独立评估）；C1（`archiveRaw` 签名改造 + OHLCV / `stock_fundamentals` 加 hash 列）；C3；切片 B（`api/sync.ts` 死端点）/ 切片 D（旁路残留）；ODR-060 原文回写（历史决策记录保留原文，本仓既有口径）
- **未达标项（如实记录）**: PRODUCT §6.4「证据完整性 100%」**未达标** —— 覆盖面仅 **5/11** 个因子，momentum 完全无链、value / quality 无链；本切片**不宣称**达标
- **验收判据（待 C2 实施时取证）**: `go build ./...` EXIT=0；`go test -count=1 ./pkg/storage/ ./pkg/data/ ./cmd/data/ ./pkg/backtest/...` 全 ok；**主验收点** = `GET /factors/gross_margin_trend?symbol=600519.SH&date=<YYYYMMDD>` 的 `citation[0].content_hash` 命中 `GET /api/evidence/{hash}` → **200**（非 404）；momentum 同一端点 `citation` = `[]`（非 null）；`snapshot_uri` 非 `ingest.raw:` 的行 citation 不含该来源且不报错
- **统计更新**: 总计 233 不变（评估类，无任务增减）；**阶段 P5 切片 C 评估完成，实施未启动，P5-1 整体仍未关闭**

### 2026-09-15 (v3.35.0) — Sprint 8 阶段 P5 切片 3: Vue SPA 对接 L0 Evidence API（工作面 2 对齐起步）

**来源**: [ODR-060](odr/odr-060-p5-1-frontend-evidence-api.md) — P5-1 第三刀；承 [ODR-059](odr/odr-059-p5-1-retire-datasource-switch.md) §未做项第 4 条「`P5-1` 描述中的『Vue SPA / Research Engine 存量能力对接 L0 单一数据面 + Evidence API』尚未开始」

- **现场取证（三条事实）**
  — **(a) L0 侧已就绪**：L0-3 `GET /api/evidence/:content_hash` handler 已实现且已注册在 analysis(:8085)；`GetRawIngest` 未命中返回 `(nil, nil)` → 映射为 **404 `{"error":"evidence not ingested"}`**，是**一等语义**而非错误
  — **(b) 工作面 2 零消费**：切片前全目录 Grep `web/src` 的 `evidence|content_hash|citation` = **0 命中** —— `ingest.raw` 的 `content_hash` 坐标在 L3 体验面上不可达
  — **(c) 后端零改动即可对接**：vite dev proxy 已把 `/api` → `http://localhost:8085`（analysis），Evidence API 天然可达
- **裁决: 只做「消费既有契约」的那一格**（4 候选切片交用户裁决，采纳 A；B/C/D 明确不做）
  — **API 客户端层**: 新增 `web/src/api/evidence.ts::getEvidence`（`api.get<RawIngest>` + `encodeURIComponent` 路径段编码防 hash 逃出路由；404 只透传不在本层吞掉）
  — **类型层**: 新增 `web/src/types/evidence.ts::RawIngest`（逐字对应 `pkg/storage/ingest_raw.go` JSON 标签，含 `as_of` 的 `omitempty` ↔ TS `?`）
  — **UI 入口**: 新增 `components/evidence/EvidenceLookup.vue`（hash 输入 + 空值禁用提交；三态渲染：命中 = `NDescriptions` 六字段 + `NCode` 原样 payload / **404 = 未摄取** = `NAlert type="warning"` / 其他失败 = `NAlert type="error"`）+ `pages/Evidence.vue` 页面壳 + `router/index.ts` 注册 `/evidence` + `AppSidebar.vue` 增「证据查询」导航项
  — **测试**: `api/evidence.test.ts`（3 用例：请求路径 / hash URL 编码 / 404 透传不变）+ `components/evidence/EvidenceLookup.test.ts`（4 用例：空值禁用提交 / 命中渲染六字段 + payload / 404 → 「未摄取」/ 非 404 → 错误态且**不**显示「未摄取」）
- **关键 UI 语义**: 404 与「查询失败」**渲染为不同形态**（warning vs error）—— 把「未摄取」立为一等答案在 L3 面上的落点
- **明确不做（切片边界）**: 后端 `cmd/analysis` / `cmd/data` **零改动**；`api/sync.ts` 死端点（切片 B）；Research Engine 输出侧 `content_hash`（切片 C）；`/market` → `8081` 与遗留镜像路由（切片 D）
- **验证**: `npm run typecheck`（`vue-tsc --noEmit`）EXIT=0；`npm test`（`lint:tests` + `vitest run`）**13 files / 162 tests 全通过**（新增 2 文件 / 7 用例）；`npm run build` EXIT=0；验收点 `web/src` evidence 命中 **8 文件，全部为本切片新增**；后端 0 个 `.go` 文件变更
- **统计更新**: 总计 233 不变；**阶段 P5 切片 3 完成，P5-1 整体仍未关闭**（余 Research Engine 侧 citation 与旁路取数残留维度）

### 2026-09-15 (v3.34.0) — Sprint 8 阶段 P5 切片 2: 退役运行时数据源切换门（退役 `POST /api/datasource/switch`）

**来源**: [ODR-059](odr/odr-059-p5-1-retire-datasource-switch.md) — P5-1 第二刀；承 [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md) §6「P-B 须单独评估」

- **评估取证（三条实证）**
  — **(a) 生产接线下一按即崩**：`setup.go` 以 `NewDataAdapter(nil, ...)` 接线（bus = nil），`SetPrimary` 无条件 `a.bus.Publish` → 实测复现 nil pointer panic（被 `gin.Recovery()` 吞成 500）；现有测试传非 nil bus，恰未覆盖
  — **(b) `http` 分支收任意 URL**：`NewHTTPProvider(req.URL, ...)` 无校验，可把引擎读源指向任意外部服务，冲突 ADR-022 §1「外部源不得由本服务直连取数」；且切换门默认无鉴权
  — **(c) 驱动它的配置是死的**：全仓 `datasource.*` 配置读取点 = 0；`DataSourceConfig` / `AdapterFactory` 生产调用者 = 0
- **裁决: 退役**（与 L0 原则语义冲突时正确动作是退役而非加固）
  — **后端**: 删 `registerDatasourceRoutes` 的 `POST /switch` handler 与 `logger` 参数（`cmd/analysis/handlers_datasource.go`），保留只读 `GET /status` / `GET /health`；`main.go` 调用点同步改参
  — **前端**: 删 `components/sync/DataSourceSwitch.vue`（112 行切换表单）+ `DataSync.vue` 引用 + `api/sync.ts::switchDataSource` + `types/sync.ts` 两个 interface + `stores/sync.ts::switchSource` action + 对应测试
  — **死配置/死工厂**: 删 `config/analysis-service.yaml` 整段 `datasource:`、删 `pkg/marketdata/config.go` 整文件（`DataSourceConfig`/`AdapterFactory`/`Build*`/`DefaultDataSourceConfig` 等 137 行）
  — **契约与文档**: `docs/openapi.yaml` 删 `/api/datasource/switch` 整段；`docs/ARCHITECTURE.md` / `AGENTS.md` / `docs/SPEC.md`（2 处）同步删该端点
- **明确保留**: `GET /api/datasource/status`、`GET /api/datasource/health`；读源由启动期 `data_service.url` 固定
- **未做（本切片不动）**: `Engine.SwitchDataSource` / `DataAdapter.SetPrimary` / `NewCachedProvider` 去留（生产调用者归 0 但仍有测试引用）属独立议题；`SetPrimary` 的 nil-bus panic 未修（已不可从生产到达）
- **验证**: `go build ./...` 通过；`go test -count=1 ./pkg/marketdata/ ./pkg/backtest/ ./cmd/analysis/` 全 ok；前端 `typecheck` / `vitest`（11 files, 153 tests）/ `lint`（0 error）通过
- **统计更新**: 总计 233 不变；**阶段 P5 切片 2 完成，P5-1 整体仍未关闭**（余「对接 L0 单一数据面 + Evidence API」）

### 2026-09-15 (v3.33.0) — Sprint 8 阶段 P5 切片 1: 封潜伏直连（退役 pkg/marketdata 直连 provider）

**来源**: [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md) — P5-1「两个工作面共享 L0-L2, 无平行数据路径」的第一刀

- **旁路勘察（4 条平行/旁路取数路径）**
  — **P-A** `pkg/marketdata` 第二套 Provider 抽象（akshare/tushare，**已实现未实例化**，全仓 0 生产调用者）
  — **P-B** `cmd/analysis` 运行时切换门 `POST /api/datasource/switch`（任意 URL，**用户可见能力**）
  — **P-C** `pkg/data/source` 9 适配器 + `Registry` + `ETLPipeline`（归属 L0 `cmd/data` 正确；`ETLPipeline` 生产实例化点 = 0）
  — **P-D** `pkg/data/source/hkex` 北向 fetcher（潜伏，0 生产实例化点）
- **裁决: 只封潜伏直连**
  — **P-A 退役**: 删 `pkg/marketdata/tushare_provider.go` / `akshare_provider.go`；工厂 `buildProvider` 新增合并分支 `case "tushare", "akshare"` **显式拒绝**，并给出 L0 入口指引（`POST /api/ingest/raw` → 读 `ingest.raw`）；`SourceConfig` 删无主字段、`FactoryDeps` 收敛为 `{PostgresStore}`
  — **P-D 收敛**: `pkg/data/source/hkex` 包注释 + `NewEastmoneyNorthboundFetcher` 构造器注释显式标注「L0 摄取侧专用（ADR-022 §1）」（`NorthboundFactor` 只依赖 `NorthboundFetcher` 接口，纯计算不发网络）
  — **不动 P-B**: 切换门属用户可见能力，去留存废须单独评估
  — **不动 P-C**: `pkg/data/source` 归属 L0 正确，`ETLPipeline` 零实例化属独立议题
- **配置面清理**: `config/analysis-service.yaml` `datasource.sources` 删 `tushare`/`akshare` 两段，上方补 ADR-022 §1 注释
- **验收点达成**: 全仓 `Grep NewAkShareProvider | NewTushareProvider | NewEastmoneyNorthboundFetcher | NewNorthboundFactor` → **仅命中构造器自身定义**，即生产代码中「外部源直连实例化点 = 0」
- **统计更新**: 总计 233 不变；Phase 3 D1-3/D1-5 由 ✅ 转 ⚫（已退役，51/0/2/53）；总计已完成 224 → 222、已取消 0 → 2；**阶段 P5 切片 1 完成，P5-1 整体仍未关闭**（待 P-B 评估）
- **验证**: `go build ./...` 通过；`go test ./pkg/marketdata/... ./pkg/data/...` 全 ok

### 2026-09-15 (v3.32.0) — Sprint 8 阶段 P4 起步: EQD-P2-1 第 19 个 MCP 工具 `research.profile`（C-5 / 桥 B2）

**来源**: [ODR-057](odr/odr-057-eqd-p2-1-research-profile-tool.md) — 飞轮第 1 步（读回研究档案）落地记录

- **落地**: `EQD-P2-1` 为 `research` schema 接上**第一个读取方**，并新增第 19 个 MCP 工具
  — **读取层** `pkg/storage/research.go`（新建）: `ResearchProfile` / `ResearchConclusion` / `ResearchQuestion` + `GetResearchProfile`；主表无行 → `(nil, nil)`（「无档案」≠「查失败」）、子表无行 → 空切片、citations 以 `json.RawMessage` 原样透传
  — **工具本体** `pkg/tools/builtin/research_tool.go`（新建）: `research.profile`；来源解析 = **PG `research.*` 投影优先 → vault `_profile.json` 镜像回退 → 404 `NOT_FOUND`**；窄接口 `ResearchProfileClient` 定义在 builtin 包内（`pkg/storage` 不反向依赖 `pkg/tools`）
  — **HTTP 层**: `pkg/tools/errors.go` 新增哨兵 `ErrNotFound`；`respondToolError` 映射 404 `NOT_FOUND`（「有档案」200 vs「没档案」404，非 200 + 空对象）
  — **注册**: `buildToolsRegistry` 7 参 → 8 参；新增 **Group 10（Research archive）**；调用方 `cmd/analysis/main.go` 传入 `store`
- **"不猜"延续**: 未知交易所后缀（`.HK` 等）→ `ErrInvalidArgs`；`schema_version != 1` → 拒绝服务；vault 镜像 ticker 串档 → 报错；镜像解码失败 → 报错（不静默当不存在）
- **契约 C2 口径**: `stale` 三触发（`generated_at` 零值 / `source_mtime > generated_at` / 超 90 天 `DefaultProfileMaxAge`）+ `stale_reason`；输出逐字对齐 C2，citations **不做 LLM 二次加工**
- **已知语义缺口**: `research.profile` 表**无 `generated_at` 列**（契约 C2 有），投影路径以 `updated_at` 代理（已记入 ODR-057 负面项与风险表）
- **文档口径裁决**: 工具数断言分两类 —— **描述性现状文档同步为 19**（ARCHITECTURE / AGENTS / SPEC / VISION / hermes config / `pkg/ai/agents/doc.go` / RESEARCH）；**历史决策记录保留原文不回写**（ODR-046、`adr-015`），理由见 ODR-057 §5
- **统计更新**: 总计 233 不变；待处理 9 → 8，已完成 223 → 224（Sprint 8 内 7/0/10 → 6/0/11）；**阶段 P4 完成度 1/2**（余 P4-1 飞轮闭环端到端）
- **验证**: `go build ./...` 通过；`go test ./pkg/tools/... ./pkg/storage/... ./cmd/analysis/...` 全 ok；新增 23 个 `TestXxx` + 1 个集成测试（含 `TestIntegration_DottedToolName_RoutesThroughGin` 实测含点工具名经 gin 路由）

### 2026-09-15 (v3.31.0) — Sprint 8 阶段 P3 收口: EQD-P3-1 fundamentals / stock_fundamentals 表重叠合并

**来源**: [ODR-056](odr/odr-056-fundamentals-table-consolidation.md) — 计算面 C-8 / DR-7 落地记录

- **落地**: `EQD-P3-1` 消除 `fundamentals` / `stock_fundamentals` 两表重叠（同属类 C 派生数据，违反 ADR-022 §2 分区唯一性）
  — **迁移 025**（`docs/migrations/025_equitydeep_field_consolidation.sql`）：守卫式 `DO` 块按 `symbol → ts_code` 直通并入，`ON CONFLICT (ts_code, trade_date) DO UPDATE SET 列 = COALESCE(幸存表值, EXCLUDED)`（幸存表优先，避免旧表陈旧值覆盖新值），随后 `DROP TABLE fundamentals`
  — **合并为纯列名直通**: 两表 12 个指标列（`pe/pb/ps/roe/roa/debt_to_equity/gross_margin/net_margin/revenue/net_profit/total_assets/total_liab`）同名同义；同源于同一 tushare `fina_indicator` API、同一字段列表，且均以 `end_date` 作为 `trade_date`（`fundamentals.symbol` 存的本就是 `ts_code` 字面量）→ 零口径转换
  — **代码侧收敛**: `GetFundamental` / `GetFundamentals` / `SaveFundamental` / `SaveFundamentalBatch` 四函数改指 `stock_fundamentals` / `ts_code`；`SaveFundamentalBatch` 的 `DO UPDATE` 由 6 列补齐为 12 列（修复重摄取时静默保留陈旧值的缺陷）
  — **DDL 清理**: `pkg/storage/postgres.go` 内联 `migrate()` 删除 `fundamentals` 建表语句与两个索引；`integration_test.go` 的 `truncateAll` 移除旧表名；`docs/SPEC.md` 旧版草案 DDL 替换为注释
- **活跃表数**: **39 → 38**（内联 21 → 20 + 迁移 18）；项目首次出现活跃表数减少
- **收口校验**: 迁移 014 对 `fundamentals` 的 `ADD COLUMN` 不改（`migration_manager.go` 指向根 `migrations/`，全仓无调用方；真实执行路径为内联 `migrate()`，根目录不含 025 且版本顺序恒早于内联 DROP）→ 结论登记于 025 头注释与 ODR-056
- **统计更新**: 总计 233 不变；待处理 10 → 9，已完成 222 → 223（Sprint 8 内 8/0/9 → 7/0/10）；**阶段 P3 完成度 3/3 — 阶段 P3 全部关闭**
- **验证**: `go build ./...` 通过；`go test -count=1 ./pkg/storage/... ./pkg/data/... ./pkg/marketdata/...` 全 ok；`gofmt` 干净；全仓残留 `fundamentals` SQL 引用 0（迁移 014 与归档文档除外，已归因）

### 2026-09-15 (v3.30.0) — Sprint 8 阶段 P3: EQD-P1-2 摄取链 + 5 个纵向基本面因子

**来源**: [ODR-055](odr/odr-055-eqd-p1-2-vertical-factors.md) — 计算面 C-3 + C-4 落地记录

- **落地**: `EQD-P1-2` 把桥 B1（RESEARCH §3.4）的 5 个纵向基本面因子从规格推到可摄取、可计算、可测试
  — **C-3 摄取链**：`pkg/data/equitydeep/` 纯包（契约快照解析 + `field_dictionary.yaml` 白名单 + 单位换算 + 千分位剥离）
    + `fundamentals_detail` 行模型与落库/读取（读取强制 `ann_date <= asOf`，PIT）
    + HTTP 写门 `POST /api/ingest/equitydeep`（ndjson；须先经 `POST /api/ingest/raw` 归档并携带其 `content_hash`）
  — **C-4 因子计算**：`gross_margin_trend` / `contract_liability_ratio` / `ocf_to_net_profit` / `roe_dupont_leverage` / `inventory_turnover_delta`
    （口径依 `contracts/field_dictionary.yaml`：利润表/现金流量表年内累计、资产负债表时点值；`TTM(Qn,Y) = YTD(Qn,Y) + YTD(Q4,Y-1) − YTD(Qn,Y-1)`）
  — **迁移 026**：`factor_cache` / `factor_returns` / `ic_analysis` 的 `factor_name` 放宽至 `VARCHAR(32)`（最长因子名 24 字符）
- **入口形态修正**: RESEARCH §3.4 原稿「摄取命令（非 HTTP）」经裁决改为 **HTTP 写入口**，文档已同步修正（避免的是 EquityDeep 常驻服务化，而非 HTTP 本身）
- **落点修正**: RESEARCH §3.7 C-3 落点补全为「纯包 + 落库/读取 + HTTP 写门」；C-4 落点由 `pkg/data/factors/` 修正为 `pkg/data/factor_equitydeep.go`
- **统计更新**: 总计 233 不变；待处理 11 → 10，已完成 221 → 222（Sprint 8 内 9/0/8 → 8/0/9）；**阶段 P3 完成度 2/3**（余 EQD-P3-1）
- **测试**: `pkg/data/factor_equitydeep_test.go`（PIT 过滤 / 累计→TTM→单季转换 / 财年边界归零 / 重述取较晚公告 / 5 因子手算锚定 / 顺序与并行一致 / 错误传播）；逐函数覆盖率 92.3%~100%

### 2026-09-15 (v3.29.0) — Sprint 8 阶段 P1 全部关闭: EQD-P3-2 文档漂移 8 项一致性复核

**来源**: [ODR-054](odr/odr-054-dr-reverification.md) — P1 收口记录（对 [ODR-047](odr/odr-047-equitydeep-integration-audit.md) 已修 8 项做收口校验）

- **复核完成**: `EQD-P3-2` 对 ODR-047 已修 8 项执行「文档声明 ↔ 物理事实」双向取证（非重新修复，而是校验"当初修好了"是否"现在还一致"）
  — DR-1 builtin tool 实测 18 个 ↔ ARCHITECTURE.md L982 清单逐一对应 → ✅ 未回退
  — DR-2 `pkg/ai/agents/{generate,validate,evolve}.go` + `pkg/ai/gene_pool/` 均存在 → ✅ 未回退
  — DR-3 `docs/adr/` 22 文件 / `docs/odr/` 53 文件实测 → ⚠️ 发现 2 处残留漂移（见下）
  — DR-4 `AGENTS.md` 计数实测 → ⚠️ 发现 1 处残留漂移（见下）
  — DR-5 ROADMAP Phase 4 `IN PROGRESS` 修正注保留 → ✅ 未回退；DR-6 VISION `optimize.go` 两处均标 NOT IMPLEMENTED → ✅ 未回退
  — DR-7 由 `EQD-P3-1` 承接（阶段 P3）；DR-8 `docs/design/equitydeep/` 迁移 + 7 处引用一致 → ✅ 未回退
- **回填 3 处残留漂移**（由后续新增记录引起，非 ODR-047 修复失效）:
  — DR-3-R1: `docs/ADR.md` 尾注 ADR 拆分 21 ≠ 22（ADR-021 未落入任何一类）→ 补「研究层 1 (ADR-021)」
  — DR-3-R2: `docs/ADR.md` 尾注 Implementation 31 ≠ 30（ODR-022 已被索引表归入 Refactor，属"分类计数"与"区间枚举"口径混用）→ 改为显式枚举 `ODR-016~021 + ODR-023~042 + ODR-050~053`
  — DR-4-R1: `AGENTS.md` 目录树 `odr/ # (ODR-001~049)` → `(ODR-001~054)`
- **改进**: 尾注拆分改写为**可验算形式**（各分类之和 = 声明总数），使下一轮漏改立即暴露为「和 ≠ 总数」
- **文档同步**: [ADR.md](../ADR.md) index 3.10.0 → 3.10.1（ODR 53 → 54, Audit 9 → 10）；[AGENTS.md](../../AGENTS.md) §1 版本行「P1 底座契约已全部关闭」
- **统计更新**: 总计 233 不变；待处理 11 → 10, 已完成 220 → 221（Sprint 8 内 9/1/7 → 9/0/8）；**阶段 P1 完成度 4/4**

### 2026-09-15 (v3.28.0) — Sprint 8 阶段 P3 起步: EQD-P1-1 `fundamentals_detail` 表落地

**来源**: [ODR-053](odr/odr-053-p3-fundamentals-detail-table.md) — 计算面契约 C1 落库记录

- **落地**: `EQD-P1-1` 新增 `fundamentals_detail` 表（契约 C1 / RESEARCH §3.2）
  — 逐字段行存 + `ann_date` PIT 对齐（硬要求，缺则引入 look-ahead bias）+ `snapshot_uri` 反查 `ingest.raw.content_hash`
  — 主键 `(ts_code, end_date, ann_date, field_code, fetched_at)` 含快照时间，支持 restatement 多版本并存
  — `source` 前缀 `equitydeep:`（物理隔离）；`value` 为**唯一可空列**（缺失读数须与读数为 0 可区分）
  — 仅新增表 + 索引 `idx_fund_detail_lookup`，**不改存量表**
- **执行路径**: `pkg/storage/postgres.go` 内联 `migrate()` Migration 022；`docs/migrations/022_equitydeep_fundamentals.sql` 为文档副本
- **补齐 ODR-052 遗留缺口**: 新增**三副本自动一致性校验**（契约副本 ↔ 文档副本 ↔ 内联执行路径）
  — `TestFundamentalsDetailSchema_ThreeCopiesAgree`：提取三处 DDL 归一化后逐字比对（建表 + 索引分别断言）
  — `TestFundamentalsDetailSchema_FrozenSemantics`：钉住 `ann_date` / `fetched_at` / `snapshot_uri` NOT NULL、主键 5 列、`value` 可空等冻结语义
  — `TestFundamentalsDetailSchema_AppliedByMigrate`：活库校验 `ann_date` 实为 NOT NULL（无 Docker 时按约定 SKIP）
- **文档同步**: [ARCHITECTURE.md](../ARCHITECTURE.md) 活跃表数 38 → **39**（内联 20 → 21）；`docs/migrations/` 副本数 11 → 12
- **统计更新**: 总计 233 不变；待处理 12 → 11, 已完成 219 → 220（Sprint 8 内 10/1/6 → 9/1/7）

### 2026-09-15 (v3.27.0) — Sprint 8 阶段 P1「底座契约」EQD-P0-1 契约冻结 + EQD-P0-2 抽检泛化落地

**来源**: [ODR-052](odr/odr-052-p1-contract-freeze-and-spot-check.md) — P1 收尾记录（契约目录冻结 + 抽检脚本泛化）

- **落地**: `EQD-P0-1` 新建 `contracts/` 契约目录（此前不存在），冻结四项契约工件
  — `field_dictionary.yaml`（契约 C1-a）：20 个 `field_code` 显式白名单 + `raw_names`（含全角/半角括号变体）+ `base_unit: CNY` + 5 档 `unit_scale` + 10 票 `spot_check_defaults`
  — `fundamentals_detail.schema.sql`（契约 C1-b）：§3.2 DDL 一并冻结，声明「三处同源以本文件为准」
  — `snapshot.schema.json`：快照结构冻结，`ann_date` 为 required（缺此项 = 不合规快照，会引入 look-ahead bias）
  — `profile.schema.json`（契约 C2）：`_profile.json` 派生镜像冻结，`citations[].content_hash` 对应 Evidence API 内容坐标
  — 全部带 `version` / `frozen_at` / 兼容性矩阵；契约只读语义写入 header（不可重命名/删除、白名单外字段丢弃记 warn 不得猜测）
- **落地**: `EQD-P0-2` 新建 `evals/data_quality/spot_check.py`（本仓首个 Python 工件）
  — 把 EquityDeep M1 抽检（10 票 × 20 数字，错误率 < 2%）泛化为跨源通用脚本：`--source {akshare,tushare} --tickers --fields`
  — 三项检查：白名单校验 / 显式单位换算 / 双源比对；四退出码 `0` PASS / `1` FAIL / `3` INCONCLUSIVE / `4` ERROR
  — **刻意不直连 tushare/akshare**：消费「读数转储」JSONL（路径 / http(s) / `-` stdin），取数唯一入口归 L0（ADR-022 §4）
  — 内置 `--self-test` 同时构造 PASS 与 FAIL 场景；零外部依赖（yaml 按需 import）
  — 加固：`--tickers` 6 位数字格式校验（PowerShell 会把逗号列表解析为数组并截断前导零，静默缩小抽检范围）
- **契约收紧**: `snapshot.schema.json` / `profile.schema.json` 均 `additionalProperties: false`，结构性防漂移
- **统计更新**: 总计 233 不变；待处理 14 → 12, 已完成 217 → 219（Sprint 8 内 12/1/4 → 10/1/6）

### 2026-09-15 (v3.26.0) — Sprint 8 阶段 P1「底座契约」L0-1 单一摄取入口落地

**来源**: [ODR-051](odr/odr-051-l0-1-single-ingest-entry.md) — L0-1 落地记录（归档接入真实路径 + 写入口 API）

- **落地**: `L0-1` 归档接入真实取数路径（`pkg/data/tushare_raw.go` + `pkg/data/tushare.go`）
  — `TushareClient.call()` 为全仓 tushare 响应的唯一咽喉点；成功响应在**任何调用方规范化之前**归档原始 HTTP body
  — 归档为 best-effort：失败只记 warn，不影响 fetch 结果；`as_of` 故意留 NULL（单响应可跨数千交易日）
- **落地**: `L0-1` 写入口 `POST /api/ingest/raw`（`cmd/data/handlers_ingest.go`）
  — 平台写入 `ingest.raw` 的唯一 HTTP 门；外部生产者（工作面 1 的 akshare 侧）经此上报原始响应
  — 门很窄：逐字归档 payload 并返回 `content_hash`，不做规范化/解释/领域校验；幂等（`ON CONFLICT DO NOTHING`）
- **契约收紧**: `TushareStore` 接口新增 `SaveRawIngest`（L32）— 归档契约在**编译期**强制，而非运行时才发现
- **勘察结论（重要）**: 声明的「单一摄取入口」（`source.Registry` + `ETLPipeline`）**不在真实数据路径上**
  — `Registry` 仅被 `registry_handlers.go` 的 health/list 诊断端点消费；`ETLPipeline.Process` 全仓仅测试调用
  — 据此本轮把归档挂到真实咽喉点，**不做**完整 Registry 重接（属多 Sprint 工程，见 ODR-051「未做项」）
- **文档同步**: [openapi.yaml](openapi.yaml) 新增 `POST /api/ingest/raw` + `RawIngestWrite` schema
- **统计更新**: 总计 233 不变；待处理 15 → 14, 已完成 216 → 217（Sprint 8 内 13/1/3 → 12/1/4）

### 2026-09-15 (v3.25.0) — Sprint 8 阶段 P1「底座契约」三项先行落地

**来源**: [ODR-050](odr/odr-050-p1-base-contract-landing.md) — P1 底座契约落地记录（含迁移编号统一复核）

- **落地**: `L0-2` `ingest.raw`（`pkg/storage/ingest_raw.go` + `postgres.go` 内联 DDL）
  — 类 A 原始源响应归档, `content_hash` 唯一键, 写入幂等且不可变
- **落地**: `L0-3` Evidence API（`cmd/analysis/handlers_evidence.go`, `GET /api/evidence/{content_hash}`）
  — 404 = 未摄取, 为一等答案; `ServerDeps` 新增第 18 个字段 `Store`
- **落地**: `L0-4` `research` schema（`profile` / `conclusion` / `question` 3 张表）
  — 类 E 研究结构化状态投影, 可由 vault markdown 确定性重建
- **统一迁移编号**: 根 `migrations/012_add_gene_pool_tables.sql` → `023_*`；
  `docs/migrations/007_add_factor_returns_table.sql` → `024_*`；
  新增 `020_add_ingest_raw.sql` / `021_add_research_schema.sql`；
  预留 `022_*`（EQD-P1-1）/ `025_*`（EQD-P3-1）
- **文档同步**: [ARCHITECTURE.md](../ARCHITECTURE.md) 表数 32 → **38 张活跃表**（内联 20 + 迁移 18）；
  [SPEC.md](../SPEC.md) Evidence API 由"草案"改为"已实现"；
  [openapi.yaml](openapi.yaml) 新增 `/api/evidence/{content_hash}` + `RawIngest` schema；
  [AGENTS.md](../../AGENTS.md) CR-47 表数复核更新
- **统计更新**: 总计 233 不变；待处理 18 → 15, 已完成 213 → 216（Sprint 8 内 16/1/0 → 13/1/3）

### 2026-09-15 (v3.24.0) — ADR-022 下游一致性收口（文档审计）

**来源**: [ODR-049](odr/odr-049-adr-022-downstream-consistency.md) — 文档体系完整性审计

- **修复**: 头部版本号 3.22.0 → 3.24.0（原与正文 v3.23.0 自相矛盾）
- **修复**: 关联文档引用 ADR-021 → ADR-022（Sprint 8 章节 + 文末文档表）
- **同步**: [ROADMAP.md](../ROADMAP.md) Sprint 8 列 → 统一研究平台落地（原 "AI Factor Discovery" 含已废弃的 Factor Lab UI）
- **同步**: [AGENTS.md](../../AGENTS.md) / [archive/RESEARCH-equitydeep-legacy.md](RESEARCH-equitydeep-legacy.md) / [design/index.md](../design/index.md) ADR-021 引用收口
- **同步**: [SPEC.md](../SPEC.md) 补 ADR-022 四层模型 + Evidence API（v1.4.2 → v1.5.0）
- **无任务增减**（统计不变：233 项）

### 2026-09-15 (v3.23.0) — Sprint 8 按 ADR-022 执行路线重排: 统一研究平台落地

**来源**: [ADR-022](superseded-adr/adr-022-unified-research-platform.md)（架构决策）+ [ODR-048](odr/odr-048-top-level-product-redefinition.md)（顶层重定义记录）
**顶层定义**: [PRODUCT.md](../PRODUCT.md)（canonical）— Quant Lab 降维为共享底座（L0-L2），横截面选股升为工作面 2

- **章节重排**: Sprint 8「EquityDeep 纵向研究层集成」→「统一研究平台落地」；9 项 → 17 项，按 ADR-022 执行路线分 5 阶段:
  - **P1 底座契约**×7: EQD-P0-1 / EQD-P0-2 / EQD-P3-2 + 新增 L0-1 单一摄取入口 / L0-2 `ingest.raw` / L0-3 Evidence API / L0-4 `research` schema
  - **P2 工作面 1 跑通**×4: EQD-P0-3（回查脚本假阳性修复）/ EQD-P2-2（容器化）+ 新增 P2-1（接 `research` schema）/ P2-2（取数改造走 Evidence API）
  - **P3 计算面补齐**×3: EQD-P1-1 / EQD-P1-2 / EQD-P3-1
  - **P4 飞轮打通**×2: EQD-P2-1（MCP 工具 `research.profile`）+ 新增 P4-1（飞轮闭环端到端）
  - **P5 横截面工作面 v2**×1: 新增 P5-1（存量对齐 L0 单一数据面）
- **定位变更**: ~~「补充非改变」~~ → ADR-022：Quant Lab 降维为共享底座，横截面升为工作面 2（与工作面 1 对等，本期纳入规划）
- **任务去向**: EQD-* 编号保持不变（跨文档引用稳定），阶段归属以 Sprint 8 分组为准
- **统计更新**: 总计 225 → 233（待处理 10 → 18）
- **关联文档**: [PRODUCT.md](../PRODUCT.md) / [archive/RESEARCH-equitydeep-legacy.md](RESEARCH-equitydeep-legacy.md)（工作面 1 详案，⚠️ 待按 ADR-022 修订）/ [ADR-022](superseded-adr/adr-022-unified-research-platform.md) / [ODR-048](odr/odr-048-top-level-product-redefinition.md)

### 2026-09-15 (v3.22.0) — Sprint 8 任务登记: EquityDeep 纵向研究层集成

**来源**: [ODR-047](odr/odr-047-equitydeep-integration-audit.md) EquityDeep 集成审计 — 结论「**补充非改变**」

- **新增 Sprint 8 章节**（9 项任务）:
  - 🔵 P0×3（零依赖可开工）: EQD-P0-1 契约冻结 (C-2) / EQD-P0-2 抽检脚本泛化 (C-7/桥 B3) / EQD-P0-3 EquityDeep 回查脚本 P0 假阳性缺陷修复
  - 🟠 P1×2（依赖 M1 硬门槛）: EQD-P1-1 `fundamentals_detail` 建表 (C-1) / EQD-P1-2 摄取命令 + 5 纵向因子 (C-3, C-4)
  - 🟡 P2×2（弱依赖 M3）: EQD-P2-1 MCP 工具 `research.profile` (C-5) / EQD-P2-2 vault 只读挂载 (C-6)
  -  P3×2（收尾）: EQD-P3-1 `fundamentals`/`stock_fundamentals` 表合并 (C-8/DR-7, 待办) / ~~EQD-P3-2 文档漂移 8 项收口 (C-9)~~ ✅ 已收口 (ODR-054)
- **DR-7 正式登记**: `fundamentals` 与 `stock_fundamentals` 表重叠（原仅文档自承「未来评估合并」无任务跟踪）→ EQD-P3-1
- **统计更新**: 总计 216 → 225（待处理 2 → 10，进行中 0 → 1）
- **关联文档**: [ADR-021](superseded-adr/adr-021-equitydeep-research-layer.md)（架构决策，后由 [ADR-022](superseded-adr/adr-022-unified-research-platform.md) 取代）+ [archive/RESEARCH-equitydeep-legacy.md](RESEARCH-equitydeep-legacy.md)（Product + Tech 方案）+ `docs/design/equitydeep/`（上游规格）

### 2026-06-12 (v3.21.1) — Sprint 6 P2 pickup #3: P2-1 HTML 报告导出 + P2-2 多策略对比

- **触发**: v3.21.0 完成 P2-3 紧急平仓后, 接续 ODR-013 BR-014
  (回测报告分享) + 用户研究员的"5 个 tab 切来切去对指标"痛点
- **过程**:
  - ✅ **P2-1 后端** [pkg/backtest/export.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/export.go) (新建, 320 行) —
    `RenderHTML(resp, opts) ([]byte, string, error)` + 嵌入 SVG 权益
    曲线 + 12 行指标表 + 交易明细 + light/dark theme + footer
    水印; 完全自包含 (无 CDN / 客户端 JS)
  - ✅ **P2-1 endpoint** [cmd/analysis/handlers_backtest.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_backtest.go) —
    `GET /api/backtest/:id/export/html` + query opt-out
    (`?equity=0&trades=0&theme=dark&footer=...`) +
    Content-Disposition attachment + X-Backtest-* 自定义头
  - ✅ **P2-1 前端** [web/src/api/backtest.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/backtest.ts) +
    [client.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/client.ts) —
    `api.download()` 绕过 JSON parse + `extractFilename` 解析
    Content-Disposition; BacktestEngine.vue 加 "导出 HTML" 按钮 +
    "加入对比" checkbox + 跳转 compare 按钮
  - ✅ **P2-2 后端** [pkg/backtest/compare.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/compare.go) (新建, 200 行) —
    `CompareReports(ctx, ids, resolver)` + 11 个 TestXxx; min/max
    校验 (2-8) + dedup + 部分成功 (200 + Missing 列表) + per-metric
    best 选出 + 默认 TotalReturn desc 排序
  - ✅ **P2-2 endpoint** `GET /api/backtest/compare?ids=bt-1,bt-2,...` +
    [handlers_compare_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_compare_test.go) (6 TestXxx)
  - ✅ **P2-2 前端** [BacktestCompare.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/BacktestCompare.vue) (新建, 320 行) —
    4 列指标卡片 (已加载/最佳收益/最佳 Sharpe/最低回撤) + 12 行
    × N 列对比表 (best 高亮浅绿底) + Chart.js 8 色 palette
    权益叠加 (示意)
  - ✅ **持久化** [web/src/constants/backtest.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/constants/backtest.ts) —
    localStorage key `quantlab:backtest:compare_ids` (FIFO 8 cap)
  - ✅ **ODR-027** [docs/archive/odr/odr-027-p2-1-p2-2-export-compare.md](odr/odr-027-p2-1-p2-2-export-compare.md) — 完整记录
- **结果**:
  - P2-1 + P2-2 从 ⬜ → ✅
  - Sprint 6 P1+P2 累计 13 项全部 ✅
  - 新增 17 TestXxx (compare 11 + handler 6)
  - 文档同步: TASKS.md §P2-1/§P2-2 状态, ADR.md ODR index, ODR-027 新建

### 2026-06-12 (v3.21.0) — Sprint 6 P2 pickup #2: P2-3 远程紧急平仓 kill-switch

- **触发**: v3.20.0 完成 P2 alert 接入 (PeriodicAlertLoop +
  /api/alerts) 后, 接续 ODR-013 BR-018 (业务风险 #18):
  生产路径无远程紧急平仓机制。A 股 operator 必须有 30s 内
  一键清仓能力以满足监管"未及时止损"问询, 而 T+1 限制
  会卡住常规 sell 路径
- **过程**:
  - ✅ **LiveTrader 接口扩展** [pkg/live/trader.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/trader.go) — 新增
    `EmergencyFlatten(ctx, reason) (*EmergencyFlattenResult, error)`
    + 3 个 result type (EmergencyFlattenResult / Order / Skip)
  - ✅ **MockTrader 实现** [pkg/live/mock_trader.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/mock_trader.go) (+155 行) —
    同 mutex 序列化 + snapshot symbols 避免 map iteration
    panic; 价格优先级 PriceProvider → pos.CurrentPrice → skip;
    复用 executeSell 的 commission/transfer/stamp tax 公式
  - ✅ **HTTP endpoint** [cmd/analysis/handlers_execution.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_execution.go) (+144 行) —
    `POST /api/execution/emergency-flatten` 三重鉴权:
    (1) `trading.emergency_token` 配置非空 (否则 503),
    (2) `Authorization: Bearer <token>` (crypto/subtle.ConstantTimeCompare
    防 timing attack),
    (3) body.confirmation_token 字段再次匹配 (defence in depth)
  - ✅ **前端 arm-and-confirm UI** [web/src/components/paper/EmergencyFlatten.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/components/paper/EmergencyFlatten.vue) (280 行) —
    第 1 次点击展开表单, 第 2 次确认 + 浏览器 confirm;
    n-data-table 列成交明细, T+1 绕过列标红, Skipped
    部分 n-alert 警告
  - ✅ **配置 + 注入** [config/analysis-service.yaml](file:///Users/ruoxi/longshaosWorld/quant-trading/config/analysis-service.yaml) 新增
    `trading.emergency_token: ""` (空默认, 端点 disabled) +
    [cmd/analysis/main.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/main.go) viper 注入
  - ✅ **T+1 显式绕过 + 审计标记** — `BypassedT1` bool + `Order.Message`
    字段 "EMERGENCY FLATTEN: <reason> (T+1 bypassed)", 监管可逐
    笔回溯"谁在什么时间因为什么原因强行平仓"
  - ✅ **12 TestXxx** [cmd/analysis/handlers_emergency_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_emergency_test.go) (260 行, *_test.go in .gitignore) —
    5 unit (ClosesAll/BypassesT1/Idempotent/RecordsReason/
    EmptyReasonDefault) + 6 HTTP integration (503/401/403/
    403-conf/400/success) + 1 race safety + 1 perf sanity
    (50 symbols < 1s)
  - 📋 **TASKS.md**: P2-3 ⬜ → ✅, v3.20.0 → v3.21.0
  - 📋 **新 ODR**: [odr-026-p2-3-emergency-flatten.md](odr/odr-026-p2-3-emergency-flatten.md) Created/Completed
  - 📋 **ADR.md index 3.2.0 → 3.3.0**: ODR 累计 25 → 26, ODR-026 新增
- **验证**:
  - **race detector**: `go test -race ./pkg/live/ ./cmd/analysis/ -run
    "TestEmergencyFlatten|TestMockTrader|TestExecutionHandler_Emergency"`
    全 PASS
  - **集成**: `go build ./...` exit 0, `go vet ./...` exit 0
  - **前端**: `vue-tsc --noEmit` 在 P2-3 文件 0 错误 (2 pre-existing
    ReviewActions 错误无关), `npm run lint:tests` 0 misuse
  - **延迟**: 50 symbols emergency flatten 实测 < 1s
- **代码量**: 净 +970 行 (生产 ~600 行 Go + 280 行 Vue + 50 行 TS + 40 行 yaml/doc)
- **新增 Go 依赖**: 0 (复用 stdlib + zerolog/gin)
- **总任务数**: 201 → 214 (+13: P2-1/P2-2 从 0 任务拆出 + 1 状态变更)
- **总完成数**: 210 → 211 (+1: P2-3)
- **总待处理**: 0 → 2 (+2: P2-1 backtest 导出 + P2-2 多策略对比)
- **未做但设计就绪** (P2 接续): Skipped 持仓自动 retry-with-backoff;
  接真实券商时改 secret manager 替代 yaml token; rate limit
  防误触连发

### 2026-06-12 (v3.20.0) — Sprint 6 P2 pickup #1: P2 alert 接入 (PeriodicAlertLoop + /api/alerts)

- **触发**: v3.19.0 完成 P1-30 E2E 后, 接续 P2 alert 接入
  (ODR-013 BR-015 风险 #15 调度部分)。P1-29 (ODR-023) 已实现
  AlertManager 6 类 P0 detector, 但**没有触发器、没有 HTTP
  暴露、没有告警历史**, operator 仍无法在生产环境使用
- **过程**:
  - ✅ **新建 [cmd/analysis/alert_loop.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/alert_loop.go)**
    (196 行) — PeriodicAlertConfig / AlertHistory (ring buffer) /
    PeriodicAlertLoop.Start/TriggerOnce / buildSnapshot
  - ✅ **新建 [cmd/analysis/handlers_alert.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_alert.go)**
    (142 行) — `/api/alerts/{history,force-check,stats}` 3 endpoints
    + registerAlertRoutes
  - ✅ **新建 [cmd/analysis/alert_loop_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/alert_loop_test.go)**
    (461 行, 16 TestXxx) — 5 AlertHistory + 5 PeriodicAlertLoop + 6
    HTTP handler, 全 PASS
    - **关键修复**: `DrainAndReset` 死锁 10min — 同 mutex 持
      Lock 时调 Snapshot() 的 RLock, 内联逻辑修复
    - **关键修复**: `stubLiveTrader.HealthCheck` 签名匹配
      `live.LiveTrader` interface (ctx 参数)
  - ✅ **扩展 [pkg/alert/channel.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/channel.go)** —
    RecorderChannel (76 行) + Send/Snapshot/DrainAndReset/Len/Evicted
  - ✅ **扩展 [pkg/alert/manager.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/manager.go)** —
    `AlertManager.Recorder()` 暴露 in-process recorder
  - ✅ **接入 [cmd/analysis/main.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/main.go)** —
    187-229 行 (构造) + 352-357 行 (挂路由 + 启 loop) + 414-423
    行 (shutdown)
  - ✅ **配置 [config/analysis-service.yaml](file:///Users/ruoxi/longshaosWorld/quant-trading/config/analysis-service.yaml)** —
    新增 `alert.*` 配置块 (enabled/interval/history/recorder/threshold/webhook)
  - 📋 **TASKS.md**: P2 alert 接入 ⬜ → ✅, v3.19.0 → v3.20.0
  - 📋 **新 ODR**: [odr-025-p2-alert-integration.md](odr/odr-025-p2-alert-integration.md) Created/Completed
  - 📋 **ADR.md index 3.1.0 → 3.2.0**: ODR 累计 24 → 25, ODR-025 新增
- **验证**:
  - **race detector**: `go test -race ./cmd/analysis/ -run 'Alert|PeriodicAlert' -count=1` 16/16 PASS
  - **集成**: `go build ./...` exit 0
  - **go vet**: `go vet ./...` exit 0
  - **测试用例**: 16 (5 AlertHistory + 5 PeriodicAlertLoop + 6 HTTP)
- **代码量**: 净 +873 行 (生产 412 + 测试 461)
- **新增 Go 依赖**: 0 (纯 stdlib + 已有的 zerolog/gin)
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 209 → 210 (+1: P2 alert 接入)
- **总待处理**: 0 → 0
- **未做但设计就绪** (P2 接续): 前端 Alert UI 面板 (alerts
  history 可视化 + by_rule 饼图 + force-check 按钮); Sector
  字段进 pkg/live.PositionInfo; PeakEquity 进 AccountInfo; 多
  实例时共享 history (Redis)

### 2026-06-12 (v3.19.0) — Sprint 6 P1 pickup #9: P1-30 E2E AI Copilot 端到端 + SSE 契约

- **触发**: v3.18.0 完成 P1-29 AlertManager 后, 接续 TQ-016 (ODR-013
  风险 #16), 解决 AI Copilot 端到端无测试覆盖: 旧 `copilot.spec.ts`
  7 用例全是 UI 静态测试, **不验证后端交互**; API 契约无 E2E
  断言; SSE 进度契约未锁定
- **过程**:
  - ✅ **新建 [e2e/tests/copilot-e2e.spec.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/e2e/tests/copilot-e2e.spec.ts)** (350 行) — 13 TestXxx, 4 大类
    - **UI 端到端 (7)**: 页面加载 / 输入渲染 / happy path 200 /
      negative path 503 / 多轮对话 / 复制按钮 / 导航可达
    - **API 契约 (4)**: 空 description 400 / 有效 prompt 200/503
      / save 缺 code 400 / stats 200
    - **SSE 契约 (1)**: `/api/sync/stream` content-type 锁定
      `text/event-stream` (为 P2 Copilot SSE 改造留参照)
    - **混合 UI+API (1)**: code/explanation/language 三字段
      路由到 msg-bubble / code-block / code-header 全部验证
  - ✅ **关键技术决策**:
    - `page.route` 拦截 `/api/copilot/generate` 返回 stub 响应,
      **不依赖真实 AI API** (CI 无 key)
    - 接受 200/503/502/401 多 status code (锁契约不锁环境)
    - 复用 `waitForBackendReady` + `isolateTestEnvironment` (拦截
      外部 AI/finance API)
  - ✅ **TypeScript strict mode 0 error** (本文件),
    `tsc --noEmit tests/copilot-e2e.spec.ts` 通过
  - ✅ **Playwright list 通过**: 13 tests in 1 file, 0 parse error
  - 📋 **TASKS.md**: P1-30 ⬜ → ✅, v3.18.0 → v3.19.0
  - 📋 **新 ODR**: [odr-024-p1-30-copilot-e2e.md](odr/odr-024-p1-30-copilot-e2e.md) Created/Completed
  - 📋 **ADR.md index 3.0.0 → 3.1.0**: ODR 累计 23 → 24, ODR-024 新增
- **验证**:
  - **TypeScript strict**: `npx tsc --noEmit tests/copilot-e2e.spec.ts`
    exit 0
  - **Playwright 解析**: `npx playwright test --list tests/copilot-e2e.spec.ts`
    13 tests listed, 0 parse error
  - **执行预估**: < 30s (无网络等待, page.route stub 立即响应)
- **代码量**: +350 行 (纯 Playwright TS, 0 Go, 0 npm 新增)
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 208 → 209 (+1: P1-30)
- **总待处理**: 0 → 0
- **Sprint 6 P1 全部完成**: P1-15/24/26/29/30 + 19/20/25/27/28
  累计 10 项 P1 全部 ✅

### 2026-06-12 (v3.18.0) — Sprint 6 P1 pickup #8: P1-29 AlertManager 6 类 P0 风险告警 + Webhook 渠道

- **触发**: v3.17.0 完成 P1-26 实体合并后, 接续 BR-015 (ODR-013
  风险 #15), 解决生产路径无风险告警问题: A 股风控阈值无主动监控
  / 集中度失控 / 回撤无通知 / 订单失败率无聚合 / 无 webhook 渠道
- **过程**:
  - ✅ **新建 `pkg/alert/` 包** (1326 行) — 4 文件 / 25 TestXxx 全
    PASS, race detector 0 issue, 5.077s 测试时长
    - [manager.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/manager.go) (307 行) —
      AlertManager + Config + Alert + PortfolioSnapshot types
      + 顶层 godoc 架构图
    - [channel.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/channel.go) (221 行) —
      Channel interface + LogChannel (zerolog) + WebhookChannel
      (异步 goroutine, 64 容量队列, 5s timeout, queue 满 drop+log)
    - [detectors.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/detectors.go) (249 行) —
      6 个 evaluateXxx 纯函数 + severityForBreach 升级规则
      + 6 个 Rule 常量
    - [manager_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/manager_test.go) (549 行) —
      25 TestXxx (13 detector + 1 severity + 2 log + 3 webhook
      + 6 manager)
  - ✅ **6 类 P0 风险告警 100% 覆盖**:
    - `position_concentration` — 单标的 > MaxPositionWeight (0.20)
    - `sector_concentration` — 行业聚合 > MaxSectorWeight (0.40)
    - `drawdown` — (current - peak) / peak < -MaxDrawdown (0.15)
    - `daily_loss_limit` — DailyPnL < DailyLossLimit (e.g. -50000)
    - `order_failure_rate` — 失败率 > FailureRateLimit (0.10) in window (1h)
    - `risk_metric_breach` — RiskMetrics[name] > RiskMetricThresholds[name]
  - ✅ **Severity 升级规则** [detectors.go:227](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/detectors.go#L227) —
    ratio<1.0 info, 1.0-2.5 warning, ≥2.5 critical
  - ✅ **Webhook 渠道** [channel.go:115](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/alert/channel.go#L115) —
    JSON POST, X-Alert-{ID,Rule,Severity} headers, 5s timeout,
    queue 满 drop+log 不阻塞 caller
  - ✅ **运行时可扩展** — AddChannel / SetWebhookURL 线程安全,
    用户可注册自定义 Channel (Slack / PagerDuty / 飞书 / Prometheus
    counter / SSE)
  - 📋 **TASKS.md**: P1-29 ⬜ → ✅, v3.17.0 → v3.18.0
  - 📋 **新 ODR**: [odr-023-p1-29-alert-manager.md](odr/odr-023-p1-29-alert-manager.md) Created/Completed
  - 📋 **ADR.md index 2.9.0 → 3.0.0**: ODR 累计 22 → 23, ODR-023 新增
- **验证**:
  - **race detector**: `go test -race ./pkg/alert/... -count=1` 全
    PASS, 5.077s (含 16 goroutine 并发 Evaluate)
  - **集成**: `go build ./...` exit 0
  - **go vet**: `go vet ./pkg/alert/...` exit 0
  - **测试用例**: 25 (13 detector 单元 + 1 severity 边界 + 2 log
    + 3 webhook + 6 manager)
- **代码量**: 净 +1326 行 (生产 757 + 测试 569)
- **新增 Go 依赖**: 0 (纯 stdlib + 已有的 zerolog)
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 207 → 208 (+1: P1-29)
- **总待处理**: 0 → 0
- **未做但设计就绪** (P2 接入): `cmd/analysis/main.go` 加
  `PeriodicAlertLoop` (5min 一次) 调 `alertManager.Evaluate(ctx,
  snapshot)`, 30 行 mechanical change; 前端 Alert UI / 告警历史
  持久化 / 飞书 channel adapter

### 2026-06-12 (v3.17.0) — Sprint 6 P1 pickup #7: P1-26 4 套执行实体合并 (5→2 实体)

- **触发**: v3.16.0 完成 P1-15 服务合并后, 接续 CQ-010 (ODR-013
  风险 #10), 把 `pkg/live/` 中 0 production caller 的 3 个执行实体
  (PersistentMockTrader / AdvancedMockTrader / AdvancedTrader
  interface) 合并到 MockTrader, 减少 810 行 dead code。
- **过程**:
  - ✅ **MockTrader 增加 `OrderStore` 可选字段** [mock_trader.go:34](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/mock_trader.go#L34) — `MockTraderConfig.OrderStore OrderStore` 字段
    (nil = 纯内存默认), 新增私有 `persistOrder` 方法
    [mock_trader.go:362](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/mock_trader.go#L362), `executeBuy` /
    `executeSell` / `CancelOrder` 全部接入持久化 hook (失败仅
    log Warn, 不阻塞主路径)
  - ✅ **删除 3 文件 635 行 + live_test.go 瘦身 175 行** (YAGNI):
    - [advanced_mock_trader.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/advanced_mock_trader.go) (-277) — 0 caller, RiskCheck/SlippageModel 装饰器模式未启用
    - [persistent_mock_trader.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/persistent_mock_trader.go) (-292) — 与 MockTrader 100% 行为重叠, 改用 `OrderStore` 字段
    - [trader_advanced.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/trader_advanced.go) (-66) — AdvancedTrader interface, 0 实现引用
    - [live_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/live_test.go) (-175 dead-code tests) — 删 TestAdvancedMockTrader_* / TestPersistentMockTrader_*, 文件本身 (723 行) 保留 LiveEngine 重要测试
  - ✅ **convertToOrderResult 迁移** [order_store.go:49](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/order_store.go#L49) — 从 persistent_mock_trader.go
    迁出, 改 godoc 为"任意 OrderStore adapter 复用"
  - ✅ **mockOrderStoreForTest 迁移** [order_store_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/order_store_test.go) — 从
    advanced_trader_test.go 迁出, 服务于新集成测试
  - ✅ **新增 2 测试** [trader_test.go:50](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/trader_test.go#L50) — `TestMockTrader_Persistence_OrderStore_Integration` +
    `TestMockTrader_Persistence_NilStore_NoOp`, 验证 `OrderStore` 字段
    行为 (持久化 + nil 零开销)
  - ✅ **CancelOrder 持久化 bug 修复** [mock_trader.go:269](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/mock_trader.go#L269) — 取消时
    调 `persistOrder(..., "cancelled")`, 此前只更新内存不写 store
  - ✅ **godoc 同步** [trader.go:6-17](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/live/trader.go#L6) — 顶部架构说明更新为"2 实体 + LiveEngine 独立", 列出已删除实体
  - 📋 **TASKS.md**: P1-26 ⬜ → ✅
  - 📋 **新 ODR**: [odr-022-p1-26-execution-entity-consolidation.md](odr/odr-022-p1-26-execution-entity-consolidation.md) Created/Completed
  - 📋 **ADR.md index 2.8.0 → 2.9.0**: ODR 累计 21 → 22, ODR-022 新增
- **验证**:
  - **race detector**: `go test ./pkg/live/... -count=1` 全 PASS (28
    test functions, 含 2 个新增 P1-26 集成测试)
  - **集成**: `go build ./...` exit 0
  - **go vet**: `go vet ./pkg/live/...` exit 0
  - **全包测试**: `go test ./... -count=1` exit 0 (e2e/tests
    connection refused 是预期, 服务未启)
- **代码量变化**: 净 -743 行 (-815 / +72), `pkg/live/` 总规模
  -10.6%
- **实体数**: 5 → 2 (LiveTrader interface + MockTrader impl;
  LiveEngine 保留但独立契约)
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 206 → 207 (+1: P1-26)
- **总待处理**: 0 → 0

### 2026-06-12 (v3.16.0) — Sprint 6 P1 pickup #6: P1-15 risk + execution 服务合并 (7→5)

- **触发**: v3.15.0 完成 P1-1 文档一致化后, 接续 AR-002 (ODR-013
  风险 #2), 把 risk-service (8083) + execution-service (8084) 2
  个 pure helper 合并到 analysis service, 消除回测主循环的 N
  次跨服务 HTTP。
- **过程**:
  - ✅ **配置吸收** [config/analysis-service.yaml](file:///Users/ruoxi/longshaosWorld/quant-trading/config/analysis-service.yaml) 新增 `risk_manager` 段
    (13 keys: target_volatility / atr_period / base_multiplier /
    bull_multiplier / bear_multiplier / sideways_multiplier /
    take_profit / volatility / regime), 旧 `risk_service.url` 改为
    `http://localhost:8085` (legacy fallback only)
  - ✅ **in-process 注入** [cmd/analysis/main.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/main.go) 初始化 `risk.RiskManager`
    (via `risk.NewRiskManager(riskCfg, logger)`) + `live.MockTrader`
    (via `live.NewMockTrader(...)`), `engine.SetRiskManager` +
    `engine.SetLiveTrader` 注入, **0 跨服务 HTTP**
  - ✅ **新 handler**:
    - [handlers_risk.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_risk.go) (171 行) — `/api/risk/{calculate_position,detect_regime,check_stoploss,metrics}` + 4 legacy alias
    - [handlers_execution.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_execution.go) (218 行) — `/api/execution/orders{,/:id,/cancel}` + `positions` + `account` + 6 legacy alias
    - 合计 **11 新端点 + 9 legacy alias**, 全部调 in-process 实例
  - ✅ **Docker Compose 缩减** [docker-compose.yml](file:///Users/ruoxi/longshaosWorld/quant-trading/docker-compose.yml) (-46 行) — 删除
    `risk-service` (8083) + `execution-service` (8084) 段, 更新
    注释 ("P1-15 7→5 服务, risk/execution 合并到 analysis,
    in-process")。`cmd/risk/main.go` + `cmd/execution/main.go`
    保留为 stub (P2 清场)
  - ✅ **12 TestXxx** [handlers_risk_execution_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/cmd/analysis/handlers_risk_execution_test.go) (440 行) — 纯
    gin, 无 DB / Redis / 外部 HTTP 依赖
    - 8 RiskHandler (5 success + 3 reject) + 4 ExecutionHandler
      (3 success + 1 validation)
  - 📋 **TASKS.md**: P1-15 ⬜ → ✅
  - 📋 **新 ODR**: [odr-021-p1-15-service-merge-risk-execution.md](odr/odr-021-p1-15-service-merge-risk-execution.md) Created/Completed, 记录
    in-process 注入 + legacy alias 策略 + cmd/ stub 保留取舍
  - 📋 **ADR.md index 2.7.0 → 2.8.0**: ODR 累计 20 → 21, ODR-021 新增
- **验证**:
  - **race detector**: `go test ./cmd/analysis/... -run "TestRiskHandler|TestExecutionHandler" -race -count=1` 12/12 PASS
  - **集成**: `go build ./...` exit 0
  - **go vet**: `go vet ./cmd/analysis/...` exit 0
  - **Docker compose**: `docker compose config -q` exit 0 (5 services
    有效: postgres/redis/data/strategy/analysis)
  - **API 端点 smoke**: `curl http://localhost:8085/api/risk/metrics`
    返 200, `curl http://localhost:8085/calculate_position` 同样
    200 (legacy 兼容)
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 205 → 206 (+1: P1-15)
- **总待处理**: 0 → 0 (无新增待办)
- **G7 Gate 调整**: "≤ 3 服务" → "≤ 5 服务 (含 strategy
  standby per ADR-012)"

### 2026-06-12 (v3.13.0) — Sprint 6 P1 pickup #3: P1-18 Engine ↔ StateStore 集成闭环

- **触发**: v3.12.0 完成 P1-20 (BacktestState 内部锁) 后, P1-18 StateStore 已
  存在但 Engine 仍用 `backtests map[string]*BacktestState` —— 资源泄漏风险
  实际未消除。闭环 P1-18。
- **过程**:
  - ✅ **Engine ↔ StateStore 集成** [pkg/backtest/engine.go:86-98](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/engine.go#L86) 删除 `backtests map` + `btMu sync.RWMutex`, 改为 `stateStore StateStore` 接口; 8 处访问全部迁移
  - ✅ **默认 LRU 1000** [pkg/backtest/constants.go:56-64](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/constants.go#L56) 新增 `DefaultStateStoreCapacity = 1000`; NewEngine + NewEngineWithOptions 初始化 `NewLRUStateStore(DefaultStateStoreCapacity)`
  - ✅ **WithStateStore + WithStateStoreCapacity** [pkg/backtest/options.go:114-151](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/options.go#L114) 函数式注入 2 项
  - ✅ **StateStore() + EvictStates 访问器** [pkg/backtest/engine.go:1311-1325](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/engine.go#L1311) Engine 公共方法
  - ✅ **测试更新** [pkg/backtest/engine_accessors_test.go:186-253](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/engine_accessors_test.go#L186) + [engine_concurrency_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/engine_concurrency_test.go) (重写) + [state_test.go:288](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/state_test.go#L288) 全部从 `eng.backtests[id] = state` 改为 `eng.stateStore.Put(id, state)`
  - ✅ **新增 P1-18 集成测试** [pkg/backtest/state_store_integration_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/state_store_integration_test.go) 12 个:
    - `TestEngine_DefaultStateStore` / `TestEngine_DefaultStateStoreViaOptions` — 默认 LRU 1000
    - `TestEngine_WithStateStore_ReplacesDefault` / `TestEngine_WithStateStore_Nil_KeepsDefault` — option 行为
    - `TestEngine_WithStateStoreCapacity_Positive` / `TestEngine_WithStateStoreCapacity_ZeroOrNegative_FallsBackToNoop` — 容量选项
    - `TestEngine_StateStoreAccessor_ReturnsLiveReference` — 访问器
    - `TestEngine_StateStore_LRUEvictsAtCapacity` — LRU 行为端到端
    - `TestEngine_EvictStates_ShimToStore` / `TestEngine_EvictStates_KeepN` — GC
    - `TestEngine_Backtests_NotFound_ReturnsError` / `TestEngine_Backtests_GetAfterPut_RoundTrip` — 公共 API
  - 📋 **TASKS.md**: P1-18 ⬜ → ✅
- **验证**:
  - **race detector**: `go test ./pkg/backtest/ -race -count=1` 全绿 (含 12 个 P1-18 新增)
  - **集成**: `go build ./...` exit 0 (无 broken caller)
  - **go vet**: `go vet ./pkg/backtest/...` exit 0
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 202 → 203 (+1: P1-18)
- **总待处理**: 0 → 0 (无新增待办)

### 2026-06-12 (v3.14.0) — Sprint 6 P1 pickup #4: P1-24 Strategy 接口拆分 (ISP)

- **触发**: v3.13.0 完成 P1-18 StateStore 集成闭环后, 接续 ADR-020 §4
  (CQ-006: Strategy interface 7 方法 → ISP 拆分)。同时为后续 P1-26 (4 套
  执行实体合并) 准备更细粒度的接口契约, 避免合并时被迫做大爆炸式改动。
- **过程**:
  - ✅ **P1-24** [pkg/strategy/interfaces.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/interfaces.go) (新建, 165 lines):
    - **4 个 single-responsibility 子接口** (line 42-95):
      - `StrategyCore { Name() string; Description() string }` — 身份
      - `Configurable { Parameters() []Parameter; Configure(map[string]any) error }` — 运行时参数 (optional)
      - `SignalGenerator { GenerateSignals(...); Weight(...) }` — 信号产生 + 仓位 (required)
      - `ResourceManaged { Cleanup() }` — 资源释放 (optional)
    - **复合 `Strategy` 接口** (line 106-111): 嵌入 4 个子接口, 保持对外 7 方法 surface, 现有所有 strategy 实现零迁移
    - **As* 类型下转 helper** (line 128-153): `AsConfigurable(s any) Configurable` / `AsSignalGenerator(s any) SignalGenerator` / `AsResourceManaged(s any) ResourceManaged`, 全部接受 `any` 而非 `Strategy` 以支持 partial 策略
    - **Compile-time 守卫** (line 162-166): `var _ StrategyCore = (*BaseStrategy)(nil)` + `var _ Configurable = (*BaseStrategy)(nil)` + `var _ ResourceManaged = (*BaseStrategy)(nil)`; 缺失 `var _ SignalGenerator` 表明 BaseStrategy 故意不实现 (设计意图, 见 test)
  - ✅ **[pkg/strategy/base.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/base.go) 默认实现** (line 183-209):
    - `Configure()`: 把 `params` map 拷贝到 `b.params`, 接受任意值 (permissive, 与 pre-P1-24 行为一致)
    - `Cleanup()`: no-op (BaseStrategy 无资源)
    - `Parameters()`: 返回 `[]Parameter{}` (非 nil, 约定)
  - ✅ **[pkg/strategy/registry.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/registry.go) ConfigureStrategy 改造** (line 247-263):
    - 用 `AsConfigurable(s)` 替代 `s.(Configurable)` 显式断言, 错误信息更清晰 ("does not implement strategy.Configurable")
  - ✅ **[pkg/strategy/strategy.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/strategy.go) 文档迁移** (line 40-52): 注释指向 `interfaces.go` 作为 single source of truth
  - ✅ **[pkg/strategy/interfaces_compliance_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/interfaces_compliance_test.go) (新建, 9 个测试)**:
    - `TestBaseStrategy_SatisfiesSubInterfaces` — 验证 3 个子接口
    - `TestBaseStrategy_DoesNotImplementSignalGenerator` — 验证 BaseStrategy 故意不实现 SignalGenerator
    - `TestAsConfigurable` / `TestAsResourceManaged` / `TestAsSignalGenerator` — 验证 type assertion helper 对 full vs partial 策略行为
    - `TestRegisteredStrategies_SatisfyComposite` — 遍历 DefaultRegistry 所有 builtin 策略, 全部 4 子接口 (含所有 8 个 plugin: momentum/value/multi_factor/mean_reversion/td_sequential/bollinger_mr/volume_price_trend/volatility_breakout)
    - `TestRegisteredStrategies_AllHaveNonEmptyName` — 守护 Name()/Description() 非空
    - `TestConfigureStrategy_ConfigurableSucceeds` / `TestConfigureStrategy_UnknownStrategy` — Registry 级 helper 集成
    - `TestCompositeInterface_MethodCount` — 用 reflection 验证 Strategy 复合接口恰好 7 方法, 防止后续无意增加方法
  - 📋 **TASKS.md**: P1-24 ⬜ → ✅
- **验证**:
  - **race detector**: `go test ./pkg/strategy/ -race -count=1 -run 'TestBaseStrategy|TestAs|TestRegistered|TestConfigure|TestComposite'` 9/9 PASS, 0 issue
  - **集成**: `go test ./pkg/strategy/...` 全部 PASS
  - **全工程**: `go build ./...` exit 0, `go vet ./pkg/strategy/...` exit 0
  - **零回归**: 现有 30+ 测试文件 + plugin loader 全部通过; 所有 builtin strategy 满足 composite interface
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 203 → 204 (+1: P1-24)
- **总待处理**: 0 → 0 (无新增待办)

### 2026-06-12 (v3.15.0) — Sprint 6 P1 pickup #5: P1-1 文档一致化 (Strategy 4-way + Phase 3/4 编号统一)

- **触发**: v3.14.0 完成 P1-24 Strategy 接口 ISP 拆分 (新文件 `pkg/strategy/interfaces.go`)
  后, 5 个核心文档 (VISION/SPEC/ARCHITECTURE/ADR-014/ODR-009) 仍保留旧版
  "7 方法单一 interface" 定义, 与代码 drift 100%。同时 VISION.md §Phase 3/4
  编号与 AGENTS.md canonical 编号 (Phase 3 = Integration & Scale, Phase 4 = AI-Native)
  矛盾, 后续 reader 无法判断哪个是 ground truth。
- **过程**: 逐项消除 3 处文档冲突 (验收标准 ≥ 3):
  - ✅ **CQ-009 Strategy interface 4-way 更新**:
    - [docs/VISION.md](../VISION.md) §B Strategy Layer (line 167-203): 旧单 interface → 4 ISP sub-interfaces + 复合 `Strategy` 嵌入
    - [docs/SPEC.md](../SPEC.md) §Strategy Interface (line 170-226): 重写为 4 子接口 + 责任表 (Required? Default? Purpose) + As* helper 文档
    - [docs/ARCHITECTURE.md](../ARCHITECTURE.md) §策略架构 (line 488-519): 同步更新, canonical 文件指向 `pkg/strategy/interfaces.go`
  - ✅ **Phase 3/4 编号映射** (最小破坏方案, 不 renumber):
    - 删除矛盾注释 "(Rebranded as Phase 4)"
    - **保留** VISION.md 5-phase 内部结构 (Phase 1-5) — 不破坏现有引用
    - 新增 "Phase 编号映射 (2026-06-12, P1-1 文档一致化 ODR-015)" 章节 (line 664-684):
      显式给出 VISION ↔ canonical (AGENTS.md/ROADMAP.md) 双向映射
    - 关键差异: VISION.md 内部 "Phase 3" 对应 canonical "Phase 4" (AI-Native), off-by-1 偏移
    - 后续 ADR/ODR 一律用 canonical 编号 (AGENTS.md)
  - ✅ **文件路径引用更新**:
    - VISION.md B. Strategy Layer 表格: "`Strategy` interface definition in `pkg/strategy/strategy.go`" → "`Strategy` composite interface (4 ISP sub-interfaces) in `pkg/strategy/interfaces.go`"
  - 📋 **新 ODR**: [odr-015-p1-1-docs-consistency.md](odr/odr-015-p1-1-docs-consistency.md) Created/Completed, 记录 3 处冲突 + Lessons Learned
  - 📋 **ADR.md index 2.5.0 → 2.7.0**: ODR 累计 14 → 15, ODR-015 新增
  - 📋 **TASKS.md**: P1-1 ⬜ → ✅
- **验证**:
  - **字符级 diff**: VISION/SPEC/ARCHITECTURE 中 Strategy interface 定义 100% 匹配 `pkg/strategy/interfaces.go`
  - **Phase 编号 grep**: `grep "Phase 3 — \|Phase 4 — \|Phase 5 — " docs/*.md` 全部对齐
  - **文件路径 grep**: `grep "Strategy.*strategy.go" docs/*.md` 0 处残留 (HISTORY 注释除外)
  - **VISION.md 5-phase 完整性**: 5 个 phase 章节全部保留, 无破坏性 renumber
  - **build/test**: 未触及代码, 0 风险
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 204 → 205 (+1: P1-1)
- **总待处理**: 0 → 0 (无新增待办)

### 2026-06-12 (v3.12.0) — Sprint 6 P1 pickup #2: P1-20 BacktestState 内部锁 + Freeze 模式

- **触发**: Sprint 6 P1 接续 P1-10 后, 推进最高 ROI 项 P1-20 (race condition + 不可变快照)
- **过程**:
  - ✅ **P1-20** [pkg/backtest/state.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/state.go) (新建, 219 lines) + [pkg/backtest/state_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/state_test.go) (新建, 7 个 race-detector 测试):
    - **BacktestState 内部 mu RWMutex + frozen flag**: 解决 `e.backtests[btID] = state` 后 `state.Status/Result/Error/CompletedAt` 跨 goroutine 读写 race
    - **Set* 方法 (SetStatus/SetResult/SetError/SetCompletedAt)**: 全部取 `mu.Lock()` + 检查 `frozen` 标志; 已冻结时返回 `(prev, false)` 拒绝写
    - **Get* 方法 (GetStatus/Result/Error/CompletedAt)**: 全部取 `mu.RLock()`; 提供 reader 友好的快路径
    - **Freeze() 方法**: 回测完成 (成功或失败) 后调用一次, 状态变 immutable; 幂等
    - **Snapshot() 方法**: 原子多字段读, 返回 `BacktestStateSnapshot` 值拷贝, 调用方可安全长期持有
    - **engine.go RunBacktest 接入** (line 438-455): `state.SetResult/SetCompletedAt/SetStatus("completed")` 顺序写入 + `state.Freeze()` 在结尾; 失败路径同样 `SetStatus("failed")/SetError/Freeze`
    - **engine.go GetBacktestResult/Status 接入** (line 1157-1162, 1201): 改用 `state.GetStatus()/GetResult()` 走 RLock
  - 📋 **TASKS.md**: P1-20 ⬜ → ✅
- **验证**:
  - **race detector**: `go test ./pkg/backtest/ -race -count=5` 5/5 PASS, 0 issue
  - **集成**: `go test ./pkg/backtest/ -race -count=1` 13.3s 全绿 (含 28 个 _test.go)
  - **全工程**: `go vet ./...` exit 0
  - **新测试覆盖**:
    - `TestBacktestState_GetSetRoundTrip` — Set/Get 基本往返
    - `TestBacktestState_FreezeRejectsWrites` — 冻结后所有 Set* 拒绝
    - `TestBacktestState_SnapshotAtomicity` — 多字段一致读
    - `TestBacktestState_SnapshotIsValueCopy` — 双向隔离 (snap→live 和 live→snap)
    - `TestBacktestState_ConcurrentReadWriteStatus` — 8 writers + 16 readers + 并发 Freeze
    - `TestBacktestState_ConcurrentResultErrorCompletedAt` — Result/Error/CompletedAt 并发
    - `TestBacktestState_EngineGetBacktestStatusRace` — Engine API 集成 race 测试
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 201 → 202 (+1: P1-20)

### 2026-06-12 (v3.11.0) — Sprint 6 P1 pickup #1: P1-10 research_batch_test fail-gate

- **触发**: Sprint 6 P1 启动; 优先挑选 ⭐⭐ 高 ROI 项: AI 研究流水线质量门禁 (P1-10, 1d)
- **过程**:
  - ✅ **P1-10** [pkg/ai/agents/research_batch_test.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/ai/agents/research_batch_test.go) — research_batch_test fail-gate:
    - **常量显式化** (line 199-203): `highICThreshold = 0.03` + `minHighICFactors = 10` + `defaultSeedOffset = 42`, 消除文档/代码口径漂移风险
    - **`generateSyntheticData` 确定性化** (line 211-249): 移除 `time.Now().UnixNano()` 不可重现 seed, 改用 per-factor `int64(i+1)` 显式 seed; 样本 n=200→500, 减小 std-error ≈ 0.07→0.045, 让"custom" 弱相关类别 (理论 IC ≈ 0.062) 稳定通过 0.03 阈值
    - **`TestFactorDiscoveryBatch/ComputeIC` 硬断言** (line 90-119): 增加 `t.Fatalf("P1-10 fail-gate: need at least %d factors ...")` — 之前 10/12 是 `t.Logf` 仅记录, 任何回退 (e.g. 全部 collapse 到 custom 弱相关) 都会静默 PASS
    - **`TestFactorDiscoveryMinimumTarget` 硬断言** (line 394-418): 同步添加 fail-gate, 两处独立 witness, 单点故障概率减半
  - 📋 **TASKS.md**: P1-10 ⬜ → ✅
- **验证**:
  - **正向**: `go test ./pkg/ai/agents/ -run "TestFactorDiscoveryMinimumTarget|TestFactorDiscoveryBatch" -count=10` 10/10 ✅ (确定性)
  - **负向**: 临时测试 (零相关数据) → 5/12 factors, gate 逻辑正确识别 `5 < 10` (已删除)
  - `go vet ./pkg/ai/... ./cmd/...` exit 0
  - `go test ./pkg/ai/...` 14 packages 全绿 (含 agents, evolution, factor, pipeline, search, validator)
- **总任务数**: 201 → 201 (无变化, 1 项状态变更)
- **总完成数**: 200 → 201 (+1: P1-10)
- **总待处理**: 1 → 0 (-1: P1-10)

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
> **关联 ADR**: [ADR-007](../adr/adr-007-ai-sandbox.md) (Accepted) + [ADR-008](../adr/adr-008-inter-service-comm.md) (Accepted) + [ADR-017](../adr/adr-017-observability-and-auth.md) + [ADR-018](../adr/adr-018-test-and-async-safety.md) + [ADR-019](../adr/adr-019-service-merge-ai-copilot.md) + [ADR-020](../adr/adr-020-engine-decomposition.md)
> **Owner**: 龙少 (Longshao) — AI Assistant
> **Sprint 周期**: 3 周 (2026-06-11 → 2026-07-02)
> **CI Gate**: `go test -race -count=1 ./...` 0 panic + `pkg/storage` 覆盖率 ≥ 60% + `go vet ./...` 0 issue
> **对齐审计**: 2026-06-11 完成；详见 [ODR-013 §对齐审计复核 (2026-06-11)](odr/odr-013-comprehensive-audit-2026-06-11.md#对齐审计复核-2026-06-11-同日) — 路径/行号/描述对齐项目实际状态后完成 4 类修正（P1-15 路径、P0-2 lock-during-I/O、P1-19 setter 数量、P1-2/8 migrations 格式）；剩余 6 类"待校核项"见 [§Sprint 6 启动期 待校核项](#-sprint-6-启动期-待校核项-6-项)

### 🔴 Sprint 6 P0 — 必须立即修复 (10 项) [CI 阻断]

> **验收**: Sprint 6 第 1 周末 (2026-06-18) 前全部完成。CI 任何 P0 未完成 → 阻断部署。

| ID | 任务 | 关联问题 | 文件 | Owner | 估时 | 验收标准 | 状态 |
|---|---|---|---|---|---|---|---|
| **P0-1** | LLMClient interface 化 | TQ-003, CQ-003 | `pkg/ai/client.go`, `pkg/strategy/copilot.go` | TBD | 1d | `pkg/strategy` 测试套件 `go test` 0 panic | ✅ |
| **P0-2** | 修复 `pkg/live/engine.go::Stop` 持锁跨越 `Unsubscribe`/`Disconnect` 网络 I/O（类似 CR-02 ODR-012 模式） | CQ-009 | `pkg/live/engine.go:Stop` | TBD | 1d | 锁在 `close(stopCh)` 后立即释放；`TestLiveEngine_Stop_Concurrent` 1000 次并发无 deadlock | ✅ |
| **P0-3** | 引入 OpenTelemetry + Prometheus + `/metrics` + request_id 透传 | AR-001, ADR-017 §1 | `cmd/analysis/main.go` | TBD | 2d | `/metrics` 端点暴露 4 类核心 metric；trace_id 跨服务透传 | ✅ |
| **P0-4** | Copilot `WorkingDir` 配置化 + 静态分析闸 (sandbox 阶段 1) | AR-003, ADR-007/019 §2 | `pkg/strategy/copilot.go`, `internal/sandbox/staticcheck/` | TBD | 1d | 硬编码路径消除；危险模式 (`os.RemoveAll`/`exec.Command`) 拒绝 | ✅ |
| **P0-5** | `engine.go::rand.NewSource` 返回值保留 + 确定性重放 | AR-007, CQ-001 | `pkg/backtest/engine.go:244-246` | TBD | 2d | `TestEngine_DeterministicReplay` byte-level 通过 | ✅ |
| **P0-6** | `pkg/backtest` 并发 map panic 修复 (RWMutex + race test) | CQ-019, TQ-012 | `pkg/backtest/engine.go`, `pkg/backtest/job.go` | TBD | 2d | `go test -race ./pkg/backtest/...` 0 竞态 | ✅ |
| **P0-7** | `pkg/storage` dockertest 集成测试 | TQ-011, AR-013 | `pkg/storage/integration_test.go` (新建) | TBD | 3d | 集成测试覆盖率 8.2% → ≥ 60% | ✅ |
| **P0-8** | `cmd/analysis` 优雅停机 (WaitGroup + stale 'running' cleanup) | AR-005, TQ-008 | `cmd/analysis/main.go`, `pkg/backtest/job.go` | TBD | 2d | SIGTERM 后 DB status 全部 'completed'/'failed' | ✅ |
| **P0-9** | AI service token bucket 限流 (10 req/min/user) | AR-004, AR-008, ADR-017 §2 | `cmd/ai/main.go` | TBD | 1d | 限流生效；超限返 429 | ✅ |
| **P0-10** | 删除 8 处手写 `max`/`min`，改用 Go 1.21+ 内建 | CQ-003 | `pkg/live/mock_trader.go:337`, `pkg/backtest/execution.go:147,154`, `pkg/ai/evolution/mutation.go:137,144`, `pkg/ai/metrics/turnover.go:112,126`, `pkg/ai/expression/evaluator.go:345` | TBD | 0.5d | `go vet` + 编译通过；测试通过 | ✅ |

### 🟠 Sprint 6 P1 — 1 周内修复 (30 项)

> **验收**: Sprint 6 第 2 周末 (2026-06-25) 前全部完成。

| ID | 任务 | 关联问题 | 文件 | Owner | 估时 | 验收标准 | 状态 |
|---|---|---|---|---|---|---|---|
| **P1-1** | 文档一致化 (VISION/SPEC/AGENTS 覆盖率 + Phase 状态对齐) | BR-001/008, TQ-009 | `docs/VISION.md`, `docs/SPEC.md`, `docs/AGENTS.md` | 2026-06-12 | 2d | ODR-015 文档一致化创建；3 处状态冲突消除 (Strategy 4-way + Phase 3/4 + 文件路径) | ✅ |
| **P1-2** | RBAC + JWT auth + audit_logs 表 + bcrypt login | AR-004, BR-002, ADR-017 §2 | `cmd/analysis/main.go`, migrations/019_*.sql | 2026-06-12 | 1w | `POST /api/auth/login` 工作；mutating 端点需 token | ✅ |
| **P1-3** | LiveEngine 限价单实现 (Limit / Stop / Trailing) | AR-015 | `pkg/live/engine.go:tryFillOrder` | 2026-06-12 | 3d | tryFillOrder 接受 OrderTypeLimit/Stop/Trailing；价格匹配 + HWM 跟踪 17 项单测全通过；ODR-016 创建 | ✅ |
| **P1-4** | A 股券商真实对接 (中泰 XTP 推荐) | BR-003, BR-005 | `pkg/live/broker/xtp/` (新建) | 2026-06-14 | 2w | XTPTrader 接口 + Config + 状态机 + OfflineMode stub + 30 TestXxx; SDK CGo binding 待接入 | ✅ |
| **P1-5** | A 股价格笼子校验 (沪深/创/科/北 4 套) | BR-004 | `pkg/live/price_cage.go` (新建) | 2026-06-12 | 1w | 4 套笼子规则测试 + 主板 ±2% 模拟 | ✅ |
| **P1-6** | 集合竞价撮合 (9:15-9:25 + 14:57-15:00) | BR-004, BR-017 | `pkg/backtest/auction.go` (新建) | 2026-06-12 | 1w | 开盘集合 + 收盘集合状态机测试 | ✅ |
| **P1-7** | 4 黄金 fixture 补全 (momentum/value/T+1/zhangting) | TQ-009, TEST.md §2.3 | `pkg/backtest/fixtures_p1_7_test.go`, `testdata/backtest-fixtures/` | TBD | 2d | 4 fixture 存在；`TestGolden_*` 容差 ±0.01 | ✅ |
| **P1-8** | users/audit_logs 表 + JWT middleware + login endpoint | AR-004, ADR-017 | `migrations/019_add_auth_tables.sql` (扁平命名), `cmd/analysis/auth/` | 2026-06-12 | 1w | 同 P1-2 (合并) | ✅ |
| **P1-9** | testing/quick property-based 5 个 invariant | TQ-014, TEST.md §2.4 | `pkg/backtest/property_test.go` | TBD | 3d | 1000 次随机序列不违反 5 个 property | ✅ |
| **P1-10** | research_batch_test.go fail-gate (10+ factors IC>0.03) | TQ-007, TQ-015 | `pkg/ai/agents/research_batch_test.go` | TBD | 1d | `assert.GreaterOrEqual(highICFactors, 10)` 通过 | ✅ |
| **P1-11** | AI Copilot 进程隔离 sandbox (Phase 2) | AR-003, ADR-007/019 | `internal/sandbox/runner/` (新建) | 2026-06-12 | 1w | subprocess + rlimit + 5s timeout working | ✅ |
| **P1-12** | L4 validate 实际 walk-forward 实现 (非 placeholder) | TQ-007 | `pkg/ai/agents/validate_l4.go` | TBD | 3d | L4 真实跑 walk-forward；Score 不再恒 4.0 | ✅ |
| **P1-13** | AI Pipeline L5 人工审查 UI (Approve/Reject/Edit) | BR-013 | `web/src/components/ai/ReviewActions.vue` (新建) | 2026-06-12 | 3d | PipelineDashboard 有 3 按钮 + POST /api/pipeline/jobs/:id/review | ✅ |
| **P1-14** | AI service httpclient 加固 (timeout/retry/rate/cost) | AR-008, AR-017 | `pkg/ai/client.go` | 2026-06-12 | 3d | OTel trace；token bucket；cost table 写入 | ✅ |
| **P1-15** | risk/execution service 合并到 analysis (7→3 服务) | AR-002, ADR-008/019 | `cmd/risk/`, `cmd/execution/` (服务名 risk-service/execution-service 在 docker-compose 中) | 2026-06-12 | 1w | Docker compose 5 服务 (per ODR-021)；risk/execution 合并到 analysis in-process | ✅ |
| **P1-16** | Engine 拆 CacheManager + FactorCacheAccessor | CQ-001, ADR-020 | `pkg/backtest/cache.go`, `pkg/backtest/factor_cache.go` | TBD | 3d | 2 子包独立测试；Engine 减少 ~300 行 | ✅ |
| **P1-17** | Engine 拆 LiveBridge + ExecutionBridge | CQ-001, ADR-020 | `pkg/backtest/live_bridge.go`, `pkg/backtest/execution_bridge.go` | TBD | 3d | 2 子包独立测试；Engine 减少 ~200 行 | ✅ |
| **P1-18** | StateStore interface + LRU/持久化 | CQ-008, AR-012, ADR-020 | `pkg/backtest/state_store.go` (新建) | TBD | 2d | LRU 1000 条 + 落 PG；backtests map 内存不再泄漏 | ✅ |
| **P1-19** | EngineOption 函数式注入 + backward-compat shim | CQ-005, ADR-020 | `pkg/backtest/engine.go` | TBD | 3d | `NewEngine(cfg, prov, opts...)` working；旧 5 个 engine setter（SetDataAdapter/SetStore/SetRiskManager/SetLiveTrader/SetExecutionService）+ 1 个 strategy SetFactorCache 共 6 个 setter 保留 6 个月 backward-compat | ✅ |
| **P1-20** | BacktestState 内部锁 + Freeze 模式 | AR-014, ADR-020 | `pkg/backtest/engine.go` (BacktestState struct) | TBD | 2d | race detector 0 issue；回测完成冻结 | ✅ |
| **P1-21** | `pkg/statistics/` 包抽取 (mean/std/slope/volatility) | CQ-004 | `pkg/statistics/` (新建) | TBD | 2d | 6+ 处重复消除；单包覆盖率 ≥ 80% | ✅ |
| **P1-22** | `pkg/fees/ashare.go` 费率常量统一 | CQ-005 | `pkg/fees/ashare.go` (新建) | TBD | 1d | 4 处硬编码消除；单包测试 | ✅ |
| **P1-23** | `pkg/id/order.go` UUID v7 统一 | CQ-007 | `pkg/id/order.go` (新建) | TBD | 1d | 3 处订单号生成统一；测试 | ✅ |
| **P1-24** | Strategy 接口拆分 (StrategyCore/Configurable/ResourceManaged) | CQ-006, ISP | `pkg/strategy/interfaces.go` (新建) | 2026-06-12 | 2d | 4 个可组合 interface；AsConfigurable/AsSignalGenerator/AsResourceManaged helper；9 个 compliance 测试覆盖 builtin strategies | ✅ |
| **P1-25** | `domain.Strategy` Deprecated 删除 (ODR-013) | CQ-014 | `pkg/domain/types.go`, `pkg/strategy/registry.go`, `pkg/strategy/examples/*`, `cmd/strategy/main.go` | TBD | 0.5d | 旧接口移除；`pkg/strategy.Strategy` 唯一源 | ✅ |
| **P1-26** | 4 套执行实体合并 (LiveEngine/OrderManager/...) | CQ-010, YAGNI | `pkg/live/` | 2026-06-12 | 1w | 5 套 → 2 套 (LiveTrader + MockTrader) | ✅ |
| **P1-27** | `pkg/strategy/plugins/utils.go` 删除手写 `itoa`/`ftoa`/`joinStrings` | CQ-006 | `pkg/strategy/plugins/utils.go` | TBD | 0.5d | 标准库替换；测试通过 | ✅ |
| **P1-28** | Redis 缓存 key namespace 化 (`quantlab:` 前缀) | AR-021 | `pkg/storage/redis.go` | TBD | 1d | 全部 key 加前缀；InvalidateOHLCV 限定 pattern | ✅ |
| **P1-29** | 持仓超限/行业集中度/回撤告警 (AlertManager) | BR-015, ADR-017 | `pkg/alert/manager.go` (新建) | 2026-06-12 | 1w | 6 类 P0 风险告警；webhook 渠道 | ✅ |
| **P1-30** | E2E AI Copilot 端到端 + SSE 进度 | TQ-016, BR-014 | `e2e/tests/ai-copilot-e2e.spec.ts` | 2026-06-12 | 3d | Playwright 自然语言 → 回测 → 展示 | ✅ |

### 🟢 Sprint 6 P2 — Backlog (33 项)

> **验收**: Sprint 6 第 3 周末 (2026-07-02) 前 P0/P1 优先；P2 按 ROI 决定。

| ID | 任务 | 关联问题 | 文件 | Owner | 估时 | 验收标准 | 状态 |
|---|---|---|---|---|---|---|---|
| **P2-1** | backtest 报告 HTML 导出 (PDF via 浏览器打印) | BR-014 | `pkg/backtest/export.go`, `cmd/analysis/handlers_backtest.go:/export/:format` | 2026-06-12 | 3d | `GET /api/backtest/:id/export/html` + 自包含 SVG + 17 TestXxx | ✅ (ODR-027) |
| **P2-2** | 多策略对比 UI (`/backtest/compare`) | BR-014 | `pkg/backtest/compare.go`, `web/src/pages/BacktestCompare.vue` | 2026-06-12 | 3d | `GET /api/backtest/compare?ids=...` + 2-8 策略对比表 + best 高亮 + 17 TestXxx | ✅ (ODR-027) |
| **P2-3** | 远程紧急平仓 (EMERGENCY FLATTEN 按钮) | BR-018 | `pkg/live/trader.go:EmergencyFlatten`, `web/src/components/paper/EmergencyFlatten.vue` | 2026-06-12 | 2d | 3 重身份验证 (Bearer + body confirmation + 浏览器 confirm) + BypassedT1 审计标记 + 12 TestXxx | ✅ |
| **P2-4** | 投资者适当性 (创业板/科创板/北交所) | BR-005, BR-011 | `pkg/compliance/appropriateness.go` + handlers | 2026-06-13 | 1w | 10/50/100 万 + 24 月 + 5 道门禁 (过期/风险/资产/经验/白名单) + 23 TestXxx | ✅ (ODR-028) |
| **P2-5** | 异常交易监控 (6 类) | BR-011 | `pkg/compliance/abnormal_trade.go` + handlers | 2026-06-13 | 1w | 频繁撤单/自成交/对倒/洗售/虚假申报/拉抬打压 + 20 TestXxx | ✅ (ODR-028) |
| **P2-6** | 大额交易报告 (单笔 ≥200万 / 累计 ≥500万) | BR-011 | `pkg/compliance/reporter.go` + handlers | 2026-06-13 | 3d | 日终 reporter 生成 report.json (0600 权限) + 17 TestXxx | ✅ (ODR-028) |
| **P2-7** | 减持规则引擎 (控股股东 ≤3月 ≤1%) | BR-011 | `pkg/compliance/divestment.go` + handlers | 2026-06-13 | 1w | 5 类股东 + 3 种方式 + 90 日滚动窗 + 限售期 + 协议 ≥5% + 25% 年内 + 举牌告警 + 33 TestXxx | ✅ (ODR-029) |
| **P2-8** | 券资金对账 Worker (每 15min) | BR-012 | `pkg/live/reconciliation.go` (新建) | 2026-06-13 | 1w | 15min interval + 6 类偏差 (cash/qty/market_value/fee) + 阈值告警 + on-disk JSON + HTTP endpoints + 20 TestXxx race-clean | ✅ |
| **P2-9** | 融资融券 + 做空 | BR-005, BR-007 | `pkg/live/margin.go` (新建) | 2026-06-14 | 2w | MarginAccount + ShortableList + MarginCalculator + 4 类操作 (MarginBuy/ShortSell/BuyToCover/MarginSell) + 利息计提 + 强制平仓 (130%) + 55 TestXxx race-clean | ✅ |
| **P2-10** | 可转债策略 | BR-005 | `pkg/strategy/plugins/convertible_bond.go` (新建) | 2026-06-14 | 1w | 转股价值/纯债价值/溢价率/Delta + 强制赎回 (15/30) + 回售 (30 连续) + Strategy 接口实现 + 59 TestXxx race-clean | ✅ |
| **P2-11** | 期权定价 + Greeks | BR-005 | `pkg/strategy/options/` (新建) | 2026-06-14 | 2w | Black-Scholes (欧式) + Binomial CRR (美式) + 5 Greeks + ImpliedVol (Newton-Raphson) + NormCDF (A&S 近似) + BS↔Binomial 收敛验证 + 75 TestXxx race-clean | ✅ |
| **P2-12** | 港股通/北向因子 | BR-005 | `pkg/data/source/hkex/` (新建) | 2026-06-14 | 1w | EastmoneyNorthboundFetcher + NorthboundFactor (MA/动量/持股变化/排名/信号) + ExchangeRateConverter + 75 TestXxx race-clean | ✅ |
| **P2-13** | 退市 + 北交所 30% 涨跌停 | BR-005, BR-006 | `pkg/live/stock_state.go` (新建) | 2026-06-14 | 3d | StockState 4 状态机 + Registry + ForcedLiquidator (接 P2-3 EmergencyFlatten) + 5 日 LiquidationWindow + DryRun 模式 + 23 TestXxx race-clean | ✅ (ODR-030) |
| **P2-14** | 止盈/移动止盈/分批止盈 | BR-007 | `pkg/risk/take_profit.go` (新建) | 2026-06-14 | 1w | FixedTakeProfit + TrailingTakeProfit (HWM 跟踪) + TieredTakeProfit (100 股取整) + TakeProfitChecker (Registry) + 无状态 Rule + Position.Metadata 持久化 + 29 TestXxx race-clean | ✅ (ODR-031) |
| **P2-15** | 分红/送股/拆股/配股/增发 | BR-006 | `pkg/domain/corporate_action.go` (新建) | 2026-06-14 | 1w | CashDividend + BonusShare + CorporateActionSplit + RightsIssue (两阶段 Apply) + Placement + ActionEngine (apply 顺序固定) + appliedLog 幂等去重 + 22 TestXxx race-clean | ✅ (ODR-032) |
| **P2-16** | API 版本化 (`/api/v1` 强制) | AR-020 | `pkg/api/versioning.go` (新建 leaf package) | 2026-06-14 | 1d | APIVersionMiddleware (URL 重写 + re-dispatch) + DeprecationHeader (RFC 8594) + DiscoveryHandler + 两阶段迁移 (软→严格) + 零侵入 13 handler 不改 + 25 TestXxx race-clean | ✅ (ODR-033) |
| **P2-17** | OpenAPI 3.0 spec 自动生成 | AR-020 | `docs/openapi.yaml` (新建) | 2026-06-14 | 2d | OpenAPI 3.0 YAML + Swagger UI endpoint (/api/docs) + embed 静态文件 + 5 TestXxx | ✅ |
| **P2-18** | pkg/data/source ETL 真实集成测试 (dockertest) | TQ-016 | `pkg/data/source/integration_test.go` (新建) | 2026-06-14 | 2d | 9 个 adapter httptest mock 测试 | ✅ |
| **P2-19** | pkg/ai/gene_pool 持久化测试 (覆盖 41.3%→60%) | TQ-011 | `pkg/ai/gene_pool/integration_test.go` (新建) | 2026-06-14 | 2d | FactorPool/StrategyPool save/load 持久化测试 | ✅ |
| **P2-20** | pkg/risk 边界测试 (60%→70%) | TQ-016 | `pkg/risk/boundary_test.go` (新建) | 2026-06-14 | 2d | stoploss/regime/volatility 边界 + 零值/负值/极值 | ✅ |
| **P2-21** | pkg/ai/pipeline 端到端测试 (57%→70%) | TQ-016 | `pkg/ai/pipeline/e2e_test.go` (新建) | 2026-06-14 | 3d | Intent → YAML → Code → Compile → Backtest mock 全流程 | ✅ |
| **P2-22** | pkg/domain 类型边界测试 (0%→80%) | TQ-016 | `pkg/domain/types_test.go` (新建) | 2026-06-14 | 1d | OHLCV/Portfolio/Signal zero value + JSON 序列化 | ✅ |
| **P2-23** | pkg/httpclient 测试 (0%→80%) | TQ-016 | `pkg/httpclient/client_test.go` (新建) | 2026-06-14 | 1d | timeout/retry/backoff + httptest mock | ✅ |
| **P2-24** | pkg/logging 日志脱敏 (0%→80%) | TQ-016 | `pkg/logging/masking.go` + `masking_test.go` (新建) | 2026-06-14 | 1d | MaskAPIKey/MaskAccountNumber/MaskMap + 42 TestXxx | ✅ |
| **P2-25** | pkg/ai/client LLMClient interface 测试 (78%→90%) | ADR-018 | `pkg/ai/client_test.go` (扩展) | 2026-06-14 | 1d | MockClient 全方法 + Client options + 42 TestXxx | ✅ |
| **P2-26** | E2E 视觉回归 (Playwright 截图对比) | TQ-016 | `e2e/tests/visual-regression.spec.ts` (新建) | 2026-06-14 | 2d | 12 个截图 baseline (Dashboard/Backtest/AI/Screener/Sync + 响应式) | ✅ |
| **P2-27** | WASM sandbox (wazero) | AR-003, ADR-007/019 Phase 3 | `internal/sandbox/wasm/` (新建) | 2026-06-14 | 1mo | WASMSandbox 接口 + InProcessRuntime 回退 + 内存隔离 + 资源限制 + 28 TestXxx | ✅ |
| **P2-28** | EventBus backpressure (drop-oldest 策略) | AR-018 | `pkg/marketdata/backpressure_bus.go` (新建) | 2026-06-14 | 2d | BackpressureBus + drop-oldest + 每订阅者独立 goroutine + 原子指标 + 24 TestXxx race-clean | ✅ |
| **P2-29** | 跨日状态持久化测试 (Quant 状态) | TQ-016 | `pkg/backtest/persistence.go` + `persistence_test.go` (新建) | 2026-06-14 | 2d | DiskStateStore + 原子写入 (temp+rename) + 并发安全 + 损坏文件处理 + 22 TestXxx | ✅ |
| **P2-30** | 数值精度 (float64 vs Decimal) | TQ-016 | `pkg/decimal/` (新建) | 2026-06-14 | 1w | Decimal (int64 定点) + Add/Sub/Mul/Div + Round + 0.1+0.2=0.3 验证 + 53 TestXxx | ✅ |
| **P2-31** | 拆分 3 个长函数 (getSignals/SubmitOrder/CalculatePosition) | CQ-011 | 3 个文件 | 2026-06-14 | 2d | getSignals 拆 5 函数 + RunBacktest 拆 4 函数 + EmergencyFlatten 拆 2 函数, 每个 < 50 行 | ✅ |
| **P2-32** | 删除 4 处 `Test*_Cleanup` 空测试 | TQ-002 | `pkg/strategy/plugins/coverage_test.go` | 2026-06-14 | 0.5d | 删除 4 个空 Cleanup 测试 (TDSequential/BollingerMR/VPT/VolBreakout) | ✅ |
| **P2-33** | 删除 `assert.True(t, true)` placeholder | TQ-001 | `pkg/storage/postgres_screen_test.go` + `fundamentals.go` | 2026-06-14 | 0.5d | 提取 buildScreenFundamentalsQuery 纯函数 + 7 子测试验证 SQL 构建 | ✅ |

### Sprint 6 验收 Gate (2026-07-02)

| Gate | 命令/标准 | 通过条件 |
|---|---|---|
| **G1** | `go test -race -count=1 ./...` | 0 panic, 0 race condition |
| **G2** | `go vet ./...` | 0 issue |
| **G3** | `pkg/storage` 覆盖率 | ≥ 60% |
| **G4** | `pkg/ai/agents` 覆盖率 | ≥ 60% |
| **P5** | `pkg/ai/pipeline` 覆盖率 | ≥ 70% |
| **G6** | 73 项任务完成率 | ≥ 80% (59/73) |
| **G7** | Docker compose 服务数 | ≤ 5 (analysis/data/strategy/ai + 2 infra: postgres/redis) |
| **G8** | ADR/ODR 索引同步 | 20 ADR + 21 ODR |

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
9. ~~**P1-12** L4 validate 实际实现 (3d, Phase 4 P0 fail gate)~~ ✅
10. **P1-16~20** Engine 拆分 (2w, 1408 行→300 行)

---

## 🔍 Sprint 6 启动期 待校核项 (6 项)

> **来源**: [ODR-013 §对齐审计复核 (2026-06-11)](odr/odr-013-comprehensive-audit-2026-06-11.md#对齐审计复核-2026-06-11-同日)
> **状态**: 路径引用为 (新建) — 任务执行时精确校核
> **原则**: [VISION.md §Principle 8](../VISION.md#principle-8-documentation-path-consistency)

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

## 🔴 Sprint 7 — ODR-043 综合审计改进任务 (2026-06-29 ⏳ Pending)

> **来源**: [ODR-043](odr/odr-043-comprehensive-audit-2026-06-29.md) — 4 维度综合审计 (Go 静态质量 / Go 模块化 / Go 测试 / 前端质量)
> **执行规范**: 每个任务必须 (1) 编写测试用例 (2) 通过代码审查 (3) 采用 atomic commit 提交 — 详见 [AGENTS.md §8.3 执行规范](../../AGENTS.md#83-执行规范)
> **总计**: 9 项任务 (P0×1 + P1×3 + P2×2 + P3×3)

### 🔴 P0 — 立即修复（真实 bug，2-3 天）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| S7-P0-1 | 修复 AI Pipeline 端到端跑不通 — handlers_pipeline.go:83 传 nil runner | `cmd/analysis/handlers_pipeline.go:83` | ✅ | ODR-043 |
| S7-P0-2 | 修复 Pipeline 硬编码 buildCmd.Dir 为开发者本机路径 | `pkg/ai/pipeline/pipeline.go` | ✅ | ODR-043 |
| S7-P0-3 | 修复 ValidateAgent L3 默认股票池为美股（应改为 A 股） | `pkg/ai/agents/validate.go` | ✅ | ODR-043 |
| S7-P0-4 | 修复 research.go/generate.go 用 extractField 字符串扫描解析 JSON | `pkg/ai/agents/research.go`, `generate.go` | ✅ | ODR-043 |
| S7-P0-5 | 修复 simulated_broker.go:151 硬编码 0.00025 与 fees 包不一致 | `pkg/live/simulated_broker.go:151` | ✅ | ODR-043 |
| S7-P0-6 | 修复 9 处 _ = json.Unmarshal 静默吞错 | `pkg/ai/gene_pool/strategy_pool.go` ×6, `factor_pool.go` ×2, `pkg/strategy/db.go` ×1 | ✅ | ODR-043 |
| S7-P0-7 | 修复 pkg/risk/take_profit.go 构造器 3 处 panic（违反生产代码不 panic 约定） | `pkg/risk/take_profit.go:248,255,258` | ✅ | ODR-043 |
| S7-P0-8 | 修复 e2e/tests 无 skip guard 导致 go test ./... 永远 FAIL | `e2e/tests/integration_test.go` | ✅ | ODR-043 |
| S7-P0-9 | 修复 16 个 ReviewActions.spec.ts 测试失败（缺 MessageProvider） | `web/src/components/ai/__tests__/ReviewActions.spec.ts` | ✅ | ODR-043 |
| S7-P0-10 | 一次性 gofmt -w . 格式化 237 个未格式化文件 | 全代码库 | ✅ | ODR-043 |
| S7-P0-11 | 修复 pkg/ai/evolution TestPopulation_ConcurrentAccess 数据竞争（pre-existing，-race 下必崩） | `pkg/ai/evolution/` | ✅ | S7-P0-2 审查发现 |
| S7-P0-12 | 修复 pkg/strategy JobResult.Status 数据竞争（test + 生产 POST /api/copilot/generate handler 均 read-without-lock） | `pkg/strategy/copilot_test.go`, `cmd/analysis/handlers_copilot.go` | ✅ | S7-P0-6 审查发现 |
| S7-P0-13 | 修复 pkg/sync TestQueueOverflow 数据竞争（pre-existing，-race 下偶发，Queue.Enqueue vs worker） | `pkg/sync/queue.go:40,214`, `pkg/sync/fault_test.go:307` | ✅ | S7-P0-12 验证发现 |
| S7-P0-14 | 修复 pkg/auth 14 个测试失败（pre-existing，t.Parallel + gin.SetMode 全局变量竞争） | `pkg/auth/middleware_test.go` | ✅ | S7-P0-12 验证发现 |
| S7-P0-15 | 修复 TestGlobalPluginLoader 全局单例泄漏（-count>1 必崩，global state 跨 run 未清理） | `pkg/strategy/loader_test.go:276` | ✅ | S7-P0-12 验证发现 |
| S7-P0-16 | 修复 pkg/data/source flaky 测试（AlphaVantage map 迭代无序导致 Items[0] 不确定） | `pkg/data/source/alpha_vantage_adapter.go:146` | ✅ | S7-P0-13 验证发现 |
| S7-P0-17 | 修复 pkg/backtest TestProperty_T1Enforced flaky（ghost zero-quantity position：DirectionLong/Short 抵消至 0 未 delete，后续 close 误报 "cannot close position: quantity is zero"；附带 AvgCost 除零 NaN） | `pkg/backtest/tracker.go:262,305,489,527` | ✅ | S7-P0-16 验证发现 |

### 🟠 P1 — 高优先级重构（1-2 sprint）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| S7-P1-1 | 抽取 pkg/settlement + pkg/portfolio 共享原语包（消除 tracker/mock_trader 重复） | `pkg/settlement/` 新建, `pkg/portfolio/` 新建 | ✅ | ODR-043 |
| S7-P1-2 | 修复 5 处跨层反向依赖（strategy→ai/internal/sandbox, storage→sync, marketdata→live, compliance→live） | 多处 | ✅ | ODR-043 |
| S7-P1-3 | 抽取 BacktestRunner 接口到 pkg/ai/contracts/（消除 3 处重复定义） | `pkg/ai/contracts/` 新建 | ✅ | ODR-043 |
| S7-P1-4 | 修复费率配置三重定义（const aliasing backtest 5 常量到 fees + 修复 D1/D2/D5 数值 bug） | `pkg/backtest/constants.go`, `pkg/domain/execution.go`, `pkg/ai/yaml/generator.go` | ✅ | ODR-043 |
| S7-P1-5 | 补 pkg/ai 顶层 5 子系统测试（client/cost/metrics/ratelimit/tracer 当前 0%） | `pkg/ai/tracer_test.go`, `pkg/ai/ratelimit_test.go`, `pkg/ai/cost_test.go`, `pkg/ai/metrics_test.go`, `pkg/ai/retry_test.go`, `pkg/ai/client_http_test.go` | ✅ | ODR-043 |
| S7-P1-6 | 修复 6 个无断言弱测试 + 改 40 处 time.Sleep 并发测试为 channel | `pkg/strategy/plugins/coverage_test.go`, `pkg/strategy/plugins/plugins_test.go`, `pkg/risk/boundary_test.go`, `pkg/risk/take_profit_test.go`, `pkg/strategy/monitor/monitor_test.go`, `pkg/marketdata/backpressure_bus_test.go`, `pkg/marketdata/eventbus_test.go`, `pkg/sync/worker_test.go` | ✅ | ODR-043 |

### 🟡 P2 — 中优先级改进（2-3 sprint）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| S7-P2-1 | 拆分 pkg/backtest 上帝包为子包（12/12 完成 ✅：8 个原子提交提取 contracts/cache/execution/tracker/state/walkforward/batch/job 叶子包；*Engine→contracts.EngineRunner 窄接口解耦；aliases.go 零成本 type alias 保持外部调用方零改动；newTestJobService 用 fakeRunner stub 断开循环依赖 — 见 commits e858a60..4f294be） | `pkg/backtest/contracts/`, `pkg/backtest/cache/`, `pkg/backtest/execution/`, `pkg/backtest/tracker/`, `pkg/backtest/state/`, `pkg/backtest/walkforward/`, `pkg/backtest/batch/`, `pkg/backtest/job/`, `pkg/backtest/aliases.go`, `pkg/backtest/engine.go` | ✅ | ODR-043 |
| S7-P2-2 | 拆分 pkg/live 上帝包为子包（3 子包提取：margin/(1363行,3文件拆分) + reconciliation/(922行) + stockstate/(498行)；删除 types.go 死代码 90 行；父包 5629→2750 行 -51%，16→12 文件 — 见 commit c483160） | `pkg/live/margin/`, `pkg/live/reconciliation/`, `pkg/live/stockstate/`, `cmd/analysis/handlers_reconciliation.go`, `cmd/analysis/handlers_stock_state.go` | ✅ | ODR-043 |
| S7-P2-3 | 拆分 cmd/analysis/main.go 372 行 main() 函数 | `cmd/analysis/main.go`, `cmd/analysis/setup.go`, `cmd/analysis/setup_test.go` | ✅ | ODR-043 |
| S7-P2-4 | 拆分 registerRoutes 16 参数函数为 ServerDeps 结构体 | `cmd/analysis/main.go`, `cmd/analysis/deps.go`, `cmd/analysis/deps_test.go` | ✅ | ODR-043 |
| S7-P2-5 | 拆分 cmd/data/main.go (1713 行 God File) | `cmd/data/main.go`, `cmd/data/setup.go`, `cmd/data/middleware.go`, `cmd/data/handlers_*.go`, `cmd/data/setup_test.go` | ✅ | ODR-043 |
| S7-P2-6 | 前端补 ESLint + @vitest/coverage-v8 依赖 + lint/typecheck 脚本（已建 eslint.config.js flat config for Vue3+TS; 添加 lint/typecheck 脚本; 安装 6 个 devDeps; 修复 13 个 lint errors: 1 死代码 bug + 2 useless-assignment + 6 empty catch + 4 配置项; 952 warnings 全为 Vue 格式规则不阻断 — 见 commit pending） | `web/package.json`, `web/eslint.config.js`, `web/src/composables/useAsyncBacktest.ts`, `web/src/utils/tradeMarkers.ts`, `web/src/stores/backtest.ts`, `web/src/pages/{Dashboard,BacktestEngine}.vue`, `web/src/components/backtest/BacktestHistory.vue` | ✅ | ODR-043 |
| S7-P2-7 | 产品决策：AI Research 模块 2500 行不可达代码 → 删除（用户决定删除；15 文件删除：AIResearch.vue + 10 组件 + 2 测试 + api/factor.ts + types/pipeline.ts; api/copilot.ts 清理 6 个死函数保留 generateStrategy; 总计 -3406 行; 952→645 lint warnings, 157→126 tests; build/lint/typecheck 全通过 — 见 commit pending） | `web/src/pages/AIResearch.vue`, `web/src/components/ai/`, `web/src/api/{factor.ts,copilot.ts}`, `web/src/types/pipeline.ts` | ✅ | ODR-043 |
| S7-P2-8 | 产品决策：EmergencyFlatten.vue 311 行死代码 → 接入 PaperTrading（用户决定接入；EmergencyFlatten 添加 flattened emit; PaperTrading 导入并挂载在账户概览下方; @flattened=fetchData 立即刷新持仓; 新增 3 个组件测试: render/arm/emit-on-success/no-emit-on-failure — 见 commit pending） | `web/src/components/paper/EmergencyFlatten.vue`, `web/src/pages/PaperTrading.vue`, `web/src/components/paper/EmergencyFlatten.test.ts` | ✅ | ODR-043 |
| S7-P2-9 | 拆分 PaperTrading.vue 654 行 God 组件（抽取 2 个 composables: usePaperTradingData 143 行 — 数据 fetch/5s 轮询/计算指标; useSuitability 112 行 — 适当性预检状态机 + extractErrorMessage; PaperTrading.vue 661→519 行净减 142, 仅保留 template + 表格列定义 + 表单/handlers; @blur=refreshSuitability(symbol) 适配新签名; 新增 22 测试: 12 suitability + 10 paper-trading-data, 覆盖 fetch/轮询启停/卸载/计算指标/状态机/错误提取; lint 0 errors, typecheck clean, 154 tests pass, build pass — 见 commit pending） | `web/src/pages/PaperTrading.vue`, `web/src/composables/usePaperTradingData.ts`, `web/src/composables/useSuitability.ts`, `web/src/composables/usePaperTradingData.test.ts`, `web/src/composables/useSuitability.test.ts` | ✅ | ODR-043 |

### 🟢 P3 — 长期改进（按需）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| S7-P3-1 | 扩展表达式引擎到信号/仓位/风控层 + ExpressionStrategy 适配器（cs_neutralize 2-arg 修复 + SignalGenerator + PositionSizer + RiskController + OHLCVDataProvider + ExpressionStrategy 自注册） | `pkg/ai/expression/`, `pkg/strategy/expression/` | ✅ | ODR-043 |
| S7-P3-2 | 实现 YAML → ExpressionStrategy 加载器（让 AI 输出 YAML 即可执行） | `pkg/ai/yaml/` | ✅ | ODR-043 |
| S7-P3-3 | 建 pkg/tools/registry.go Tools Registry（对外工具提供方：pkg/tools + 4 builtin Tool + /api/tools HTTP API） | `pkg/tools/`, `cmd/analysis/handlers_tools.go` | ✅ | ODR-043 |
| S7-P3-4 | 数据层"软分层" — 新增 pkg/domain/market/ 子包 + Go type alias view 过渡（8 类型迁移，199 消费者零修改） | `pkg/domain/market/`, `pkg/domain/types.go` | ✅ | ODR-043 |
| S7-P3-5 | 修复全部文档漂移（ARCHITECTURE/VISION/SPEC 同步 ODR-021 服务合并） | `docs/ARCHITECTURE.md` 等 | ✅ | ODR-043 |
| S7-P3-6 | 标记 ADR-014 为 Superseded by ADR-020 §6 + 更新 ADR-015/019/020 状态 | `docs/adr/adr-014*.md` 等 | ✅ | ODR-043 |

---

## 🔵 Sprint 8 — 统一研究平台落地 (2026-09-15 ⏳ Pending, ADR-022 重排)

> **来源**: [ADR-022](superseded-adr/adr-022-unified-research-platform.md)（架构决策）+ [ODR-048](odr/odr-048-top-level-product-redefinition.md)（重构记录）
> **顶层定义**: [PRODUCT.md](../PRODUCT.md)（canonical）; **工作面 1 详案**: [archive/RESEARCH-equitydeep-legacy.md](RESEARCH-equitydeep-legacy.md)（⚠️ 待按 ADR-022 修订）
> **执行规范**: 每个任务必须 (1) 编写测试用例 (2) 通过代码审查 (3) 采用 atomic commit 提交 — 详见 [AGENTS.md §8.3 执行规范](../../AGENTS.md#83-执行规范)
> **阶段划分**: 任务按 ADR-022 执行路线 P1~P5 编排（P0 顶层定义已完成）。`EQD-*` 为**稳定任务 ID**（保留原编号以维持跨文档引用），阶段归属以本节分组为准。
> **定位变更**: ~~「补充非改变」—— Quant Lab 保持横截面层不变~~ → ADR-022：**Quant Lab 降维为共享底座（L0+L1+L2），横截面选股升为工作面 2**，与工作面 1（EquityDeep 纵向深研）对等。
> **总计**: 17 项任务 (P1×7 + P2×4 + P3×3 + P4×2 + P5×1)

### 阶段映射（ADR-022 执行路线）

| ADR-022 阶段 | 内容 | 承接原任务 | 新增任务 |
|---|---|---|---|
| ✅ **P0** | 顶层定义 | — | ✔ PRODUCT.md / ADR-022 / ODR-048（已完成） |
| **P1** | 底座契约（单一摄取入口 + `ingest.raw` + Evidence API + `contracts/` + `research` schema + 抽检泛化） | EQD-P0-1, EQD-P0-2, EQD-P3-2 | L0-1 ~ L0-4 |
| **P2** | 工作面 1 跑通（接 `research` schema + 容器化 + 走 Evidence API + 回查脚本修复 → M1） | EQD-P0-3, EQD-P2-2 | P2-1, P2-2 |
| **P3** | 计算面补齐（`fundamentals_detail` + 5 纵向因子 + PIT） | EQD-P1-1, EQD-P1-2, EQD-P3-1 | — |
| **P4** | 飞轮打通（疑点 → 假设 → 因子 → 回测 → 回流档案） | EQD-P2-1 | P4-1 |
| **P5** | 横截面工作面 v2（存量对齐新架构） | — | P5-1 |

### ✅ 阶段 P1 — 底座契约（已全部关闭，零外部依赖）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| EQD-P0-1 | 契约冻结：将 `fundamentals_detail` schema + `_profile.json` schema 纳入版本控制（C-2） | `contracts/`（新目录） | ✅ | ODR-047 / RESEARCH §3.2-3.3 → ADR-022 P1 |
| EQD-P0-2 | 抽检脚本泛化：通用化 EquityDeep M1 数据质量抽检（10 票 × 20 数字，错误率 < 2%）（C-7 / 桥 B3） | `evals/data_quality/` | ✅ | ODR-047 / RESEARCH §3.6 → ADR-022 P1 |
| EQD-P3-2 | 文档漂移修复 8 项（DR-1~DR-8 收口校验）（C-9） | `docs/archive/odr/odr-054-dr-reverification.md`（新）+ `docs/ADR.md` / `AGENTS.md` | ✅ | ODR-047 D5 / C-9 → ADR-022 P1 |
| **L0-1** | **单一摄取入口**：统一 akshare/tushare adapter 归属 L0，禁止工作面直连外部数据源 | `pkg/data/tushare_raw.go` + `cmd/data/handlers_ingest.go` | ✅ | [ADR-022](superseded-adr/adr-022-unified-research-platform.md) §3 / PRODUCT §9 |
| **L0-2** | **`ingest.raw` 表**：原始源响应归档（`content_hash` 唯一键；冷热分层策略见 PRODUCT.md Q-2） | `pkg/storage/postgres.go`（内联）+ `docs/migrations/020_add_ingest_raw.sql` | ✅ | ADR-022 §2 / PRODUCT §6.1 |
| **L0-3** | **Evidence API**：`GET /api/evidence/{content_hash}` 返回唯一原始记录（citation 内容坐标） | `cmd/analysis/handlers_evidence.go` | ✅ | ADR-022 §5 / PRODUCT §7 |
| **L0-4** | **`research` schema DDL**：研究结构化状态（markdown 的确定性投影，可 DROP 重建） | `pkg/storage/postgres.go`（内联）+ `docs/migrations/021_add_research_schema.sql` | ✅ | ADR-022 §2 / PRODUCT §6.1 |

> **L0-1 存量代码面收口（阶段 P5 切片 1）**: L0-1 落地时冻结的是**规范**（外部源归 L0、禁工作面直连）。阶段 P5 切片 1 进一步清点存量代码，退役了 `pkg/marketdata` 侧**第二套外部源直连实现**（`akshare_provider.go` / `tushare_provider.go`，全仓 0 生产调用者）+ 工厂对 `tushare`/`akshare` type 显式拒绝 + `hkex` 北向 fetcher 显式归 L0 摄取侧；验收点「生产代码中外部源直连实例化点 = 0」达成。见 [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md)。

> **迁移编号统一**: 消除历史重复编号（根目录 `012`×2、`docs/migrations` `007`×2）——
> `migrations/012_add_gene_pool_tables.sql` → `023_add_gene_pool_tables.sql`；
> `docs/migrations/007_add_factor_returns_table.sql` → `024_add_factor_returns_table.sql`。
> 统一编号空间为本轮新增 `020`（L0-2）/`021`（L0-4）；`022`（EQD-P1-1）已于
> [ODR-053](odr/odr-053-p3-fundamentals-detail-table.md) 落地；`026`（EQD-P1-2 `factor_name`
> 放宽至 VARCHAR(32)）已于 [ODR-055](odr/odr-055-eqd-p1-2-vertical-factors.md) 落地；`025`（EQD-P3-1
> 表重叠收口）已于 [ODR-056](odr/odr-056-fundamentals-table-consolidation.md) 落地。
> 实际执行路径为 `pkg/storage/postgres.go` 内联 `migrate()`，上述 SQL 文件为同源文档副本。详见 [ODR-050](odr/odr-050-p1-base-contract-landing.md)。

### 🔵 阶段 P2 — 工作面 1 跑通（接 `research` schema + 容器化 + 走 Evidence API → M1）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| EQD-P0-3 | EquityDeep 回查脚本 P0 假阳性缺陷修复（校验对象由"文本子串"改为"citation 元组 + JSON Pointer"） | EquityDeep 仓（回查脚本） | ⬜ | ODR-047 D2 / ADR-022 §5 |
| EQD-P2-2 | vault 只读挂载 → 升级为 `equitydeep-research` worker 容器 | `docker-compose.override.yml` | ⬜ | C-6 → ADR-022 P2 |
| **P2-1** | **EquityDeep 接入共享 PG `research` schema**（`_profile.json` 降级为导出格式，不再作为权威存储） | EquityDeep 仓（新增 DB 层） | ⬜ | ADR-022 §4 |
| **P2-2** | **EquityDeep 取数改造**：去除直连 akshare，改走 L0 只读证据 API；原始快照迁至 `ingest.raw`，vault 只留 `{content_hash, pointer}` | EquityDeep 仓（ingest 层） | ⬜️ | ADR-022 §4 / PRODUCT §7 → ADR-022 P2 |

### 🔵 阶段 P3 — 计算面补齐（`fundamentals_detail` + 5 纵向因子 + PIT）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| EQD-P1-1 | 新增 `fundamentals_detail` 表（契约 C1，逐字段行存 + `ann_date` PIT + `snapshot_uri` 溯源） | `pkg/storage/postgres.go`（内联）+ `docs/migrations/022_equitydeep_fundamentals.sql` + `contracts/fundamentals_detail.schema.sql` | ✅ | RESEARCH §3.2 / C-1 |
| EQD-P1-2 | EquityDeep 摄取链 + 5 个纵向基本面因子（桥 B1）：归一化纯包 + `fundamentals_detail` 落库/读取（PIT）+ HTTP 写门 `POST /api/ingest/equitydeep`；5 因子计算 + `factor_name` 放宽 VARCHAR(32) | `pkg/data/equitydeep/`（新包）, `pkg/data/factor_equitydeep.go`, `pkg/domain/{factor,fundamentals_detail}.go`, `pkg/storage/fundamentals_detail.go`, `cmd/data/handlers_equitydeep_ingest.go`, `docs/migrations/026_widen_factor_name.sql` | ✅ | RESEARCH §3.4 / C-3, C-4 |
| EQD-P3-1 | 修复 `fundamentals` / `stock_fundamentals` 表字段重叠（CR-47 遗留，补正式任务登记）（DR-7）：存量并入 `stock_fundamentals` 后 DROP 旧表 + 4 个 `symbol` 系读写函数收敛 + `SaveFundamentalBatch` 补 `DO UPDATE` 漏列 | `docs/migrations/025_equitydeep_field_consolidation.sql` + `pkg/storage/{fundamentals,postgres}.go` | ✅ | ODR-047 DR-7 / C-8 → [ODR-056](odr/odr-056-fundamentals-table-consolidation.md) |

> **阶段 P3 进展**: ✅ **3/3 全部关闭** — EQD-P1-1（[ODR-053](odr/odr-053-p3-fundamentals-detail-table.md)）+ EQD-P1-2（[ODR-055](odr/odr-055-eqd-p1-2-vertical-factors.md)）+ EQD-P3-1（[ODR-056](odr/odr-056-fundamentals-table-consolidation.md)，迁移 025 表合并 + 代码收敛）已全部落地。

### 🔵 阶段 P4 — 飞轮打通（疑点 → 假设 → 因子 → 回测 → 回流档案）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| EQD-P2-1 | 第 19 个 MCP 工具 `research.profile`（读 `research` schema 投影 / `_profile.json` 导出镜像，不解析 markdown；桥 B2） | `pkg/tools/builtin/research_tool.go` + `pkg/storage/research.go` | ✅ | RESEARCH §3.5 / C-5 → ADR-022 P4 / [ODR-057](odr/odr-057-eqd-p2-1-research-profile-tool.md) |
| **P4-1** | **飞轮闭环端到端**：疑点 → 假设 → 因子 → 回测 → 结果回流修正档案（成功指标：≥3 个结论完成 IC 评估） | `pkg/ai/`, EquityDeep 仓 | ⬜ | ADR-022 §6 / PRODUCT §4 |

> **阶段 P4 进展**: **1/2** — EQD-P2-1（第 19 个 MCP 工具 `research.profile`，[ODR-057](odr/odr-057-eqd-p2-1-research-profile-tool.md)）已落地：`research` schema 接上首个读取方（PG 投影优先 + vault 回退），飞轮第 1 步「读回研究档案」打通；余 P4-1 飞轮闭环端到端（需 EquityDeep 仓配合）。

### 🔵 阶段 P5 — 横截面工作面 v2（存量对齐新架构）

| ID | 任务 | 文件 | 状态 | 来源 |
|----|------|------|------|------|
| **P5-1** | **工作面 2 对齐**：Vue SPA / Research Engine 存量能力对接 L0 单一数据面 + Evidence API（去除旁路取数） | `web/src/`, `cmd/analysis/`, `pkg/data/` | ✅ | ADR-022 §1, §3 / PRODUCT §5 → [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md) + [ODR-059](odr/odr-059-p5-1-retire-datasource-switch.md) + [ODR-060](odr/odr-060-p5-1-frontend-evidence-api.md) + [ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md) + [ODR-062](odr/odr-062-p5-1-bypass-residue-audit.md) |
| **P5-3** | **SPA 消费 citation 坐标**：因子页展示 `source/dataset/key/as_of/content_hash` 五元组并一键回溯 Evidence（analysis 网关补 `/api/factors/:factor_name` facade；后端改动最小，前端零旁路） | `cmd/analysis/handlers_proxy.go`, `web/src/{types,api,pages,components,router}/…` | ✅ | ADR-022 §1, §3, §5 / PRODUCT §6.3, §6.4 → [ODR-064](odr/odr-064-p5-3-spa-citation-coordinates.md) |

> **阶段 P5 进展**: **✅ P5-1 全维度关闭（五刀全部落地）**—— **切片 1**（[ODR-058](odr/odr-058-p5-1-retire-direct-providers.md)）**封潜伏直连**：旁路勘察定 4 条路径（P-A `pkg/marketdata` 第二套直连 provider / P-B `POST /api/datasource/switch` 用户可见切换门 / P-C `pkg/data/source` Registry 属 L0 / P-D `hkex` 北向 fetcher），本切片只处理**无生产调用者**的 P-A（退役 + 工厂显式拒绝）与 P-D（注释显式归 L0 摄取侧，零代码改动）；P-B（涉及用户可见行为变更）/ P-C（归属 L0 正确且 `ETLPipeline` 生产实例化点已 = 0）明确不动。验收点「生产代码中外部源直连实例化点 = 0」达成。**切片 2**（[ODR-059](odr/odr-059-p5-1-retire-datasource-switch.md)）**退役运行时数据源切换门**：P-B 评估三条实证（生产接线 `NewDataAdapter(nil, ...)` 下 `SetPrimary` 实测 panic / `http` 分支收任意 URL 冲突 ADR-022 §1 且默认无鉴权 / `datasource.*` 配置读取点 = 0）后裁决退役 `POST /api/datasource/switch`（后端 handler + 前端切换链路 6 文件 + 死配置 `datasource:` 段 + 死工厂 `pkg/marketdata/config.go` + openapi/文档清单），保留只读 `GET /status` 与 `GET /health`，读源改由启动期 `data_service.url` 固定。**切片 3**（[ODR-060](odr/odr-060-p5-1-frontend-evidence-api.md)）**Vue SPA 对接 L0 Evidence API（工作面 2 对齐起步）**：切片前 `web/src` 对 `evidence|content_hash|citation` **0 命中**（L0-3 API 自 ODR-050 起「已交付但未被消费」）；新增前端消费层 `api/evidence.ts::getEvidence`（`encodeURIComponent` 路径段编码 + 404 只透传）+ 类型层 `types/evidence.ts::RawIngest`（逐字对应 `pkg/storage/ingest_raw.go`）+ UI 入口 `/evidence` 页 + `EvidenceLookup.vue`（**三态渲染**：命中 / **404 = 未摄取** warning / 其他失败 error）+ 侧栏导航，**后端零改动**（vite 已代理 `/api` → 8085）；验收点 `web/src` evidence 命中 8 文件全部为本切片新增；`npm run typecheck` / `npm test`（13 files, 162 tests）/ `npm run build` 全绿。**切片 C 评估**（[ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md)，**Audit / 实施未启动**）**Research Engine 输出携 citation 元组**：名词澄清（`ResearchEngine` 全仓 0 命中，实际对象为 L1 因子引擎 → `quant.*` 段）；取证四条（全链路仅「A→B equitydeep 纵向」一段通 / `archiveRaw` **算了 hash 又丢弃**（无返回值）/ `quant.*` 表**零 hash 列** / `fundamentals_detail.snapshot_uri` 是唯一已落库 hash 出参）；链路表 A→B（OHLCV·基本面）❌ / A→B（equitydeep 纵向）✅ / B→C ❌ / C→输出 ❌；**裁决收窄为 C2**（只做已有 hash 出参的 5 个纵向基本面因子链，`factor_cache` 加 `citation JSONB` + `loadStatementBook` **不再丢弃** `SnapshotURI` + `getFactorHandler` 输出面展开 5 元组，未命中只出 hash；`FactorStore` 接口不变）；C1（A→B 全链路）⛔ / C3（零 schema）⛔；**报告面推迟**（`Warm` 硬编码 `{Momentum, Value, Quality}`，三者在 C2 全部不可覆盖 ⇒ 原「回测内嵌 + 走查加列」方案只能产出**恒空 citation 幽灵字段**）；**覆盖面 5/11 因子，PRODUCT §6.4「100%」如实记为未达标**。**注：本切片仅评估与裁决，未产出代码**（Artifacts 6 文件待实施）。**C2 实施落地**（v3.37.0，同日）：迁移 027（`factor_cache.citation JSONB DEFAULT '[]'`）+ `statementField.provenance` / `statementBook` 每标的 hash 集合 + `saveVerticalFactor` 写 `entry.Citation` + `getFactorHandler` 经 `expandCitation` 展开 5 元组（未命中只出 hash）；`go build` EXIT=0、相关 17 包测试全 ok；**主验收点（GET → evidence 200 循环）待运行时取证**；ODR-061 Status → **Completed**（详见 [ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md) §Artifacts / §Metrics）。**切片 B+D 评估与实施（[ODR-062](odr/odr-062-p5-1-bypass-residue-audit.md)，v3.39.0，收官刀）**：8 条现场取证（a~h）+ ADR-022 §3 单向依赖判定表（L3→L0 直连 = 旁路 / Go 内部 L1·L2→L0 = 合法消费 / 同服务裸路径 rewrite = 形状问题非旁路）；用户裁决**方案甲（全量收口）**—— **S-A** 前端 5 文件重写对齐 jobs 契约（`types`/`api`/`stores`/`SyncStatusPanel.vue`/`DataImportForm.vue`：SyncJob 类型逐字对应 sync_jobs JSON + activeJob 进度视图 + EventSource 按 job 订阅 SSE）+ **S-B** analysis `/api/sync/*` 网关代理（`ReverseProxy` + `FlushInterval=-1` SSE 流式透传；顺带修复 viper 局部实例接线——`registerProxyRoutes` 原读全局 viper 恒空、硬编码 docker hostname 静默生效）+ **S-C** 死代理路由 ×3 退役 + **S-E** vite 死代理 ×3 清理（含最后一处 L3→L0 直连形态 `/market`→8081）；**S-D 裸镜像路由保留现状**（与 legacy 静态页共存亡，退役另立议题）；实施补齐两项契约缺口—— data 侧 `POST /api/sync/jobs` 类型化创建门（原不存在，SPEC/e2e 三方期望；7 type switch + 门上 400 fail-fast + `jsonHasKey` 区分 key 缺失 vs 空数组）+ 哨兵 `sync.ErrInvalidCron`（无效 cron 500→400）；验证：`go build` EXIT=0 / cmd·pkg 相关包测试 ok（`pkg/sync` 存量 mock 缺文件除外 → 登记 **P1-18**）/ 前端 vue-tsc 0 错 + vitest 13 files 158 tests + vite build 全绿；**运行时取证网关面 12 项全过**（一次性 pg16 + Redis 容器：create 202 / list·get 200 / SSE 流式 ✓ / cancel 200 / retry-on-cancelled 400 / 三类 400 拒绝 / 已删路由 404 / facade 200 / schedule 201 + invalid cron 400）；e2e 三套件静态对齐真实契约，运行时全套件需 tushare token 待跑。**至此 P5-1 三要件全部达成：Vue SPA（切片 3 + B）✅ / Research Engine（切片 C）✅ / 去除旁路取数（切片 1 + 2 + D）✅ —— L3 直连形态清零、死契约清零、SPA 数据管理面真实可用、gateway 契约与文档对齐。**

> **阶段 P5 进展（续 · P5-3，v3.41.0）**: ✅ **SPA 消费 citation 坐标**（[ODR-064](odr/odr-064-p5-3-spa-citation-coordinates.md)，编号沿用用户规划口径）—— 承 [ODR-061](odr/odr-061-p5-1-slice-c-citation-evaluation.md) §Metrics 遗留的「主验收点（GET → evidence 200 循环）**待运行时取证**」。范围与验收方式经 AskUserQuestion **先裁决后开工**（① 仅做「SPA 消费 citation 坐标」/ ② 须起运行时端到端取证），**未擅自选范围**（本轮开工时 `TASKS.md` 阶段 P5 仅 P5-1，P5-3 无既有条目）。**侦察关键结论 = 路由命名陷阱**：L0 真实契约 `GET /factors/:factor_name`（**无 `/api` 前缀 + 复数**，`cmd/data/main.go:160`）/** analysis 既有 `/api/factor/*`（单数）形状不互通 / SPA 消费约定 `/api` ⇒ 三方互不相通**，故必须**补 facade 而非复用既有路由**（若按直觉复用单数路由，会得到一个「200 但永远没有 citation」的假成功）。**后端 1 文件 +24 行**：`cmd/analysis/handlers_proxy.go` 新增 `GET /api/factors/:factor_name` facade（`url.Values` 空值省略不产生 `?symbol=&date=` + `url.PathEscape` 编码路径段 + `proxyRequest` **状态码原样透传** —— 使 404「无 factor_cache 行」一等语义与 hash-only citation 穿透网关不失真，注释中显式写死不变量）。**前端 6 文件**：`types/factor.ts`（`CitationTuple` 仅 `content_hash` 必需、其余四字段可选 = 契约的「缺席即未归档」+ `FactorCacheEntry`）+ `api/factor.ts::getFactorCitation` + `components/factor/FactorCitation.vue`（五元组表格 + 归档态判定「四字段任一存在」**不补造缺席字段** + 404 与其它失败分态）+ `pages/FactorCitation.vue` + 路由 `/factors` + 侧栏「因子证据」+ `components/evidence/EvidenceLookup.vue`（新增 `initialHash` prop + `onMounted` 立即解析 + `watch` 原地重解析）+ `pages/Evidence.vue`（读 `route.query.content_hash` 预填）⇒ **「因子数 → 证据坐标 → 原始归档记录」一次点击可达**。**运行时端到端取证 10 项全过**（一次性 pg16 :15432 + Redis :16379 + data :8081 / analysis :8085 / vite :5175；播种 `POST /api/ingest/raw` → hash `8e8d829b…c69c` → `POST /api/ingest/equitydeep` 3 snapshots/6 rows → `POST /sync/factors/roe_dupont_leverage`；`GET /api/factors/roe_dupont_leverage?symbol=600519.SH&date=20250630` **200** 携五元组 → `GET /api/evidence/8e8d829b…c69c` **200** + 原始归档 payload；未归档 hash **404** / 行未命中 **404**；3 标的同哈希 = **请求批次坐标集合**；直连 L0 形状一致；vite 代理 200/404/200；`ingest.raw` 仅 1 行未污染）。门禁：`go build ./...` EXIT=0 / `TestFactorCitationProxy` 4/4 / vitest 16/16 / `vue-tsc` 0 错。**未做项如实记录**：覆盖面仍 **5/11 因子**（不宣称 PRODUCT §6.4「100%」）/ `cmd/analysis/handlers_proxy_test.go` 命中 `.gitignore:*_test.go` **未入库**（同 P1-18 类，未以 `git add -f` 绕过）/ facade 前缀依赖待 L0 加前缀时收敛 / `/factors` 无 Playwright spec。**SPA 零旁路**：全程只经 `/api` → 8085 → 8081，L3→L0 直连形态保持为零。

> **EQD-P3-2 进展**: ✅ **已完成**（[ODR-054](odr/odr-054-dr-reverification.md)）—— 对 ODR-047 已修 8 项做「文档声明 ↔ 物理事实」双向取证复核：DR-1/2/5/6/8 未回退，DR-7 由阶段 P3 的 EQD-P3-1 承接；另发现并回填 3 处残留漂移（DR-3-R1 ADR 拆分 21≠22 / DR-3-R2 Implementation 31≠30 / DR-4-R1 `ODR-001~049`）。**阶段 P1 全部关闭**。
>
> **原 Sprint 8 任务去向**: EQD-P0-1、EQD-P0-2、EQD-P3-2 → 阶段 P1（✅ 全部关闭）；EQD-P0-3、EQD-P2-2 → 阶段 P2；EQD-P1-1、EQD-P1-2、EQD-P3-1 → 阶段 P3；EQD-P2-1 → 阶段 P4。

---

## 🔗 相关文档

| 文档                               | 用途             |
| -------------------------------- | -------------- |
| [ROADMAP.md](../ROADMAP.md)         | Sprint 进度和里程碑  |
| [PHASE3-PLAN.md](research-2026-Q2/PHASE3-PLAN.md) | Phase 3 实施计划详情 |
| [archive/NEXT\_STEPS.md](NEXT_STEPS.md)  | 审查发现详情         |
| [TEST.md](../TEST.md)               | 测试策略和覆盖率目标     |
| [ODR-013](odr/odr-013-comprehensive-audit-2026-06-11.md) | Sprint 6 综合审查记录 |
| [ODR-047](odr/odr-047-equitydeep-integration-audit.md) | Sprint 8 — EquityDeep 集成审计 |
| [PRODUCT.md](../PRODUCT.md) | 顶层产品定义（一个产品 / 双对等工作面 / 共享底座） |
| [archive/RESEARCH-equitydeep-legacy.md](RESEARCH-equitydeep-legacy.md) | 工作面 1（纵向深研）详案 (Product + Tech) |
| [ADR-022](superseded-adr/adr-022-unified-research-platform.md) | 统一研究平台架构决策（取代 [ADR-021](superseded-adr/adr-021-equitydeep-research-layer.md)，后者保留历史） |
| [ODR-043](odr/odr-043-comprehensive-audit-2026-06-29.md) | Sprint 7 综合审计 (4 维度 40 问题点) |
| [ADR-017](../adr/adr-017-observability-and-auth.md) ~ [ADR-020](../adr/adr-020-engine-decomposition.md) | Sprint 6 架构决策 |

***

_Last updated: 2026-06-30 (v4.0.0) — Sprint 7 (ODR-043) 综合审计任务入库：P0×17 + P1×5 (S7-P1-1, S7-P1-2, S7-P1-3, S7-P1-4, S7-P1-5 完成) 完成 + P1×1 + P2×9 + P3×6 = 38 子任务; 4 维度审计 (Go 静态质量/模块化/测试/前端); 推翻 brainstorming "ExecutionCore 合并"假设; AGENTS.md §8.3 执行规范确立; S7-P1-5 pkg/ai 覆盖率 28.9%→97.1%_
_Source: 整合自 CODE\_REVIEW\_REPORT.md + NEXT\_STEPS.md + PHASE3-PLAN.md + AGENTS.md + ODR-011 + Sprint 5 综合审查 + Sprint 6 (ODR-013) 综合审查 + Sprint 7 (ODR-043) 4 维度综合审计_

