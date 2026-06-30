package yaml

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseConfig_ValidFullYAML verifies that a complete YAML document
// with all sections (including the new expression section) parses
// correctly and every field is populated as expected.
func TestParseConfig_ValidFullYAML(t *testing.T) {
	yamlStr := `strategy:
  name: my_expr_strat
  type: expression
  description: test expression strategy
expression:
  signal:
    expression: "cs_rank(close) > 0.8"
    action: buy
    direction: long
    min_strength: 0.5
    lookback: 90
  sizing:
    method: equal
    fixed_weight: 0.05
    max_per_stock: 0.15
    max_total: 0.9
  risk:
    max_position_pct: 0.15
    max_open_positions: 25
    min_cash_buffer: 0.10
backtest:
  start_date: 2022-01-01
  end_date: 2024-01-01
  initial_capital: 1000000
data:
  universe: csi300
  timeframe: 1d
`
	config, err := ParseConfig(yamlStr)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Strategy section
	assert.Equal(t, "my_expr_strat", config.Strategy.Name)
	assert.Equal(t, "expression", config.Strategy.Type)
	assert.Equal(t, "test expression strategy", config.Strategy.Description)

	// Expression.Signal
	assert.Equal(t, "cs_rank(close) > 0.8", config.Expression.Signal.Expression)
	assert.Equal(t, "buy", config.Expression.Signal.Action)
	assert.Equal(t, "long", config.Expression.Signal.Direction)
	assert.Equal(t, 0.5, config.Expression.Signal.MinStrength)
	assert.Equal(t, 90, config.Expression.Signal.Lookback)

	// Expression.Sizing
	assert.Equal(t, "equal", config.Expression.Sizing.Method)
	assert.Equal(t, 0.05, config.Expression.Sizing.FixedWeight)
	assert.Equal(t, 0.15, config.Expression.Sizing.MaxPerStock)
	assert.Equal(t, 0.9, config.Expression.Sizing.MaxTotal)

	// Expression.Risk
	assert.Equal(t, 0.15, config.Expression.Risk.MaxPositionPct)
	assert.Equal(t, 25, config.Expression.Risk.MaxOpenPositions)
	assert.Equal(t, 0.10, config.Expression.Risk.MinCashBuffer)

	// Backtest section
	assert.Equal(t, "2022-01-01", config.Backtest.StartDate)
	assert.Equal(t, "csi300", config.Data.Universe)
}

// TestParseConfig_EmptyString verifies that an empty input is rejected
// with a clear error mentioning "empty".
func TestParseConfig_EmptyString(t *testing.T) {
	_, err := ParseConfig("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestParseConfig_MalformedYAML verifies that a syntactically invalid
// YAML document is rejected with a parse error.
func TestParseConfig_MalformedYAML(t *testing.T) {
	// Unclosed quote + bad indentation should fail yaml.v3 parsing.
	yamlStr := `strategy:
  name: "unterminated
  type: expression
`
	_, err := ParseConfig(yamlStr)
	require.Error(t, err)
}

// TestParseConfig_MissingStrategySection verifies that a YAML document
// without the required top-level strategy: section is rejected.
func TestParseConfig_MissingStrategySection(t *testing.T) {
	yamlStr := `backtest:
  start_date: 2022-01-01
data:
  universe: csi300
`
	_, err := ParseConfig(yamlStr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy:")
}

// TestParseConfig_NoExpressionSection verifies that a YAML document
// with strategy/backtest/data but WITHOUT an expression section parses
// successfully — the Expression field is simply zero-valued. This is
// the backward-compatible case: existing YAML without expression data
// still parses, and LoadStrategy (Phase 2) decides whether to error.
func TestParseConfig_NoExpressionSection(t *testing.T) {
	yamlStr := `strategy:
  name: simple_strategy
  type: momentum
  description: no expression here
backtest:
  start_date: 2022-01-01
data:
  universe: csi300
`
	config, err := ParseConfig(yamlStr)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "simple_strategy", config.Strategy.Name)
	// Expression section is absent → zero value.
	assert.Equal(t, "", config.Expression.Signal.Expression)
	assert.Equal(t, "", config.Expression.Sizing.Method)
	assert.Equal(t, 0, config.Expression.Risk.MaxOpenPositions)
}
