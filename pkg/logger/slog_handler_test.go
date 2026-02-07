package logger

import (
	"bytes"
	"encoding/json"
	appctx "project/pkg/context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFieldOrder(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	ctx := appctx.NewContext(nil)
	ctx = appctx.WithServiceName(ctx, "svc")
	ctx = appctx.WithOperationName(ctx, "op")
	ctx = appctx.WithRequestID(ctx, "rid")

	InfoWithContext(ctx, "hello")

	line := strings.TrimSpace(buf.String())
	require.NotEmpty(t, line)

	assert.True(t, strings.Index(line, `"time":`) < strings.Index(line, `"level":`))
	assert.True(t, strings.Index(line, `"level":`) < strings.Index(line, `"msg":`))
	assert.True(t, strings.Index(line, `"msg":`) < strings.Index(line, `"service":`))
	assert.True(t, strings.Index(line, `"service":`) < strings.Index(line, `"operation":`))
	assert.True(t, strings.Index(line, `"operation":`) < strings.Index(line, `"request_id":`))
	assert.True(t, strings.Index(line, `"request_id":`) < strings.Index(line, `"file":`))
	assert.True(t, strings.Index(line, `"file":`) < strings.Index(line, `"func":`))
}

func TestBusinessCallerLocation(t *testing.T) {
	var buf bytes.Buffer
	Configure(DefaultConfig())
	SetOutput(&buf)

	Info("caller-check")

	var logMap map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logMap))

	file, _ := logMap["file"].(string)
	function, _ := logMap["func"].(string)
	assert.NotContains(t, file, "/pkg/logger/logger.go")
	assert.NotContains(t, file, "/pkg/logger/context_logger.go")
	assert.NotContains(t, function, "project/pkg/logger.Info")
	assert.NotContains(t, function, "project/pkg/logger.InfoWithContext")
}
