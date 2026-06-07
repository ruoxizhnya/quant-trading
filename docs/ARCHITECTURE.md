# 量化交易系统架构文档

> **Status**: Active (Reference)
> **Version:** 2.1.0 (Phase 4 AI-Native Update)
> **Last Updated:** 2026-05-05
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [VISION.md](VISION.md) (principles), [SPEC.md](SPEC.md) (API), [ROADMAP.md](ROADMAP.md) (progress)
>
> **Changelog v2.0 (Migration):**
> - 添加标准元数据头部（Status, Owner, Related）
> - 统一文档格式与 AGENTS Template v2.0 对齐
> - 添加前端架构章节（Vue SPA + Legacy HTML 双轨制）

_原最后更新: 2026-04-08 (Phase 3)_

**Phase 4 更新 (AI-Native Evolution):**
- AI 研究服务 (port 8086): 因子发现、策略生成、优化、进化、漂移检测
- 执行服务抽象: BacktestExecutionService 支持固定/浮动/无滑点模型
- 模拟交易 API: 完整订单生命周期管理 + 模拟券商
- AI 前端组件: FactorLab、StrategyWorkshop、EvolutionObs、GenealogyTree、FitnessChart
- 基因池: Factor/Strategy 基因池 + PostgreSQL 持久化
- 指标计算: IC/RankIC、换手率计算器
- 搜索优化: TPE 贝叶斯优化、遗传算法、滚动窗口验证
- 漂移检测: 均值漂移、方差漂移、分布漂移检测
- 进化算法: 种群管理 + 选择/交叉/变异算子

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
| ai-research-service | 8086 | 8086 | AI 研究服务 | ✅ 运行中 |
| postgres | 5432 | - | 数据库 | ✅ 运行中 |
| redis | 6379 | - | 缓存层 | ✅ 运行中 |
| risk-service | 8083 | 8083 | 风控服务 | ✅ 运行中 |
| execution-service | 8084 | 8084 | 执行服务 | ✅ 运行中 |

---

## 数据同步架构 (ADR-013)

> **状态**: Proposed — 详见 [ADR-013](adr/adr-013-data-sync-enhancement.md)

### 当前架构 (Phase 3 现状)

Data Service (`:8081`) 提供 13 个手动触发的同步端点，直接调用 Tushare API：

```
Browser ──POST /sync/ohlcv────► Data Service ──► Tushare API
       ──POST /sync/stocks───►   (direct call)   (200 req/min)
       ──POST /sync/... ─────►
```

**问题**: 全手动触发、无任务状态追踪、无定时调度、前端无数据管理界面。

### 目标架构 (ADR-013 提案)

引入统一同步任务队列 + 定时调度器 + 管理页面：

```
┌─────────────┐     POST /api/sync/jobs   ┌──────────────┐
│  Data Sync  │ ────────────────────────► │  Data Service│
│    UI       │  SSE /api/sync/stream     │              │
│  (Vue SPA)  │ ◄──────────────────────── │              │
└─────────────┘                           └──────────────┘
                                                 │
                    ┌────────────────────────────┘
                    ▼
            ┌──────────────┐
            │  Sync Job    │  ◄── PostgreSQL sync_jobs 表
            │   Queue      │
            └──────────────┘
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
   ┌─────────┐ ┌─────────┐ ┌─────────┐
   │ Worker  │ │ Worker  │ │ Worker  │  (goroutine pool)
   │  #1     │ │  #2     │ │  #N     │
   └────┬────┘ └────┬────┘ └────┬────┘
        │           │           │
        └───────────┼───────────┘
                    ▼
            ┌──────────────┐
            │   Tushare    │
            │     API      │
            └──────────────┘
```

### 定时同步调度

| 任务 | 默认 Cron | 说明 |
|------|-----------|------|
| OHLCV 增量同步 | `0 9 * * *` | 每日 09:00 同步前一交易日数据 |
| 财务数据同步 | `0 8 * * 1` | 每周一 08:00 同步 |
| 股票列表同步 | `0 6 1 * *` | 每月 1 日 06:00 同步 |
| 交易日历同步 | `0 0 1 1 *` | 每年 1 月 1 日同步 |
| 因子数据计算 | `0 10 * * *` | 每日 10:00 计算 |

### 新增数据库表

#### sync_jobs (同步任务队列)
```sql
id VARCHAR(64) PK                          -- UUID
job_type VARCHAR(30) NOT NULL              -- ohlcv | fundamental | stocks | ...
mode VARCHAR(20) NOT NULL DEFAULT 'incremental' -- incremental | full
status VARCHAR(20) NOT NULL DEFAULT 'pending'   -- pending | running | completed | failed | cancelled
symbols TEXT                               -- JSON 数组或 NULL(全部)
start_date DATE
end_date DATE
progress INT DEFAULT 0                     -- 0-100
total_items INT DEFAULT 0
processed_items INT DEFAULT 0
error_message TEXT
retry_count INT DEFAULT 0
max_retries INT DEFAULT 3
created_at TIMESTAMPTZ DEFAULT NOW()
started_at TIMESTAMPTZ
completed_at TIMESTAMPTZ
schedule_id VARCHAR(64)                    -- 关联定时任务配置
Indexes: idx_sj_status, idx_sj_type, idx_sj_created_at
```

#### sync_schedules (定时同步配置)
```sql
id VARCHAR(64) PK                          -- UUID
name VARCHAR(100) NOT NULL
job_type VARCHAR(30) NOT NULL
cron_expression VARCHAR(50) NOT NULL
is_enabled BOOLEAN DEFAULT TRUE
symbols TEXT                               -- 股票池配置
options JSONB DEFAULT '{}'                 -- 额外选项
last_run_at TIMESTAMPTZ
next_run_at TIMESTAMPTZ
created_at TIMESTAMPTZ DEFAULT NOW()
updated_at TIMESTAMPTZ DEFAULT NOW()
```

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

> **状态**: 18 张活跃表 (2026-05-17 清理后，从 24 张减到 18 张，已删除 10 张未使用表: backtest_data, new_share, stk_managers, stk_rewards, stock_company, trade_calendar, daily, trade_cal, market_data, stock_basic。详见 ODR-010)
>
> 完整迁移定义见 `migrations/` (12 个 SQL 文件) + `pkg/storage/postgres.go` (inline migrations)。

### 主表（核心 6 张）

#### stocks
```sql
symbol VARCHAR(20) PK
name VARCHAR(100)
exchange VARCHAR(10)  -- "SSE" | "SZSE"
industry VARCHAR(50)
market_cap BIGINT
list_date DATE
status VARCHAR(20)
```

#### ohlcv_daily_qfq
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

#### stock_fundamentals
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

#### trading_calendar
```sql
trade_date DATE PK
exchange VARCHAR(10)
is_trading_day BOOLEAN
```

#### strategies
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

### 辅助表（其余 12 张）

> 以下表用于缓存、分析、AI 研究等场景，详细 schema 见 `migrations/` 和 `pkg/storage/postgres.go`。

| 表名 | 用途 | 主要字段 |
|------|------|---------|
| `dividends` | 分红送股数据 | symbol, ex_date, cash_div, share_div |
| `splits` | 拆股数据 | symbol, ex_date, split_ratio |
| `fundamentals` | 财务数据（独立接口） | ts_code, trade_date, pe, pb, roe |
| `factor_cache` | 因子计算结果缓存 | symbol, trade_date, factor_name, value |
| `factor_returns` | 因子收益分析 | factor_name, period, return |
| `ic_analysis` | 因子 IC 分析结果 | factor_name, trade_date, ic, rank_ic, top_ic |
| `index_constituents` | 指数成分股 | index_code, symbol, in_date, out_date |
| `walk_forward_reports` | Walk-forward 分析报告 | strategy_id, train_start, train_end, metrics |
| `factor_genes` | AI 因子基因池 (Phase 4) | id, name, formula, ic_history JSONB, performance JSONB, genealogy JSONB, status |
| `strategy_genes` | AI 策略基因池 (Phase 4) | id, name, code, params JSONB, fitness JSONB, genealogy JSONB, generation, status |
| `sync_jobs` | 数据同步任务队列 (Phase 3) | id, job_type, status, payload JSONB, scheduled_at |
| `sync_schedules` | 定时同步调度 (Phase 3) | id, name, cron, job_type, is_active |

> 备注: `fundamentals` 与 `stock_fundamentals` 字段重叠但使用场景不同，未来评估合并。`orders` 表 (migrations/003 定义) 当前未被代码引用，可考虑删除。

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
- `LiveTrader` — 实盘/纸交易桥接接口（可选）

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

### 实盘桥接 (Backtest → Paper → Live)

Engine 通过 `LiveTrader` 接口实现回测到实盘的平滑过渡：

```go
// 纸交易模式：相同的回测引擎，不同的执行后端
trader := live.NewMockTrader(live.MockTraderConfig{InitialCash: 1e6}, logger)
engine.SetLiveTrader(trader)

// 单信号执行
result, err := engine.ExecuteSignalViaLiveTrader(ctx, signal, price)

// 批量信号执行（日终再平衡）
results := engine.ExecuteSignalsViaLiveTrader(ctx, signals, prices)
```

**桥接方法** (`engine.go`):
| 方法 | 说明 |
|------|------|
| `SetLiveTrader(trader)` | 附加/ detach LiveTrader |
| `GetLiveTrader()` | 获取当前 trader |
| `ExecuteSignalViaLiveTrader(ctx, signal, price)` | 单信号委托 |
| `ExecuteSignalsViaLiveTrader(ctx, signals, prices)` | 批量委托 |
| `HealthCheckLiveTrader(ctx)` | 检查 trader 健康状态 |

**执行模式对比**:
| 模式 | Engine 行为 | 用途 |
|------|------------|------|
| 纯回测 (默认) | Tracker 内部模拟 | 策略研发、历史验证 |
| 纸交易 | Tracker + MockTrader 并行 | 策略上线前实盘模拟 |
| 混合模式 | Tracker 记录 + LiveTrader 执行 | 小资金实盘验证 |
| 纯实盘 | 仅 LiveTrader 执行 | 生产环境 |

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
│   ├── live/           — 实盘交易接口与模拟实现
│   │   ├── trader.go           — LiveTrader 核心接口定义 (A-share 规则)
│   │   ├── mock_trader.go      — MockTrader 模拟交易 (T+1/印花税/过户费)
│   │   ├── trader_advanced.go  — AdvancedTrader 扩展接口 (批量/流式/保证金)
│   │   ├── advanced_mock_trader.go — AdvancedMockTrader 完整实现
│   │   ├── persistent_mock_trader.go — 持久化 MockTrader (OrderStore)
│   │   ├── order_store.go      — OrderStore 接口 (订单持久化)
│   │   ├── postgres_order_store.go — PostgreSQL 订单存储
│   │   ├── redis_order_store.go — Redis 订单缓存
│   │   └── types.go            — 扩展类型 (TradeRecord, MarketData, CashFlow)
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
│   ├── StrategyLab.vue  — 策略实验室
│   └── DataSync.vue     — 数据同步管理 (ADR-013)
├── components/          — 可复用子组件
│   ├── backtest/        — 回测引擎子组件
│   │   ├── BacktestForm.vue      — 回测参数表单
│   │   ├── MetricsCards.vue      — 指标卡片网格
│   │   ├── EquityChart.vue       — 净值曲线 (Chart.js + 交易标记)
│   │   ├── TradeTable.vue        — 交易记录表
│   │   ├── DetailMetrics.vue     — 详细指标
│   │   └── BacktestHistory.vue   — 历史记录列表
│   ├── sync/            — 数据同步子组件 (ADR-013)
│   │   ├── SyncOverviewCards.vue    — 数据概览卡片
│   │   ├── SyncControlPanel.vue     — 同步控制面板
│   │   ├── SyncJobQueue.vue         — 同步任务队列
│   │   ├── SyncLogViewer.vue        — 同步日志查看器
│   │   └── DataQualityDashboard.vue — 数据质量仪表盘
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
│   ├── backtest.ts      — 回测历史 + 结果状态
│   └── sync.ts          — 数据同步状态 (ADR-013)
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

---

## AI 研究架构 (pkg/ai/) — Phase 4

> **状态**: Active — 核心组件已实现，服务运行中
> **定位**: AI 作为资深量化研究员，通过现有回测基础设施验证假设
> **入口**: `cmd/ai/main.go` (:8086)

### 服务架构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         AI Research Service (:8086)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐│
│  │  Research   │  │  Generate   │  │  Validate   │  │      Evolve         ││
│  │   Agent     │  │   Agent     │  │   Agent     │  │      Agent          ││
│  │             │  │             │  │             │  │                     ││
│  │ • 因子假设   │  │ • 表达式生成 │  │ • 批量回测   │  │ • 遗传算法          ││
│  │ • 文献理解   │  │ • 代码生成   │  │ • IC 分析   │  │ • 漂移检测          ││
│  │ • 制度学习   │  │ • 模板填充   │  │ • 过拟合检测 │  │ • 自动重训          ││
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘│
│         │                │                │                    │          │
│         └────────────────┴────────────────┘                    │          │
│                                   │                            │          │
│                    ┌──────────────┴──────────────┐    ┌────────┴─────────┐│
│                    │      Expression Engine      │    │     Gene Pool    ││
│                    │      (DSL + AST)            │    │   (PG + JSONB)   ││
│                    └──────────────┬──────────────┘    └──────────────────┘│
│                                   │                                        │
└───────────────────────────────────┼────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Analysis Service (:8085) — Existing                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐│
│  │   Backtest  │  │   Batch     │  │   Factor    │  │    Strategy         ││
│  │   Engine    │  │   Engine    │  │  Analyzer   │  │    Registry         ││
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
```

### 核心组件

| 组件 | 文件 | 职责 | 状态 |
|------|------|------|------|
| Expression Engine | `pkg/ai/expression/` | 因子表达式 DSL 解析、AST 求值、向量化计算 | ✅ 已实现 |
| Intent Parser | `pkg/ai/intent/` | 自然语言意图解析：中文/英文 → 结构化策略参数 | ✅ 已实现 |
| YAML Generator | `pkg/ai/yaml/` | 结构化意图 → YAML 策略配置 | ✅ 已实现 |
| Pipeline | `pkg/ai/pipeline/` | 完整流水线：意图解析 → YAML → 代码生成 → 编译验证 → 回测 | ✅ 已实现 |
| Research Agent | `pkg/ai/agents/research.go` | LLM 驱动因子假设生成 | ✅ 已实现 |
| Generate Agent | `pkg/ai/agents/generate.go` | 自然语言 → 策略代码生成 | 🔄 规划中 |
| Validate Agent | `pkg/ai/agents/validate.go` | 分层验证：L1 语法 → L2 快速回测 → L3 标准回测 → L4 Walk-Forward | 🔄 规划中 |
| Evolve Agent | `pkg/ai/agents/evolve.go` | 遗传算法 + 概念漂移检测 | 🔄 规划中 |
| Gene Pool | `pkg/ai/gene_pool/` | 因子/策略基因库 (PostgreSQL JSONB) | 🔄 规划中 |
| Backtest Client | `pkg/ai/client/backtest_client.go` | HTTP 客户端调用回测 API | ✅ 已实现 |
| Factor Client | `pkg/ai/client/factor_client.go` | HTTP 客户端调用因子计算 API | ✅ 已实现 |

### 意图解析引擎 (Intent Parser)

```go
// 自然语言 → 结构化意图
type Intent struct {
    StrategyType    string            // momentum | mean_reversion | breakout | ...
    StrategyName    string            // snake_case name
    Parameters      []Parameter       // 提取的参数 (lookback_days, rsi_threshold, ...)
    Indicators      []string          // 技术指标 (rsi, macd, ma, bollinger, ...)
    Universe        string            // csi300 | csi500 | csi800 | all
    Timeframe       string            // 1d | 1w | 1M
    RiskConstraints *RiskConstraints  // 止损/止盈/最大回撤/最大持仓
}

// 支持中文/英文混合输入
// "20日动量策略，在沪深300中选出最强10只股票，止损5%"
// "RSI mean reversion, oversold 30, csi500, max drawdown 10%"
```

### YAML 配置生成器 (YAML Generator)

```go
// 结构化意图 → 完整 YAML 配置
type Config struct {
    Strategy    StrategyConfig    // name, type, parameters, indicators
    Backtest    BacktestConfig    // start_date, end_date, initial_capital, commission
    Data        DataConfig        // universe, timeframe, providers
    Risk        RiskConfig        // max_positions, stop_loss, take_profit
    Execution   ExecutionConfig   // order_type, price_tolerance
}
```

### 策略生成流水线 (Pipeline)

```
用户输入: "20日动量策略，沪深300，止损5%"
    ↓
[Intent Parser] 提取结构化参数
    ↓
[YAML Generator] 生成策略配置
    ↓
[LLM CodeGen] 生成 Go 策略代码
    ↓
[Compiler] 验证代码可编译
    ↓
[Backtest] 运行快速回测验证
    ↓
返回: {intent, yaml, code, backtest_result}
```

### 表达式引擎 (Factor Expression DSL)

```go
type FactorExpression struct {
    ID       string
    Formula  string      // e.g., "ts_corr(close, volume, 20) / ts_std(returns, 60)"
    AST      *ExprNode   // Parsed AST
    Inputs   []string    // Required raw data fields
    Category string      // "momentum" | "value" | "quality" | "custom"
}

// Supported operators
// Time-series: ts_mean, ts_std, ts_corr, ts_delay, ts_rank, ts_delta
// Cross-section: cs_rank, cs_zscore, cs_percentile
// Math: log, sqrt, abs, sign
// Data fields: open, high, low, close, volume, turnover, market_cap, pe, pb, roe
```

### 验证分层

| 层级 | 目的 | 数据量 | 时间 | 淘汰率 |
|------|------|--------|------|--------|
| L1 语法检查 | 确保表达式可解析 | — | < 1s | 10% |
| L2 快速回测 | 筛选明显劣策略 | 1年/100股 | < 10s | 70% |
| L3 标准回测 | 全面绩效评估 | 3年/500股 | < 2min | 15% |
| L4 Walk-Forward | 过拟合检测 | 5年/全市场 | < 10min | 4% |
| L5 人类审核 | 最终决策 | — | — | 1% |

### 前端 AI 模块

```
web/src/components/ai/
├── FactorLab.vue           # 因子实验室：发现、验证、可视化
├── StrategyWorkshop.vue    # 策略工坊：生成、编辑、回测
├── EvolutionObs.vue        # 进化观察室：种群、谱系、漂移
├── FactorCard.vue          # 因子卡片
├── StrategyCard.vue        # 策略卡片
├── GenealogyTree.vue       # 策略谱系树
└── FitnessChart.vue        # 适应度进化曲线
```

### 新增页面

```
web/src/pages/
└── AIResearch.vue          # AI 研究主页面 (整合 FactorLab + StrategyWorkshop + EvolutionObs)
```
