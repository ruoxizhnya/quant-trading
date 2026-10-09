// 算子注册表 —— 表达式引擎里「有哪些算子」的**单一事实源**。
//
// ─── 为什么要有这个文件 ──────────────────────────────────────────────
//
// 在本次收敛之前，「算子名合法集合」散落在 5 处、彼此独立、会漂移：
//   - ast.go 的 IsTimeSeriesOp / IsCrossSectionalOp / IsMathOp（三个硬编码 slice）
//   - parser.go:318 用 IsCrossSectionalOp 决定 CS 节点（外加 name=="cs_neutralize"
//     这一处硬编码 arity）
//   - evaluator.go 的 applyTimeSeriesOp（switch + default 报错）
//   - evaluator.go 的 applyCrossSectionalOp（switch + **default 静默直通**）
//   - evaluator.go 的 isUnaryOp（switch）
//
// 收敛成一个注册表后：名字/类别/参数个数/声明/求值绑定只在**这里**写一遍，
// 其余各处一律查表。OBS-06（L1 闸门不校验算子名，`CROSS(MA(...))` 假合法）
// 正是「名字合法集合没有单一事实源」的直接后果之一。
//
// ─── 表里有什么 ──────────────────────────────────────────────────────
//
//   - 时间序列 ts_*（13 个）= 10 个存量 FIR 算子 + 3 个 L2 递推算子（K3 切片 2）
//   - 横截面 cs_*（4 个）
//   - 一元（6 个）：neg / abs / log / sqrt / sign / exp
//   - 二元（8 个）：+ - * / ^ > < ==
//
// 每个算子一条 OperatorDef：名字 / 类别 / 参数个数 / indicator.OperatorSpec
// （ADR-028 §4 七项）/ 求值绑定。参数化算子的 Lookback / Warmup 是「参数的
// 函数」，扁平结构体装不下 —— 由 selfLookback / selfWarmup 按实参推导，
// Spec 里存 0 哨兵（详见 OperatorDef 注释）。
//
// ─── 防漂移护栏 ──────────────────────────────────────────────────────
//
// registry_test.go 里有两道护栏：
//  1. 自洽：遍历本表，每个算子都能被 parser 解析 + 过 Validate 闸门 +
//     名字出现在 AvailableOperators()；
//  2. 反向：扫本包（registry.go 之外、非 _test.go）的字符串字面量，
//     任何以 "ts_" / "cs_" 开头的算子名都算违约 —— 名字只能在 registry.go。
package expression

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// OpCategory 是算子的语法类别，决定 parser/evaluator 如何承载与求值。
type OpCategory string

const (
	// CatTimeSeries 时序算子：进整条序列、出整条序列（FIR 或 L2 递推）。
	CatTimeSeries OpCategory = "ts"
	// CatCrossSectional 横截面算子：同一天跨标量求值。
	CatCrossSectional OpCategory = "cs"
	// CatUnary 一元逐元素算子（不进序列窗口，逐点变换）。
	CatUnary OpCategory = "unary"
	// CatBinary 二元逐元素算子（+, -, *, /, ^, >, <, ==）。
	CatBinary OpCategory = "binary"
)

// tsEvalFunc 是时序算子的求值绑定：参数已按 symbol 取出的整条序列。
//
// **性能约束（勿违反）**：对每个 symbol，整条序列一次算完再按下标取；
// **禁止每根 bar 重算整条序列**。L2 递推算子（ts_rma/ts_ewma/ts_kalman）
// 尤其如此 —— 它们复用 indicator 包的 Batch 实现（单遍递推），若在
// evaluator 里对每根 bar 重跑 Batch，复杂度从 O(L) 退化成 O(L²)，且会写出
// 第二份递推（违反 ADR-028 §7 的双实现共享核）。
type tsEvalFunc func(args [][]float64) ([]float64, error)

// csEvalFunc 是横截面算子的求值绑定（当日截面 values + 可选分组 group）。
type csEvalFunc func(values, group []float64) []float64

// unaryEvalFunc 是一元算子的求值绑定（逐点）。
type unaryEvalFunc func(v float64) float64

// OperatorDef 是一个算子的完整声明——ADR-028 §4 的 7 项 + 三个语法维度
// （名字 / 类别 / 参数个数）+ 求值绑定。
//
// 关于 Spec.Lookback / Spec.Warmup 对**参数化算子**取 0 的说明：
// ADR-028 §4 的 lookback / warmup 列填的是**规则**（`ts_mean(x,N)` 的
// lookback=N−1、warmup=N−1；`ts_ewma(x,α)` 的 warmup=ceil(ln(1e-6)/ln(1-α))），
// 扁平 int 字段装不下。这与本次 `Init float64→string` 改型是同一类问题；
// 由于本切片只裁决了 Init 改型，Lookback/Warmup 仍为 int，故参数化算子在其
// Spec 里存 0 哨兵，真实值由 selfLookback / selfWarmup 按实参推导（并被
// DeriveWarmup / DeriveLookback 使用）。非参数化算子的这两个字段就是真值 0。
type OperatorDef struct {
	Name     string
	Category OpCategory
	Arity    int
	Spec     indicator.OperatorSpec

	// ScalarParams 声明哪些参数位必须是**标量**——即不依赖任何序列数据的
	// 常量表达式。索引从 0 起；nil 表示该算子没有标量参数位。
	//
	// ─── 为什么要有这个字段（K3a）───────────────────────────────────
	// ADR-028 §4 的签名（如 `ts_mean : Series × Scalar → Series`）此前只是
	// Spec.Signature 里的一句**字符串**，闸门不校验它 ⇒ `ts_mean(close,
	// volume)` 参数个数与节点形态都合法、能过闸，但求值时 firstScalar 取到
	// 序列首值（成交量）或 NaN ⇒ `int(NaN)` 未定义 ⇒ 窗口荒谬 ⇒ **整条序列
	// NaN**。这是「假合法残留」：危害被 OBS-01 兜住（判无效运行，不产出假
	// 结论），但白白浪费一次 AI 试验，且报错信息毫无指向性。
	//
	// 把标量位声明成字段后，闸门能在**解析期**把它拦住，报错直接点名参数位。
	//
	// ─── 裁决：要求是「常量表达式」而不是「字面量」─────────────────
	// 严格只认 *LiteralNode 会误拒合法的常量写法（例如用 `1/20` 表达 EWMA
	// 衰减率 α）—— 误拒比假合法更危险（K3c 教训）。故判定标准为「子树不引用
	// 任何序列数据」：字面量、纯常量的 + - * / ^ 与一元变换都算标量；
	// 出现 IdentifierNode / 时序算子 / 横截面算子则不算。见 isConstantExpr。
	ScalarParams []int

	// selfWarmup / selfLookback：本算子**自身**对 warmup / lookback 的贡献，
	// 按实参推导（args 为该算子的参数 AST）。nil ⇒ 贡献 0。
	//
	// warmup(f(args)) = max_i warmup(args_i) + selfWarmup；并行取 max、串行累加
	// （ADR-028 §8）。lookback 同构，且任一节点 State=true ⇒ ∞。
	selfWarmup   func(args []Node) int
	selfLookback func(args []Node) int

	tsEval    tsEvalFunc
	csEval    csEvalFunc
	unaryEval unaryEvalFunc
}

// ─── 数据字段注册表（IdentifierNode 的单一事实源） ─────────────────────
//
// 停牌/缺失语义由 SeriesSpec.nan_policy 声明（ADR-028 §5/§9）；这里只负责
// 「这个名字是不是已知字段」，并声明它属于哪类**数据来源**（FieldSource）。
//
// ─── 为什么带来源 ─────────────────────────────────────────────────────
//
// 此前字段白名单是一张扁平的 `map[string]bool`（OBS-08 前的病灶）：它既
// 不知道字段从哪来，也没跟任何 provider 对齐过，于是放行了 6 个 provider
// 永远求不出的字段（`market_cap`/`roe_ttm`/`eps`/… = 假合法），又拦掉了
// provider 真正支持的 2 个（`ps`/`roa` = 误拒）。带来源后：
//   - 每条字段声明它属于 market / fundamentals / group 中的哪一类；
//   - provider 侧用 Fields() 自报能供应的字段，护栏把「注册表里来源非
//     group 的字段集合」与「provider 能供应的字段集合」钉成双向相等。
//
// ─── 三类来源 ─────────────────────────────────────────────────────────
//
//   - market：来自行情（OHLCV）—— open/high/low/close/volume/turnover；
//   - fundamentals：来自财报（domain.Fundamental / PIT 对齐）；
//   - group：**不是数据字段**，是 `cs_neutralize(x, group)` 的分组标签。
//     `sector` 是唯一一个，它依赖 storage 的 stock_sector_map（当前 0 行，
//     可用性声明属 OBS-08 切片 2，本切片只把它从「数据字段」正名为「分组
//     标签」并保留其语法合法性）。
//
// ⚠️ 曾登记但被移除（OBS-08 切片 1，④类）：`market_cap` / `roe_ttm` /
// `eps` —— `domain.Fundamental` 里根本没有这三个字段，provider 永远求不出
// 值，留在闸门里就是「假合法」，只会让 AI 反复撞墙。**若将来 domain 补上
// 对应字段（并在 provider 的 fundamentalValue/Fields 里实现），重新加入即可。**
type FieldSource string

const (
	// FieldSourceMarket 行情来源（OHLCV）。
	FieldSourceMarket FieldSource = "market"
	// FieldSourceFundamentals 财报来源（domain.Fundamental，PIT 对齐）。
	FieldSourceFundamentals FieldSource = "fundamentals"
	// FieldSourceGroup 横截面分组标签（cs_neutralize 的 group 参数）——不是数据字段。
	FieldSourceGroup FieldSource = "group"
)

// ─── 物理数据表（可用性探测的对象）───────────────────────────────────
//
// 集中成常量：这些表名同时被 fieldRegistry 的 DependsOn、可用性探测与护栏
// 引用，散落成字符串迟早拼错（拼错 = 探测一张不存在的表 = 永远「不可用」）。
const (
	// TableOHLCV 日线行情（前复权）——本项目唯一有大量数据的表。
	TableOHLCV = "ohlcv_daily_qfq"
	// TableFundamentals 财报（PIT 对齐）。
	TableFundamentals = "stock_fundamentals"
	// TableSectorMap 个股 → 板块映射（cs_neutralize 的 sector 分组标签来源）。
	TableSectorMap = "stock_sector_map"
)

// FieldDef 是一个字段的完整声明：来源类别 + 依赖的物理数据表。
//
// ─── 为什么要有 DependsOn（OBS-08 切片 2）───────────────────────────
// OBS-08 切片 1 把字段白名单与 **provider 能力**对齐了（provider 代码认不认
// 这个字段）。但「provider 认」不等于「表里有数据」：实测
// `stock_fundamentals` 与 `stock_sector_map` 都是 **0 行**（免费 Tushare
// 额度拉不到），于是 pe/pb/ps/roe/roa/revenue/profit/sector 这 8 个字段
// 语法合法、过闸门、provider 也认，**但求值必然拿不到数据** —— AI 对着空
// 数据静默产垃圾（危害被 OBS-01 兜住，但白白浪费一轮试验）。
//
// 故字段声明要再往下走一层：从「能力」到「可用性」。DependsOn 指明该字段的
// 数据来自哪张物理表，可用性探测据此判定「当前有没有数据」。
type FieldDef struct {
	// Source 是来源类别（market / fundamentals / group）。
	Source FieldSource
	// DependsOn 是该字段的数据来源表（可用性探测的对象）。空串 = 无物理表
	// 依赖（纯派生字段，恒可用）。
	DependsOn string
}

// fieldRegistry 是字段名合法集合的**唯一权威来源**：字段 → 声明。
var fieldRegistry = map[string]FieldDef{
	// 行情
	"open":     {Source: FieldSourceMarket, DependsOn: TableOHLCV},
	"high":     {Source: FieldSourceMarket, DependsOn: TableOHLCV},
	"low":      {Source: FieldSourceMarket, DependsOn: TableOHLCV},
	"close":    {Source: FieldSourceMarket, DependsOn: TableOHLCV},
	"volume":   {Source: FieldSourceMarket, DependsOn: TableOHLCV},
	"turnover": {Source: FieldSourceMarket, DependsOn: TableOHLCV},
	// 基本面（对应 domain.Fundamental 的 PE/PB/PS/ROE/ROA/Revenue/NetProfit）
	"pe":      {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	"pb":      {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	"ps":      {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	"roe":     {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	"roa":     {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	"revenue": {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	"profit":  {Source: FieldSourceFundamentals, DependsOn: TableFundamentals},
	// 横截面分组标签（cs_neutralize 的 group 参数）——不是数据字段
	"sector": {Source: FieldSourceGroup, DependsOn: TableSectorMap},
}

// operatorRegistry 是唯一的名字合法集合。map 便于 O(1) 查表；
// AvailableOperators() 排序后输出以保证确定性。
var operatorRegistry = map[string]OperatorDef{
	// ══ 时序算子（存量 FIR，stateless，causal，Lookback/Warmup 由参数决定）══
	"ts_mean": {
		Name: "ts_mean", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_mean", "ts_mean : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsMean(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_std": {
		Name: "ts_std", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_std", "ts_std : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsStd(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_sum": {
		Name: "ts_sum", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_sum", "ts_sum : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsSum(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_max": {
		Name: "ts_max", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_max", "ts_max : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsMax(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_min": {
		Name: "ts_min", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_min", "ts_min : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsMin(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_delay": {
		Name: "ts_delay", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_delay", "ts_delay : Series × Scalar → Series"),
		selfWarmup:   periodSelfWarmup,
		selfLookback: periodSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsDelay(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_delta": {
		Name: "ts_delta", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_delta", "ts_delta : Series × Scalar → Series"),
		selfWarmup:   periodSelfWarmup,
		selfLookback: periodSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsDelta(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_pct_change": {
		Name: "ts_pct_change", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_pct_change", "ts_pct_change : Series × Scalar → Series"),
		selfWarmup:   periodSelfWarmup,
		selfLookback: periodSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsPctChange(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_corr": {
		Name: "ts_corr", Category: CatTimeSeries, Arity: 3,
		ScalarParams: []int{2},
		Spec:         seriesStatelessSpec("ts_corr", "ts_corr : Series × Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmupAt2,
		selfLookback: windowSelfLookbackAt2,
		tsEval: func(args [][]float64) ([]float64, error) {
			return tsCorr(args[0], args[1], int(firstScalar(args[2]))), nil
		},
	},
	"ts_rank": {
		Name: "ts_rank", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         seriesStatelessSpec("ts_rank", "ts_rank : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsRank(args[0], int(firstScalar(args[1]))), nil },
	},

	// ══ L2 递推算子（K3 切片 2；stateful，causal，Lookback=∞ 走 State 约定）══
	//
	// 求值一律复用 pkg/indicator 的 Batch 实现（单遍递推、与 Step 共享核）——
	// 不在 expression 里写第二份递推（ADR-028 §7 双实现共享核）。
	"ts_rma": {
		Name: "ts_rma", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         recursiveSpec("ts_rma", "ts_rma : Series × Scalar → Series", "sma(first,N)"),
		selfWarmup:   rmaSelfWarmup,
		selfLookback: stateSelfLookback, // ∞（0 + State=true）
		tsEval: func(args [][]float64) ([]float64, error) {
			return indicator.RMABatch(args[0], int(firstScalar(args[1])))
		},
	},
	"ts_ewma": {
		Name: "ts_ewma", Category: CatTimeSeries, Arity: 2,
		ScalarParams: []int{1},
		Spec:         recursiveSpec("ts_ewma", "ts_ewma : Series × Scalar → Series", "x[0]"),
		selfWarmup:   ewmaSelfWarmup,
		selfLookback: stateSelfLookback, // ∞
		tsEval: func(args [][]float64) ([]float64, error) {
			return indicator.EWMABatch(args[0], firstScalar(args[1]))
		},
	},
	"ts_kalman": {
		Name: "ts_kalman", Category: CatTimeSeries, Arity: 3,
		ScalarParams: []int{1, 2},
		Spec:         recursiveSpec("ts_kalman", "ts_kalman : Series × Scalar × Scalar → Series", "p0=r"),
		selfWarmup:   kalmanSelfWarmup,
		selfLookback: stateSelfLookback, // ∞
		tsEval: func(args [][]float64) ([]float64, error) {
			return indicator.KalmanBatch(args[0], firstScalar(args[1]), firstScalar(args[2]))
		},
	},

	// ══ 横截面算子（同日截面，lookback=0）══
	"cs_rank": {
		Name: "cs_rank", Category: CatCrossSectional, Arity: 1,
		Spec:   crossSectionalSpec("cs_rank", "cs_rank : Series → Series"),
		csEval: func(values, _ []float64) []float64 { return csRank(values) },
	},
	"cs_zscore": {
		Name: "cs_zscore", Category: CatCrossSectional, Arity: 1,
		Spec:   crossSectionalSpec("cs_zscore", "cs_zscore : Series → Series"),
		csEval: func(values, _ []float64) []float64 { return csZScore(values) },
	},
	"cs_percentile": {
		Name: "cs_percentile", Category: CatCrossSectional, Arity: 1,
		Spec:   crossSectionalSpec("cs_percentile", "cs_percentile : Series → Series"),
		csEval: func(values, _ []float64) []float64 { return csPercentile(values) },
	},
	"cs_neutralize": {
		Name: "cs_neutralize", Category: CatCrossSectional, Arity: 2,
		Spec:   crossSectionalSpec("cs_neutralize", "cs_neutralize : Series × Series → Series"),
		csEval: func(values, group []float64) []float64 { return csNeutralize(values, group) },
	},

	// ══ 一元逐元素算子（lookback=0）══
	"neg":  unaryDef("neg"),
	"abs":  unaryDef("abs"),
	"log":  unaryDef("log"),
	"sqrt": unaryDef("sqrt"),
	"sign": unaryDef("sign"),
	"exp":  unaryDef("exp"),

	// ══ 二元逐元素算子（lookback=0）══
	//
	// 求值绑定在 evaluator.go 的 applyBinaryOp（按符号 switch）；此处只登记
	// 名字/类别/参数个数/声明，供 parser 与 Validate 闸门查表。
	"+":  binaryDef("+"),
	"-":  binaryDef("-"),
	"*":  binaryDef("*"),
	"/":  binaryDef("/"),
	"^":  binaryDef("^"),
	">":  binaryDef(">"),
	"<":  binaryDef("<"),
	"==": binaryDef("=="),
}

// ─── Spec 构造助手 ────────────────────────────────────────────────────

// seriesStatelessSpec 构造时序**无状态**算子的声明（Lookback/Warmup 参数化，
// 存 0 哨兵；Causal=true、State=false、Init=none）。
func seriesStatelessSpec(name, signature string) indicator.OperatorSpec {
	return indicator.OperatorSpec{
		Name:      name,
		Signature: signature,
		Lookback:  0, // 参数化（N−1 或 d）；见 selfLookback
		Causal:    true,
		State:     false,
		Warmup:    0, // 参数化；见 selfWarmup
		Init:      "none",
		NaNPolicy: "propagate",
	}
}

// crossSectionalSpec 构造横截面算子的声明。
func crossSectionalSpec(name, signature string) indicator.OperatorSpec {
	return indicator.OperatorSpec{
		Name: name, Signature: signature,
		Lookback: 0, Causal: true, State: false, Warmup: 0,
		Init: "none", NaNPolicy: "propagate",
	}
}

// recursiveSpec 构造 L2 递推算子的声明：State=true、Lookback=∞（0 约定）、
// Init 走受控词表（由调用方给出）。
func recursiveSpec(name, signature, init string) indicator.OperatorSpec {
	return indicator.OperatorSpec{
		Name: name, Signature: signature,
		Lookback: 0, // ∞：递推状态表达（0 + State=true）
		Causal:   true,
		State:    true,
		Warmup:   0, // 参数化；由实例 Spec() 或 selfWarmup 给出
		Init:     init,
		// Batch 未预热位用 NaN 表达 unknown（与 Step 的 Value() error 对偶）。
		NaNPolicy: "unknown→NaN",
	}
}

// unaryDef 构造一元算子的完整定义（求值走 applyUnaryOp）。
func unaryDef(name string) OperatorDef {
	return OperatorDef{
		Name: name, Category: CatUnary, Arity: 1,
		Spec: indicator.OperatorSpec{
			Name: name, Signature: name + " : Series → Series",
			Lookback: 0, Causal: true, State: false, Warmup: 0,
			Init: "none", NaNPolicy: "propagate",
		},
		unaryEval: func(v float64) float64 { return applyUnaryOp(name, v) },
	}
}

// binaryDef 构造二元算子的完整定义（求值走 applyBinaryOp）。
func binaryDef(op string) OperatorDef {
	return OperatorDef{
		Name: op, Category: CatBinary, Arity: 2,
		Spec: indicator.OperatorSpec{
			Name: op, Signature: op + " : Series × Series → Series",
			Lookback: 0, Causal: true, State: false, Warmup: 0,
			Init: "none", NaNPolicy: "propagate",
		},
	}
}

// ─── 参数化算子的 self 贡献推导 ───────────────────────────────────────

// windowSelfWarmup：滑动窗口算子（窗口在 args[1]）自身 warmup = N−1。
func windowSelfWarmup(args []Node) int { return windowSelfWarmupAt(args, 1) }

// windowSelfLookback：滑动窗口算子自身 lookback = N−1。
func windowSelfLookback(args []Node) int { return windowSelfLookbackAt(args, 1) }

// windowSelfWarmupAt2 / windowSelfLookbackAt2：窗口在 args[2]（ts_corr）。
func windowSelfWarmupAt2(args []Node) int   { return windowSelfWarmupAt(args, 2) }
func windowSelfLookbackAt2(args []Node) int { return windowSelfLookbackAt(args, 2) }

func windowSelfWarmupAt(args []Node, idx int) int {
	n, ok := literalIntAt(args, idx)
	if !ok || n < 1 {
		return 0
	}
	return n - 1
}

func windowSelfLookbackAt(args []Node, idx int) int {
	n, ok := literalIntAt(args, idx)
	if !ok || n < 1 {
		return 0
	}
	return n - 1
}

// periodSelfWarmup / periodSelfLookback：滞后 d 期算子自身 warmup = lookback = d。
func periodSelfWarmup(args []Node) int   { return periodSelfAt(args, 1) }
func periodSelfLookback(args []Node) int { return periodSelfAt(args, 1) }

func periodSelfAt(args []Node, idx int) int {
	d, ok := literalIntAt(args, idx)
	if !ok || d < 0 {
		return 0
	}
	return d
}

// rmaSelfWarmup：ts_rma(x,N) 自身 warmup = N（实例给出，复用 indicator 真值）。
func rmaSelfWarmup(args []Node) int {
	n, ok := literalIntAt(args, 1)
	if !ok {
		return 0
	}
	ind, err := indicator.NewRMA(n)
	if err != nil {
		return 0
	}
	return ind.Warmup()
}

// ewmaSelfWarmup：ts_ewma(x,α) 自身 warmup = ceil(ln(1e-6)/ln(1-α))（实例给出）。
func ewmaSelfWarmup(args []Node) int {
	alpha, ok := literalFloatAt(args, 1)
	if !ok {
		return 0
	}
	ind, err := indicator.NewEWMA(alpha)
	if err != nil {
		return 0
	}
	return ind.Warmup()
}

// kalmanSelfWarmup：ts_kalman 自身 warmup = 1。
func kalmanSelfWarmup(args []Node) int {
	ind, err := indicator.NewKalman(0, 1)
	if err != nil {
		return 0
	}
	return ind.Warmup()
}

// stateSelfLookback：递推算子的自身 lookback 贡献为 0 —— ∞ 由 Spec.State
// 触发 DeriveLookback 的 infinite 标记（K0 约定）。
func stateSelfLookback(args []Node) int { return 0 }

// literalIntAt / literalFloatAt 读第 idx 个实参的常量字面量值。
func literalIntAt(args []Node, idx int) (int, bool) {
	v, ok := literalFloatAt(args, idx)
	if !ok {
		return 0, false
	}
	return int(v), true
}

func literalFloatAt(args []Node, idx int) (float64, bool) {
	if idx < 0 || idx >= len(args) {
		return 0, false
	}
	lit, ok := args[idx].(*LiteralNode)
	if !ok {
		return 0, false
	}
	return lit.Value, true
}

// ─── 查表 API ─────────────────────────────────────────────────────────

// OperatorCategory 返回算子名所属类别；未登记 → (_, false)。
func OperatorCategory(name string) (OpCategory, bool) {
	def, ok := operatorRegistry[name]
	if !ok {
		return "", false
	}
	return def.Category, true
}

// OperatorArity 返回算子名的参数个数；未登记 → 0。
func OperatorArity(name string) int {
	if def, ok := operatorRegistry[name]; ok {
		return def.Arity
	}
	return 0
}

// AvailableOperators 返回全部已登记算子名（升序）——供闸门报错提示与自洽
// 护栏使用。**这是「可用算子」的唯一权威来源**。
func AvailableOperators() []string {
	names := make([]string, 0, len(operatorRegistry))
	for name := range operatorRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AvailableFields 返回全部已知字段名（升序，**含 group 标签**）——即
// 「DSL 里语法合法的字段名」全集。签名与语义与 OBS-08 之前保持一致。
func AvailableFields() []string {
	fields := make([]string, 0, len(fieldRegistry))
	for f := range fieldRegistry {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	return fields
}

// AvailableFieldsInSource 返回某来源下的全部字段名（升序）。未知来源 →
// 空切片。source=FieldSourceGroup 时返回分组标签（如 sector）。
func AvailableFieldsInSource(source FieldSource) []string {
	fields := make([]string, 0)
	for f, s := range fieldRegistry {
		if s.Source == source {
			fields = append(fields, f)
		}
	}
	sort.Strings(fields)
	return fields
}

// AvailableDataFields 返回**真正的数据字段**（升序）——即来源非 group 的
// 全部字段。`sector` 是分组标签，不算数据字段，故被排除。这是「provider
// 应当供应哪些字段」的权威集合，供跨包护栏（pkg/strategy/expression）对齐。
func AvailableDataFields() []string {
	fields := make([]string, 0, len(fieldRegistry))
	for f, s := range fieldRegistry {
		if s.Source != FieldSourceGroup {
			fields = append(fields, f)
		}
	}
	sort.Strings(fields)
	return fields
}

// FieldSourceOf 返回字段名所属来源；未登记 → (_, false)。
func FieldSourceOf(name string) (FieldSource, bool) {
	def, ok := fieldRegistry[name]
	if !ok {
		return "", false
	}
	return def.Source, true
}

// ─── 可用性声明（OBS-08 切片 2）─────────────────────────────────────
//
// 与「能力层」的分工（重要，别混）：
//   - 能力层（切片 1 已做）：provider 代码认不认这个字段 —— `AvailableDataFields()`
//     是这一层的权威集合，被跨包护栏（≡ OHLCVDataProvider.Fields()）钉住；
//   - 可用性层（本切片）：那张表里**当前有没有数据**。
//
// 「认」不等于「有」。实测 stock_fundamentals / stock_sector_map 均为 0 行。
// 所以本层**不改**能力层的任何语义，只在它之上叠加一个运行时判定。

// AvailabilityProbe 探测一张物理表当前是否有数据。
//
// 由**调用方注入**（本包不连数据库 —— 表达式引擎是纯计算层，不该知道 DB）。
// 语义：返回 (有数据, error)。error 表示「探测本身失败」，与「探测成功但没
// 数据」是两回事，前者必须 fail-loud（不能当成「不可用」悄悄降级，否则
// 库挂了会伪装成「字段没数据」）。
type AvailabilityProbe func(ctx context.Context, table string) (bool, error)

// FieldAvailability 按 probe 判定每个字段当前是否有数据。
//
// 返回字段名 → 是否有数据。**只含已登记字段**，未登记字段不在返回里。
// 无依赖表（DependsOn 为空）的字段恒为 true（纯派生，不依赖数据）。
//
// 探测失败（error）→ 整体返回 error，不给部分结果：一份「部分正确」的可用
// 性地图比没有更危险（AI 会相信里面「可用」的那几个）。
func FieldAvailability(ctx context.Context, probe AvailabilityProbe) (map[string]bool, error) {
	if probe == nil {
		return nil, fmt.Errorf("expression: FieldAvailability 的 probe 为 nil（需由调用方注入数据来源探测）")
	}
	// 同表只探一次（一张表通常被多个字段依赖）。
	tableOK := make(map[string]bool)
	out := make(map[string]bool, len(fieldRegistry))
	for field, def := range fieldRegistry {
		if def.DependsOn == "" {
			out[field] = true
			continue
		}
		if ok, cached := tableOK[def.DependsOn]; cached {
			out[field] = ok
			continue
		}
		ok, err := probe(ctx, def.DependsOn)
		if err != nil {
			return nil, fmt.Errorf("expression: 字段 %q 的依赖表 %q 可用性探测失败: %w", field, def.DependsOn, err)
		}
		tableOK[def.DependsOn] = ok
		out[field] = ok
	}
	return out, nil
}

// AvailableDataFieldsWith 在能力层之上叠加可用性：只返回**当前有数据**的
// 数据字段（升序）。
//
// avail 为 nil 时等价于 AvailableDataFields()（不叠加可用性，能力层全集）——
// 调用方拿不到探测结果时的降级行为，**语义明确**（不是「全部可用」，而是
// 「未做可用性过滤」）。调用方应优先传入真实探测结果。
func AvailableDataFieldsWith(avail map[string]bool) []string {
	all := AvailableDataFields()
	if avail == nil {
		return all
	}
	out := make([]string, 0, len(all))
	for _, f := range all {
		if avail[f] {
			out = append(out, f)
		}
	}
	return out
}

// FieldsDependingOn 返回依赖给定物理表的全部字段名（升序）。
//
// 用途：数据补上/清空时，一眼看出影响哪些字段；也是护栏与诊断的入口。
func FieldsDependingOn(table string) []string {
	out := make([]string, 0, len(fieldRegistry))
	for f, def := range fieldRegistry {
		if def.DependsOn == table {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// availableOperatorsHint 生成「（可用算子：a, b, …）」后缀，供闸门/求值报错
// 指明合法集合，便于 AI 自纠（OBS-06 的核心诉求）。
func availableOperatorsHint() string {
	return "（可用算子：" + strings.Join(AvailableOperators(), ", ") + "）"
}

// AvailableFieldsHint 生成「（可用字段：…）」后缀，**按来源分组**列出全部
// 合法字段，供闸门/工具/求值报错指明合法集合，便于 AI 自纠（OBS-08）。
//
// 形如：（可用字段：market: close, high, …；fundamentals: pb, pe, …；group: sector）
func AvailableFieldsHint() string {
	return "（可用字段：" + fieldsBySourceString() + "）"
}

// fieldsBySourceString 把注册表按来源分组渲染成 "market: a, b；fundamentals: …"。
// 来源顺序固定为 market → fundamentals → group，保证输出稳定可断言。
func fieldsBySourceString() string {
	order := []FieldSource{FieldSourceMarket, FieldSourceFundamentals, FieldSourceGroup}
	parts := make([]string, 0, len(order))
	for _, src := range order {
		fields := AvailableFieldsInSource(src)
		if len(fields) == 0 {
			continue
		}
		parts = append(parts, string(src)+": "+strings.Join(fields, ", "))
	}
	return strings.Join(parts, "；")
}

// ─── 语法 + 算子闸门（Expression.Validate 的单一实现） ────────────────
//
// fail-closed：注册表里没有的算子名、数据字段一律不通过。这是修 OBS-06 的
// 核心 —— 之前 Parse 成功即 valid=true，于是 `CROSS(MA(...))`（两个不存在
// 的算子）被判「合法」。闸门同时校验参数个数（`ts_mean(close)` 这类拦截）。

// ValidateNode 递归校验单个 AST 节点（path 是位置面包屑，见下）。
// isConstantExpr 报告一棵子树是否是**标量**（常量表达式）——即不依赖任何
// 序列数据，可以在解析期就折叠成一个数。
//
// ─── 判定（K3a）─────────────────────────────────────────────────────
//   - LiteralNode（裸数字）→ 是；
//   - UnaryOpNode / BinaryOpNode（两侧都是常量）→ 是（允许 `1/20` 这类写法
//     表达衰减率，避免误拒）；
//   - FunctionNode 但算子是**一元逐元素**算子且参数皆常量 → 是（如 `neg(5)`）；
//   - IdentifierNode（数据字段）→ **否**（这是 K3a 要拦的病根：
//     `ts_mean(close, volume)` 的 volume 会被引擎当窗口长度用）；
//   - FunctionNode 且是**时序**算子 → **否**（如 `ts_delay(volume,1)`，
//     它输出的是序列不是标量）；
//   - CrossSectionalNode → **否**（同样输出序列）。
//
// 为什么不是「只认字面量」：只认 *LiteralNode 会误拒 `1/20` 这类合法常量
// 写法——误拒比假合法更危险（K3c 教训：把正经用法挡在外面，比放一条垃圾
// 进来更伤）。判定标准收敛到「是否引用序列数据」这一条语义线上。
func isConstantExpr(n Node) bool {
	switch v := n.(type) {
	case *LiteralNode:
		return true
	case *UnaryOpNode:
		return isConstantExpr(v.Expr)
	case *BinaryOpNode:
		return isConstantExpr(v.Left) && isConstantExpr(v.Right)
	case *FunctionNode:
		def, ok := operatorRegistry[v.Name]
		if !ok || def.Category != CatUnary {
			return false // 时序算子 / 未知算子 ⇒ 产出序列，不是标量
		}
		for _, arg := range v.Args {
			if !isConstantExpr(arg) {
				return false
			}
		}
		return true
	default:
		// IdentifierNode / CrossSectionalNode / 未知节点
		return false
	}
}

// describeNode 给报错用的节点简述（不打印整棵子树，避免报错过长）。
func describeNode(n Node) string {
	switch v := n.(type) {
	case *IdentifierNode:
		return fmt.Sprintf("数据字段 %q（序列，不是标量）", v.Name)
	case *FunctionNode:
		return fmt.Sprintf("算子 %q(...)（产出序列，不是标量）", v.Name)
	case *CrossSectionalNode:
		return fmt.Sprintf("横截面算子 %q(...)（产出序列，不是标量）", v.Op)
	case *LiteralNode:
		return "字面量"
	default:
		return fmt.Sprintf("%T", n)
	}
}

func validateNode(node Node, path string) error {
	switch n := node.(type) {
	case *LiteralNode:
		return nil

	case *IdentifierNode:
		if !IsDataField(n.Name) {
			return fmt.Errorf("%s: unknown data field %q%s", path, n.Name, AvailableFieldsHint())
		}
		return nil

	case *UnaryOpNode:
		def, ok := operatorRegistry[n.Op]
		if !ok || def.Category != CatUnary {
			return fmt.Errorf("%s: unknown unary operator %q%s", path, n.Op, availableOperatorsHint())
		}
		return validateNode(n.Expr, path+".expr")

	case *BinaryOpNode:
		def, ok := operatorRegistry[n.Op]
		if !ok || def.Category != CatBinary {
			return fmt.Errorf("%s: unknown binary operator %q%s", path, n.Op, availableOperatorsHint())
		}
		if err := validateNode(n.Left, path+".left"); err != nil {
			return err
		}
		return validateNode(n.Right, path+".right")

	case *FunctionNode:
		def, ok := operatorRegistry[n.Name]
		if !ok || (def.Category != CatTimeSeries && def.Category != CatUnary) {
			return fmt.Errorf("%s: unknown operator %q%s", path, n.Name, availableOperatorsHint())
		}
		if len(n.Args) != def.Arity {
			return fmt.Errorf("%s: operator %q expects %d argument(s), got %d",
				path, n.Name, def.Arity, len(n.Args))
		}
		// K3a：标量参数位必须是不依赖序列数据的常量表达式。
		//
		// 拦的是「参数个数与节点形态都合法、但语义上把序列当标量用」的假合法
		// —— 例如 ts_mean(close, volume)：过闸后求值时 firstScalar 取到成交量
		// 首值（或 NaN）⇒ int(NaN) 未定义 ⇒ 窗口荒谬 ⇒ 整条序列 NaN。
		// 在解析期拦住，报错点名参数位，AI 能自纠。
		for _, i := range def.ScalarParams {
			if i < 0 || i >= len(n.Args) {
				// 注册表自身声明越界 ⇒ 闸门配置错误，fail-loud（不静默跳过）。
				return fmt.Errorf("%s: operator %q 的 ScalarParams 声明越界 [%d]（arity=%d，注册表错误）",
					path, n.Name, i, len(n.Args))
			}
			if !isConstantExpr(n.Args[i]) {
				return fmt.Errorf("%s: operator %q 的第 %d 个参数必须是标量（常量），got %s",
					path, n.Name, i, describeNode(n.Args[i]))
			}
		}
		for i, arg := range n.Args {
			if err := validateNode(arg, fmt.Sprintf("%s.arg[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil

	case *CrossSectionalNode:
		def, ok := operatorRegistry[n.Op]
		if !ok || def.Category != CatCrossSectional {
			return fmt.Errorf("%s: unknown cross-sectional operator %q%s", path, n.Op, availableOperatorsHint())
		}
		argc := 1
		if n.Group != nil {
			argc = 2
		}
		if argc != def.Arity {
			return fmt.Errorf("%s: operator %q expects %d argument(s), got %d",
				path, n.Op, def.Arity, argc)
		}
		if err := validateNode(n.Expr, path+".expr"); err != nil {
			return err
		}
		if n.Group != nil {
			return validateNode(n.Group, path+".group")
		}
		return nil

	default:
		return fmt.Errorf("%s: unknown AST node type %T", path, node)
	}
}
