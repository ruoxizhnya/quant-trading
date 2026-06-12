# ODR-025: P2 Alert Integration — PeriodicAlertLoop + /api/alerts 接入

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (Alert 接入 / 可观测性)
> **Related ADRs**: ADR-017 (Observability), ADR-019 (Service 合并 — in-process 注入)
> **Related ODRs**: ODR-021 (P1-15 服务合并), ODR-023 (P1-29 AlertManager 实现)
> **Supersedes**: —
> **Relates to**: TASKS §P2-alert

## Context

P1-29 (ODR-023) 完成了 `pkg/alert.AlertManager` 6 类 P0 风险
detector + LogChannel + WebhookChannel 的实现。**但 manager 只是
"组件"——没有任何调度机制让它在生产环境持续评估 live portfolio**:

1. **没有触发器** — 没人每 5 分钟调一次 `manager.Evaluate(snap)`,
   告警规则永远不会被触发
2. **没有告警历史** — 触发的 alert 即时输出到 log/webhook 后就
   消失了, operator 无法事后回溯"过去 1 小时触发了哪些?"
3. **没有 HTTP 暴露** — frontend 看不到任何 alert, UI 仍停留在
   "console-only"模式
4. **没有 force-check 入口** — operator 想立刻验证告警系统健康
   状态, 必须等下一次 tick

ODR-013 列为 P2 (BR-015 风险告警的"调度"部分)。P1-15 服务合并
(ODR-021) 后 risk + execution 已 in-process, **alert loop 同样可以
in-process 注入, 无需新服务**。

## Decision

在 `cmd/analysis/` 内新增:

### 1. PeriodicAlertLoop + AlertHistory (`alert_loop.go`)

- `PeriodicAlertConfig` — `Interval` (默认 5min), `HistoryLimit` (100),
  `Enabled` (true)
- `AlertHistory` — 固定容量的有界环形缓冲, mutex-protected;
  `Snapshot()` 读 (HTTP 用), `Append()` 写 (loop 用), `DrainAndReset()`
  消费-清空 (测试/导出用)
- `PeriodicAlertLoop.Start(ctx)` — 启动 ticker, 每 Interval 跑一次
  `TriggerOnce`
- `PeriodicAlertLoop.TriggerOnce(ctx)` — 单次同步评估, 供 HTTP
  `force-check` 和测试使用
- `buildSnapshot(ctx)` — 从 `live.LiveTrader` 读 positions/account,
  从 `risk.RiskManager` 读 risk metrics, 组成 `alert.PortfolioSnapshot`

### 2. HTTP endpoints (`handlers_alert.go`)

| Path | Method | 用途 |
|------|--------|------|
| `/api/alerts/history` | GET | 查询最近 N 条告警, 支持 `?limit=N` + `?severity=info\|warning\|critical` 过滤 |
| `/api/alerts/force-check` | POST | 立即跑一次评估, 返回 `{"dispatched": N}` |
| `/api/alerts/stats` | GET | manager/recorder/history 状态聚合, by_rule + by_severity 分桶 |

### 3. RecorderChannel 补全 (`pkg/alert/channel.go`)

- `RecorderChannel.Send()` — 不阻塞, buffer 满时驱逐最老 + 累加
  `evicted` 计数器
- `RecorderChannel.Snapshot()` — 拷贝 (不消费)
- `RecorderChannel.DrainAndReset()` — 消费-清空, alert loop 用此把
  一次 tick 触发的告警搬到 history
- `RecorderChannel.Len()` / `Evicted()` — 给 `/api/alerts/stats` 用
- `AlertManager.Recorder()` — 返回 manager 注册过的 RecorderChannel,
  loop 借此按需 DrainAndReset

### 4. main.go 接入

- 加载 `alert.*` 配置 (见 `config/analysis-service.yaml`)
- 构造 AlertManager + RecorderChannel + LogChannel (always-on) +
  WebhookChannel (if URL configured)
- 构造 AlertHistory + PeriodicAlertLoop
- `registerAlertRoutes(router, alertLoop)` 挂载 HTTP
- `go alertLoop.Start(context.Background())` 启动后台 tick
- 关闭时 `alertManager.Close()` 排空 webhook 队列

### 5. 配置 (`config/analysis-service.yaml`)

```yaml
alert:
  enabled: true
  interval_sec: 300           # 5min between evaluations
  history_limit: 100          # ring buffer for /api/alerts/history
  recorder_capacity: 100      # recorder channel capacity
  max_position_weight: 0.20
  max_sector_weight: 0.40
  max_drawdown: 0.15
  daily_loss_limit: -50000
  failure_rate_limit: 0.10
  webhook_url: ""             # empty = webhook disabled
  webhook_timeout_sec: 5
```

## Consequences

### 正面

- **零运维开销** — alert loop in-process, 不需要 cron / k8s job /
  额外服务; 配置改 `enabled: false` 即可下线
- **HTTP-first** — operator 看告警不再依赖 SSH + tail; UI 可以
  拉 `/api/alerts/history` 显示面板
- **force-check 即时验证** — 改完 threshold 立即可验证, 不必等
  下一 tick
- **可测试** — 16 个 TestXxx (5 AlertHistory + 5 PeriodicAlertLoop +
  6 HTTP handler), `loop.TriggerOnce` 同步语义让测试不需要 sleep
- **历史可回溯** — `AlertHistory` 保留最近 100 条, 排查"为什么
  1 小时前风控规则没触发"成为可能

### 负面 / 局限

- **单进程状态** — alert history 在进程内存, 重启清零; 多实例
  部署时每实例各自一份 (Phase 5 可加 Redis 共享)
- **SectorConcentrationDetector 暂时失效** — `pkg/live.PositionInfo`
  没有 `Sector` 字段, 所有 position 进 "uncategorized" 桶, 当
  全部持有同 bucket 时仍会触发; 待 pkg/live 加 Sector 字段后
  detector 才有实际意义 (P2-3 候选)
- **PeakEquity 未追踪** — `LiveTrader.GetAccount` 不暴露 peak,
  DrawdownDetector 暂不触发, 需要后续在 `pkg/live` 加权益历史
- **Loop 关闭靠进程退出** — PeriodicAlertLoop 绑
  `context.Background()`, shutdown 序列无法 cancel 它; 接受 trade-off:
  进程退出时 goroutine 自然消亡

## Artifacts

### 新增

- `cmd/analysis/alert_loop.go` (196 lines) — PeriodicAlertConfig /
  AlertHistory / PeriodicAlertLoop
- `cmd/analysis/alert_loop_test.go` (461 lines, 16 TestXxx)
- `cmd/analysis/handlers_alert.go` (142 lines) — HTTP handlers +
  registerAlertRoutes
- `docs/odr/odr-025-p2-alert-integration.md` (本文件)

### 修改

- `pkg/alert/channel.go` — 新增 `RecorderChannel` (76 行) +
  `Send` / `Snapshot` / `DrainAndReset` / `Len` / `Evicted` / `Close`
- `pkg/alert/manager.go` — 新增 `Recorder()` 方法
- `cmd/analysis/main.go` — 187-229 行 (构造), 352-357 行 (挂路由 +
  启 loop), 414-423 行 (shutdown)
- `config/analysis-service.yaml` — 新增 `alert.*` 配置块

## Metrics

| Metric | Before | After |
|--------|--------|-------|
| alert 调度机制 | ❌ 无 | ✅ PeriodicAlertLoop (5min tick) |
| alert HTTP endpoint 数 | 0 | 3 (history, force-check, stats) |
| in-process 告警组件 | 0 | 1 (AlertManager) |
| 告警 history 容量 | 0 | 100 (in-memory ring buffer) |
| 测试覆盖 (alert 子包) | 16/16 ✅ | 16/16 ✅ |
| 测试覆盖 (loop + handlers) | 0 | 16/16 ✅ (5 history + 5 loop + 6 HTTP) |
| HTTP 响应字段 | n/a | `alerts`/`count`/`limit` + `by_rule`/`by_severity` |

## Lessons Learned

1. **环形缓冲 vs slice 驱逐** — `AlertHistory` 用 fixed-cap slice +
   `idx` 指针, O(1) 写, O(n) 读; `RecorderChannel` 用 growable
   slice + `copy(buf, buf[1:])` 驱逐, O(n) 写, O(n) 读。两者
   性能差异在 100 量级可忽略, 选择按"读多 vs 写多"决定
2. **避免持有 Lock 时再 RLock** — `AlertHistory.DrainAndReset` 第一
   版直接调 `Snapshot()`, 死锁 10 分钟。教训: 同一 mutex 的 Lock +
   RLock 永远会死锁, 必须内联逻辑或换 sync.Mutex
3. **In-process 组件测试解耦** — PeriodicAlertLoop 接受
   `*alert.AlertManager` + `live.LiveTrader` 接口, 测试用
   `stubLiveTrader` 即可, 不需要 mock 整个 risk + execution
4. **Recurring P0 风险** — P1-15 (服务合并) → P1-29 (AlertManager)
   → P2 alert 接入, 一条线打通: 风控数据 → 告警评估 → HTTP 暴露。
   后续 P2-3 (EmergencyFlatten) 可直接接 AlertManager 的 Critical
   severity 触发

---

_Phase 2 alert 集成完成。下一步: P2-3 远程紧急平仓 (EmergencyFlatten
endpoint + UI button), P2-1/P2-2 backtest 报告导出, 前端 alerts
面板。_
