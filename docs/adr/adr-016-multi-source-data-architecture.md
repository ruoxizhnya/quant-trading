# ADR-016: Multi-Source Data Architecture

> **Status**: Proposed
> **Date**: 2026-05-17
> **Category**: Architecture
> **Related ADRs**: ADR-013 (Data Sync Enhancement), ADR-007 (AI Sandbox)
> **Related ODRs**: ODR-011 (Multi-Source Integration)
> **Author**: AI Assistant (Trae IDE)
> **Supersedes**: N/A

---

## Context

### 现状

quant-trading 当前**仅依赖 Tushare 作为外部数据源**（`pkg/data/tushare.go`，8 个 API）：

| API | 用途 | 频率 |
|-----|------|------|
| `stock_basic` | 股票基础信息 | 静态 |
| `stk_factor_pro` | K线 + 前复权 | 日级 |
| `fina_indicator` | 财务指标 | 季度 |
| `financial_data` | 财务摘要 | 季度 |
| `index_weight` | 指数成分股 | 月级 |
| `trade_cal` | 交易日历 | 年度 |
| `dividend` | 分红 | 事件 |
| `split` | 拆股 | 事件 |

**核心局限**:
1. ❌ **无实时行情** — 仅日级，盘中策略无法做
2. ❌ **无资金流数据** — 主力行为、跟庄因子无法计算
3. ❌ **无板块/概念归属** — 板块轮动策略缺失
4. ❌ **无龙虎榜/涨停池** — 事件驱动策略缺失
5. ❌ **无公告/新闻** — 事件驱动、舆情因子缺失
6. ❌ **无数据源标识** — 表结构无 `source` 列，无法区分数据血缘
7. ❌ **无降级机制** — Tushare 限流时无备用源

### 外部数据源机会

跨项目 `ashare-data-source-fetchers`（基于 SKILL.md V3.2.2，2026-06-12）已识别并验证的外部数据源：

| 类别 | 数据源 | 端点数 | 价值 |
|------|-------|-------|------|
| A股实时 | **mootdx** (TCP 协议) | 5 | 五档盘口/逐笔成交/分钟K线 |
| A股资讯 | 东财 push2 (HTTP) | 1 | 资金流（分钟级） |
| A股板块 | 东财 slist (HTTP) | 1 | 概念板块归属 |
| A股榜单 | 东财 龙虎榜/涨停池 | 2 | 游资动向/涨停战法 |
| A股公告 | 巨潮 (HTTP) | 1 | 公告事件 |
| A股舆情 | 雪球热搜 (HTTP) | 1 | 社交情绪 |
| 全球 | Alpha Vantage | 15 | 美股/全球行情/财报/技术指标 |
| 全球 | Yahoo Finance | 9 | 美股/全球行情/财报 |

ashare 仓库已实现 `cn_akshare → cn_baostock → yfinance → alpha_vantage` 的 Provider 降级链。

---

## Decision

### 1. 架构总览：分层 + 适配器模式

```
┌──────────────────────────────────────────────────────────────┐
│                  Quant Strategy / AI Agent                   │
└──────────────────────────┬───────────────────────────────────┘
                           ▼
┌──────────────────────────────────────────────────────────────┐
│            DataSourceRegistry (统一访问层)                     │
│   - Fallback Chain Management                                 │
│   - Rate Limit Aggregation                                   │
│   - Health Check                                              │
└──────────────────────────┬───────────────────────────────────┘
                           ▼
┌──────────────────────────────────────────────────────────────┐
│         DataSourceAdapter Interface (统一抽象)                │
│   - TushareAdapter (existing, refactor)                       │
│   - MootdxAdapter     (new - 实时/分钟)                       │
│   - EastMoneyAdapter  (new - push2/slist/榜单)                │
│   - JuchaoAdapter     (new - 公告)                            │
│   - XueqiuAdapter     (new - 热搜)                            │
│   - AlphaVantageAdapter (new - 全球)                          │
│   - YahooFinanceAdapter  (new - 全球)                         │
└──────────────────────────┬───────────────────────────────────┘
                           ▼
┌──────────────────────────────────────────────────────────────┐
│                ETL Pipeline (与 sync_jobs 集成)               │
│   Normalize → Validate → Dedup → Persist (with source)       │
└──────────────────────────┬───────────────────────────────────┘
                           ▼
┌──────────────────────────────────────────────────────────────┐
│   PostgreSQL + TimescaleDB                                   │
│   - ohlcv_daily_qfq + source column (migration)              │
│   - realtime_quote (new hypertable)                          │
│   - ohlcv_minute (new hypertable)                            │
│   - capital_flow (new)                                        │
│   - sectors + stock_sector_map (new)                          │
│   - top_list, announcements, news (new)                      │
└──────────────────────────────────────────────────────────────┘
```

### 2. 核心抽象：`DataSourceAdapter` 接口

```go
// pkg/data/source/adapter.go
package source

import (
    "context"
    "time"
)

type DataSourceAdapter interface {
    // 标识
    Name() string                    // "tushare" | "mootdx" | "eastmoney" | ...
    Type() AdapterType               // HTTP | SDK | WebSocket
    Enabled() bool

    // 数据获取
    Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error)
    HealthCheck(ctx context.Context) error

    // 限流配置
    RateLimit() RateLimitConfig

    // 元数据
    Schema() DataSchema              // 该源支持的字段和类型
}

type FetchRequest struct {
    DataType   string                 // "ohlcv_daily" | "ohlcv_minute" | "capital_flow" | ...
    Symbols    []string
    StartDate  time.Time
    EndDate    time.Time
    Extra      map[string]interface{}
}

type FetchResponse struct {
    Items      []DataItem
    Source     string                 // adapter.Name()
    FetchedAt  time.Time
    Latency    time.Duration
    HasMore    bool
    NextCursor string
}

type DataItem struct {
    Symbol    string
    TradeTime time.Time
    Data      map[string]interface{} // 原始字段，标准化在 ETL 中
}
```

### 3. 适配器注册与降级链

```go
// pkg/data/source/registry.go
type Registry struct {
    adapters  map[string]DataSourceAdapter
    fallbacks map[string][]string      // data_type → [primary, secondary, ...]
    mu        sync.RWMutex
}

func (r *Registry) Register(a DataSourceAdapter) error { ... }
func (r *Registry) Fetch(ctx context.Context, dataType string, req FetchRequest) (*FetchResponse, error) {
    // 1. 尝试 primary
    // 2. 失败时按降级链 fallback
    // 3. 所有失败时聚合错误返回
}
```

**降级链设计**:
- A股 K线日线: `tushare` → `eastmoney` → `sina` → `tencent`
- A股 实时: `mootdx` → `eastmoney` → `sina`
- A股 资金流: `eastmoney` only (唯一源)
- 财务: `tushare` → `eastmoney` → `xueqiu` → `tonghuashun`
- 美股: `yahoo` → `alpha_vantage`

### 4. 数据存储演进

**Phase A: 兼容性迁移（不破坏现有）**
- 给所有数据表加 `source VARCHAR(32) DEFAULT 'tushare'`
- 给所有数据表加 `ingest_time TIMESTAMPTZ DEFAULT NOW()`
- 加 `data_version INT DEFAULT 1` 字段（ETL 重处理用）

**Phase B: 新表（增量）**

| 新表 | 用途 | 关键字段 | TimescaleDB |
|------|------|---------|------------|
| `realtime_quote` | 实时行情 | symbol, ts, price, bid1-5, ask1-5, vol | ✅ hypertable |
| `ohlcv_minute` | 分钟 K 线 | symbol, ts, OHLCV | ✅ hypertable |
| `capital_flow` | 资金流 | symbol, date, period, main_net, retail_net | ❌ |
| `sectors` | 板块/概念 | code, name, source, list_date | ❌ |
| `stock_sector_map` | 股票-板块映射 | symbol, sector_code, in_date, out_date | ❌ |
| `top_list` | 龙虎榜 | trade_date, symbol, side, amount | ❌ |
| `limit_up_pool` | 涨停池 | date, symbol, limit_type, consecutive | ❌ |
| `announcements` | 公告 | id, symbol, ann_date, title, content, source | ❌ |
| `news` | 新闻 | id, symbol?, pub_date, title, content, sentiment | ❌ |
| `hot_search` | 雪球热搜 | date, rank, symbol, hot_value | ❌ |

**Phase C: 元数据表**
```sql
CREATE TABLE data_source_registry (
  name VARCHAR(32) PRIMARY KEY,
  type VARCHAR(16) NOT NULL,
  enabled BOOLEAN DEFAULT true,
  rate_limit_per_min INT,
  priority INT NOT NULL,        -- 降级链顺序
  config JSONB,
  last_health_check TIMESTAMPTZ,
  last_error TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE data_fallback_chain (
  data_type VARCHAR(64) NOT NULL,
  source_name VARCHAR(32) NOT NULL,
  priority INT NOT NULL,
  PRIMARY KEY (data_type, source_name),
  FOREIGN KEY (source_name) REFERENCES data_source_registry(name)
);
```

### 5. ETL 集成（与 sync_jobs 框架）

```go
// pkg/data/source/etl.go
type ETLPipeline struct {
    registry *Registry
    store    *storage.PostgresStore
}

func (p *ETLPipeline) Process(ctx context.Context, dataType string, req FetchRequest) (int, error) {
    // 1. Fetch from registry (with fallback)
    resp, err := p.registry.Fetch(ctx, dataType, req)
    if err != nil {
        return 0, err
    }

    // 2. Normalize to UnifiedDataPoint
    points, err := Normalize(dataType, resp)
    if err != nil {
        return 0, err
    }

    // 3. Deduplicate (symbol + trade_time + source)
    points = Deduplicate(points)

    // 4. Validate
    if err := Validate(dataType, points); err != nil {
        return 0, err
    }

    // 5. Persist (bulk insert with source column)
    return p.store.BulkInsertWithSource(ctx, dataType, points, resp.Source)
}
```

### 6. 适配器优先级与 Sprint 划分

| Sprint | 适配器 | 实施理由 | 量化价值 |
|--------|-------|---------|---------|
| **Sprint 1 (1-2周)** | mootdx + eastmoney-push2 | 实时 + 主力资金 | 实时信号 + 主力因子 |
| **Sprint 2 (2-3周)** | eastmoney-slist + top_list | 板块 + 龙虎榜 | 板块轮动 + 情绪因子 |
| **Sprint 3 (3-4周)** | juchao + xueqiu | 公告 + 舆情 | 事件驱动 + 舆情因子 |
| **Sprint 4 (4-6周)** | alpha_vantage + yahoo | 全球扩展 | 跨市场策略（远期） |

### 7. 兼容性保证

1. **现有 `pkg/data/tushare.go` 不删除** — 改造为 `TushareAdapter` 实现 `DataSourceAdapter` 接口
2. **现有 `pkg/storage/` 接口不变** — 仅添加带 `source` 的新方法（如 `BulkInsertOHLCVWithSource`）
3. **数据库现有数据无破坏** — `source` 列有默认值 `tushare`，老数据自动归入
4. **API 端点不变** — 新增 `/api/datasource/*` 路径管理适配器
5. **前端组件不破坏** — 现有 BacktestForm 等不需改动

### 8. 测试策略

| 级别 | 范围 | 工具 | 通过标准 |
|------|------|------|---------|
| L1 单元 | 每个 Adapter 的 Normalize | go test | 100% 字段映射 |
| L2 集成 | Adapter → ETL → DB | docker-compose | 端到端无 error |
| L3 一致性 | mootdx 实时 vs tushare 日线 | 对账脚本 | 收盘价 ±0.01% |
| L4 因子 | 资金流/板块/舆情因子 IC | backtest | IC > 0.02 |

---

## Consequences

### 正面影响

1. **数据维度极大丰富**: 从 8 类数据 → 20+ 类
2. **实时能力**: 引入 mootdx 后支持盘中策略
3. **鲁棒性**: 降级链 + 限流聚合，外部故障不阻塞量化
4. **可扩展**: 任何新数据源只需实现 `DataSourceAdapter` 接口
5. **数据血缘**: 所有数据可追溯来源
6. **AI 增强**: AI Agent 可基于更丰富数据生成 alpha 因子

### 负面影响

1. **复杂度增加**: 适配器/降级链/ETL 增加系统复杂度
2. **新依赖**: mootdx (Go SDK), 需添加 go.mod
3. **数据一致性挑战**: 多源数据可能存在差异（需对账）
4. **运维负担**: 多数据源健康监控、限流配置
5. **测试矩阵**: 数据源组合爆炸，测试覆盖度需管理

### 中性影响

- 速率限制逻辑保持独立（每个 adapter 自管理）
- 缓存策略保持不变（Cache-Aside）
- API 网关不需改动

---

## Alternatives Considered

### Alt A: 单一 Tushare 扩展
- **方案**: 升级 Tushare 套餐，扩展 API 调用
- **缺点**: Tushare 实时数据需 5000+ 积分，**实时能力弱**，**无资金流/板块**，**单点故障**
- **拒绝**: 业务增长需要多源验证

### Alt B: 直接用 akshare
- **方案**: 引入 akshare Python，通过 sidecar 调用
- **缺点**: Python 进程增加运维负担，**架构不一致**（Go 主项目）
- **拒绝**: 优先用 Go 原生库 (mootdx)，仅在必要时用 akshare

### Alt C: 直接 HTTP 调用东财
- **方案**: 跳过 mootdx，直接用东财 HTTP API
- **缺点**: 东财有反爬，**稳定性差**，无五档/逐笔
- **拒绝**: mootdx 通过 TCP 更稳定

---

## Artifacts

### 新增

| 文件 | 类型 | 说明 |
|------|------|------|
| `docs/adr/adr-016-multi-source-data-architecture.md` | ADR | 本文档 |
| `docs/odr/odr-011-multi-source-integration.md` | ODR | 整合实施记录 |
| `pkg/data/source/adapter.go` | Go | DataSourceAdapter 接口 |
| `pkg/data/source/registry.go` | Go | 适配器注册与降级链 |
| `pkg/data/source/etl.go` | Go | ETL 管道 |
| `pkg/data/source/unified.go` | Go | UnifiedDataPoint 模型 |
| `pkg/data/source/tushare_adapter.go` | Go | Tushare 适配器（重构） |
| `pkg/data/source/mootdx_adapter.go` | Go | mootdx 实时适配器 (Sprint 1) |
| `pkg/data/source/eastmoney_adapter.go` | Go | 东财 push2/slist 适配器 |
| `pkg/data/source/juchao_adapter.go` | Go | 巨潮公告适配器 (Sprint 3) |
| `pkg/data/source/xueqiu_adapter.go` | Go | 雪球热搜适配器 (Sprint 3) |
| `pkg/data/source/alpha_vantage_adapter.go` | Go | Alpha Vantage 适配器 (Sprint 4) |
| `pkg/data/source/yahoo_finance_adapter.go` | Go | Yahoo Finance 适配器 (Sprint 4) |
| `migrations/013_add_source_columns.sql` | SQL | 给所有表加 source 列 |
| `migrations/014_add_realtime_tables.sql` | SQL | realtime_quote, ohlcv_minute (hypertable) |
| `migrations/015_add_market_dimension_tables.sql` | SQL | sectors, capital_flow, top_list 等 |
| `migrations/016_add_data_source_registry.sql` | SQL | 元数据表 |

### 修改

| 文件 | 变更 |
|------|------|
| `pkg/data/tushare.go` | 改造为 TushareAdapter（实现接口），保持向后兼容 |
| `pkg/storage/postgres.go` | 新增 `BulkInsertWithSource` 方法 |
| `pkg/storage/migrations.go` | 注册新迁移脚本 |
| `cmd/data/main.go` | 初始化 Registry、注册所有 adapter |
| `cmd/data/sync_handlers.go` | 新增 `/api/datasource/*` 端点 |
| `docs/ARCHITECTURE.md` | 数据模型章节补充新表 |
| `docs/SPEC.md` | API 章节补充 datasource endpoints |
| `docs/TASKS.md` | 追加多源任务 |

---

## Metrics

| 指标 | 目标 | 测量方式 |
|------|------|---------|
| 适配器数量 | 7 (含 Tushare) | `data_source_registry` count |
| 数据源 SLA | > 99% 可用 | 健康检查 + 成功率 |
| 降级链命中率 | 100% (无数据缺失) | 失败后 fallback 成功率 |
| 实时数据延迟 | < 1 秒 (mootdx) | `fetched_at - trade_time` |
| 多源一致性 | 收盘价 ±0.01% | 对账脚本 |
| 新因子 IC | > 0.02 | factor backtest |

---

## Risks & Mitigations

| 风险 | 影响 | 缓解 |
|------|-----|-----|
| 外部 API 变更 | 高 | Adapter 抽象层 + 单元测试 + 监控 |
| mootdx 反编译失效 | 中 | 备用 eastmoney/sina 降级 |
| 多源数据冲突 | 中 | 优先级 + 版本号 + 人工仲裁 |
| 数据库膨胀 | 中 | TimescaleDB hypertable 自动压缩 |
| 实时数据积压 | 中 | 背压机制 + 降采样 |

---

_Last updated: 2026-05-17_
_Implementation: ODR-011_
_Phase 4 evolution: see ADR-015 (AI Agent)_
