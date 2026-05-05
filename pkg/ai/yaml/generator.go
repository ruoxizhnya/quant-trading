package yaml

import (
	"fmt"
	"strings"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/intent"
)

// Generator generates YAML configuration from strategy intents
type Generator struct {
	// indentSize controls the number of spaces per indentation level
	indentSize int
}

// NewGenerator creates a new YAML generator
func NewGenerator() *Generator {
	return &Generator{indentSize: 2}
}

// NewGeneratorWithIndent creates a new YAML generator with custom indentation
func NewGeneratorWithIndent(indent int) *Generator {
	return &Generator{indentSize: indent}
}

// Config represents a complete strategy configuration in YAML
type Config struct {
	Strategy     StrategyConfig     `yaml:"strategy"`
	Backtest     BacktestConfig     `yaml:"backtest"`
	Data         DataConfig         `yaml:"data"`
	Risk         RiskConfig         `yaml:"risk,omitempty"`
	Execution    ExecutionConfig    `yaml:"execution,omitempty"`
	Optimization OptimizationConfig `yaml:"optimization,omitempty"`
}

// StrategyConfig holds strategy-specific configuration
type StrategyConfig struct {
	Name        string                 `yaml:"name"`
	Type        string                 `yaml:"type"`
	Description string                 `yaml:"description"`
	Indicators  []string               `yaml:"indicators,omitempty"`
	Parameters  map[string]interface{} `yaml:"parameters"`
}

// BacktestConfig holds backtest settings
type BacktestConfig struct {
	StartDate      string  `yaml:"start_date"`
	EndDate        string  `yaml:"end_date"`
	InitialCapital float64 `yaml:"initial_capital"`
	CommissionRate float64 `yaml:"commission_rate"`
	SlippageRate   float64 `yaml:"slippage_rate"`
	RebalanceFreq  string  `yaml:"rebalance_frequency"`
}

// DataConfig holds data source configuration
type DataConfig struct {
	Universe    string   `yaml:"universe"`
	Timeframe   string   `yaml:"timeframe"`
	Providers   []string `yaml:"providers,omitempty"`
	AdjustPrice bool     `yaml:"adjust_price"`
}

// RiskConfig holds risk management settings
type RiskConfig struct {
	MaxPositions   int     `yaml:"max_positions,omitempty"`
	MaxDrawdown    float64 `yaml:"max_drawdown,omitempty"`
	StopLoss       float64 `yaml:"stop_loss,omitempty"`
	TakeProfit     float64 `yaml:"take_profit,omitempty"`
	PositionSizing string  `yaml:"position_sizing,omitempty"`
}

// ExecutionConfig holds execution settings
type ExecutionConfig struct {
	OrderType      string  `yaml:"order_type"`
	PriceTolerance float64 `yaml:"price_tolerance,omitempty"`
}

// OptimizationConfig holds parameter optimization settings
type OptimizationConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Method     string   `yaml:"method,omitempty"`
	MaxIter    int      `yaml:"max_iterations,omitempty"`
	Parameters []string `yaml:"parameters,omitempty"`
}

// Generate creates a YAML configuration from an intent
func (g *Generator) Generate(i *intent.Intent) string {
	if i == nil {
		return ""
	}

	config := g.intentToConfig(i)
	return g.configToYAML(config)
}

// GenerateWithOptions creates a YAML configuration with additional options
func (g *Generator) GenerateWithOptions(i *intent.Intent, opts GenerateOptions) string {
	if i == nil {
		return ""
	}

	config := g.intentToConfig(i)

	// Override with options
	if opts.StartDate != "" {
		config.Backtest.StartDate = opts.StartDate
	}
	if opts.EndDate != "" {
		config.Backtest.EndDate = opts.EndDate
	}
	if opts.InitialCapital > 0 {
		config.Backtest.InitialCapital = opts.InitialCapital
	}
	if opts.RebalanceFreq != "" {
		config.Backtest.RebalanceFreq = opts.RebalanceFreq
	}

	return g.configToYAML(config)
}

// GenerateOptions allows customizing the generated YAML
type GenerateOptions struct {
	StartDate      string
	EndDate        string
	InitialCapital float64
	RebalanceFreq  string
}

// intentToConfig converts an Intent to a Config struct
func (g *Generator) intentToConfig(i *intent.Intent) Config {
	// Build parameters map
	params := make(map[string]interface{})
	for _, p := range i.Parameters {
		params[p.Name] = p.Value
	}

	config := Config{
		Strategy: StrategyConfig{
			Name:        i.StrategyName,
			Type:        string(i.StrategyType),
			Description: i.Description,
			Indicators:  i.Indicators,
			Parameters:  params,
		},
		Backtest: BacktestConfig{
			StartDate:      "2020-01-01",
			EndDate:        "2024-01-01",
			InitialCapital: 1000000,
			CommissionRate: 0.00025,
			SlippageRate:   0.001,
			RebalanceFreq:  "daily",
		},
		Data: DataConfig{
			Universe:    i.Universe,
			Timeframe:   i.Timeframe,
			Providers:   []string{"postgres", "tushare"},
			AdjustPrice: true,
		},
		Execution: ExecutionConfig{
			OrderType:      "market",
			PriceTolerance: 0.01,
		},
		Optimization: OptimizationConfig{
			Enabled: false,
			Method:  "grid_search",
			MaxIter: 100,
		},
	}

	// Add risk constraints if present
	if i.RiskConstraints != nil {
		rc := i.RiskConstraints
		if rc.MaxPositions != nil {
			config.Risk.MaxPositions = *rc.MaxPositions
		}
		if rc.MaxDrawdown != nil {
			config.Risk.MaxDrawdown = *rc.MaxDrawdown
		}
		if rc.StopLoss != nil {
			config.Risk.StopLoss = *rc.StopLoss
		}
		if rc.TakeProfit != nil {
			config.Risk.TakeProfit = *rc.TakeProfit
		}
		if rc.PositionSizing != nil {
			config.Risk.PositionSizing = *rc.PositionSizing
		}
	}

	return config
}

// configToYAML converts a Config to YAML string
func (g *Generator) configToYAML(config Config) string {
	var b strings.Builder
	indent := strings.Repeat(" ", g.indentSize)

	// Strategy section
	b.WriteString("strategy:\n")
	b.WriteString(fmt.Sprintf("%sname: %s\n", indent, config.Strategy.Name))
	b.WriteString(fmt.Sprintf("%stype: %s\n", indent, config.Strategy.Type))
	b.WriteString(fmt.Sprintf("%sdescription: %s\n", indent, config.Strategy.Description))

	if len(config.Strategy.Indicators) > 0 {
		b.WriteString(fmt.Sprintf("%sindicators:\n", indent))
		for _, ind := range config.Strategy.Indicators {
			b.WriteString(fmt.Sprintf("%s%s- %s\n", indent, indent, ind))
		}
	}

	b.WriteString(fmt.Sprintf("%sparameters:\n", indent))
	for name, value := range config.Strategy.Parameters {
		b.WriteString(fmt.Sprintf("%s%s%s: %v\n", indent, indent, name, value))
	}

	// Backtest section
	b.WriteString("\nbacktest:\n")
	b.WriteString(fmt.Sprintf("%sstart_date: %s\n", indent, config.Backtest.StartDate))
	b.WriteString(fmt.Sprintf("%send_date: %s\n", indent, config.Backtest.EndDate))
	b.WriteString(fmt.Sprintf("%sinitial_capital: %.0f\n", indent, config.Backtest.InitialCapital))
	b.WriteString(fmt.Sprintf("%scommission_rate: %.5f\n", indent, config.Backtest.CommissionRate))
	b.WriteString(fmt.Sprintf("%sslippage_rate: %.3f\n", indent, config.Backtest.SlippageRate))
	b.WriteString(fmt.Sprintf("%srebalance_frequency: %s\n", indent, config.Backtest.RebalanceFreq))

	// Data section
	b.WriteString("\ndata:\n")
	b.WriteString(fmt.Sprintf("%suniverse: %s\n", indent, config.Data.Universe))
	b.WriteString(fmt.Sprintf("%stimeframe: %s\n", indent, config.Data.Timeframe))
	if len(config.Data.Providers) > 0 {
		b.WriteString(fmt.Sprintf("%sproviders:\n", indent))
		for _, provider := range config.Data.Providers {
			b.WriteString(fmt.Sprintf("%s%s- %s\n", indent, indent, provider))
		}
	}
	b.WriteString(fmt.Sprintf("%sadjust_price: %t\n", indent, config.Data.AdjustPrice))

	// Risk section
	hasRisk := config.Risk.MaxPositions > 0 || config.Risk.MaxDrawdown > 0 ||
		config.Risk.StopLoss > 0 || config.Risk.TakeProfit > 0 ||
		config.Risk.PositionSizing != ""

	if hasRisk {
		b.WriteString("\nrisk:\n")
		if config.Risk.MaxPositions > 0 {
			b.WriteString(fmt.Sprintf("%smax_positions: %d\n", indent, config.Risk.MaxPositions))
		}
		if config.Risk.MaxDrawdown > 0 {
			b.WriteString(fmt.Sprintf("%smax_drawdown: %.2f\n", indent, config.Risk.MaxDrawdown))
		}
		if config.Risk.StopLoss > 0 {
			b.WriteString(fmt.Sprintf("%sstop_loss: %.2f\n", indent, config.Risk.StopLoss))
		}
		if config.Risk.TakeProfit > 0 {
			b.WriteString(fmt.Sprintf("%stake_profit: %.2f\n", indent, config.Risk.TakeProfit))
		}
		if config.Risk.PositionSizing != "" {
			b.WriteString(fmt.Sprintf("%sposition_sizing: %s\n", indent, config.Risk.PositionSizing))
		}
	}

	// Execution section
	b.WriteString("\nexecution:\n")
	b.WriteString(fmt.Sprintf("%sorder_type: %s\n", indent, config.Execution.OrderType))
	if config.Execution.PriceTolerance > 0 {
		b.WriteString(fmt.Sprintf("%sprice_tolerance: %.2f\n", indent, config.Execution.PriceTolerance))
	}

	// Optimization section
	b.WriteString("\noptimization:\n")
	b.WriteString(fmt.Sprintf("%senabled: %t\n", indent, config.Optimization.Enabled))
	if config.Optimization.Enabled {
		if config.Optimization.Method != "" {
			b.WriteString(fmt.Sprintf("%smethod: %s\n", indent, config.Optimization.Method))
		}
		if config.Optimization.MaxIter > 0 {
			b.WriteString(fmt.Sprintf("%smax_iterations: %d\n", indent, config.Optimization.MaxIter))
		}
		if len(config.Optimization.Parameters) > 0 {
			b.WriteString(fmt.Sprintf("%sparameters:\n", indent))
			for _, param := range config.Optimization.Parameters {
				b.WriteString(fmt.Sprintf("%s%s- %s\n", indent, indent, param))
			}
		}
	}

	return b.String()
}

// Validate checks if a YAML config string is valid
func (g *Generator) Validate(yamlStr string) error {
	if yamlStr == "" {
		return fmt.Errorf("YAML string is empty")
	}

	requiredSections := []string{"strategy:", "backtest:", "data:"}
	for _, section := range requiredSections {
		if !strings.Contains(yamlStr, section) {
			return fmt.Errorf("missing required section: %s", section)
		}
	}

	requiredStrategyFields := []string{"name:", "type:", "description:"}
	for _, field := range requiredStrategyFields {
		if !strings.Contains(yamlStr, field) {
			return fmt.Errorf("missing required strategy field: %s", field)
		}
	}

	return nil
}

// GenerateMinimal creates a minimal YAML configuration for quick testing
func (g *Generator) GenerateMinimal(i *intent.Intent) string {
	if i == nil {
		return ""
	}

	var b strings.Builder
	indent := strings.Repeat(" ", g.indentSize)

	b.WriteString("strategy:\n")
	b.WriteString(fmt.Sprintf("%sname: %s\n", indent, i.StrategyName))
	b.WriteString(fmt.Sprintf("%stype: %s\n", indent, i.StrategyType))
	b.WriteString(fmt.Sprintf("%sdescription: %s\n", indent, i.Description))

	if len(i.Parameters) > 0 {
		b.WriteString(fmt.Sprintf("%sparameters:\n", indent))
		for _, p := range i.Parameters {
			b.WriteString(fmt.Sprintf("%s%s%s: %v\n", indent, indent, p.Name, p.Value))
		}
	}

	b.WriteString("\nbacktest:\n")
	b.WriteString(fmt.Sprintf("%sstart_date: 2022-01-01\n", indent))
	b.WriteString(fmt.Sprintf("%send_date: 2024-01-01\n", indent))
	b.WriteString(fmt.Sprintf("%sinitial_capital: 1000000\n", indent))

	b.WriteString("\ndata:\n")
	b.WriteString(fmt.Sprintf("%suniverse: %s\n", indent, i.Universe))
	b.WriteString(fmt.Sprintf("%stimeframe: %s\n", indent, i.Timeframe))

	return b.String()
}

// MergeConfigs merges multiple YAML configurations into a single composite config
func (g *Generator) MergeConfigs(configs []string) (string, error) {
	if len(configs) == 0 {
		return "", fmt.Errorf("no configs to merge")
	}
	if len(configs) == 1 {
		return configs[0], nil
	}

	var b strings.Builder
	b.WriteString("# Composite Strategy Configuration\n")
	b.WriteString(fmt.Sprintf("# Merged from %d strategies\n\n", len(configs)))

	b.WriteString("composite:\n")
	indent := strings.Repeat(" ", g.indentSize)
	b.WriteString(fmt.Sprintf("%sname: merged_strategy\n", indent))
	b.WriteString(fmt.Sprintf("%stype: composite\n", indent))
	b.WriteString(fmt.Sprintf("%sdescription: Auto-generated composite strategy\n", indent))
	b.WriteString(fmt.Sprintf("%sstrategies:\n", indent))

	for i, config := range configs {
		b.WriteString(fmt.Sprintf("%s%s# Strategy %d\n", indent, indent, i+1))
		lines := strings.Split(config, "\n")
		for _, line := range lines {
			if line != "" {
				b.WriteString(fmt.Sprintf("%s%s%s\n", indent, indent, line))
			}
		}
	}

	return b.String(), nil
}
