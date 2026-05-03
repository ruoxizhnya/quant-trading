# Phase 3 融合发展计划 v2

> **日期**: 2026-04-08
> **基于**: VISION.md + SPEC.md + ARCHITECTURE.md + 金策智算对比分析
> **状态**: ✅ 已批准，待实施

---

## 一、用户决策记录

| 决策项 | 选择 |
|--------|------|
| 执行顺序 | D1→D2→D3→D4→D5→D6 (按推荐顺序) |
| 数据源范围 | 全部5个：Tushare / AkShare / Postgres / HTTP / Cached装饰器 |
| 新策略 | 4个全做：TD Sequential / Bollinger MR / Volume-Price Trend / Vol Breakout |
| 架构风格 | **Event-Driven 数据管道**（回测+实时共用同一套逻辑） |
| 实盘实现 | ❌ 暂不实现，只预留接口 |

---

## 二、核心架构升级：Event-Driven Data Pipeline

### 2.1 当前架构 vs 目标架构

```
【当前架构】(同步调用链):
Engine.mainLoop()
  ├── provider.GetOHLCV() ──→ HTTP → data-service → PostgreSQL
  ├── httpClient.Post(strategy-service) 
  ├── riskManager.DetectRegime()
  └── tracker.ExecuteTrade()

问题:
- 数据源硬编码为 HTTP → data-service
- 切换数据源需要改代码重编译
- 回测和实时数据走不同路径
```

```
【目标架构】(Event-Driven Pipeline):

                    ┌─────────────────────┐
                    │   DataEventBus      │
                    │  (pub/sub channel)  │
                    └─────────┬───────────┘
                              │ subscribe
              ┌───────────────┼───────────────┐
              ↓               ↓               ↓
    ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
    │ DataAdapter  │ │ Strategy     │ │ RiskManager  │
    │ (pluggable)  │ │ Engine       │ │ (in-process) │
    └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
           │                │                │
           ↓                ↓                ↓
    ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
    │ TushareProv. │ │ SignalGen    │ │ PositionSize │
    │ AkShareProv. │ │ MultiFactor  │ │ StopLoss     │
    │ PostgresProv.│ │ Momentum     │ │ RegimeDetect │
    │ HttpProvider │ │ VolBreakout  │ │             │
    │ CachedProv.  │ │ ...          │ │             │
    └──────────────┘ └──────────────┘ └──────────────┘

关键特性:
- DataAdapter 可运行时切换 (Hot-Swap)
- 所有消费者通过 EventBus 接收数据 (解耦)
- 回测模式: DataAdapter 从历史数据库读，推送到 EventBus
- 实时模式: DataAdapter 从行情源读，推送到 EventBus (未来)
- 策略/风控不关心数据从哪来，只关心 EventBus 上的事件
```

### 2.2 核心类型定义

```go
// pkg/marketdata/eventbus.go — 新文件

type EventType string
const (
    EventTypeOHLCV       EventType = "ohlcv"        // K线数据就绪
    EventTypeFundamental EventType = "fundamental"   // 基本面数据就绪
    EventTypeTradeCal    EventType = "trade_cal"     // 交易日历就绪
    EventTypeError       EventType = "error"         // 数据错误
    EventTypeSourceSwitch EventType = "source_switch" // 数据源切换通知
)

type DataEvent struct {
    Type      EventType
    Symbol    string
    Timestamp time.Time
    Payload   interface{} // *OHLCV, *Fundamental, []time.Time, error
    Source    string      // 哪个 provider 产生的
}

type EventHandler func(event DataEvent)

type DataEventBus interface {
    Subscribe(eventType EventType, handler EventHandler) Unsubscriber
    Publish(event DataEvent)
    Close()
}

type Unsubscriber func()
```

```go
// pkg/marketdata/adapter.go — 新文件

// DataAdapter 是数据源的统一入口。
// 它负责: 1) 从配置的 Provider 获取原始数据; 2) 转换为 DataEvent; 3) 发布到 EventBus。
// 回测模式下，它按交易日顺序推送历史数据。
// 实时模式下(未来)，它订阅实时行情源并即时推送。
type DataAdapter struct {
    bus         DataEventBus
    primary     Provider       // 主数据源
    fallback    Provider       // 备选数据源
    cache       Provider       // 缓存层 (CachedProvider 包装 primary)
    logger      zerolog.Logger
    switchMu    sync.RWMutex
    currentName string
}

func NewDataAdapter(bus DataEventBus, primary, fallback Provider, logger zerolog.Logger) *DataAdapter
func (a *DataAdapter) SetPrimary(name string, p Provider) error  // 运行时切换主数据源
func (a *DataAdapter) Primary() string                            // 当前主数据源名称
func (a *DataAdapter) PushHistoricalData(ctx context.Context, symbols []string, start, end time.Time) error // 回测模式: 推送历史K线
func (a *DataAdapter) StartRealtime(ctx context.Context, symbols []string) error                          // 实时模式: 启动实时推送 (Phase 4)
func (a *DataAdapter) Stop()
```

---

## 三、六大模块详细计划

### D1: 多数据源适配器框架 ⭐⭐⭐ P0 (Week 1-2)

**目标**: 实现 Event-Driven 数据管道，支持 5 种数据源运行时切换。

#### D1.1 DataEventBus 实现 [0.5d]

文件: `pkg/marketdata/eventbus.go`
- 基于 Go channel 的 pub/sub 实现
- 支持 typed subscription (按 EventType 过滤)
- 支持 Unsubscribe (返回 cleanup 函数)
- 线程安全 (sync.Mutex 保护 handler map)

#### D1.2 增强 Provider 接口 [0.5d]

文件: `pkg/marketdata/provider.go` (修改已有)
新增方法:
```go
type Provider interface {
    // ... existing methods ...
    
    Name() string                                              // provider 名称标识
    CheckConnectivity(ctx context.Context) error               // 健康检查
    GetTradingCalendar(ctx context.Context, exchange string) ([]time.Time, error) // 交易日历
}
```

#### D1.3 TushareProvider 重构 [0.5d]

文件: `pkg/marketdata/tushare_provider.go` (从 data/tushare.go 迁移)
- 将现有 tushare.go 逻辑封装为 Provider 实现
- 保持所有现有功能不变
- 实现 Name() = "tushare", CheckConnectivity()

#### D1.4 PostgresProvider 新增 [1d]

文件: `pkg/marketdata/postgres_provider.go`
- 直接查询 PostgreSQL/TimescaleDB (零网络延迟)
- OHLCV: SELECT FROM ohlcv_daily_qfq WHERE symbol=$1 AND date BETWEEN $2 AND $3
- Fundamental: SELECT FROM fundamentals WHERE symbol=$1 AND date=$2
- Stocks: SELECT FROM stocks WHERE exchange=$1
- 优势: 回测场景下比 HTTP 快 10-100x

#### D1.5 AkShareProvider 新增 [0.5d]

文件: `pkg/marketdata/akshare_provider.go`
- 通过 exec.Command 调用 akshare Python 包 (或纯 Go reimplementation)
- 作为 Tushare 的免费备选方案
- Note: AkShare 是 Python 库，Go 端可通过 cgo 或 subprocess 调用

#### D1.6 HttpProvider 新增 [0.5d]

文件: `pkg/marketdata/http_provider.go`
- 通用 HTTP API 适配器
- 配置驱动的 endpoint mapping:
  ```yaml
  http_provider:
    base_url: "http://localhost:8081"
    endpoints:
      ohlcv: "/api/v1/data/ohlcv/{symbol}"
      fundamental: "/api/v1/data/fundamental/{symbol}"
      stocks: "/api/v1/data/stocks"
  ```

#### D1.7 CachedProvider 装饰器 [0.5d]

文件: `pkg/marketdata/cached_provider.go`
- 装饰器模式: 包装任意 Provider + Redis 缓存
- TTL 配置: OHLCV=24h, Fundamental=6h, Stocks=1h
- cache-aside: Check Redis → Hit return / Miss → delegate → Populate → Return
- 与现有 L2 Redis 缓存整合

#### D1.8 DataAdapter 实现 [1d]

文件: `pkg/marketdata/adapter.go`
- 整合 EventBus + Primary/Fallback/Cache 三层
- PushHistoricalData: 按交易日遍历，每天发布 EventTypeOHLCV 事件
- SetPrimary: 运行时原子切换 (switchMu 保护)
- 自动 fallback: primary 失败时自动降级到 fallback

#### D1.9 Engine 集成 DataAdapter [0.5d]

文件: `pkg/backtest/engine.go` (修改)
- NewEngine 接受 DataAdapter 替代裸 Provider
- mainLoop 改为从 EventBus 订阅 OHLCV 事件 (而非直接调 provider.GetOHLCV)
- 向后兼容: 如果未传入 DataAdapter，走旧逻辑

#### D1.10 Config + API [0.5d]

文件: `config.yaml`, `internal/config/config.go`, API handlers
- config.yaml 新增 data.adapters 段
- API: GET /api/data/sources (列出可用数据源)
- API: POST /api/data/sources/switch {"primary": "postgres"}
- API: GET /api/data/sources/current (当前活跃数据源)

**D1 验收标准**:
- [ ] 切换到 postgres provider 后，500股回测 < 5s (本地直读)
- [ ] Tushare 不可用时自动 fallback 到 akshare
- [ ] 所有 55+ 现有测试通过
- [ ] API 可以在运行时切换数据源

---

### D2: 批量回测框架 ⭐⭐⭐ P0 (Week 2-3)

**目标**: CSV 任务队列 → 并发执行 → Walk-Forward 过拟合检测 → A/B/C/D 评级

详见上文第二节设计，子任务 D2.1-D2.7 不变。

**D2 验收标准**:
- [ ] 100 任务 (10股票×4策略×区间池) < 30s 完成
- [ ] 输出含评级 + OverfitScore + StabilityScore
- [ ] CSV 兼容金策格式 + 我们的扩展格式

---

### D3: Go Plugin 策略热加载 ⭐⭐ P1 (Week 3-4)

**目标**: .so 编译 → 运行时 Load/Unload/Reload，无需重启服务

详见上文第三节设计，子任务 D3.1-D3.5 不变。

**D3 验收标准**:
- [ ] 动态加载 .so 策略，立即生效
- [ ] Reload 后新代码生效
- [ ] 提供 Makefile 一键编译插件

---

### D4: 实盘交易接口预留 ⭐⭐ P1 (Week 4)

**目标**: 只定义 LiveTrader 接口 + MockTrader，不做真实实现

详见上文第四节设计，子任务 D4.1-D4.4 不变。

**D4 验收标准**:
- [ ] LiveTrader 接口编译通过
- [ ] MockTrader 可用于测试/Paper Trading
- [ ] 文档清楚描述接入规范

---

### D5: 更多实战策略插件 ⭐⭐ P1 (Week 5-6)

**目标**: 4 个新策略，全部利用 FactorCache 加速

| 策略 | 核心因子 | 预计性能 (500股/5yr) |
|------|---------|---------------------|
| TD Sequential (神奇九转) | 价格序列模式计数 | ≤ 3s |
| Bollinger Mean Rejection | BB位置 + RSI | ≤ 3s |
| Volume-Price Trend | 量价配合度 + MA共振 | ≤ 3s |
| Volatility Breakout | ATR突破 + 方向过滤 | ≤ 3s |

详见上文第五节设计，子任务 D5.1-D5.5 不变。

**D5 验收标准**:
- [ ] 4 个新策略注册到 GlobalRegistry
- [ ] 每个策略 ≥ 3 个单元测试
- [ ] FactorCache 加速生效

---

### D6: AI Copilot 深度集成 ⭐ P2 (Week 6-7)

**目标**: 中文自然语言 → YAML 参数 → 编译验证 → 回测报告

详见上文第六节设计，子任务 D6.1-D6.4 不变。

**D6 验收标准**:
- [ ] 中文描述 → 30s 内得到回测结果
- [ ] ≥ 5 种策略描述正确解析

---

## 四、时间线总览

```
Week 1-2:  ════════════════════════════ D1: 多数据源 (Event-Driven Pipeline)
            D1.1 EventBus → D1.2 Provider增强 → D1.3 Tushare重构
            → D1.4 PG直读 → D1.5 AkShare → D1.6 HTTP通用
            → D1.7 Cache装饰器 → D1.8 Adapter → D1.9 Engine集成 → D1.10 Config+API

Week 2-3:  ════════════════════════════ D2: 批量回测框架
            D2.1 类型定义 → D2.2 CSV解析 → D2.3 BatchEngine(goroutine pool)
            → D2.4 Scorer → D2.5 WF集成 → D2.6 汇总 → D2.7 API

Week 3-4:  ════════════════════════════ D3: Go Plugin 热加载
            D3.1 PluginLoader → D3.2 Load/Unload/Reload → D3.3 示例插件
            → D3.4 API → D3.5 文档

            ════════════════════════════ D4: 实盘接口预留 (Week 4)
            D4.1 LiveTrader接口 → D4.2 MockTrader → D4.3 Engine预留 → D4.4 文档

Week 5-6:  ════════════════════════════ D5: 新策略插件
            D5.1 TD9转 → D5.2 Bollinger → D5.3 量价趋势 → D5.4 波动突破 → D5.5 测试

Week 6-7:  ════════════════════════════ D6: AI Copilot 深度集成
            D6.1 LLM意图解析 → D6.2 YAML生成 → D6.3 Pipeline → D6.4 Dashboard
```

---

## 五、文件变更清单 (新增/修改)

### 新增文件 (~20个)

```
pkg/marketdata/
  eventbus.go              # D1.1 EventBus pub/sub
  adapter.go               # D1.8 DataAdapter (历史/实时切换)
  tushare_provider.go      # D1.3 Tushare Provider 实现
  postgres_provider.go     # D1.4 PostgreSQL 直读
  akshare_provider.go      # D1.5 AkShare 适配
  http_provider.go         # D1.6 通用 HTTP 适配
  cached_provider.go       # D1.7 Redis 缓存装饰器

pkg/backtest/
  batch.go                 # D2 BatchEngine + BatchTask/Result
  batch_scorer.go          # D2 评分 + 分级算法
  batch_csv.go             # D2 CSV 任务解析

pkg/strategy/
  loader.go                # D3 PluginLoader (.so 热加载)
  plugins/
    td_sequential.go       # D5 TD Sequential (神奇九转)
    bollinger_mr.go        # D5 Bollinger Mean Reversion
    vol_price_trend.go     # D5 Volume-Price Trend
    vol_breakout.go        # D5 Volatility Breakout

pkg/live/
  trader.go                # D4 LiveTrader 接口定义
  mock_trader.go           # D4 MockTrader (测试用)
```

### 修改文件 (~8个)

```
pkg/marketdata/provider.go     # D1.2 增强: Name()/CheckConnectivity()/GetTradingCalendar()
pkg/backtest/engine.go         # D1.9 接受 DataAdapter; D2 集成 BatchEngine
cmd/analysis/main.go           # D1.10 注册新 API routes; D3 注册 plugin API
config.yaml                    # D1.10 data.adapters 配置段
internal/config/config.go      # D1.10 解析 adapters 配置
docs/SPEC.md                   # 更新 MarketData 接口定义
docs/ARCHITECTURE.md           # 更新 Event-Driven 架构图
ROADMAP.md                     # 新增 Phase 3 sprints
```

---

## 六、不做的事 (明确排除)

| 排除项 | 原因 | 未来考虑 |
|--------|------|----------|
| ❌ K线蜡烛图前端 | 工程量大，非核心差异化；净值曲线+买卖标记已实现(Chart.js) | Phase 4 |
| ❌ 实盘交易实现 | 需券商API对接，法律风险 | Phase 4+ |
| ❌ WebSocket 实时推送 | 当前无实时数据源 | 接入实盘时再做 |
| ❌ Webhook 通知 | 低优先级 | Phase 4 |
| ❌ Python 策略支持 | 与 Go native 原则冲突 | 不考虑 |
| ❌ 照搬三省六部命名 | 文化隐喻增加认知负担 | 保持工程命名 |
