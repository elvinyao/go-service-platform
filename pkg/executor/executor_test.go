package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"project/pkg/ruleengine"
)

type recordingExecutor struct {
	executed bool
}

func (e *recordingExecutor) Type() string {
	return "record"
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
