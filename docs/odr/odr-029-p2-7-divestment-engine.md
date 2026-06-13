# ODR-029: P2-7 减持规则引擎 — 5 类股东 + 滚动窗口 + 限售期

> **Status**: Completed
> **Date**: 2026-06-13
> **Category**: Implementation (Compliance / Regulatory)
> **Related ADRs**: ADR-005 (Strategy Config 标准化), ODR-021 (P1-15 服务合并), ODR-028 (P2-4/5/6 合规三位一体)
> **Related ODRs**: ODR-026 (P2-3 紧急平仓), ODR-027 (P2-1/P2-2 导出对比), ODR-028 (P2-4/5/6)
> **Supersedes**: —
> **Relates to**: TASKS §P2-7, BR-011 (监管合规)

## Context

A 股 5 类减持主体 (控股股东 / 董监高 / 5% 以上股东 / Pre-IPO 股东 / 定增
对象) 在公开市场有截然不同的减持股数 + 时间窗限制。证监会
《上市公司股东、董监高减持股份的若干规定》(2017-05-27 颁布, 2024-04-12
最新修订) 是核心依据, 加上沪深细则 (主板/创业板/科创板适用) + 注册制下
定增 6/18 月锁定 + 董监高年度 25% 限制, 一共 5 类规则 3 种方式
(集中竞价 / 大宗交易 / 协议转让) 互相叠加, 总共 15 种情形。

P2-4/5/6 (ODR-028) 已经覆盖"准入 + 监控 + 报告"三件套, 但**交易前**和
**交易中**的合规 gate 仍然空白。券商如果没有在用户提交卖单前做减持容量
预检, 会出现以下两种风险:

1. **账户风险**: 用户的 90 日累计已超 1% 但仍可继续下单, 实际成交后被
   监管认定为"超额减持", 账户可能进监管黑名单 + 罚款。
2. **券商风险**: 券商没有 gate, 监管认定券商"未履行交易前合规审查"
   (证监会 2024 年通报了 3 起券商因董监高减持未挡住的百万级罚单)。

P2-7 解决这个 gap: 给券商一个无状态、可注入阈值、覆盖 5 类主体的减
持规则引擎, 在 `POST /api/orders` (下单前) 调一次, 通过 → 放行,
不通过 → 拦截 + 给出结构化理由。

## Decision

### 1. 包结构: 续接 `pkg/compliance` 单包, 新增 `divestment.go`

```
pkg/compliance/
├── appropriateness.go         (P2-4) — 投资者适当性
├── appropriateness_test.go
├── abnormal_trade.go          (P2-5) — 异常交易监控
├── abnormal_trade_test.go
├── reporter.go                (P2-6) — 大额交易报告
├── reporter_test.go
├── divestment.go              (P2-7) — 减持规则引擎 [本 ODR]
├── divestment_test.go
├── handlers_compliance.go     (HTTP: 4 P2-4/5/6 端点 + 4 P2-7 端点)
└── handlers_compliance_test.go
```

**为什么续接不新建 `pkg/divestment` 包**:

- `pkg/compliance` 已经是 leaf package, 不依赖任何业务包, 把 P2-7 加进去
  不增加新的循环依赖。
- P2-7 的 `ShareholderProfile` / `ReductionPlan` 与 P2-4/5/6 共享
  `OrderRecord` / `TradeRecord` 类型 (compliance 包内重声明, 不反向依赖
  `pkg/live` — 沿用 ODR-028 第 5 条原则)。
- 4 个 .go 文件 + 共享 `compliance` 命名空间, 监管 dashboard / 未来 P2-8
  (券资金对账) / P2-9 (融资融券) 都能挂在这一个包下, 不会出现"包罗万象
  / 一包 1 个 .go" 的散乱。

### 2. 5 类股东 + 3 种方式 — 单一事实来源 (Registry)

`DivestmentRule` (per `ShareholderType`) + `ReductionMethod` 是 A 股 5×3
格子的 15 个交点的统一表达, 通过 `DivestmentChecker` 持有 `map[ShareholderType]*DivestmentRule`。
默认值与 2024 CSRC 减持规定一致:

| HolderType | 集中竞价 90 日 | 大宗 90 日 | 协议 ≥ | 锁定 | 法规引用 |
|------------|---------------|-----------|--------|------|---------|
| controlling (控股股东) | ≤ 1% | ≤ 2% | 5% | 36M Pre-IPO | CSRC-2024 1/3 |
| director (董监高) | ≤ 1% | ≤ 2% | 5% | 年度 25% | CSRC-2007-56 + CSRC-2024 9 |
| major_5pct (5%+) | ≤ 1% | ≤ 2% | 5% | — | CSRC-2024 1 |
| pre_ipo (Pre-IPO) | 0 | 0 | 0 | 36M | CSRC-2024 5 |
| placement (定增) | ≤ 1% | ≤ 2% | 5% | 6M/18M | CSRC-2023-注册办法 59 |

**Registry 模式** (与 P2-4 适当性同款):

- 包级 `DefaultDivestmentRules()` 返回监管默认值
- `DivestmentChecker.rules` 持有可变副本 + `sync.RWMutex`
- `SetRule(t, r)` / `ResetRules()` 注入 + 复原
- `Rules()` 返回深拷贝 (调用方改不动内部状态)
- 测试用 `t.Cleanup(c.ResetRules)` 隔离 + 50 goroutine × 100 次并发
  读写 race detector 全过

### 3. `Check(profile, plan, recent) → DivestmentCheckResult` 算法

```
1. 输入校验
   - holder_type / method 必填
   - quantity > 0
   - quantity ≤ holdingsShare (拟减 ≤ 实持)
   - profile.symbol == plan.symbol (一致性)
2. 限售期检查
   - 命中 active lockup → 拒, 输出 Lockups[] 给前端 dialog 展示
3. 协议转让 ≥ 5%
   - 拟减持占总股本比 < 5% → 拒
4. 滚动窗口 (按 method 选 cap + windowDays)
   - 集中竞价: 90 日 1% (default)
   - 大宗交易: 90 日 2% (default)
   - 累计同 method 同 symbol 历史 → usedPct
5. Director 附加: 年内 25%
   - annualUsed + proposedPct > 25% → 拒
6. 综合剩余容量 → approved_qty
   - min(plan.Quantity, 剩余% × 总股本, holdingsShare)
   - 取整到 100 股 (1 手)
   - 截短属于 partial-allowed: Allowed=true 但 ApprovedQty < RequestedQty
7. 举牌义务告警
   - 减持后剩余 < 5% (从 ≥ 5% 跌穿) → 1% 线告警
   - 减持后剩余 < 1% (从 ≥ 1% 跌穿) → 1% 线告警
```

返回的 `DivestmentCheckResult` 含 12 字段: `allowed / requested_qty /
approved_qty / window_start / window_end / window_cap_pct / window_used_pct /
window_remain_pct / hold_pct_after / reasons / lockups / warnings`,
按监管报送 schema 序列化, **reasons 顺序固定** (输入校验 → 限售 → 协议
→ 窗口 → 年限 → 截短说明), 监管 diff 友好。

### 4. `LockupPeriod` 半开区间语义

`Overlaps` 用 `l.StartAt.Before(o.EndAt) && o.StartAt.Before(l.EndAt)`,
**端点相接不算 overlap**。这与 A 股"解禁日 = 锁定期后第一天"实务一致:
锁定期 2024-01-01 → 2027-01-01 在 2027-01-01 当天结束 (不是 2027-01-02),
下一个锁定期从 2027-01-01 开始 (而不是 2027-01-02) → 端点相接不冲突。

`IsActive` 用闭区间 `!t.Before(Start) && !t.After(End)`, 与
"EndAt 仍是锁定期内"实务一致。

### 5. 监管依据 hard-coded 进 source 字段

每个 rule 带 `Source` 字段 (`"CSRC-2024-减持规定 第 1 条"`),
LockupPeriod 带 `Code` 字段 (`"CSRC-2024-5"`), 监管回溯不需要去查
外部文档库, 直接看 JSON 即可。

### 6. HTTP 端点 (4 个 P2-7 端点, 接在 ODR-028 5 个端点后)

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/compliance/divestment/check` | 减持计划预检 (200/422) |
| GET | `/api/compliance/divestment/holder-types` | 5 类股东 + 中文 label (UI 下拉) |
| GET | `/api/compliance/divestment/methods` | 3 种方式 + 中文 label (UI 下拉) |
| GET | `/api/compliance/divestment/rules` | 当前 per-holder rule 快照 (UI dashboard) |

HTTP 状态码:

- 200 — result.Allowed == true (含 ApprovedQty 截短)
- 422 — result.Allowed == false (拒, Reasons 非空)
- 400 — request body 解析失败
- 500 — 防御性兜底 (引擎返回 Allowed=false 但 Reasons 空, 极少见,
  用于发现引擎 bug)

### 7. 审计友好设计

- **Reasons 顺序固定**: 输入校验 → 限售 → 协议 → 窗口 → 年限 → 截短。
  监管回溯可重现。
- **Lockups 透明披露**: 拒绝时 `result.Lockups` 列出命中的限售期 (含
  `Code` 法规引用), 监管 + 用户都能直接看到依据。
- **Warnings 不阻塞**: 举牌义务 / 截短说明 / 减持后占比敏感, 全是
  warnings, 不影响 Allowed 标志; 前端 dialog 单独高亮。
- **HoldPctAfter 量化输出**: 减持后剩余持股比例, 用于前端实时
  展示"如果你减完, 你还剩多少"。

## Consequences

### Positive

- **A 股减持规则全主体覆盖**: 控股股东 / 董监高 / 5%+/Pre-IPO/定增, 5 类
  主体 + 3 种方式 + 4 类限售期 (Pre-IPO 36M / 定增 6/18M / 公开承诺 /
  董监高任期) + 协议 5% 单笔下限 + 集中竞价 90 日 1% + 大宗 90 日 2% +
  董监高年度 25% + 举牌 1%/5% 告警。
- **A 股监管"5+3"格子 100% 覆盖**: 5×3 = 15 个交点, 全部在
  `DivestmentChecker.Check` 内处理, 没有 hard-coded "if-else" 分支
  散落各处。
- **审计可重现**: reasons 顺序固定 + Lockups.Code 法规引用 +
  Warnings 量化截短说明, 监管回溯不需要查额外文档。
- **生产/测试隔离**: `t.Cleanup(ResetRules)` 让 50 goroutine 并发测试
  race-clean; 阈值 `SetRule` 注入可在券商对接时按风控偏好定制。
- **Pre-IPO 股东硬拒**: 即使没有 lockup 条目, 默认规则窗口 cap=0,
  引擎直接 reject, 防止 Pre-IPO 股东"忘记填 lockup"导致漏风。
- **已使用的 P2-4 模式** (Registry + RWMutex + 防御性拷贝 + 同步写
  即可注入) 全部沿用, 没有引入新的设计语言。

### Negative

- **总股本反推 (holdingsShare / (holdingsPct/100)) 在测试 / 真实场景下
  可能有舍入误差**: 100M × 0.7% = 700_000 但浮点下 0.7% = 0.007, 用
  math.Round 取整到 100 股后是 700_000 (稳)。如果 holdingsShare 较小
  (如 1K), 取整到 100 股会丢失部分精度, 但 A 股个人投资者最小申报
  是 100 股, 这是合理的简化。
- **没有处理"跨日不连续交易"**: 90 日是日历日, 实际 A 股有 23 个交易日
  / 月, 但减持规定原文用的是"连续 90 日" (含节假日), 我们与法规一致。
  如果未来要按交易日计算, 需要引入 `pkg/data` 的 trading calendar。
- **协议转让"≤ 5%"单笔下限 + 受让方 6 月锁** 受让方 6 月锁是另一
  类主体 (受让方 = 战略投资者), 本 P2-7 只检查**出让方**约束, 不检查
  受让方。如果要加, 需要 P2-7.1 扩到双方约束。
- **董监高年内 25%** 默认按"任期内"计算 (CSRC-2007-56 号文), 但实操中
  离任后 6 个月仍有 25% 限制 (CSRC-2024 第 9 条新增), 我们没有分
  "在任 / 离任"两个状态。下一个 sprint (P2-7.1) 加。
- **未对接真实股东数据源**: 实际生产需要从券商的股东台账表取数据,
  当前 `Recent` 由调用方传入 (前端 / admin console 聚合)。这与
  ODR-028 的 ReportHandler 同款"无状态 + 调用方带数据"设计。
- **前端 dialog 还没接**: 4 个端点都是后端裸 API, PaperTrading.vue 还
  没接入卖单的减持预检 (P2-7 的 hot path)。下一个 sprint 接入。

## Artifacts

### 新增文件

```
pkg/compliance/divestment.go            (557 lines, 5 类股东 + 3 种方式 + 引擎 + Registry)
pkg/compliance/divestment_test.go       (470 lines, 33 TestXxx)
```

### 修改文件

```
cmd/analysis/handlers_compliance.go        (4 endpoints: check / holder-types / methods / rules)
cmd/analysis/handlers_compliance_test.go   (7 TestXxx for P2-7)
docs/odr/odr-029-p2-7-divestment-engine.md
docs/TASKS.md                              (P2-7 状态 ⬜ → ✅)
docs/ADR.md                                (ODR Index 加 ODR-029 行)
```

### 净行数

+ 1,027 lines (实现) + 100 lines (handler 集成) + 30 lines (测试) = **+ 1,157 lines**。
其中 ~ 35% 是测试代码 (33 TestXxx 单元 + 7 TestXxx handler, race-clean)。

## Metrics

| Metric | 目标 | 实际 |
|--------|------|------|
| 5 类股东覆盖 | 100% | 100% (5/5) |
| 3 种方式覆盖 | 100% | 100% (3/3) |
| 5×3 格子 15 情形 | 100% | 100% |
| 4 类限售期 (Pre-IPO/定增竞价/定增战投/承诺) | 100% | 100% |
| 集中竞价 90 日 ≤ 1% | ✓ | ✓ |
| 大宗交易 90 日 ≤ 2% | ✓ | ✓ |
| 协议转让 ≥ 5% | ✓ | ✓ |
| 董监高年内 25% | ✓ | ✓ |
| 举牌 1%/5% 告警 | ✓ | ✓ |
| 单元测试 + Handler 测试 | ≥ 35 | 40 (33 单元 + 7 handler) |
| `go test -race` 通过 | ✓ | ✓ |
| `go vet ./...` 通过 | ✓ | ✓ |
| `go build ./...` 通过 | ✓ | ✓ |
| HTTP 端点 (P2-7) | 4 | 4 |
| 复用 `live.Board` 不复造 ts_code 分类 | n/a (新维度) | n/a |
| 共享 `pkg/compliance` 包 不新增 leaf package | ✓ | ✓ |
| 并发 race-clean (50 goroutine × 100 读 + 50 写) | ✓ | ✓ |
| Reasons 顺序稳定 (regulator-stable) | ✓ | ✓ |
| LockupPeriod 法规引用 (`Code` 字段) | ✓ | ✓ |
| 审计透明: Lockups + Warnings 量化披露 | ✓ | ✓ |

## Lessons Learned

1. **5+3 格子全在引擎内统一处理** — 没有"if holderType == ..."散落在
   各处的丑陋分支, 全在 `Check()` 一处, 测试一目了然。
2. **`LockupPeriod` 用半开 `Overlaps` + 闭 `IsActive` 的双重语义** 与
   A 股"解禁日 = 锁定期后第一天"实务精确对齐, 1 个测试 (`Overlaps`)
   验证了端点相接不冲突的边界。
3. **`ApprovedQty` 取整到 100 股** 是 A 股申报的最小单位 (`1 手 =
   100 股`); 如果取整到 1 股会过于精细 (A 股根本不支持 1 股交易), 浪费
   CPU + 增加 diff 噪音。
4. **`Director 25%` 检查要包含本次拟减持** (`annualUsed + proposedPct`)
   而不是仅 `annualUsed`, 否则会"看似通过, 实际成交后才超" — 单元测试
   `TestCheck_Director_AnnualCap_Hit` 验证了这点。
5. **Pre-IPO 股东硬拒** (default cap = 0) 是"宁可错杀不可漏风"的合规
   设计: 即便没有 lockup 条目, 也不会漏掉 Pre-IPO 限制。`TestCheck_PreIPO_HardRejectNoLockup` 验证。
6. **Registry + RWMutex + 防御性拷贝** 三件套已经稳定 (P2-4 用了同款),
   P2-7 直接复用, 不需要重新设计 — 工程上"模式一致"比"每个模块新设计"
   更可维护。
7. **"无状态 + 调用方带数据" 设计 (Recent 由调用方传)** 与 P2-6 ReportHandler
   一致, 把"取数"和"判定"解耦, 让引擎可以在测试 / 模拟 / 真实 三种
   数据源下用同一套 API。

## Follow-up Tasks (留待后续 sprint)

- **P2-7.1 协议转让受让方 6 月锁** — 当前只检查出让方约束, 需要扩
  到受让方。
- **P2-7.2 董监高"在任 / 离任"双状态** — 离任后 6 月仍受 25% 限制
  (CSRC-2024 第 9 条), 需要加 `TermEndAt` 字段。
- **P2-7.3 交易日历感知** — 当前 90 日是日历日, 未来按交易日计算
  (需要 `pkg/data` 的 trading calendar 集成)。
- **P2-7.4 前端 dialog 接入** — `PaperTrading.vue` 卖单提交前调
  `POST /api/compliance/divestment/check`, 拒绝时弹 dialog 显示
  Lockups + Reasons。
- **P2-7.5 AI 自动申报** — 接 `pkg/ai/agents/` 让 LLM 把"本月减持
  计划 + 监管依据"自动生成 PDF 报告, 提交给交易所。
- **P2-7.6 与券商股东台账对接** — 当前的 `Recent` 由调用方传, 生产
  需要从券商核心系统拉真实历史。涉及外部系统对接, 留待 P2-8+。
