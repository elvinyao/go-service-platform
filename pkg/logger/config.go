package logger

import (
	"os"
	"strings"
)

// LoadConfigFromEnv loads logger configuration from environment variables
func LoadConfigFromEnv() Config {
	config := DefaultConfig()

	// Check for log level
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Level = level
	}

	// Check for log format
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		config.Format = format
	}

	// Check for time format
	if timeFormat := os.Getenv("LOG_TIME_FORMAT"); timeFormat != "" {
		config.TimeFormat = timeFormat
	}

	// Check for caller info
	if callerInfo := os.Getenv("LOG_CALLER_INFO"); callerInfo != "" {
		config.CallerInfo = strings.ToLower(callerInfo) == "true" || callerInfo == "1"
	}

	// Check for output destination
	if output := os.Getenv("LOG_OUTPUT"); output != "" {
		config.Output = output
	}

	return config
}

// InitFromEnv initializes the logger using environment variables
func InitFromEnv() {
	Configure(LoadConfigFromEnv())
}
