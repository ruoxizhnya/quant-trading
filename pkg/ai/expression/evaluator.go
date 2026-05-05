package expression

import (
	"fmt"
	"math"
)

// DataProvider provides time-series data for evaluation
type DataProvider interface {
	GetField(symbol string, field string, lookback int) ([]float64, error)
	GetSymbols() []string
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
	// Evaluate all arguments
	argResults := make([]map[string][]float64, len(n.Args))
	for i, arg := range n.Args {
		res, err := e.Evaluate(arg, lookback)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		argResults[i] = res
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

func (e *Evaluator) evaluateCrossSectional(n *CrossSectionalNode, lookback int) (map[string][]float64, error) {
	expr, err := e.Evaluate(n.Expr, lookback)
	if err != nil {
		return nil, err
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

	ranked := applyCrossSectionalOp(n.Op, latestValues)
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

// applyTimeSeriesOp applies a time-series operator
func applyTimeSeriesOp(op string, args [][]float64) ([]float64, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no arguments")
	}

	switch op {
	case "ts_mean":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_mean requires 2 arguments")
		}
		window := int(args[1][0])
		return tsMean(args[0], window), nil
	case "ts_std":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_std requires 2 arguments")
		}
		window := int(args[1][0])
		return tsStd(args[0], window), nil
	case "ts_sum":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_sum requires 2 arguments")
		}
		window := int(args[1][0])
		return tsSum(args[0], window), nil
	case "ts_max":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_max requires 2 arguments")
		}
		window := int(args[1][0])
		return tsMax(args[0], window), nil
	case "ts_min":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_min requires 2 arguments")
		}
		window := int(args[1][0])
		return tsMin(args[0], window), nil
	case "ts_delay":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_delay requires 2 arguments")
		}
		periods := int(args[1][0])
		return tsDelay(args[0], periods), nil
	case "ts_delta":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_delta requires 2 arguments")
		}
		periods := int(args[1][0])
		return tsDelta(args[0], periods), nil
	case "ts_pct_change":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_pct_change requires 2 arguments")
		}
		periods := int(args[1][0])
		return tsPctChange(args[0], periods), nil
	case "ts_corr":
		if len(args) != 3 {
			return nil, fmt.Errorf("ts_corr requires 3 arguments")
		}
		window := int(args[2][0])
		return tsCorr(args[0], args[1], window), nil
	case "ts_rank":
		if len(args) != 2 {
			return nil, fmt.Errorf("ts_rank requires 2 arguments")
		}
		window := int(args[1][0])
		return tsRank(args[0], window), nil
	default:
		return nil, fmt.Errorf("unknown time-series operator: %s", op)
	}
}

// applyCrossSectionalOp applies a cross-sectional operator
func applyCrossSectionalOp(op string, values []float64) []float64 {
	switch op {
	case "cs_rank":
		return csRank(values)
	case "cs_zscore":
		return csZScore(values)
	case "cs_percentile":
		return csPercentile(values)
	default:
		return values
	}
}

// Helper functions

func broadcast(val float64, n int) []float64 {
	result := make([]float64, n)
	for i := range result {
		result[i] = val
	}
	return result
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
