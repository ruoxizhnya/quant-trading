// Package yaml provides YAML configuration generation and parsing for
// AI-generated strategies. The Generator emits YAML from an intent; the
// Loader (this file) parses YAML back into structured config and can
// build an executable ExpressionStrategy from it.
//
// S7-P3-2 (ODR-043 Sprint 7): Added ParseConfig and LoadStrategy to
// enable the flow AI → YAML → ExpressionStrategy → backtest, bypassing
// the LLM Go-codegen + compile path for expression-based strategies.
package yaml

import (
	"fmt"

	yamlv3 "gopkg.in/yaml.v3"
)

// ParseConfig parses a YAML config string into a Config struct.
//
// This is the inverse of Generator.configToYAML: it accepts a YAML
// document matching the Config schema (strategy/backtest/data/risk/
// execution/optimization/expression) and returns the structured form.
// Required fields are validated: the strategy section and its name
// field must be present.
//
// Returns a clear error for:
//   - empty input
//   - malformed YAML (syntax errors)
//   - missing top-level strategy: section
//   - missing strategy.name field
//
// The Expression section is optional; if absent, the returned Config
// has a zero-valued Expression field. Callers that need an
// ExpressionStrategy should use LoadStrategy, which applies detection
// logic (expression section presence or strategy.type == "expression").
func ParseConfig(yamlStr string) (*Config, error) {
	if yamlStr == "" {
		return nil, fmt.Errorf("yaml: ParseConfig: input is empty")
	}

	var config Config
	if err := yamlv3.Unmarshal([]byte(yamlStr), &config); err != nil {
		return nil, fmt.Errorf("yaml: ParseConfig: %w", err)
	}

	// Validate required top-level section.
	if config.Strategy.Name == "" && config.Strategy.Type == "" && config.Strategy.Description == "" {
		return nil, fmt.Errorf("yaml: ParseConfig: missing required section: strategy:")
	}

	// Validate required strategy.name field. We treat name as the
	// primary identifier (it becomes the registry key downstream).
	if config.Strategy.Name == "" {
		return nil, fmt.Errorf("yaml: ParseConfig: missing required strategy field: name:")
	}

	return &config, nil
}
