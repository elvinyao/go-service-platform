package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"project/pkg/ruleengine"
)

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

func TestHTTPExecutorPostsRenderedBody(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	exe := NewHTTPExecutor()
	err := exe.Execute(context.Background(), ruleengine.Message{
		ID: "m1", Type: "AAA", Content: "hello", Timestamp: time.Now(),
	}, ruleengine.Action{
		ID:       "post",
		Executor: "http",
		Params: map[string]interface{}{
			"url":           server.URL,
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotCustomHeader = r.Header.Get("X-Test")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{
			"url": server.URL,
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{"url": server.URL},
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{
			"url":        server.URL,
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

func TestHTTPExecutorRequiresURL(t *testing.T) {
	err := NewHTTPExecutor().Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{})
	if err == nil {
		t.Fatalf("expected missing URL error")
	}
	if !strings.Contains(err.Error(), "params.url") {
		t.Fatalf("error = %q, want params.url", err.Error())
	}
}
