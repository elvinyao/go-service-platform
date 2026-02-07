package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Fields = map[string]interface{}

var (
	logMu       sync.RWMutex
	levelVar    = new(slog.LevelVar)
	baseLogger  *slog.Logger
	currentOut  io.Writer = os.Stdout
	currentConf Config
)

// Config holds configuration for the logger
type Config struct {
	Level      string `json:"level" yaml:"level"`
	Format     string `json:"format" yaml:"format"` // "json" or "text"
	TimeFormat string `json:"time_format" yaml:"time_format"`
	CallerInfo bool   `json:"caller_info" yaml:"caller_info"`
	Output     string `json:"output" yaml:"output"` // "stdout", "stderr", or a file path
}

// DefaultConfig returns a default logger configuration
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Format:     "json",
		TimeFormat: time.RFC3339,
		CallerInfo: true,
		Output:     "stdout",
	}
}

// Entry is a fluent logging entry compatible with existing logger usage.
type Entry struct {
	ctx   context.Context
	attrs []slog.Attr
}

func init() {
	Configure(DefaultConfig())
}

// Init initializes the logger with default configuration.
func Init() {
	Configure(DefaultConfig())
}

// Configure configures the logger with the provided configuration.
func Configure(config Config) {
	logMu.Lock()
	defer logMu.Unlock()

	out := resolveOutput(config.Output)
	currentOut = out
	currentConf = config
	levelVar.Set(parseLevel(config.Level))
	baseLogger = slog.New(newHandler(out, config))
}

// SetOutput sets the output destination for the logger.
func SetOutput(output io.Writer) {
	logMu.Lock()
	defer logMu.Unlock()

	currentOut = output
	baseLogger = slog.New(newHandler(currentOut, currentConf))
}

// SetLevel sets the log level.
func SetLevel(level string) {
	logMu.Lock()
	defer logMu.Unlock()
	levelVar.Set(parseLevel(level))
	currentConf.Level = level
}

// WithField creates an entry with a single field.
func WithField(key string, value interface{}) *Entry {
	return rootEntry(context.Background()).WithField(key, value)
}

// WithFields creates an entry with multiple fields.
func WithFields(fields map[string]interface{}) *Entry {
	return rootEntry(context.Background()).WithFields(fields)
}

// WithError creates an entry with the "error" field set to the given error.
func WithError(err error) *Entry {
	return rootEntry(context.Background()).WithError(err)
}

// Debug logs a message at level Debug.
func Debug(args ...interface{}) {
	rootEntry(context.Background()).Debug(args...)
}

// Debugf logs a formatted message at level Debug.
func Debugf(format string, args ...interface{}) {
	rootEntry(context.Background()).Debugf(format, args...)
}

// Info logs a message at level Info.
func Info(args ...interface{}) {
	rootEntry(context.Background()).Info(args...)
}

// Infof logs a formatted message at level Info.
func Infof(format string, args ...interface{}) {
	rootEntry(context.Background()).Infof(format, args...)
}

// Warn logs a message at level Warn.
func Warn(args ...interface{}) {
	rootEntry(context.Background()).Warn(args...)
}

// Warnf logs a formatted message at level Warn.
func Warnf(format string, args ...interface{}) {
	rootEntry(context.Background()).Warnf(format, args...)
}

// Error logs a message at level Error.
func Error(args ...interface{}) {
	rootEntry(context.Background()).Error(args...)
}

// Errorf logs a formatted message at level Error.
func Errorf(format string, args ...interface{}) {
	rootEntry(context.Background()).Errorf(format, args...)
}

// Fatal logs a message at level Fatal and exits.
func Fatal(args ...interface{}) {
	rootEntry(context.Background()).Fatal(args...)
}

// Fatalf logs a formatted message at level Fatal and exits.
func Fatalf(format string, args ...interface{}) {
	rootEntry(context.Background()).Fatalf(format, args...)
}

// Panic logs a message at level Panic and panics.
func Panic(args ...interface{}) {
	rootEntry(context.Background()).Panic(args...)
}

// Panicf logs a formatted message at level Panic and panics.
func Panicf(format string, args ...interface{}) {
	rootEntry(context.Background()).Panicf(format, args...)
}

// WithPackageName adds the package name to the log entry.
func WithPackageName() *Entry {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return WithField("package", "unknown")
	}

	details := runtime.FuncForPC(pc)
	if details == nil {
		return WithField("package", "unknown")
	}

	packageName := details.Name()
	if idx := strings.LastIndex(packageName, "."); idx != -1 {
		packageName = packageName[:idx]
	}

	return WithField("package", packageName)
}

// GetLogger returns the underlying slog logger.
func GetLogger() *slog.Logger {
	logMu.RLock()
	defer logMu.RUnlock()
	return baseLogger
}

func (e *Entry) WithField(key string, value interface{}) *Entry {
	out := e.clone()
	out.attrs = append(out.attrs, slog.Any(key, normalizeValue(value)))
	return out
}

func (e *Entry) WithFields(fields map[string]interface{}) *Entry {
	out := e.clone()
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.attrs = append(out.attrs, slog.Any(k, normalizeValue(fields[k])))
	}
	return out
}

func (e *Entry) WithError(err error) *Entry {
	if err == nil {
		return e
	}
	return e.WithField("error", err.Error())
}

func (e *Entry) Debug(args ...interface{}) {
	e.log(slog.LevelDebug, fmt.Sprint(args...))
}

func (e *Entry) Debugf(format string, args ...interface{}) {
	e.log(slog.LevelDebug, fmt.Sprintf(format, args...))
}

func (e *Entry) Info(args ...interface{}) {
	e.log(slog.LevelInfo, fmt.Sprint(args...))
}

func (e *Entry) Infof(format string, args ...interface{}) {
	e.log(slog.LevelInfo, fmt.Sprintf(format, args...))
}

func (e *Entry) Warn(args ...interface{}) {
	e.log(slog.LevelWarn, fmt.Sprint(args...))
}

func (e *Entry) Warnf(format string, args ...interface{}) {
	e.log(slog.LevelWarn, fmt.Sprintf(format, args...))
}

func (e *Entry) Error(args ...interface{}) {
	e.log(slog.LevelError, fmt.Sprint(args...))
}

func (e *Entry) Errorf(format string, args ...interface{}) {
	e.log(slog.LevelError, fmt.Sprintf(format, args...))
}

func (e *Entry) Fatal(args ...interface{}) {
	e.log(slog.LevelError+4, fmt.Sprint(args...))
	os.Exit(1)
}

func (e *Entry) Fatalf(format string, args ...interface{}) {
	e.log(slog.LevelError+4, fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (e *Entry) Panic(args ...interface{}) {
	msg := fmt.Sprint(args...)
	e.log(slog.LevelError+8, msg)
	panic(msg)
}

func (e *Entry) Panicf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	e.log(slog.LevelError+8, msg)
	panic(msg)
}

func (e *Entry) log(level slog.Level, message string) {
	l := GetLogger()
	if l == nil {
		return
	}

	attrs := make([]slog.Attr, 0, len(e.attrs)+2)
	attrs = append(attrs, e.attrs...)

	if callerEnabled() {
		file, function := resolveBusinessCaller()
		if file != "" {
			attrs = append(attrs, slog.String("file", file))
		}
		if function != "" {
			attrs = append(attrs, slog.String("func", function))
		}
	}

	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	l.LogAttrs(ctx, level, message, attrs...)
}

func (e *Entry) clone() *Entry {
	out := &Entry{
		ctx:   e.ctx,
		attrs: make([]slog.Attr, len(e.attrs)),
	}
	copy(out.attrs, e.attrs)
	return out
}

func rootEntry(ctx context.Context) *Entry {
	return &Entry{ctx: ctx, attrs: []slog.Attr{}}
}

func callerEnabled() bool {
	logMu.RLock()
	defer logMu.RUnlock()
	return currentConf.CallerInfo
}

func resolveOutput(output string) io.Writer {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "", "stdout":
		return os.Stdout
	case "stderr":
		return os.Stderr
	default:
		file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			return os.Stdout
		}
		return file
	}
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case error:
		return val.Error()
	default:
		return v
	}
}

func resolveBusinessCaller() (string, string) {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := frames.Next()
		if !shouldSkipFrame(frame.File, frame.Function) {
			return frame.File, frame.Function
		}
		if !more {
			break
		}
	}
	return "", ""
}

func shouldSkipFrame(file, function string) bool {
	if strings.HasPrefix(function, "runtime.") {
		return true
	}
	if strings.Contains(function, "log/slog") {
		return true
	}
	base := filepath.Base(file)
	if strings.Contains(file, "/pkg/logger/") && !strings.HasSuffix(base, "_test.go") {
		return true
	}
	return false
}
