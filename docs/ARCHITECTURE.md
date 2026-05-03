# 量化交易系统架构文档

> **Status**: Active (Reference)
> **Version:** 2.0.0 (AGENTS Template v2.0 Migration)
> **Last Updated:** 2026-04-11
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [VISION.md](VISION.md) (principles), [SPEC.md](SPEC.md) (API), [ROADMAP.md](ROADMAP.md) (progress)
>
> **Changelog v2.0 (Migration):**
> - 添加标准元数据头部（Status, Owner, Related）
> - 统一文档格式与 AGENTS Template v2.0 对齐
> - 添加前端架构章节（Vue SPA + Legacy HTML 双轨制）

_原最后更新: 2026-04-08 (Phase 3)_

**Phase 3 更新:**
- Event-Driven 数据管道 (pkg/marketdata/eventbus.go + provider 接口)
- 多数据源适配器: Tushare / AkShare / Postgres / HTTP / Cached (Redis)
- 因子缓存预热: Engine 自动从 factor_cache 表加载 z-score，注入 FactorZScoreReader
- 限价单支持: strategy.Signal 增加 OrderType/LimitPrice，Tracker 按日内高低价判断成交
- 股息/送股处理: Tracker.ProcessDividend + ProcessSplit，Engine 日循环自动处理
- 指数成分股股票池: BacktestRequest.IndexCode，从 index_constituents 按日期加载
- 实盘接口预留: pkg/live/ (LiveTrader 接口 + MockTrader 实现)
- 新策略: TD Sequential / Bollinger MR / Volume-Price Trend / Vol Breakout
- 批量回测框架: pkg/backtest/batch.go + walkforward.go

**Phase 2.5 更新:**
- 新增 `pkg/errors` 统一错误处理模块
- 新增 ATR StopLoss 风控组件 (pkg/risk/stoploss.go)
- 策略接口统一化 (pkg/strategy/strategy.go)
- 测试覆盖大幅提升 (>55 cases)

---

## 系统概览

```
┌─────────────────────────────────────────────────────────────┐
│                        用户 (Browser)                        │
│              Vue SPA: http://localhost:5173                  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Analysis Service (:8085)                  │
│                    (Go + Gin + ZeroLog)                     │
│                                                             │
│  GET  /health              → 健康检查                       │
│  GET  /api/health          → API 健康检查                   │
│  GET  /api/v1              → API 信息                       │
│  POST /api/backtest        → 回测引擎 (同步/异步)            │
│  GET  /api/backtest        → 回测任务列表                    │
│  GET  /api/backtest/:id    → 回测任务状态                    │
│  GET  /api/backtest/:id/report → 回测报告                   │
│  GET  /api/strategies      → 策略列表                        │
│  POST /api/copilot/generate → AI 策略生成                   │
│  GET  /api/datasource/status → 数据源状态                    │
│  GET  /api/factor/list     → 因子列表                        │
│  GET  /ohlcv/:sym          → proxy → data-service:8081     │
│  POST /screen              → proxy → data-service:8081     │
└─────────────────────────────────────────────────────────────┘
         │              │              │
         │              │              │
         ▼              ▼              ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ Data Service │ │Risk Service  │ │Execution Svc │
│   (:8081)    │ │  (:8083)     │ │  (:8084)     │
└──────┬───────┘ └──────┬───────┘ └──────┬───────┘
       │                │                │
       └────────────────┴────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                      PostgreSQL (:5432)                      │
│                                                             │
│  stocks              — 5491 只股票列表                       │
│  ohlcv_daily_qfq     — 1527 万条 K 线（前复权）             │
│  stock_fundamentals  — 财务数据（PE/PB/ROE 等）              │
│  trading_calendar     — 沪深交易日历                          │
│  backtest_jobs        — 异步回测任务队列                      │
└─────────────────────────────────────────────────────────────┘
                          ▲
                          │
┌─────────────────────────────────────────────────────────────┐
│                      Redis (:6379)                           │
│                                                             │
│  factor_cache         — 因子缓存                             │
│  session_store        — 会话存储                             │
└─────────────────────────────────────────────────────────────┘
```

---

## 服务端口映射

| 服务 | 容器内端口 | Host 端口 | 用途 | 状态 |
|------|-----------|----------|------|------|
| analysis-service | 8085 | 8085 | 回测 API 网关 | ✅ 运行中 |
| data-service | 8081 | 8081 | 数据同步 + 选股 API | ✅ 运行中 |
| strategy-service | 8082 | - | 外部策略服务（备用）| 🔄 备用 |
| postgres | 5432 | - | 数据库 | ✅ 运行中 |
| redis | 6379 | - | 缓存层 | ✅ 运行中 |
| risk-service | 8083 | 8083 | 风控服务 | ✅ 运行中 |
| execution-service | 8084 | 8084 | 执行服务 | ✅ 运行中 |

---

## API 端点

### Analysis Service (8085)

> 完整 API 定义见 [SPEC.md](SPEC.md)

```
GET  /health              — 健康检查
     → {"status": "healthy", "service": "analysis-service"}

GET  /api/v1              — API 信息和端点列表

# Backtest
POST /backtest             — 发起回测（同步或异步）
GET  /backtest?limit=20    — 回测任务列表
GET  /backtest/:id         — 回测任务状态
GET  /backtest/:id/report  — 回测报告
GET  /backtest/:id/trades  — 交易记录
GET  /backtest/:id/equity  — 净值曲线数据

# Data Proxies (→ data-service :8081)
GET  /ohlcv/:symbol        — K 线数据
POST /screen               — 选股请求
GET  /stocks/count         — 股票计数
GET  /market/index         — 市场指数
POST /sync/calendar        — 交易日历同步
GET  /api/v1/trading/calendar — 交易日历查询

# Strategy Management
GET    /api/strategies     — 策略列表
POST   /api/strategies     — 创建策略
GET    /api/strategies/:id — 策略详情
PUT    /api/strategies/:id — 更新策略
DELETE /api/strategies/:id — 删除策略

# AI Copilot
POST /api/copilot/generate        — AI 策略生成
GET  /api/copilot/generate/:job_id — 轮询生成结果
GET  /api/copilot/stats           — Copilot 统计
POST /api/copilot/save            — 保存策略代码

# Data Source Management
GET  /api/datasource/status       — 数据源状态
POST /api/datasource/switch       — 切换数据源
GET  /api/datasource/health       — 数据源健康检查

# Factor Analysis
GET  /api/factor/returns/:factor  — 因子收益时间序列
GET  /api/factor/ic/:factor       — 因子 IC 时间序列
POST /api/factor/compute-returns  — 计算因子收益
POST /api/factor/compute-ic       — 计算因子 IC
GET  /api/factor/list             — 列出可用因子

# Legacy HTML (deprecated — use Vue SPA instead)
GET  /, /screen, /dashboard, /copilot, /strategy-selector
```

### Data Service (8081)

```
POST /sync/ohlcv              — 同步单只 K 线
POST /sync/ohlcv/all          — 全量 K 线同步
POST /sync/fundamentals       — 同步财务数据
     Body: {"symbols": ["600519.SH"], "date": "20240930"}

GET  /fundamentals/:symbol   — 最新财务数据
GET  /fundamentals/:symbol/history — 历史财务数据

POST /screen                  — 选股筛选
     Body: {"filters": ScreenFilters, "date": "YYYYMMDD", "limit": 50}
     → {"count": N, "results": [ScreenResult]}
```

---

## 数据模型

### stocks
```sql
symbol VARCHAR(20) PK
name VARCHAR(100)
exchange VARCHAR(10)  -- "SSE" | "SZSE"
industry VARCHAR(50)
market_cap BIGINT
list_date DATE
status VARCHAR(20)
```

### ohlcv_daily_qfq
```sql
symbol VARCHAR(20) PK
trade_date DATE PK
open FLOAT
high FLOAT
low FLOAT
close FLOAT
volume BIGINT
turnover FLOAT
```

### stock_fundamentals
```sql
id SERIAL PK
ts_code VARCHAR(20) + trade_date DATE → UNIQUE
trade_date DATE
pe FLOAT          -- 市盈率（可能为 NULL）
pb FLOAT           -- 市净率
ps FLOAT           -- 市销率
roe FLOAT          -- 净资产收益率
roa FLOAT          -- 总资产收益率
debt_to_equity FLOAT
gross_margin FLOAT
net_margin FLOAT
revenue_growth FLOAT
net_profit_growth FLOAT
```

### trading_calendar
```sql
trade_date DATE PK
exchange VARCHAR(10)
is_trading_day BOOLEAN
```

### strategies
```sql
id SERIAL PK
strategy_id VARCHAR(50) UNIQUE NOT NULL  -- 如 "momentum", "value_momentum"
name VARCHAR(100) NOT NULL
description TEXT
strategy_type VARCHAR(30) NOT NULL        -- "trend_following" | "mean_reversion" | ...
params JSONB NOT NULL DEFAULT '{}'       -- 策略参数配置
is_active BOOLEAN NOT NULL DEFAULT TRUE
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
Indexes: idx_strategies_type, idx_strategies_active
```

### backtest_jobs (异步回测任务队列 + 结果持久化)
```sql
id VARCHAR(64) PK                          -- UUID
strategy_id VARCHAR(50) NOT NULL           -- 策略名
params JSONB NOT NULL DEFAULT '{}'         -- 回测参数
universe VARCHAR(100) NOT NULL             -- 股票池（逗号分隔）
start_date DATE NOT NULL                   -- 回测起始日期
end_date DATE NOT NULL                     -- 回测结束日期
status VARCHAR(20) NOT NULL DEFAULT 'pending' -- pending | running | completed | failed
result JSONB                               -- 完整回测结果（BacktestResponse JSON）
error_message TEXT                         -- 失败时的错误信息
created_at TIMESTAMPTZ DEFAULT NOW()       -- 创建时间
started_at TIMESTAMPTZ                      -- 开始执行时间
completed_at TIMESTAMPTZ                    -- 完成时间
Indexes: idx_bj_status, idx_bj_created_at
```

---

## 缓存设计 (Redis)

> **来源**: 整合自 CACHE.md (2026-04-11)

采用两层 cache-aside 架构，将回测延迟从分钟级降至 <5s。

```
Backtest Engine
    │
    ▼
pkg/data/cache.go  ← cache-aside facade
    │
    ├── Redis (pkg/storage/cache.go)   ← L1: hot data
    └── PostgreSQL (pkg/storage/postgres.go) ← L2: source of truth
```

### Redis Key 设计

| 数据 | Key Pattern | TTL |
|------|-------------|-----|
| OHLCV bars | `ohlcv:{symbol}:{start}:{end}` | 1h (recent), 24h (historical) |
| Fundamentals | `fund:{symbol}:{date}` | 24h |
| Stock list | `stocks:all` | 24h |

- Keys 使用 YYYYMMDD 日期格式
- OHLCV TTL 对近期数据（最近 7 天）较短，保持实时性；历史数据较长

### L1 — Redis (`pkg/storage/cache.go`)

底层 Redis 封装，使用 `go-redis/v9`：
- `Get` / `SetEX` — 带 TTL 的原始字节操作
- `CacheOHLCV` / `GetCachedOHLCV` — 领域级 OHLCV 缓存
- `CacheStocks` / `GetCachedStocks` — 股票列表缓存
- `Ping` — 健康检查

### L2 — Cache-Aside Facade (`pkg/data/cache.go`)

`DataCache` 封装 Redis + PostgreSQL：

```go
// 先查 Redis → miss 时查 PostgreSQL → 缓存结果
func (dc *DataCache) GetOHLCV(ctx, symbol, start, end string) ([]domain.OHLCV, error)
func (dc *DataCache) SetOHLCV(ctx, symbol, start, end string, bars []domain.OHLCV) error
func (dc *DataCache) GetFundamentals(ctx, symbol, date string) ([]domain.Fundamental, error)
```

TTL 自动选择：
- 最近 7 天数据 → 1h TTL
- 更早数据 → 24h TTL

---

## 策略架构

### 策略接口 (pkg/strategy/strategy.go)
> **Canonical definition** — matches [SPEC.md](SPEC.md) and source code
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]Signal, error)
    Weight(signal Signal, portfolioValue float64) float64
    Cleanup()
}
```

### 策略列表

| 策略 | 文件 | 说明 |
|------|------|------|
| momentum | `plugins/momentum.go` | 动量策略：买强势股 |
| mean_reversion | `plugins/mean_reversion.go` | 均值回归 |
| multi_factor | `plugins/multi_factor.go` | 多因子评分（PE+ROE+动量），支持 FactorAware 缓存 |
| value_screening | `plugins/value_screen.go` | 价值筛选（PE/PB/ROE 过滤 + 动量排名）|
| td_sequential | `plugins/new_strategies.go` | TD Sequential：Tom DeMark 趋势衰竭指标 |
| bollinger_mr | `plugins/new_strategies.go` | 布林带均值回归：限价单买入（下轨挂单） |
| vpt | `plugins/new_strategies.go` | 量价趋势：成交量确认价格突破 |
| vol_breakout | `plugins/new_strategies.go` | 波动率突破：ATR 通道突破 |

### 策略加载流程
1. `plugins/` 包通过 `init()` 自动注册到 `strategy.GlobalRegistry`
2. `analysis-service` 启动时 `import _ "pkg/strategy/plugins"` 触发注册
3. 回测时 engine 先查本地 registry，fallback 到外部 strategy-service

---

## 回测引擎架构 (pkg/backtest/)

### 核心组件
- `Engine` — 主引擎，协调各组件
- `Tracker` — 持仓追踪（T+1、佣金、印花税）
- `Signal` → `Trade` — 信号转换为交易

### 回测流程
```
每日循环:
  1. 获取当日 K 线数据 (marketDataCache)
  2. 获取信号 (getSignals → 本地 registry 或外部 service)
     - 若策略实现 FactorAware，注入 FactorZScoreReader
  3. 处理信号 → 执行交易 (Tracker.ExecuteTrade)
     - 限价单: 检查日内低/高价是否触及 LimitPrice
     - 市价单: 按当日收盘价成交
  4. 更新持仓 (T+1 规则)
  5. 检查涨跌停 (涨停日禁买，跌停日禁卖)
  5.5 处理股息/送股 (ProcessDividend / ProcessSplit)
  6. 记录每日组合价值
  7. AdvanceDay (T+1 滚动)
```

### 佣金计算规则
- 买入：value × 0.0003（最低 5 元）+ value × 0.00001（过户费）
- 卖出：value × 0.0003 + value × 0.00001 + value × 0.001（印花税）

### T+1 规则
- `QuantityYesterday` — 昨日持仓（可今日卖出）
- `QuantityToday` — 今日买入（明日才可卖出）

---

## 前复权计算 (pkg/data/tushare.go)

```
前复权收盘价 = 不复权收盘价 × (latest_adj_factor / adj_factor_at_date)
```

流程：
1. 调用 `daily` API 获取不复权 K 线
2. 调用 `adj_factor` API 获取复权因子历史
3. 以最新复权因子为基准，向前回算

---

## 目录结构

```
quant-trading/
├── cmd/
│   ├── analysis/        — 回测 UI 服务 (:8085)
│   ├── data/           — 数据同步服务 (:8081)
│   └── strategy/       — 外部策略服务 (:8082, 备用)
├── pkg/
│   ├── backtest/       — 回测引擎
│   │   ├── engine.go    — 主引擎 (因子缓存预热 + 股息/送股 + 指数成分股)
│   │   ├── tracker.go   — 持仓/佣金追踪 (限价单 + ProcessDividend/ProcessSplit)
│   │   ├── batch.go     — 批量回测框架
│   │   ├── walkforward.go — Walk-Forward 验证
│   │   └── job.go       — 异步回测任务 (混合context模式: Background+parent监控)
│   ├── data/
│   │   ├── tushare.go  — Tushare API 封装
│   │   ├── factor.go   — 因子计算 + 缓存
│   │   └── factor_attribution.go — 因子归因分析
│   ├── domain/
│   │   └── types.go    — 核心类型（OHLCV, Trade, Position, Signal, OrderType 等）
│   ├── live/           — 实盘接口预留
│   │   ├── trader.go    — LiveTrader 接口定义
│   │   └── mock_trader.go — MockTrader 模拟交易实现
│   ├── marketdata/     — Event-Driven 数据管道
│   │   ├── eventbus.go  — DataEventBus (pub/sub)
│   │   ├── provider.go  — Provider 接口
│   │   ├── tushare_provider.go
│   │   ├── akshare_provider.go
│   │   ├── postgres_provider.go
│   │   ├── http_provider.go
│   │   └── cached_provider.go — Redis 缓存装饰器
│   ├── risk/           — 风控模块
│   │   ├── manager.go
│   │   ├── regime.go
│   │   ├── stoploss.go  — ATR StopLoss
│   │   └── volatility.go
│   ├── strategy/
│   │   ├── strategy.go — 策略接口 + FactorAware + Signal(OrderType/LimitPrice)
│   │   ├── registry.go — 策略注册中心
│   │   ├── copilot.go  — AI Copilot 服务
│   │   ├── db.go       — 策略配置 CRUD
│   │   └── plugins/    — 内置策略实现
│   └── storage/
│       ├── postgres.go  — PostgreSQL 操作 (含 GetDividendsInRange/GetIndexConstituentsByDate)
│       └── cache.go     — Redis 缓存
├── docker-compose.yml
└── config/
    └── config.yaml
```

---

## 工作流程规范（写→审→测）

每个功能任务必须经过：

1. **Coding Agent** — 实现功能，提交 push
2. **Review Agent** — 审查代码，修复 bug
3. **Test Agent** — 单元测试，覆盖率 ≥ 80%
4. **CEO 复核** — 确认符合高级需求

详见: `~/.openclaw/workspace/PRINCIPLES.md`

---

## 前端架构 (Vue 3 SPA)

> **定位**: Vue 3 SPA 是唯一正式前端，`cmd/analysis/static/` 中的 legacy HTML 已 deprecated

### 技术栈

| 层 | 技术 | 用途 |
|----|------|------|
| 框架 | Vue 3 + Composition API | 响应式 UI |
| 语言 | TypeScript 5.x | 类型安全 |
| 构建 | Vite 6.x | 开发服务器 + HMR |
| UI 库 | Naive UI (dark theme) | 组件库 |
| 图表 | Chart.js 4.x | 净值曲线 + 交易标记 |
| 状态管理 | Pinia | 全局状态 (backtest store) |
| 路由 | Vue Router 4 | SPA 导航 |
| HTTP | fetch wrapper | API 客户端 |

### 页面结构

```
web/src/
├── App.vue              — 根组件 (Provider 层)
├── main.ts              — 入口
├── api/                 — API 客户端层
│   ├── client.ts        — fetch 封装 + 错误处理
│   ├── backtest.ts      — 回测 API
│   ├── market.ts        — 市场 API
│   └── strategy.ts      — 策略 API
├── pages/               — 页面组件 (编排容器, ~100-200行)
│   ├── Dashboard.vue    — 控制台 (编排: MarketMetrics + QuickBacktest + NavTiles + ConsoleHistory)
│   ├── BacktestEngine.vue — 回测引擎 (编排: BacktestForm + MetricsCards + EquityChart + TradeTable + DetailMetrics + BacktestHistory)
│   ├── Screener.vue     — 选股器
│   ├── Copilot.vue      — AI Copilot
│   └── StrategyLab.vue  — 策略实验室
├── components/          — 可复用子组件
│   ├── backtest/        — 回测引擎子组件
│   │   ├── BacktestForm.vue      — 回测参数表单
│   │   ├── MetricsCards.vue      — 指标卡片网格
│   │   ├── EquityChart.vue       — 净值曲线 (Chart.js + 交易标记)
│   │   ├── TradeTable.vue        — 交易记录表
│   │   ├── DetailMetrics.vue     — 详细指标
│   │   └── BacktestHistory.vue   — 历史记录列表
│   └── dashboard/       — 控制台子组件
│       ├── MarketMetrics.vue     — 市场概览 (指数数据)
│       ├── QuickBacktest.vue     — 快速回测表单
│       ├── NavTiles.vue          — 导航磁贴
│       └── ConsoleHistory.vue    — 控制台历史
├── composables/         — 组合式函数
│   └── useBacktestChart.ts       — Chart.js 渲染逻辑 (创建/销毁/采样/标记)
├── components/layout/   — 布局组件
│   ├── AppLayout.vue    — 主布局 (sidebar + header + content)
│   ├── AppSidebar.vue   — 侧边导航
│   └── AppHeader.vue    — 顶部栏
├── stores/              — Pinia stores
│   └── backtest.ts      — 回测历史 + 结果状态
├── types/               — TypeScript 接口定义
│   └── api.ts           — API 响应类型
├── utils/               — 工具函数
│   └── format.ts        — 格式化 (百分比, 数字)
└── styles/              — 全局样式
    ├── variables.css    — CSS 变量 (暗色主题)
    └── global.css       — 全局样式
```

### 与后端通信

```
Browser (:5173)                    Backend (:8085)
┌─────────────┐                   ┌──────────────┐
│ Vue SPA     │  ───HTTP────▶    │ analysis-svc │
│             │                  │              │
│ api/client  │  GET  /health    │ /health      │
│ api/market  │  GET  /stocks/*  │ /stocks/*    │
│ api/backtest│  POST /backtest  │ /backtest    │
│ api/strategy│  GET  /strategies│ /strategies   │
└─────────────┘                   └──────────────┘
```

开发模式: Vite dev server proxy → `http://localhost:8085`
生产模式: `web/dist/` 由 Nginx 托管, proxy 到后端

### Legacy HTML (deprecated)

`cmd/analysis/static/*.html` 是早期原型，功能已被 Vue SPA 完全替代。
保留原因: 部分后端测试仍引用这些静态文件。计划在 Phase 3 移除。
