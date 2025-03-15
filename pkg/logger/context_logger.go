package logger

import (
	"context"
	appctx "project/pkg/context"
	"time"

	"github.com/sirupsen/logrus"
)

// FromContext creates a logrus.Entry with fields from the context
func FromContext(ctx context.Context) *logrus.Entry {
	if ctx == nil {
		return log.WithField("context", "missing")
	}

	entry := log.WithFields(logrus.Fields{})

	// Add request ID if present
	if requestID := appctx.GetRequestID(ctx); requestID != "" {
		entry = entry.WithField("request_id", requestID)
	}

	// Add trace ID if present
	if traceID := appctx.GetTraceID(ctx); traceID != "" {
		entry = entry.WithField("trace_id", traceID)
	}

	// Add user ID if present
	if userID := appctx.GetUserID(ctx); userID != "" {
		entry = entry.WithField("user_id", userID)
	}

	// Add service name if present
	if serviceName := appctx.GetServiceName(ctx); serviceName != "" {
		entry = entry.WithField("service", serviceName)
	}

	// Add operation name if present
	if operationName := appctx.GetOperationName(ctx); operationName != "" {
		entry = entry.WithField("operation", operationName)
	}

	// Add elapsed time if start time is present
	startTime := appctx.GetStartTime(ctx)
	if !startTime.IsZero() {
		elapsed := time.Since(startTime)
		entry = entry.WithField("elapsed_ms", elapsed.Milliseconds())
	}

	return entry
}

// Debug logs a message at level Debug with context information
func DebugWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Debug(args...)
}

// Debugf logs a formatted message at level Debug with context information
func DebugfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Debugf(format, args...)
}

// Info logs a message at level Info with context information
func InfoWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Info(args...)
}

// Infof logs a formatted message at level Info with context information
func InfofWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Infof(format, args...)
}

// Warn logs a message at level Warn with context information
func WarnWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Warn(args...)
}

// Warnf logs a formatted message at level Warn with context information
func WarnfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Warnf(format, args...)
}

// Error logs a message at level Error with context information
func ErrorWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Error(args...)
}

// Errorf logs a formatted message at level Error with context information
func ErrorfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Errorf(format, args...)
}

// Fatal logs a message at level Fatal with context information
func FatalWithContext(ctx context.Context, args ...interface{}) {
	FromContext(ctx).Fatal(args...)
}

// Fatalf logs a formatted message at level Fatal with context information
func FatalfWithContext(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Fatalf(format, args...)
}

// WithContextError adds error information to the logging context
func WithContextError(ctx context.Context, err error) *logrus.Entry {
	return FromContext(ctx).WithError(err)
}

// LogOperation logs the beginning and end of an operation with elapsed time
func LogOperation(ctx context.Context, operation string, fn func(ctx context.Context) error) error {
	// Create a new context with the operation name
	opCtx := appctx.WithOperationName(ctx, operation)

	// Log the start of the operation
	InfofWithContext(opCtx, "Starting operation: %s", operation)

	// Execute the operation
	err := fn(opCtx)

	// Log the result of the operation
	if err != nil {
		ErrorfWithContext(opCtx, "Operation failed: %s, error: %v", operation, err)
	} else {
		InfofWithContext(opCtx, "Operation completed: %s, elapsed: %d ms",
			operation, appctx.GetElapsedTime(opCtx).Milliseconds())
	}

	return err
}

// LogTimingOperation logs only the timing for an operation without extra start/end messages
func LogTimingOperation(ctx context.Context, operation string, fn func(ctx context.Context) error) error {
	// Create a new context with the operation name and start time
	opCtx := appctx.WithOperationName(ctx, operation)

	// Execute the operation
	err := fn(opCtx)

	// Log the result with timing
	fields := logrus.Fields{
		"operation":  operation,
		"elapsed_ms": appctx.GetElapsedTime(opCtx).Milliseconds(),
	}

	if err != nil {
		log.WithFields(fields).WithError(err).Error("Operation error")
	} else {
		log.WithFields(fields).Debug("Operation timing")
	}

	return err
}
