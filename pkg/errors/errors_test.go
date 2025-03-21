package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppError(t *testing.T) {
	// Test creating an error without a wrapped error
	t.Run("ErrorWithoutWrapped", func(t *testing.T) {
		err := New(TypeNotFound, "resource not found", nil)
		assert.Equal(t, TypeNotFound, err.Type)
		assert.Equal(t, "resource not found", err.Message)
		assert.Nil(t, err.Err)
		assert.Equal(t, "resource not found", err.Error())
	})

	// Test creating an error with a wrapped error
	t.Run("ErrorWithWrapped", func(t *testing.T) {
		baseErr := fmt.Errorf("original error")
		err := New(TypeInternal, "internal error occurred", baseErr)
		assert.Equal(t, TypeInternal, err.Type)
		assert.Equal(t, "internal error occurred", err.Message)
		assert.Equal(t, baseErr, err.Err)
		assert.Equal(t, "internal error occurred: original error", err.Error())
	})
}

func TestWrap(t *testing.T) {
	// Test wrapping a simple error
	t.Run("WrapSimpleError", func(t *testing.T) {
		baseErr := fmt.Errorf("database connection failed")
		err := Wrap(baseErr, "could not query database", TypeServiceUnavailable)
		assert.Equal(t, TypeServiceUnavailable, err.Type)
		assert.Equal(t, "could not query database", err.Message)
		assert.Equal(t, baseErr, err.Err)
		assert.Equal(t, "could not query database: database connection failed", err.Error())
	})

	// Test wrapping a nil error
	t.Run("WrapNilError", func(t *testing.T) {
		err := Wrap(nil, "no error to wrap", TypeInvalidInput)
		assert.Equal(t, TypeInvalidInput, err.Type)
		assert.Equal(t, "no error to wrap", err.Message)
		assert.Nil(t, err.Err)
		assert.Equal(t, "no error to wrap", err.Error())
	})

	// Test wrapping an AppError
	t.Run("WrapAppError", func(t *testing.T) {
		baseErr := New(TypeTimeout, "operation timed out", nil)
		err := Wrap(baseErr, "database query failed", TypeServiceUnavailable)
		assert.Equal(t, TypeServiceUnavailable, err.Type)
		assert.Equal(t, "database query failed", err.Message)
		assert.Equal(t, baseErr, err.Err)
		assert.Equal(t, "database query failed: operation timed out", err.Error())
	})
}

func TestWithField(t *testing.T) {
	err := New(TypeInvalidInput, "validation error", nil)

	// Add a single field
	err.WithField("field", "username")
	assert.Contains(t, err.Fields, "field")
	assert.Equal(t, "username", err.Fields["field"])

	// Add another field
	err.WithField("code", 400)
	assert.Contains(t, err.Fields, "code")
	assert.Equal(t, 400, err.Fields["code"])

	// Test chaining
	err2 := New(TypeInternal, "database error", nil).
		WithField("table", "users").
		WithField("operation", "insert")

	assert.Contains(t, err2.Fields, "table")
	assert.Contains(t, err2.Fields, "operation")
	assert.Equal(t, "users", err2.Fields["table"])
	assert.Equal(t, "insert", err2.Fields["operation"])
}

func TestWithFields(t *testing.T) {
	err := New(TypeInvalidInput, "validation error", nil)

	// Add multiple fields
	fields := map[string]interface{}{
		"field":     "email",
		"code":      400,
		"attempted": "test@example",
	}
	err.WithFields(fields)

	assert.Contains(t, err.Fields, "field")
	assert.Contains(t, err.Fields, "code")
	assert.Contains(t, err.Fields, "attempted")
	assert.Equal(t, "email", err.Fields["field"])
	assert.Equal(t, 400, err.Fields["code"])
	assert.Equal(t, "test@example", err.Fields["attempted"])

	// Test merging fields
	additionalFields := map[string]interface{}{
		"new_field": "value",
		"field":     "updated_value", // This should override the previous value
	}
	err.WithFields(additionalFields)

	assert.Contains(t, err.Fields, "new_field")
	assert.Equal(t, "value", err.Fields["new_field"])
	assert.Equal(t, "updated_value", err.Fields["field"]) // Value should be updated
}

func TestUnwrap(t *testing.T) {
	baseErr := fmt.Errorf("original error")
	err := New(TypeInternal, "wrapped error", baseErr)

	unwrapped := err.Unwrap()
	assert.Equal(t, baseErr, unwrapped)
}

func TestErrorIs(t *testing.T) {
	// Test direct match
	t.Run("DirectMatch", func(t *testing.T) {
		err := New(TypeNotFound, "resource not found", ErrNotFound)
		assert.True(t, Is(err, ErrNotFound))
	})

	// Test wrapped match
	t.Run("WrappedMatch", func(t *testing.T) {
		innerErr := fmt.Errorf("inner: %w", ErrInvalidInput)
		err := New(TypeInvalidInput, "outer", innerErr)
		assert.True(t, Is(err, ErrInvalidInput))
	})

	// Test non-match
	t.Run("NonMatch", func(t *testing.T) {
		err := New(TypeTimeout, "timeout error", nil)
		assert.False(t, Is(err, ErrNotFound))
	})
}

func TestErrorAs(t *testing.T) {
	// Test direct match
	t.Run("DirectMatch", func(t *testing.T) {
		original := New(TypeNotFound, "test error", nil)
		err := fmt.Errorf("wrapped: %w", original)

		var appErr *AppError
		assert.True(t, As(err, &appErr))
		assert.Equal(t, original, appErr)
	})

	// Test non-match
	t.Run("NonMatch", func(t *testing.T) {
		err := errors.New("standard error")

		var appErr *AppError
		assert.False(t, As(err, &appErr))
	})
}
