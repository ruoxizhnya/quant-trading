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

// fieldRegistry 是字段名合法集合的**唯一权威来源**：字段 → 来源。
var fieldRegistry = map[string]FieldSource{
	// 行情
	"open":     FieldSourceMarket,
	"high":     FieldSourceMarket,
	"low":      FieldSourceMarket,
	"close":    FieldSourceMarket,
	"volume":   FieldSourceMarket,
	"turnover": FieldSourceMarket,
	// 基本面（对应 domain.Fundamental 的 PE/PB/PS/ROE/ROA/Revenue/NetProfit）
	"pe":      FieldSourceFundamentals,
	"pb":      FieldSourceFundamentals,
	"ps":      FieldSourceFundamentals,
	"roe":     FieldSourceFundamentals,
	"roa":     FieldSourceFundamentals,
	"revenue": FieldSourceFundamentals,
	"profit":  FieldSourceFundamentals,
	// 横截面分组标签（cs_neutralize 的 group 参数）——不是数据字段
	"sector": FieldSourceGroup,
}

// operatorRegistry 是唯一的名字合法集合。map 便于 O(1) 查表；
// AvailableOperators() 排序后输出以保证确定性。
var operatorRegistry = map[string]OperatorDef{
	// ══ 时序算子（存量 FIR，stateless，causal，Lookback/Warmup 由参数决定）══
	"ts_mean": {
		Name: "ts_mean", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_mean", "ts_mean : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsMean(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_std": {
		Name: "ts_std", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_std", "ts_std : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsStd(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_sum": {
		Name: "ts_sum", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_sum", "ts_sum : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsSum(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_max": {
		Name: "ts_max", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_max", "ts_max : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsMax(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_min": {
		Name: "ts_min", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_min", "ts_min : Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmup,
		selfLookback: windowSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsMin(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_delay": {
		Name: "ts_delay", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_delay", "ts_delay : Series × Scalar → Series"),
		selfWarmup:   periodSelfWarmup,
		selfLookback: periodSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsDelay(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_delta": {
		Name: "ts_delta", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_delta", "ts_delta : Series × Scalar → Series"),
		selfWarmup:   periodSelfWarmup,
		selfLookback: periodSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsDelta(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_pct_change": {
		Name: "ts_pct_change", Category: CatTimeSeries, Arity: 2,
		Spec:         seriesStatelessSpec("ts_pct_change", "ts_pct_change : Series × Scalar → Series"),
		selfWarmup:   periodSelfWarmup,
		selfLookback: periodSelfLookback,
		tsEval:       func(args [][]float64) ([]float64, error) { return tsPctChange(args[0], int(firstScalar(args[1]))), nil },
	},
	"ts_corr": {
		Name: "ts_corr", Category: CatTimeSeries, Arity: 3,
		Spec:         seriesStatelessSpec("ts_corr", "ts_corr : Series × Series × Scalar → Series"),
		selfWarmup:   windowSelfWarmupAt2,
		selfLookback: windowSelfLookbackAt2,
		tsEval: func(args [][]float64) ([]float64, error) {
			return tsCorr(args[0], args[1], int(firstScalar(args[2]))), nil
		},
	},
	"ts_rank": {
		Name: "ts_rank", Category: CatTimeSeries, Arity: 2,
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
		Spec:         recursiveSpec("ts_rma", "ts_rma : Series × Scalar → Series", "sma(first,N)"),
		selfWarmup:   rmaSelfWarmup,
		selfLookback: stateSelfLookback, // ∞（0 + State=true）
		tsEval: func(args [][]float64) ([]float64, error) {
			return indicator.RMABatch(args[0], int(firstScalar(args[1])))
		},
	},
	"ts_ewma": {
		Name: "ts_ewma", Category: CatTimeSeries, Arity: 2,
		Spec:         recursiveSpec("ts_ewma", "ts_ewma : Series × Scalar → Series", "x[0]"),
		selfWarmup:   ewmaSelfWarmup,
		selfLookback: stateSelfLookback, // ∞
		tsEval: func(args [][]float64) ([]float64, error) {
			return indicator.EWMABatch(args[0], firstScalar(args[1]))
		},
	},
	"ts_kalman": {
		Name: "ts_kalman", Category: CatTimeSeries, Arity: 3,
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
		if s == source {
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
		if s != FieldSourceGroup {
			fields = append(fields, f)
		}
	}
	sort.Strings(fields)
	return fields
}

// FieldSourceOf 返回字段名所属来源；未登记 → (_, false)。
func FieldSourceOf(name string) (FieldSource, bool) {
	s, ok := fieldRegistry[name]
	return s, ok
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
