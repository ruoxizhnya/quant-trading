package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
)

// ResearchAgent generates factor hypotheses and validates them
type ResearchAgent struct {
	llm *ai.Client
}

// NewResearchAgent creates a new research agent
func NewResearchAgent() *ResearchAgent {
	return &ResearchAgent{
		llm: ai.NewClient(),
	}
}

// FactorHypothesis represents a generated factor hypothesis
type FactorHypothesis struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Formula     string   `json:"formula"`
	Category    string   `json:"category"`
	Confidence  float64  `json:"confidence"`
	Rationale   string   `json:"rationale"`
}

// GenerateHypothesis generates a factor hypothesis from a research topic
func (a *ResearchAgent) GenerateHypothesis(ctx context.Context, topic string) (*FactorHypothesis, error) {
	if a.llm == nil || !a.llm.IsConfigured() {
		return nil, fmt.Errorf("AI client not configured")
	}

	prompt := fmt.Sprintf(`You are a quantitative research analyst specializing in A-share market factors.

Research Topic: "%s"

Generate a factor hypothesis with the following format:
1. Factor Name (short, descriptive)
2. Category (momentum, value, quality, volatility, liquidity, or custom)
3. Formula (using the factor expression DSL)
4. Rationale (why this factor should work in A-share market)

Factor Expression DSL Syntax:
- Data fields: close, open, high, low, volume, turnover, market_cap, pe, pb, roe
- Time-series ops: ts_mean(x, window), ts_std(x, window), ts_delay(x, periods), ts_delta(x, periods), ts_pct_change(x, periods), ts_corr(x, y, window), ts_rank(x, window)
- Cross-sectional ops: cs_rank(x), cs_zscore(x), cs_percentile(x)
- Math ops: abs(x), log(x), sqrt(x), sign(x)
- Arithmetic: +, -, *, /, ^

Example formulas:
- Momentum: ts_pct_change(close, 20)
- Mean Reversion: cs_rank(ts_mean(close, 5) / ts_mean(close, 20))
- Volatility: ts_std(close, 20) / ts_mean(close, 20)
- Quality: roe / pe

Output ONLY valid JSON:
{
  "name": "factor_name",
  "category": "momentum",
  "formula": "ts_pct_change(close, 20)",
  "rationale": "explanation"
}`, topic)

	messages := []ai.ChatMessage{
		{Role: "system", Content: "You are a quantitative research analyst. Output ONLY valid JSON."},
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// Clean and parse response
	resp = cleanJSONResponse(resp)

	var hypothesis FactorHypothesis
	// Simple parsing - in production use json.Unmarshal
	// For now, create a structured response
	hypothesis = FactorHypothesis{
		ID:          generateID(),
		Name:        extractField(resp, "name"),
		Category:    extractField(resp, "category"),
		Formula:     extractField(resp, "formula"),
		Rationale:   extractField(resp, "rationale"),
		Confidence:  0.7,
		Description: topic,
	}

	if hypothesis.Formula == "" {
		// Fallback: generate a simple formula based on topic
		hypothesis = a.generateFallbackHypothesis(topic)
	}

	return &hypothesis, nil
}

// ValidateFormula validates a factor formula by parsing it
func (a *ResearchAgent) ValidateFormula(formula string) (*expression.Expression, error) {
	parser := expression.NewParser()
	expr, err := parser.Parse(formula)
	if err != nil {
		return nil, fmt.Errorf("formula validation failed: %w", err)
	}

	if err := expr.Validate(); err != nil {
		return nil, fmt.Errorf("expression validation failed: %w", err)
	}

	return expr, nil
}

// generateFallbackHypothesis creates a simple hypothesis when LLM fails
func (a *ResearchAgent) generateFallbackHypothesis(topic string) FactorHypothesis {
	lowerTopic := strings.ToLower(topic)

	switch {
	case strings.Contains(lowerTopic, "momentum") || strings.Contains(lowerTopic, "动量"):
		return FactorHypothesis{
			ID:          generateID(),
			Name:        "momentum_20d",
			Category:    "momentum",
			Formula:     "ts_pct_change(close, 20)",
			Rationale:   "20-day price momentum captures short-term trend persistence",
			Confidence:  0.6,
			Description: topic,
		}
	case strings.Contains(lowerTopic, "value") || strings.Contains(lowerTopic, "价值"):
		return FactorHypothesis{
			ID:          generateID(),
			Name:        "pe_ratio",
			Category:    "value",
			Formula:     "1 / pe",
			Rationale:   "Low PE ratio indicates potential undervaluation",
			Confidence:  0.6,
			Description: topic,
		}
	case strings.Contains(lowerTopic, "quality") || strings.Contains(lowerTopic, "质量"):
		return FactorHypothesis{
			ID:          generateID(),
			Name:        "roe_quality",
			Category:    "quality",
			Formula:     "roe",
			Rationale:   "High ROE indicates strong profitability",
			Confidence:  0.6,
			Description: topic,
		}
	case strings.Contains(lowerTopic, "volatility") || strings.Contains(lowerTopic, "波动"):
		return FactorHypothesis{
			ID:          generateID(),
			Name:        "volatility_20d",
			Category:    "volatility",
			Formula:     "ts_std(close, 20) / ts_mean(close, 20)",
			Rationale:   "Volatility normalization captures relative risk",
			Confidence:  0.6,
			Description: topic,
		}
	default:
		return FactorHypothesis{
			ID:          generateID(),
			Name:        "custom_factor",
			Category:    "custom",
			Formula:     "close / ts_mean(close, 20)",
			Rationale:   "Price relative to moving average",
			Confidence:  0.5,
			Description: topic,
		}
	}
}

// Helper functions

var (
	idCounter int
	idMutex   sync.Mutex
)

func generateID() string {
	// Simple ID generation - in production use UUID
	idMutex.Lock()
	defer idMutex.Unlock()
	idCounter++
	return fmt.Sprintf("factor_%d", idCounter)
}

func extractField(jsonStr, field string) string {
	// Simple field extraction - in production use proper JSON parsing
	searchStr := fmt.Sprintf("\"%s\":", field)
	idx := strings.Index(jsonStr, searchStr)
	if idx == -1 {
		return ""
	}
	start := idx + len(searchStr)
	// Skip whitespace and quotes
	for start < len(jsonStr) && (jsonStr[start] == ' ' || jsonStr[start] == '"') {
		start++
	}
	end := start
	for end < len(jsonStr) && jsonStr[end] != '"' && jsonStr[end] != '\n' && jsonStr[end] != ',' {
		end++
	}
	return strings.TrimSpace(jsonStr[start:end])
}

func cleanJSONResponse(resp string) string {
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	return strings.TrimSpace(resp)
}
