package context

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewContext(t *testing.T) {
	// Test with nil parent
	t.Run("NilParent", func(t *testing.T) {
		ctx := NewContext(nil)
		assert.NotNil(t, ctx)
		assert.NotEmpty(t, GetRequestID(ctx))
		assert.False(t, GetStartTime(ctx).IsZero())
	})

	// Test with existing parent
	t.Run("WithParent", func(t *testing.T) {
		parent := context.Background()
		ctx := NewContext(parent)
		assert.NotNil(t, ctx)
		assert.NotEmpty(t, GetRequestID(ctx))
		assert.False(t, GetStartTime(ctx).IsZero())
	})

	// Test with already populated parent
	t.Run("WithPopulatedParent", func(t *testing.T) {
		parent := context.WithValue(context.Background(), requestIDKey, "existing-id")
		startTime := time.Now().Add(-time.Hour)
		parent = context.WithValue(parent, startTimeKey, startTime)

		ctx := NewContext(parent)
		assert.NotNil(t, ctx)
		assert.Equal(t, "existing-id", GetRequestID(ctx))
		assert.Equal(t, startTime, GetStartTime(ctx))
	})
}

func TestContextWithValues(t *testing.T) {
	// Test adding request ID
	t.Run("WithRequestID", func(t *testing.T) {
		ctx := WithRequestID(context.Background(), "req-123")
		assert.Equal(t, "req-123", GetRequestID(ctx))
	})

	// Test adding trace ID
	t.Run("WithTraceID", func(t *testing.T) {
		ctx := WithTraceID(context.Background(), "trace-123")
		assert.Equal(t, "trace-123", GetTraceID(ctx))
	})

	// Test adding user ID
	t.Run("WithUserID", func(t *testing.T) {
		ctx := WithUserID(context.Background(), "user-123")
		assert.Equal(t, "user-123", GetUserID(ctx))
	})

	// Test adding service name
	t.Run("WithServiceName", func(t *testing.T) {
		ctx := WithServiceName(context.Background(), "auth-service")
		assert.Equal(t, "auth-service", GetServiceName(ctx))
	})

	// Test adding operation name
	t.Run("WithOperationName", func(t *testing.T) {
		ctx := WithOperationName(context.Background(), "login")
		assert.Equal(t, "login", GetOperationName(ctx))
	})
}

func TestGetters(t *testing.T) {
	// Test with nil context
	t.Run("NilContext", func(t *testing.T) {
		assert.Empty(t, GetRequestID(nil))
		assert.Empty(t, GetTraceID(nil))
		assert.Empty(t, GetUserID(nil))
		assert.Empty(t, GetServiceName(nil))
		assert.Empty(t, GetOperationName(nil))
		assert.True(t, GetStartTime(nil).IsZero())
		assert.Equal(t, time.Duration(0), GetElapsedTime(nil))
	})

	// Test with context that doesn't have values
	t.Run("EmptyContext", func(t *testing.T) {
		ctx := context.Background()
		assert.Empty(t, GetRequestID(ctx))
		assert.Empty(t, GetTraceID(ctx))
		assert.Empty(t, GetUserID(ctx))
		assert.Empty(t, GetServiceName(ctx))
		assert.Empty(t, GetOperationName(ctx))
		assert.True(t, GetStartTime(ctx).IsZero())
		assert.Equal(t, time.Duration(0), GetElapsedTime(ctx))
	})
}

func TestTimeoutAndDeadline(t *testing.T) {
	// Test timeout
	t.Run("WithTimeout", func(t *testing.T) {
		parent := context.Background()
		ctx, cancel := WithTimeout(parent, 100*time.Millisecond)
		defer cancel()

		// Verify we still have an AppContext
		assert.IsType(t, &AppContext{}, ctx)

		// Verify the timeout works
		select {
		case <-ctx.Done():
			// Expected - timeout occurred
		case <-time.After(200 * time.Millisecond):
			t.Error("Context did not timeout as expected")
		}
	})

	// Test deadline
	t.Run("WithDeadline", func(t *testing.T) {
		parent := context.Background()
		deadline := time.Now().Add(100 * time.Millisecond)
		ctx, cancel := WithDeadline(parent, deadline)
		defer cancel()

		// Verify we still have an AppContext
		assert.IsType(t, &AppContext{}, ctx)

		// Verify the deadline works
		select {
		case <-ctx.Done():
			// Expected - deadline occurred
		case <-time.After(200 * time.Millisecond):
			t.Error("Context did not reach deadline as expected")
		}
	})
}

func TestGetElapsedTime(t *testing.T) {
	startTime := time.Now().Add(-500 * time.Millisecond)
	ctx := context.WithValue(context.Background(), startTimeKey, startTime)

	elapsed := GetElapsedTime(ctx)
	assert.True(t, elapsed >= 500*time.Millisecond)
	assert.True(t, elapsed < 600*time.Millisecond) // Allowing some overhead
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Equal(t, 32, len(id1)) // UUID without dashes
	assert.Equal(t, 32, len(id2))
}

func TestFromContext(t *testing.T) {
	// Test with regular context
	t.Run("RegularContext", func(t *testing.T) {
		ctx := context.Background()
		appCtx := FromContext(ctx)
		assert.IsType(t, &AppContext{}, appCtx)
	})

	// Test with AppContext
	t.Run("AppContext", func(t *testing.T) {
		ctx := NewContext(nil)
		appCtx := FromContext(ctx)
		assert.Same(t, ctx, appCtx)
	})
}

func TestToContext(t *testing.T) {
	// Test with nil
	t.Run("NilContext", func(t *testing.T) {
		ctx := ToContext(nil)
		assert.NotNil(t, ctx)
		_, ok := ctx.(context.Context)
		assert.True(t, ok)
	})

	// Test with regular context
	t.Run("RegularContext", func(t *testing.T) {
		original := context.Background()
		ctx := ToContext(original)
		assert.Equal(t, reflect.TypeOf(original), reflect.TypeOf(ctx))
	})
}

func TestFromRequest(t *testing.T) {
	// Create a test request with headers
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("X-Request-ID", "req-from-header")
	req.Header.Set("X-Trace-ID", "trace-from-header")
	req.Header.Set("X-User-ID", "user-from-header")

	// Test extraction from request
	ctx := FromRequest(req)
	assert.Equal(t, "req-from-header", GetRequestID(ctx))
	assert.Equal(t, "trace-from-header", GetTraceID(ctx))
	assert.Equal(t, "user-from-header", GetUserID(ctx))
}
