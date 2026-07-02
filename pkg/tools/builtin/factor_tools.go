package builtin

import (
	"context"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/client"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
)

// ─── ValidateFactorTool ────────────────────────────────────────────────
//
// L1 validation gate: checks that a factor expression parses successfully
// under the expression DSL grammar. Does NOT compute values — that is
// the job of factor.compute / compute_factor_ic. Cheap (<1ms), so an
// agent should always call this before committing to a longer compute.
//
// Tool name: "validate_factor"
// Input: expression (required)
// Output: { valid: bool, inputs: []string, ast: string, error: string }
//
// Note on error semantics: a parse failure is NOT returned as a Go error
// from Execute — it's a successful validation result with valid=false.
// The agent reads the `valid` flag and decides whether to retry. Tool-
// level errors (missing args) are still returned as ErrInvalidArgs.
type ValidateFactorTool struct{}

var _ tools.Tool = (*ValidateFactorTool)(nil)

// NewValidateFactorTool constructs a ValidateFactorTool. No dependencies
// are injected because expression.Parser carries per-call mutable state
// (tokens, pos) and is not safe for concurrent use across goroutines.
// Instead, Execute creates a fresh Parser per call — cheap and safe.
func NewValidateFactorTool() *ValidateFactorTool {
	return &ValidateFactorTool{}
}

func (t *ValidateFactorTool) Name() string { return "validate_factor" }

func (t *ValidateFactorTool) Description() string {
	return "Validate a factor expression's syntax (L1 gate). Returns validity, required data inputs (e.g. close, volume), and the parsed AST. Does not compute values — call compute_factor_ic for that."
}

func (t *ValidateFactorTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{
			Name:        "expression",
			Type:        "string",
			Description: "Factor DSL expression to validate, e.g. 'ts_rank(close, 20)' or '(close - ts_mean(close, 20)) / ts_std(close, 20)'.",
			Required:    true,
		},
	}
}

func (t *ValidateFactorTool) OutputSchema() tools.OutputSchema {
	return tools.OutputSchema{
		Type:        "object",
		Description: "Validation result. If valid is false, error explains the syntax problem.",
		Fields: []tools.OutputField{
			{Name: "valid", Type: "bool", Description: "true if the expression parses successfully."},
			{Name: "inputs", Type: "array", Description: "Required data fields (e.g. [\"close\", \"volume\"]). Empty if invalid."},
			{Name: "ast", Type: "string", Description: "String representation of the parsed AST. Empty if invalid."},
			{Name: "error", Type: "string", Description: "Parse error message. Empty if valid."},
		},
	}
}

func (t *ValidateFactorTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	expr, err := requireString(args, "expression")
	if err != nil {
		return nil, err
	}

	// Fresh parser per call: expression.Parser holds mutable tokens/pos
	// state on the receiver, so a shared instance would race across
	// concurrent HTTP requests. NewParser is allocation-cheap.
	parser := expression.NewParser()
	parsed, err := parser.Parse(expr)
	if err != nil {
		// Parse failure is a validation result, not a tool error. The
		// agent inspects valid=false and the error string to retry.
		return map[string]interface{}{
			"valid":  false,
			"inputs": []string{},
			"ast":    "",
			"error":  err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"valid":  true,
		"inputs": parsed.Inputs,
		"ast":    parsed.AST.String(),
		"error":  "",
	}, nil
}

// ─── ComputeFactorICTool ───────────────────────────────────────────────
//
// L2 validation gate: computes a factor expression's Information
// Coefficient (IC) and Information Ratio (IR) over a symbol list and
// date range. IC > 0.03 is generally useful; IC > 0.05 is strong.
//
// Tool name: "compute_factor_ic"
// Input: expression, symbols, start_date, end_date (all required)
// Output: *client.FactorMetrics { IC, IR }
//
// Design note (deviation from hermes-agent-integration-system-design.md
// §3.2): the design doc specifies a `universe` parameter (default
// "csi300") instead of `symbols`. A universe resolver would map a named
// pool ("csi300") to a concrete []string. That resolver is Phase 2
// work (needs storage or data-service backing). For Phase 1 we accept
// an explicit `symbols` list, matching the existing factor.evaluate /
// factor.compute tools. When the resolver lands, a future commit can
// add `universe` as an alternative input — both fields will be accepted
// and exactly one will be required.
type ComputeFactorICTool struct {
	c *client.FactorClient
}

var _ tools.Tool = (*ComputeFactorICTool)(nil)

// NewComputeFactorICTool constructs a ComputeFactorICTool backed by c.
// Panics if c is nil — wiring bug, fail loud at startup.
func NewComputeFactorICTool(c *client.FactorClient) *ComputeFactorICTool {
	if c == nil {
		panic("builtin: NewComputeFactorICTool called with nil FactorClient")
	}
	return &ComputeFactorICTool{c: c}
}

func (t *ComputeFactorICTool) Name() string { return "compute_factor_ic" }

func (t *ComputeFactorICTool) Description() string {
	return "Compute a factor expression's Information Coefficient (IC) and Information Ratio (IR) over a symbol list and date range (L2 gate). IC > 0.03 is useful; IC > 0.05 is strong."
}

func (t *ComputeFactorICTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{
			Name:        "expression",
			Type:        "string",
			Description: "Factor DSL expression, e.g. 'ts_rank(close, 20)'.",
			Required:    true,
		},
		{
			Name:        "symbols",
			Type:        "[]string",
			Description: "List of stock symbols to evaluate the factor over, e.g. [\"000001.SZ\", \"600000.SH\"].",
			Required:    true,
		},
		{
			Name:        "start_date",
			Type:        "string",
			Description: "Evaluation window start date in YYYY-MM-DD format.",
			Required:    true,
		},
		{
			Name:        "end_date",
			Type:        "string",
			Description: "Evaluation window end date in YYYY-MM-DD format.",
			Required:    true,
		},
	}
}

func (t *ComputeFactorICTool) OutputSchema() tools.OutputSchema {
	return tools.OutputSchema{
		Type:        "object",
		Description: "Factor quality metrics.",
		Fields: []tools.OutputField{
			{Name: "ic", Type: "float", Description: "Information Coefficient: rank correlation between factor value and forward return. |IC| > 0.03 is useful."},
			{Name: "ir", Type: "float", Description: "Information Ratio: IC mean / IC std. IR > 0.5 is strong."},
		},
	}
}

func (t *ComputeFactorICTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	expr, err := requireString(args, "expression")
	if err != nil {
		return nil, err
	}
	symbols, err := requireStringSlice(args, "symbols")
	if err != nil {
		return nil, err
	}
	startDate, err := requireString(args, "start_date")
	if err != nil {
		return nil, err
	}
	endDate, err := requireString(args, "end_date")
	if err != nil {
		return nil, err
	}

	m, err := t.c.EvaluateFactor(ctx, expr, symbols, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("compute_factor_ic: %w", err)
	}
	return m, nil
}
