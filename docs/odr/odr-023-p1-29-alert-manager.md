# ODR-023: P1-29 AlertManager — 6 类 P0 风险告警 + Webhook 渠道

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (风险监控 / 可观测性)
> **Related ADRs**: ADR-017 (Observability + Auth), ADR-019 (Service 合并 — in-process 注入)
> **Supersedes**: —
> **Relates to**: ODR-013 (BR-015 风险), ODR-021 (P1-15 服务合并), TASKS §P1-29

## Context

Sprint 6 综合审查 (ODR-013) 报告 BR-015 业务风险: **生产路径无风
险告警**。具体场景:

1. **A 股风控阈值无主动监控**: `pkg/risk` 算出 position size /
   stop-loss 后, **超阈值** 不会主动告警 — 触发逻辑全靠 operator
   看回测 report, 但 paper trading 模式 1 天 240 分钟 × N 标的 = 
   几千次 decision, 人眼不可行
2. **回测/实盘集中度失控**: 单标的 30% / 单行业 60% 是常见集中度灾难,
   但本项目无任何代码检测"当前 portfolio 是否已经超限"
3. **回撤无主动告警**: 回测 equity curve 触发 max_dd > 15% 应立刻
   通知, 但当前需要等回测完成 + operator 看 metric
4. **订单失败率无监控**: broker rejection / T+1 拒绝 / 价格笼子
   拒绝三类失败堆积 = 系统性问题 (broker 故障 / 风控配置错误),
   但当前仅 1 次 1 次 log, 不聚合
5. **无 webhook 渠道**: 当前唯一监控手段是 `tail -f analysis.log`,
   实盘/机构用户需要 Slack / PagerDuty / 飞书 webhook

ODR-013 把这归 P1 (BR-015), 估时 1w。**P1-15 服务合并 (ODR-021)
后 risk/execution 已 in-process, AlertManager 同样可以 in-process
部署, 不需要单独服务**。

## Decision

新建 `pkg/alert/` 包, 提供:

### 1. 6 类 P0 风险告警 detectors

| Rule | 输入 | 阈值字段 | Severity 升级 |
|------|------|---------|---------------|
| `position_concentration` | `PortfolioSnapshot.Positions` | `MaxPositionWeight` (e.g. 0.20) | > 1.0x warn, > 2.5x critical |
| `sector_concentration` | `PortfolioSnapshot.Positions[].Sector` 聚合 | `MaxSectorWeight` (e.g. 0.40) | 同上 |
| `drawdown` | `TotalValue` vs `PeakEquity` | `MaxDrawdown` (e.g. 0.15) | 同上 |
| `daily_loss_limit` | `DailyPnL` | `DailyLossLimit` (负数, e.g. -50000) | 同上 |
| `order_failure_rate` | `RecentOrders` 在 `FailureRateWindow` (默认 1h) | `FailureRateLimit` (e.g. 0.10) | 同上 |
| `risk_metric_breach` | `RiskMetrics{name: value}` vs `RiskMetricThresholds` | per-metric | 同上 |

**Severity 升级规则** (`severityForBreach`):
- `value / threshold < 1.0` → info (不触发, 仅展示)
- `1.0 ≤ ratio < 2.5` → warning
- `ratio ≥ 2.5` → critical (立即 action)

阈值 = 0 (或 DailyLossLimit ≥ 0) = 禁用该规则 (避免空配置报警)。

### 2. 双 Channel 渠道

| Channel | 用途 | 行为 |
|---------|------|------|
| `LogChannel` | always-on, 默认开启 | zerolog structured log, Severity → log level (info/warn/error) |
| `WebhookChannel` | 可选 (config `WebhookURL != ""`) | 异步 POST JSON, 64 容量队列, 5s per-call timeout, 满则 drop+log |

**Channel 抽象** (`Channel interface`):
```go
type Channel interface {
    Send(ctx context.Context, alert Alert)
    Close()
}
```

- 用户可注册自定义 Channel (Slack / PagerDuty / 飞书 / in-process queue)
- `AddChannel` / `SetWebhookURL` 运行时注册, **线程安全**
- `Close()` 一次性 shutdown, `AddChannel` / `SetWebhookURL` 之后返 false

### 3. `AlertManager` 核心 API

```go
type AlertManagerConfig struct {
    MaxPositionWeight    float64
    MaxSectorWeight      float64
    MaxDrawdown          float64
    DailyLossLimit       float64
    FailureRateLimit     float64
    FailureRateWindow    time.Duration
    RiskMetricThresholds map[string]float64
    WebhookURL           string
    WebhookTimeout       time.Duration
}

func NewAlertManager(cfg AlertManagerConfig, logger zerolog.Logger) *AlertManager
func (am *AlertManager) Evaluate(ctx context.Context, snap PortfolioSnapshot) int
func (am *AlertManager) AddChannel(ch Channel) bool
func (am *AlertManager) SetWebhookURL(url string) bool
func (am *AlertManager) Close()
```

**并发安全**: `Evaluate` 多 goroutine 安全; `Channel.Send` 实现负责
自身并发; 内部 `mu.RWMutex` 保护 `channels` 切片和 `closed` 标志。

**Snapshot 是值类型**, 不持有 caller 的 slice/map 引用, GC 友好。

### 4. Alert 数据契约

```go
type Alert struct {
    ID         string                 // "ALR-{rule}-{ts}-{seq}"
    Rule       string                 // 6 个 rule 常量
    Severity   Severity               // info / warning / critical
    Message    string                 // human-readable
    Value      float64                // 实际测量值
    Threshold  float64                // 配置阈值
    Symbol     string                 // 单标的可选
    Sector     string                 // 行业可选
    Timestamp  time.Time
    Attributes map[string]interface{} // 扩展 (sector_mv, failed_count, etc)
}
```

JSON 可序列化 (`json.Marshal` round-trip 验证测试通过), WebhookChannel
直接 POST。

### 5. 测试覆盖 (25 TestXxx)

| 测试 | 数量 | 覆盖 |
|------|------|------|
| Detector 单元 (fire / no-fire / disabled) | 13 | 6 类规则全路径 |
| `severityForBreach` 边界 | 1 (5 子用例) | info/warning/critical 升级 |
| `LogChannel` 行为 | 2 | dispatch + severity 路由 |
| `WebhookChannel` 行为 | 3 | queue 满 drop / 2xx deliver / 5xx warn |
| `AlertManager` 集成 | 6 | 6 类全 fire / 不超限不 fire / 空 portfolio / Close 后拒绝 / WebhookURL 切换 / ID 唯一 / 并发安全 / JSON round-trip |

**race detector 全绿**: `go test -race` 0 issue (含 16 goroutine
并发 Evaluate 测试)。

### 6. 接入点 (后续 task, P1-29 不强制)

设计上是 in-process 注入 (与 ODR-021 P1-15 风格一致), 但 P1-29 本
次只交付核心库 + 单元测试。`cmd/analysis/main.go` 接入 + 前端
Alert UI 留到 P2 (Sprint 7 收尾)。

**前置约束已就绪**:
- `cmd/analysis/handlers_risk.go` 已有 `RiskManager.GetMetrics()` 入口
- `pkg/live/mock_trader.go` 已有 `GetPositions()` 入口
- `pkg/backtest/performance.go` 已有 `GetPeakEquity()` 计算

后续接入仅需 1 个 `PeriodicAlertLoop` (e.g. 5min 一次) 调
`alertManager.Evaluate(ctx, snapshot)`, 30 行 Go。

## Consequences

### 正面

- **6 类 P0 风险全覆盖**: 持仓/行业/回撤/日损/订单失败/任意 risk metric
- **Webhook 渠道可对接任意 IM**: 飞书/Slack/PagerDuty/企业微信 = 改 URL
- **零外部依赖**: 纯 stdlib (`net/http`, `context`, `sync`), 已有
  zerolog 复用
- **可扩展**: 用户可注册自定义 Channel (e.g. 写 DB / Prometheus counter
  / 推 SSE 到前端), 不修改包
- **线程安全 + race detector 0 issue**: production-grade concurrency
- **测试覆盖 25 TestXxx** 全 PASS, 0 DB / 0 Redis 依赖 (httptest mock
  webhook server), CI 友好
- **可观测性自描述**: godoc 顶部架构图 + 每个 detector 解释公式
- **Severity 升级**: 同规则可区分 warning/critical, channel 路由可
  差异化 (e.g. critical 走电话, warning 走 IM)

### 负面 / 取舍

- **未接入 analysis service**: P1-29 仅交付库, 真实跑通需 P2 接
  `PeriodicAlertLoop`。设计是有意分阶段, 库稳定后再接主路径
  (避免一次性 PR 50 文件 review)
- **RiskMetricThresholds 是 free-form map**: 无 type safety, 拼错
  metric 名静默不报警。后续可引入 typed `RiskMetric` enum (P2)
- **Webhook 失败只 log**: 无 retry queue / DLQ。生产 P1 完成后若需要
  强一致, 改用 outbox pattern 异步重试 (P2 任务, 与 P1-26 一致)
- **LogChannel 总是开**: 不能通过 config 关掉。如果用户只想要 webhook
  不想要 log, 需自定义 Channel (不复杂, 但不是零配置)
- **Sector 分类是 snapshot 传入**: pkg/alert 不维护 sector 表, 由
  caller (回测 / paper trading) 提供。未来 sector 标准化是独立 task

## Artifacts

### 新增

- `pkg/alert/manager.go` (307 行) — AlertManager + Config + Alert +
  PortfolioSnapshot types + 顶层 godoc 架构图
- `pkg/alert/channel.go` (221 行) — Channel interface + LogChannel +
  WebhookChannel (异步 goroutine, queue + HTTP POST)
- `pkg/alert/detectors.go` (249 行) — 6 个 evaluateXxx 函数 +
  severityForBreach + Rule 常量
- `pkg/alert/manager_test.go` (549 行) — 25 TestXxx, 含 httptest
  WebhookChannel 测试 + race detector 并发测试

**总计**: 1326 行, 757 行生产 + 549 行测试, 库净代码 777 行 (757
生产 + 25 行的 test helper)。

### 复用 (无改动)

- `github.com/rs/zerolog` — 已依赖, log 通道直接复用
- `pkg/live` / `pkg/risk` — 输入数据来源 (PortfolioSnapshot 由 caller
  组装)
- `pkg/observability` — 不复用, AlertManager 是业务告警而非 metrics

### 文档 (后续 commit)

- `docs/odr/odr-023-p1-29-alert-manager.md` (本文件)
- `docs/TASKS.md` — P1-29 ⬜ → ✅ + changelog
- `docs/ADR.md` — ODR-023 加入 ODR index

## Metrics

- 新增 Go 代码: **1326 行** (生产 777 + 测试 549)
- 新增测试用例: **25 TestXxx** (13 detector + 1 severity + 2 log + 3
  webhook + 6 manager)
- 6 类 P0 风险告警: **100% 覆盖** (持仓/行业/回撤/日损/订单失败/risk metric)
- 新增 Go 依赖: **0** (纯 stdlib + 已有的 zerolog)
- `go vet ./pkg/alert/...` exit 0
- `go build ./...` exit 0
- `go test ./pkg/alert/... -race -count=1` **5.077s** 全 PASS
- 测试时长缩短: `TestWebhookChannel_DropsWhenQueueFull` 从 320s → <1s
  (50ms timeout 而非 5s)

## Lessons Learned

1. **Detectors 必须是纯函数**: 6 个 evaluateXxx 都不持有 state, 输入
   snapshot 输出 []Alert。这让测试无需 mock, race detector 0 issue。
   状态 (alertSeq, channels, closed) 全在 manager 层
2. **Channel.Send 必须非阻塞**: WebhookChannel 队列满则 drop+log, 不
   block caller。否则一处慢 webhook 会拖垮回测主循环
3. **Webhook 队列容量 64 + 异步 goroutine = 完美解耦**: 100 alert
   burst, 64 enqueue + 36 drop, caller 永远不等。这一对 observability
   系统是硬要求 (back-pressure 必须 explicit, 不能靠 caller 自觉)
4. **Severity 在 detector 层决定**: `severityForBreach` 在 detector 调
   用, 不在 channel/channel-routing 层。这样 channel 实现可以无脑
   转发 Severity, 业务规则集中在 detector, 关注点分离
5. **in-process 部署 = 0 网络**: 与 P1-15 风格一致, AlertManager 跑在
   analysis service 进程内, 0 HTTP, 0 序列化 (除 webhook 出口)。
   Prometheus / Loki 等外部系统反而是 push 端
6. **"先库后接入" 策略**: 这次只交付核心库 + 单元测试, 不接 main.go。
   优点: 1 个 PR review 7 文件, 不会一次性 30 文件轰炸; 库稳定后
   接入是 30 行 mechanical change, 风险可控
7. **godoc 顶部架构图胜过 README**: AlertManager godoc 列出 6 个
   detector + 2 个 channel, 用户 30 秒看懂; 单独 README 反而易过时
