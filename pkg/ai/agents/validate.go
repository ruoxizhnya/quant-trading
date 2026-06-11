package agents

import (
	"context"
	"fmt"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/client"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/gene_pool"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/metrics"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/validator"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// ValidationLevel defines the depth of validation.
type ValidationLevel int

const (
	// L1Syntax validates formula syntax only.
	L1Syntax ValidationLevel = 1
	// L2QuickEval validates with quick evaluation on sample data.
	L2QuickEval ValidationLevel = 2
	// L3StandardBacktest runs full backtest.
	L3StandardBacktest ValidationLevel = 3
	// L4WalkForward runs walk-forward validation.
	L4WalkForward ValidationLevel = 4
)

// ValidationResult holds the result of factor validation.
type ValidationResult struct {
	Level       ValidationLevel `json:"level"`
	Passed      bool            `json:"passed"`
	Score       float64         `json:"score"`
	IC          float64         `json:"ic"`
	IR          float64         `json:"ir"`
	Turnover    float64         `json:"turnover"`
	Sharpe      float64         `json:"sharpe"`
	Errors      []string        `json:"errors,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
	Formula     string          `json:"formula"`
	Inputs      []string        `json:"inputs"`
	Description string          `json:"description"`
}

// ValidateAgent validates factor hypotheses through multiple levels.
type ValidateAgent struct {
	parser       *expression.Parser
	evaluator    *expression.Evaluator
	icCalc       *metrics.ICCalculator
	turnoverCalc *metrics.TurnoverCalculator
	dataProvider expression.DataProvider
	btClient     *client.BacktestClient
	codeValidator *validator.CodeValidator
}

// NewValidateAgent creates a new ValidateAgent.
func NewValidateAgent(dataProvider expression.DataProvider) *ValidateAgent {
	return &ValidateAgent{
		parser:        expression.NewParser(),
		icCalc:        metrics.NewICCalculator(),
		turnoverCalc:  metrics.NewTurnoverCalculator(),
		dataProvider:  dataProvider,
		codeValidator: validator.NewCodeValidator(),
	}
}

// NewValidateAgentWithBacktest creates a ValidateAgent with backtest client.
func NewValidateAgentWithBacktest(dataProvider expression.DataProvider, btClient *client.BacktestClient) *ValidateAgent {
	return &ValidateAgent{
		parser:        expression.NewParser(),
		icCalc:        metrics.NewICCalculator(),
		turnoverCalc:  metrics.NewTurnoverCalculator(),
		dataProvider:  dataProvider,
		btClient:      btClient,
		codeValidator: validator.NewCodeValidator(),
	}
}

// Validate runs validation at the specified level.
func (a *ValidateAgent) Validate(ctx context.Context, formula string, level ValidationLevel) (*ValidationResult, error) {
	result := &ValidationResult{
		Level:   level,
		Formula: formula,
		Errors:  []string{},
		Warnings: []string{},
	}

	// L1: Syntax validation
	if err := a.validateL1(result); err != nil {
		return result, nil // Return with errors, don't fail
	}

	if level >= L2QuickEval {
		if err := a.validateL2(ctx, result); err != nil {
			return result, nil
		}
	}

	if level >= L3StandardBacktest {
		if err := a.validateL3(ctx, result); err != nil {
			return result, nil
		}
	}

	if level >= L4WalkForward {
		if err := a.validateL4(ctx, result); err != nil {
			return result, nil
		}
	}

	result.Passed = len(result.Errors) == 0 && result.Score > 0
	return result, nil
}

// ValidateGene validates a FactorGene and updates its metrics.
func (a *ValidateAgent) ValidateGene(ctx context.Context, gene *gene_pool.FactorGene, level ValidationLevel) (*ValidationResult, error) {
	result, err := a.Validate(ctx, gene.Formula, level)
	if err != nil {
		return nil, err
	}

	// Update gene with validation results
	gene.IC = result.IC
	gene.IR = result.IR
	gene.Turnover = result.Turnover
	gene.Sharpe = result.Sharpe
	gene.Fitness = result.Score

	if result.Passed {
		gene.Status = "validated"
	} else {
		gene.Status = "rejected"
	}

	return result, nil
}

// validateL1 performs syntax validation.
func (a *ValidateAgent) validateL1(result *ValidationResult) error {
	expr, err := a.parser.Parse(result.Formula)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Parse error: %v", err))
		return err
	}

	result.Inputs = expr.Inputs

	if err := expr.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Validation error: %v", err))
		return err
	}

	// Check for common issues
	if len(expr.Inputs) == 0 {
		result.Warnings = append(result.Warnings, "Formula has no data inputs (pure literal)")
	}

	// Check formula complexity
	complexity := estimateComplexity(expr.AST)
	if complexity > 10 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("High complexity: %d nodes", complexity))
	}

	result.Score = 1.0 // Base score for valid syntax
	return nil
}

// validateL2 performs quick evaluation validation.
func (a *ValidateAgent) validateL2(ctx context.Context, result *ValidationResult) error {
	if a.dataProvider == nil {
		result.Warnings = append(result.Warnings, "No data provider available for L2 validation")
		return fmt.Errorf("no data provider")
	}

	a.evaluator = expression.NewEvaluator(a.dataProvider)

	expr, err := a.parser.Parse(result.Formula)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Parse error: %v", err))
		return err
	}

	// Evaluate on sample data
	evalResult, err := a.evaluator.Evaluate(expr.AST, 0)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Evaluation error: %v", err))
		return err
	}

	// Check for NaN/Inf in results
	nanCount := 0
	totalCount := 0
	for _, values := range evalResult {
		for _, v := range values {
			totalCount++
			if math.IsNaN(v) || math.IsInf(v, 0) {
				nanCount++
			}
		}
	}

	if totalCount > 0 {
		nanRatio := float64(nanCount) / float64(totalCount)
		if nanRatio > 0.5 {
			result.Errors = append(result.Errors, fmt.Sprintf("Too many NaN/Inf values: %.1f%%", nanRatio*100))
		} else if nanRatio > 0.1 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Elevated NaN/Inf values: %.1f%%", nanRatio*100))
		}
	}

	// Check result variance
	variance := computeVariance(evalResult)
	if variance == 0 {
		result.Errors = append(result.Errors, "Zero variance in factor values")
	} else if variance < 1e-10 {
		result.Warnings = append(result.Warnings, "Very low variance in factor values")
	}

	result.Score = 2.0 // Base score for successful evaluation
	return nil
}

// validateL3 performs standard backtest validation.
func (a *ValidateAgent) validateL3(ctx context.Context, result *ValidationResult) error {
	if a.btClient == nil {
		result.Warnings = append(result.Warnings, "No backtest client available for L3 validation")
		result.Score = 3.0
		return fmt.Errorf("no backtest client")
	}

	// Run quick backtest via API
	req := client.BacktestRequest{
		StrategyName: result.Formula,
		StockPool:    []string{"AAPL", "GOOGL", "MSFT"},
		StartDate:    "2023-01-01",
		EndDate:      "2024-01-01",
	}

	btResult, err := a.btClient.RunBacktest(ctx, req)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Backtest failed: %v", err))
		result.Score = 3.0
		return err
	}

	if btResult != nil {
		result.Sharpe = btResult.SharpeRatio
		result.Score = 3.0 + btResult.SharpeRatio
	}

	return nil
}

// validateL4 performs walk-forward validation.
func (a *ValidateAgent) validateL4(ctx context.Context, result *ValidationResult) error {
	// L4 requires walk-forward engine - placeholder for now
	result.Warnings = append(result.Warnings, "L4 validation requires walk-forward integration")
	result.Score = 4.0
	return nil
}

// estimateComplexity counts AST nodes.
func estimateComplexity(node expression.Node) int {
	if node == nil {
		return 0
	}

	count := 1
	for _, child := range node.Children() {
		count += estimateComplexity(child)
	}
	return count
}

// computeVariance calculates the sample (n-1) variance of all finite
// evaluation results across runs.
//
// The NaN/Inf filter is preserved here (legacy behaviour) and the
// remaining math is delegated to pkg/statistics (ODR-013 P1-21).
func computeVariance(results map[string][]float64) float64 {
	var allValues []float64
	for _, values := range results {
		for _, v := range values {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				allValues = append(allValues, v)
			}
		}
	}
	return statistics.SampleVariance(allValues)
}

// ComputeFitness calculates a composite fitness score.
func (a *ValidateAgent) ComputeFitness(ic, ir, turnover, sharpe float64) float64 {
	// Composite fitness: higher IC, higher IR, lower turnover, higher Sharpe
	// Normalize each component
	icScore := math.Max(0, ic) * 100       // IC typically 0.01-0.1
	irScore := math.Max(0, ir) * 50        // IR typically 0.1-1.0
	turnoverScore := math.Max(0, 1-turnover) * 20 // Lower turnover is better
	sharpeScore := math.Max(0, sharpe) * 30 // Sharpe typically 0.5-2.0

	fitness := icScore + irScore + turnoverScore + sharpeScore
	return math.Min(fitness, 100.0) // Cap at 100
}

// BatchValidate validates multiple genes at once.
func (a *ValidateAgent) BatchValidate(ctx context.Context, genes []*gene_pool.FactorGene, level ValidationLevel) ([]*ValidationResult, error) {
	var results []*ValidationResult

	for _, gene := range genes {
		result, err := a.ValidateGene(ctx, gene, level)
		if err != nil {
			// Continue with other genes even if one fails
			result = &ValidationResult{
				Level:   level,
				Passed:  false,
				Formula: gene.Formula,
				Errors:  []string{fmt.Sprintf("Validation error: %v", err)},
			}
		}
		results = append(results, result)
	}

	return results, nil
}
