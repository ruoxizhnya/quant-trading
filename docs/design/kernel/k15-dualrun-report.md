# K15 双跑对齐实验报告（Go 内核 vs NautilusTrader 侧车）

> 任务：TASKS.md K15（结果映射 + 双跑对齐实验）。交付 commit 见台账 ⑦ 收口记录。
> 数据文件：[k15-dualrun-report-bh.md](k15-dualrun-report-bh.md)（买入持有）/ [k15-dualrun-report-quarterly.md](k15-dualrun-report-quarterly.md)（季度调仓）——由 `cmd/dualrun` 自动生成，逐指标 + 逐笔对账 + 归因清单完整可复查。
>
> ⚠️ **口径注记（2026-10-14 · DATA-04）**：本文与两份分报告的数值是在 `ohlcv_daily_qfq.volume` 为**「手」**口径下测得的。同日 DATA-04 将该字段统一为**「股」**（×`sharesPerLot`，对齐规范层 schema 的既有声明）——`pkg/backtest/tracker` 的流动性阈值 `volume × 0.1` 与订单量（股）此前存在 **100 倍量纲错配**，修正后成交行为会有变化。本报告作为**当时的实测记录不改**（历史不可改写）；若需按新口径对标具体数值，须重跑 `cmd/dualrun`。**已复验**：DATA-04 后 `go run ./cmd/dualrun -strategy bh -gate` 仍在 K15 阈值内全绿，**对齐结论不受影响**。

## 1. 实验设置

| 项 | 值 |
|---|---|
| 窗口 | 2024-01-02 .. 2024-12-31（全年，两侧同日历） |
| 标的 | 000001.SZ（平安银行）、600000.SH（浦发银行）——主板，避开 ST/创业板（§8.4 最小集） |
| 本金 | 1,000,000 CNY（两侧同：NT 按 K14-R1 A 每实例分摊 capital/N，Go 同口径） |
| 信号流 | 同一份目标权重流（wire `signals` 字段，两侧同源消费）；`bh` 首日建仓/末日清仓，`quarterly` 季度调仓含减仓/加仓/清仓 |
| 费率 | 两侧同源：Go 走 Tracker+ComputeFees（全三项，与 NT `ashare.compute_fees` parity fixture 同参数：佣金 0.03%/最低 ¥5、过户费 0.001%、印花税卖侧 0.05%）；滑点 0；D_max=0.10；整手 100；主板 ±10% |
| 数据 | Go：PG `ohlcv_daily_qfq`；NT：Parquet catalog（同源 PG 导出，§8.2 口径） |
| 复现命令 | `go run ./cmd/dualrun -strategy bh` / `-strategy quarterly`（前置：PG 就绪 + `python -m sidecar --port 9100 --catalog nt-sidecar/catalog`） |

## 2. 结论

**两内核在同一信号流下，指标级对齐度极高**：

| 指标 | bh 相对差 | quarterly 相对差 | 实测容差阈值（写回 §8 表 1） |
|---|---:|---:|---|
| total_return | -0.086% | -0.076% | **|Δ| ≤ 0.2%** |
| annual_return | -0.086% | -0.076% | \|Δ\| ≤ 0.2% |
| sharpe | -0.109% | -0.040% | \|Δ\| ≤ 0.05（绝对） |
| sortino | -0.276% | -0.345% | \|Δ\| ≤ 0.05（绝对） |
| max_drawdown | -0.346% | -0.278% | \|Δ\| ≤ 0.5% |
| calmar | +0.261% | +0.202% | \|Δ\| ≤ 0.05（绝对） |
| total_trades | +1.85% | -3.70% | ~~\|Δ\| ≤ 5%（相对）~~ **不设硬阈**（K16 ① 裁决：配对计数 + Direction 词汇敏感，见 §3 归因④扩展；笔数守恒由 fills 对账腿 \|Δ\|≤5% 执法） |
| win_rate | +37.7% | +42.1% | **不设硬阈**（见归因④） |

- **确定性腿 ✅**：两侧各跑两遍，净值/成交/费率逐项一致（忽略随机 UUID 编号）。
- **成交明细对账**：matched 363/328（bh/quarterly），go_only 68/89、nt_only 61/81——全部为价源精度差的下游路径分叉（±100 股级 delta 偏移），无机制性缺笔（归因①③）。
- **阈值判定法**：对齐回归超阈 → 显式失败；win_rate 与 total_trades 不进指标阈值（同族配对敏感性，归因④及扩展），笔数守恒由 fills 对账腿（原始条数 |Δ|≤5%）+ mismatch=0 执法。门禁执法者 = `cmd/dualrun -gate`（CI `align-gate` job）。

## 3. 差异归因（实测 → 口径，完整版见数据文件）

1. **价源精度（主因）**：PG 前复权浮点价（7.566182…）vs catalog Decimal tick 0.01（7.57）——每笔 ≤0.005 价差，经恒定名义额再平衡放大成持仓路径微分叉（go_only/nt_only 的 ±100 股级 delta 偏移）。两侧同源 PG，非取数时点问题（§8.2 观察项关闭）。
2. **费用（K15-R1 复查后关闭）**：两侧费用口径**同源且逐笔对齐**（matched 行 go=77.40/nt=77.44、40.24/40.24、5.03/5.03，残差全部来自 1 的价差）。K15 首轮报告的「NT 缺过户费」是 **diff 展示层 bug**（matched 行 nt 费用误用 Go trade 的 commission 分量 74.905，而非 NT trade 的全项 77.44），已在 K15-R1 修复并重跑验证——NT FeeModel 链路本身无缺陷。
3. **win_rate 敏感性**：配对算法对明细错位放大，不设硬阈。
4. **归因③/④扩展（K16 ① fixture 复核，2026-10-13）——total_trades 也不设硬阈**：`metrics.calculateTradeMetrics` 的 total_trades 是 entry/exit **配对完成数**（entryPrice 加权均价、逐笔 min(qty) 抵扣、配对完成才 totalTrades++），与 win_rate **同一个算法、同一族敏感性**；更根本的是它对成交的 **Direction 词汇表**敏感（long/short/close 三支语义分流），而两侧词汇本就不同（wrReplay 卖出记 `DirectionShort`，NT fills 映射记 `DirectionClose`）。K15 真数据 216 vs 220（+1.85%）落在 5% 内是小样本运气——CI 合成 fixture 实测 128 vs 233（+82%）证明它作为硬阈是 flake 源。**裁决**：total_trades 从指标层硬阈降级（report 照常输出、仅不设阈），改为**词汇无关的成交笔数对账腿**（fills 原始条数 |Δ| ≤ 5%，见 `cmd/dualrun/gate.go`）；若将来要指标级稳定，需另立任务改配对口径（同 §5 遗留）。
5. **未触发项如实记录**：本窗口无涨跌停拒单、无 T+1 拒单（卖昨仓合法）——护栏由两侧单测覆盖（Go tracker T+1 测试、侧车 sellable 测试、wrReplay no-chase 破坏腿）+ K15-R1 的 fills 费用回归腿（`tests/test_k15r1_fees.py`）。

## 4. 工单③-5 费率口径裁决（b 项执行记录）

**裁决：b) 报告单列归因。** Go 记账选 Tracker+ComputeFees 路径（全三项费率，与 NT parity fixture 同源），「撮合路径只计佣金」的 CostModel 口径边界**不进入本次对齐**；ExecutionService 路径的口径边界维持 §8.4 记档不变。实测两侧费率公式同源成立（费用残差 = 价源精度差，非费率表差异）。

## 5. 遗留与后续

- **K15-R1 已收口**（费用链路复查）：根因是对账报告的展示 bug 而非 NT FeeModel 缺陷；修复后费用逐笔对齐。回归腿 `tests/test_k15r1_fees.py::FillReportFeeRegressionTest` 钉住「fills commission == compute_fees.total」。
- **运维教训（记档）**：本轮排查中本机 9100 端口曾同时存在**两个 sidecar 监听进程**（Windows 允许端口复用绑定），连接随机分配到新旧代码——任何「同请求不同结果」的诡异现象先查端口占用者（`Get-NetTCPConnection -LocalPort 9100`）再查代码。
- win_rate 配对算法对微差敏感是 `metrics.calculateTradeMetrics` 的既有特性（非本任务范围）；若要指标级稳定，需另立任务改配对口径。
- K16 前置已满足（K15 完成）；对齐回归进 CI 门禁是 K16 ① 的范围，本实验的容差阈值表即其门禁参数。
