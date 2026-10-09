package expression

import (
	"fmt"
	"math"
)

// DataProvider provides time-series data for evaluation.
type DataProvider interface {
	GetField(symbol string, field string, lookback int) ([]float64, error)
	GetSymbols() []string

	// Fields 返回本 provider **能供应**的数据字段名（升序、无重复）。
	//
	// 这是「provider 的能力声明」—— 它和语言侧注册表（registry.go 的
	// fieldRegistry / AvailableFields）是两件事：
	//   - 注册表回答「这个字段名在 DSL 里是否**合法**」；
	//   - Fields() 回答「这个 provider **真的能取到**哪些字段」。
	//
	// 二者必须对齐：注册了的字段若 provider 供不了，就是「假合法」——AI
	// 在闸门通过、求值才炸；provider 能供的字段若没注册，就是「误拒」——
	// 正经用法被挡在门外。跨包护栏（pkg/strategy/expression/
	// data_provider_fields_test.go）把两侧钉死，禁止再漂移。
	//
	// 2026-10（OBS-08 切片 1）：新增此方法。此前闸门白名单是手写 map，
	// 与唯一生产 provider 实测能力错配（放行 market_cap/eps/roe_ttm 等
	// 永远无法求值的字段，又拦掉 provider 支持的 ps/roa）。把能力交给
	// provider 自报、由护栏对齐，是「名字合法集合只能有一处」在字段上的
	// 同款收敛（OBS-06 已对算子名做过）。
	Fields() []string
}

// Evaluator evaluates AST nodes against market data
type Evaluator struct {
	provider DataProvider
}

// NewEvaluator creates a new evaluator with the given data provider
func NewEvaluator(provider DataProvider) *Evaluator {
	return &Evaluator{provider: provider}
}

// Evaluate evaluates an expression AST and returns the result for all symbols
func (e *Evaluator) Evaluate(node Node, lookback int) (map[string][]float64, error) {
	if node == nil {
		return nil, fmt.Errorf("cannot evaluate nil node")
	}

	switch n := node.(type) {
	case *LiteralNode:
		return e.evaluateLiteral(n)
	case *IdentifierNode:
		return e.evaluateIdentifier(n, lookback)
	case *BinaryOpNode:
		return e.evaluateBinaryOp(n, lookback)
	case *UnaryOpNode:
		return e.evaluateUnaryOp(n, lookback)
	case *FunctionNode:
		return e.evaluateFunction(n, lookback)
	case *CrossSectionalNode:
		return e.evaluateCrossSectional(n, lookback)
	default:
		return nil, fmt.Errorf("unknown node type: %T", node)
	}
}

func (e *Evaluator) evaluateLiteral(n *LiteralNode) (map[string][]float64, error) {
	result := make(map[string][]float64)
	for _, symbol := range e.provider.GetSymbols() {
		result[symbol] = []float64{n.Value}
	}
	return result, nil
}

func (e *Evaluator) evaluateIdentifier(n *IdentifierNode, lookback int) (map[string][]float64, error) {
	result := make(map[string][]float64)
	for _, symbol := range e.provider.GetSymbols() {
		data, err := e.provider.GetField(symbol, n.Name, lookback)
		if err != nil {
			return nil, fmt.Errorf("failed to get field %s for %s: %w", n.Name, symbol, err)
		}
		result[symbol] = data
	}
	return result, nil
}

func (e *Evaluator) evaluateBinaryOp(n *BinaryOpNode, lookback int) (map[string][]float64, error) {
	left, err := e.Evaluate(n.Left, lookback)
	if err != nil {
		return nil, fmt.Errorf("left operand: %w", err)
	}

	right, err := e.Evaluate(n.Right, lookback)
	if err != nil {
		return nil, fmt.Errorf("right operand: %w", err)
	}

	result := make(map[string][]float64)
	for _, symbol := range e.provider.GetSymbols() {
		leftVals := left[symbol]
		rightVals := right[symbol]

		// Align lengths (broadcast scalar to vector)
		maxLen := max(len(leftVals), len(rightVals))
		if len(leftVals) == 1 && maxLen > 1 {
			leftVals = broadcast(leftVals[0], maxLen)
		}
		if len(rightVals) == 1 && maxLen > 1 {
			rightVals = broadcast(rightVals[0], maxLen)
		}

		if len(leftVals) != len(rightVals) {
			return nil, fmt.Errorf("length mismatch for %s: left=%d, right=%d", symbol, len(leftVals), len(rightVals))
		}

		vals := make([]float64, len(leftVals))
		for i := range leftVals {
			vals[i] = applyBinaryOp(n.Op, leftVals[i], rightVals[i])
		}
		result[symbol] = vals
	}

	return result, nil
}

func (e *Evaluator) evaluateUnaryOp(n *UnaryOpNode, lookback int) (map[string][]float64, error) {
	expr, err := e.Evaluate(n.Expr, lookback)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]float64)
	for _, symbol := range e.provider.GetSymbols() {
		vals := expr[symbol]
		out := make([]float64, len(vals))
		for i, v := range vals {
			out[i] = applyUnaryOp(n.Op, v)
		}
		result[symbol] = out
	}

	return result, nil
}

func (e *Evaluator) evaluateFunction(n *FunctionNode, lookback int) (map[string][]float64, error) {
	// 算子类别查注册表（单一事实源）。未登记的一律显式报错，不静默直通。
	cat, ok := OperatorCategory(n.Name)
	if !ok || (cat != CatTimeSeries && cat != CatUnary) {
		return nil, fmt.Errorf("unknown operator: %s%s", n.Name, availableOperatorsHint())
	}

	// Evaluate all arguments
	argResults := make([]map[string][]float64, len(n.Args))
	for i, arg := range n.Args {
		res, err := e.Evaluate(arg, lookback)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		argResults[i] = res
	}

	// 一元算子（neg / abs / log / sqrt / sign / exp）走逐元素，不是时序。
	//
	// 此前这里无条件走 applyTimeSeriesOp，于是 `neg(x)` 会报
	// "unknown time-series operator: neg" —— 解析得过、跑不起来。受影响的
	// 不止新加的估值表达式，multi_factor 的默认表达式
	// `cs_rank(neg(ts_std(close, 20)))` 一直是坏的（P2-12 时才被发现）。
	if isUnaryOp(n.Name) {
		if len(argResults) != 1 {
			return nil, fmt.Errorf("%s requires 1 argument, got %d", n.Name, len(argResults))
		}
		result := make(map[string][]float64)
		for symbol, vals := range argResults[0] {
			out := make([]float64, len(vals))
			for i, v := range vals {
				out[i] = applyUnaryOp(n.Name, v)
			}
			result[symbol] = out
		}
		return result, nil
	}

	result := make(map[string][]float64)
	for _, symbol := range e.provider.GetSymbols() {
		// Collect arguments for this symbol
		args := make([][]float64, len(argResults))
		for i, argRes := range argResults {
			args[i] = argRes[symbol]
		}

		vals, err := applyTimeSeriesOp(n.Name, args)
		if err != nil {
			return nil, fmt.Errorf("%s for %s: %w", n.Name, symbol, err)
		}
		result[symbol] = vals
	}

	return result, nil
}

// isUnaryOp reports whether name is an element-wise unary operator
// (handled by applyUnaryOp) rather than a time-series operator.
//
// 类别查注册表（此前是此处一份 switch + registry 一份声明的双重事实源）。
func isUnaryOp(name string) bool {
	cat, ok := OperatorCategory(name)
	return ok && cat == CatUnary
}

func (e *Evaluator) evaluateCrossSectional(n *CrossSectionalNode, lookback int) (map[string][]float64, error) {
	expr, err := e.Evaluate(n.Expr, lookback)
	if err != nil {
		return nil, err
	}

	// If the op has a group expression (cs_neutralize), evaluate it and
	// build a group-values slice aligned with latestValues. The latest
	// group value per symbol acts as a categorical label.
	var groupValues []float64
	if n.Group != nil {
		groupExpr, err := e.Evaluate(n.Group, lookback)
		if err != nil {
			return nil, fmt.Errorf("group expr: %w", err)
		}
		symbols := e.provider.GetSymbols()
		groupValues = make([]float64, len(symbols))
		for i, symbol := range symbols {
			vals := groupExpr[symbol]
			if len(vals) > 0 {
				groupValues[i] = vals[len(vals)-1]
			}
		}
	}

	result := make(map[string][]float64)
	symbols := e.provider.GetSymbols()

	// Cross-sectional operations work on a single time point across all symbols
	// For simplicity, we apply to the latest value
	latestValues := make([]float64, len(symbols))
	for i, symbol := range symbols {
		vals := expr[symbol]
		if len(vals) > 0 {
			latestValues[i] = vals[len(vals)-1]
		}
	}

	ranked, err := applyCrossSectionalOp(n.Op, latestValues, groupValues)
	if err != nil {
		return nil, err
	}
	for i, symbol := range symbols {
		result[symbol] = []float64{ranked[i]}
	}

	return result, nil
}

// applyBinaryOp applies a binary operator to two values
func applyBinaryOp(op string, a, b float64) float64 {
	switch op {
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	case "/":
		if b == 0 {
			return math.NaN()
		}
		return a / b
	case "^":
		return math.Pow(a, b)
	case ">":
		if a > b {
			return 1
		}
		return 0
	case "<":
		if a < b {
			return 1
		}
		return 0
	case "==":
		if a == b {
			return 1
		}
		return 0
	default:
		return math.NaN()
	}
}

// applyUnaryOp applies a unary operator to a value
func applyUnaryOp(op string, v float64) float64 {
	switch op {
	case "neg":
		return -v
	case "abs":
		return math.Abs(v)
	case "log":
		if v <= 0 {
			return math.NaN()
		}
		return math.Log(v)
	case "sqrt":
		if v < 0 {
			return math.NaN()
		}
		return math.Sqrt(v)
	case "sign":
		if v > 0 {
			return 1
		} else if v < 0 {
			return -1
		}
		return 0
	case "exp":
		return math.Exp(v)
	default:
		return math.NaN()
	}
}

// applyTimeSeriesOp 按时序算子名查注册表取求值绑定。
//
// 未登记算子 → 显式报错（此前是 switch + default 报错，但名字集合是
// evaluator.go 里第二份硬编码）。L2 递推算子（ts_rma/ts_ewma/ts_kalman）的
// 绑定复用 indicator 包的 Batch 实现，对每个 symbol 一次算完整条序列。
func applyTimeSeriesOp(op string, args [][]float64) ([]float64, error) {
	def, ok := operatorRegistry[op]
	if !ok || def.Category != CatTimeSeries || def.tsEval == nil {
		return nil, fmt.Errorf("unknown time-series operator: %s%s", op, availableOperatorsHint())
	}
	if len(args) != def.Arity {
		return nil, fmt.Errorf("%s requires %d arguments, got %d", op, def.Arity, len(args))
	}
	return def.tsEval(args)
}

// firstScalar 取标量参数序列的首值；空序列返回 NaN（调用方已由闸门保证
// 参数是常量字面量 ⇒ 长度 1，此处的防御仅为直接的稳健性）。
func firstScalar(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	return xs[0]
}

// applyCrossSectionalOp 按横截面算子名查注册表取求值绑定。
//
// **此前 default 分支是 `return values`（静默直通）** —— 未登记算子原样返回
// 输入，等于偷偷放行一个不存在的算子。收敛后未登记一律显式报错。
//
// For 1-arg ops (cs_rank, cs_zscore, cs_percentile), group is nil and
// ignored. For cs_neutralize, group carries the per-symbol categorical
// labels used for per-group mean subtraction.
func applyCrossSectionalOp(op string, values, group []float64) ([]float64, error) {
	def, ok := operatorRegistry[op]
	if !ok || def.Category != CatCrossSectional || def.csEval == nil {
		return nil, fmt.Errorf("unknown cross-sectional operator: %s%s", op, availableOperatorsHint())
	}
	return def.csEval(values, group), nil
}

// Helper functions

func broadcast(val float64, n int) []float64 {
	result := make([]float64, n)
	for i := range result {
		result[i] = val
	}
	return result
}

// max is provided by Go 1.21+ builtin (P0-10).
