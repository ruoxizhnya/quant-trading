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

// CrossSectionalNode represents a cross-sectional operation (e.g., cs_rank,
// cs_zscore, cs_neutralize).
//
// For 1-arg ops (cs_rank, cs_zscore, cs_percentile), Group is nil.
// For cs_neutralize, Group is the grouping expression — typically an
// identifier like `sector`, but may be any expression (e.g.
// `market_cap > 1e10` for binary grouping). The evaluator uses the
// latest group value per symbol as a categorical label.
type CrossSectionalNode struct {
	Op    string
	Expr  Node
	Group Node // optional; non-nil only for cs_neutralize
}

func (n *CrossSectionalNode) Type() NodeType { return NodeTypeCrossSectional }
func (n *CrossSectionalNode) String() string {
	if n.Group != nil {
		return fmt.Sprintf("%s(%s, %s)", n.Op, n.Expr.String(), n.Group.String())
	}
	return fmt.Sprintf("%s(%s)", n.Op, n.Expr.String())
}
func (n *CrossSectionalNode) Children() []Node {
	if n.Group != nil {
		return []Node{n.Expr, n.Group}
	}
	return []Node{n.Expr}
}

// Expression represents a complete factor expression
type Expression struct {
	ID       string
	Formula  string
	AST      Node
	Inputs   []string // Required raw data fields
	Category string   // "momentum" | "value" | "quality" | "custom"
}

// Validate 是 L1 的**语法 + 算子闸门**（单一实现：validate_factor 与
// agents 两条路共用；见 registry.go 的 validateNode）。
//
// 它递归走 AST 并 fail-closed 校验：
//   - FunctionNode.Name / CrossSectionalNode.Op / 一元 / 二元算子名必须在
//     注册表里（未登记 → 报错并列出可用算子名，便于 AI 自纠）；
//   - 参数个数与注册表声明一致（`ts_mean(close)` 这类拦截）；
//   - IdentifierNode.Name 必须是已知数据字段。
//
// 修 OBS-06：此前这里只判 formula/AST 非空，Parse 成功即 valid=true，于是
// `CROSS(MA(close,5), MA(close,20))`（两个都不存在的算子）被判「合法」。
// 现在算子名合法集合有单一事实源（注册表），未登记一律不通过。
func (e *Expression) Validate() error {
	if e.Formula == "" {
		return fmt.Errorf("formula cannot be empty")
	}
	if e.AST == nil {
		return fmt.Errorf("AST cannot be nil")
	}
	return validateNode(e.AST, "root")
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
		extractInputsRecursive(n.Group, inputs)
	}
}

// IsTimeSeriesOp 报告 op 是否为已登记的时序算子。
//
// 名字合法集合由 registry.go 的 operatorRegistry 单一提供（此前是此处一份
// 与 evaluator.go 一份的硬编码 slice —— 会漂移，是 OBS-06 的成因之一）。
func IsTimeSeriesOp(op string) bool {
	def, ok := operatorRegistry[op]
	return ok && def.Category == CatTimeSeries
}

// IsCrossSectionalOp 报告 op 是否为已登记的横截面算子。
func IsCrossSectionalOp(op string) bool {
	def, ok := operatorRegistry[op]
	return ok && def.Category == CatCrossSectional
}

// IsMathOp 报告 op 是否为已登记的一元逐元素算子（neg/abs/log/sqrt/sign/exp）。
func IsMathOp(op string) bool {
	def, ok := operatorRegistry[op]
	return ok && def.Category == CatUnary
}

// IsDataField 报告 name 是否为已知数据字段（单一事实源：registry.go 的
// fieldRegistry）。
func IsDataField(name string) bool {
	_, ok := fieldRegistry[name]
	return ok
}
