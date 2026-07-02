package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/client"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
)

// ═══════════════════════════════════════════════════════════════════════
//  ValidateFactorTool tests
// ═══════════════════════════════════════════════════════════════════════

func TestValidateFactorTool_Name(t *testing.T) {
	tt := NewValidateFactorTool()
	assert.Equal(t, "validate_factor", tt.Name())
}

func TestValidateFactorTool_Description(t *testing.T) {
	tt := NewValidateFactorTool()
	desc := tt.Description()
	assert.NotEmpty(t, desc)
	// Description starts with "Validate" (capital V) — case-sensitive match.
	assert.Contains(t, desc, "Validate")
	assert.Contains(t, desc, "L1 gate")
}

func TestValidateFactorTool_Parameters(t *testing.T) {
	tt := NewValidateFactorTool()
	params := tt.Parameters()
	require.Len(t, params, 1)
	assert.Equal(t, "expression", params[0].Name)
	assert.Equal(t, "string", params[0].Type)
	assert.True(t, params[0].Required, "expression should be required")
}

func TestValidateFactorTool_OutputSchema(t *testing.T) {
	tt := NewValidateFactorTool()
	schema := tt.OutputSchema()
	assert.Equal(t, "object", schema.Type)
	require.NotEmpty(t, schema.Fields)

	expected := map[string]string{
		"valid":  "bool",
		"inputs": "array",
		"ast":    "string",
		"error":  "string",
	}
	assert.Len(t, schema.Fields, len(expected))
	for _, f := range schema.Fields {
		typ, ok := expected[f.Name]
		require.True(t, ok, "unexpected field %q", f.Name)
		assert.Equal(t, typ, f.Type, "field %q type mismatch", f.Name)
	}
}

func TestValidateFactorTool_Execute_ValidExpression(t *testing.T) {
	tt := NewValidateFactorTool()
	args := map[string]interface{}{
		"expression": "ts_rank(close, 20)",
	}

	result, err := tt.Execute(context.Background(), args)
	require.NoError(t, err, "valid expression should not return a tool error")
	require.NotNil(t, result)

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be map[string]interface{}, got %T", result)

	valid, ok := m["valid"].(bool)
	require.True(t, ok, "valid should be bool, got %T", m["valid"])
	assert.True(t, valid, "expression 'ts_rank(close, 20)' should be valid")

	inputs, ok := m["inputs"].([]string)
	require.True(t, ok, "inputs should be []string, got %T", m["inputs"])
	assert.Contains(t, inputs, "close", "inputs should contain 'close'")

	ast, ok := m["ast"].(string)
	require.True(t, ok, "ast should be string, got %T", m["ast"])
	assert.NotEmpty(t, ast, "ast should be non-empty for valid expression")

	errStr, ok := m["error"].(string)
	require.True(t, ok, "error should be string, got %T", m["error"])
	assert.Empty(t, errStr, "error should be empty for valid expression")
}

func TestValidateFactorTool_Execute_ComplexExpression(t *testing.T) {
	tt := NewValidateFactorTool()
	args := map[string]interface{}{
		"expression": "(close - ts_mean(close, 20)) / ts_std(close, 20)",
	}

	result, err := tt.Execute(context.Background(), args)
	require.NoError(t, err)

	m := result.(map[string]interface{})
	assert.True(t, m["valid"].(bool))
	inputs := m["inputs"].([]string)
	assert.Contains(t, inputs, "close")
}

func TestValidateFactorTool_Execute_InvalidSyntax(t *testing.T) {
	// Missing closing paren — syntax error.
	tt := NewValidateFactorTool()
	args := map[string]interface{}{
		"expression": "ts_rank(close, 20",
	}

	result, err := tt.Execute(context.Background(), args)
	// Parse failure is a VALIDATION RESULT, not a tool error.
	require.NoError(t, err, "parse failure should NOT be returned as a Go error")
	require.NotNil(t, result)

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be map[string]interface{}, got %T", result)

	valid, ok := m["valid"].(bool)
	require.True(t, ok, "valid should be bool")
	assert.False(t, valid, "malformed expression should have valid=false")

	errStr, ok := m["error"].(string)
	require.True(t, ok, "error should be string")
	assert.NotEmpty(t, errStr, "error message should explain the syntax problem")

	inputs, ok := m["inputs"].([]string)
	require.True(t, ok, "inputs should be []string even when invalid")
	assert.Empty(t, inputs, "inputs should be empty for invalid expression")
}

func TestValidateFactorTool_Execute_EmptyFormula(t *testing.T) {
	// Empty string is a parse error (parser rejects empty formulas),
	// not an ErrInvalidArgs — requireString rejects "" but the parser
	// would also reject it. Verify the parser path returns valid=false.
	// Actually requireString rejects empty strings with ErrInvalidArgs,
	// so this exercises the arg-validation path, not the parser path.
	tt := NewValidateFactorTool()
	args := map[string]interface{}{
		"expression": "",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "expression")
}

func TestValidateFactorTool_Execute_MissingExpression(t *testing.T) {
	tt := NewValidateFactorTool()
	args := map[string]interface{}{}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "expression")
}

func TestValidateFactorTool_Execute_WrongType(t *testing.T) {
	tt := NewValidateFactorTool()
	args := map[string]interface{}{
		"expression": 42,
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "expression")
}

func TestValidateFactorTool_RegisterInRegistry(t *testing.T) {
	tt := NewValidateFactorTool()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tt))

	got, err := reg.Get("validate_factor")
	require.NoError(t, err)
	assert.Equal(t, "validate_factor", got.Name())

	info := reg.List()
	require.Len(t, info, 1)
	assert.Equal(t, "validate_factor", info[0].Name)
}

// ═══════════════════════════════════════════════════════════════════════
//  ComputeFactorICTool tests
// ═══════════════════════════════════════════════════════════════════════

func TestComputeFactorICTool_Name(t *testing.T) {
	tt := NewComputeFactorICTool(client.NewFactorClient("http://x"))
	assert.Equal(t, "compute_factor_ic", tt.Name())
}

func TestComputeFactorICTool_Description(t *testing.T) {
	tt := NewComputeFactorICTool(client.NewFactorClient("http://x"))
	desc := tt.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "IC")
}

func TestComputeFactorICTool_Parameters(t *testing.T) {
	tt := NewComputeFactorICTool(client.NewFactorClient("http://x"))
	params := tt.Parameters()
	require.Len(t, params, 4)

	expectedNames := []string{"expression", "symbols", "start_date", "end_date"}
	gotNames := make([]string, len(params))
	for i, p := range params {
		gotNames[i] = p.Name
	}
	assert.Equal(t, expectedNames, gotNames)

	for _, p := range params {
		assert.True(t, p.Required, "parameter %q should be required", p.Name)
	}
}

func TestComputeFactorICTool_OutputSchema(t *testing.T) {
	tt := NewComputeFactorICTool(client.NewFactorClient("http://x"))
	schema := tt.OutputSchema()
	assert.Equal(t, "object", schema.Type)

	found := map[string]string{}
	for _, f := range schema.Fields {
		found[f.Name] = f.Type
	}
	assert.Equal(t, "float", found["ic"], "ic field should be float")
	assert.Equal(t, "float", found["ir"], "ir field should be float")
}

func TestComputeFactorICTool_Execute_HappyPath(t *testing.T) {
	srv := newFactorTestServer()
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"expression": "ts_rank(close, 20)",
		"symbols":    []string{"000001.SZ", "600000.SH"},
		"start_date": "2022-01-01",
		"end_date":   "2024-01-01",
	}

	result, err := tt.Execute(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	m, ok := result.(*client.FactorMetrics)
	require.True(t, ok, "result should be *client.FactorMetrics, got %T", result)
	assert.InDelta(t, 0.045, m.IC, 1e-9)
	assert.InDelta(t, 0.62, m.IR, 1e-9)

	// Verify the server received the correct request body.
	assert.Equal(t, "ts_rank(close, 20)", srv.lastBody["formula"])
}

func TestComputeFactorICTool_Execute_SymbolsAsInterfaceSlice(t *testing.T) {
	// json.Unmarshal decodes a JSON array into []interface{}.
	srv := newFactorTestServer()
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"expression": "ts_rank(close, 20)",
		"symbols":    []interface{}{"000001.SZ", "600000.SH"},
		"start_date": "2022-01-01",
		"end_date":   "2024-01-01",
	}

	_, err := tt.Execute(context.Background(), args)
	require.NoError(t, err)
}

func TestComputeFactorICTool_Execute_MissingExpression(t *testing.T) {
	srv := newFactorTestServer()
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"symbols":    []string{"000001.SZ"},
		"start_date": "2022-01-01",
		"end_date":   "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "expression")
}

func TestComputeFactorICTool_Execute_MissingSymbols(t *testing.T) {
	srv := newFactorTestServer()
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"expression": "close",
		"start_date": "2022-01-01",
		"end_date":   "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "symbols")
}

func TestComputeFactorICTool_Execute_MissingStartDate(t *testing.T) {
	srv := newFactorTestServer()
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"expression": "close",
		"symbols":    []string{"000001.SZ"},
		"end_date":   "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "start_date")
}

func TestComputeFactorICTool_Execute_MissingEndDate(t *testing.T) {
	srv := newFactorTestServer()
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"expression": "close",
		"symbols":    []string{"000001.SZ"},
		"start_date": "2022-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "end_date")
}

func TestComputeFactorICTool_Execute_DownstreamError(t *testing.T) {
	srv := newFactorTestServer()
	srv.failWith = "evaluation failed"
	defer srv.close()

	tt := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))
	args := map[string]interface{}{
		"expression": "close",
		"symbols":    []string{"000001.SZ"},
		"start_date": "2022-01-01",
		"end_date":   "2024-01-01",
	}
	_, err := tt.Execute(context.Background(), args)
	require.Error(t, err)
	// Args were valid — the failure is downstream, so it must NOT be
	// conflated with ErrInvalidArgs.
	assert.False(t, errors.Is(err, tools.ErrInvalidArgs))
	assert.Contains(t, err.Error(), "compute_factor_ic")
}

func TestNewComputeFactorICTool_NilClientPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewComputeFactorICTool(nil)
	})
}

func TestComputeFactorICTool_RegisterInRegistry(t *testing.T) {
	c := client.NewFactorClient("http://x")
	tt := NewComputeFactorICTool(c)
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tt))

	got, err := reg.Get("compute_factor_ic")
	require.NoError(t, err)
	assert.Equal(t, "compute_factor_ic", got.Name())

	info := reg.List()
	require.Len(t, info, 1)
	assert.Equal(t, "compute_factor_ic", info[0].Name)
	assert.Len(t, info[0].Parameters, 4)
}

// ═══════════════════════════════════════════════════════════════════════
//  Cross-tool: ValidateFactor + ComputeFactorIC end-to-end flow
// ═══════════════════════════════════════════════════════════════════════

// TestPipeline_ValidateThenComputeIC simulates the L1→L2 gate flow that
// Hermes will use: validate syntax first, then (if valid) compute IC.
// This catches integration regressions early (e.g. a parser change that
// breaks both tools simultaneously).
func TestPipeline_ValidateThenComputeIC(t *testing.T) {
	validateTool := NewValidateFactorTool()
	srv := newFactorTestServer()
	defer srv.close()
	icTool := NewComputeFactorICTool(client.NewFactorClient(srv.server.URL))

	args := map[string]interface{}{
		"expression": "ts_rank(close, 20)",
		"symbols":    []string{"000001.SZ"},
		"start_date": "2022-01-01",
		"end_date":   "2024-01-01",
	}

	// L1: validate
	vResult, err := validateTool.Execute(context.Background(), args)
	require.NoError(t, err)
	vMap := vResult.(map[string]interface{})
	require.True(t, vMap["valid"].(bool), "L1 gate: expression should validate")

	// L2: compute IC (only because L1 passed)
	icResult, err := icTool.Execute(context.Background(), args)
	require.NoError(t, err)
	m := icResult.(*client.FactorMetrics)
	assert.InDelta(t, 0.045, m.IC, 1e-9)
}
