package domain

// BuiltinFactorDefinitions 是 11 个冻结因子的库里公式定义种子（D3）。
//
// 分型依据（D3 ③3 判据）：
//
//   - momentum / value：公式型——DSL 现成（ts_delay + 算术）；
//   - quality：叶子型——字段直取；
//   - 纵向 5 因子：代码型——单季差分 / TTM 桥接 / 跨期趋势需要财报期
//     坐标（periodOf/shiftQuarters/ttm），表达引擎的求值模型是日频数字
//     序列，没有「季」这个维度，硬凑公式必然改变数学定义 → 保留 Go 出口；
//   - size / volatility / growth：pending 型——冻结名册里有名字，但计算
//     线从未实现（全仓无 Compute* 出口），没有可公式化的现算数值。
//     D3 不发明数学定义（超出「不改任何因子数学定义」的边界），登记
//     为「名册在册、内核待定」，查询 fail-closed。
//
// ⚠️ 改这里 = 改因子定义 = 所有引用方的版本指纹全变（VersionHash 由
// 内容派生）。这是有意的：定义变更必须可见、可追溯，不允许静默改公式。
//
// InputFields 里的财报字段码是 contracts/field_dictionary.yaml 的契约
// 常量（与 pkg/data/equitydeep.FieldX 同值——domain 不能 import 该包，
// 由其反向依赖；一致性由 pkg/data 的种子词表测试钉住）。
var BuiltinFactorDefinitions = []FactorDefinition{
	{
		Name:          FactorMomentum,
		Kernel:        KernelFormula,
		Formula:       "close / ts_delay(close, 20) - 1",
		Validity:      "close > 0 and ts_delay(close, 20) > 0",
		Caliber:       CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      30,
	},
	{
		Name:          FactorValue,
		Kernel:        KernelFormula,
		Formula:       "1 / pe",
		Validity:      "pe > 0",
		Caliber:       CaliberZScore,
		InputFields:   []string{"pe"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      30,
	},
	{
		Name:          FactorQuality,
		Kernel:        KernelLeaf,
		Formula:       "roe",
		Validity:      "roe > 0",
		Caliber:       CaliberZScore,
		InputFields:   []string{"roe"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      30,
	},
	{
		Name:          FactorGrossMarginTrend,
		Kernel:        KernelCode,
		CodeRef:       "ComputeGrossMarginTrendFactor",
		Caliber:       CaliberZScore,
		InputFields:   []string{"total_revenue", "operating_cost"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	},
	{
		Name:          FactorContractLiabilityRatio,
		Kernel:        KernelCode,
		CodeRef:       "ComputeContractLiabilityRatioFactor",
		Caliber:       CaliberZScore,
		InputFields:   []string{"contract_liability", "total_revenue"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	},
	{
		Name:          FactorOCFToNetProfit,
		Kernel:        KernelCode,
		CodeRef:       "ComputeOCFToNetProfitFactor",
		Caliber:       CaliberZScore,
		InputFields:   []string{"ocf_net", "net_profit_attr"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	},
	{
		Name:          FactorROEDuPontLeverage,
		Kernel:        KernelCode,
		CodeRef:       "ComputeROEDuPontLeverageFactor",
		Caliber:       CaliberZScore,
		InputFields:   []string{"total_assets", "equity_attr"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	},
	{
		Name:          FactorInventoryTurnoverDelta,
		Kernel:        KernelCode,
		CodeRef:       "ComputeInventoryTurnoverDeltaFactor",
		Caliber:       CaliberZScore,
		InputFields:   []string{"inventory", "operating_cost"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	},
	{
		Name:          FactorSize,
		Kernel:        KernelPending,
		Caliber:       CaliberZScore,
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	},
	{
		Name:          FactorVolatility,
		Kernel:        KernelPending,
		Caliber:       CaliberZScore,
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	},
	{
		Name:          FactorGrowth,
		Kernel:        KernelPending,
		Caliber:       CaliberZScore,
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	},
}
