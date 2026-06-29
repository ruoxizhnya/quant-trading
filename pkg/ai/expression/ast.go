package expression

import (
	"fmt"
	"strings"
)

// NodeType represents the type of an AST node
type NodeType string

const (
	NodeTypeLiteral        NodeType = "literal"
	NodeTypeIdentifier     NodeType = "identifier"
	NodeTypeBinaryOp       NodeType = "binary_op"
	NodeTypeUnaryOp        NodeType = "unary_op"
	NodeTypeFunction       NodeType = "function"
	NodeTypeCrossSectional NodeType = "cross_sectional"
)

// Node is the interface for all AST nodes
type Node interface {
	Type() NodeType
	String() string
	Children() []Node
}

// LiteralNode represents a numeric literal
type LiteralNode struct {
	Value float64
}

func (n *LiteralNode) Type() NodeType   { return NodeTypeLiteral }
func (n *LiteralNode) String() string   { return fmt.Sprintf("%g", n.Value) }
func (n *LiteralNode) Children() []Node { return nil }

// IdentifierNode represents a data field identifier (e.g., "close", "volume")
type IdentifierNode struct {
	Name string
}

func (n *IdentifierNode) Type() NodeType   { return NodeTypeIdentifier }
func (n *IdentifierNode) String() string   { return n.Name }
func (n *IdentifierNode) Children() []Node { return nil }

// BinaryOpNode represents a binary operation (+, -, *, /, etc.)
type BinaryOpNode struct {
	Op    string // +, -, *, /, >, <, ==, etc.
	Left  Node
	Right Node
}

func (n *BinaryOpNode) Type() NodeType { return NodeTypeBinaryOp }
func (n *BinaryOpNode) String() string {
	return fmt.Sprintf("(%s %s %s)", n.Left.String(), n.Op, n.Right.String())
}
func (n *BinaryOpNode) Children() []Node { return []Node{n.Left, n.Right} }

// UnaryOpNode represents a unary operation (-, abs, log, etc.)
type UnaryOpNode struct {
	Op   string
	Expr Node
}

func (n *UnaryOpNode) Type() NodeType { return NodeTypeUnaryOp }
func (n *UnaryOpNode) String() string {
	return fmt.Sprintf("%s(%s)", n.Op, n.Expr.String())
}
func (n *UnaryOpNode) Children() []Node { return []Node{n.Expr} }

// FunctionNode represents a function call (e.g., ts_mean(close, 20))
type FunctionNode struct {
	Name string
	Args []Node
}

func (n *FunctionNode) Type() NodeType { return NodeTypeFunction }
func (n *FunctionNode) String() string {
	args := make([]string, len(n.Args))
	for i, arg := range n.Args {
		args[i] = arg.String()
	}
	return fmt.Sprintf("%s(%s)", n.Name, strings.Join(args, ", "))
}
func (n *FunctionNode) Children() []Node {
	return n.Args
}

// CrossSectionalNode represents a cross-sectional operation (e.g., cs_rank, cs_zscore)
type CrossSectionalNode struct {
	Op   string
	Expr Node
}

func (n *CrossSectionalNode) Type() NodeType { return NodeTypeCrossSectional }
func (n *CrossSectionalNode) String() string {
	return fmt.Sprintf("%s(%s)", n.Op, n.Expr.String())
}
func (n *CrossSectionalNode) Children() []Node { return []Node{n.Expr} }

// Expression represents a complete factor expression
type Expression struct {
	ID       string
	Formula  string
	AST      Node
	Inputs   []string // Required raw data fields
	Category string   // "momentum" | "value" | "quality" | "custom"
}

// Validate checks if the expression AST is valid
func (e *Expression) Validate() error {
	if e.Formula == "" {
		return fmt.Errorf("formula cannot be empty")
	}
	if e.AST == nil {
		return fmt.Errorf("AST cannot be nil")
	}
	return nil
}

// ExtractInputs extracts all required data fields from the AST
func ExtractInputs(node Node) []string {
	inputs := make(map[string]bool)
	extractInputsRecursive(node, inputs)

	result := make([]string, 0, len(inputs))
	for input := range inputs {
		result = append(result, input)
	}
	return result
}

func extractInputsRecursive(node Node, inputs map[string]bool) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *IdentifierNode:
		inputs[n.Name] = true
	case *BinaryOpNode:
		extractInputsRecursive(n.Left, inputs)
		extractInputsRecursive(n.Right, inputs)
	case *UnaryOpNode:
		extractInputsRecursive(n.Expr, inputs)
	case *FunctionNode:
		for _, arg := range n.Args {
			extractInputsRecursive(arg, inputs)
		}
	case *CrossSectionalNode:
		extractInputsRecursive(n.Expr, inputs)
	}
}

// IsTimeSeriesOp checks if an operator is a time-series operator
func IsTimeSeriesOp(op string) bool {
	tsOps := []string{"ts_mean", "ts_std", "ts_corr", "ts_delay", "ts_rank", "ts_delta", "ts_sum", "ts_max", "ts_min", "ts_pct_change"}
	for _, tsOp := range tsOps {
		if op == tsOp {
			return true
		}
	}
	return false
}

// IsCrossSectionalOp checks if an operator is a cross-sectional operator
func IsCrossSectionalOp(op string) bool {
	csOps := []string{"cs_rank", "cs_zscore", "cs_percentile", "cs_neutralize"}
	for _, csOp := range csOps {
		if op == csOp {
			return true
		}
	}
	return false
}

// IsMathOp checks if an operator is a math operator
func IsMathOp(op string) bool {
	mathOps := []string{"abs", "log", "sqrt", "sign", "exp", "pow"}
	for _, mathOp := range mathOps {
		if op == mathOp {
			return true
		}
	}
	return false
}

// IsDataField checks if a name is a valid data field
func IsDataField(name string) bool {
	dataFields := []string{"open", "high", "low", "close", "volume", "turnover", "market_cap", "pe", "pb", "roe", "roe_ttm", "eps", "revenue", "profit"}
	for _, field := range dataFields {
		if name == field {
			return true
		}
	}
	return false
}
