package logger

import (
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

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

// Init initializes the logger with default configuration
func Init() {
	Configure(DefaultConfig())
}

// Configure configures the logger with the provided configuration
func Configure(config Config) {
	// Set output
	switch strings.ToLower(config.Output) {
	case "stdout":
		log.Out = os.Stdout
	case "stderr":
		log.Out = os.Stderr
	default:
		// Attempt to use a file
		file, err := os.OpenFile(config.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Errorf("Failed to open log file %s, using stdout: %v", config.Output, err)
			log.Out = os.Stdout
		} else {
			log.Out = file
		}
	}

	// Set level
	SetLevel(config.Level)

	// Set formatter
	switch strings.ToLower(config.Format) {
	case "json":
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: config.TimeFormat,
			PrettyPrint:     false,
		})
	default:
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: config.TimeFormat,
		})
	}

	// Set caller info
	if config.CallerInfo {
		log.SetReportCaller(true)
	}
}

// SetOutput sets the output destination for the logger
func SetOutput(output io.Writer) {
	log.Out = output
}

// SetLevel sets the log level
func SetLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	case "fatal":
		log.SetLevel(logrus.FatalLevel)
	case "panic":
		log.SetLevel(logrus.PanicLevel)
	case "trace":
		log.SetLevel(logrus.TraceLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}
}

// WithField creates an entry with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return log.WithField(key, value)
}

// WithFields creates an entry with multiple fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}

// WithError creates an entry with the "error" field set to the given error
func WithError(err error) *logrus.Entry {
	return log.WithError(err)
}

// Debug logs a message at level Debug
func Debug(args ...interface{}) {
	log.Debug(args...)
}

// Debugf logs a formatted message at level Debug
func Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// Info logs a message at level Info
func Info(args ...interface{}) {
	log.Info(args...)
}

// Infof logs a formatted message at level Info
func Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Warn logs a message at level Warn
func Warn(args ...interface{}) {
	log.Warn(args...)
}

// Warnf logs a formatted message at level Warn
func Warnf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Error logs a message at level Error
func Error(args ...interface{}) {
	log.Error(args...)
}

// Errorf logs a formatted message at level Error
func Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

// Fatal logs a message at level Fatal
func Fatal(args ...interface{}) {
	log.Fatal(args...)
}

// Fatalf logs a formatted message at level Fatal
func Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

// Panic logs a message at level Panic
func Panic(args ...interface{}) {
	log.Panic(args...)
}

// Panicf logs a formatted message at level Panic
func Panicf(format string, args ...interface{}) {
	log.Panicf(format, args...)
}

// WithPackageName adds the package name to the log entry
func WithPackageName() *logrus.Entry {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return log.WithField("package", "unknown")
	}

	details := runtime.FuncForPC(pc)
	if details == nil {
		return log.WithField("package", "unknown")
	}

	packageName := details.Name()
	if idx := strings.LastIndex(packageName, "."); idx != -1 {
		packageName = packageName[:idx]
	}

	return log.WithField("package", packageName)
}

// GetLogger returns the underlying logrus logger
func GetLogger() *logrus.Logger {
	return log
}

// 其他日志方法...
