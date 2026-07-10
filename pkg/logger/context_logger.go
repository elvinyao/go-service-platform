package logger

import (
	"context"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"time"
)

// FromContext creates an Entry with fields from the context.
func FromContext(ctx context.Context) *Entry {
	if ctx == nil {
		return rootEntry(context.Background()).WithField("context", "missing")
	}

	entry := rootEntry(ctx)

	// Keep stable attribute order for readable JSON.
	if serviceName := appctx.GetServiceName(ctx); serviceName != "" {
		entry = entry.WithField("service", serviceName)
	}
	if operationName := appctx.GetOperationName(ctx); operationName != "" {
		entry = entry.WithField("operation", operationName)
	}
	if requestID := appctx.GetRequestID(ctx); requestID != "" {
		entry = entry.WithField("request_id", requestID)
	}
	if traceID := appctx.GetTraceID(ctx); traceID != "" {
		entry = entry.WithField("trace_id", traceID)
	}
	if userID := appctx.GetUserID(ctx); userID != "" {
		entry = entry.WithField("user_id", userID)
	}

	startTime := appctx.GetStartTime(ctx)
	if !startTime.IsZero() {
		entry = entry.WithField("elapsed_ms", time.Since(startTime).Milliseconds())
	}

	return entry
}

// Debug logs a message at level Debug with context information.
func DebugWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Debug(args...)
}

// Debugf logs a formatted message at level Debug with context information.
func DebugfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Debugf(format, args...)
}

// Info logs a message at level Info with context information.
func InfoWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Info(args...)
}

// Infof logs a formatted message at level Info with context information.
func InfofWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Infof(format, args...)
}

// Warn logs a message at level Warn with context information.
func WarnWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Warn(args...)
}

// Warnf logs a formatted message at level Warn with context information.
func WarnfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Warnf(format, args...)
}

// Error logs a message at level Error with context information.
func ErrorWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Error(args...)
}

// Errorf logs a formatted message at level Error with context information.
func ErrorfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Errorf(format, args...)
}

// Fatal logs a message at level Fatal with context information.
func FatalWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Fatal(args...)
}

// Fatalf logs a formatted message at level Fatal with context information.
func FatalfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Fatalf(format, args...)
}

// WithContextError adds error information to the logging context.
func WithContextError(ctx context.Context, err error) *Entry {
	entry := FromContext(ctx).WithError(err)

	if fieldErr, ok := err.(interface{ GetFields() map[string]interface{} }); ok {
		fields := fieldErr.GetFields()
		if len(fields) > 0 {
			entry = entry.WithFields(fields)
		}
	} else if fieldErr, ok := err.(interface{ Fields() map[string]interface{} }); ok {
		fields := fieldErr.Fields()
		if len(fields) > 0 {
			entry = entry.WithFields(fields)
		}
	}
	return entry
}

// WithContextFields adds fields to the logging entry from the context.
func WithContextFields(ctx context.Context, fields Fields) *Entry {
	return FromContext(ctx).WithFields(fields)
}

// LogOperation logs the beginning and end of an operation with elapsed time.
func LogOperation(ctx context.Context, operation string, fn func(ctx context.Context) error) error {
	opCtx := appctx.WithOperationName(ctx, operation)
	startTime := time.Now()

	InfofWithContext(opCtx, "Starting operation: %s", operation)

	err := fn(opCtx)
	duration := time.Since(startTime)

	fields := Fields{
		"operation":   operation,
		"duration_ms": duration.Milliseconds(),
		"success":     err == nil,
	}

	if err != nil {
		WithContextFields(opCtx, fields).WithError(err).Error("Operation failed")
	} else {
		WithContextFields(opCtx, fields).Info("Operation completed")
	}

	return err
}

// LogTimingOperation logs only the timing for an operation without extra start/end messages.
func LogTimingOperation(ctx context.Context, operation string, fn func(ctx context.Context) error) error {
	opCtx := appctx.WithOperationName(ctx, operation)
	startTime := time.Now()

	err := fn(opCtx)
	duration := time.Since(startTime)

	fields := Fields{
		"operation":   operation,
		"duration_ms": duration.Milliseconds(),
		"success":     err == nil,
	}

	if err != nil {
		WithContextFields(opCtx, fields).WithError(err).Error("Operation error")
	} else {
		WithContextFields(opCtx, fields).Debug("Operation timing")
	}

	return err
}
