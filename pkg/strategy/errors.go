// Package strategy provides structured error types for strategy operations.
package strategy

import (
	"fmt"
)

// SignalErrorCode represents the type of signal error.
type SignalErrorCode string

const (
	// ErrInvalidParameter indicates a strategy parameter is out of range or invalid type.
	ErrInvalidParameter SignalErrorCode = "INVALID_PARAMETER"
	// ErrInsufficientData indicates not enough market data to generate signals.
	ErrInsufficientData SignalErrorCode = "INSUFFICIENT_DATA"
	// ErrCalculationFailure indicates a mathematical or logical error during signal generation.
	ErrCalculationFailure SignalErrorCode = "CALCULATION_FAILURE"
	// ErrExternalService indicates a failure calling an external service (e.g., screening API).
	ErrExternalService SignalErrorCode = "EXTERNAL_SERVICE_ERROR"
	// ErrConfiguration indicates the strategy is missing required configuration.
	ErrConfiguration SignalErrorCode = "CONFIGURATION_ERROR"
)

// SignalError is a structured error type for strategy operations.
// It provides error codes, field-level context, and human-readable messages.
type SignalError struct {
	Code    SignalErrorCode `json:"code"`
	Field   string          `json:"field,omitempty"`
	Message string          `json:"message"`
	Cause   error           `json:"-"`
}

// Error implements the error interface.
func (e *SignalError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Field, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for errors.Is/errors.As support.
func (e *SignalError) Unwrap() error {
	return e.Cause
}

// IsCode checks if the error matches the given code.
func (e *SignalError) IsCode(code SignalErrorCode) bool {
	return e.Code == code
}

// NewSignalError creates a new SignalError with the given code and message.
func NewSignalError(code SignalErrorCode, message string) *SignalError {
	return &SignalError{
		Code:    code,
		Message: message,
	}
}

// NewSignalErrorf creates a new SignalError with formatted message.
func NewSignalErrorf(code SignalErrorCode, format string, args ...interface{}) *SignalError {
	return &SignalError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// WrapSignalError wraps an existing error with a SignalError code.
func WrapSignalError(code SignalErrorCode, message string, cause error) *SignalError {
	return &SignalError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// InvalidParameterError creates an error for invalid parameter values.
func InvalidParameterError(field, message string) *SignalError {
	return &SignalError{
		Code:    ErrInvalidParameter,
		Field:   field,
		Message: message,
	}
}

// InsufficientDataError creates an error for missing market data.
func InsufficientDataError(message string) *SignalError {
	return &SignalError{
		Code:    ErrInsufficientData,
		Message: message,
	}
}

// ExternalServiceError creates an error for external service failures.
func ExternalServiceError(message string, cause error) *SignalError {
	return &SignalError{
		Code:    ErrExternalService,
		Message: message,
		Cause:   cause,
	}
}
