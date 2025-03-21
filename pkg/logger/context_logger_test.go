package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	appctx "project/pkg/context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Custom error type with fields for testing
type testError struct {
	msg    string
	fields map[string]interface{}
}

func (e *testError) Error() string {
	return e.msg
}

func (e *testError) Fields() map[string]interface{} {
	return e.fields
}

func TestFromContext(t *testing.T) {
	// Setup the logger
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	// Test with nil context
	t.Run("NilContext", func(t *testing.T) {
		buf.Reset()
		entry := FromContext(nil)
		entry.Info("test message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "test message", logMap["msg"])
		assert.Equal(t, "missing", logMap["context"])
	})

	// Test with populated context
	t.Run("PopulatedContext", func(t *testing.T) {
		buf.Reset()

		// Create a context with start time first
		ctx := appctx.NewContext(nil) // This sets the start time

		// Add context fields
		ctx = appctx.WithRequestID(ctx, "req-123")
		ctx = appctx.WithTraceID(ctx, "trace-456")
		ctx = appctx.WithUserID(ctx, "user-789")
		ctx = appctx.WithServiceName(ctx, "test-service")
		ctx = appctx.WithOperationName(ctx, "test-operation")

		// Sleep to make sure elapsed time is significant
		time.Sleep(5 * time.Millisecond)

		entry := FromContext(ctx)
		entry.Info("test message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "test message", logMap["msg"])
		assert.Equal(t, "req-123", logMap["request_id"])
		assert.Equal(t, "trace-456", logMap["trace_id"])
		assert.Equal(t, "user-789", logMap["user_id"])
		assert.Equal(t, "test-service", logMap["service"])
		assert.Equal(t, "test-operation", logMap["operation"])

		// Check if the elapsed_ms field exists, but don't assert a specific value
		// as it depends on timing and we can't predict it precisely
		_, ok := logMap["elapsed_ms"]
		assert.True(t, ok, "elapsed_ms field should exist")
	})
}

func TestLogWithContext(t *testing.T) {
	// Setup the logger
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	// Create a test context
	ctx := appctx.NewContext(nil)
	ctx = appctx.WithRequestID(ctx, "req-test")

	// Test various log level functions
	t.Run("DebugWithContext", func(t *testing.T) {
		buf.Reset()
		SetLevel("debug")
		DebugWithContext(ctx, "debug message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "debug message", logMap["msg"])
		assert.Equal(t, "debug", logMap["level"])
		assert.Equal(t, "req-test", logMap["request_id"])
	})

	t.Run("DebugfWithContext", func(t *testing.T) {
		buf.Reset()
		DebugfWithContext(ctx, "debug %s", "formatted")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "debug formatted", logMap["msg"])
	})

	t.Run("InfoWithContext", func(t *testing.T) {
		buf.Reset()
		InfoWithContext(ctx, "info message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "info message", logMap["msg"])
		assert.Equal(t, "info", logMap["level"])
	})

	t.Run("InfofWithContext", func(t *testing.T) {
		buf.Reset()
		InfofWithContext(ctx, "info %s", "formatted")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "info formatted", logMap["msg"])
	})

	t.Run("WarnWithContext", func(t *testing.T) {
		buf.Reset()
		WarnWithContext(ctx, "warn message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "warn message", logMap["msg"])
		assert.Equal(t, "warning", logMap["level"])
	})

	t.Run("WarnfWithContext", func(t *testing.T) {
		buf.Reset()
		WarnfWithContext(ctx, "warn %s", "formatted")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "warn formatted", logMap["msg"])
	})

	t.Run("ErrorWithContext", func(t *testing.T) {
		buf.Reset()
		ErrorWithContext(ctx, "error message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "error message", logMap["msg"])
		assert.Equal(t, "error", logMap["level"])
	})

	t.Run("ErrorfWithContext", func(t *testing.T) {
		buf.Reset()
		ErrorfWithContext(ctx, "error %s", "formatted")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "error formatted", logMap["msg"])
	})

	// Note: We're not testing Fatal* functions as they would exit the program
}

func TestWithContextError(t *testing.T) {
	// Setup the logger
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	// Create a test context
	ctx := appctx.NewContext(nil)

	// Test with standard error
	t.Run("StandardError", func(t *testing.T) {
		buf.Reset()
		err := errors.New("standard error")
		WithContextError(ctx, err).Error("error occurred")

		var logMap map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "error occurred", logMap["msg"])
		assert.Equal(t, "standard error", logMap["error"])
	})

	// Test with custom error that implements Fields()
	t.Run("CustomError", func(t *testing.T) {
		buf.Reset()
		customErr := &testError{
			msg: "custom error",
			fields: map[string]interface{}{
				"error_code": 500,
				"error_type": "internal",
			},
		}
		WithContextError(ctx, customErr).Error("error occurred")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "error occurred", logMap["msg"])
		assert.Equal(t, "custom error", logMap["error"])
		assert.Equal(t, float64(500), logMap["error_code"])
		assert.Equal(t, "internal", logMap["error_type"])
	})
}

func TestWithContextFields(t *testing.T) {
	// Setup the logger
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	// Create a test context
	ctx := appctx.NewContext(nil)
	ctx = appctx.WithRequestID(ctx, "req-fields")

	// Test adding fields
	fields := logrus.Fields{
		"custom_field1": "value1",
		"custom_field2": 42,
	}
	WithContextFields(ctx, fields).Info("message with fields")

	var logMap map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logMap)
	assert.NoError(t, err)
	assert.Equal(t, "message with fields", logMap["msg"])
	assert.Equal(t, "req-fields", logMap["request_id"])
	assert.Equal(t, "value1", logMap["custom_field1"])
	assert.Equal(t, float64(42), logMap["custom_field2"])
}

func TestLogOperation(t *testing.T) {
	// Setup the logger
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	// Create a test context
	ctx := appctx.NewContext(nil)

	// Test successful operation
	t.Run("SuccessfulOperation", func(t *testing.T) {
		buf.Reset()

		err := LogOperation(ctx, "test-operation", func(opCtx context.Context) error {
			// Check that operation name is set in context
			assert.Equal(t, "test-operation", appctx.GetOperationName(opCtx))
			return nil
		})

		assert.NoError(t, err)

		// The operation produces two log entries, so we need to split them
		lines := bytes.Split(buf.Bytes(), []byte("\n"))
		assert.Equal(t, 2, len(lines)-1) // -1 because last line is empty

		// Check start log
		var startLog map[string]interface{}
		err = json.Unmarshal(lines[0], &startLog)
		assert.NoError(t, err)
		assert.Equal(t, "Starting operation: test-operation", startLog["msg"])
		assert.Equal(t, "test-operation", startLog["operation"])

		// Check end log
		var endLog map[string]interface{}
		err = json.Unmarshal(lines[1], &endLog)
		assert.NoError(t, err)
		assert.Equal(t, "Operation completed", endLog["msg"])
		assert.Equal(t, "test-operation", endLog["operation"])
		assert.True(t, endLog["success"].(bool))
		assert.True(t, endLog["duration_ms"].(float64) >= 0)
	})

	// Test failed operation
	t.Run("FailedOperation", func(t *testing.T) {
		buf.Reset()

		testErr := errors.New("operation failed")
		err := LogOperation(ctx, "failed-operation", func(opCtx context.Context) error {
			return testErr
		})

		assert.Equal(t, testErr, err)

		// The operation produces two log entries
		lines := bytes.Split(buf.Bytes(), []byte("\n"))
		assert.Equal(t, 2, len(lines)-1)

		// Check end log
		var endLog map[string]interface{}
		err = json.Unmarshal(lines[1], &endLog)
		assert.NoError(t, err)
		assert.Equal(t, "Operation failed", endLog["msg"])
		assert.Equal(t, "failed-operation", endLog["operation"])
		assert.False(t, endLog["success"].(bool))
		assert.Equal(t, "operation failed", endLog["error"])
	})
}

func TestLogTimingOperation(t *testing.T) {
	// Setup the logger
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)
	SetLevel("debug")

	// Create a test context
	ctx := appctx.NewContext(nil)

	// Test successful operation
	t.Run("SuccessfulOperation", func(t *testing.T) {
		buf.Reset()

		err := LogTimingOperation(ctx, "timing-op", func(opCtx context.Context) error {
			// Check that operation name is set in context
			assert.Equal(t, "timing-op", appctx.GetOperationName(opCtx))
			return nil
		})

		assert.NoError(t, err)

		// The operation produces only one log entry
		var logMap map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "Operation timing", logMap["msg"])
		assert.Equal(t, "timing-op", logMap["operation"])
		assert.True(t, logMap["success"].(bool))
		assert.True(t, logMap["duration_ms"].(float64) >= 0)
	})

	// Test failed operation
	t.Run("FailedOperation", func(t *testing.T) {
		buf.Reset()

		testErr := errors.New("timing operation failed")
		err := LogTimingOperation(ctx, "failed-timing", func(opCtx context.Context) error {
			return testErr
		})

		assert.Equal(t, testErr, err)

		var logMap map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "Operation error", logMap["msg"])
		assert.Equal(t, "failed-timing", logMap["operation"])
		assert.False(t, logMap["success"].(bool))
		assert.Equal(t, "timing operation failed", logMap["error"])
	})
}
