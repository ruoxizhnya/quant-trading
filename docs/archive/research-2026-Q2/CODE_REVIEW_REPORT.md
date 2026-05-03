# Quant Lab 全面代码审查报告

> **审查日期**: 2026-04-10
> **审查范围**: Go 后端 (`cmd/`, `pkg/`)、Vue 3 前端 (`web/src/`)、E2E 测试 (`e2e/`)、文档 (`docs/`)、基础设施 (`docker-compose.yml`, `config/`)
> **审查维度**: 代码质量、测试覆盖、架构设计、文档一致性

---

## 📊 总体评分

| 维度 | 评分 | 说明 |
|------|------|------|
| **代码实现质量** | ⭐⭐⭐ (3/5) | 后端存在多处错误忽略、硬编码、安全缺失；前端有 XSS 风险和类型安全问题 |
| **测试覆盖率** | ⭐ (1/5) | 后端零测试，前端仅 3 个测试文件，E2E 有基础覆盖但断言宽松 |
| **架构设计** | ⭐⭐⭐ (3/5) | 模块化思路正确，但存在"上帝文件"、反向依赖、半废弃服务等问题 |
| **文档一致性** | ⭐⭐ (2/5) | SPEC.md 严重过时，18 处高严重度不一致，核心类型定义冲突 |

---

## 🔴 P0 — 必须立即修复（安全/数据完整性风险）

### P0-1: 前端 XSS 漏洞
- **文件**: `web/src/pages/Copilot.vue:7`
- **问题**: `v-html="formatMessage(msg.content)"` 直接渲染 AI 返回内容，`formatMessage` 仅做 `\n→<br>` 替换，未转义 HTML
- **风险**: AI 返回恶意 HTML/JS 可导致 XSS 攻击
- **修复**: 使用文本插值 `{{ msg.content }}` + CSS `white-space: pre-line`，或引入 DOMPurify 净化

### P0-2: 批量数据库操作无事务保护
- **文件**: `pkg/storage/ohlcv.go:39-67`, `pkg/storage/cache.go` 多处
- **问题**: `SaveOHLCVBatch`、`SaveFactorCacheBatch`、`SaveFactorReturnBatch`、`SaveICEntryBatch` 等使用 `pgx.Batch` 但无事务包裹，部分失败时已插入记录不回滚
- **修复**: 使用 `pgx.Tx` 包裹批量操作，失败时回滚

### P0-3: HTTP 默认客户端无超时（可导致 goroutine 永久阻塞）
- **文件**: `cmd/analysis/main.go:337`, L573-634
- **问题**: AI API 调用和所有代理端点使用 `http.DefaultClient`/`http.Get()`/`http.Post()`，无超时设置
- **修复**: 创建带超时的 `http.Client{Timeout: 30*time.Second}`，复用连接池

### P0-4: 日期字符串切片可导致 panic
- **文件**: `cmd/data/main.go:1023-1024`
- **问题**: `syncCalendarHandler` 中 `req.StartDate[:4]` 在输入短于 4 字符时 panic
- **修复**: 先验证字符串长度，或使用 `time.Parse()` 解析

### P0-5: 所有 HTTP 端点无认证/速率限制
- **文件**: `cmd/analysis/main.go:142-143`, `cmd/data/main.go:91`
- **问题**: 仅使用 `gin.Recovery()`，缺少 CORS、认证、速率限制中间件；`/api/datasource/switch` 等危险操作完全开放
- **修复**: 添加 CORS 中间件、JWT 认证中间件、令牌桶速率限制

### P0-6: 配置文件明文密码
- **文件**: `config/analysis-service.yaml:7`
- **问题**: `postgres://postgres:postgres@...` 明文密码
- **修复**: 改用环境变量 `DATABASE_URL`，YAML 中仅保留模板

---

## 🟠 P1 — 尽快修复（代码质量/可维护性）

### P1-1: 后端核心模块零测试覆盖
- **现状**: 整个 Go 后端不存在任何 `*_test.go` 文件
- **最需测试的模块**（按优先级）:
  1. `pkg/backtest/performance.go` — Sharpe/Sortino/MaxDrawdown/VaR/CVaR 计算
  2. `pkg/backtest/tracker.go` — 交易执行、T+1 规则、佣金计算
  3. `pkg/risk/volatility.go` — 波动率仓位、regime 调整
  4. `pkg/risk/stoploss.go` — 止损止盈触发
  5. `pkg/risk/regime.go` — 市场状态检测
- **修复**: 为每个模块创建 `*_test.go`，覆盖正常路径和边界条件

### P1-2: "上帝文件" — main.go 过于臃肿
- **文件**: `cmd/analysis/main.go` (~1347行), `cmd/data/main.go` (~1894行)
- **问题**: 路由注册、handler、中间件、AI 调用逻辑全部混在一个文件中
- **修复**: 拆分为 `cmd/analysis/handlers/`、`cmd/data/handlers/`、`cmd/analysis/middleware/` 等独立包

### P1-3: 硬编码的服务 URL 和配置
- **位置**:
  - `"http://data-service:8081"` — `cmd/analysis/main.go:582` 及多处
  - `"http://localhost:8085"` — `cmd/data/main.go:1634`
  - `"gpt-4o-mini"` + `max_tokens: 2000` — `cmd/analysis/main.go:312`
  - `"/Users/ruoxi/longshaosWorld/quant-trading"` — `pkg/strategy/copilot.go:159`
- **修复**: 全部提取到配置文件或环境变量

### P1-4: data-service 反向依赖 analysis-service
- **文件**: `cmd/data/main.go:1634`
- **问题**: walk-forward handler 通过 HTTP 回调 analysis-service，违反服务依赖方向，且硬编码 URL 在 Docker 环境不可用
- **修复**: 将 walk-forward 逻辑完全移入 analysis-service

### P1-5: strategy-service 半废弃状态
- **问题**: analysis-service 已通过 `_ import` 内化所有策略插件，strategy-service 使用旧 `domain.Strategy` 接口，实际不再被调用
- **修复**: 明确决策——废弃 strategy-service（从 docker-compose 移除）或改造为独立策略服务

### P1-6: 前端 `buildTradeMarkers` 重复且逻辑不一致
- **文件**: `web/src/utils/tradeMarkers.ts` vs `web/src/composables/useBacktestChart.ts:40-54`
- **问题**: 两个版本的 `buildTradeMarkers` 逻辑不同，旧版对 `close` 方向取到错误日期
- **修复**: 删除 `useBacktestChart.ts` 中的旧版实现（该文件整体为死代码，未被引用）

### P1-7: 前端 `shallowRef` 遗漏
- **位置**:
  - `web/src/pages/Dashboard.vue:56` — `ref<any>(null)` 应为 `shallowRef<BacktestResult|null>`
  - `web/src/stores/backtest.ts:90` — `ref<BacktestResult|null>` 应为 `shallowRef`
  - `web/src/stores/backtest.ts:94` — `ref<Map<...>>` 应为 `shallowRef`

### P1-8: 前端 TypeScript `any` 类型滥用（7处严重）
- **位置**: `web/src/api/copilot.ts:8`, `web/src/components/dashboard/QuickBacktest.vue:43`, `web/src/components/dashboard/MarketMetrics.vue:49`, `web/src/components/backtest/EquityChart.vue:112`, `web/src/pages/Dashboard.vue:53/56/81`
- **修复**: 为每个位置定义具体接口类型

---

## 🟡 P2 — 计划修复（架构/文档改进）

### P2-1: domain/types.go 过度膨胀（509行）
- **问题**: 包含不应在此的接口（`MarketDataProvider`、`RiskManager`）、配置类型（`DatabaseConfig`）、已废弃的 `Strategy` 接口
- **修复**: 精简为纯值对象，接口移入各自实现的包，删除 `Deprecated` 标记的旧 `Strategy` 接口

### P2-2: SPEC.md 严重过时
- **问题**: 缺少 20+ 个已实现的 API 端点，部分端点路径/方法与实际不符
- **关键不一致**:
  - `POST /analyze` 文档有但代码不存在
  - `POST /strategies/load` 实际为 `POST /strategies/reload`
  - `POST /stop_loss` 实际为 `POST /check_stoploss`
  - `GET /regime` 实际为 `POST /detect_regime`
  - Direction 类型文档为 `int`，代码为 `string`
  - ErrorCode 类型文档为 `int`，代码为 `string`
- **修复**: 全面更新 SPEC.md，按实际代码同步

### P2-3: AGENTS.md Redis 端口错误
- **问题**: Data Flow 中写 `Redis (:6377)`，实际 docker-compose 为 `6379:6379`
- **修复**: 更正为 `Redis (:6379)`

### P2-4: ARCHITECTURE.md 服务状态过时
- **问题**: risk-service 和 execution-service 标记为 `🔲 规划中`，实际已实现并运行
- **修复**: 更新状态为 `✅ 已实现`

### P2-5: 新旧两套 Strategy 接口并存
- **问题**: `domain.Strategy`（旧，`Signals()` 方法）vs `strategy.Strategy`（新，`GenerateSignals()` 方法），strategy-service 仍使用旧接口
- **修复**: 完成迁移后删除旧接口和旧注册表 `DefaultOldRegistry`

### P2-6: 回测引擎无法水平扩展
- **文件**: `pkg/backtest/engine.go:86`
- **问题**: `currentBacktest *BacktestState` 单例，只保存最近一次回测；`inMemoryOHLCV` 进程内缓存不共享
- **修复**: 改为 `map[string]*BacktestState` 支持并发，L1 缓存改为 Redis + 本地二级缓存

### P2-7: 代理端点代码高度重复
- **文件**: `cmd/analysis/main.go:573-650`
- **问题**: 4 个代理端点（/ohlcv、/screen、/stocks/count、/market/index）代码结构完全重复
- **修复**: 提取通用代理函数 `proxyRequest(c *gin.Context, targetURL string)`

### P2-8: 前端缺少 404 路由
- **文件**: `web/src/router/index.ts`
- **修复**: 添加 `{ path: '/:pathMatch(.*)*', component: NotFound }`

### P2-9: 前端生产环境调试代码残留
- **问题**: 37 处 `console.log/warn/error`，其中 `useAsyncBacktest.ts` 有 17 处
- **修复**: 引入 Vite `define` 配置在生产构建时移除 console，或使用日志库

### P2-10: E2E 测试无效断言
- **文件**: `e2e/tests/backtest-engine.spec.ts:382`
- **问题**: `expect(toastExists || true).toBeTruthy()` 永远为真；`.catch(() => {})` 吞掉错误
- **修复**: 改为有意义的断言

---

## 🟢 P3 — 持续改进

| 编号 | 问题 | 修复建议 |
|------|------|---------|
| P3-1 | `isRebalanceDay()` 在 3 个策略文件中重复 | 提取到 `pkg/strategy/utils.go` |
| P3-2 | `callScreenAPI()` 在 2 个策略文件中重复 | 提取公共实现 |
| P3-3 | `sampleData()` 在 EquityChart.vue 和 useBacktestChart.ts 中重复 | 提取到 `utils/` |
| P3-4 | 前端 `HistoryEntry`/`TradeInfo` 接口在 3 个文件中重复定义 | 统一到 `types/` |
| P3-5 | 前端 icon 组件未使用 `markRaw()` 包装 | `web/src/components/layout/AppSidebar.vue:60-66`, `web/src/components/dashboard/NavTiles.vue:36-40` |
| P3-6 | catch 子句 `e: any` → `e: unknown` | 8 处需修改 |
| P3-7 | API 路径前缀不统一 | `/api/strategies` vs `/backtest` |
| P3-8 | `doScreen` 命名不规范且放在 copilot API 中 | 独立为 `screener.ts` |
| P3-9 | `pkg/strategy/copilot.go` 硬编码绝对路径 | 改为相对路径或配置 |
| P3-10 | Copilot prompt 中 Strategy 接口不完整 | 缺少 `Configure`/`Weight`/`Cleanup`，AI 生成策略会不合规 |
| P3-11 | `rand.Seed()` 已在 Go 1.20+ 废弃 | 改用 `rand.New(rand.NewSource(...))` |
| P3-12 | `pkg/strategy/registry.go` 中 `panic()` | 改为返回 error |
| P3-13 | execution-service 订单使用内存 map 存储 | 持久化到 Redis/PostgreSQL |
| P3-14 | 数据库迁移硬编码在 `migrate()` 中 | 引入 golang-migrate 工具 |
| P3-15 | 单 Dockerfile 构建所有服务 | 为每个服务创建独立 Dockerfile |

---

## 📋 可执行修复计划

### 第 1 周：安全与数据完整性

| 序号 | 任务 | 涉及文件 | 预估工作量 |
|------|------|---------|-----------|
| 1 | 修复 Copilot.vue XSS 漏洞 | `web/src/pages/Copilot.vue` | 0.5h |
| 2 | 为批量 DB 操作添加事务 | `pkg/storage/ohlcv.go`, `cache.go` | 2h |
| 3 | 创建带超时的 HTTP 客户端 | `cmd/analysis/main.go`, `cmd/data/main.go` | 1h |
| 4 | 修复 syncCalendarHandler panic | `cmd/data/main.go:1023` | 0.5h |
| 5 | 添加 CORS + 速率限制中间件 | `cmd/analysis/main.go`, `cmd/data/main.go` | 2h |
| 6 | 配置文件移除明文密码 | `config/analysis-service.yaml` | 0.5h |

### 第 2 周：核心测试补全

| 序号 | 任务 | 涉及文件 | 预估工作量 |
|------|------|---------|-----------|
| 7 | `performance_test.go` — 绩效指标测试 | `pkg/backtest/performance.go` | 4h |
| 8 | `tracker_test.go` — 交易执行测试 | `pkg/backtest/tracker.go` | 6h |
| 9 | `risk_test.go` — 风控模块测试 | `pkg/risk/volatility.go`, `stoploss.go`, `regime.go` | 4h |
| 10 | `format.test.ts` 补充边界测试 | `web/src/utils/format.test.ts` | 1h |
| 11 | `useAsyncBacktest.test.ts` — 状态机测试 | `web/src/composables/` | 3h |

### 第 3 周：代码结构优化

| 序号 | 任务 | 涉及文件 | 预估工作量 |
|------|------|---------|-----------|
| 12 | 拆分 analysis/main.go handler | `cmd/analysis/main.go` → `handlers/` | 4h |
| 13 | 拆分 data/main.go handler | `cmd/data/main.go` → `handlers/` | 4h |
| 14 | 移除 data→analysis 反向依赖 | `cmd/data/main.go` walk-forward | 2h |
| 15 | 清理 domain/types.go | `pkg/domain/types.go` | 3h |
| 16 | 删除死代码 useBacktestChart.ts | `web/src/composables/useBacktestChart.ts` | 0.5h |
| 17 | 修复前端 shallowRef/markRaw 遗漏 | 5 个文件 | 1h |

### 第 4 周：文档同步与架构决策

| 序号 | 任务 | 涉及文件 | 预估工作量 |
|------|------|---------|-----------|
| 18 | 全面更新 SPEC.md | `docs/SPEC.md` | 4h |
| 19 | 更新 ARCHITECTURE.md 服务状态 | `docs/ARCHITECTURE.md` | 1h |
| 20 | 修正 AGENTS.md Redis 端口 | `AGENTS.md` | 0.5h |
| 21 | 决策 strategy-service 去留 | docker-compose.yml, docs/ | 2h |
| 22 | 统一新旧 Strategy 接口 | `pkg/domain/types.go`, `pkg/strategy/` | 3h |
| 23 | 更新 Copilot prompt 接口定义 | `cmd/analysis/main.go:197-202` | 1h |

---

## 📈 问题统计总览

| 严重度 | 后端 | 前端 | 测试 | 架构 | 文档 | 合计 |
|--------|------|------|------|------|------|------|
| 🔴 P0 | 5 | 1 | 0 | 0 | 0 | **6** |
| 🟠 P1 | 4 | 4 | 1 | 3 | 0 | **12** |
| 🟡 P2 | 3 | 3 | 1 | 4 | 3 | **14** |
| 🟢 P3 | 6 | 5 | 0 | 2 | 2 | **15** |
| **合计** | **18** | **13** | **2** | **9** | **5** | **47** |

---

## ✅ 项目亮点

1. **SQL 注入防护优秀** — 所有 SQL 查询均使用参数化（`$1`, `$2`），零注入风险
2. **Strategy 接口一致性** — SPEC.md/VISION.md/AGENTS.md/pkg/strategy/strategy.go 四方完全一致
3. **marketdata 包设计优秀** — Provider 接口 + 6 种实现 + 运行时切换 + 自动回退
4. **前端 Composition API 100% 合规** — 所有 .vue 文件均使用 `<script setup lang="ts">`
5. **E2E 测试覆盖主要用户流程** — 9 个 spec 文件，~55 个用例，含跨页面导航测试
6. **依赖管理精简** — Go 仅 7 个直接依赖，无冗余引入

---

## 附录 A：后端代码实现质量详细审查

### A.1 错误处理

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **高** | `cmd/analysis/main.go` | L320 | `json.Marshal(payload)` 的错误被忽略（`payloadBytes, _ := json.Marshal(payload)`），如果序列化失败会导致发送空请求体到 AI 服务 |
| **高** | `cmd/analysis/main.go` | L604 | `json.Marshal(reqBody)` 的错误被忽略（`bodyBytes, _ := json.Marshal(reqBody)`），screen proxy 中序列化失败会导致空请求 |
| **高** | `cmd/analysis/main.go` | L690 | `json.Marshal(req.Params)` 的错误被忽略（`bytes, _ := json.Marshal(req.Params)`），策略参数序列化失败时写入空字符串 |
| **高** | `cmd/analysis/main.go` | L756 | 同上，PUT /api/strategies/:id 中 `json.Marshal(req.Params)` 错误被忽略 |
| **高** | `cmd/data/main.go` | L1023-1024 | `syncCalendarHandler` 中在验证日期格式**之前**就进行字符串切片操作（`req.StartDate[:4]`），如果输入短于8字符会 panic |
| **中** | `pkg/strategy/db.go` | L77 | `json.Unmarshal([]byte(cfg.Params), &params)` 的错误被忽略（`_ = json.Unmarshal(...)`），DB 策略参数反序列化失败时静默跳过 |
| **中** | `pkg/strategy/registry.go` | L213 | `Register()` 函数中调用 `panic("old registry not initialized, call OldInit first")`，生产代码中不应使用 panic，应返回 error |
| **中** | `pkg/backtest/engine.go` | L217 | `rand.Seed(config.Seed)` 已在 Go 1.20+ 中废弃（deprecated），应使用 `rand.New(rand.NewSource(...))` |
| **低** | `pkg/risk/manager.go` | L122 | `currentPrice <= 0` 时直接赋值 `currentPrice = 100.0`，这是一个魔法数字，且静默覆盖了无效价格而非返回错误 |

### A.2 context.Context 使用

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **高** | `cmd/data/main.go` | L667 | `syncAllOHLCVHandler` 中使用 `context.Background()` 替代请求 context，虽然注释解释了原因（后台处理），但应在返回响应前从请求 context 派生，以便在客户端断开时取消 |
| **高** | `cmd/data/main.go` | L694-695 | 后台 goroutine 中使用 `context.Background()` 而非带超时的 context，如果 Tushare API 挂起会导致 goroutine 永久阻塞 |
| **中** | `cmd/data/main.go` | L1676 | `runWalkForwardHandler` 的后台 goroutine 使用 `context.Background()`，无超时和取消机制 |
| **中** | `pkg/strategy/plugins/multi_factor.go` | L262 | `callScreenAPI` 中使用 `httpReq.WithContext(context.Background())`，忽略了传入的 context，无法在请求取消时终止 HTTP 调用 |
| **中** | `pkg/strategy/plugins/value_screen.go` | L248 | 同上，`callScreenAPI` 使用 `context.Background()` 忽略了请求 context |

### A.3 硬编码与魔法数字

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **高** | `cmd/analysis/main.go` | L312 | AI 模型名 `"gpt-4o-mini"` 和 `max_tokens: 2000` 硬编码，应从配置读取 |
| **高** | `cmd/analysis/main.go` | L444 | 生成策略文件保存目录 `"./generated_strategies"` 硬编码 |
| **高** | `cmd/analysis/main.go` | L582 | 数据服务代理 URL `"http://data-service:8081"` 硬编码在多处，应从配置读取 |
| **高** | `cmd/data/main.go` | L1634 | `analysisServiceURL = "http://localhost:8085"` 硬编码为包级常量 |
| **中** | `pkg/strategy/plugins/multi_factor.go` | L255 | Screen API URL `"http://data-service:8081/screen"` 硬编码 |
| **中** | `pkg/strategy/plugins/value_screen.go` | L241 | 同上，Screen API URL 硬编码 |
| **中** | `pkg/backtest/engine.go` | L196-206 | 默认配置值硬编码：`InitialCapital=1000000`, `CommissionRate=0.0003`, `SlippageRate=0.0001`, `RiskFreeRate=0.03` |
| **中** | `pkg/strategy/plugins/momentum.go` | L209-215 | Weight 方法中 `0.05`, `0.01` 等权重限制硬编码 |
| **低** | `cmd/strategy/main.go` | L351 | `MarketCap: 1_000_000_000` 占位符硬编码 |

### A.4 并发安全性

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **高** | `pkg/strategy/copilot.go` | L159 | `buildCmd.Dir = "/Users/ruoxi/longshaosWorld/quant-trading"` 硬编码了绝对路径，在其他环境中无法工作 |
| **中** | `pkg/backtest/engine.go` | L354-356 | `e.currentBacktest` 只保存最近一次回测状态，多次并发回测会互相覆盖。虽然当前是同步执行，但设计上存在竞态风险 |
| **中** | `pkg/strategy/registry.go` | L110 | `Init()` 函数直接替换 `DefaultRegistry` 指针，无锁保护，如果并发调用存在数据竞争 |
| **低** | `pkg/strategy/copilot.go` | L36 | `jobs sync.Map` 永远不会清理已完成的 job，长时间运行会导致内存泄漏 |

### A.5 资源泄漏

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **高** | `cmd/analysis/main.go` | L337 | `http.DefaultClient.Do(httpReq)` 使用了默认的 HTTP Client，没有超时设置，如果 AI API 无响应会永久阻塞 |
| **中** | `cmd/analysis/main.go` | L583-594 | OHLCV proxy 使用 `http.Get()` 默认客户端，无超时和连接池管理 |
| **中** | `cmd/analysis/main.go` | L605-618 | Screen proxy 使用 `http.Post()` 默认客户端，同上 |
| **中** | `cmd/analysis/main.go` | L622-634 | stocks/count 和 market/index 代理均使用默认 HTTP 客户端 |
| **低** | `cmd/analysis/main.go` | L111 | `NewPostgresStore(context.Background(), dbURL)` 使用 `context.Background()`，如果数据库连接挂起，应用启动会永久阻塞 |

### A.6 Strategy 接口合规性

| 策略 | 文件 | 合规状态 | 问题描述 |
|------|------|----------|----------|
| momentum | `plugins/momentum.go` | **合规** | 全部7个方法均已实现 |
| mean_reversion | `plugins/mean_reversion.go` | **合规** | 全部7个方法均已实现 |
| multi_factor | `plugins/multi_factor.go` | **合规** | 全部7个方法均已实现，额外实现了 `FactorAware` 接口 |
| value_screening | `plugins/value_screen.go` | **合规** | 全部7个方法均已实现 |
| td_sequential | `plugins/new_strategies.go` | **合规** | 全部7个方法均已实现 |
| bollinger_mr | `plugins/new_strategies.go` | **合规** | 全部7个方法均已实现 |
| volume_price_trend | `plugins/new_strategies.go` | **合规** | 全部7个方法均已实现 |
| volatility_breakout | `plugins/new_strategies.go` | **合规** | 全部7个方法均已实现 |
| value_momentum (examples) | `examples/value_momentum.go` | **不合规** | 实现的是 `domain.Strategy`（旧接口），方法签名是 `Signals()` 而非 `GenerateSignals()` |
| momentum (examples) | `examples/momentum.go` | **不合规** | 同上，实现的是旧 `domain.Strategy` 接口 |

### A.7 API 端点实现问题

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **高** | `cmd/analysis/main.go` | L428-465 | `saveStrategyHandler` 仅做了简单的文件名清理，但未验证代码内容是否为合法 Go 代码，存在任意代码写入风险 |
| **高** | `cmd/analysis/main.go` | L573-595 | OHLCV proxy 中 `symbol` 参数未做验证/清理，直接拼接到 URL 中，存在路径注入风险（如 `../` 或特殊字符） |
| **中** | `cmd/analysis/main.go` | L783-842 | POST /backtest 端点通过两次 `json.Unmarshal` 来判断请求格式，如果请求体部分匹配两种格式会导致意外行为 |
| **中** | `cmd/data/main.go` | L597-635 | `syncOHLCVHandler` 中 `StartDate`/`EndDate` 未验证格式，直接传给 Tushare API |
| **低** | `cmd/analysis/main.go` | L785 | GET /backtest 的 `limit` 参数允许最大值 100，但未做下限检查 |

### A.8 数据层问题

| 严重度 | 文件 | 行号 | 问题描述 |
|--------|------|------|----------|
| **低** | 全部 storage 文件 | - | **所有 SQL 查询均使用参数化查询（`$1`, `$2`, ...）**，SQL 注入风险极低 |
| **高** | `pkg/storage/ohlcv.go` | L39-67 | `SaveOHLCVBatch` 使用 `pgx.Batch` 但未包裹在事务中 |
| **高** | `pkg/storage/cache.go` | L16-44 | `SaveFactorCacheBatch` 同上，批量操作无事务保护 |
| **中** | `pkg/storage/postgres.go` | L26-30 | 连接池参数从 `constants.go` 读取，配置合理但无法通过 YAML 配置覆盖 |
| **中** | `pkg/storage/ohlcv.go` | L100-121 | `GetTradingDays` 使用 `SELECT DISTINCT trade_date FROM ohlcv_daily_qfq`，应优先使用 `trading_calendar` 表 |
| **中** | `cmd/data/main.go` | L385-386 | `marketIndexHandler` 对每个指数执行两次独立查询，应合并为一次查询 |

---

## 附录 B：前端代码实现质量详细审查

### B.1 TypeScript `any` 类型滥用（25处）

#### 严重（7处）

| # | 文件 | 行号 | 代码 | 建议 |
|---|------|------|------|------|
| 1 | `web/src/api/copilot.ts` | 8 | `export function saveStrategy(data: any)` | 定义 `SaveStrategyRequest` 接口 |
| 2 | `web/src/components/dashboard/QuickBacktest.vue` | 43 | `quickResult: any` | 使用 `BacktestResult \| null` 类型 |
| 3 | `web/src/components/dashboard/MarketMetrics.vue` | 49 | `metrics: Record<string, any>` | 使用 `MarketIndex` 接口替代 |
| 4 | `web/src/components/backtest/EquityChart.vue` | 112 | `const datasets: any[] = []` | 使用 Chart.js 的 `ChartData['datasets']` 类型 |
| 5 | `web/src/pages/Dashboard.vue` | 53 | `const marketMetrics = ref<Record<string, any>>({})` | 使用 `MarketIndex` 类型 |
| 6 | `web/src/pages/Dashboard.vue` | 56 | `const quickResult = ref<any>(null)` | 使用 `ref<BacktestResult \| null>(null)` |
| 7 | `web/src/pages/Dashboard.vue` | 81 | `marketMetrics.value = latest as any` | 修复类型不匹配问题 |

#### 中等（8处 catch 子句）

| # | 文件 | 行号 |
|---|------|------|
| 8 | `BacktestEngine.vue` | 260 |
| 9 | `BacktestEngine.vue` | 292 |
| 10 | `useAsyncBacktest.ts` | 226 |
| 11 | `StrategyLab.vue` | 84 |
| 12 | `Dashboard.vue` | 106 |
| 13 | `Screener.vue` | 123 |
| 14 | `Copilot.vue` | 71 |
| 15 | `client.ts` | 86 |

> 建议：catch 子句统一使用 `catch (e: unknown)` + `e instanceof Error` 判断模式

#### 低（10处）

| # | 文件 | 行号 | 建议 |
|---|------|------|------|
| 16 | `format.ts` | 33 | `fmtVolume(v: any)` → `v: number \| null \| undefined` |
| 17 | `format.ts` | 40 | `fmtAmount(a: any)` → `a: number \| null \| undefined` |
| 18 | `format.ts` | 45 | `fmtMetric(v: any)` → `v: number \| null \| undefined` |
| 19 | `api.ts` | 67 | `params: Record<string, any>` → `Record<string, unknown>` |
| 20 | `client.ts` | 4 | `public body?: any` → `unknown` |
| 21 | `client.ts` | 74 | `let errBody: any` → `unknown` |
| 22 | `Screener.vue` | 87 | `render: (_: any, i: number)` → 定义行渲染函数参数类型 |
| 23 | `Dashboard.vue` | 70 | `(s: any) => s.name \|\| s.id` → 使用 `Strategy` 类型 |
| 24 | `BacktestEngine.vue` | 219 | `(s: any) => s.name \|\| s.id` → 同上 |
| 25 | `env.d.ts` | 5 | `DefineComponent<{}, {}, any>` → Vite 模板声明，可接受 |

### B.2 重复函数/逻辑

| 严重度 | 函数 | 文件1 | 文件2 | 说明 |
|--------|------|-------|-------|------|
| **严重** | `buildTradeMarkers` | `utils/tradeMarkers.ts:11-38` | `composables/useBacktestChart.ts:40-54` | 逻辑不一致，旧版对 close 方向取错误日期 |
| **严重** | `sampleData` | `EquityChart.vue:66-78` | `useBacktestChart.ts:26-38` | 逻辑完全相同，应提取到 utils/ |
| **中等** | `HistoryEntry`/`TradeInfo` 接口 | `stores/backtest.ts:10,23` | `BacktestHistory.vue:85,98` + `ConsoleHistory.vue:28` | 三个文件分别定义了结构相似但名称不同的接口 |
| **中等** | `directionLabel` 函数 | `BacktestHistory.vue:162` | `TradeTable.vue:22` | 逻辑类似 |
| **低** | `itemTitle`/`itemDesc` 函数 | `BacktestHistory.vue:141` | `ConsoleHistory.vue:50` | 逻辑类似 |

### B.3 Store 设计问题

| 问题 | 文件 | 行号 | 说明 |
|------|------|------|------|
| `currentResult` 使用 `ref` 而非 `shallowRef` | `stores/backtest.ts` | 90 | BacktestResult 含 portfolio_values 和 trades 大数组 |
| `tradesMap` 使用 `ref` 而非 `shallowRef` | `stores/backtest.ts` | 94 | Map 内含大量交易数据 |
| `loadHistoryFromDB` 空 catch | `stores/backtest.ts` | 196 | `catch {}` 吞掉了所有错误，无任何日志 |
| `addToHistory` 空 catch | `stores/backtest.ts` | 138 | `try { localStorage.setItem(...) } catch {}` 吞掉了存储错误 |
| store 暴露 `historyWithTrades` 但命名为 `history` | `stores/backtest.ts` | 213 | 外部使用 `store.history` 实际得到的是 computed 结果，容易混淆 |
| 缺少 `currentResult` 的清理方法 | - | - | 没有 `clearResult()` 或 `reset()` 方法 |

### B.4 API 调用不一致

| 问题 | 文件 | 行号 | 说明 |
|------|------|------|------|
| API 命名不一致 | `factor.ts` | 1 | 使用 `import apiClient`，而其他文件使用 `import api` |
| `doScreen` 命名不规范 | `copilot.ts` | 16 | 不符合项目命名惯例，且选股功能放在 copilot API 中不合理 |
| `getOHLCV` 返回类型未泛型化 | `backtest.ts` | 39 | 缺少泛型参数 `<OHLCVAPIResponse>` |
| `getStrategies` 路径不一致 | `strategy.ts` | 5 | 使用 `/api/strategies`，而 backtest 使用 `/backtest` |

---

## 附录 C：测试代码覆盖率详细审查

### C.1 后端测试（Go）— 严重缺失

项目中 **不存在任何 `*_test.go` 文件**。整个后端 Go 代码库没有任何单元测试、集成测试或基准测试。

#### 缺少测试的关键模块

| 优先级 | 模块 | 关键缺失 |
|--------|------|---------|
| P0 | `pkg/backtest/performance.go` | Sharpe/Sortino/MaxDrawdown/VaR/CVaR 计算的正确性和边界 |
| P0 | `pkg/backtest/tracker.go` | 交易执行（买入/卖出/做空/平仓）、T+1 规则、佣金/印花税、限价单、流动性约束 |
| P0 | `pkg/risk/volatility.go` | 波动率计算、仓位权重、regime 调整 |
| P0 | `pkg/risk/stoploss.go` | ATR 计算、止损止盈触发 |
| P0 | `pkg/risk/regime.go` | 市场状态检测 |
| P1 | `pkg/data/factor.go` | ZScore 计算 |
| P1 | `pkg/strategy/registry.go` | 注册/查找/重载 |
| P1 | `pkg/strategy/plugins/*` | 各策略信号生成 |
| P1 | `pkg/domain/types.go` | ParseFactorType |
| P2 | `pkg/storage/*` | 数据库 CRUD（需集成测试） |
| P2 | `pkg/marketdata/*` | 行情数据获取 |

#### Mock 需求评估

以下模块在测试时需要 mock：
- `storage.PostgresStore` — 需要 interface 抽象或 testcontainers
- `marketdata.Provider` — 已有 interface，可直接 mock
- `httpclient.Client` — 需要 mock HTTP 调用
- `strategy.Strategy` — 已有 interface，可直接 mock

### C.2 前端测试（Vitest）— 基础覆盖

#### 现有测试文件

| 文件 | 用例数 | 覆盖 | 评价 |
|------|-------|------|------|
| `web/src/utils/format.test.ts` | ~22 | fmtPercent/fmtNumber/fmtCurrency/formatDate/fmtVolume/fmtMetric | **良好** |
| `web/src/stores/backtest.test.ts` | 11 | 初始状态/添加结果/trades访问/字段保留/清空/去重 | **良好** |
| `web/src/components/backtest/EquityChart.test.ts` | 11 | 空输入/买入标记/卖出标记/过滤/混合/回退字段/方向/多标的 | **良好** |

#### 缺少测试的前端模块

| 模块 | 文件 | 缺失严重程度 |
|------|------|------------|
| **API Client** | `web/src/api/client.ts` | **高** — 核心网络层，含超时/重试/错误处理，完全无测试 |
| **API 模块** | `web/src/api/backtest.ts` 等 6 个文件 | **高** — 所有 API 调用封装无测试 |
| **useAsyncBacktest** | `web/src/composables/useAsyncBacktest.ts` | **高** — 异步回测状态机，含轮询/超时/取消逻辑 |
| **useBacktestChart** | `web/src/composables/useBacktestChart.ts` | **中** — Chart.js 渲染逻辑 |
| **Vue 组件** | 20 个 .vue 文件 | **中** — 无组件级单元测试 |

### C.3 E2E 测试（Playwright）— 较为完善

#### 现有测试文件

| 文件 | 用例数 | 覆盖页面/流程 | 评价 |
|------|-------|-------------|------|
| `dashboard.spec.ts` | 9 | Dashboard 页面加载、侧边栏、指标卡、快速回测 | **优秀** |
| `backtest-engine.spec.ts` | ~20 | 回测页面加载、表单、同步/异步模式、运行回测 | **优秀** |
| `screener.spec.ts` | 8 | 选股器加载、筛选输入、操作按钮、重置 | **良好** |
| `strategy-selector.spec.ts` | 7 | 策略实验室加载、标题按钮、搜索、策略卡片 | **良好** |
| `copilot.spec.ts` | 7 | Copilot 页面、聊天界面、欢迎消息 | **良好** |
| `cross-navigation.spec.ts` | 7 | 跨页面导航、SPA 路由、JS 错误检测 | **优秀** |
| `api-health.spec.ts` | 5 | Health、stocks/count、market/index、strategies | **良好** |
| `api-backtest.spec.ts` | 8 | 回测 API、结果持久化、报告查询 | **优秀** |
| `api-strategy.spec.ts` | 2 | 策略列表、Copilot 生成 | **一般** |

#### E2E 测试问题

1. **无效断言**: `expect(toastExists || true).toBeTruthy()` 永远为真
2. **错误吞掉**: `.catch(() => {})` 吞掉了错误
3. **硬编码等待**: `page.waitForTimeout(1000/1500/2000/3000)` 容易导致 flaky test
4. **宽松断言**: `expect([200, 201, 202, 400]).toContain(res.status())` 接受 400 作为成功

#### 缺少的 E2E 场景

- Copilot 实际发送消息并收到回复的完整流程
- 策略实验室创建/编辑/删除策略
- 选股器实际筛选并展示结果
- 回测参数验证（无效日期范围、负数初始资金等）
- 网络断开/超时的错误处理

---

## 附录 D：系统架构合理性详细审查

### D.1 模块化程度

| 服务 | 代码行数 | 评估 |
|------|---------|------|
| `cmd/analysis/main.go` | ~1347行 | **严重臃肿** |
| `cmd/data/main.go` | ~1894行 | **严重臃肿** |
| `cmd/strategy/main.go` | ~449行 | 轻度臃肿 |
| `cmd/execution/main.go` | ~341行 | 合理 |
| `cmd/risk/main.go` | ~459行 | 轻度臃肿 |

### D.2 pkg/ 包职责划分

| 包 | 职责 | 评估 |
|---|------|------|
| `domain/` | 核心类型定义 | **过度膨胀**（含不应在此的接口和配置类型） |
| `storage/` | PostgreSQL + Redis | 职责清晰但与业务耦合 |
| `backtest/` | 回测引擎 | 职责清晰，设计良好 |
| `strategy/` | 策略注册 + 插件 + Copilot + DB | **职责过多** |
| `marketdata/` | 数据源抽象层 | 设计优秀 |
| `data/` | 数据缓存 + 因子计算 | 职责清晰 |
| `risk/` | 风险管理 | 职责清晰 |
| `live/` | 实盘交易接口 | 职责清晰 |
| `ai/` | AI 客户端 | 职责清晰 |

### D.3 服务间通信

**当前全部采用同步 HTTP 调用**，无 gRPC、无消息队列。

| 调用路径 | 方式 | 评估 |
|---------|------|------|
| analysis → data-service | HTTP 代理 | 合理 |
| analysis → strategy-service | HTTP（已有本地回退） | 合理但冗余 |
| analysis → risk-service | HTTP（已有本地回退） | 合理但冗余 |
| data → analysis-service | HTTP | **反向依赖！** |

### D.4 扩展性评估

| 扩展维度 | 评估 | 说明 |
|---------|------|------|
| 添加新策略 | **容易** | 实现 Strategy 接口 + init() 注册即可 |
| 添加新数据源 | **容易** | 实现 Provider 接口即可，marketdata 包设计优秀 |
| 水平扩展回测 | **困难** | 单例 BacktestState + 进程内缓存 |
| 水平扩展执行 | **困难** | 内存 orderStore，无持久化 |
| 配置热更新 | **困难** | 部分配置硬编码，部分从环境变量读取 |

### D.5 部署架构问题

| 问题 | 说明 |
|------|------|
| 端口映射冲突风险 | Redis 6379、PostgreSQL 5432 与宿主机常用端口冲突 |
| strategy-service 和 risk-service 依赖 data-service 但实际不使用 | 移除不必要的 `depends_on` |
| 单 Dockerfile 构建所有服务 | 任何服务变更都触发全部重新构建 |
| 配置文件明文密码 | `config/analysis-service.yaml` 包含明文数据库密码 |

### D.6 数据流架构

#### 回测引擎数据流

```
用户请求 → analysis-service (POST /backtest)
  → Engine.RunBacktest()
    → warmCache()          : 批量预加载 OHLCV 到进程内存
    → warmFactorCache()    : 从 PostgreSQL 加载因子 z-score 到进程内存
    → getTradingDays()     : 从 data-service 获取交易日历
    ┌─ 每个交易日循环 ──────────────────────────────┐
    │  → getOHLCV()         : L1 内存 → Provider 回退 │
    │  → detectRegime()     : 本地 RiskManager → HTTP  │
    │  → getSignals()       : 本地 Registry → HTTP     │
    │  → calculatePosition(): 本地 RiskManager → HTTP  │
    │  → checkStopLosses()  : 本地 RiskManager → HTTP  │
    │  → Tracker.ExecuteTrade() / RecordDailyValue()   │
    └────────────────────────────────────────────────┘
    → GenerateBacktestResult()
    → JobService.SaveSyncResult() → PostgreSQL
```

#### 三层缓存架构

| 层级 | 位置 | TTL | 用途 |
|------|------|-----|------|
| L1 | Engine.inMemoryOHLCV | 回测生命周期 | 回测期间零延迟访问 |
| L2 | Redis (DataCache) | 1h/24h | 跨请求共享 |
| L3 | PostgreSQL | 永久 | 持久化存储 |

#### 缓存问题

- L1 缓存回测结束后不清理，多次回测导致内存增长
- Redis 缓存键 `ohlcv:{symbol}:{start}:{end}` 粒度太粗，相同股票不同日期范围产生不同缓存条目

---

## 附录 E：文档与代码一致性详细审查

### E.1 API 端点不一致（高严重度）

| 不一致项 | 文档定义 | 实际代码 | 严重程度 |
|---------|---------|---------|---------|
| `POST /analyze` | SPEC.md 定义 | **代码中不存在** | 高 |
| `POST /strategies/load` | SPEC.md 定义 | 实际为 `POST /strategies/reload` | 高 |
| `POST /strategies/unload/:name` | SPEC.md 定义 | **代码中不存在** | 高 |
| `POST /signals` | SPEC.md 定义 | 实际为 `POST /strategies/:name/signals` | 高 |
| `GET /signals/:date` | SPEC.md 定义 | **代码中不存在** | 高 |
| `POST /stop_loss` | SPEC.md 定义 | 实际为 `POST /check_stoploss` | 高 |
| `GET /regime` | SPEC.md 定义 | 实际为 `POST /detect_regime` | 高 |
| `GET /ohlcv?symbol=...` | SPEC.md 定义 | 实际为 `GET /ohlcv/:symbol` | 高 |
| `GET /fundamentals?symbol=...` | SPEC.md 定义 | 实际为 `GET /fundamental/:symbol` | 高 |
| `POST /sync` | SPEC.md 定义 | 实际拆分为多个端点 | 高 |

### E.2 核心类型定义冲突

| 类型 | SPEC.md 定义 | 实际代码 | 严重程度 |
|------|-------------|---------|---------|
| `Direction` | `int` (1/-1/0) | `string` ("long"/"short"/"close"/"hold") | 高 |
| `ErrorCode` | `int` (1000+iota) | `string` ("INTERNAL"/"INVALID_INPUT"/...) | 高 |
| `Signal.Date` | `time.Time` | `interface{}` (strategy.Signal) | 中 |
| `Signal.Action` | 无 | `string` (strategy.Signal 独有) | 中 |
| `Signal.CompositeScore` | `float64` | 仅 domain.Signal 有 | 中 |

### E.3 数据模型不一致

| 类型 | SPEC.md 字段 | 实际 domain 字段 | 缺失字段 |
|------|-------------|-----------------|---------|
| Stock | Market, Sector, FloatMarketCap | Industry, ListDate | 文档缺少 Industry/ListDate，代码缺少 Market/Sector/FloatMarketCap |
| OHLCV | 基础字段 | TradeDays, LimitUp, LimitDown | 文档缺少 A 股特有字段 |
| Position | 基础字段 | MarketValue, Weight, EntryDate, BuyDate, QuantityToday, QuantityYesterday | 文档缺少多个字段 |
| Portfolio | 基础字段 | UpdatedAt | 文档缺少 UpdatedAt |

### E.4 服务状态过时

| 服务 | ARCHITECTURE.md 状态 | 实际状态 |
|------|---------------------|---------|
| risk-service | `🔲 规划中` | 已实现并运行 |
| execution-service | `🔲 规划中` | 已实现并运行 |
| strategy-service | `🔄 备用` | 已配置并运行 |

### E.5 端口不一致

| 项目 | AGENTS.md | 实际 docker-compose.yml |
|------|----------|------------------------|
| Redis | `:6377` | `6379:6379` |
| risk-service | Data Flow 未显示 | `8083:8083` |
| execution-service | Data Flow 未显示 | `8084:8084` |

### E.6 日志框架不一致

| 文档 | 描述 | 实际 |
|------|------|------|
| SPEC.md | "Structured logging with zerolog" | 正确，全部使用 zerolog |
| AGENTS.md | "Use logrus or standard log package" | **错误**，实际全部使用 zerolog，无 logrus |

---

_报告生成时间: 2026-04-10_
_审查工具: 人工 + 自动化代码扫描_
_下次审查建议: 完成 P0/P1 修复后进行复审_
