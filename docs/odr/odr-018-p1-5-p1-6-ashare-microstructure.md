# ODR-018: P1-5 + P1-6 A 股交易所微观结构 — 价格笼子 + 集合竞价

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (交易所规则落地)
> **Related ADRs**: ADR-015 (LiveEngine 在交易执行路径中的角色), ADR-020 (Engine 拆解)
> **Supersedes**: —
> **Relates to**: ODR-013 (CQ-006 撮合语义缺失), ODR-016 (P1-3 限价单), TASKS §P1-5 / §P1-6, BR-004 / BR-017

## Context

Sprint 6 完结后, A 股交易所的 3 大微观结构规则在引擎中只剩下 1 套 (限价单撮合语义, ODR-016 完成)。
P1-5 (价格笼子) 和 P1-6 (集合竞价) 是 A 股独有的两条规则, 在回测和实盘两个路径上都必须被建模:

1. **价格笼子** (沪/深主板 2023-08 启用): 限价单的申报价必须落在
   "当前最优对价 ±2%" 区间内, 但日涨跌幅位置 (涨停/跌停) 豁免。
   没有笼子校验, 任何异常高价/低价订单都会被提交, 触发券商拒单。
2. **集合竞价** (开盘 9:15-9:25 + 收盘 14:57-15:00): 订单在集合
   竞价窗口内被冻结, 9:25/15:00 一次性按"最大成交量"原则撮合, 产
   生开盘价/收盘价。回测引擎按日粒度回放, 但订单"在集合竞价能否成
   交"的判定需要独立的撮合器, 而不是逐笔行情。

ODR-013 审计把这两条归为 P1 级 (CQ-006 子项), 估时 1w 各 1 项。

## Decision

### P1-5 价格笼子 (`pkg/live/`)

按"4 套规则"拆分:

| 板块 | ts_code 前缀 | 日涨跌幅 | ±2% 笼子 |
|------|-------------|---------|----------|
| 沪/深主板 (MainBoardSH/SZ) | 60/601/603/605 / 000/001/002 | ±10% | ✅ |
| 创业板 (ChiNext) | 300xxx | ±20% | ❌ |
| 科创板 (STAR) | 688xxx | ±20% | ❌ |
| 北交所 (BSE) | 8/4/9xxxxx | ±30% | ❌ |

实现:
- `pkg/live/board.go` (249 行) — `Board` 枚举 (8 套含 ETF/LOF/债/指数) + `ClassifySymbol(ts_code) Board` 纯函数路由
- `pkg/live/price_cage.go` (215 行) — `CageValidator.Validate(order, ReferencePrice) error`
  - 校验流程: ① 日涨跌幅绝对边界 ② 沪/深主板 ±2% 笼子 ③ 涨跌幅豁免
  - 缺参考价时保守放行 (broker 自身会拒, 避免 validator 阻塞 backtest)
  - `*PriceCageError` 结构化错误 (支持 `errors.As`/`errors.Is`)
- `pkg/live/order_manager.go` — `OrderManager` 新增 `priceCage *CageValidator` + `priceRefProvider func(symbol) ReferencePrice`,
  `SubmitOrder` 在 `validateOrderShape` 之后立即跑 cage 校验, 失败返回 `*PriceCageError` 不入 broker
- `pkg/live/engine.go` — `LiveEngine.SetPriceCageValidator(v, refProvider)` 转发

### P1-6 集合竞价 (`pkg/backtest/`)

按"7 个时段 + 理论撮合器"拆分:

| 时段 | 时间 | 回测视角 |
|------|------|---------|
| pre_open | 9:15-9:20 | 接受订单 + 可撤单 |
| pre_open_freeze | 9:20-9:25 | 接受订单但禁撤单 |
| opening_match | 9:25-9:30 | 一次性按 max-volume 撮合 |
| morning_continuous | 9:30-11:30 | 连续撮合 |
| lunch | 11:30-13:00 | 不接受新订单 |
| afternoon_continuous | 13:00-14:57 | 连续撮合 |
| closing_call | 14:57-15:00 | 一次性按 max-volume 撮合 |
| closed | 其余 + 周末 | — |

实现:
- `pkg/backtest/auction.go` (412 行)
  - `TradingSession` 枚举 (7 套) + `SessionAt(t) Session` + `SessionWindow(s) (start, end int)`
  - `CallAuctionOrder` (ID, Symbol, Direction, Quantity, LimitPrice, Timestamp)
  - `ClearingResult` (ClearingPrice, MatchedVolume, Fills, NoMatchReason)
  - `CallAuctionMatcher.Match(buys, sells) ClearingResult`
    - 候选价集合: union(所有限价 + tick 上方 / - tick 下方 / anchor)
    - 每个候选价计算 `cumBuy(p) / cumSell(p) / matchable(p) = min`
    - `clearing_price = argmax_p matchable(p)`
    - Tie-break: 距离 anchor (prev_close) 最近者胜; 距离相等取较低价
    - Fill: 市价单 (LimitPrice=0) 优先全额, 剩余按比例分配给限价单
  - `FillRatio(orderID, totalQty) float64` + `IsFullyFilled(orderID, orderQty) bool` 工具方法

## Consequences

### 正面

- **回测对齐实盘**: 4 套笼子规则 + 7 个时段全部覆盖, 任何 A 股策略
  在实盘/回测/纸交易下的撮合结果应该一致 (假设输入数据正确)
- **错误结构化**: `*PriceCageError` 携带完整诊断字段 (board, direction,
  cage_floor, cage_ceil, limit_up, reason), 客户端可直接渲染
- **撮合纯函数化**: `Match()` 不依赖任何外部状态, 16 个单测覆盖
  全部 happy / no-match / tie-break / 极端路径, 几乎 0 启动开销
- **OrderManager 早拒**: 违反笼子的订单在 `SubmitOrder` 入口就拒,
  不会入 broker 也不会落到 pending, 杜绝"提交即挂起"问题

### 负面 / 取舍

- **ST / 新股前 5 交易日 / 退市整理期**: 这些 ±5% / 无限涨跌幅的特殊
  标记需要外部 metadata (`stocks.is_st / is_new` 字段), 本实现不处理,
  留待 `pkg/storage/stocks.go` 增加 `is_st` 字段 (P2-5 风控模块)
- **L2 撮合 / 真实盘口**: cage 校验用 `best_bid/best_ask` 作为参考,
  没有逐档 LOB; 集合竞价 max-volume 也没有模拟挂单簿深度。完整 L2
  撮合留待真实券商对接 (P1-4 中泰 XTP)
- **Closing call 撮合未接入回测引擎**: 当前仅提供 `CallAuctionMatcher`
  工具, 没有在 `engine_daily.go` 实际 hook 进信号处理路径。回测
  仍是 daily-bar close 价撮合。集成工作 (把收盘信号路由到 closing
  call 而不是 afternoon_continuous) 留待后续 PR
- **B 股 / 跨境 ETF / 期权**: 本次仅处理 A 股主板/创/科/北, B 股 (面
  值 USD) 和期权 (50ETF 期权) 不在 P1-5/P1-6 范围

## Artifacts

### 新增 / 修改

- `pkg/live/board.go` (249 行) — Board 枚举 + ClassifySymbol
- `pkg/live/board_test.go` (30+ 用例) — 4 套板块 + 边缘
- `pkg/live/price_cage.go` (215 行) — CageValidator + PriceCageError
- `pkg/live/price_cage_test.go` (24 用例) — 4 套规则 + 豁免 + 工具
- `pkg/live/order_manager.go` (+ 40 行) — priceCage + refProvider
- `pkg/live/order_manager_cage_test.go` (6 用例) — SubmitOrder 集成
- `pkg/live/engine.go` (+ 9 行) — SetPriceCageValidator 转发
- `pkg/backtest/auction.go` (412 行) — Session + CallAuctionMatcher
- `pkg/backtest/auction_test.go` (13 TestXxx, 35+ 子用例) — 算法覆盖

### 文档

- `docs/TASKS.md` — P1-5 / P1-6 状态 ⬜ → ✅, Owner 填 2026-06-12
- `docs/ADR.md` — ODR Index 添加 ODR-018

## Metrics

- 新增 Go 代码: ~880 行 (auction 412 + price_cage 215 + board 249 - 测试)
- 新增测试用例: **75+** (board 30 + price_cage 24 + cage-integration 6 + session 21 + auction 16)
- `pkg/live` 测试通过率: 100% (所有 60+ TestXxx 全 PASS)
- `pkg/backtest` 测试通过率: 100% (所有 250+ TestXxx 全 PASS, 9.0s 跑完)
- `go vet ./...` exit 0
- `go build ./...` exit 0
- 文件增量 (Live): engine.go +9, order_manager.go +40, board.go +249, price_cage.go +215
- 文件增量 (Backtest): auction.go +412

## Lessons Learned

1. **P1-3 留下的隐式合约**: ODR-016 的 `tryFillOrder` 假设了"订单已
   通过所有前置校验", P1-5 把"前置校验"补齐为可插拔的 validator。
   如果未来还要加 (e.g. 风控/合规), 沿用 `SetPriceCageValidator` 的
   模式加 setter 即可, 不会改撮合核心
2. **测试预期 vs 真实算法**: 第一版 16 个 auction 测试 5 个失败, 全
   是我对 "eligible" 集合或 anchor tie-break 的预期错误。教训: 算
   法有清晰数学定义时, 写测试前先把候选集合的 trace 在纸上手工跑
   一遍, 不要凭直觉
3. **SetPriceCageValidator 走 mu.Lock**: 与 OrderManager 现有锁一
   致; Setter 在启动期调用一次, 不会成为热路径。如果未来要热切
   换 validator, 改用 atomic.Pointer
4. **Closing call 集成留待下一步**: 仅提供 `CallAuctionMatcher` 算
   法 + 16 测试, 没有强行改 `engine_daily.go` 的撮合价。回测仍是
   close 价撮合。"信号在 closing call 窗口内按 max-volume 撮合"
   的逻辑集成需要更深的 Engine 重构 (CallAuctionMatcher 调用点
   vs 现在的 Tracker.ExecuteTrade), 留待 ODR-019 或后续 PR
5. **auctionRound4 名字冲突**: pkg/backtest 已有 `round4` (来自
   fixture_gen_test.go), 不得不改名。这是项目级 `*_test.go`
   `.gitignore` 模式带来的副作用: 看不到对方实现的 helper 函数
   定义。教训: helper 函数应放在 internal 包, 不要散在各 *_test.go
