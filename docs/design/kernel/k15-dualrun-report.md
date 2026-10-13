# K15 双跑对齐实验报告（Go 内核 vs NautilusTrader 侧车）

> 任务：TASKS.md K15（结果映射 + 双跑对齐实验）。交付 commit 见台账 ⑦ 收口记录。
> 数据文件：[k15-dualrun-report-bh.md](k15-dualrun-report-bh.md)（买入持有）/ [k15-dualrun-report-quarterly.md](k15-dualrun-report-quarterly.md)（季度调仓）——由 `cmd/dualrun` 自动生成，逐指标 + 逐笔对账 + 归因清单完整可复查。

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
| total_trades | +1.85% | -3.70% | \|Δ\| ≤ 5%（相对） |
| win_rate | +37.7% | +42.1% | **不设硬阈**（见归因④） |

- **确定性腿 ✅**：两侧各跑两遍，净值/成交/费率逐项一致（忽略随机 UUID 编号）。
- **成交明细对账**：matched 363/328（bh/quarterly），go_only 68/89、nt_only 61/81——全部为价源精度差的下游路径分叉（±100 股级 delta 偏移），无机制性缺笔（归因①③）。
- **阈值判定法**：对齐回归超阈 → 显式失败；win_rate 不进阈值（明细敏感性，归因④）。

## 3. 差异归因（实测 → 口径，完整版见数据文件）

1. **价源精度**（主因）：PG 前复权浮点价（7.566182…）vs catalog Decimal tick 0.01（7.57）——每笔 ≤0.005 价差，经恒定名义额再平衡放大成持仓路径微分叉。两侧同源 PG，非取数时点问题（§8.2 观察项关闭）。
2. **费用微差**：Go ComputeFees 全三项逐笔精确对上；NT fills 报告的 commission ≈ 纯佣金（过户费分量未体现，单笔约 3% 低估）→ **立 K15-R1**：查 AShareFeeModel 返回值到 NT fills 报告的链路。累计影响约 -0.08% total_return。
3. **win_rate 敏感性**：配对算法对明细错位放大，不设硬阈。
4. **未触发项如实记录**：本窗口无涨跌停拒单、无 T+1 拒单（卖昨仓合法）——护栏由两侧单测覆盖（Go tracker T+1 测试、侧车 sellable 测试、wrReplay no-chase 破坏腿）。

## 4. 工单③-5 费率口径裁决（b 项执行记录）

**裁决：b) 报告单列归因。** Go 记账选 Tracker+ComputeFees 路径（全三项费率，与 NT parity fixture 同源），「撮合路径只计佣金」的 CostModel 口径边界**不进入本次对齐**；ExecutionService 路径的口径边界维持 §8.4 记档不变。实测中两侧费率公式同源成立（每笔费用差全部归因到 2 的链路疑点，非费率表差异）。

## 5. 遗留与后续

- **K15-R1**（随本任务立）：NT 侧过户费分量未在 fills 报告体现——复查 AShareFeeModel 在 NT 2.0.0rc6 的生效路径（FeeModel.get_commission 返回的 Money 是否被撮合层覆盖/拆分）。修复后阈值可收紧（预计 total_return |Δ| ≤ 0.1%）。
- win_rate 配对算法对微差敏感是 `metrics.calculateTradeMetrics` 的既有特性（非本任务范围）；若要指标级稳定，需另立任务改配对口径。
- K16 前置已满足（K15 完成）；对齐回归进 CI 门禁是 K16 ① 的范围，本实验的容差阈值表即其门禁参数。
