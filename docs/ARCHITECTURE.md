# 量化交易系统架构文档

_最后更新: 2026-03-24_

---

## 系统概览

```
┌─────────────────────────────────────────────────────────────┐
│                        用户 (Browser)                        │
│           http://localhost:8085/[dashboard|screen|]           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Analysis Service (:8085)                  │
│                    (Go + Gin + ZeroLog)                     │
│                                                             │
│  GET /            → index.html (回测 UI)                   │
│  GET /dashboard   → dashboard.html (控制台)                  │
│  GET /screen      → screen.html (选股器)                    │
│  GET /health      → 健康检查                                │
│  GET /api/strategies → 可用策略列表                         │
│  POST /screen     → proxy → data-service:8081/screen        │
│  GET /ohlcv/:sym  → proxy → data-service:8081/ohlcv/*      │
│  POST /backtest   → 回测引擎                                │
└─────────────────────────────────────────────────────────────┘
         │                                           │
         │  HTTP (docker network)                     │
         ▼                                           ▼
┌──────────────────────┐              ┌──────────────────────────┐
│   Data Service (:8081) │            │   Strategy Service (:8082) │
│   Go + Gin + ZeroLog   │            │   Go + Gin                │
│                        │              │                          │
│  POST /sync/ohlcv/all  │              │  (外部策略服务，HTTP API)  │
│  POST /sync/fundamentals│              └──────────────────────────┘
│  GET  /screen          │
│  GET  /fundamentals/:sym│
└──────────┬─────────────┘
           │
           ▼
┌─────────────────────────────────────────────────────────────┐
│                      PostgreSQL (:5432)                      │
│                                                             │
│  stocks              — 5491 只股票列表                       │
│  ohlcv_daily_qfq     — 1527 万条 K 线（前复权）             │
│  stock_fundamentals  — 财务数据（PE/PB/ROE 等）              │
│  trading_calendar     — 沪深交易日历                          │
└─────────────────────────────────────────────────────────────┘
```

---

## 服务端口映射

| 服务 | 容器内端口 | Host 端口 | 用途 |
|------|-----------|----------|------|
| analysis-service | 8085 | 8085 | 回测 UI + API 网关 |
| data-service | 8081 | 8081 | 数据同步 + 选股 API |
| strategy-service | 8082 | - | 外部策略服务（备用）|
| postgres | 5432 | - | 数据库 |
| redis | 6379 | - | 缓存（备用）|

---

## API 端点

### Analysis Service (8085)

```
GET  /                     — 回测首页 (index.html)
GET  /dashboard            — 控制台 (dashboard.html)
GET  /screen               — 选股器 (screen.html)
GET  /index.html           — 回测首页 (别名)

GET  /health              — 健康检查
     → {"status": "healthy", "service": "analysis-service"}

GET  /api/strategies       — 可用策略列表
     → {"strategies": [{"name": "momentum", ...}, ...]}

POST /screen               — 选股请求 (proxy → data-service)
     Body: {"filters": {"pe_max": 30, "roe_min": 0.1}, "limit": 10}
     → {"count": 5, "results": [...]}

GET  /ohlcv/:symbol        — K 线数据 (proxy → data-service)
     Query: ?start_date=YYYYMMDD&end_date=YYYYMMDD

POST /backtest             — 发起回测
     Body: BacktestRequest
     → BacktestResponse

GET  /backtest/:id/report  — 回测报告
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

---

## 策略架构

### 策略接口 (pkg/strategy/strategy.go)
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    GenerateSignals(ctx, bars, portfolio) ([]Signal, error)
}
```

### 策略列表

| 策略 | 文件 | 说明 |
|------|------|------|
| momentum | `plugins/momentum.go` | 动量策略：买强势股 |
| mean_reversion | `plugins/mean_reversion.go` | 均值回归 |
| multi_factor | `plugins/multi_factor.go` | 多因子评分（PE+ROE+动量）|
| value_screening | `plugins/value_screen.go` | 价值筛选（PE/PB/ROE 过滤 + 动量排名）|

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
  3. 处理信号 → 执行交易 (Tracker.ExecuteTrade)
  4. 更新持仓 (T+1 规则)
  5. 检查涨跌停 (涨停日禁买，跌停日禁卖)
  6. 记录每日组合价值
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
│   │   ├── engine.go    — 主引擎
│   │   └── tracker.go   — 持仓/佣金追踪
│   ├── data/
│   │   └── tushare.go  — Tushare API 封装
│   ├── domain/
│   │   └── types.go    — 核心类型（OHLCV, Trade, Position, Signal 等）
│   ├── strategy/
│   │   ├── strategy.go — 策略接口定义
│   │   ├── registry.go — 策略注册中心
│   │   └── plugins/    — 内置策略实现
│   └── storage/
│       └── postgres.go  — PostgreSQL 操作
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
