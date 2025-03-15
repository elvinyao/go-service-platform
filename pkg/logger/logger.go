package logger

import (
	"os"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

// Init initializes the logger with default configuration
func Init() {
	log.Out = os.Stdout
	log.SetLevel(logrus.InfoLevel)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
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

// 其他日志方法...
