// K16 ① · 对齐回归门禁：阈值评估（「双跑 diff 超阈即红」）。
//
// 阈值表是 K15 实验的产物（docs/design/kernel/k15-dualrun-report.md §2，
// 回写设计文档 §8 表 1），本文件是它的**执法者**：同一组数字只有这一处
// Go 实现，改阈值必须同步改 K15 报告与设计文档（否则门禁与文档脱节，
// 这正是本项目最忌讳的双真相）。
//
// 判定口径与报告表格一致：
//   - 相对差阈值（RelDiff = (NT-Go)/Go）：total_return / annual_return
//     ≤ 0.2%，max_drawdown ≤ 0.5%；
//   - 绝对差阈值（Diff = NT-Go）：sharpe / sortino / calmar ≤ 0.05；
//   - win_rate **不设阈值**（K15 归因④：配对算法对成交明细微差放大，
//     阈值只会制造假红；明细对账为准）。
//
// total_trades 的裁决（K16 ① fixture 实测复核，2026-10-13）：
//
//	total_trades 与 win_rate **同出** metrics.calculateTradeMetrics 的
//	entry/exit 配对算法（entryPrice 加权均价、逐笔 min(qty) 抵扣、配对
//	完成才 totalTrades++），配对敏感性是同一机制——K15 归因④描述的
//	「配对序列错位」同时作用于两者。更根本的是它对成交的 **Direction
//	词汇表**敏感（性能计数按 long/short/close 三支语义分流，见
//	performance.go:284-419），而两侧词汇本就不同（wrReplay 卖出记
//	DirectionShort，NT fills 映射记 DirectionClose）——K15 真数据
//	216 vs 220（+1.85%）落在 5% 内是小样本运气，CI fixture 实测
//	128 vs 233（+82%）证明它**不可作为机制门禁**（flake 源）。
//	故 total_trades 从指标层硬阈降级，改由**词汇无关的成交笔数对账腿**
//	执法（下方 fills 腿）；report 的 total_trades 行照常输出，仅不设阈。
//
// 门禁还守三条腿（确定性 + 机制完整性），它们不是容差而是布尔/笔数口径：
//   - 任一侧两遍重跑不一致 → 红（确定性是归因的前提）；
//   - 成交对账出现 mismatch（同键量价费矛盾）→ 红（机制性差异，
//     价源精度分叉只该产生 go_only/nt_only，不该产生同键矛盾）；
//   - 两侧成交笔数相对差 > 5% → 红（词汇无关的量级守恒，代替 total_trades）。
package main

import (
	"fmt"
	"math"
	"strings"
)

// tolerance 是一个指标的容差。Kind 区分相对差/绝对差两种口径。
type tolerance struct {
	metric string
	kind   string // "rel"（|NT-Go|/|Go|）或 "abs"（|NT-Go|）
	limit  float64
}

// k15Tolerances 钉住 K15 实验的容差表（报告 §2 → 设计文档 §8 表 1）。
// win_rate 与 total_trades 刻意缺席：两者同出配对算法（K15 归因④ +
// K16 ① 复核，见包注释），total_trades 由 fills 腿（下方）词汇无关执法。
var k15Tolerances = []tolerance{
	{metric: "total_return", kind: "rel", limit: 0.002},  // |Δ| ≤ 0.2%
	{metric: "annual_return", kind: "rel", limit: 0.002}, // |Δ| ≤ 0.2%
	{metric: "max_drawdown", kind: "rel", limit: 0.005},  // |Δ| ≤ 0.5%
	{metric: "sharpe", kind: "abs", limit: 0.05},         // |Δ| ≤ 0.05（绝对）
	{metric: "sortino", kind: "abs", limit: 0.05},        // |Δ| ≤ 0.05（绝对）
	{metric: "calmar", kind: "abs", limit: 0.05},         // |Δ| ≤ 0.05（绝对）
}

// fillsTolerance 是成交笔数对账腿的相对差阈（与 K15 total_trades 的 5%
// 同量级，但比的是**原始 fill 条数**而非配对计数）。
const fillsTolerance = 0.05

// GateViolation 一条超阈/破坏项。Message 直接进 CI 日志与告警。
type GateViolation struct {
	Rule    string
	Message string
}

// EvaluateGate 评估一次双跑报告是否通过对齐门禁。
//
// 返回 nil = 通过；非 nil = 违例清单（至少一条）。相对差的分母用 |Go|
// （Go 侧是基准内核），Go=0 时退化为绝对差口径并在消息里注明——
// 不用「除零跳过」掩盖一条无法判定的指标。
func EvaluateGate(r *Report) []GateViolation {
	var violations []GateViolation

	if !r.GoDeterministic {
		violations = append(violations, GateViolation{
			Rule:    "determinism/go",
			Message: "Go 侧两遍重跑不一致（确定性腿失败）——先修确定性再谈对齐",
		})
	}
	if !r.NTDeterministic {
		violations = append(violations, GateViolation{
			Rule:    "determinism/nt",
			Message: "NT 侧两遍重跑不一致（确定性腿失败）——先修确定性再谈对齐",
		})
	}

	// 指标层：逐项对阈值表。
	for _, tol := range k15Tolerances {
		row, ok := findMetric(r, tol.metric)
		if !ok {
			violations = append(violations, GateViolation{
				Rule:    "metric/" + tol.metric,
				Message: fmt.Sprintf("指标 %s 不在报告中——门禁无法判定，视为失败（缺指标 ≠ 通过）", tol.metric),
			})
			continue
		}
		var errText string
		switch tol.kind {
		case "abs":
			if math.Abs(row.Diff) > tol.limit {
				errText = fmt.Sprintf("|Δ|=%.6g 超绝对阈 %.6g", math.Abs(row.Diff), tol.limit)
			}
		case "rel":
			if row.Go == 0 {
				// Go=0 无可比相对差：退化成绝对差口径（阈值即限值本身），
				// 并注明口径变化，绝不用「除零跳过」放过一条。
				if math.Abs(row.Diff) > tol.limit {
					errText = fmt.Sprintf("Go=0（相对差不可算）|Δ|=%.6g 超绝对退化阈 %.6g", math.Abs(row.Diff), tol.limit)
				}
			} else if rel := math.Abs(row.Diff / row.Go); rel > tol.limit {
				errText = fmt.Sprintf("相对差=%.4g%% 超阈 %.4g%%", rel*100, tol.limit*100)
			}
		}
		if errText != "" {
			violations = append(violations, GateViolation{
				Rule: "metric/" + tol.metric,
				Message: fmt.Sprintf("%s: go=%.6g nt=%.6g %s（K15 阈值表，见 k15-dualrun-report.md §2）",
					tol.metric, row.Go, row.NT, errText),
			})
		}
	}

	// 明细层：mismatch 是同键（日期/标的/方向/量/价到分）的量价费矛盾，
	// 价源精度分叉只应产生 go_only/nt_only——出现 mismatch 即机制性差异。
	for _, row := range r.TradeRows {
		if row.Kind == "mismatch" {
			violations = append(violations, GateViolation{
				Rule: "trades/mismatch",
				Message: fmt.Sprintf("成交对账 mismatch: %s %s %s %s — 同键量价费矛盾，机制性差异",
					row.Day, row.Symbol, row.Side, row.Detail),
			})
		}
	}

	// fills 腿（total_trades 的词汇无关替身，见包注释）：两侧**原始成交
	// 笔数**（matched+单侧）的相对差 ≤ 5%。对账计数不看 Direction 词汇，
	// 只数 fill 条数——wrReplay 的 DirectionShort 与 NT 的 DirectionClose
	// 在此同为一条 SELL fill，不再放大成假红。
	goFills := r.MatchedTrades + r.GoOnlyTrades
	ntFills := r.MatchedTrades + r.NTOnlyTrades
	if goFills == 0 && ntFills == 0 {
		violations = append(violations, GateViolation{
			Rule:    "trades/fills",
			Message: "两侧成交笔数均为 0——门禁无法判定对齐（空成交 ≠ 通过），检查信号流/数据是否灌进来了",
		})
	} else {
		base := goFills
		if base == 0 {
			base = ntFills // Go 侧 0 笔而 NT 有：退化为绝对条数差，注明口径
		}
		rel := math.Abs(float64(ntFills-goFills)) / float64(base)
		if rel > fillsTolerance {
			violations = append(violations, GateViolation{
				Rule: "trades/fills",
				Message: fmt.Sprintf(
					"成交笔数相对差=%.4g%% 超阈 %.4g%%（go=%d nt=%d，matched=%d go_only=%d nt_only=%d）——词汇无关的量级守恒被破坏",
					rel*100, fillsTolerance*100, goFills, ntFills, r.MatchedTrades, r.GoOnlyTrades, r.NTOnlyTrades),
			})
		}
	}
	return violations
}

func findMetric(r *Report, name string) (metricRow, bool) {
	for _, m := range r.MetricRows {
		if m.Name == name {
			return m, true
		}
	}
	return metricRow{}, false
}

// FormatViolations 渲染违例清单（CI 日志 / 告警正文）。
func FormatViolations(vs []GateViolation) string {
	if len(vs) == 0 {
		return "对齐门禁通过：全部指标在 K15 阈值内，确定性腿一致，无成交 mismatch"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "对齐门禁失败：%d 项违例\n", len(vs))
	for i, v := range vs {
		fmt.Fprintf(&b, "  %d. [%s] %s\n", i+1, v.Rule, v.Message)
	}
	return b.String()
}
