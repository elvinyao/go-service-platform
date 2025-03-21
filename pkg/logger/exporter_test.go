package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiWriter(t *testing.T) {
	// Create test buffers
	buf1 := new(bytes.Buffer)
	buf2 := new(bytes.Buffer)

	// Create a MultiWriter with both buffers
	mw := NewMultiWriter(buf1, buf2)

	// Write data
	testData := []byte("test data for multi writer")
	n, err := mw.Write(testData)

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, string(testData), buf1.String())
	assert.Equal(t, string(testData), buf2.String())
}

func TestMultiWriterAddRemove(t *testing.T) {
	// Create test buffers
	buf1 := new(bytes.Buffer)
	buf2 := new(bytes.Buffer)
	buf3 := new(bytes.Buffer)

	// Create a MultiWriter with one buffer
	mw := NewMultiWriter(buf1)

	// Initial write
	testData1 := []byte("test data 1")
	mw.Write(testData1)
	assert.Equal(t, string(testData1), buf1.String())
	assert.Empty(t, buf2.String())

	// Add a second buffer
	mw.AddWriter(buf2)

	// Write more data, should go to both buffers
	testData2 := []byte("test data 2")
	mw.Write(testData2)
	assert.Equal(t, string(testData1)+string(testData2), buf1.String())
	assert.Equal(t, string(testData2), buf2.String())

	// Remove the first buffer
	mw.RemoveWriter(buf1)

	// Add a third buffer
	mw.AddWriter(buf3)

	// Write more data, should only go to second and third buffers
	testData3 := []byte("test data 3")
	mw.Write(testData3)
	assert.Equal(t, string(testData1)+string(testData2), buf1.String()) // Unchanged
	assert.Equal(t, string(testData2)+string(testData3), buf2.String())
	assert.Equal(t, string(testData3), buf3.String())
}

func TestFileRotationHook(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "test.log")

	// Create a small file rotation hook (100KB max size, 1 hour rotation)
	hook, err := NewFileRotationHook(logFile, 1, 1)
	require.NoError(t, err)
	defer hook.Close()

	// Verify the hook has been created correctly
	assert.NotNil(t, hook.file)
	assert.Equal(t, logFile, hook.filename)
	assert.Equal(t, int64(1*1024*1024), hook.maxSize) // 1MB
	assert.Equal(t, time.Hour, hook.rotateTime)

	// Create a logrus entry to test the hook
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	entry := logrus.NewEntry(logger)
	entry = entry.WithField("test", "value")

	// Fire the hook
	err = hook.Fire(entry)
	assert.NoError(t, err)

	// Verify the file exists
	_, err = os.Stat(logFile)
	assert.NoError(t, err)

	// Test that we get all levels
	levels := hook.Levels()
	assert.Equal(t, logrus.AllLevels, levels)
}

func TestFileRotationHookRotation(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "rotation.log")

	// Create a hook with a very small max size to force rotation
	hook, err := NewFileRotationHook(logFile, 1, 1) // 1MB, but we'll manipulate the size
	require.NoError(t, err)
	defer hook.Close()

	// Set a very small max size to force rotation
	hook.maxSize = 10 // Only 10 bytes

	// Create a logrus entry
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{DisableColors: true})
	entry := logrus.NewEntry(logger)
	entry = entry.WithField("test", "value")

	// Fire the hook multiple times to trigger rotation
	for i := 0; i < 5; i++ {
		err = hook.Fire(entry)
		assert.NoError(t, err)
	}

	// Check that at least one rotated file exists
	files, err := os.ReadDir(tempDir)
	assert.NoError(t, err)

	// There should be more than one file (the current log and at least one rotation)
	assert.True(t, len(files) > 1, "Expected rotated files but found only %d files", len(files))

	// Check that the current log file exists
	_, err = os.Stat(logFile)
	assert.NoError(t, err)
}

func TestFileRotationHookTimeRotation(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "time-rotation.log")

	// Create a hook with a very short rotation time
	hook, err := NewFileRotationHook(logFile, 100, 1) // 100MB, 1 hour
	require.NoError(t, err)
	defer hook.Close()

	// Set a very short rotation time to force rotation
	hook.rotateTime = 1 * time.Millisecond

	// Create a logrus entry
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{DisableColors: true})
	entry := logrus.NewEntry(logger)

	// Fire the hook, then wait for rotation time to pass
	err = hook.Fire(entry)
	assert.NoError(t, err)

	// Wait for rotation time to pass
	time.Sleep(10 * time.Millisecond)

	// Fire again to trigger time-based rotation
	err = hook.Fire(entry)
	assert.NoError(t, err)

	// Check that at least one rotated file exists
	files, err := os.ReadDir(tempDir)
	assert.NoError(t, err)

	// There should be more than one file
	assert.True(t, len(files) > 1, "Expected rotated files but found only %d files", len(files))
}

func TestAddFileLogger(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "add-file.log")

	// Save the original hooks
	originalHooks := log.Hooks
	defer func() {
		// Restore original hooks after test
		log.Hooks = originalHooks
	}()

	// Add file logger
	err = AddFileLogger(logFile, 1, 1)
	assert.NoError(t, err)

	// Verify a hook was added
	assert.NotEmpty(t, log.Hooks[logrus.InfoLevel])

	// Log something
	Info("test message for file logger")

	// Verify the file exists and has content
	fileContent, err := os.ReadFile(logFile)
	assert.NoError(t, err)
	assert.Contains(t, string(fileContent), "test message for file logger")
}
