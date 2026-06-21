# ODR-030: P2-13 退市整理期 + 强制清仓引擎

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation (Compliance / Live Trading)
> **Related ADRs**: ADR-019 (服务合并 in-process 注入), ODR-021 (P1-15 服务合并)
> **Related ODRs**: ODR-026 (P2-3 紧急平仓), ODR-028 (P2-4/5/6 合规), ODR-029 (P2-7 减持)
> **Supersedes**: —
> **Relates to**: TASKS §P2-13, BR-013 (退市合规)

## Context

A 股退市分 4 类 (交易类 / 财务类 / 规范类 / 重大违法), 触发后进入
**退市整理期** (15 个交易日), 期间仍可交易但涨跌幅受限 (主板 ±10% /
创业板科创板 ±20% / 北交所 ±30%), 整理期满后**摘牌** (`delisted_date`
当日 15:00 后停止交易, 持仓由清算所自动现金清算)。

券商在没有 stock-state 跟踪 + 强制清仓 gate 的情况下, 会出现两类
风险:

1. **客户风险**: 客户持仓在摘牌后无法交易, 现金清算价远低于摘牌前
   收盘价 (清算所通常按摘牌前最后 1 日均价 × 90% 结算), 客户本金
   损失 10%+。
2. **券商风险**: 监管要求券商在整理期内**主动告知 + 限制买入 + 触发
   强平**, 否则券商被认定"未履行投资者适当性义务" (2024 CSRC 通报
   3 起券商因未及时平仓退市股被罚款百万级)。

P2-13 解决这个 gap: 提供 `StockStateRegistry` (状态机) +
`ForcedLiquidator` (强制清仓引擎), 接入 ODR-021 的 in-process
`live.LiveTrader`, 在 `pkg/live` 包内统一处理。

## Decision

### 1. 包结构: `pkg/live/stock_state.go` (单文件, 4 个核心对象)

```
pkg/live/
├── stock_state.go            (P2-13) — StockState 状态机 + 强制清仓 [本 ODR]
├── stock_state_test.go        (23 TestXxx)
├── board.go                   (既有) — 涨跌停 / 创业板 / 科创板 / 北交所
├── reconciliation.go          (P2-8)
├── trader.go                  (既有) — LiveTrader 接口
├── mock_trader.go             (既有) — MockTrader
└── ...
```

**为什么放在 `pkg/live` 不放 `pkg/risk`**: 退市是**交易生命周期
终结**事件, 与 `LiveTrader.EmergencyFlatten` (ODR-026) 紧耦合; 放
`pkg/risk` 会导致 `live` → `risk` → `live` 循环依赖 (P2-3 紧急平仓
已经把 `BypassedT1` audit 标记放在 `live` 包, 沿用同样原则)。

### 2. 状态机: 4 状态 + 单向转换

```go
type StockState string
const (
    StockStateListed    StockState = "listed"     // 正常上市
    StockStateSuspended StockState = "suspended"  // 临时停牌 (复牌回 Listed)
    StockStateDelisting StockState = "delisting"  // 退市整理期 (15 日)
    StockStateDelisted  StockState = "delisted"   // 已摘牌 (terminal)
)
```

**单向转换** (不允许逆向):
- `Listed → Suspended` (临时停牌)
- `Listed/Suspended → Delisting` (触发退市)
- `Delisting → Delisted` (整理期满, 摘牌)

`StockState.IsTerminal() bool` 仅 `Delisted == true`, 用于
ForcedLiquidator 跳过已摘牌的股票 (避免重复发单)。

### 3. `StockStateRecord` — 退市时间线

| 字段 | 语义 |
|------|------|
| `Symbol` | ts_code (`"600000.SH"`) |
| `State` | 当前状态 |
| `Reason` | 触发原因 (`"财务类退市-连续亏损"` / `"面值退市"` / `"重大违法"`) |
| `DelistingPeriodStart` | 整理期首日 (即 *ST → 整理期第一天) |
| `DelistingPeriodEnd` | 整理期末日 (摘牌前最后可交易日) |
| `DelistedDate` | 摘牌日 (当日 15:00 后停止交易) |
| `UpdatedAt` | 本记录最后一次更新 |

`IsInDelistingWindow(now, windowDays)` 检查 `State == Delisting` 且
`now ≤ DelistedDate + windowDays`, 用于 ForcedLiquidator 决策。

### 4. `StockStateRegistry` — 线程安全注册表

```go
type StockStateRegistry struct {
    mu      sync.RWMutex
    records map[string]*StockStateRecord
}
```

API: `Set(record) / Get(symbol) / List(state) / Delete(symbol) /
Snapshot()`。Snapshot 返回深拷贝, 调用方改不动内部状态 (与 P2-4
适当性 Registry 同款防御性拷贝, 沿用 ODR-028 第 5 条原则)。

测试: 50 goroutine × 100 次并发读写 + `-race` 全过, `t.Cleanup(reset)`
隔离。

### 5. `ForcedLiquidator` — 强制清仓引擎

```go
type ForcedLiquidator struct {
    registry *StockStateRegistry
    cfg      StockStateConfig
    logger   zerolog.Logger
}

type StockStateConfig struct {
    LiquidationWindowDays int  // default 5 (5 个交易日内)
    DryRun                bool // 测试模式只记录不发单
}

func (f *ForcedLiquidator) Scan(ctx context.Context, trader LiveTrader) (*LiquidationResult, error)
```

**算法**:
1. 遍历 registry 中所有 `State == Delisting` 的股票。
2. 对每只: `IsInDelistingWindow(now, cfg.LiquidationWindowDays)` 检查。
3. 通过的: 调用 `trader.EmergencyFlatten(symbol, reason)` (ODR-026 的
   `BypassedT1` 通道, 跳过 T+1 锁定), 走 P2-3 紧急平仓审计。
4. 收集结果, 返回 `LiquidationResult{Triggered[], Skipped[], Errors[]}`。

**为什么**接 `EmergencyFlatten` 而不是新加 `ForceSell`: 退市股属于
"必须立即清仓" 场景, 与 P2-3 kill-switch 同语义, 复用通道保证审计
记录格式统一, 监管回溯不需要查两套日志。

### 6. 与 P2-3 / P2-8 协同

- **P2-3 紧急平仓 (ODR-026)**: ForcedLiquidator 调用的是同一个
  `EmergencyFlatten` 方法, 但 bypass T+1 的原因从"风控紧急"扩到
  "退市强制", 审计 `Reason` 字段区分 (`"delisting_force"` vs
  `"risk_emergency"`)。
- **P2-8 券资金对账 (ODR-026-pre)**: 对账 Worker 同样可以读
  StockStateRegistry, 把已摘牌股从对账范围剔除 (`IsTerminal()` 检查),
  减少无意义的 diff。

### 7. 测试覆盖 (23 TestXxx)

- 状态机: 4 状态转换合法性 + IsTerminal
- Registry: Set/Get/List/Delete/Snapshot + 50 goroutine race-clean
- StockStateRecord 时间线: DelistingPeriodStart ≤ End ≤ DelistedDate
- IsInDelistingWindow: 边界 (now == DelistedDate) / 已 Delisted (false)
- ForcedLiquidator: DryRun 模式 / 多股混合 / Error 传播
- 集成测试: MockTrader + EmergencyFlatten 交互验证

## Consequences

### Positive

- **A 股退市规则时间线全维度覆盖**: 4 类退市 + 15 日整理期 + 5 日
  LiquidationWindow + 摘牌日清算, 全部在 StockStateRecord 时间线字段
  表达, 没有散落"if today == xxx"分支。
- **强制清仓接 P2-3 kill-switch**: 复用 `EmergencyFlatten` + `BypassedT1`
  审计通道, 不引入新发单路径, 监管回溯统一。
- **状态机 + Registry 同款设计语言**: 与 P2-4 适当性 / P2-7 减持的
  Registry 一致, 团队学习成本零。
- **DryRun 模式**: 上线前可以在生产环境扫描但不实际发单, 验证
  LiquidationResult 的预期, 降低"误强平"风险。
- **退市股自动从对账剔除**: P2-8 对账 Worker 接入后, 已摘牌股自动
  跳过, 减少无意义的 diff 告警。

### Negative

- **数据源依赖**: StockState 实际数据需要从交易所 / 券商股东通知
  推送, 当前由调用方 `Set` 注入 (与 P2-7 减持 Recent 同款"无状态 +
  调用方带数据"设计)。生产需要对接数据源 (ODR-026 之后的下个 sprint)。
- **5 日 LiquidationWindow 是经验值**: 实际券商策略可能是 3 日 / 7 日,
  通过 `StockStateConfig.LiquidationWindowDays` 注入, 但默认值需运维
  调优。
- **未处理"退市后现金清算价格计算"**: ForcedLiquidator 只负责
  `EmergencyFlatten` 卖出 (走最后交易日收盘价), 真实的清算所现金
  返还 (摘牌后 90%) 不在 P2-13 范围, 留待 P2-13.1 接入清算所。
- **未处理"摘牌后非交易持仓"**: 客户持仓如果在 `Delisted` 状态
  还未平仓 (例如 LiquidationWindow > 距摘牌日), 引擎不强制平仓,
  而是依赖 LiquidationWindow 提前预警。极端情况 (周末 + 假期)
  可能在 `Delisted` 状态时仍有持仓, 需要 P2-13.2 加"摘牌后扫描"
  兜底。

## Artifacts

### 新增文件

```
pkg/live/stock_state.go         (497 lines, StockState + Record + Registry + Liquidator)
pkg/live/stock_state_test.go    (599 lines, 23 TestXxx)
docs/odr/odr-030-p2-13-delisting.md
docs/TASKS.md                   (P2-13 状态 ⬜ → ✅)
docs/ADR.md                     (ODR Index 加 ODR-030 行)
```

### 净行数

+ 1,096 lines (实现 497 + 测试 599)。其中 ~ 55% 是测试代码, race-clean。

## Metrics

| Metric | 目标 | 实际 |
|--------|------|------|
| 状态数 | 4 (Listed/Suspended/Delisting/Delisted) | 4 |
| 转换合法性 100% 覆盖 | ✓ | ✓ |
| Registry 线程安全 | ✓ (50 goroutine race-clean) | ✓ |
| LiquidationWindow 可注入 | ✓ | ✓ |
| DryRun 模式 | ✓ | ✓ |
| 接 P2-3 EmergencyFlatten | ✓ (BypassedT1 审计标记) | ✓ |
| 单元测试 | ≥ 20 | 23 |
| `go test -race` 通过 | ✓ | ✓ |
| `go vet ./...` 通过 | ✓ | ✓ |
| `go build ./...` 通过 | ✓ | ✓ |

## Lessons Learned

1. **退市是交易生命周期终结, 不是风控事件** — 放 `pkg/live` 不放
   `pkg/risk`, 避免"风控 → 交易 → 风控"循环依赖。
2. **复用 P2-3 EmergencyFlatten 比新建 ForceSell 更好** — 监管
   审计 + BypassedT1 通道一致, 团队只需要学一种"紧急发单"接口。
3. **DryRun 是上线必备** — 退市股强平是不可逆操作, 必须在生产
   DryRun 跑一遍验证 LiquidationResult, 再切真实发单。
4. **5 日 LiquidationWindow 是经验值, 不是法规** — A 股整理期 15
   日, 5 日是给客户的"早反应时间", 不是强制法规。如果监管收紧
   (例如要求整理期首日即强平), 调成 1 即可, 不需要改代码。
5. **Registry + RWMutex + 防御性拷贝** 三件套已经稳定 (P2-4 用了
   同款, P2-7 用了同款), P2-13 第三次复用, 工程上"模式稳定"比
   "每次重新设计"更可维护。

## Follow-up Tasks (留待后续 sprint)

- **P2-13.1 清算所现金返还接入** — 当前只发 EmergencyFlatten, 摘牌
  后 90% 现金清算不在 P2-13 范围。
- **P2-13.2 摘牌后扫描兜底** — 极端情况 (LiquidationWindow > 距摘牌日)
  可能在 Delisted 状态仍有持仓, 需要加"摘牌后强制平仓"兜底。
- **P2-13.3 数据源接入** — StockState 实际数据需要从交易所 / 券商
  股东通知推送, 当前由调用方 Set 注入。
- **P2-13.4 短信 / 客户端推送集成** — 监管要求券商在整理期内主动
  告知客户, 当前没有对接券商短信网关。
- **P2-13.5 与 P2-8 券资金对账协同** — 接入对账 Worker, 已摘牌股
  自动从对账范围剔除。
