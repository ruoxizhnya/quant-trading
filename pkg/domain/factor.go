package domain

// ─── 因子名册：兼容薄壳（D3 起降级） ───
//
// 本文件的 FactorType 常量曾是因子注册表的「代码冻结」形态。D3 起，
// 计算注册表的唯一权威是 factor_def_seed.go 的 BuiltinFactorDefinitions
// （库里公式种子）+ pkg/data.Registry（加载时分派）；名册常量降级为
// **兼容薄壳**——保留原因（引用面盘点，2026-10-14）：
//
//   - pkg/backtest/cache/factor_cache.go：回测预热清单（存量，D6 收敛）；
//   - pkg/strategy/plugins/multi_factor.go：策略插件逐因子读取；
//   - pkg/storage、cmd/*：查询/序列化边界按名寻址。
//
// 纪律：新增计算因子只加种子定义，不在这里加常量；ParseFactorType 仍是
// 名册的合法集合判定（种子定义的名字必须落在其内——NewRegistry 校验）。
//
// ⚠️ D5 协议变更记录（用户批准 2026-10-15，落地 2026-10-16；蓝图 §6
// 「协议变更纪律」）：
// FactorMarketActivity 是 D3 之后**第一个新增名册名**。此前名册是冻结面
// （蓝图 §5.5 / D8 记录「不动态新增因子名」指运行期动态新增；正式协议
// 变更走评审）。D5 是「挂新数据 → 可挖因子」全链路样板：不新开名册名，
// 新数据源的公式因子无处登记（size/volatility/growth 三个 pending 名
// 语义上都不接受活跃度数据，硬塞等于发明数学定义）。故按蓝图 §6 协议
// 变更纪律走正式变更：加名 + 种子定义带 D3 十字段骨架 + D4 登记四样
// （公式/哈希/登记人/生效日）+ 蓝图与 SPEC 同步留痕。后续新因子沿用
// 本路径，不放宽任何校验。

import (
	"encoding/json"
	"time"
)

type FactorType string

const (
	FactorMomentum   FactorType = "momentum"
	FactorValue      FactorType = "value"
	FactorQuality    FactorType = "quality"
	FactorSize       FactorType = "size"
	FactorVolatility FactorType = "volatility"
	FactorGrowth     FactorType = "growth"

	// 桥 B1 纵向基本面因子（ADR-022 计算面，EQD-P1-2）。
	// 名字逐字对齐 docs/RESEARCH.md §3.4，最长 24 字符（见迁移 026 的列宽放宽）。
	FactorGrossMarginTrend       FactorType = "gross_margin_trend"
	FactorContractLiabilityRatio FactorType = "contract_liability_ratio"
	FactorOCFToNetProfit         FactorType = "ocf_to_net_profit"
	FactorROEDuPontLeverage      FactorType = "roe_dupont_leverage"
	FactorInventoryTurnoverDelta FactorType = "inventory_turnover_delta"

	// FactorMarketActivity 市场活跃度因子（D5 新数据源样板，2026-10-16
	// 协议变更）。引用 market_activity 表（daily_basic 换手率族）的
	// 公式因子：turnover_rate / ts_mean(turnover_rate, 20) - 1
	// （相对自身近期历史的异常活跃度）。是本项目「挂新数据 → 可挖
	// 因子」全链路（蓝图 §4.4）的第一个样板因子，也是 D3 之后第一个
	// 走正式协议变更新增的名册名（变更记录见本文件头）。
	FactorMarketActivity FactorType = "market_activity"
)

func ParseFactorType(s string) (FactorType, bool) {
	switch s {
	case string(FactorMomentum):
		return FactorMomentum, true
	case string(FactorValue):
		return FactorValue, true
	case string(FactorQuality):
		return FactorQuality, true
	case string(FactorSize):
		return FactorSize, true
	case string(FactorVolatility):
		return FactorVolatility, true
	case string(FactorGrowth):
		return FactorGrowth, true
	case string(FactorGrossMarginTrend):
		return FactorGrossMarginTrend, true
	case string(FactorContractLiabilityRatio):
		return FactorContractLiabilityRatio, true
	case string(FactorOCFToNetProfit):
		return FactorOCFToNetProfit, true
	case string(FactorROEDuPontLeverage):
		return FactorROEDuPontLeverage, true
	case string(FactorInventoryTurnoverDelta):
		return FactorInventoryTurnoverDelta, true
	case string(FactorMarketActivity):
		return FactorMarketActivity, true
	default:
		return "", false
	}
}

// 因子假设的来源种类（P2-3）。
//
// 区分它们的意义：同样是"一个能算出正 IC 的因子"，来自文献的和在数据上
// 挖出来的，可信度不是一个量级。前者有先验的因果机制，后者很可能是噪声
// 的形状 —— 换个时间段就散了。
const (
	// FactorSourceLiterature：有公开文献支撑（学术因子）。
	FactorSourceLiterature = "literature"
	// FactorSourceSupplyChain：来自产业链逻辑推导（桥 B1 纵向因子，
	// 见 ADR-022 / docs/RESEARCH.md §3.4）。
	FactorSourceSupplyChain = "supply_chain"
	// FactorSourceAI：AI 提出的假设（Hermes 挖掘链路）。
	// 这一类必须通过可证伪预测的检验才可用 —— 见 P2-9f 因果维。
	FactorSourceAI = "ai_hypothesis"
	// FactorSourceAdHoc：临时/未记录来源。能用，但别太当真。
	FactorSourceAdHoc = "ad_hoc"
)

// FactorHypothesis 记录一个因子"为什么该有效"。
//
// P2-3：因子的数值在 factor_cache 里，但它的**假设**此前没有地方记。
// 没有这条记录，就无法区分「有经济学依据」和「数据挖掘挖出来的」。
type FactorHypothesis struct {
	FactorName FactorType `json:"factor_name"`
	SourceKind string     `json:"source_kind"`
	// Hypothesis 是一句话的机制陈述：为什么这个因子应该带来超额收益。
	Hypothesis string `json:"hypothesis"`
	// Reference 是出处（文献 / 设计文档章节）。可为空 —— 没写就是没写，
	// 不填一个看起来像出处的占位。
	Reference string `json:"reference,omitempty"`
}

// BuiltinFactorHypotheses 是内置因子的假设来源。
//
// 这些不是编出来的：前六个是公开学术因子，后五个（桥 B1）出自
// ADR-022 的产业链推导，逐字对齐 docs/RESEARCH.md §3.4。
var BuiltinFactorHypotheses = []FactorHypothesis{
	{
		FactorName: FactorMomentum,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "过去一段时间涨得好的股票在短期内继续跑赢：投资者的反应不足与" +
			"羊群效应使价格趋势具有惯性。",
		Reference: "Jegadeesh & Titman (1993)",
	},
	{
		FactorName: FactorValue,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "估值便宜（低 PE/PB）的股票长期回报更高：市场为" +
			"基本面风险与情绪定价过度，便宜本身是一种风险补偿。",
		Reference: "Fama & French (1992, 1993)",
	},
	{
		FactorName: FactorQuality,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "高 ROE / 高盈利质量公司的超额利润更具持续性，市场" +
			"对盈利质量的定价存在滞后。",
		Reference: "Piotroski (2000) F-Score；Novy-Marx (2013) 质量因子",
	},
	{
		FactorName: FactorSize,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "小市值公司承担更高的流动性与破产风险，需要更高的预期回报补偿。",
		Reference:  "Banz (1981)；Fama & French (1993) SMB",
	},
	{
		FactorName: FactorVolatility,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "低波动股票的回报不低（低波异象）：受约束的投资者追逐高波动" +
			"博弈性资产，压低了低波股票的相对价格。",
		Reference: "Ang et al. (2006, 2009) 低波异象",
	},
	{
		FactorName: FactorGrowth,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "营收/利润持续高增长反映未被定价的竞争优势，市场外推不足。",
		Reference:  "Lakonishok, Shleifer & Vishny (1994)",
	},
	{
		FactorName: FactorGrossMarginTrend,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "毛利率抬升说明公司在产业链上的议价能力变强（能提价或压低" +
			"上游成本），这种优势变化领先于财报总量。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorContractLiabilityRatio,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "合同负债（预收）占营收上升意味着下游需求真实且提前锁单，" +
			"是订单可见度的领先指标。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorOCFToNetProfit,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "经营现金流与净利润的比值低说明利润没有变成现金，" +
			"利润质量差；持续背离往往先于基本面恶化。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorROEDuPontLeverage,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "把 ROE 拆成周转率、净利率、杠杆三项，能分辨高 ROE 是来自" +
			"经营效率还是单纯加杠杆 —— 后者不可持续。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorInventoryTurnoverDelta,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "存货周转率变化反映供需格局：周转加快说明需求走强或" +
			"库存去化顺利，是产业链景气度的早期信号。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorMarketActivity,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "高换手（高关注度）的股票吸引更多注意力驱动交易：个人投资者" +
			"的买入决策受关注度排序影响而非估值排序，注意力冲击带来持续的价格" +
			"压力与随后的关注度衰减，异常活跃度相对历史均值的变化是可交易的" +
			"关注度信号。",
		Reference: "Barber & Odean (2008) All That Glitters；D5 样板（daily_basic 换手率族）",
	},
}

// BuiltinHypothesisFor 查内置因子的假设来源。
//
// 找不到时返回 ok=false —— 调用方应当据此如实上报"来源未记录"，
// 而不是编一个。
func BuiltinHypothesisFor(ft FactorType) (FactorHypothesis, bool) {
	for _, h := range BuiltinFactorHypotheses {
		if h.FactorName == ft {
			return h, true
		}
	}
	return FactorHypothesis{}, false
}

type FactorCacheEntry struct {
	ID         int64      `json:"id"`
	Symbol     string     `json:"symbol"`
	TradeDate  time.Time  `json:"trade_date"`
	FactorName FactorType `json:"factor_name"`
	RawValue   float64    `json:"raw_value"`
	ZScore     float64    `json:"z_score"`
	Percentile float64    `json:"percentile"`
	// Citation is the stored provenance of this row's source batches, of the
	// form [{"content_hash":"<64hex>"}]. It is deliberately json.RawMessage:
	// the stored (hash-only) shape and the output shape (ADR-022 §5 five-tuple,
	// expanded by the handler) differ, and the stored form must stay losslessly
	// upgradable. Empty citation is '[]' — "no A→B chain established yet",
	// never null (ODR-061).
	Citation json.RawMessage `json:"citation,omitempty"`
}

type FactorReturn struct {
	ID               int64      `json:"id"`
	FactorName       FactorType `json:"factor_name"`
	TradeDate        time.Time  `json:"trade_date"`
	Quintile         int        `json:"quintile"`
	AvgReturn        float64    `json:"avg_return"`
	CumulativeReturn float64    `json:"cumulative_return"`
	TopMinusBot      float64    `json:"top_minus_bot"`
}

type ICEntry struct {
	ID         int64      `json:"id"`
	FactorName FactorType `json:"factor_name"`
	TradeDate  time.Time  `json:"trade_date"`
	IC         float64    `json:"ic"`
	PValue     float64    `json:"p_value"`
	TopIC      float64    `json:"top_ic"`
}
