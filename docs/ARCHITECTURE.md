---
status: evergreen
type: reference
last-verified: 2026-09-21
verified-by: 代码审查（2026-09-16）；AUD-14 校准（2026-09-21）—— 顶层定位/执行载体改 ADR-023/024、表数口径改「内联 DDL 37 张（唯一执行路径）」、删掉已删除/已取消的服务行、标注非表项
---

# 架构参考（Reference）

> **本文是 Reference 类文档**：干、全、与代码严格对应，用于**查阅**（端口 / API / 表结构 / 缓存 / 引擎细节）。
>
> **不适合入门**：先读 [README.md](README.md) 导航 → [PRODUCT.md](PRODUCT.md)（是什么）→ 本文顶部「架构总览」（怎么搭）。
>
> 设计原则见 [VISION.md](VISION.md) · API 契约见 [SPEC.md](SPEC.md) · 待办见 [TASKS.md](TASKS.md)。

_原文截至 2026-04-08 (Phase 3)，2026-09-16 补充架构总览与现状对比。_

**Phase 4 更新 (AI-Native Evolution):**
- AI 研究服务 (port 8086): 因子发现、策略生成、优化、进化、漂移检测
- 执行服务抽象: BacktestExecutionService 支持固定/浮动/无滑点模型
- 模拟交易 API: 完整订单生命周期管理 + 模拟券商
- AI 前端组件: ~~FactorLab、StrategyWorkshop、EvolutionObs、GenealogyTree、FitnessChart~~
  — ⚠️ **已删除**（P1-13 创建后 S7-P2-7 作为死代码删除，详见 [ODR-045](archive/odr/odr-045-frontend-ai-component-deprecation.md)）；自主研究改由 Hermes Agent + MCP 工具 + 自然语言交互提供（[ODR-046](archive/odr/odr-046-hermes-agent-integration-decision.md)）
- 基因池: Factor/Strategy 基因池 + PostgreSQL 持久化
- 指标计算: IC/RankIC、换手率计算器
- 搜索优化: TPE 贝叶斯优化、遗传算法、滚动窗口验证
- 漂移检测: 均值漂移、方差漂移、分布漂移检测
- 进化算法: 种群管理 + 选择/交叉/变异算子
- **实验室定位 (Accepted, ADR-023)**: 一间**单人自托管量化研究实验室** —— AI 实验员（Hermes，唯一编排者）操作底座，人当实验室主任。三层模型：AI 编排层（L3）/ 能力层（L2，禁止 AI 直接摸数据）/ 数据层（L1）。EquityDeep 从「工作面」**降为数据底座**（产业链图谱 + 研究洞察）。见 [ADR-023](adr/adr-023-ai-experimenter-lab.md)（取代 [ADR-022](archive/superseded-adr/adr-022-unified-research-platform.md) / [ADR-021](archive/superseded-adr/adr-021-equitydeep-research-layer.md)，两者均从未实施）
- **执行载体 = 表达式 (Accepted, ADR-024)**: 意图 → YAML → `ExpressionStrategy`；LLM 生成的 Go 代码只是**可审阅 artifact**，不加载、不执行；不做 `plugin.Open`。见 [ADR-024](adr/adr-024-expression-as-execution-target.md)

**Phase 3 更新:**
- Event-Driven 数据管道 (pkg/marketdata/eventbus.go + provider 接口)
- 多数据源适配器: Postgres / HTTP / InMemory / Cached (Redis)（原 `pkg/marketdata` 的 Tushare / AkShare **直连** provider 已于 ADR-022 §1 退役，外部源统一经 L0 单一摄取入口，见 [ODR-058](archive/odr/odr-058-p5-1-retire-direct-providers.md)）
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

## 架构总览（三层模型）

> **这是目标形态。** 当前实现尚未完全对齐，差距见本节末尾的对照表。

```
L3  AI 编排层    AI 实验员（唯一编排者，会循环） · 因果审查（被调用，不循环）
                 ↓ 经 MCP 工具桥调用    ↑ 返回结果
L2  能力层       [MCP Tool Bridge] → 因子计算 | 回测 | 产业链查询 | 证据查询 | 验证器链×5
                 ↓ 读取事实            ↑ 写入产物
L1  数据层       行情 | 财务 | 产业链图谱 | 研究洞察 | 实验日志
                                                      ↑ 由 L3 产生、经 L2 写入
```

| 层 | 职责 | 关键约束 |
|---|---|---|
| **L1 数据层** | 事实 + 执行产物，只增不改 | PIT 正确性、质量门禁、单一摄取入口 |
| **L2 能力层** | **确定性**、可组合（可写执行产物）。含 5 个验证器（统计/经济/稳健/偏差/冗余） | **禁止 AI 直接摸数据**——本层存在的意义就是拦住 AI 直接读库 / 读文件 |
| **L3 AI 编排层** | 试错、搜索、**因果审查** | **唯一需要人在环的一层**；会自主循环的只有实验员 |

**分界线是"能否无人值守"，不是"静态 vs 动态"，也不是"有无状态"**：L1/L2 给定输入必有确定输出，可自动测试、可复现；L3 会失败，必须可监督。
注意 L2 **可以有副作用**（回测写 `backtest_jobs`、因子写 `factor_cache`）——确定性指的是**行为可复现**，不是无状态。

**三个必须记住的位置**：

- **MCP 工具桥属于 L2**，是 AI 调用能力层的唯一通道（AI 不得绕过它直连能力实现）。现状 19 个工具已暴露于 `/api/tools`，但**尚无 agent driver 循环调用**（见 TASKS P1-2）。
- **实验日志落在 L1**：由 L3 产生、经 L2 写入，被验证器与审阅台共同读取。它是过拟合检测、路径回放、复盘的**共同数据源**——不是附属功能，是循环的必要产物。
- **验证器不是第二个 agent**：它**被调用**、不自主循环。六个检验维度中五个（统计 / 经济 / 稳健 / 偏差 / 冗余）是纯计算，落在 **L2**；只有"因果"需语义理解，落在 **L3**。见 [ADR-023 §4](adr/adr-023-ai-experimenter-lab.md)。

### 现状 vs 目标（2026-09-16）

| 层 | 现状 | 目标 |
|---|---|---|
| **L3** | 实验员循环控制器（`pkg/ai/loop`）+ 验证器链六维（含因果审查，P2-9 已接线）+ Explore 干预页已落地；~~`cmd/ai` 仅 2 个端点~~ 已于 2026-09-18 删除（P2-5，零调用方）。策略执行载体按 ADR-024 改为 YAML → ExpressionStrategy（确定性引擎），不再生成并加载 Go 代码 | 验证器链接真实回测的端到端取证（缺数据，P2-13） |
| **L2** | 19 个 MCP 工具已暴露于 `/api/tools`，但**无 agent driver 循环调用**；产业链查询、证据查询未接通 | 被 AI 循环调用；产业链 / 证据能力补全 |
| **L1** | `ingest.raw`（content_hash 主键）与 `research` schema 已建，但 DDL 硬编码在 `pkg/storage/postgres.go`，`migrations/` 无版本管理；`market` / `quant` schema 未建 | schema 收口 + 版本管理 + PIT 修正 + 宏观/跨境与产业链接入 |

> ⚠️ **下文各章节是"当前实现的 Reference 细节"**，可能领先或落后于目标形态。**以代码为准**；若发现本文与代码不符，改代码或改本文，不要两边都留着。

---

## 系统概览

> 下图是**当前实现视图**（服务拓扑）。目标形态见上文 §架构总览。

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
│  POST /api/risk/*         → in-process risk.RiskManager    │
│  POST /api/execution/*    → in-process live.MockTrader     │
└─────────────────────────────────────────────────────────────┘
         │
         │  (ODR-021: risk + execution merged in-process)
         ▼
┌──────────────┐
│ Data Service │
│   (:8081)    │
└──────┬───────┘
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
| analysis-service | 8085 | 8085 | 回测 API 网关 + in-process risk/execution | ✅ 运行中 |
| data-service | 8081 | 8081 | 数据同步 + 选股 API | ✅ 运行中 |
| strategy-service | 8082 | - | 外部策略服务（备用）| 🔄 备用 |
| ~~ai-research-service~~ | ~~8086~~ | - | ~~AI 研究服务~~ — **已删除**（2026-09-18，TASKS P2-5）：零调用方且建在废弃交互层上 | ❌ 不存在 |
| postgres | 5432 | - | 数据库 | ✅ 运行中 |
| redis | 6379 | - | 缓存层 | ✅ 运行中 |
| ~~equitydeep-research~~ | - | - | ~~工作面 1 纵向研究 worker（Python 3.11）~~ — **已取消**：ADR-023 把 EquityDeep 降为数据底座，不再是独立 worker | ❌ 已取消 (ADR-023) |

> **ODR-021 (P1-15, 2026-06-12)**: `risk-service(8083)` + `execution-service(8084)`
> 已合并入 `analysis-service` 作为 in-process 组件（`risk.RiskManager` +
> `live.MockTrader`）。端点暴露在 `/api/risk/*` 和 `/api/execution/*`
> （均带 legacy 无 `/api` 前缀的别名）。docker-compose 服务数 7 → 5。

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

# Backtest (CR-12, ODR-012: paths now use /api/backtest/* prefix)
POST /api/backtest          — 发起回测（同步或异步）
GET  /api/backtest?limit=20 — 回测任务列表
GET  /api/backtest/:id      — 回测任务状态
GET  /api/backtest/:id/report — 回测报告
GET  /api/backtest/:id/trades — 交易记录
GET  /api/backtest/:id/equity — 净值曲线数据
# Legacy aliases (registered as 301 redirects to /api/backtest/*)
POST /backtest              — legacy alias
GET  /backtest              — legacy alias
GET  /backtest/:id          — legacy alias
GET  /backtest/:id/report   — legacy alias
GET  /backtest/:id/trades   — legacy alias
GET  /backtest/:id/equity   — legacy alias

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
GET  /api/datasource/health       — 数据源健康检查
# (POST /api/datasource/switch 已于 ODR-059 退役; 读源由启动期 data_service.url 固定)

# Factor Analysis
GET  /api/factor/returns/:factor  — 因子收益时间序列
GET  /api/factor/ic/:factor       — 因子 IC 时间序列
POST /api/factor/compute-returns  — 计算因子收益
POST /api/factor/compute-ic       — 计算因子 IC
GET  /api/factor/list             — 列出可用因子

# Tools Registry (S7-P3-3, ODR-043) — 对外工具提供方
GET  /api/tools              — 列出所有工具 + schema
GET  /api/tools/:name        — 单工具 schema
POST /api/tools/:name        — 执行工具 (body: {"args": {...}})

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

> **状态（2026-09-21 校准，AUD-14）**: **内联 DDL 37 张** ——
> `pkg/storage/postgres.go` 的 `migrate()` 数组，grep `CREATE TABLE IF NOT EXISTS`
> 实测。**这是唯一的执行路径，也是唯一该引用的数字。**
>
> ⚠️ **「内联 N 张 + 迁移新增 M 张 = 总数」这个口径不成立。**
> `migrations/` 与 `docs/migrations/` 里的 `.sql` **不被执行**
> （`migration_manager.go` 已于 2026-09-18 删除，零调用方；见
> [`../migrations/README.md`](../migrations/README.md)）。迁移那一半从来不参与建表，
> 加进去只是把两个不相干的数拼成一个看起来合理的数 —— 而且会掩盖「ETL 要写的表
> 压根没被创建」这类真问题（C5 就是这么埋了几个月）。
>
> **本行出现过的历史数字都是各自时点的快照，别照抄**：
> `32 张 (14+18)` → `38 张 (20+18)`（ODR-056）→ `39 张`（ODR-053）→
> 审查报告里的 `内联 22 张`（ODR-065 时点）→ 现在 **37 张**
> （C5 补 11 张多源目标表 + 后续再补 4 张代码直接引用的表）。**数表就 grep 源码。**
>
> 分区：`ingest` schema 1 张（`ingest.raw`，原始源响应归档）、`research` schema 3 张
> （`profile` / `conclusion` / `question`，研究结构化状态投影，可 DROP 重建）、
> 其余 33 张在 `public`。分区原则与可重建性见
> [ADR-023](adr/adr-023-ai-experimenter-lab.md)。
>
> ⚠️ 历史遗留：`data_fallback_chain` / `data_source_registry` **从来不是表** ——
> 数据源注册表是内存态（`pkg/data/source/registry.go` 明确不写这两张表），
> 旧版本文档把它们算进「迁移新增」是错的。
>
> ODR-010 之前曾清掉 10 张废弃表 (backtest_data, new_share, stk_managers,
> stk_rewards, stock_company, trade_calendar, daily, trade_cal, market_data,
> stock_basic)。

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

### 辅助表（其余 31 张，含 `ingest.raw` + `research.*`）

> `users` / `audit_logs` 属其余 31 张之内，未在下表单列（内联定义见 `pkg/storage/postgres.go`）。
>
> 6 (主表) + 31 (辅助) = **37**，即上方「内联 DDL 37 张」。
> `fundamentals_detail` 为**未落地**的规划表（EQD-P1-1，预留 `migrations/022`）。

> 以下表用于缓存、分析、AI 研究、多数据源接入等场景，详细 schema 见
> `migrations/` 和 `pkg/storage/postgres.go`。

| 表名 | 用途 | 主要字段 |
|------|------|---------|
| `dividends` | 分红送股数据 | symbol, ex_date, cash_div, share_div |
| `splits` | 拆股数据 | symbol, ex_date, split_ratio |
| `fundamentals_detail`  | **深财务明细快照（表已落地 — ODR-053 / EQD-P1-1）** 逐字段行存 + `ann_date` PIT 对齐 + `snapshot_uri` 溯源。⚠️ **摄取链路未通、表内无数据**（`EQD-P1-2` 待做）—— "表存在" ≠ "数据可用"，取数前不要假设深财务字段有值 | ts_code, end_date, ann_date, field_code, raw_field_name, value, unit, source, fetched_at, snapshot_uri |
| `factor_cache` | 因子计算结果缓存 | symbol, trade_date, factor_name, value |
| `factor_returns` | 因子收益分析 | factor_name, period, return |
| `ic_analysis` | 因子 IC 分析结果 | factor_name, trade_date, ic, rank_ic, top_ic |
| `index_constituents` | 指数成分股 | index_code, symbol, in_date, out_date |
| `walk_forward_reports` | Walk-forward 分析报告 | strategy_id, train_start, train_end, metrics |
| `orders` | 实盘/纸交易订单 (Phase 4) | order_id, symbol, direction, status, timestamps |
| `factor_genes` | AI 因子基因池 (Phase 4) | id, name, formula, ic_history JSONB, performance JSONB, genealogy JSONB, status |
| `strategy_genes` | AI 策略基因池 (Phase 4) | id, name, code, params JSONB, fitness JSONB, genealogy JSONB, generation, status |
| `sync_jobs` | 数据同步任务队列 (Phase 3) | id, job_type, mode, status, progress, payload JSONB |
| `sync_schedules` | 定时同步调度 (Phase 3) | id, name, cron, job_type, is_enabled |
| `sectors` | 行业/板块定义 | sector_code, name, level |
| `stock_sector_map` | 股票-行业映射 | symbol, sector_code, in_date |
| `top_list` | 龙虎榜数据 | trade_date, symbol, buyer, seller, net_amount |
| `limit_up_pool` | 涨停板池 | trade_date, symbol, limit_type, consecutive_days |
| `announcements` | 公告数据 | symbol, ann_date, title, content |
| `news` | 财经新闻 | publish_time, title, summary, source |
| `hot_search` | 热门搜索/概念 | rank, keyword, heat_score, trade_date |
| `global_ohlcv` | 全球市场 OHLCV (港股/美股) | symbol, trade_date, OHLCV |
| `ohlcv_minute` | 分钟 K 线 | symbol, trade_time, OHLCV |
| `capital_flow` | 资金流向 (主力/散户) | symbol, trade_date, super_net, large_net, retail_net |
| `realtime_quote` | 实时行情快照 | symbol, last_price, bid/ask, volume, ts |
| ~~`data_source_registry`~~ | ⚠️ **不是表** — 数据源注册表是内存态（`pkg/data/source/registry.go` 明确不写库），只存在于 `migrations/014` 的 SQL 副本里。**不算进 37 张** | — |
| ~~`data_fallback_chain`~~ | ⚠️ **不是表** — 同上，降级链也是内存态。**不算进 37 张** | — |
| `ingest.raw` | **原始源响应归档（已落地）** 所有外部源响应按 `content_hash` 唯一归档，是全部数字的最终证据坐标 | content_hash PK, source, dataset, key, as_of, payload JSONB, fetched_at |
| `research.*` | **研究结构化状态投影（已落地）** 3 张表 `profile` / `conclusion` / `question`（结论/疑点字段 + citations），可由 vault markdown 确定性重建（可 DROP） | content_hash FK, conclusion, thesis, citations JSONB, evidence_pointer |

> 备注: `fundamentals` 与 `stock_fundamentals` 的字段重叠**已收口** —— 原先登记为 `TASKS.md` C-8（[ODR-047](archive/odr/odr-047-equitydeep-integration-audit.md) DR-7），已于 `EQD-P3-1` 落地（[ODR-056](archive/odr/odr-056-fundamentals-table-consolidation.md)：迁移 `025` 存量并入 `stock_fundamentals` 后 `DROP TABLE fundamentals`）。**基本面数据一律读写 `stock_fundamentals`。** `orders` 表 (`migrations/003` 定义) 当前未被代码引用 —— 它只在**不被执行**的迁移目录里，因此**从来没被创建过**，可考虑删除。
>
> `fundamentals_detail`：ADR-023 把 EquityDeep 降为数据底座后，此表不再是「研究的前置步骤」，而是 AI 生成假设时**按需检索**的数据源之一。schema 定义见 [archive/RESEARCH-equitydeep-legacy.md §3.2](archive/RESEARCH-equitydeep-legacy.md)，建表 SQL 见 `docs/migrations/022_equitydeep_fundamentals.sql`（**不执行**，实际 DDL 在 `pkg/storage/postgres.go`）。⚠️ 当前**表已建、数据未摄取**。

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

### 策略接口 (pkg/strategy/interfaces.go)
> **Canonical definition** — matches [SPEC.md](SPEC.md#strategy-interface) §Strategy Interface
> 与 [VISION.md](VISION.md#b-strategy-layer) §B Strategy Layer, P1-24 (ADR-020 §6)

```go
// 4 个 single-responsibility 子接口 (P1-24 ISP 拆分)
type StrategyCore interface {
    Name() string
    Description() string
}
type Configurable interface {
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
}
type SignalGenerator interface {
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]domain.Signal, error)
    Weight(signal domain.Signal, portfolioValue float64) float64
}
type ResourceManaged interface {
    Cleanup()
}

// 复合接口 (向后兼容, 7 方法 surface 不变)
type Strategy interface {
    StrategyCore
    Configurable
    SignalGenerator
    ResourceManaged
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
| expression_template | `expression/strategy.go` | DSL 表达式策略：cs_rank/sizing/risk 全流程（S7-P3-1） |

### 策略加载流程
1. `plugins/` 和 `expression/` 包通过 `init()` 自动注册到 `strategy.GlobalRegistry`
2. `analysis-service` 启动时 `import _ "pkg/strategy/plugins"` 和 `import _ "pkg/strategy/expression"` 触发注册
3. 回测时 engine 先查本地 registry，fallback 到外部 strategy-service

### ExpressionStrategy 架构 (pkg/strategy/expression/, S7-P3-1)

DSL 表达式 → 策略的端到端流水线，让 AI 输出的 YAML 表达式可直接作为策略运行：

```
GenerateSignals(ctx, bars, portfolio)
  │
  ├─ NewOHLCVDataProvider(bars)      → aiexpr.DataProvider 适配
  ├─ aiexpr.NewEvaluator(provider)   → 表达式求值器
  ├─ SignalGenerator.Generate()      → DSL 表达式 → []Signal (truthy 过滤)
  ├─ PositionSizer.Size()            → equal/strength_prop/fixed 权重
  ├─ RiskController.Check()          → 单仓上限 + 持仓数 + 现金缓冲
  └─ 返回 signals (Strength = 最终权重, Metadata.raw_strength = 原始 DSL 值)
```

**组件层级**:
- `pkg/ai/expression/` — DSL 解析器 + AST + 求值器 (cs_rank/cs_zscore/cs_neutralize/ts_*)
- `pkg/strategy/expression/signal.go` — SignalGenerator: DSL → Signal
- `pkg/strategy/expression/sizing.go` — PositionSizer: Signal → 权重
- `pkg/strategy/expression/risk.go` — RiskController: 权重 → 风控过滤
- `pkg/strategy/expression/data_provider.go` — OHLCVDataProvider: bars → DataProvider
- `pkg/strategy/expression/strategy.go` — ExpressionStrategy: 组合以上为 strategy.Strategy

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
- 卖出：value × 0.0003 + value × 0.00001 + value × 0.0005（印花税，2023-08-28 起）

> 印花税自 2023-08-28 从 0.1% 减半至 0.05%（财政部/税务总局 2023 年第 39 号
> 公告）。AUD-06 修正前此处写 0.001，是减半前的旧税率。

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
│   │   ├── types.go    — 核心类型（OHLCV, Trade, Position, Signal, OrderType 等）
│   │   │                 （S7-P3-4: 市场类型已迁移至 market/，此处为 type alias）
│   │   └── market/     — 市场数据类型 canonical 定义（S7-P3-4 软分层）
│   │       ├── types.go    — OHLCV, Stock, Fundamental, FundamentalData 等
│   │       ├── provider.go — Provider 接口（原 MarketDataProvider，重命名）
│   │       └── doc.go      — 包文档 + 软分层策略说明
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

## ~~统一研究平台架构~~ — ADR-022（已废弃，仅供溯源）

> ⚠️ **本节描述的是 2026-09-15 的定位（"两个对等工作面 + 飞轮闭环"），已于 2026-09-16 被取代。**
>
> **新定位**：AI 实验员 + 人类监督者实验室——三层模型（AI 编排 / 能力 / 数据），EquityDeep 从"独立工作面"降为数据底座（产业链图谱 + 研究洞察库）。
> 见本文 §架构总览（三层模型）与 [PRODUCT.md](PRODUCT.md)。
>
> 保留本节仅供溯源。**现行决策以 [adr/](adr/) 中未废弃的条目为准。**

### 顶层定位：一个产品，两个对等工作面，一个共享底座

原 Quant Lab 的 Go 后端 + PG + 微服务**降维为共享底座**（数据面 + 计算面 + 编排面），其上承载两个**对等工作面**：

```
        ┌──────────────────────┐   ┌──────────────────────
        │  工作面 1：纵向深研    │   │  工作面 2：横截面选股  │
        │  1 股 × N 季度        │   │  N 股 × 1 因子        │
        │  产出：研究档案        │   │  产出：交易信号        │
        └──────────┬───────────┘   └──────────┬───────────┘
                   │      飞轮闭环 ①②↔③④      │
                   └───────────┬───────────────
                               ▼
        ┌──────────────────────────────────────────────────┐
        │  共享底座 = 原 Quant Lab 全部能力（降维为 L0-L2）   │
        └──────────────────────────────────────────────────┘
```

- **工作面 1（纵向深研）**：EquityDeep —— 1 股 × N 季度，季度频，产出研究档案，是本产品的**首要高层工作面**。
- **工作面 2（横截面选股）**：原横截面能力 —— N 股 × 1 因子，日频，产出交易信号，与工作面 1 **对等**，本期纳入规划。

### 按数据性质分区（"不重复存储"的机制）

**不重复存储不靠约定，靠物理归属**——每类数据只有一个权威位置：

| 类别 | 内容 | 可重建？ | 唯一权威位置 | 其他侧副本 |
|---|---|---|---|---|
| A | 原始源响应（含中文原始字段名） | 可（重抓） | PG `ingest.raw`（`content_hash` 唯一键） | ❌ 仅持 `content_hash` |
| B | 规范化数据（OHLCV / 财报字段 / 日历 / 公司行为） | 可（从 A 重算） | PG `market.*` | ❌ 只读证据 API |
| C | 派生计算结果（因子 / 回测 / IC） | 可（从 B 重算） | PG `quant.*` + Redis `factor_cache` |  只读计算 API |
| D | 研究叙事（结论 / 疑点的自然语言正文） | **不可**（人的判断） | **Vault markdown**（事实源） | — 本身即事实源 |
| E | 研究结构化状态（结论/疑点字段 + citations） | 可（从 D 确定性投影） | PG `research.*`（**投影，非权威**） | 权威在 D；可 DROP 重建 |

**判据**："重复存储" = 同一份数据有两个都可写的位置并导致口径漂移。本方案中：A/B/C 物理唯一于 PG，工作面 1 运行期按需读 API、**零本地副本**；D 物理唯一于 vault markdown；E 是 D 的**确定性投影**（`equitydeep sync`，非 LLM 生成），单一写者、可丢弃、冲突时以 markdown 为准 —— 性质等同索引 / 物化视图，存在理由仅是可做跨层 SQL join。

### 四层架构与单向依赖

```
L3 体验面   Obsidian Vault（工作面1） | Vue SPA（工作面2） | Hermes Agent（编排）
L2 编排面   Research Pipeline（纵向） | Research Engine（横截面） | MCP Tool Bridge
L1 计算面   因子引擎 | 回测引擎 | 验证门禁 L1-L5 | 风控·执行
L0 数据面   ingest.raw | market.* | quant.* | research.* | Evidence API   ← 唯一事实源
```

原则：**L0 唯一数据面 + 单一写者 + 单向依赖（L3→L2→L1→L0）+ 可重建性标注 + 契约优先**。

### EquityDeep 形态变更（允许有 DB / Docker，但零数据副本）

| 项 | ADR-021（原） | ADR-022（本决策） |
|---|---|---|
| DB | 无（"文件系统即数据库"） | **接入共享 PostgreSQL 的 `research` schema**（不新建实例） |
| Docker | 无 | **`equitydeep-research` worker 容器**加入 docker-compose |
| 取数 | 自行调用 akshare | **严格单一入口**：经 L0 只读证据 API；akshare adapter 归入 L0 |
| 原始快照 | vault 内 `snapshots/*.json` | 迁至 PG `ingest.raw`；vault 只留 `{content_hash, pointer}` |
| 叙事 | `_profile.md` | **不变** —— 仍是事实源，Obsidian 仍是工作面 |
| 结构化状态 | `_profile.json` 镜像文件 | 升级为 PG `research.*` 投影（JSON 保留为导出格式） |
| **不变** | Python 3.11 / 7-stage 固定流程 / 逐数溯源 / 三硬承诺 / 非目标红线 | **全部保留**（产品价值本体） |

### 证据服务升级为平台能力

Citation 从"文件路径 + 模糊字符串"升级为**不可变内容坐标**：

```
citation = { source, dataset, key, as_of, content_hash }
GET /api/evidence/{content_hash}  →  ingest.raw 中的唯一原始记录（不可变）
```

这条同时**在架构层面消除** ODR-047 发现的 P0 缺陷：回查脚本的校验对象从"文本子串"变为"声明（citation 元组）+ JSON Pointer 精确解析"，假阳性在机制上不可能发生。

### 飞轮闭环是"一个产品"的判据

```
① 纵向深挖 → 产出可检验假设
② 横截面验证 → 假设变因子 → 全市场回测 → IC/Sharpe
③ 结果回流 → 修正/限制原结论（证伪也是收益）
④ 异常触发 → 横截面命中异常板块 → 触发纵向深挖 → 回到 ①
```

没有这个闭环，产品退化为两个独立工具；有了它，**护城河是积累起来的研究资产（档案 + 因子 + 对应关系）**，而非任何单点技术。

**边界**：EquityDeep 不产出信号或目标价；档案是**审查材料与因子假设来源**，不是信号源。

**执行路线（P0-P5）**：P0 顶层定义 → P1 底座契约 → P2 工作面 1 跑通 → P3 计算面补齐 → P4 飞轮打通 → P5 横截面工作面对齐。详见 [TASKS.md](TASKS.md) Sprint 8。

---

## Tools Registry 架构 (pkg/tools/) — S7-P3-3

> **设计方向**: 本服务 = 对外的 API/工具提供方；agent = 外部消费者，
> 通过 HTTP 调用 `/api/tools/*` 发现并执行工具，基于响应生成 skills。

### 包结构

```
pkg/tools/
├── tool.go              # Tool 接口（ISP 拆分：ToolCore + SchemaProvider + Executable）
├── registry.go          # Registry（factory 注入、allow replacement、sorted List）
├── errors.go            # sentinel errors (ErrToolNotRegistered / ErrInvalidArgs / ...)
├── helpers.go           # AsExecutable / AsSchemaProvider 类型断言
└── builtin/
    ├── backtest.go           # backtest.run → contracts.BacktestRunner
    ├── factor.go             # factor.compute / factor.evaluate → client.FactorClient
    ├── factor_tools.go       # validate_factor / compute_factor_ic
    ├── datafetch.go          # data.ohlcv / data.stocks / data.fundamentals → data-service HTTP
    ├── strategy_registry.go  # strategy.list / strategy.get → strategy.GlobalList/Get
    ├── gene_pool_tools.go    # list_factors / list_strategies / save_factor / save_strategy
    ├── lineage_tool.go       # get_strategy_lineage
    ├── walkforward_tool.go   # walk_forward_validate
    ├── market_regime_tool.go # get_market_regime
    ├── summarize_tool.go     # summarize_backtest
    └── gate.go               # L1-L4 验证门禁
```

### 设计要点

- **共存适配器**: BacktestTool 委托给现有 `contracts.BacktestRunner`，不破坏现有 agent
- **factory 注入**: Registry 通过 `ServerDeps.ToolsRegistry` 注入，无全局实例
- **builtin/ 子包隔离**: `pkg/tools/` 保持纯净（只有接口），具体实现依赖在 `builtin/`
- **19 个 builtin tool**（ODR-046 扩展后 + ODR-057 新增 `research.profile`）: backtest.run | factor.compute | factor.evaluate | validate_factor | compute_factor_ic | list_factors | list_strategies | save_factor | save_strategy | get_strategy_lineage | walk_forward_validate | get_market_regime | summarize_backtest | data.ohlcv | data.stocks | data.fundamentals | strategy.list | strategy.get | research.profile

> **DR-1 修复 (ODR-047)**: 本节原称「8 个 builtin tool」，与 `pkg/tools/builtin/` 实际注册的 18 个不符（已逐一核对 `Name()` 实现）。数量以本文为准，新增工具需同步更新此列表。

---

## Domain Market 软分层架构 (pkg/domain/market/) — S7-P3-4

> **设计来源**: [ODR-043](archive/odr/odr-043-comprehensive-audit-2026-06-29.md) D3 决策 —
> "不做 big-bang schema 重构，采用'软分层 + view 过渡'，新增 `pkg/domain/market/` 子包"
> **目标**: 将市场数据类型从扁平的 `pkg/domain/types.go` 迁移到专用子包，
> 同时通过 Go type alias 保持 199 个消费者文件零修改。

### 软分层策略

采用 **type alias**（`type OHLCV = market.OHLCV`）而非新类型（`type OHLCV market.OHLCV`）。
两者区别：

| 写法 | 含义 | 消费者影响 |
|------|------|-----------|
| `type OHLCV = market.OHLCV` | alias — 同一类型 | 零修改，`domain.OHLCV` 与 `market.OHLCV` 可互换 |
| `type OHLCV market.OHLCV` | 新类型 — 需显式转换 | 199 个文件需在边界做类型转换 |

本架构选择 alias，实现"零破坏"重构：旧代码继续工作，新代码可逐步迁移到
`market.` 命名空间。

### 包结构

```
pkg/domain/
├── types.go              # 保留 Signal/Order/Position 等交易类型 + 8 个市场类型 alias
├── market_alias_test.go  # 编译期断言：alias 同一性 canary
└── market/               # 市场数据类型 canonical 定义（S7-P3-4 新增）
    ├── doc.go            # 包文档 + 软分层策略说明
    ├── types.go          # OHLCV, Stock, IndexConstituent, Split, Dividend,
    │                     # Fundamental, FundamentalData（7 个 struct）
    ├── provider.go       # Provider 接口（原 MarketDataProvider，重命名）
    └── types_test.go     # 9 个 JSON 往返测试 + nil 指针边界
```

### 迁移的类型（8 个）

| 类型 | 原位置 | 新位置 | 性质 |
|------|--------|--------|------|
| `OHLCV` | `domain.OHLCV` | `market.OHLCV` | 行情数据 |
| `Stock` | `domain.Stock` | `market.Stock` | 标的基础信息 |
| `IndexConstituent` | `domain.IndexConstituent` | `market.IndexConstituent` | 指数成分股 |
| `Split` | `domain.Split` | `market.Split` | 除权事件 |
| `Dividend` | `domain.Dividend` | `market.Dividend` | 分红事件 |
| `Fundamental` | `domain.Fundamental` | `market.Fundamental` | 财务基本面（纯领域） |
| `FundamentalData` | `domain.FundamentalData` | `market.FundamentalData` | 持久化视图（带 ID/CreatedAt） |
| `MarketDataProvider` | `domain.MarketDataProvider` | `market.Provider` | 接口（重命名，旧名 alias 保留） |

### 设计要点

- **alias 而非新类型**: `type X = Y` 让 `domain.X` 与 `market.X` 是同一类型，199 个消费者零修改
- **`MarketDataProvider` → `Provider` 重命名**: 0 外部消费者，重命名安全；旧名通过 alias 保留
- **FundamentalData 作为持久化视图**: 与纯领域类型 `Fundamental` 区分 — 前者带 DB 元数据
  (ID/CreatedAt) 且使用 `*float64` 区分 "missing"(nil) 与 "zero"(0.0)
- **不迁移的类型**: 交易类型（Signal/Order/Position）、风控类型、Config 类型保留在 `pkg/domain/`
- **编译期 canary**: `TestAlias_MarketTypesIdentical` 通过编译时类型赋值断言 alias 同一性，
  若未来 refactor 破坏 alias，测试将**编译失败**（比运行时断言更强）

### 消费者迁移指引

新代码 SHOULD 直接 `import "github.com/ruoxizhnya/quant-trading/pkg/domain/market"`
并使用 `market.OHLCV` 等命名。旧代码无需修改 — alias 保证向后兼容。

```go
// 旧写法（仍可用）
import "github.com/ruoxizhnya/quant-trading/pkg/domain"
var bar domain.OHLCV

// 新写法（推荐）
import "github.com/ruoxizhnya/quant-trading/pkg/domain/market"
var bar market.OHLCV
```

---

## AI 研究架构 (pkg/ai/) — Phase 4

> **状态**: Active — 核心组件已实现
> **定位**: AI 是**操作仪器的实验员**（ADR-023），人是实验室主任
> **入口**: `cmd/analysis/main.go` (:8085) — MCP 工具层 `/api/tools` 是 AI 操作底座的唯一通道
>
> ⚠️ 原文此处写的是 `cmd/ai/main.go` (:8086)。该服务已于 2026-09-18 删除
> （TASKS P2-5）：它只有 2 个端点且零调用方，建在废弃交互层的定位上。
> AI 能力现在由 `cmd/analysis` 的 MCP 工具层（20 个工具）承载。

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
| Generate Agent | `pkg/ai/agents/generate.go` | 自然语言 → 策略代码生成 | ✅ 已实现（ODR-046 后 deprecated） |
| Validate Agent | `pkg/ai/agents/validate.go` | 分层验证：L1 语法 → L2 快速回测 → L3 标准回测 → L4 Walk-Forward | ✅ 已实现（ODR-046 后 deprecated） |
| Evolve Agent | `pkg/ai/agents/evolve.go` | 遗传算法 + 概念漂移检测 | ✅ 已实现（ODR-046 后 deprecated） |
| Gene Pool | `pkg/ai/gene_pool/` | 因子/策略基因库 (PostgreSQL JSONB) | ✅ 已实现 |
| Backtest Client | `pkg/ai/client/backtest_client.go` | HTTP 客户端调用回测 API | ✅ 已实现 |
| Factor Client | `pkg/ai/client/factor_client.go` | HTTP 客户端调用因子计算 API | ✅ 已实现 |

> **DR-2 修复 (ODR-047)**: Generate / Validate / Evolve Agent 与 Gene Pool 此前标为「🔄 规划中」，但对应文件（`pkg/ai/agents/{generate,validate,evolve}.go`、`pkg/ai/gene_pool/`）均已存在，故改标已实现；其 Go-native agent 层已在 [ODR-046](archive/odr/odr-046-hermes-agent-integration-decision.md) 中标记 deprecated（Hermes Agent 为研究主路径，`pkg/ai/agents/` 保留向后兼容）。

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

> **YAML 直执行路径 (S7-P3-2)**: 当策略为 expression 类型时，
> `Pipeline.ExecuteFromYAML` 绕过 LLM codegen + 编译，直接
> `LoadStrategy → registerOrConfigure → RunBacktest`。适用于 AI 迭代
> 调参场景（同 YAML 多次执行 / 微调 expression 后重跑）。同名策略
> 重复执行时走 `Configure` in-place 重配，异类型冲突报错。详见
> [SPEC.md](SPEC.md#direct-execution-via-executefromyaml)。

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

### 前端 AI 模块 — ❌ DEPRECATED (ODR-045, 2026-07-02)

> 以下组件 P1-13 创建 (ODR-017) 后 S7-P2-7 作为死代码删除 (ODR-043, commit `d7c2a38`)。
> `web/src/components/ai/` 目录已不存在。Hermes Agent 自然语言交互替代 (ODR-046)。
> 不要重建这些组件 — 研究主路径现为 Hermes → MCP bridge (`pkg/tools/builtin/`, 19 工具) → Go 后端。

```
web/src/components/ai/     # ❌ 目录已删除 (S7-P2-7)
├── FactorLab.vue           # ❌ 已删除 (was 362 lines)
├── StrategyWorkshop.vue    # ❌ 已删除 (was 297 lines)
├── EvolutionObs.vue        # ❌ 已删除 (was 304 lines)
├── FactorCard.vue          # ❌ 已删除 (was 233 lines)
├── StrategyCard.vue        # ❌ 已删除 (was 225 lines)
├── GenealogyTree.vue       # ❌ 已删除 (was 182 lines)
└── FitnessChart.vue        # ❌ 已删除 (was 254 lines)
```

### 新增页面 — ❌ DEPRECATED

```
web/src/pages/
└── AIResearch.vue          # ❌ 已删除 (was 36 lines, never registered in router)
```
