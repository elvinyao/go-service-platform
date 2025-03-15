package context

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	// Keys for context values
	requestIDKey     contextKey = "request_id"
	traceIDKey       contextKey = "trace_id"
	userIDKey        contextKey = "user_id"
	serviceNameKey   contextKey = "service_name"
	operationNameKey contextKey = "operation_name"
	startTimeKey     contextKey = "start_time"
)

// AppContext extends the standard context with application-specific fields
type AppContext struct {
	context.Context
}

// NewContext creates a new AppContext with a request ID
func NewContext(parent context.Context) *AppContext {
	if parent == nil {
		parent = context.Background()
	}

	// Generate a new request ID if not present
	if GetRequestID(parent) == "" {
		parent = context.WithValue(parent, requestIDKey, generateRequestID())
	}

	// Set start time if not present
	if GetStartTime(parent).IsZero() {
		parent = context.WithValue(parent, startTimeKey, time.Now())
	}

	return &AppContext{
		Context: parent,
	}
}

// WithTimeout returns a copy of the parent context with a timeout
func WithTimeout(parent context.Context, timeout time.Duration) (*AppContext, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	return &AppContext{Context: ctx}, cancel
}

// WithDeadline returns a copy of the parent context with a deadline
func WithDeadline(parent context.Context, deadline time.Time) (*AppContext, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(parent, deadline)
	return &AppContext{Context: ctx}, cancel
}

// WithRequestID adds a request ID to the context
func WithRequestID(parent context.Context, requestID string) *AppContext {
	ctx := context.WithValue(parent, requestIDKey, requestID)
	return &AppContext{Context: ctx}
}

// WithTraceID adds a trace ID to the context
func WithTraceID(parent context.Context, traceID string) *AppContext {
	ctx := context.WithValue(parent, traceIDKey, traceID)
	return &AppContext{Context: ctx}
}

// WithUserID adds a user ID to the context
func WithUserID(parent context.Context, userID string) *AppContext {
	ctx := context.WithValue(parent, userIDKey, userID)
	return &AppContext{Context: ctx}
}

// WithServiceName adds a service name to the context
func WithServiceName(parent context.Context, serviceName string) *AppContext {
	ctx := context.WithValue(parent, serviceNameKey, serviceName)
	return &AppContext{Context: ctx}
}

// WithOperationName adds an operation name to the context
func WithOperationName(parent context.Context, operationName string) *AppContext {
	ctx := context.WithValue(parent, operationNameKey, operationName)
	return &AppContext{Context: ctx}
}

// GetRequestID gets the request ID from the context
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetTraceID gets the trace ID from the context
func GetTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// GetUserID gets the user ID from the context
func GetUserID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(userIDKey).(string); ok {
		return id
	}
	return ""
}

// GetServiceName gets the service name from the context
func GetServiceName(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if name, ok := ctx.Value(serviceNameKey).(string); ok {
		return name
	}
	return ""
}

// GetOperationName gets the operation name from the context
func GetOperationName(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if name, ok := ctx.Value(operationNameKey).(string); ok {
		return name
	}
	return ""
}

// GetStartTime gets the start time from the context
func GetStartTime(ctx context.Context) time.Time {
	if ctx == nil {
		return time.Time{}
	}
	if t, ok := ctx.Value(startTimeKey).(time.Time); ok {
		return t
	}
	return time.Time{}
}

// GetElapsedTime returns the time elapsed since the context was created
func GetElapsedTime(ctx context.Context) time.Duration {
	startTime := GetStartTime(ctx)
	if startTime.IsZero() {
		return 0
	}
	return time.Since(startTime)
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// FromContext converts a standard context to an AppContext
func FromContext(ctx context.Context) *AppContext {
	if appCtx, ok := ctx.(*AppContext); ok {
		return appCtx
	}
	return &AppContext{Context: ctx}
}

// ToContext ensures we have a standard context
func ToContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// FromRequest creates an AppContext from an HTTP request
// It extracts common headers like X-Request-ID and X-Trace-ID if present
func FromRequest(r *http.Request) context.Context {
	ctx := r.Context()

	// Create new AppContext
	appCtx := NewContext(ctx)

	// Extract request ID from header if present
	if requestID := r.Header.Get("X-Request-ID"); requestID != "" {
		appCtx = WithRequestID(appCtx, requestID)
	}

	// Extract trace ID from header if present
	if traceID := r.Header.Get("X-Trace-ID"); traceID != "" {
		appCtx = WithTraceID(appCtx, traceID)
	}

	// Extract user ID from header if present
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		appCtx = WithUserID(appCtx, userID)
	}

	return appCtx
}
