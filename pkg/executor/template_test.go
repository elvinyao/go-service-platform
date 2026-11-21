package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func TestRenderTemplateJSONEscapesUserText(t *testing.T) {
	msg := ruleengine.Message{
		Content: "he said \"hi\"\nthen <@bot> left\", \"channel\": \"other",
		Payload: map[string]interface{}{"count": 3, "tags": []interface{}{"a", "b"}},
	}
	out, err := RenderTemplate(`{"text":{{json .Content}},"count":{{json .Payload.count}},"tags":{{json .Payload.tags}}}`, msg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("rendered body is not valid JSON: %v\n%s", err, out)
	}
	if body["text"] != msg.Content {
		t.Fatalf("text = %q, want the original content", body["text"])
	}
	if _, injected := body["channel"]; injected {
		t.Fatalf("user text injected a field: %s", out)
	}
	if !strings.Contains(out, "<@bot>") {
		t.Fatalf("json helper should not HTML-escape chat markup: %s", out)
	}
}

func TestRenderTemplateFieldAndDefaultHelpers(t *testing.T) {
	msg := ruleengine.Message{Payload: map[string]interface{}{
		"channel": map[string]interface{}{"name": "ops"},
		"thread":  map[string]interface{}{"id": ""},
	}}
	out, err := RenderTemplate(`{{field "payload.channel.name"}}|{{field "payload.missing"}}|{{field "payload.missing" | default "n/a"}}|{{field "payload.thread.id" | default "none"}}|{{default "x" nil}}|{{default "x" 0}}`, msg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "ops||n/a|none|x|0"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestRenderTemplateRejectsMissingMapKeys(t *testing.T) {
	cases := map[string]string{
		"missing payload key": `{{.Payload.channel}}`,
		"nil payload":         `{{.Payload.anything}}`,
		"misspelled field":    `{{.Contnet}}`,
	}
	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			msg := ruleengine.Message{}
			if name == "missing payload key" {
				msg.Payload = map[string]interface{}{"other": 1}
			}
			out, err := RenderTemplate(tmpl, msg)
			if err == nil {
				t.Fatalf("expected an error, rendered %q", out)
			}
		})
	}
}

func TestRenderTemplateReportsJSONEncodingErrors(t *testing.T) {
	msg := ruleengine.Message{Payload: map[string]interface{}{"bad": func() {}}}
	if _, err := RenderTemplate(`{{json .Payload.bad}}`, msg); err == nil {
		t.Fatalf("expected an encoding error")
	}
}

func TestLogExecutorTemplateErrorsAreNotRetried(t *testing.T) {
	err := NewLogExecutor().Execute(nil, ruleengine.Message{}, ruleengine.Action{
		ID:     "a1",
		Params: map[string]interface{}{"template": "{{.Payload.missing}}"},
	})
	if err == nil || !IsPermanent(err) {
		t.Fatalf("error = %v, want a permanent template error", err)
	}
}
