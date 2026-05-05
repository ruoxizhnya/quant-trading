package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ruoxizhnya/quant-trading/pkg/ai"
)

// GenerateAgent generates trading strategy code from natural language descriptions.
type GenerateAgent struct {
	llm *ai.Client
}

// NewGenerateAgent creates a new generate agent.
func NewGenerateAgent() *GenerateAgent {
	return &GenerateAgent{
		llm: ai.NewClient(),
	}
}

// StrategyTemplate represents a generated strategy template.
type StrategyTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Code        string   `json:"code"`
	Params      []Param  `json:"params"`
	Factors     []string `json:"factors"`
	Confidence  float64  `json:"confidence"`
}

// Param represents a strategy parameter.
type Param struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Default     interface{} `json:"default"`
	Description string      `json:"description"`
}

// GenerateStrategy generates a strategy from a description.
func (a *GenerateAgent) GenerateStrategy(ctx context.Context, description string) (*StrategyTemplate, error) {
	if a.llm == nil || !a.llm.IsConfigured() {
		return nil, fmt.Errorf("AI client not configured")
	}

	prompt := fmt.Sprintf(`You are a quantitative strategy developer specializing in A-share market.

Strategy Description: "%s"

Generate a trading strategy with the following format:
1. Strategy Name (short, descriptive)
2. Type (momentum, mean_reversion, multi_factor, or custom)
3. Parameters (name, type, default, description)
4. Factor Formulas (list of factor expressions)
5. Strategy Logic (pseudocode for signal generation)

Output ONLY valid JSON:
{
  "name": "strategy_name",
  "type": "momentum",
  "params": [
    {"name": "lookback", "type": "int", "default": 20, "description": "Lookback period"}
  ],
  "factors": ["ts_mean(close, 20)", "ts_std(close, 60)"],
  "logic": "Buy when price > moving average"
}`, description)

	messages := []ai.ChatMessage{
		{Role: "system", Content: "You are a quantitative strategy developer. Output ONLY valid JSON."},
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	resp = cleanJSONResponse(resp)

	// Parse response
	template := &StrategyTemplate{
		ID:          generateStrategyID(),
		Name:        extractField(resp, "name"),
		Type:        extractField(resp, "type"),
		Description: description,
		Code:        extractField(resp, "logic"),
		Confidence:  0.7,
	}

	if template.Name == "" {
		template = a.generateFallbackStrategy(description)
	}

	return template, nil
}

// GenerateFromFactors generates a strategy that combines multiple factors.
func (a *GenerateAgent) GenerateFromFactors(ctx context.Context, factorFormulas []string, description string) (*StrategyTemplate, error) {
	if len(factorFormulas) == 0 {
		return nil, fmt.Errorf("no factors provided")
	}

	factorsStr := strings.Join(factorFormulas, "\n")
	prompt := fmt.Sprintf(`Create a multi-factor strategy using these factors:
%s

Requirements: %s

Generate:
1. Strategy name
2. Signal logic (how to combine factors)
3. Position sizing rules
4. Risk management rules

Output JSON with fields: name, type, logic, params, factors`, factorsStr, description)

	messages := []ai.ChatMessage{
		{Role: "system", Content: "You are a quantitative strategy developer."},
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	resp = cleanJSONResponse(resp)

	template := &StrategyTemplate{
		ID:          generateStrategyID(),
		Name:        extractField(resp, "name"),
		Type:        "multi_factor",
		Description: description,
		Code:        extractField(resp, "logic"),
		Factors:     factorFormulas,
		Confidence:  0.75,
	}

	if template.Name == "" {
		template.Name = fmt.Sprintf("MultiFactor_%d", strategyIDCounter)
		template.Code = "Equal-weighted factor combination"
	}

	return template, nil
}

// generateFallbackStrategy creates a simple fallback strategy.
func (a *GenerateAgent) generateFallbackStrategy(description string) *StrategyTemplate {
	return &StrategyTemplate{
		ID:          generateStrategyID(),
		Name:        "SimpleMomentum",
		Type:        "momentum",
		Description: description,
		Code:        "Buy when close > moving average",
		Params: []Param{
			{Name: "lookback", Type: "int", Default: 20, Description: "Lookback period for MA"},
			{Name: "threshold", Type: "float", Default: 0.0, Description: "Signal threshold"},
		},
		Factors:    []string{"ts_mean(close, 20)", "close"},
		Confidence: 0.5,
	}
}

// Strategy generation helpers
var (
	strategyIDCounter int
	strategyIDMutex   sync.Mutex
)

func generateStrategyID() string {
	strategyIDMutex.Lock()
	defer strategyIDMutex.Unlock()
	strategyIDCounter++
	return fmt.Sprintf("strategy_%d", strategyIDCounter)
}
