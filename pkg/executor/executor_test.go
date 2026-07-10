package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type executorRoundTripFunc func(*http.Request) (*http.Response, error)

func (f executorRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func executorHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

type recordingExecutor struct {
	executed bool
}

type emptyTypeExecutor struct{}

func (e *recordingExecutor) Type() string {
	return "record"
}

func (e *emptyTypeExecutor) Type() string {
	return ""
}

func (e *emptyTypeExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	return nil
}

func (e *recordingExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	e.executed = true
	return nil
}

func TestRegistryRegistersAndRetrievesExecutor(t *testing.T) {
	registry := NewRegistry()
	exe := &recordingExecutor{}

	if err := registry.Register(exe); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	got, ok := registry.Get("record")
	if !ok {
		t.Fatalf("executor not found")
	}
	if got.Type() != "record" {
		t.Fatalf("executor type = %q, want record", got.Type())
	}
}

func TestRegistryRejectsInvalidExecutors(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); err == nil {
		t.Fatalf("expected nil executor error")
	}
	if err := registry.Register(&emptyTypeExecutor{}); err == nil {
		t.Fatalf("expected empty executor type error")
	}
	var typedNil *recordingExecutor
	if err := registry.Register(typedNil); err == nil {
		t.Fatalf("expected typed nil executor error")
	}
}

func TestRegistryRejectsDuplicateWithoutReplacingFirst(t *testing.T) {
	registry := NewRegistry()
	first := &recordingExecutor{}
	second := &recordingExecutor{}
	if err := registry.Register(first); err != nil {
		t.Fatalf("register first executor: %v", err)
	}
	if err := registry.Register(second); err == nil {
		t.Fatalf("expected duplicate executor error")
	}
	got, exists := registry.Get("record")
	if !exists || got != first {
		t.Fatalf("registered executor = %v/%v, want first %p", got, exists, first)
	}
}

func TestRenderTemplateUsesMessageFields(t *testing.T) {
	msg := ruleengine.Message{ID: "m1", Type: "AAA", Content: "hello"}
	rendered, err := RenderTemplate("{{.Type}} {{.ID}} {{.Content}}", msg)
	if err != nil {
		t.Fatalf("render template: %v", err)
	}
	if rendered != "AAA m1 hello" {
		t.Fatalf("rendered = %q, want AAA m1 hello", rendered)
	}
}

func TestRenderTemplateReturnsParseError(t *testing.T) {
	_, err := RenderTemplate("{{", ruleengine.Message{})
	if err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestRenderTemplateReturnsExecuteError(t *testing.T) {
	_, err := RenderTemplate("{{call .Type}}", ruleengine.Message{Type: "AAA"})
	if err == nil {
		t.Fatalf("expected execute error")
	}
}

func TestLogExecutorTypeAndExecuteLevels(t *testing.T) {
	exe := NewLogExecutor()
	if exe.Type() != "log" {
		t.Fatalf("type = %q, want log", exe.Type())
	}
	for _, level := range []string{"debug", "info", "warn", "error", ""} {
		err := exe.Execute(context.Background(), ruleengine.Message{
			ID:      "m1",
			Type:    "AAA",
			Content: "hello",
		}, ruleengine.Action{
			ID:       "log",
			Executor: "log",
			Params: map[string]interface{}{
				"level":    level,
				"template": "type={{.Type}} content={{.Content}}",
			},
		})
		if err != nil {
			t.Fatalf("execute level %q: %v", level, err)
		}
	}
}

func TestLogExecutorReturnsTemplateError(t *testing.T) {
	err := NewLogExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{"template": "{{"},
	})
	if err == nil {
		t.Fatalf("expected template error")
	}
}

func TestLogExecutorPreservesContextFields(t *testing.T) {
	var output bytes.Buffer
	config := logger.DefaultConfig()
	config.CallerInfo = false
	logger.Configure(config)
	logger.SetOutput(&output)
	defer logger.Init()

	ctx := appctx.WithRequestID(context.Background(), "request-123")
	err := NewLogExecutor().Execute(ctx, ruleengine.Message{ID: "m1", Type: "AAA"}, ruleengine.Action{
		ID:       "log",
		Executor: "log",
		Params:   map[string]interface{}{"template": "context-aware"},
	})
	if err != nil {
		t.Fatalf("execute log action: %v", err)
	}
	if !strings.Contains(output.String(), `"request_id":"request-123"`) {
		t.Fatalf("log output missing request ID: %s", output.String())
	}
}

func TestHTTPExecutorPostsRenderedBody(t *testing.T) {
	var gotBody string
	client := &http.Client{Transport: executorRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", request.Method)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		return executorHTTPResponse(request, http.StatusNoContent, ""), nil
	})}

	exe := NewHTTPExecutorWithClient(client)
	err := exe.Execute(context.Background(), ruleengine.Message{
		ID: "m1", Type: "AAA", Content: "hello", Timestamp: time.Now(),
	}, ruleengine.Action{
		ID:       "post",
		Executor: "http",
		Params: map[string]interface{}{
			"url":           "https://service.example/events",
			"body_template": `{"type":"{{.Type}}","content":"{{.Content}}"}`,
		},
	})
	if err != nil {
		t.Fatalf("execute http action: %v", err)
	}
	if gotBody != `{"type":"AAA","content":"hello"}` {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestHTTPExecutorUsesDefaultPostMethodAndHeaders(t *testing.T) {
	if NewHTTPExecutor().Type() != "http" {
		t.Fatalf("http executor type = %q", NewHTTPExecutor().Type())
	}

	var gotMethod, gotContentType, gotCustomHeader string
	client := &http.Client{Transport: executorRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotMethod = request.Method
		gotContentType = request.Header.Get("Content-Type")
		gotCustomHeader = request.Header.Get("X-Test")
		return executorHTTPResponse(request, http.StatusOK, ""), nil
	})}

	err := NewHTTPExecutorWithClient(client).Execute(nil, ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{
			"url": "https://service.example/events",
			"headers": map[string]interface{}{
				"X-Test": "yes",
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", gotContentType)
	}
	if gotCustomHeader != "yes" {
		t.Fatalf("custom header = %q, want yes", gotCustomHeader)
	}
}

func TestHTTPExecutorReturnsErrorForNon2xx(t *testing.T) {
	client := &http.Client{Transport: executorRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return executorHTTPResponse(request, http.StatusBadGateway, "nope"), nil
	})}

	err := NewHTTPExecutorWithClient(client).Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{"url": "https://service.example/events"},
	})
	if err == nil {
		t.Fatalf("expected non-2xx error")
	}
	if !strings.Contains(err.Error(), "status=502") {
		t.Fatalf("error = %q, want status=502", err.Error())
	}
}

func TestHTTPExecutorReturnsTemplateError(t *testing.T) {
	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{
			"url":           "http://127.0.0.1:1",
			"body_template": "{{",
		},
	})
	if err == nil {
		t.Fatalf("expected template error")
	}
	if !strings.Contains(err.Error(), "render http body template") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestHTTPExecutorReturnsTimeoutError(t *testing.T) {
	client := &http.Client{Transport: executorRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	err := NewHTTPExecutorWithClient(client).Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{
			"url":        "https://service.example/events",
			"timeout_ms": 1,
		},
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "execute request") {
		t.Fatalf("error = %q, want execute request", err.Error())
	}
}

func TestHTTPExecutorReturnsRequestCreationError(t *testing.T) {
	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{"url": "://invalid"},
	})
	if err == nil || !strings.Contains(err.Error(), "create request") {
		t.Fatalf("request creation error = %v", err)
	}
}

func TestHTTPExecutorRequiresURL(t *testing.T) {
	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{})
	if err == nil {
		t.Fatalf("expected missing URL error")
	}
	if !strings.Contains(err.Error(), "params.url") {
		t.Fatalf("error = %q, want params.url", err.Error())
	}
}
