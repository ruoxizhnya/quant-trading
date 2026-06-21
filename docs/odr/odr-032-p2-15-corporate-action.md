# ODR-032: P2-15 公司行为 (分红 / 送股 / 拆股 / 配股 / 增发)

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation (Domain / Position Lifecycle)
> **Related ADRs**: ADR-020 (Engine God Object 拆分 + Strategy 接口 ISP)
> **Related ODRs**: ODR-030 (P2-13 退市), ODR-031 (P2-14 止盈)
> **Supersedes**: —
> **Relates to**: TASKS §P2-15, BR-014 (持仓生命周期)

## Context

A 股 5 类公司行为 (Corporate Action) 在除权除息日 (ex-date) 实际
生效, 持仓数量 / 现金 / 成本均价 / 股价参考价都会相应调整, 漏掉
任何一种都会导致账面与实际严重不符:

- **现金分红** (派息): 持仓数量不变, 现金增加 `qty * per_share`, ex-date
  开盘参考价 = 收盘价 - `per_share`。
- **送股** (10 送 5): 持仓数量 × `(1 + 0.5)`, 成本均价 ÷ 1.5, ex-date
  开盘参考价 ÷ 1.5。
- **拆股** (1 → 2): 持仓数量 × 2, 成本均价 ÷ 2, ex-date 开盘参考价 ÷ 2。
- **配股** (10 配 3 @ 5 元): 持仓股东在 pay_date 之前**可缴款** (call
  券商), 缴款后持仓数量 +30%, 现金减少对应金额; 逾期未缴款视为放弃,
  持股数不变。
- **增发**: 不直接增加原股东持仓, 但 ex-date 开盘参考价调整 (市场
  稀释预期)。

P2-15 解决这个 gap: 提供 `CorporateAction` 接口 + 5 种实现 +
`ActionEngine` 多 action 调度器, 在 `pkg/domain` 包内统一处理 (与
`Position` 类型同包, 因为 Apply 直接 mutate Position 字段)。

## Decision

### 1. 包结构: `pkg/domain/corporate_action.go` (单文件, 5 个 Action + 1 个 Engine)

```
pkg/domain/
├── position.go                 (既有) — Position 类型
├── signal.go                   (既有) — Signal 类型
├── ohlcv.go                    (既有) — OHLCV 类型
├── corporate_action.go         (P2-15) — 5 Action + Engine [本 ODR]
├── corporate_action_test.go    (22 TestXxx)
└── ...
```

**为什么放 `pkg/domain` 不放 `pkg/live`**: Apply 直接修改
`Position` 字段 (Quantity / AvgCost / MarketValue), 是**纯函数 + 持仓
生命周期事件**, 不是交易执行事件。`pkg/domain` 是 leaf package,
没有循环依赖风险, 适合放"领域规则"。

### 2. `CorporateAction` 接口 — 纯函数语义

```go
type CorporateAction interface {
    Type() ActionType
    Symbol() string
    ExDate() time.Time
    RecordDate() time.Time
    PayDate() time.Time
    Apply(pos Position, prevClose float64) (Position, float64)
    Description() string
}
```

**设计原则**:
- `Apply` 是**纯函数**: 给定 `(pos, prevClose)`, 返回 `(newPos, cashDelta)`。
  不修改入参, 调用方拿到新 pos 自行决定是否替换。
- Cash 增量 (正=收到现金, 负=支付现金) 由调用方累加。
- 每个 action 暴露 `Description()` 给审计日志 / UI 提示。
- 字段命名 `SymbolValue / ExDateValue` 等 (避免与接口方法名冲突)。

### 3. 5 种 Action 实现 — 公式与 A 股法规对齐

#### 3.1 `CashDividend` — 现金分红

```go
type CashDividend struct {
    SymbolValue  string
    ExDateValue  time.Time
    RecordValue  time.Time
    PayValue     time.Time
    CashPerShare float64
}
```

**公式**: `cash = qty * cashPerShare` (税前, 含税场景下应预扣 20%
个人所得税, **暂不在引擎内处理**, 留待 tax 模块)。持仓本身不变
(ex-date 后股价调整由 data-feed / broker 同步)。

#### 3.2 `BonusShare` — 送股

```go
type BonusShare struct {
    SymbolValue string
    ExDateValue time.Time
    RecordValue time.Time
    PayValue    time.Time
    BonusPer10  float64
}
```

**公式**: `ratio = 1 + BonusPer10 / 10`, 持仓 `Qty *= ratio`,
`AvgCost /= ratio`。同步更新 `MarketValue` / `UnrealizedPnL` (按
`CurrentPrice` 重算)。

**例**: 持仓 1000 股 @ 10 元 → 10 送 5 → 1500 股 @ 6.67 元 (10 / 1.5)。

#### 3.3 `CorporateActionSplit` — 拆股 / 并股

```go
type CorporateActionSplit struct {
    SymbolValue string
    ExDateValue time.Time
    RecordValue time.Time
    PayValue    time.Time
    SplitRatio  float64
}
```

**公式**: 拆股 (`SplitRatio > 1`, e.g. 2 = 1→2), 持仓 `Qty *= ratio`,
`AvgCost /= ratio`。并股 (`SplitRatio < 1`, e.g. 0.5 = 2→1) 反向。

**命名约定**: `CorporateActionSplit` 而非 `Split`, 避免与
`pkg/domain/types.go` 里的 `Split` (Tushare 风格) 冲突。

#### 3.4 `RightsIssue` — 配股

```go
type RightsIssue struct {
    SymbolValue         string
    ExDateValue         time.Time
    RecordValue         time.Time
    PayValue            time.Time
    RightsPer10         float64
    RightsPricePerShare float64
}
```

**两阶段 apply**:
- `Apply(pos)`: 默认按"未缴款"处理, 持仓不变 (放弃配股)。
- `ApplyPaid(pos)`: 已缴款, 持仓增加 `qty * rights_per_10 / 10`,
  cash 减少 `newShares * rights_price_per_share`, 新均价 =
  `(原成本 + 新股成本) / 新股数`。

**为什么 Apply 默认放弃**: A 股配股有"缴款截止日" (通常 ex-date 后
5 个交易日), 券商客户端应在 pay_date 之前弹窗提示, 由用户决定
是否缴款; 缴款时再调用 ApplyPaid, 引擎不能在用户未决定时就
自动扣款。

#### 3.5 `Placement` — 增发

```go
type Placement struct {
    SymbolValue   string
    ExDateValue   time.Time
    RecordValue   time.Time
    PayValue      time.Time
    NewShares     float64
    PricePerShare float64
}
```

**公式**: 持仓不变, cash 不变。UI 端应基于 `NewShares` 计算
"持股比例稀释 = qty / (qty + NewShares)" 提示。

### 4. `ActionEngine` — 多 action 调度

```go
type ActionEngine struct {
    appliedLog map[string]bool // key = "symbol:ex_date:action_type"
}

func (e *ActionEngine) ApplyAll(
    asOf time.Time,
    positions []Position,
    prevCloses map[string]float64,
    actions []CorporateAction,
) (newPositions []Position, totalCashDelta float64, outcomes []ApplyOutcome)
```

**算法**:
1. **排序**: 按 `ex_date asc`, 同日按 A 股标准 apply 顺序:
   `Split (0) → BonusShare (1) → RightsIssue (2) → CashDividend (3) → Placement (4)`。
   顺序固定, 因为 "split + cash div on same ex-date" 必须先 split 再
   cash (否则 cash 会按旧 qty 计算)。
2. **遍历**: 对每个 action, 检查:
   - 该 symbol 是否有持仓? 没有 → 标记 applied + skip reason
     "no_position"。
   - `ex_date <= asOf`? 否 → skip reason "ex_date_in_future"。
   - 已 applied? 是 → skip reason "already_applied"。
3. **Apply**: 调用 `act.Apply(newPositions[idx], prevClose)`, 累计
   `totalCashDelta`, 替换 `newPositions[idx]`。
4. **MarkApplied**: 写入 `appliedLog` (key = `symbol:ex_date:type`),
   防止未来 `ApplyAll` 重复触发。

**为什么引擎无状态**: 状态由调用方持有 (生产场景是数据库 / Redis
持久化), 引擎只负责"在 asOf 时点, 哪些 action 应该被 apply"。
测试用 `MarkApplied` / `IsApplied` 验证去重。

### 5. 与 P2-13 / P2-14 协同

- **P2-13 退市 (ODR-030)**: 退市股的 CorporateAction (如退市前
  派息) 仍应正常 apply, 但 apply 后该股进入 `Delisting` 状态,
  TakeProfitChecker / ForcedLiquidator 接管后续处理。
- **P2-14 止盈 (ODR-031)**: 止盈规则触发后, 持仓 Quantity 减少,
  后续的 CashDividend / BonusShare apply 时按新 qty 计算。Apply
  纯函数保证不会影响"卖出"决策。

### 6. 测试覆盖 (22 TestXxx)

- 基础: 5 种 action 各自的 Apply 公式验证
- 边界: `Quantity <= 0` / `CashPerShare <= 0` / `BonusPer10 = 0`
- 排序: ActionEngine.ApplyAll 同 ex_date 多 action 顺序 (Split +
  CashDividend 测试)
- 去重: 同一 action 多次 ApplyAll 只触发一次
- 多 symbol: 不同 symbol 的 action 独立处理
- 未到 ex-date: future action skip
- 配股: Apply 默认放弃, ApplyPaid 后数量 + 现金变化
- 拆股 vs 并股: ratio > 1 vs < 1 双向测试
- 集成: 5 种 action 串联 (先 Split, 再 BonusShare, 再 CashDividend)
  验证 avg_cost 累计调整正确

## Consequences

### Positive

- **A 股 5 类公司行为 100% 覆盖**: 现金分红 / 送股 / 拆股 / 配股 /
  增发, 全部有 Apply 实现 + 公式 + 测试, 没有"if actionType == ..."
  散落分支。
- **Apply 纯函数语义**: 回测 / 实盘 / 模拟 三种环境下行为一致, 不
  会出现"实盘按 1000 股派息, 回测按 500 股派息"的不一致。
- **apply 顺序固定 (Split → Bonus → Rights → Cash → Placement)** 与
  A 股实务一致, "split + cash div on same ex-date" 测试
  `TestScenario_StockSplitThenCashDividend` 验证了顺序正确性
  (split 先于 cash, cash 按新 qty 计算)。
- **`appliedLog` 幂等**: `MarkApplied` / `IsApplied` 防止 `ApplyAll`
  重复触发, 测试用 `engine.IsApplied` 验证。
- **配股两阶段 Apply** (Apply 默认放弃 + ApplyPaid 显式缴款) 与 A 股
  配股实务对齐: 券商客户端应在 pay_date 之前弹窗提示, 由用户决定
  是否缴款; 引擎不替用户决策。
- **审计可重现**: 每次 apply 的 `ApplyResult` 含 `Action / OldPos /
  NewPos / CashDelta / Applied / SkipReason`, 直接进 audit_logs。

### Negative

- **未处理现金分红含税场景**: 实际派息应预扣 20% 个人所得税
  (沪深规则略有差异), 当前 Apply 输出税前现金, 留待 P2-15.1 加
  tax 模块。
- **未处理除权除息日参考价**: ex-date 开盘参考价由 data-feed / broker
  同步, 引擎不主动计算 (只接受 prevClose 入参), 留待 P2-15.2 加
  ExRefPrice 工具方法 (RightsIssue 已有, 其他 4 类未加)。
- **未处理"先送股后配股"同日复合**: A 股实务允许"先 10 送 5, 再 10
  配 3 @ 5 元" 同日触发。当前 Apply 顺序 BonusShare(1) → RightsIssue(2)
  是 A 股标准顺序, 但 ApplyPaid 后 qty 变化会传给 CashDividend
  (顺序 3), 验证过没问题; 真实场景需更多测试覆盖。
- **未对接交易所 / 券商的 action 公告推送**: 当前 action 由调用方
  `ApplyAll` 注入 (与 P2-7 减持 / P2-13 退市同款"无状态 + 调用方带
  数据"设计), 生产需要数据源对接。
- **拆股 ratio 极端值未防御**: `SplitRatio = 0.001` (1→1000 拆) 是
  合法但极端, 引擎不挡。如果券商数据源有误传 ratio=0 (除零), 当前
  Apply 会 panic-free 但 `newPos.AvgCost = Inf`, UI 显示 NaN — 需要
  P2-15.3 加 ratio 范围检查。

## Artifacts

### 新增文件

```
pkg/domain/corporate_action.go         (587 lines, 5 Action + 1 Engine)
pkg/domain/corporate_action_test.go    (367 lines, 22 TestXxx)
docs/odr/odr-032-p2-15-corporate-action.md
docs/TASKS.md                          (P2-15 状态 ⬜ → ✅)
docs/ADR.md                            (ODR Index 加 ODR-032 行)
```

### 净行数

+ 954 lines (实现 587 + 测试 367)。其中 ~ 38% 是测试代码, race-clean。

## Metrics

| Metric | 目标 | 实际 |
|--------|------|------|
| 5 类公司行为 100% 覆盖 | ✓ | ✓ (cash_div / bonus / split / rights / placement) |
| Apply 纯函数语义 | ✓ | ✓ (不修改入参) |
| apply 顺序固定 (Split 先于 Cash) | ✓ | ✓ |
| `appliedLog` 幂等去重 | ✓ | ✓ |
| 配股两阶段 (Apply 默认放弃 + ApplyPaid) | ✓ | ✓ |
| 单元测试 | ≥ 20 | 22 |
| `go test -race` 通过 | ✓ | ✓ |
| `go vet ./...` 通过 | ✓ | ✓ |
| `go build ./...` 通过 | ✓ | ✓ |
| ExRefPrice 工具方法 (参考价) | 部分 | ✓ (RightsIssue), 4 类未加 (P2-15.2) |

## Lessons Learned

1. **Apply 必须是纯函数** — 同一个 action 在回测 / 实盘 / 模拟三
   种环境下的输出必须完全一致, 这是回测可信度的根基。修改入参会
   引入"调用一次 vs 调用多次"的行为差异, 是最难调试的 bug 类型。
2. **apply 顺序固定** (Split → Bonus → Rights → Cash → Placement) 是
   A 股实务的硬约束, 不能让调用方传 order。`actionApplyOrder()`
   函数集中管理, 单元测试 `TestScenario_StockSplitThenCashDividend`
   验证了顺序错误时的成本均价偏差 (按旧 qty 派息 → 现金多了 50%)。
3. **配股"两阶段 Apply" (默认放弃 + ApplyPaid) 优于"自动决策"** —
   配股缴款是用户决策, 引擎不应该替用户扣款。`ApplyPaid` 显式
   调用, 符合 A 股"用户主动行为" 流程。
4. **ex-date 开盘参考价由 data-feed 同步而非引擎计算** — 引擎
   只接受 `prevClose` 入参, 不主动调整当前价。这样引擎逻辑保持
   纯函数, 真实股价由 broker / 行情源同步, 责任清晰。
5. **`appliedLog` 幂等去重避免重复派息** — 这是 P2-15 最重要的
   不变量: 即使 `ApplyAll` 被多次调用 (例如每日定时 + 用户手动),
   同一 action 只 apply 一次, 不会重复给客户发钱。

## Follow-up Tasks (留待后续 sprint)

- **P2-15.1 现金分红含税预扣** — 沪深规则略有差异 (A 股 20% 个人
  所得税, 港股通 20% / 28% 分段), 需要 tax 模块支持。
- **P2-15.2 ExRefPrice 工具方法** — 当前仅 RightsIssue 有, 其他 4 类
  (CashDividend / BonusShare / Split / Placement) 应加同款方法
  给 UI 提示"如果按 ex-date 参考价, 你的持仓市值会是多少"。
- **P2-15.3 SplitRatio 范围防御** — `SplitRatio <= 0` 或极端值
  (e.g. 0.001) 应在构造时 panic 或返回错误, 防止 data-feed 误传。
- **P2-15.4 数据源接入** — 当前 action 由调用方注入, 生产需要从
  交易所 / 券商的 action 公告推送对接。
- **P2-15.5 Position 持久化层补齐** — apply 后 Position 需要持久化
  (数量 / 均价 / 现金), 当前是 in-memory, 接 DB 留待 P2-15.5。
- **P2-15.6 UI 推送** — ex-date 当日给客户推送"您的持仓 X 已
  派息 Y 元 / 送股 Z 股" 通知, 与 P2-13 / P2-14 同通道。
