package agents

import (
	"context"
	"encoding/json"
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

	// S7-P0-4 (ODR-043-4): parse with json.Unmarshal instead of the
	// former extractField string-scanner, which truncated values at
	// commas, escaped quotes, and newlines.
	parsed, err := parseStrategyTemplateJSON(resp)
	if err != nil || parsed.Name == "" {
		template := a.generateFallbackStrategy(description)
		return template, nil
	}

	template := &StrategyTemplate{
		ID:          generateStrategyID(),
		Name:        parsed.Name,
		Type:        parsed.Type,
		Description: description,
		Code:        parsed.Code,
		Params:      parsed.Params,
		Factors:     parsed.Factors,
		Confidence:  0.7,
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

	// S7-P0-4 (ODR-043-4): json.Unmarshal instead of extractField.
	parsed, err := parseStrategyTemplateJSON(resp)
	if err != nil || parsed.Name == "" {
		parsed.Name = fmt.Sprintf("MultiFactor_%d", strategyIDCounter)
		parsed.Code = "Equal-weighted factor combination"
	}

	template := &StrategyTemplate{
		ID:          generateStrategyID(),
		Name:        parsed.Name,
		Type:        "multi_factor",
		Description: description,
		Code:        parsed.Code,
		Factors:     factorFormulas,
		Confidence:  0.75,
	}

	return template, nil
}

// strategyTemplateJSON is the JSON shape the LLM is prompted to emit
// for strategy generation. "logic" maps to the Code field (the prompt
// asks for "logic" as the pseudocode field name).
//
// S7-P0-4 (ODR-043-4): replaces extractField, which could not parse
// arrays (factors/params) or values containing commas / escaped quotes.
type strategyTemplateJSON struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Logic   string   `json:"logic"`
	Params  []Param  `json:"params"`
	Factors []string `json:"factors"`
}

// parseStrategyTemplateJSON parses an LLM response into a
// StrategyTemplate using json.Unmarshal. The response may be wrapped
// in ```json markdown fences (stripped first). Returns an error if the
// response is not valid JSON so the caller can fall back.
//
// S7-P0-4 (ODR-043-4): replaces extractField in generate.go.
func parseStrategyTemplateJSON(resp string) (StrategyTemplate, error) {
	cleaned := cleanJSONResponse(resp)
	if cleaned == "" {
		return StrategyTemplate{}, fmt.Errorf("empty response after cleaning")
	}

	var parsed strategyTemplateJSON
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return StrategyTemplate{}, fmt.Errorf("parse strategy template JSON: %w", err)
	}

	return StrategyTemplate{
		Name:    parsed.Name,
		Type:    parsed.Type,
		Code:    parsed.Logic,
		Params:  parsed.Params,
		Factors: parsed.Factors,
	}, nil
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
