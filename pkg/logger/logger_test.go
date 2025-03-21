package logger

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.Equal(t, "info", config.Level)
	assert.Equal(t, "json", config.Format)
	assert.Equal(t, time.RFC3339, config.TimeFormat)
	assert.True(t, config.CallerInfo)
	assert.Equal(t, "stdout", config.Output)
}

func TestSetLevel(t *testing.T) {
	testCases := []struct {
		level    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"fatal", logrus.FatalLevel},
		{"panic", logrus.PanicLevel},
		{"trace", logrus.TraceLevel},
		{"invalid", logrus.InfoLevel}, // Default to info for invalid levels
	}

	for _, tc := range testCases {
		t.Run("Level_"+tc.level, func(t *testing.T) {
			SetLevel(tc.level)
			assert.Equal(t, tc.expected, log.GetLevel())
		})
	}
}

func TestConfigure(t *testing.T) {
	// Test JSON formatter
	t.Run("JSONFormatter", func(t *testing.T) {
		var buf bytes.Buffer
		config := DefaultConfig()
		config.Format = "json"
		Configure(config)
		SetOutput(&buf)

		Info("test message")

		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "test message", logMap["msg"])
		assert.Equal(t, "info", logMap["level"])
	})

	// Test Text formatter
	t.Run("TextFormatter", func(t *testing.T) {
		var buf bytes.Buffer
		config := DefaultConfig()
		config.Format = "text"
		Configure(config)
		SetOutput(&buf)

		Info("test message")

		logLine := buf.String()
		assert.Contains(t, logLine, "level=info")
		assert.Contains(t, logLine, "msg=\"test message\"")
	})

	// Reset to default after tests
	Configure(DefaultConfig())
}

func TestWithField(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	WithField("key", "value").Info("test message")

	var logMap map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logMap)
	assert.NoError(t, err)
	assert.Equal(t, "test message", logMap["msg"])
	assert.Equal(t, "value", logMap["key"])
}

func TestWithFields(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	fields := logrus.Fields{
		"key1": "value1",
		"key2": 42,
	}
	WithFields(fields).Info("test message")

	var logMap map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logMap)
	assert.NoError(t, err)
	assert.Equal(t, "test message", logMap["msg"])
	assert.Equal(t, "value1", logMap["key1"])
	assert.Equal(t, float64(42), logMap["key2"])
}

func TestWithError(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	testErr := os.ErrNotExist
	WithError(testErr).Info("test message")

	var logMap map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logMap)
	assert.NoError(t, err)
	assert.Equal(t, "test message", logMap["msg"])
	assert.Contains(t, logMap["error"].(string), "file does not exist")
}

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	// Test Info and Infof
	t.Run("Info", func(t *testing.T) {
		buf.Reset()
		Info("info message")
		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "info message", logMap["msg"])
		assert.Equal(t, "info", logMap["level"])
	})

	t.Run("Infof", func(t *testing.T) {
		buf.Reset()
		Infof("formatted %s", "message")
		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "formatted message", logMap["msg"])
	})

	// Test Debug level when enabled
	t.Run("Debug", func(t *testing.T) {
		buf.Reset()
		SetLevel("debug")
		Debug("debug message")
		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "debug message", logMap["msg"])
		assert.Equal(t, "debug", logMap["level"])
	})

	// Test Warn and Warnf
	t.Run("Warn", func(t *testing.T) {
		buf.Reset()
		SetLevel("warn")
		Warn("warning message")
		var logMap map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logMap)
		assert.NoError(t, err)
		assert.Equal(t, "warning message", logMap["msg"])
		assert.Equal(t, "warning", logMap["level"])
	})
}

func TestWithPackageName(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	WithPackageName().Info("test message")

	var logMap map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logMap)
	assert.NoError(t, err)
	assert.Equal(t, "test message", logMap["msg"])
	assert.True(t, strings.Contains(logMap["package"].(string), "logger"))
}

func TestGetLogger(t *testing.T) {
	logger := GetLogger()
	assert.NotNil(t, logger)
	assert.IsType(t, &logrus.Logger{}, logger)
}
