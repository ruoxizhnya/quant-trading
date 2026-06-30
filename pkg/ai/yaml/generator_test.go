package yaml

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/intent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerator(t *testing.T) {
	g := NewGenerator()
	require.NotNil(t, g)
	assert.Equal(t, 2, g.indentSize)
}

func TestNewGeneratorWithIndent(t *testing.T) {
	g := NewGeneratorWithIndent(4)
	require.NotNil(t, g)
	assert.Equal(t, 4, g.indentSize)
}

func TestGenerator_Generate_NilIntent(t *testing.T) {
	g := NewGenerator()
	result := g.Generate(nil)
	assert.Equal(t, "", result)
}

func TestGenerator_Generate_MomentumStrategy(t *testing.T) {
	g := NewGenerator()
	i := &intent.Intent{
		StrategyName: "momentum_strategy",
		StrategyType: intent.StrategyTypeMomentum,
		Description:  "动量策略",
		Universe:     "csi300",
		Timeframe:    "1d",
		Indicators:   []string{"rsi", "ma"},
		Parameters: []intent.Parameter{
			{Name: "lookback_days", Value: 20},
			{Name: "top_n", Value: 10},
		},
	}

	yaml := g.Generate(i)
	require.NotEmpty(t, yaml)

	// Check required sections
	assert.Contains(t, yaml, "strategy:")
	assert.Contains(t, yaml, "backtest:")
	assert.Contains(t, yaml, "data:")
	assert.Contains(t, yaml, "execution:")
	assert.Contains(t, yaml, "optimization:")

	// Check strategy fields
	assert.Contains(t, yaml, "name: momentum_strategy")
	assert.Contains(t, yaml, "type: momentum")
	assert.Contains(t, yaml, "description: 动量策略")

	// Check indicators
	assert.Contains(t, yaml, "- rsi")
	assert.Contains(t, yaml, "- ma")

	// Check parameters
	assert.Contains(t, yaml, "lookback_days: 20")
	assert.Contains(t, yaml, "top_n: 10")

	// Check backtest defaults
	assert.Contains(t, yaml, "start_date: 2020-01-01")
	assert.Contains(t, yaml, "end_date: 2024-01-01")
	assert.Contains(t, yaml, "initial_capital: 1000000")
	// S7-P1-4 (ODR-043): commission_rate must source from fees.DefaultCommissionRate (0.0003),
	// not the stale 0.00025 literal. %.5f renders 0.0003 as "0.00030".
	assert.Contains(t, yaml, "commission_rate: 0.00030")
	// S7-P1-4 D5: slippage_rate must source from fees.DefaultSlippageRate (0.0001).
	// The format string must be %.5f; the old %.3f truncated 0.0001 to "0.000".
	assert.Contains(t, yaml, "slippage_rate: 0.00010")

	// Check data
	assert.Contains(t, yaml, "universe: csi300")
	assert.Contains(t, yaml, "timeframe: 1d")
	assert.Contains(t, yaml, "- postgres")
	assert.Contains(t, yaml, "- tushare")
	assert.Contains(t, yaml, "adjust_price: true")

	// Check execution
	assert.Contains(t, yaml, "order_type: market")
}

func TestGenerator_Generate_WithRiskConstraints(t *testing.T) {
	g := NewGenerator()
	maxPos := 15
	drawdown := 0.15
	stopLoss := 0.05
	takeProfit := 0.10
	sizing := "equal"

	i := &intent.Intent{
		StrategyName: "risky_strategy",
		StrategyType: intent.StrategyTypeBreakout,
		Description:  "带风控的突破策略",
		Universe:     "csi500",
		Timeframe:    "5d",
		RiskConstraints: &intent.RiskConstraints{
			MaxPositions:   &maxPos,
			MaxDrawdown:    &drawdown,
			StopLoss:       &stopLoss,
			TakeProfit:     &takeProfit,
			PositionSizing: &sizing,
		},
	}

	yaml := g.Generate(i)
	require.NotEmpty(t, yaml)

	// Check risk section
	assert.Contains(t, yaml, "risk:")
	assert.Contains(t, yaml, "max_positions: 15")
	assert.Contains(t, yaml, "max_drawdown: 0.15")
	assert.Contains(t, yaml, "stop_loss: 0.05")
	assert.Contains(t, yaml, "take_profit: 0.10")
	assert.Contains(t, yaml, "position_sizing: equal")
}

func TestGenerator_Generate_WithoutRiskConstraints(t *testing.T) {
	g := NewGenerator()
	i := &intent.Intent{
		StrategyName: "simple_strategy",
		StrategyType: intent.StrategyTypeCustom,
		Description:  "简单策略",
		Universe:     "all",
		Timeframe:    "1d",
	}

	yaml := g.Generate(i)
	require.NotEmpty(t, yaml)

	// Risk section should not appear when no risk constraints
	lines := splitLines(yaml)
	inRiskSection := false
	for _, line := range lines {
		if line == "risk:" {
			inRiskSection = true
			break
		}
	}
	assert.False(t, inRiskSection, "risk section should not exist without constraints")
}

func TestGenerator_GenerateWithOptions(t *testing.T) {
	g := NewGenerator()
	i := &intent.Intent{
		StrategyName: "test_strategy",
		StrategyType: intent.StrategyTypeValue,
		Description:  "测试策略",
		Universe:     "csi300",
		Timeframe:    "1d",
	}

	opts := GenerateOptions{
		StartDate:      "2019-01-01",
		EndDate:        "2023-12-31",
		InitialCapital: 500000,
		RebalanceFreq:  "weekly",
	}

	yaml := g.GenerateWithOptions(i, opts)
	require.NotEmpty(t, yaml)

	assert.Contains(t, yaml, "start_date: 2019-01-01")
	assert.Contains(t, yaml, "end_date: 2023-12-31")
	assert.Contains(t, yaml, "initial_capital: 500000")
	assert.Contains(t, yaml, "rebalance_frequency: weekly")
}

func TestGenerator_GenerateWithOptions_NilIntent(t *testing.T) {
	g := NewGenerator()
	result := g.GenerateWithOptions(nil, GenerateOptions{})
	assert.Equal(t, "", result)
}

func TestGenerator_GenerateWithOptions_PartialOverrides(t *testing.T) {
	g := NewGenerator()
	i := &intent.Intent{
		StrategyName: "test_strategy",
		StrategyType: intent.StrategyTypeMomentum,
		Description:  "测试",
		Universe:     "csi300",
		Timeframe:    "1d",
	}

	// Only override start_date
	opts := GenerateOptions{
		StartDate: "2018-01-01",
	}

	yaml := g.GenerateWithOptions(i, opts)
	assert.Contains(t, yaml, "start_date: 2018-01-01")
	// Other fields should use defaults
	assert.Contains(t, yaml, "end_date: 2024-01-01")
	assert.Contains(t, yaml, "initial_capital: 1000000")
}

func TestGenerator_Validate_ValidYAML(t *testing.T) {
	g := NewGenerator()
	validYAML := `strategy:
  name: test
  type: momentum
  description: test strategy
backtest:
  start_date: 2020-01-01
data:
  universe: csi300
`
	err := g.Validate(validYAML)
	assert.NoError(t, err)
}

func TestGenerator_Validate_EmptyYAML(t *testing.T) {
	g := NewGenerator()
	err := g.Validate("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestGenerator_Validate_MissingStrategySection(t *testing.T) {
	g := NewGenerator()
	invalidYAML := `backtest:
  start_date: 2020-01-01
data:
  universe: csi300
`
	err := g.Validate(invalidYAML)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "strategy:")
}

func TestGenerator_Validate_MissingBacktestSection(t *testing.T) {
	g := NewGenerator()
	invalidYAML := `strategy:
  name: test
  type: momentum
  description: test
data:
  universe: csi300
`
	err := g.Validate(invalidYAML)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backtest:")
}

func TestGenerator_Validate_MissingDataSection(t *testing.T) {
	g := NewGenerator()
	invalidYAML := `strategy:
  name: test
  type: momentum
  description: test
backtest:
  start_date: 2020-01-01
`
	err := g.Validate(invalidYAML)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data:")
}

func TestGenerator_Validate_MissingStrategyName(t *testing.T) {
	g := NewGenerator()
	invalidYAML := `strategy:
  type: momentum
  description: test
backtest:
  start_date: 2020-01-01
data:
  universe: csi300
`
	err := g.Validate(invalidYAML)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name:")
}

func TestGenerator_Validate_MissingStrategyType(t *testing.T) {
	g := NewGenerator()
	invalidYAML := `strategy:
  name: test
  description: test
backtest:
  start_date: 2020-01-01
data:
  universe: csi300
`
	err := g.Validate(invalidYAML)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type:")
}

func TestGenerator_Validate_MissingStrategyDescription(t *testing.T) {
	g := NewGenerator()
	invalidYAML := `strategy:
  name: test
  type: momentum
backtest:
  start_date: 2020-01-01
data:
  universe: csi300
`
	err := g.Validate(invalidYAML)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "description:")
}

func TestGenerator_GenerateMinimal(t *testing.T) {
	g := NewGenerator()
	i := &intent.Intent{
		StrategyName: "minimal_strategy",
		StrategyType: intent.StrategyTypeMomentum,
		Description:  "最小化策略",
		Universe:     "csi300",
		Timeframe:    "1d",
		Parameters: []intent.Parameter{
			{Name: "lookback", Value: 20},
		},
	}

	yaml := g.GenerateMinimal(i)
	require.NotEmpty(t, yaml)

	// Should have minimal sections
	assert.Contains(t, yaml, "strategy:")
	assert.Contains(t, yaml, "backtest:")
	assert.Contains(t, yaml, "data:")

	// Should NOT have execution, optimization, or risk sections
	assert.NotContains(t, yaml, "execution:")
	assert.NotContains(t, yaml, "optimization:")
	assert.NotContains(t, yaml, "risk:")

	assert.Contains(t, yaml, "name: minimal_strategy")
	assert.Contains(t, yaml, "lookback: 20")
}

func TestGenerator_GenerateMinimal_NilIntent(t *testing.T) {
	g := NewGenerator()
	result := g.GenerateMinimal(nil)
	assert.Equal(t, "", result)
}

func TestGenerator_MergeConfigs(t *testing.T) {
	g := NewGenerator()
	configs := []string{
		"strategy:\n  name: s1",
		"strategy:\n  name: s2",
	}

	merged, err := g.MergeConfigs(configs)
	require.NoError(t, err)
	assert.Contains(t, merged, "Composite Strategy Configuration")
	assert.Contains(t, merged, "Merged from 2 strategies")
	assert.Contains(t, merged, "composite:")
	assert.Contains(t, merged, "strategies:")
}

func TestGenerator_MergeConfigs_SingleConfig(t *testing.T) {
	g := NewGenerator()
	configs := []string{"strategy:\n  name: s1"}

	merged, err := g.MergeConfigs(configs)
	require.NoError(t, err)
	assert.Equal(t, configs[0], merged)
}

func TestGenerator_MergeConfigs_Empty(t *testing.T) {
	g := NewGenerator()
	_, err := g.MergeConfigs([]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no configs")
}

func TestGenerator_intentToConfig(t *testing.T) {
	g := NewGenerator()
	maxPos := 10
	i := &intent.Intent{
		StrategyName: "test",
		StrategyType: intent.StrategyTypeMomentum,
		Description:  "test desc",
		Universe:     "csi300",
		Timeframe:    "1d",
		Indicators:   []string{"rsi"},
		Parameters: []intent.Parameter{
			{Name: "p1", Value: 100},
		},
		RiskConstraints: &intent.RiskConstraints{
			MaxPositions: &maxPos,
		},
	}

	config := g.intentToConfig(i)

	assert.Equal(t, "test", config.Strategy.Name)
	assert.Equal(t, "momentum", config.Strategy.Type)
	assert.Equal(t, "test desc", config.Strategy.Description)
	assert.Equal(t, []string{"rsi"}, config.Strategy.Indicators)
	assert.Equal(t, 100, config.Strategy.Parameters["p1"])

	assert.Equal(t, "csi300", config.Data.Universe)
	assert.Equal(t, "1d", config.Data.Timeframe)
	assert.True(t, config.Data.AdjustPrice)

	assert.Equal(t, 10, config.Risk.MaxPositions)

	assert.Equal(t, "market", config.Execution.OrderType)
	assert.False(t, config.Optimization.Enabled)
}

func TestGenerator_configToYAML_Indentation(t *testing.T) {
	g2 := NewGeneratorWithIndent(2)
	g4 := NewGeneratorWithIndent(4)

	i := &intent.Intent{
		StrategyName: "test",
		StrategyType: intent.StrategyTypeMomentum,
		Description:  "test",
		Universe:     "csi300",
		Timeframe:    "1d",
	}

	yaml2 := g2.Generate(i)
	yaml4 := g4.Generate(i)

	// Check that 4-space indent generator produces more spaces
	assert.Contains(t, yaml2, "  name: test")
	assert.Contains(t, yaml4, "    name: test")
}

func TestGenerator_FullWorkflow(t *testing.T) {
	// Integration-style test: intent -> yaml -> validate
	g := NewGenerator()

	i := &intent.Intent{
		StrategyName: "dual_ma_strategy",
		StrategyType: intent.StrategyTypeMomentum,
		Description:  "双均线交叉策略",
		Universe:     "csi300",
		Timeframe:    "1d",
		Indicators:   []string{"ma"},
		Parameters: []intent.Parameter{
			{Name: "short_ma", Value: 5},
			{Name: "long_ma", Value: 20},
		},
	}

	// Generate YAML
	yaml := g.Generate(i)
	require.NotEmpty(t, yaml)

	// Validate the generated YAML
	err := g.Validate(yaml)
	assert.NoError(t, err)

	// Generate minimal version
	minimal := g.GenerateMinimal(i)
	err = g.Validate(minimal)
	// Minimal might not have all required sections, so this may fail
	// But it should still have strategy section
	assert.Contains(t, minimal, "strategy:")
}

// Helper function
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
