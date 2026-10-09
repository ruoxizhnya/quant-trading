package contracts

import (
	"fmt"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// OBS-01：把「假成功」（票池为空 / 0 成交 / 零值日期 / 垃圾指标）从
// 「成功」里摘出来，收敛成一处纯函数 + 一组稳定的 Code。
//
// 落点为什么在 contracts（而不是 pkg/backtest 或 pkg/validation）：
//   - contracts 是叶子包（只依赖 domain / fees / market），任何消费方
//     （引擎本身、job、pipeline、loop）引用它都不会引入 import 环。
//   - 裁定输入是 domain.BacktestResult（引擎的产物），输出是纯数据 —— 不放
//     在引擎里，是为了让「谁算的」和「谁看的」解耦：引擎算一次挂到结果上，
//     报告层与自动化层只读，不各自重算（口径漂移正是 OBS-01 要防的病）。

// 四类「无效运行」的 Code。它们是**稳定标识**，不是给人读的文案 ——
// 文案会改，Code 不能改：自动化层（loop 的失败计数、验证器维度）靠它做判断。
const (
	// InvalidEmptyUniverse：整轮回测里没有任何一个交易日的票池非空。
	InvalidEmptyUniverse = "empty_universe"
	// InvalidZeroTrades：成交数为 0。
	InvalidZeroTrades = "zero_trades"
	// InvalidZeroStartDate：起始日期是零值（0001-01-01）。
	InvalidZeroStartDate = "zero_start_date"
	// InvalidGarbageMetric：某个公开指标是 NaN / ±Inf，或超出物理上限。
	InvalidGarbageMetric = "garbage_metric"
)

// maxMetricAbs 是「物理上限」阈值。
//
// 裁决：任何真实 A 股回测的公开指标都不会超过 1e12（总收益 100 万倍、
// Sharpe 1e12 都已是荒谬量级）。而 math.MaxFloat64（≈1.797e308）这类由
// 「零成交 / 零下行波动」除出来的垃圾值、NaN、±Inf 一律远超它。
// 取 1e12 既能把垃圾值网住，又给正常回测留了 5 个数量级的余量。
//
// 已知边界：一条「全程无下跌日」的曲线会让 Sortino（分母=下行波动）退化成
// MaxFloat64 —— 那本就不是可用的数字，被判为 garbage_metric 是符合预期的。
const maxMetricAbs = 1e12

// InvalidReason 描述一条「无效运行」的判定依据。
//
// Code 用于机器判断（稳定）；Detail 用于人读（可改）。
type InvalidReason struct {
	Code   string
	Detail string
}

// CheckValidity 对一次回测结果做「有效运行」裁定，返回全部命中的依据
// （可能多于一条 —— 空票池通常同时是零成交，两条都该报出来）。
//
// 纯函数：只读 res，不碰 DB、不碰时钟、不产生副作用。
// res 为 nil 时返回 nil（没有结果 ≠ 结果无效，那是调用方的另一条错误路径）。
func CheckValidity(res *domain.BacktestResult) []InvalidReason {
	if res == nil {
		return nil
	}

	var reasons []InvalidReason

	if res.UniverseMaxSize <= 0 {
		reasons = append(reasons, InvalidReason{
			Code: InvalidEmptyUniverse,
			Detail: "整个回测区间内没有任何一个交易日的票池非空（eligibleUniverse 始终为空）—— " +
				"票池解析失败或标的元数据缺失，本次运行没有观测到任何可交易标的",
		})
	}

	if res.TotalTrades == 0 {
		reasons = append(reasons, InvalidReason{
			Code: InvalidZeroTrades,
			Detail: "成交数为 0 —— 一笔成交都没有，收益与风险指标无从谈起" +
				"（零成交不是零成本，它是什么都没证明）",
		})
	}

	if res.StartDate.IsZero() {
		reasons = append(reasons, InvalidReason{
			Code:   InvalidZeroStartDate,
			Detail: "起始日期是零值（0001-01-01）—— 回测区间没有被正确传入",
		})
	}

	for _, m := range resultMetrics(res) {
		if math.IsNaN(m.value) || math.IsInf(m.value, 0) || math.Abs(m.value) > maxMetricAbs {
			reasons = append(reasons, InvalidReason{
				Code: InvalidGarbageMetric,
				Detail: fmt.Sprintf(
					"指标 %s=%g 不是有限值或超出物理上限（|x|>%g）—— "+
						"这类 MaxFloat64 / NaN / ±Inf 是计算垃圾值，不是真实结果",
					m.name, m.value, maxMetricAbs),
			})
		}
	}

	return reasons
}

// InvalidReasonCodes 把裁定的 Detail 丢掉，只留 Code —— 挂到结果 / 响应
// 上的就是这份列表（自动化层按 Code 判断，不需要读文案）。
func InvalidReasonCodes(reasons []InvalidReason) []string {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]string, len(reasons))
	for i, r := range reasons {
		out[i] = r.Code
	}
	return out
}

// metricEntry 是一项被裁定的公开指标。
type metricEntry struct {
	name  string
	value float64
}

// resultMetrics 列出要做有限性 / 上限裁定的公开数值指标。
//
// 只收「公开指标」：整数成交计数（WinTrades/LoseTrades）与时点
// （MaxDrawdownDate）不在其列 —— 它们不会被除出 MaxFloat64。
func resultMetrics(res *domain.BacktestResult) []metricEntry {
	return []metricEntry{
		{"total_return", res.TotalReturn},
		{"annual_return", res.AnnualReturn},
		{"sharpe_ratio", res.SharpeRatio},
		{"sortino_ratio", res.SortinoRatio},
		{"max_drawdown", res.MaxDrawdown},
		{"win_rate", res.WinRate},
		{"avg_holding_days", res.AvgHoldingDays},
		{"calmar_ratio", res.CalmarRatio},
	}
}
