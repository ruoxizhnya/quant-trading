---
status: archived
last-verified: 2026-09-21
verified-by: 腾子（对 ODR-065 全栈审查的独立复核，含编译/测试实证）
---

# ODR-065 审查结果的独立复核（Meta-Review）

> **日期**: 2026-09-21
> **被复核对象**: [ODR-065](../odr/odr-065-fullstack-static-review.md) + [review-report-20260921.md](review-report-20260921.md)
> **性质**: 对审查本身的审查。ODR-065 是**纯静态**审查（自称"本机无 Go 工具链"）；本轮复核改为**实证优先**——能编译就编译、能跑测试就跑测试、能跑 git 就跑 git，只把真正跑不了的留作静态推演。
> **结论一句话**: ODR-065 的**判断力可靠**（24 项中 22 项确证），但**前提、定量与实施方案三处有硬伤**，照抄 §16 实施方案会编译失败。

---

## 1. 复核方法：把"静态推演"换成"能跑就跑"

ODR-065 §2.4 把 `go build` / `go vet` / `go test` / `npm test` / `-race` 全部列为"未执行"，据此声明"编译健康度未知"。复核实测：

| 命令 | ODR-065 声明 | 复核实测 | 判定 |
|------|-------------|---------|------|
| `go build ./...` | 未执行（无工具链） | **全绿，exit 0** | 前提错误 |
| `go vet ./...` | 未执行 | **全绿，零输出** | 前提错误 |
| `go test ./... -count=1` | 未执行 | **全绿，零失败包**（50s 跑完） | 前提错误 |
| `go test -race` | 未执行 | 无法执行：`-race requires cgo`，本机无 `gcc` | **限制属实**（但理由不是"无 Go 工具链"） |
| Git 推送状态 | main 领先 origin **51 提交** | `git rev-parse main` == `git ls-remote origin refs/heads/main` == `b18f0d0` | **误报** |

**Go 工具链位置**：`/c/Users/ruoxi/sdk/go1.25.0/bin`（`go version go1.25.0 windows/amd64`）。不在默认 PATH，需 `export PATH="/c/Users/ruoxi/sdk/go1.25.0/bin:$PATH"`。

> 这条本身值得记：ODR-065 因为没找到 Go 而放弃了全部动态验证，又因为这个放弃，把 C2/H5 的"数据竞争"停留在代码推演层面。**工具链在，只是没加 PATH。**

---

## 2. 逐条判定表（24 项）

| # | 发现 | 复核判定 | 复核手段 | 备注 |
|---|------|---------|---------|------|
| **C1** | 日收益率公式把现金腿当外部资金流 | ✅ **确证**（实证） | 写 repro 测试实跑 | 定量推导有误，见 §3.1 |
| **C2** | walk-forward 共享 runner | ✅ **确证** | 读 `walkforward.go` + `cache/cache.go` | 机制描述有误且**实际更严重**，见 §3.2 |
| **C3** | `/api/copilot/save` 路径遍历 + RCE | ✅ **确证** | 读 `handlers_copilot.go#L174-195`、路由 L278 | 无净化、无 staticcheck、落热加载目录 |
| **C4** | RBAC 未接线 | ✅ **确证** | 全仓 grep `RequireRole` 仅 1 处生产调用 | `handlers_auth.go:30` |
| **C5** | 11 张表 DDL 断层 | ✅ **确证** | 求差集实测 | 内联 DDL 实测 **22 张**，缺 11 张 |
| H1 | 印花税 0.001 应为 0.0005 | ✅ **确证** | `ashare.go#L53` | 注释"0.1%"，史实应为 0.05% |
| H2 | 涨跌停缺板块档位 | ✅ **确证** | `engine_daily.go#L168-178` | 仅 Normal/New/ST 三档，无 0.01 取整 |
| H3 | `*ST` 识别失效 | ✅ **确证** | `engine.go#L1507` `name[:2]` | 拿 2 字符去比 3/4 字符前缀，恒 false |
| H4 | 测试固化 H3 的 bug | ✅ **确证** | `engine_accessors_test.go#L287` | `{"*STXYZ.SH", false}` |
| H5 | MockTrader RLock 下经指针写 | ✅ **确证** | `mock_trader.go#L301-338` | 报告"降级 High"的定性恰当 |
| H6 | 无整手取整 | ◐ **维持负向证据** | grep 未找到取整逻辑 | 复核也未找到，仍未正向确认 |
| H7 | Windows 沙箱 rlimit no-op | ✅ **确证** | `rlimit_windows.go` 注释明写 "silently skipped" | |
| H8 | CI 无 `-race` 无前端 job | ✅ **确证** | `ci.yml#L48` 仅 `go test ./... -count=1` | |
| H9 | compose 端口绑 0.0.0.0 | ✅ **确证** | `docker-compose.yml#L28,L40` | |
| M1 | AGENTS.md 架构漂移 | ✅ **确证** | 实测 ADR-023/024 存在、021/022 已 superseded | |
| M2 | 表数口径"38 张" | ✅ **确证** | 内联实测 22 张；`AGENTS.md#L657` 写"内联 20 张" | |
| M3 | 双 migrations 死文档 | ✅ **确证** | `migrations/` 与 `docs/migrations/` 均不被执行 | |
| M4 | staticcheck 正则可绕过 | ✅ **确证** | `staticcheck.go#L197 Check` / `#L210 CheckOrError` | |
| M5 | 死 E2E 打 :8086 | ✅ **确证** | `ai-research.spec.ts#L76,81,88` | `cmd/ai` 已删 |
| M6 | `fundamentals_detail` 空表 | ⚠ **部分不准** | 摄取代码**存在**（`pkg/data/equitydeep/`、`cmd/data/handlers_equitydeep_ingest.go`） | "数据摄取代码未见"不准确；未接线/未跑数才是实情 |
| **L1** | main 领先 origin 51 提交 | ❌ **误报** | `git rev-parse` vs `git ls-remote` 完全一致 | 已在 `b18f0d0` 推送到位 |
| L2 | live engine 组合状态 | ◐ 未复核 | — | 报告自己标注未复核 |
| L3 | legacy HTML 残留 | ✅ 属实 | — | 低价值 |
| L4 | 文档导航失效 | ✅ **确证** | `docs/odr/` 不存在，ODR 在 `docs/archive/odr/` | ODR-065 自身也归档在后者 |

**统计**：确证 22 / 误报 1（L1）/ 部分不准 1（M6）/ 未复核 1（L2）。
ODR-065 自我标称"误报剔除 4 项"——复核认为其自身的剔除记录是准确的，但漏剔了 L1。

---

## 3. 三处需要纠正的实质错误

### 3.1 C1 的代数推导错了，夸大 2 倍

报告原文（§4 C1）：

> `netValue = (C₀ + b + P₀) - (-b) = C₀ + P₀ + 2b`，`dailyReturn = 2b / TotalValue`

**笔误在第一步**：买入花费 b 后现金是 `C₀ - b`，不是 `C₀ + b`。正确的 `currValue = (C₀ - b) + (P₀ + b) = C₀ + P₀`，于是 `netValue = C₀ + P₀ + b`，`dailyReturn = b / TotalValue`。

**实测验证**（临时 repro 测试，跑后已删）：T0 全现金 100,000 → T1 全仓买入且价格未动 → T2 价格未动。

```
returns = [1, 0]     // T1 日收益 = 1.0，即 100%
want    = [0, 0]
```

实测 **100%**，与我的 `b/TotalValue` 吻合，与报告的 `2b/TotalValue`(=200%) 不符。

**通用形式**（报告未给出，但更有用）：该公式等价于 `(持仓市值变动) / 总资产`。推论：

- **无交易日完全正确**（cashFlow=0 时公式退化为正确式）；
- **只有交易当日**注入 `±成交额/总资产` 的虚假脉冲（买正、卖负）。

所以报告"波动率被系统性高估"方向对，但"日收益率序列被污染"的说法过宽——准确说法是**仅在交易日注入脉冲，交易越频繁污染越大**。另外，"满仓时注入 ±10% 量级"也不对：满仓买入 b≈TotalValue 时注入的是 **100%** 量级。

> 结论不变（是真 bug、该修、修法也对），但**定量数字不可引用**。

### 3.2 C2 的机制描述错了，实际更严重

报告说共享 runner 导致"窗口 N+1 的 OOS 数据可能在窗口 N 执行期间被写入共享缓存并被读到 → OOS 指标**偏乐观**"。

**真实机制在 `pkg/backtest/cache/cache.go` 的 `Warm()` fast-path（L99-110）**：

```go
haveAll := len(cm.inMemoryOHLCV) >= len(symbols)   // 只看 symbol 在不在
if haveAll { ... return nil }                       // 命中就跳过 fetch，完全不看日期范围
```

缓存 key 是 `symbol → 全部 bars`，**不含日期范围**。于是：

1. 窗口 1 warm 了 `[2020-01-01, 2020-12-31]`；
2. 窗口 2 请求 `[2021-01-01, 2021-12-31]`，fast-path 判定"已 warm"→ **直接跳过拉取**；
3. 窗口 2 的 `Get()` 在 2020 年的 bars 上做 `DateRangeBounds(2021 范围)` → 返回**空集**或**错误区间数据**。

后果不是"轻微偏乐观"，而是**后续窗口跑在错误/空的数据上，结果完全无效**。

**数据竞争这条也比报告说的重**。报告称"map/slice 无锁保护，-race 必报"。实际 `CacheManager` 用了 `atomic.Pointer`，但：

```go
cm.inMemoryOHLCV[symbol] = bars              // 写者在 mu.Lock 内改 map
cm.inMemoryOHLCVAtomic.Store(&cm.inMemoryOHLCV)  // 存的是同一 map 字段的地址！
```

快照存的是**同一个 map 的地址**，没有隔离效果。读者 lock-free 读 `*snap` 就是读同一个 map → 构成 Go runtime 的**并发 map 读写**，后果不止 `-race` 报警，可能是 `fatal error: concurrent map read and map write`（**不可 recover**）。

> 这条复核无法用 `-race` 实证（无 gcc），属静态推演，但推理链直接落在源码行上。

### 3.3 §16 实施方案存在编译级错误

§16 开头写明"落地前已核实现行签名"，但两处签名是错的：

| 声明 | 实际 | 后果 |
|------|------|------|
| `NewWalkForwardEngine(factory, store)` （2 参数） | `walkforward.NewWalkForwardEngine(runner, store, logger)`（**3 参数**，`walkforward.go#L26`） | 照抄**编译失败**（漏 `logger`） |
| `return backtest.NewEngine(v, provider, logger)` | `func NewEngine(...) (*Engine, error)`（`engine.go#L171`，**双返回值**） | 签名不匹配 |

另外 `cmd/analysis/setup.go#L342` 实际调用的是 `pkg/backtest/aliases.go#L147` 的 2 参数 wrapper（内部补 logger），改工厂时要同时动 aliases——§16 未提这一层。

**教训**：实施方案里"已核实"的签名，必须真的回源码核对。这跟 ODR-065 自己总结的 Lesson 2（"没有执法者的约定必然漂移"）是同一类病——**声明已核实 ≠ 已核实**。

---

## 4. ODR-065 遗漏的、但对排期最关键的一条：修 C1/C2 现在是零成本

报告 §10 把 C1 的代价写成"修复后**必须重跑历史回测**，此前因子筛选结论作废重评"。

这个推演隐含假设"库里已经有一批历史回测结论"。**项目实际不是这样**：按项目记录，数据库当前是**空的**——`stocks` / `ohlcv_daily_qfq` / `stock_fundamentals` / `trading_calendar` 全 0 行，同步卡在 `TUSHARE_TOKEN` 未设置。

这把优先级完全翻转了：

- **现在修 C1/C2：没有历史结论要作废，成本≈0。**
- 一旦数据同步跑通、开始积累回测与 walk-forward 报告后再修：全量重跑 + 结论作废。

也就是说 **C1/C2 应该排在 C3/C4（安全项）之前**，而不是报告建议的把 C3 排第一。理由：C3 是"认证后 RCE"，在本项目是**单人自托管本地工具**的语境下，攻击面有限；而 C1/C2 是"污染此后一切研究结论"的根因，且**修复窗口正在关闭**。

---

## 5. 建议的排期（复核后修正版）

| 批次 | 项 | 理由 |
|------|-----|------|
| **0（先做）** | C1 日收益率 + C2 窗口隔离 | **趁库空，零作废成本**。修完再开始积累任何回测结论 |
| 1 | 数据同步（`TUSHARE_TOKEN`） | 否则后面所有修复都无从在真实数据上验证 |
| 2 | C3 save 端点 + C4 RBAC 接线 | 安全收口，改动都不大 |
| 3 | C5 DDL 补齐 + 一致性断言 | 新环境可用性 |
| 4 | H1/H2/H3+H4 A 股规则（H3+H4 同 commit） | 与数据同步并行可做 |
| 5 | H8 CI 门禁（`-race` + 前端）→ H5 → H7 | H5 依赖 `-race`，而 `-race` 在 CI（Linux）上才有 gcc |
| 6 | 文档对齐 M1/M2/M3/L4 + M5/M6 清理 | |

**另需补一条任务**：AUD-12 要加的 `-race` 门禁，在本机（Windows，无 gcc）**无法本地预验**。要么装 gcc（TDM-GCC / mingw），要么明确"race 只能在 CI 验"。建议在 CI 上先跑一次 `-race` 摸清存量竞争数量，再决定门禁是否一次性开启。

---

## 6. 对 ODR-065 本身的评价

**值得肯定的**：取证纪律（每条带 `文件:行号`）、主动记录误报剔除（4 项）、区分正负向证据并标注置信度（H6 明确标"负向证据"）、降级 MockTrader 时如实说明"实害低于字面严重性"。这些都做到了，所以 24 项里 22 项经得起实证复核——**这个准确率本身说明方法是有效的**。

**要警惕的**：

1. **"环境不具备"要真的去查**。一句"无 Go 工具链"放弃了全部动态验证，进而让两条 Critical 只能停在推演层面，还顺势产生了 C1 的定量错误（跑一次就能发现）。
2. **定量推导要能被实测检验**。C1 的 `2b` 是纯笔误，一个 5 行的 repro 测试就能当场证伪。
3. **"已核实"是承诺，不是修辞**。§16 的签名错误会直接让照抄实施方案的人编译失败。
4. **影响推演要贴着项目实际状态**。C1 的"历史结论作废"在空库前提下不成立，这个判断直接影响排期。

---

## 7. 附带发现：被复核报告自身有 2 处死链，而检查器抓不到

`review-report-20260921.md` 归档后相对链接层级未同步修正（它自己在开头声明"已同步修正"）：

| 报告中的链接 | 实测 | 应为 |
|-------------|------|------|
| `[ODR-065](../../odr/odr-065-fullstack-static-review.md)` | **DEAD**（`docs/odr/` 不存在） | `../odr/...` |
| `[docs/TASKS.md](../../../TASKS.md)` | **DEAD**（仓库根无 TASKS.md） | `../../TASKS.md` |

**为什么没被抓到**：`tools/check_doc_links.py` 只扫常青层与活跃层，**显式跳过 `archive/`**（实测输出："扫描 51 个 Markdown 文件（仅常青层与活跃层（跳过 archive/））"）。而归档层恰恰是 ODR/报告最密集、链接层级最容易错的地方——**元检查的盲区正好落在最需要它的区域**。

建议（可与 AUD-14 合并）：让 `check_doc_links.py` 增加 `--include-archive` 开关，或至少对 `docs/archive/` 做一次全量补扫。这与 ODR-065 自己总结的 Lesson 2 是同一条：**没有执法者的约定必然漂移**，而这里连执法者的管辖范围都漏了一块。

---

## 8. 复核未覆盖

- `-race` 相关（C2 竞争、H5）：无 gcc，**未实证**；
- 前端 `npm test` / `typecheck`：未跑（非本轮重点，且报告亦未覆盖）；
- M6 摄取链路是否真的未接线：仅确认代码存在，未追踪调用链；
- H6 整手取整：仍未正向确认（与 ODR-065 一样是负向证据）。

---

*复合对象：ODR-065 + review-report-20260921.md。本报告为一次性审计快照，按 AGENTS.md Rule 3 归档于 `docs/archive/reports-2026-Q3/`。*
