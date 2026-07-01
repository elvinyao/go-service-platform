package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalconfig "project/internal/config"
	"project/internal/manager"
	"project/internal/service"
	"project/pkg/health"
	"project/pkg/ruleengine"
)

func TestExecutorType(t *testing.T) {
	exe := NewExecutor(nil)
	if exe.Type() != "mattermost" {
		t.Fatalf("type = %q, want mattermost", exe.Type())
	}
}

func TestExecutorSendsRenderedMessageToConfiguredChannel(t *testing.T) {
	posts := make(chan struct {
		ChannelID string `json:"channel_id"`
		Message   string `json:"message"`
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts" {
			http.NotFound(w, r)
			return
		}
		var post struct {
			ChannelID string `json:"channel_id"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
			t.Errorf("decode post: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		posts <- post
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "post-id"})
	}))
	defer server.Close()

	sm := manager.NewServiceManager("test")
	svc := service.NewMattermostService("MattermostService", "Global", internalconfig.MattermostConfig{
		ServerURL:    server.URL,
		APIToken:     "test-token",
		Channel:      "default-channel",
		WebsocketURL: "ws://127.0.0.1:1",
	}, "test")
	sm.RegisterService(svc)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start mattermost service: %v", err)
	}
	defer svc.Stop(context.Background())

	err := NewExecutor(sm).Execute(context.Background(), ruleengine.Message{
		ID:      "m1",
		Type:    "AAA",
		Content: "hello",
	}, ruleengine.Action{
		ID:       "send",
		Executor: "mattermost",
		Params: map[string]interface{}{
			"channel_id": "alerts",
			"template":   "Matched {{.Type}} {{.Content}}",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	post := <-posts
	if post.ChannelID != "alerts" {
		t.Fatalf("channel = %q, want alerts", post.ChannelID)
	}
	if post.Message != "Matched AAA hello" {
		t.Fatalf("message = %q, want rendered template", post.Message)
	}
}

func TestExecutorSendsDefaultMessageToConfiguredChannel(t *testing.T) {
	posts := make(chan struct {
		ChannelID string `json:"channel_id"`
		Message   string `json:"message"`
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var post struct {
			ChannelID string `json:"channel_id"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
			t.Errorf("decode post: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		posts <- post
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "post-id"})
	}))
	defer server.Close()

	sm := manager.NewServiceManager("test")
	svc := service.NewMattermostService("MattermostService", "Global", internalconfig.MattermostConfig{
		ServerURL:    server.URL,
		APIToken:     "test-token",
		Channel:      "default-channel",
		WebsocketURL: "ws://127.0.0.1:1",
	}, "test")
	sm.RegisterService(svc)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start mattermost service: %v", err)
	}
	defer svc.Stop(context.Background())

	err := NewExecutor(sm).Execute(context.Background(), ruleengine.Message{
		ID:      "m1",
		Type:    "AAA",
		Content: "hello",
	}, ruleengine.Action{Params: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	post := <-posts
	if post.ChannelID != "default-channel" {
		t.Fatalf("channel = %q, want default-channel", post.ChannelID)
	}
	if post.Message != "Event received: type=AAA id=m1 content=hello" {
		t.Fatalf("message = %q, want default fallback", post.Message)
	}
}

func TestExecutorReturnsErrorWhenMattermostServiceMissing(t *testing.T) {
	err := NewExecutor(manager.NewServiceManager("test")).Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{})
	if err == nil {
		t.Fatalf("expected missing service error")
	}
	if !strings.Contains(err.Error(), "MattermostService not found") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestExecutorReturnsErrorWhenMattermostServiceHasWrongType(t *testing.T) {
	sm := manager.NewServiceManager("test")
	sm.RegisterService(&wrongMattermostService{name: "MattermostService"})

	err := NewExecutor(sm).Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{})
	if err == nil {
		t.Fatalf("expected type mismatch error")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestExecutorReturnsTemplateError(t *testing.T) {
	sm := manager.NewServiceManager("test")
	svc := service.NewMattermostService("MattermostService", "Global", internalconfig.MattermostConfig{
		ServerURL:    "http://127.0.0.1:1",
		APIToken:     "test-token",
		Channel:      "default-channel",
		WebsocketURL: "ws://127.0.0.1:1",
	}, "test")
	sm.RegisterService(svc)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start mattermost service: %v", err)
	}
	defer svc.Stop(context.Background())

	err := NewExecutor(sm).Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{
		Params: map[string]interface{}{"template": "{{"},
	})
	if err == nil {
		t.Fatalf("expected template error")
	}
	if !strings.Contains(err.Error(), "render mattermost template") {
		t.Fatalf("error = %q", err.Error())
	}
}

type wrongMattermostService struct {
	name string
}

func (s *wrongMattermostService) Start(ctx context.Context) error { return nil }
func (s *wrongMattermostService) Stop(ctx context.Context) error  { return nil }
func (s *wrongMattermostService) Restart(ctx context.Context) error {
	return nil
}
func (s *wrongMattermostService) GetName() string     { return s.name }
func (s *wrongMattermostService) GetWorkflow() string { return "Global" }
func (s *wrongMattermostService) GetType() string     { return "wrong" }
func (s *wrongMattermostService) IsRunning(ctx context.Context) bool {
	return true
}
func (s *wrongMattermostService) GetMetrics(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{"running": true}
}
func (s *wrongMattermostService) Configure(ctx context.Context, cfg interface{}) error {
	return fmt.Errorf("not configurable")
}
func (s *wrongMattermostService) RegisterHealthChecks() []health.Checker { return nil }
func (s *wrongMattermostService) ReportHealth(ctx context.Context, report *health.Report) {
}
