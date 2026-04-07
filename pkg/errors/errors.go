package errors

import (
	"fmt"
	"runtime"
)

// ErrorCode represents a categorical error code for programmatic handling.
type ErrorCode string

const (
	// ErrCodeInternal indicates an internal system error.
	ErrCodeInternal ErrorCode = "INTERNAL"
	// ErrCodeInvalidInput indicates invalid input parameters.
	ErrCodeInvalidInput ErrorCode = "INVALID_INPUT"
	// ErrCodeNotFound indicates resource not found.
	ErrCodeNotFound ErrorCode = "NOT_FOUND"
	// ErrCodeConflict indicates a conflict (e.g., duplicate resource).
	ErrCodeConflict ErrorCode = "CONFLICT"
	// ErrCodeUnavailable indicates service unavailable (e.g., external API down).
	ErrCodeUnavailable ErrorCode = "UNAVAILABLE"
	// ErrCodeTimeout indicates operation timeout.
	ErrCodeTimeout ErrorCode = "TIMEOUT"
	// ErrCodePermission indicates insufficient permissions.
	ErrCodePermission ErrorCode = "PERMISSION"
	// ErrCodeRateLimit indicates rate limit exceeded.
	ErrCodeRateLimit ErrorCode = "RATE_LIMIT"
	// ErrCodeDataQuality indicates data quality issues (insufficient, stale, etc).
	ErrCodeDataQuality ErrorCode = "DATA_QUALITY"
)

// AppError is a structured application error with code, message, and context.
type AppError struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	Operation  string    `json:"operation,omitempty"`
	Cause      error     `json:"-"`
	StackTrace string    `json:"stack_trace,omitempty"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v (op=%s)", e.Code, e.Message, e.Cause, e.Operation)
	}
	return fmt.Sprintf("[%s] %s (op=%s)", e.Code, e.Message, e.Operation)
}

// Unwrap implements the errors.Unwrap interface for error chaining.
func (e *AppError) Unwrap() error {
	return e.Cause
}

// Is checks if the target error matches this AppError by code or message.
func (e *AppError) Is(target error) bool {
	if t, ok := target.(*AppError); ok {
		return t.Code == e.Code
	}
	return false
}

// New creates a new AppError with the given code and message.
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:      code,
		Message:   message,
		StackTrace: captureStack(),
	}
}

// Wrap wraps an existing error with additional context (code, message, operation).
func Wrap(err error, code ErrorCode, message string, operation string) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{
		Code:       code,
		Message:    message,
		Operation:  operation,
		Cause:      err,
		StackTrace: captureStack(),
	}
}

// Wrapf wraps an existing error with formatted message.
func Wrapf(err error, code ErrorCode, operation string, format string, args ...interface{}) *AppError {
	if err == nil {
		return nil
	}
	message := fmt.Sprintf(format, args...)
	return &AppError{
		Code:       code,
		Message:    message,
		Operation:  operation,
		Cause:      err,
		StackTrace: captureStack(),
	}
}

// InvalidInput creates an INVALID_INPUT error with optional cause.
func InvalidInput(message string, operation string) *AppError {
	return New(ErrCodeInvalidInput, message).WithOperation(operation)
}

// NotFound creates a NOT_FOUND error.
func NotFound(resource string, id string) *AppError {
	return New(ErrCodeNotFound, fmt.Sprintf("%s not found: %s", resource, id))
}

// Internal creates an INTERNAL error wrapping an existing error.
func Internal(err error, operation string) *AppError {
	return Wrap(err, ErrCodeInternal, "internal error", operation)
}

// Unavailable creates an UNAVAILABLE error for external service failures.
func Unavailable(service string, err error) *AppError {
	return Wrap(err, ErrCodeUnavailable, fmt.Sprintf("%s service unavailable", service), "external_call")
}

// Timeout creates a TIMEOUT error.
func Timeout(operation string, err error) *AppError {
	return Wrap(err, ErrCodeTimeout, fmt.Sprintf("operation timeout: %s", operation), operation)
}

// DataQuality creates a DATA_QUALITY error.
func DataQuality(message string, operation string) *AppError {
	return New(ErrCodeDataQuality, message).WithOperation(operation)
}

// WithOperation adds operation context to the error.
func (e *AppError) WithOperation(op string) *AppError {
	e.Operation = op
	return e
}

// WithCause adds a wrapped cause to the error.
func (e *AppError) WithCause(cause error) *AppError {
	e.Cause = cause
	return e
}

// ToMap converts the error to a map for JSON serialization (without stack trace).
func (e *AppError) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"code":    e.Code,
		"message": e.Message,
		"op":      e.Operation,
	}
}

// captureStack captures the current goroutine's stack trace (skip 2 frames).
func captureStack() string {
	buf := make([]byte, 2048)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}