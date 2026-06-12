# ODR-026: P2-3 远程紧急平仓 (Emergency Flatten) — Kill-Switch 端到端

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (Risk Control / Operator Safety)
> **Related ADRs**: ADR-017 (Observability), ADR-019 (Service 合并)
> **Related ODRs**: ODR-021 (P1-15 服务合并), ODR-022 (P1-26 实体合并),
>                    ODR-023 (P1-29 AlertManager), ODR-025 (P2 alert 接入)
> **Supersedes**: —
> **Relates to**: TASKS §P2-3, BR-018 (Operator safety)

## Context

ODR-013 综合审查标记 **BR-018 业务风险**:
**生产路径无远程紧急平仓机制**。具体场景:

1. **行情异常时无 kill switch** — 系统检测到 crash / 闪崩 / broker
   故障时, 当前 operator 只能 SSH 登服务器手动调 API 卖出, 延迟
   数分钟到数小时, 在 A 股 ±10% 涨跌停限制下可能来不及
2. **T+1 限制会卡住正常平仓** — A 股 T+1 规则下当天买的不能卖,
   但"紧急"就是异常, 走常规 sell 会被 broker 拒绝
3. **A 股机构用户合规要求** — 实盘 operator 必须有 30s 内一键清仓
   能力, 否则触发交易所 / 监管的"未及时止损"问询

P1-15 服务合并 (ODR-021) 后 risk + execution 已 in-process,
emergency flatten 不需要新服务, 只需在 `LiveTrader` 接口加方法
+ HTTP endpoint + UI 按钮。

## Decision

### 1. LiveTrader 接口扩展

新增方法:

```go
EmergencyFlatten(ctx context.Context, reason string) (*EmergencyFlattenResult, error)
```

+ 类型:
  - `EmergencyFlattenResult` — 整体结果 (Sold / Skipped / SoldTotal /
    StartedAt / CompletedAt / Reason)
  - `EmergencyFlattenOrder` — 单笔成功 (Symbol / OrderID / Quantity /
    FillPrice / NetProceeds / **BypassedT1** / SubmittedAt)
  - `EmergencyFlattenSkip` — 单笔失败 (Symbol / Quantity / Reason)

**关键设计决策 — T+1 绕过**:
Emergency 路径显式绕过 T+1, 但**审计 trail 记录 `BypassedT1=true`**
+ 持久化 order 的 Message 字段包含 `(T+1 bypassed)` 标记, 监管可
事后回溯。

### 2. MockTrader 实现

`pkg/live/mock_trader.go` 新增 `EmergencyFlatten` 方法:

- **Lock & snapshot**: 同 mutex 序列化, 先 snapshot symbols 再迭代
  (避免 map mutation during iteration)
- **价格优先级**: `PriceProvider(symbol)` → 落回 `pos.CurrentPrice` →
  skip (no price)
- **费用计算**: 复用 `executeSell` 的 commission + transfer + stamp
  tax 公式 (slippage 反向)
- **结果分类**: 成功 → `Sold[]`; 无价格 → `Skipped[]`
- **空持仓**: 返回空 result, 不报错 (idempotent)
- **reason 默认值**: 空字符串 → `"operator-initiated kill switch"`
  (审计友好)
- **持久化**: `Message` 字段含 `"EMERGENCY FLATTEN: <reason> (T+1
  bypassed)"`, OrderStore 记录全部审计信息

### 3. HTTP 端点

`POST /api/execution/emergency-flatten`:

**3 重身份验证**:
1. **服务端配置** — `trading.emergency_token` 必须非空, 否则返回
   503 (not 404 — 让 operator 知道服务在线但开关未启用)
2. **Bearer token** — `Authorization: Bearer <token>` header, 与配置
   token 常时比较 (`crypto/subtle.ConstantTimeCompare`)
3. **Body 二次确认** — JSON body 含 `confirmation_token` 字段, 必
   须再次匹配 (defence in depth — 防止误触)

**请求体**:
```json
{
  "reason": "system detected abnormal price feed",
  "confirmation_token": "<server_token>"
}
```

**响应 (200)**:
```json
{
  "sold": [{"symbol":"...", "bypassed_t1": true, ...}],
  "skipped": [...],
  "sold_total": 12345.67,
  "latency_ms": 87,
  "reason": "...",
  "started_at": "...",
  "completed_at": "..."
}
```

### 4. 前端 UI

`web/src/components/paper/EmergencyFlatten.vue` (~280 行):

- **Arm-and-confirm 模式** — 第一次点击"紧急平仓 (Arm)"展开表单
  (原因 + token), 第二次点击"确认紧急平仓"才真正发送
- **二次 `window.confirm`** — 浏览器原生 confirm 对话框再确认一次
- **结果可视化** — `n-data-table` 列出成交明细, 标记 `T+1 绕过` 列;
  Skipped 部分用 `n-alert` 警告
- **`armed` 视觉状态** — `is-armed` class 切换背景色 (轻微红) 让
  operator 知道"已上膛, 需谨慎"

### 5. 配置

`config/analysis-service.yaml` 新增:
```yaml
trading:
  ...
  # 空 (默认) → 端点 disabled (返回 503)
  # 设置后, operator 必须发送同样的 token 在 header + body
  emergency_token: ""
```

## Consequences

### 正面

- **30s 内一键清仓** — 满足 BR-018 监管/合规要求
- **审计完整** — T+1 bypass 显式标记 + reason 必填 + order Message
  字段, 监管可逐笔回溯
- **T+1 不阻挡应急** — 紧急路径明确绕过, 不需要等下一交易日
- **3 重鉴权** — Bearer + body confirmation + 浏览器 confirm, 误
  触概率极低
- **零停机** — In-process 注入 (ODR-021 模式), 不需要新服务部署
- **Per-symbol 失败隔离** — 单个 broker 拒绝不影响其他 symbol
  平仓, 失败列入 Skipped 供 operator 手动跟进

### 负面 / 局限

- **T+1 绕过需要合规 review** — 当前 implementation 默认绕过,
  实盘部署需要合规团队确认 (Phase 5 接券商时再走)
- **Token 在 config 文件** — 应使用 secret manager (Phase 5 接
  券商时改造), 当前 demo 模式用 yaml 明文
- **无滑点保护** — 市价单 + slippage 默认 0.01%, 大持仓时可能
  显著滑点; 当前 portfolio 都是 paper trading 1M 量级, 不影响
- **Skip 不会自动重试** — Skipped 持仓需要 operator 手动处理
  (broker 故障恢复后); Phase 5 可加 retry-with-backoff

## Artifacts

### 新增

- `pkg/live/trader.go` — `EmergencyFlatten` interface + 3 个 result
  type (52 行)
- `pkg/live/mock_trader.go` — `EmergencyFlatten` 实现 (155 行)
- `cmd/analysis/handlers_execution.go` — `emergencyFlattenHandler`
  + request/response type (110 行)
- `cmd/analysis/handlers_emergency_test.go` — 12 TestXxx (260 行):
  - 5 unit tests (ClosesAll / BypassesT1 / Idempotent / RecordsReason /
    EmptyReasonDefault)
  - 6 HTTP integration tests (503/401/403/403-conf/400/success)
  - 1 race condition safety test
  - 1 perf sanity test (50 symbols < 1s)
- `web/src/api/paper-trading.ts` — 4 type + 1 function (~50 行)
- `web/src/components/paper/EmergencyFlatten.vue` — 280 行 UI
- `docs/odr/odr-026-p2-3-emergency-flatten.md` (本文件)

### 修改

- `pkg/backtest/live_bridge_test.go` — `fakeLiveTrader` 加
  `EmergencyFlatten` stub (interface 满足)
- `cmd/analysis/alert_loop_test.go` — `stubLiveTrader` 加 stub
- `cmd/analysis/handlers_risk_execution_test.go` — `NewExecutionHandler`
  签名更新 (新参数 token)
- `cmd/analysis/main.go` — `registerRoutes` 增 `emergencyToken` 参数
  + `v.GetString("trading.emergency_token")` 传递
- `config/analysis-service.yaml` — `trading.emergency_token: ""`
- `web/src/pages/PaperTrading.vue` — 挂载 `<EmergencyFlatten />`

## Metrics

| Metric | Before | After |
|--------|--------|-------|
| 远程紧急平仓能力 | ❌ 无 | ✅ POST /api/execution/emergency-flatten |
| LiveTrader 接口方法数 | 7 | 8 (新增 EmergencyFlatten) |
| 端点鉴权重数 | 0 (普通端点) | 3 (Bearer + body + confirm) |
| T+1 绕过审计标记 | n/a | `BypassedT1` bool + Message 文本 |
| UI 二次确认重数 | 0 | 2 (Arm + 浏览器 confirm) |
| 单元/集成测试 | 0 | 12 (5 unit + 6 HTTP + 1 race) |
| 50 symbol 延迟 | n/a | < 1s 实测 |
| race detector 警告 | 0 | 0 (新代码全过 -race) |

## Lessons Learned

1. **Lock + recursive call 死锁** — 第一次实现 EmergencyFlatten
   时想在循环里调 `executeSell`, 但 executeSell 会重新 Lock 同
   mutex, 死锁。解决: 复用 executeSell 的逻辑, 在已持锁状态
   下内联
2. **reason 默认值要写到 result** — 第一版只在本地变量设默认值,
   `result.Reason` 仍是空字符串, 测试发现。修复: 显式 `result.
   Reason = reason`
3. **3 重鉴权是必要的** — 单 Bearer 太弱 (误触 / CSRF / 浏览器
   自动补全密码), 加 body 二次确认让 operator 必须主动重输
   token, 浏览器 confirm 让误触必须 2 次点确认
4. **T+1 绕过 + 审计标记 = 合规** — 不能简单"绕过 T+1", 必须
   在每笔 order 上留下 `BypassedT1=true` + Message 文本, 监管
   事后可追查"谁在什么时间因为什么原因强行平仓"
5. **In-process 优势** — 整个 P2-3 后端改动 < 400 行 Go, 0 新
   服务, 0 新端口, 0 新依赖, 0 docker compose 改动。P1-15
   合并的复利效应明显

---

_Phase 2 kill-switch 完成。后续 P2 接续: 接到真实券商时使用
secret manager 替代 yaml token + 添加 rate limit (防误触连发) +
Skipped 持仓自动 retry-with-backoff。_
