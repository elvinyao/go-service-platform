package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigFromEnvAndInit(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("LOG_TIME_FORMAT", time.RFC822)
	t.Setenv("LOG_CALLER_INFO", "1")
	t.Setenv("LOG_OUTPUT", "stderr")

	cfg := LoadConfigFromEnv()
	if cfg.Level != "debug" || cfg.Format != "text" || cfg.TimeFormat != time.RFC822 || !cfg.CallerInfo || cfg.Output != "stderr" {
		t.Fatalf("config from env = %+v", cfg)
	}

	InitFromEnv()
	if levelVar.Level() != slog.LevelDebug {
		t.Fatalf("level = %s, want debug", levelVar.Level())
	}
	Init()
}

func TestTopLevelFormattedErrorAndPanicLogs(t *testing.T) {
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.CallerInfo = false
	Configure(cfg)
	SetOutput(&buf)
	SetLevel("debug")

	Debugf("debug %s", "formatted")
	Warnf("warn %s", "formatted")
	Error("error plain")
	Errorf("error %s", "formatted")

	for _, want := range []string{"debug formatted", "warn formatted", "error plain", "error formatted"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("log output missing %q: %s", want, buf.String())
		}
	}

	assertPanicsWithMessage(t, "panic plain", func() { Panic("panic plain") })
	assertPanicsWithMessage(t, "panic formatted", func() { Panicf("panic %s", "formatted") })
	assertPanicsWithMessage(t, "entry panic", func() { WithField("k", "v").Panic("entry panic") })
	assertPanicsWithMessage(t, "entry panicf", func() { WithField("k", "v").Panicf("entry %s", "panicf") })
}

func TestEntryWithNilErrorAndNilLoggerAreSafe(t *testing.T) {
	entry := WithField("key", "value")
	if entry.WithError(nil) != entry {
		t.Fatalf("WithError(nil) should return the original entry")
	}

	logMu.Lock()
	previous := baseLogger
	baseLogger = nil
	logMu.Unlock()
	t.Cleanup(func() {
		logMu.Lock()
		baseLogger = previous
		logMu.Unlock()
	})

	(&Entry{}).Info("ignored")
}

func TestResolveOutputAndLoggerHelpers(t *testing.T) {
	if resolveOutput("") != os.Stdout {
		t.Fatalf("empty output should resolve to stdout")
	}
	if resolveOutput("stderr") != os.Stderr {
		t.Fatalf("stderr output should resolve to stderr")
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "service.log")
	writer := resolveOutput(filePath)
	if closer, ok := writer.(*os.File); ok {
		_, _ = writer.Write([]byte("hello"))
		_ = closer.Close()
	} else {
		t.Fatalf("file output did not return *os.File")
	}
	if data, err := os.ReadFile(filePath); err != nil || string(data) != "hello" {
		t.Fatalf("file output data = %q %v", string(data), err)
	}

	if resolveOutput(filepath.Join(dir, "missing", "service.log")) != os.Stdout {
		t.Fatalf("unopenable file should fall back to stdout")
	}
	if normalizeValue(errors.New("boom")) != "boom" {
		t.Fatalf("normalizeValue did not stringify error")
	}
	if !shouldSkipFrame("runtime/proc.go", "runtime.goexit") {
		t.Fatalf("runtime frame should be skipped")
	}
	if !shouldSkipFrame("/repo/pkg/logger/logger.go", "github.com/elvinyao/go-service-platform/pkg/logger.Info") {
		t.Fatalf("logger implementation frame should be skipped")
	}
	if got := relativizePath(""); got != "." {
		t.Fatalf("empty path = %q, want cleaned current directory", got)
	}
	if detectProjectRoot() == "" {
		t.Fatalf("project root should be detectable")
	}
}

func TestOrderedJSONHandlerAttrsGroupsAndValueKinds(t *testing.T) {
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.CallerInfo = false
	handler := newHandler(&buf, cfg)
	logger := slog.New(handler).With(
		"component", "api",
		"duration", 2*time.Second,
		"when", time.Unix(10, 0).UTC(),
		"err", errors.New("boom"),
	).WithGroup("request")

	logger.Info("handled",
		slog.Int64("id", 42),
		slog.Uint64("size", 7),
		slog.Float64("ratio", 1.5),
		slog.Bool("ok", true),
		slog.Group("nested", slog.String("name", "inner")),
	)

	var payload map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if payload["request.component"] != "api" {
		t.Fatalf("component = %v", payload["request.component"])
	}
	if payload["request.id"] != float64(42) || payload["request.nested.name"] != "inner" {
		t.Fatalf("grouped payload = %+v", payload)
	}
	if payload["request.duration"] != "2s" || payload["request.when"] != "1970-01-01T00:00:10Z" || payload["request.err"] != "boom" {
		t.Fatalf("special values = %+v", payload)
	}
}

func TestOrderedJSONHandlerReturnsMarshalAndWriteErrors(t *testing.T) {
	handler := newHandler(&bytes.Buffer{}, DefaultConfig())
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "bad", 0)
	record.AddAttrs(slog.Any("bad", func() {}))
	if err := handler.Handle(context.Background(), record); err == nil {
		t.Fatalf("expected marshal error")
	}

	errWriter := failingLoggerWriter{}
	handler = newHandler(errWriter, DefaultConfig())
	record = slog.NewRecord(time.Now(), slog.LevelInfo, "write", 0)
	if err := handler.Handle(context.Background(), record); err == nil {
		t.Fatalf("expected write error")
	}
}

type failingLoggerWriter struct{}

func (failingLoggerWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func assertPanicsWithMessage(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic %q", want)
		}
		if got != want {
			t.Fatalf("panic = %v, want %q", got, want)
		}
	}()
	fn()
}
