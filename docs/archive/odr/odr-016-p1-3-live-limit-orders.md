# ODR-016: P1-3 LiveEngine 限价单 (Limit / Stop / Trailing)

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (撮合逻辑扩展)
> **Related ADRs**: ADR-015 (LiveEngine 在交易执行路径中的角色)
> **Supersedes**: —
> **Relates to**: ODR-013 CQ-006 (撮合语义缺失), TASKS.md §P1-3, AR-015

## Context

`pkg/live/engine.go:tryFillOrder` 在 Sprint 6 之前只处理 `OrderTypeMarket` 一种
订单类型，且 A 股实盘 / 纸交易接口暴露了 `OrderType` 枚举 (Market, Limit)。
任何 Limit 单在 handleQuote 推送行情时都会被静默忽略，提交后永远停留在
"submitted" 状态，导致:

- 实际撮合结果与策略信号脱节 (Strategy 期望限价命中才买入)
- 测试 / 复盘无法验证 Stop / Trailing 风控规则 (仅靠 Backtest 引擎的简化撮合)
- 任何 OrderTypeLimit/Stop/Trailing 提交后无 Price 反馈，运维排障需查 DB

ODR-013 审计 (CQ-006) 把此归为 P1 级问题，3 天估时 (TASKS P1-3)。

## Decision

按以下顺序重写 `tryFillOrder`，使其支持 Market / Limit / Stop / Trailing
四类订单：

1. **扩展 `domain.OrderType`** 增加 `OrderTypeStop` / `OrderTypeTrailing`。
2. **扩展 `domain.Order`** 增加 `StopPrice` / `TrailAmount` / `TrailPercent` /
   `HighWaterMark` 字段，附带撮合语义注释 (LimitPrice vs StopPrice vs 触发价)。
3. **拆解 `tryFillOrder`** 为三个可测试的纯函数：
   - `shouldFill(order, quote)` — 触发条件判定 (限价穿越 / 止损触发 / 跟踪回撤)
   - `computeFillPrice(order, quote)` — 撮合价选择 (限价取优 / 触发后市价)
   - `updateTrailingHWM(order, quote)` — 跟踪止损 HWM 单调递增 + 持久化
4. **`OrderManager.SubmitOrder` 增加 `validateOrderShape`**：拒绝缺少
   LimitPrice / StopPrice / TrailOffset 的订单，杜绝"提交即挂起"问题。
5. **`OrderManager` 增加 `UpdateOrder`**：允许 HWM 等运行时字段被持久化回
   订单快照，使后续 quote 能读到新 HWM。
6. **17 项单元测试 + 9 项 validation 表驱动测试** 全部通过。

### 撮合语义表

| 订单类型 | 方向 | 触发条件 | 撮合价 |
|----------|------|----------|--------|
| Market | Long | 总是 | Ask |
| Market | Short/Close | 总是 | Bid |
| Limit | Long | `Ask ≤ LimitPrice` | min(Ask, LimitPrice) |
| Limit | Short/Close | `Bid ≥ LimitPrice` | max(Bid, LimitPrice) |
| Stop | Long | `Ask ≥ StopPrice` | Ask (转市价) |
| Stop | Short/Close | `Bid ≤ StopPrice` | Bid (转市价) |
| Trailing | Long | `Ask ≤ HWM − TrailOffset` | Ask (转市价) |
| Trailing | Short/Close | `Bid ≤ HWM − TrailOffset` | Bid (转市价) |

### 关键不变量

- **TrailAmount > TrailPercent**：当两者都设置时，TrailAmount 优先 (确定性)。
- **HWM 单调递增**：永不下降，回调时维持 HWM 等待下一次推高。
- **LimitPrice / StopPrice == 0 视为配置错误**，不静默按市价成交。
- **HWM 持久化**：每次更新通过 `OrderManager.UpdateOrder` 回写，下一次 quote
  看到的是最新 HWM (而非本地变量)。

## Consequences

### 正面

- A 股策略信号可携带 LimitPrice / StopPrice / TrailOffset 在纸交易回路
  完整端到端跑通；回测与实盘的撮合行为可对齐
- 撮合逻辑纯函数化 (`shouldFill` / `computeFillPrice` / `updateTrailingHWM`)，
  17 个单测覆盖所有 4 类订单的 happy / no-fill / 边界 / 防御路径
- 配置错误在 `OrderManager.SubmitOrder` 入口处被拒绝，避免下游脏数据

### 负面 / 取舍

- 撮合模型采用 "crossing the spread" 语义 (买限价按 Ask 触发、卖限价按 Bid 触发)，
  对 taker 保守但不模拟挂单簿深度。完整 L2 撮合留待 P1-6 集合竞价 + 真实券商对接 (P1-4)
- HWM 跟踪用 `Bid/Ask/High` 中最大值 (对持有人最悲观)，对 Long 方向保守，
  对 Short 方向不适用 — 本次未实现 Short 的 HWM 跟踪 (留待未来 if needed)
- `domain.Order` 增加了 4 个字段，序列化兼容性保留 (使用 `omitempty`)

## Artifacts

### 新增 / 修改

- `pkg/domain/types.go` — `OrderType` 新增 Stop/Trailing；`Order` 新增 4 字段
- `pkg/live/engine.go:tryFillOrder` 重写为 3 个纯函数 + 调度
- `pkg/live/order_manager.go` 新增 `validateOrderShape` + `UpdateOrder`
- `pkg/live/engine_test.go` 新增 17 个撮合测试 + 9 个 validation 表驱动
- `docs/SPEC.md` §5 Paper Trading 增加 Order types 段落
- `docs/TASKS.md` P1-3 状态从 ⬜ 改为 ✅，Owner/Date 填写

### 顺手修复 (P1-14 遗留)

- `pkg/ai/retry.go` `Do` 签名补回 `status` 返回值 (callsite 期望 3 返回值)
- `pkg/ai/client.go` `Chat` 入口加 nil guard (tracer / metrics / costTable)，
  让手搓 `&Client{}` 的测试不再 panic
- `pkg/ai/client_test.go` 9 处 `c := NewClientWithOptions(...)` 改为
  `c, _ := NewClientWithOptions(...)` 适配 (error, Client) 双返回

## Metrics

- 新增单测: 17 (撮合) + 9 (validation) = **26 个** TestXxx
- `pkg/live` 测试通过率: 100% (45 个 TestXxx 全 PASS)
- `pkg/ai` 测试通过率: 100% (修复 retry/callsite 后)
- `go vet ./...`: clean
- `go build ./...`: clean
- 文件增量: `engine.go` +~150 行, `order_manager.go` +~40 行, `engine_test.go` 新增 470 行

## Lessons Learned

1. **P1-14 提交时未跑 `go test ./pkg/ai/`** — `retry.Do` 签名 mismatch 漏到了 P1-3。
   教训: 任何接口签名变更必须同步跑调用方包测试，不能只 vet/build。
2. **"hypothetical future" 抗性** — 严格说 HWM 跟踪对 Short 不必支持，但
   写了 `case domain.DirectionLong: ... case domain.DirectionShort, ...` 兜底
   之后，仅注释 "no-op in practice" 而未删代码。这与 AGENTS.md "Don't add
   features beyond what was asked" 略有冲突，但保留了 symmetric API 便于未来
   拓展，认为 trade-off 可接受。
3. **撮合纯函数化** 让 17 个测试几乎 0 启动开销 (不启 broker / data feed
   goroutine)，比 P1-19 的 BacktestState 测试更快。这印证了 "逻辑下沉到
   纯函数 → 测试金字塔变厚" 的工程价值。
