# ODR-031: P2-14 止盈 / 移动止盈 / 分批止盈

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation (Risk / Live Trading)
> **Related ADRs**: ADR-019 (服务合并 in-process 注入), ADR-020 (Strategy 接口 ISP §6)
> **Related ODRs**: ODR-026 (P2-3 紧急平仓), ODR-030 (P2-13 退市), ODR-029 (P2-7 减持)
> **Supersedes**: —
> **Relates to**: TASKS §P2-14, BR-008 (投资者适当性 + 风控工具)

## Context

中国证券业协会《证券公司投资者适当性管理指引》(2023) §3.4 明确要求
券商客户端提供 "**目标止盈** / **移动止盈** / **分批止盈**" 三类止盈
工具, 触发后必须留痕 (含触发时间 / 触发价 / 实际成交价) 到审计日志。
监管对止盈工具的合规要求与止损一致: 不允许前端篡改触发阈值, 触发后
必须以 "策略 + 阈值 + 时间" 三元组记录。

业务侧的需求:
- **短线超跌反弹 / 突破追涨** → 固定止盈 (`+10%` / `+20%` 一刀切),
  简单粗暴。
- **趋势跟随, 不想错过大波段** → 移动止盈 (激活后从最高点回撤
  `8%` 触发), 让利润奔跑。
- **长线持股 + 阶段性止盈** → 分批止盈 (`+10%/+20%/+30%` 各卖
  `1/3`), 锁定部分利润, 留底仓博更大波段。

P2-14 解决这个 gap: 提供 `TakeProfitRule` 接口 + 3 种实现 +
`TakeProfitChecker` 注册表, 接入 ODR-021 的 in-process 注入, 在
`pkg/risk` 包内统一处理 (与 P2-3 紧急平仓 / P2-4 适当性 / P2-7 减持
同包)。

## Decision

### 1. 包结构: `pkg/risk/take_profit.go` (单文件, 3 个 Rule + 1 个 Checker)

```
pkg/risk/
├── stoploss.go              (既有) — 止损
├── take_profit.go           (P2-14) — 止盈 [本 ODR]
├── take_profit_test.go      (29 TestXxx)
├── appropriateness.go       (既有) — 适当性
├── divestment.go            (P2-7)
├── ...
```

**为什么放 `pkg/risk` 不放 `pkg/strategy`**: 止盈是**风控工具**, 不
是 alpha 策略。P2-3 紧急平仓已经在 `pkg/risk` (`kill_switch.go`),
沿用同包, 风控工具集中。

### 2. `TakeProfitRule` 接口 — 单一事实来源

```go
type TakeProfitRule interface {
    Name() string
    Evaluate(pos domain.Position, currentPrice float64) (TakeProfitAction, bool)
}
```

**设计原则**:
- 规则应该是**无状态**的, 状态从 `Position.Metadata` 读取 (trailing
  的 high watermark / tiered 的 last triggered index)。
- 不修改 `pos` (传入是值拷贝, Metadata 字段是 map 引用, 修改可见)。
- 不下单, 仅返回 `TakeProfitAction`, 调用方 (LiveEngine / BacktestEngine)
  转换为 Order 提交。

返回的 `TakeProfitAction` 含 9 字段: `Symbol / Rule / Level / TriggerPrice /
SellQuantity / SellFraction / Reason / TriggeredAt / Source` (P2-14 后
扩), 序列化后直接进审计日志, 监管回溯可重现。

### 3. 三种 Rule 实现

#### 3.1 `FixedTakeProfit` — 固定阈值

```go
type FixedTakeProfit struct {
    ProfitPct float64 // e.g. 0.20 = +20%
}
```

**算法**: `currentPrice >= pos.AvgCost * (1 + ProfitPct)` → 全部卖出。

**适用**: 短线超跌反弹 / 突破追涨, `+10%` / `+20%` 一刀切。

#### 3.2 `TrailingTakeProfit` — 移动止盈

```go
type TrailingTakeProfit struct {
    ActivationPct float64 // e.g. 0.05 = +5% 后激活
    TrailPct      float64 // e.g. 0.08 = -8% 触发
}
```

**两阶段算法**:
1. **未激活**: `currentPrice < entry * (1 + ActivationPct)` → 不触发,
   不更新 high (因为尚未开始跟踪)。
2. **已激活**: 持续更新 `pos.Metadata["trailing_high"]` (从最高点
   取 max)。`currentPrice <= high * (1 - TrailPct)` → 触发全部卖出。

**状态存储**: `pos.Metadata["trailing_activated"]` (bool) +
`pos.Metadata["trailing_high"]` (float64)。回测时 Metadata 默认空,
视为未激活。

**关键边界** (`trailing_high` 默认 0): 如果激活时 `currentPrice` 略
低于 `activationTrigger` 的 `1` 个 tick, 需要等下一根 K 线才更新
high。单元测试 `TestTrailingTakeProfit_ActivateThenTrigger` 验证了
"激活 → 创新高 → 回撤 8% → 触发" 的完整时序。

#### 3.3 `TieredTakeProfit` — 分批止盈

```go
type TakeProfitTier struct {
    SellFraction float64 // 0~1
    ProfitPct    float64 // e.g. 0.10 = +10%
}

type TieredTakeProfit struct {
    Tiers []TakeProfitTier // 按 ProfitPct 升序
}
```

**算法**: 找到 `pos.Metadata["tiered_last_triggered"]` 之后第一个
未触发的 tier。`currentPrice >= entry * (1 + tier.ProfitPct)` → 触发
`SellFraction * pos.Quantity` (取整到 100 股, 1 手), 更新
`last_triggered = tier_index`。

**为什么取整到 100 股**: A 股最小申报单位是 100 股, 1 股精度的分批
止盈是无意义的精细化, 浪费 CPU + 增加 diff 噪音。

**关键边界** (`last_triggered` 默认 -1): 第一次触发 tier 0 后, 回到
tier 0 价位**不会重复触发**。`TestTieredTakeProfit_SecondLevelTriggers`
验证。

### 4. `TakeProfitChecker` — 规则注册表 + 评估

```go
type TakeProfitChecker struct {
    mu     sync.RWMutex
    rules  map[string]TakeProfitRule // symbol → rule
    logger zerolog.Logger
}

func (c *TakeProfitChecker) Register(symbol string, rule TakeProfitRule)
func (c *TakeProfitChecker) Evaluate(symbol string, pos domain.Position, currentPrice float64) (TakeProfitAction, bool)
func (c *TakeProfitChecker) EvaluateAll(positions []domain.Position, prices map[string]float64) []TakeProfitAction
```

**EvaluateAll**: 给定 (positions, prices), 输出**所有触发**的 actions
(可能有多个 symbol 同时触发), LiveEngine 一并转换为订单。

**Registry 模式** (P2-4 / P2-7 / P2-13 同款):
- `DefaultTakeProfitRules()` 返回空 map
- `Register` / `Unregister` 注入 + 移除
- `Rules()` 返回深拷贝 (调用方改不动内部状态)
- 测试用 `t.Cleanup(reset)` 隔离, race-clean

### 5. 与 P2-3 / P2-7 / P2-13 协同

- **P2-3 紧急平仓 (ODR-026)**: 止盈是"主动止盈", 紧急平仓是"被动
  风控"。两者走不同 action 通道: 止盈 → `LiveTrader.Submit` (普通
  订单), 紧急平仓 → `LiveTrader.EmergencyFlatten` (BypassedT1)。
- **P2-7 减持 (ODR-029)**: 5%/25% 卖出上限是**监管约束**, 止盈
  是**策略触发**。如果某只持仓触发止盈但超 5% 减持上限, P2-7 引擎
  会截短到剩余容量, 止盈 action 的 `ApprovedQty < SellQuantity`。
  P2-7.4 接入前端时合并 dialog 提示。
- **P2-13 退市 (ODR-030)**: 退市股进入 Delisting 状态, 止盈规则
  不应触发 (因为即将被强制清仓)。TakeProfitChecker 在评估前
  检查 StockStateRegistry, 跳过 `State == Delisting/Delisted`
  的持仓。

### 6. 测试覆盖 (29 TestXxx)

- 基础: 三种 rule 各自的触发条件 / 不触发条件
- 边界: `Quantity <= 0` / `AvgCost <= 0` / 空 Tiers / 全 tier 已触发
- 状态: Trailing 激活时序 / Tiered last_triggered 持久化
- 取整: Tiered SellQuantity 100 股取整
- 集成: TakeProfitChecker.Register + Evaluate + EvaluateAll
- 并发: 50 goroutine × 100 次 Register / Evaluate, race-clean
- 协同: 5%/25% 减持截短 (`TestChecker_DivestmentClamp`)
- 协同: Delisting 状态跳过 (`TestChecker_SkipDelistedStock`)

## Consequences

### Positive

- **A 股监管三类止盈工具全实现**: 固定 / 移动 / 分批 100% 覆盖
  中证协 2023 §3.4 要求。
- **无状态 Rule + Metadata 持久化**: 同一持仓在回测 / 实盘 / 模拟
  三种环境下行为一致 (状态都从 Metadata 读取), 不会出现"实盘
  触发, 回测不触发"的不一致。
- **Registry 模式** (P2-4 / P2-7 / P2-13 同款) — 团队学习成本零,
  50 goroutine race-clean。
- **与 P2-3 / P2-7 / P2-13 协同** — 止盈不会与紧急平仓 / 减持
  上限 / 退市强平冲突, 规则在 EvaluateAll 入口先过滤 Delisting
  状态, 然后 P2-7 截短超限部分。
- **审计留痕**: 每次触发的 action 含 9 字段 (含 Rule / Level /
  TriggerPrice / SellQuantity / Reason / TriggeredAt), 直接进
  audit_logs, 监管回溯不需要查额外文档。

### Negative

- **没有"止盈撤销"机制**: 一旦触发, action 立即生成订单。如果客户
  在订单提交后 1ms 反悔, 撤单要走普通撤单通道 (T+1 锁定), 与
  止盈策略无关。监管上也没有要求券商提供"止盈撤销"功能。
- **Metadata 持久化依赖 Position 元数据 schema**: 当前
  `pos.Metadata` 是 `map[string]any`, JSON 序列化后类型是
  `float64` (Go JSON 数字默认), 所以 `last_triggered` 必须存为
  float64 才能 round-trip。如果未来 Position.Metadata 升级到
  typed schema (ADR-020 后续优化项), 需要迁移。
- **TrailingTakeProfit 高水位 0 处理**: 首次激活时 `high` 是 0
  (Metadata 空), 激活后第一根 K 线才会更新 high。如果客户在
  激活后立即用市价单卖出 (不在 TakeProfitChecker 评估周期内),
  trailing 不会被触发, 这是符合预期的 (因为已经卖了)。
- **没有"分批止盈 + 移动止盈"组合**: 当前每持仓只能挂一种 rule。
  实盘可能有"分批 30% + 移动 70%"的需求, P2-14.1 加 Multi-Rule
  支持。
- **前端 UI 还没接**: TakeProfitChecker 输出 actions 还没有
  PaperTrading.vue 可视化配置入口 (类似止损 UI), 留待 P2-14.1。

## Artifacts

### 新增文件

```
pkg/risk/take_profit.go         (458 lines, 3 Rule + 1 Checker + 1 Action)
pkg/risk/take_profit_test.go    (433 lines, 29 TestXxx)
docs/odr/odr-031-p2-14-take-profit.md
docs/TASKS.md                   (P2-14 状态 ⬜ → ✅)
docs/ADR.md                     (ODR Index 加 ODR-031 行)
```

### 净行数

+ 891 lines (实现 458 + 测试 433)。其中 ~ 49% 是测试代码, race-clean。

## Metrics

| Metric | 目标 | 实际 |
|--------|------|------|
| 三类止盈工具 100% 覆盖 | ✓ | ✓ (fixed / trailing / tiered) |
| Rule 接口 ISP 化 | ✓ | ✓ (Name + Evaluate) |
| 无状态 Rule + Metadata 持久化 | ✓ | ✓ |
| Registry 模式 (P2-4/7/13 同款) | ✓ | ✓ |
| 与 P2-3 / P2-7 / P2-13 协同 | ✓ | ✓ (3 协同测试) |
| 单元测试 | ≥ 25 | 29 |
| `go test -race` 通过 | ✓ | ✓ |
| `go vet ./...` 通过 | ✓ | ✓ |
| `go build ./...` 通过 | ✓ | ✓ |
| 100 股取整 (A 股最小申报单位) | ✓ | ✓ (tiered SellQuantity) |

## Lessons Learned

1. **规则无状态 + Metadata 持久化** 是回测 / 实盘一致性的关键 — 同
   一持仓在两种环境下行为完全相同, 不会出现"实盘触发, 回测不触发"
   的诡异 bug。
2. **Registry + RWMutex + 防御性拷贝** 三件套已经稳定 (P2-4 用了
   同款, P2-7 用了同款, P2-13 用了同款), P2-14 第四次复用, 工程
   上"模式稳定"是生产代码可维护的根基。
3. **A 股 100 股取整** 不是法规明文, 但是行业事实 (1 手 = 100 股),
   浪费 1 股精度的分批止盈是无意义的精细化, 还会增加 diff 噪音。
4. **协同设计优先于模块独立** — 止盈 / 减持 / 退市 / 紧急平仓
   四个看似独立的风控工具, 实盘必须协同 (退市股跳过止盈, 减持
   截短超限部分), 在 EvaluateAll 入口先过滤 + 后处理, 比每个
   模块独立评估更符合监管要求。
5. **Metadata 用 float64 存整数** 是 Go JSON 的 hard constraint
   (数字默认 float64), 团队需要约定: 整数也存为 float64, 反序列化
   时 `int(floatVal)` 取整。这条经验已经在 P2-7.4 的 Director
   25% 检查中验证过。

## Follow-up Tasks (留待后续 sprint)

- **P2-14.1 Multi-Rule 支持** — 当前每持仓只能挂一种 rule, 加
  Multi-Rule 后可支持"分批 30% + 移动 70%"等组合。
- **P2-14.2 前端 UI 配置入口** — PaperTrading.vue 加止盈配置
  panel, 类似止损 UI。
- **P2-14.3 止盈撤销机制** — 客户反悔时撤单走普通撤单通道
  (T+1 锁定), 与止盈策略解耦。
- **P2-14.4 Position.Metadata typed schema** — 当前 `map[string]any`
  升级到 typed schema (key 集合枚举), 编译期防止 typo。
- **P2-14.5 短信 / 客户端推送** — 触发后给客户推送通知 (与
  P2-13 退市推送同通道)。
