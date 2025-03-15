package errors

import (
	"errors"
	"fmt"
)

// Standard error types to use throughout the application
var (
	ErrNotFound           = errors.New("resource not found")
	ErrInvalidInput       = errors.New("invalid input")
	ErrServiceUnavailable = errors.New("service unavailable")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrInternal           = errors.New("internal error")
	ErrTimeout            = errors.New("timeout error")
	ErrPartialFailure     = errors.New("partial failure")
)

// ErrorType represents the type of error
type ErrorType string

const (
	// Error types
	TypeNotFound           ErrorType = "not_found"
	TypeInvalidInput       ErrorType = "invalid_input"
	TypeServiceUnavailable ErrorType = "service_unavailable"
	TypeUnauthorized       ErrorType = "unauthorized"
	TypeForbidden          ErrorType = "forbidden"
	TypeInternal           ErrorType = "internal"
	TypeTimeout            ErrorType = "timeout"
	TypePartialFailure     ErrorType = "partial_failure"
)

// AppError is a custom error type that provides additional context
type AppError struct {
	Type    ErrorType
	Message string
	Err     error
	Fields  map[string]interface{}
}

// Error returns the error message
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the wrapped error
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithField adds a field to the error
func (e *AppError) WithField(key string, value interface{}) *AppError {
	if e.Fields == nil {
		e.Fields = make(map[string]interface{})
	}
	e.Fields[key] = value
	return e
}

// WithFields adds multiple fields to the error
func (e *AppError) WithFields(fields map[string]interface{}) *AppError {
	if e.Fields == nil {
		e.Fields = make(map[string]interface{})
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// New creates a new AppError
func New(errType ErrorType, message string, err error) *AppError {
	return &AppError{
		Type:    errType,
		Message: message,
		Err:     err,
	}
}

// Wrap wraps an error with additional message and error type
func Wrap(err error, message string, errType ErrorType) *AppError {
	return &AppError{
		Type:    errType,
		Message: message,
		Err:     err,
	}
}

// Is determines if target error matches one of our standard errors
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}
