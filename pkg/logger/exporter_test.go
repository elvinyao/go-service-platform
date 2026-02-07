package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiWriter(t *testing.T) {
	buf1 := new(bytes.Buffer)
	buf2 := new(bytes.Buffer)

	mw := NewMultiWriter(buf1, buf2)

	testData := []byte("test data for multi writer")
	n, err := mw.Write(testData)

	assert.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, string(testData), buf1.String())
	assert.Equal(t, string(testData), buf2.String())
}

func TestMultiWriterAddRemove(t *testing.T) {
	buf1 := new(bytes.Buffer)
	buf2 := new(bytes.Buffer)
	buf3 := new(bytes.Buffer)

	mw := NewMultiWriter(buf1)

	testData1 := []byte("test data 1")
	_, _ = mw.Write(testData1)
	assert.Equal(t, string(testData1), buf1.String())
	assert.Empty(t, buf2.String())

	mw.AddWriter(buf2)
	testData2 := []byte("test data 2")
	_, _ = mw.Write(testData2)
	assert.Equal(t, string(testData1)+string(testData2), buf1.String())
	assert.Equal(t, string(testData2), buf2.String())

	mw.RemoveWriter(buf1)
	mw.AddWriter(buf3)
	testData3 := []byte("test data 3")
	_, _ = mw.Write(testData3)

	assert.Equal(t, string(testData1)+string(testData2), buf1.String())
	assert.Equal(t, string(testData2)+string(testData3), buf2.String())
	assert.Equal(t, string(testData3), buf3.String())
}

func TestFileRotationHook(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "test.log")
	hook, err := NewFileRotationHook(logFile, 1, 1)
	require.NoError(t, err)
	defer hook.Close()

	assert.NotNil(t, hook.file)
	assert.Equal(t, logFile, hook.filename)
	assert.Equal(t, int64(1*1024*1024), hook.maxSize)
	assert.Equal(t, time.Hour, hook.rotateTime)

	_, err = hook.Write([]byte("hello\n"))
	assert.NoError(t, err)

	_, err = os.Stat(logFile)
	assert.NoError(t, err)
}

func TestFileRotationHookRotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "rotation.log")
	hook, err := NewFileRotationHook(logFile, 1, 1)
	require.NoError(t, err)
	defer hook.Close()

	hook.maxSize = 10
	for i := 0; i < 5; i++ {
		_, err = hook.Write([]byte("0123456789abcdef\n"))
		assert.NoError(t, err)
	}

	files, err := os.ReadDir(tempDir)
	assert.NoError(t, err)
	assert.True(t, len(files) > 1, "Expected rotated files but found only %d files", len(files))

	_, err = os.Stat(logFile)
	assert.NoError(t, err)
}

func TestFileRotationHookTimeRotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "time-rotation.log")
	hook, err := NewFileRotationHook(logFile, 100, 1)
	require.NoError(t, err)
	defer hook.Close()

	hook.rotateTime = 1 * time.Millisecond

	_, err = hook.Write([]byte("line1\n"))
	assert.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	_, err = hook.Write([]byte("line2\n"))
	assert.NoError(t, err)

	files, err := os.ReadDir(tempDir)
	assert.NoError(t, err)
	assert.True(t, len(files) > 1, "Expected rotated files but found only %d files", len(files))
}

func TestAddFileLogger(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logger-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "add-file.log")

	Configure(DefaultConfig())
	err = AddFileLogger(logFile, 1, 1)
	assert.NoError(t, err)

	Info("test message for file logger")

	fileContent, err := os.ReadFile(logFile)
	assert.NoError(t, err)
	assert.Contains(t, string(fileContent), "test message for file logger")
}
