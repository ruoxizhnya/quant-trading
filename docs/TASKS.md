# Quant Lab — 统一任务追踪

> **Status**: Active (Long-Live Task Tracker)
> **Version:** 2.0.0
> **Last Updated:** 2026-04-11
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [ROADMAP.md](ROADMAP.md) (sprint progress), [PHASE3-PLAN.md](PHASE3-PLAN.md) (implementation details)
>
> **Purpose**: 本文件是 Quant Lab 项目的**单一任务追踪源**。所有可执行任务必须在此记录，不得散落在其他文档中。
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
| P1-1 | 提升 `pkg/data` 测试覆盖率               | 26.7% → 70%+ | 🔵 | AGENTS.md            |
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
| D1-1  | 实现 DataEventBus (pub/sub)                                  | `pkg/marketdata/eventbus.go`          | ⬜  | 0.5d |
| D1-2  | 增强 Provider 接口 (Name/CheckConnectivity/GetTradingCalendar) | `pkg/marketdata/provider.go`          | ⬜  | 0.5d |
| D1-3  | TushareProvider 重构                                         | `pkg/marketdata/tushare_provider.go`  | ⬜  | 0.5d |
| D1-4  | PostgresProvider 新增 (零网络延迟)                                | `pkg/marketdata/postgres_provider.go` | ✅  | 1d   |
| D1-5  | AkShareProvider 新增 (免费备选)                                  | `pkg/marketdata/akshare_provider.go`  | ⬜  | 0.5d |
| D1-6  | HttpProvider 新增 (通用 HTTP 适配)                               | `pkg/marketdata/http_provider.go`     | ⬜  | 0.5d |
| D1-7  | CachedProvider 装饰器 (Redis 缓存)                              | `pkg/marketdata/cached_provider.go`   | ⬜  | 0.5d |
| D1-8  | DataAdapter 实现 (整合三层)                                      | `pkg/marketdata/adapter.go`           | ✅  | 1d   |
| D1-9  | Engine 集成 DataAdapter                                      | `pkg/backtest/engine.go`              | ✅  | 0.5d |
| D1-10 | Config + API (数据源切换)                                       | `config.yaml`, API handlers           | ⬜  | 0.5d |

**D1 验收标准**:

- [ ] 切换到 postgres provider 后，500股回测 < 5s
- [ ] Tushare 不可用时自动 fallback 到 akshare
- [ ] 所有 55+ 现有测试通过
- [ ] API 可以在运行时切换数据源

### D2: 批量回测框架 (Week 2-3)

| ID   | 任务                           | 文件                             | 状态 | 预估 |
| ---- | ---------------------------- | ------------------------------ | -- | -- |
| D2-1 | 类型定义 (BatchTask/BatchResult) | `pkg/backtest/batch.go`        | ⬜  | —  |
| D2-2 | CSV 任务解析                     | `pkg/backtest/batch_csv.go`    | ⬜  | —  |
| D2-3 | BatchEngine (goroutine pool) | `pkg/backtest/batch.go`        | ⬜  | —  |
| D2-4 | Scorer (评级 + OverfitScore)   | `pkg/backtest/batch_scorer.go` | ⬜  | —  |
| D2-5 | Walk-Forward 集成              | `pkg/backtest/batch.go`        | ⬜  | —  |
| D2-6 | 汇总报告生成                       | `pkg/backtest/batch.go`        | ⬜  | —  |
| D2-7 | API 端点                       | `cmd/analysis/main.go`         | ⬜  | —  |

**D2 验收标准**:

- [ ] 100 任务 (10股票×4策略×区间池) < 30s 完成
- [ ] 输出含评级 + OverfitScore + StabilityScore
- [ ] CSV 兼容金策格式 + 我们的扩展格式

### D3: Go Plugin 策略热加载 (Week 3-4)

| ID   | 任务                     | 文件                       | 状态 | 预估 |
| ---- | ---------------------- | ------------------------ | -- | -- |
| D3-1 | PluginLoader 实现        | `pkg/strategy/loader.go` | ⬜  | —  |
| D3-2 | Load/Unload/Reload API | `pkg/strategy/loader.go` | ⬜  | —  |
| D3-3 | 示例插件                   | `pkg/strategy/plugins/`  | ⬜  | —  |
| D3-4 | API 端点                 | `cmd/analysis/main.go`   | ⬜  | —  |
| D3-5 | 文档更新                   | `docs/`                  | ⬜  | —  |

**D3 验收标准**:

- [ ] 动态加载 .so 策略，立即生效
- [ ] Reload 后新代码生效
- [ ] 提供 Makefile 一键编译插件

### D4: 实盘交易接口预留 (Week 4)

| ID   | 任务              | 文件                        | 状态 | 预估 |
| ---- | --------------- | ------------------------- | -- | -- |
| D4-1 | LiveTrader 接口定义 | `pkg/live/trader.go`      | ⬜  | —  |
| D4-2 | MockTrader 实现   | `pkg/live/mock_trader.go` | ⬜  | —  |
| D4-3 | Engine 预留实盘接口   | `pkg/backtest/engine.go`  | ⬜  | —  |
| D4-4 | 文档更新            | `docs/`                   | ⬜  | —  |

**D4 验收标准**:

- [ ] LiveTrader 接口编译通过
- [ ] MockTrader 可用于测试/Paper Trading
- [ ] 文档清楚描述接入规范

### D5: 更多实战策略插件 (Week 5-6)

| ID   | 任务                       | 描述                               | 状态 | 预估   |
| ---- | ------------------------ | -------------------------------- | -- | ---- |
| D5-1 | TD Sequential (神奇九转)     | 价格序列模式计数                         | ⬜  | ≤ 3s |
| D5-2 | Bollinger Mean Reversion | BB位置 + RSI                       | ⬜  | ≤ 3s |
| D5-3 | Volume-Price Trend       | 量价配合度 + MA共振                     | ⬜  | ≤ 3s |
| D5-4 | Volatility Breakout      | ATR突破 + 方向过滤                     | ⬜  | ≤ 3s |
| D5-5 | 单元测试 (每个策略 ≥ 3 个)        | `pkg/strategy/plugins/*_test.go` | ⬜  | —    |

**D5 验收标准**:

- [ ] 4 个新策略注册到 GlobalRegistry
- [ ] 每个策略 ≥ 3 个单元测试
- [ ] FactorCache 加速生效

### D6: AI Copilot 深度集成 (Week 6-7)

| ID   | 任务           | 描述             | 状态 | 预估 |
| ---- | ------------ | -------------- | -- | -- |
| D6-1 | LLM 意图解析     | 中文自然语言 → 策略参数  | ⬜  | —  |
| D6-2 | YAML 生成      | 参数 → YAML 配置   | ⬜  | —  |
| D6-3 | Pipeline 集成  | 解析 → 编译验证 → 回测 | ⬜  | —  |
| D6-4 | Dashboard 集成 | 前端 UI 更新       | ⬜  | —  |

**D6 验收标准**:

- [ ] 中文描述 → 30s 内得到回测结果
- [ ] ≥ 5 种策略描述正确解析

***

## �📊 统计

| 优先级             | 待处理    | 进行中   | 已完成    | 已阻塞   | 已取消   | 总计     |
| --------------- | ------ | ----- | ------ | ----- | ----- | ------ |
| P0              | 0      | 0     | 6      | 0     | 0     | 6      |
| P1              | 2      | 1     | 14     | 0     | 0     | 17     |
| P2              | 1      | 0     | 17     | 0     | 0     | 18     |
| P3              | 3      | 0     | 15     | 1     | 0     | 19     |
| Phase 3 (D1-D6) | 25     | 0     | 3      | 0     | 0     | 28     |
| **总计**          | **31** | **1** | **55** | **1** | **0** | **88** |

***

## 📝 任务变更日志

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

## 🔗 相关文档

| 文档                               | 用途             |
| -------------------------------- | -------------- |
| [ROADMAP.md](ROADMAP.md)         | Sprint 进度和里程碑  |
| [PHASE3-PLAN.md](PHASE3-PLAN.md) | Phase 3 实施计划详情 |
| [NEXT\_STEPS.md](NEXT_STEPS.md)  | 审查发现详情         |
| [TEST.md](TEST.md)               | 测试策略和覆盖率目标     |

***

_Last updated: 2026-04-12_
_Source: 整合自 CODE\_REVIEW\_REPORT.md + NEXT\_STEPS.md + PHASE3-PLAN.md + AGENTS.md_
