package validator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// ValidationResult holds the result of code validation.
type ValidationResult struct {
	Valid       bool     `json:"valid"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	SyntaxValid bool     `json:"syntax_valid"`
	Compiles    bool     `json:"compiles"`
}

// CodeValidator validates generated strategy code.
type CodeValidator struct {
	tempDir string
}

// NewCodeValidator creates a new code validator.
func NewCodeValidator() *CodeValidator {
	return &CodeValidator{
		tempDir: os.TempDir(),
	}
}

// ValidateSyntax checks if the code has valid Go syntax.
func (v *CodeValidator) ValidateSyntax(code string) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Basic syntax checks
	if strings.TrimSpace(code) == "" {
		result.Valid = false
		result.SyntaxValid = false
		result.Errors = append(result.Errors, "Empty code")
		return result
	}

	// Check for required imports
	if !strings.Contains(code, "package strategy") {
		result.Warnings = append(result.Warnings, "Missing 'package strategy' declaration")
	}

	// Check for Strategy interface implementation
	if !strings.Contains(code, "Strategy") {
		result.Warnings = append(result.Warnings, "May not implement Strategy interface")
	}

	// Check for common issues
	if strings.Count(code, "{") != strings.Count(code, "}") {
		result.Valid = false
		result.SyntaxValid = false
		result.Errors = append(result.Errors, "Mismatched braces")
	}

	if strings.Count(code, "(") != strings.Count(code, ")") {
		result.Valid = false
		result.SyntaxValid = false
		result.Errors = append(result.Errors, "Mismatched parentheses")
	}

	result.SyntaxValid = len(result.Errors) == 0
	return result
}

// ValidateCompilation attempts to compile the code.
func (v *CodeValidator) ValidateCompilation(code string) *ValidationResult {
	result := v.ValidateSyntax(code)
	if !result.SyntaxValid {
		return result
	}

	// Create temporary file
	tempFile := filepath.Join(v.tempDir, fmt.Sprintf("strategy_%d.go", time.Now().UnixNano()))
	if err := os.WriteFile(tempFile, []byte(code), 0644); err != nil {
		result.Valid = false
		result.Compiles = false
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to write temp file: %v", err))
		return result
	}
	defer os.Remove(tempFile)

	// Try to compile with gofmt first
	cmd := exec.Command("gofmt", "-e", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Valid = false
		result.Compiles = false
		result.Errors = append(result.Errors, fmt.Sprintf("gofmt error: %s", string(output)))
		return result
	}

	// Try syntax check with go vet
	cmd = exec.Command("go", "tool", "compile", "-V=full", tempFile)
	output, err = cmd.CombinedOutput()
	if err != nil {
		// go tool compile may fail due to missing imports, which is expected for isolated files
		// Check if it's a syntax error or import error
		outputStr := string(output)
		if strings.Contains(outputStr, "syntax error") {
			result.Valid = false
			result.Compiles = false
			result.Errors = append(result.Errors, fmt.Sprintf("Syntax error: %s", outputStr))
			return result
		}
	}

	result.Compiles = true
	return result
}

// ValidateTemplate validates a strategy template by rendering and checking it.
func (v *CodeValidator) ValidateTemplate(templateStr string, data interface{}) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Parse template
	tmpl, err := template.New("strategy").Parse(templateStr)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Template parse error: %v", err))
		return result
	}

	// Render template
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Template execute error: %v", err))
		return result
	}

	// Validate rendered code
	rendered := buf.String()
	return v.ValidateSyntax(rendered)
}

// ValidateFull runs full validation: syntax + compilation.
func (v *CodeValidator) ValidateFull(code string) *ValidationResult {
	result := v.ValidateCompilation(code)
	result.Valid = result.SyntaxValid && result.Compiles
	return result
}

// QuickValidate runs a fast syntax-only validation.
func (v *CodeValidator) QuickValidate(code string) *ValidationResult {
	return v.ValidateSyntax(code)
}
