package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/mattermost"
	"github.com/elvinyao/go-service-platform/pkg/mattermost/mattermosttest"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func newTestMattermostInput(t *testing.T, srv *mattermosttest.Server, token string) *MattermostInputService {
	t.Helper()
	svc, err := NewMattermostInputService(MattermostInputServiceName, "WorkflowEngine", MattermostInputConfig{
		ServerURL: srv.URL,
		Token:     token,
		Listener:  mattermost.ListenerConfig{ReconnectInterval: 20 * time.Millisecond},
	}, "test")
	if err != nil {
		t.Fatalf("new input: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	return svc
}

func checkStatus(t *testing.T, svc *MattermostInputService) health.Status {
	t.Helper()
	checkers := svc.RegisterHealthChecks()
	if len(checkers) == 0 {
		t.Fatalf("no health checkers")
	}
	result := checkers[len(checkers)-1].Check(context.Background())
	if result.Level != health.LevelCritical {
		t.Fatalf("level = %v, want critical", result.Level)
	}
	return result.Status
}

func TestMattermostInputServiceLifecycleHealthAndRouting(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	svc := newTestMattermostInput(t, srv, srv.Token)
	if checkStatus(t, svc) != health.StatusDown {
		t.Fatalf("a stopped input must be down")
	}

	// Events that arrive before a handler is registered are dropped.
	svc.dispatch(context.Background(), ruleengine.Message{ID: "early"})
	if err := svc.Start(nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("second start: %v", err)
	}
	srv.WaitConnected(t, 2*time.Second)

	received := make(chan ruleengine.Message, 1)
	svc.OnMessage(func(_ context.Context, message ruleengine.Message) { received <- message })
	srv.EmitPost(mattermosttest.PostEvent{Message: "hello"})
	select {
	case message := <-received:
		if message.Content != "hello" {
			t.Fatalf("received %q", message.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no message routed")
	}

	if checkStatus(t, svc) != health.StatusUp {
		t.Fatalf("a connected input must be up")
	}
	metrics := svc.GetMetrics(context.Background())
	if metrics["connected"] != true || metrics["bot_username"] != mattermosttest.DefaultBotUsername || metrics["routed_events"] != int64(1) || metrics["last_event_at"] == nil {
		t.Fatalf("metrics = %v", metrics)
	}
	for key, value := range metrics {
		if strings.Contains(strings.ToLower(key), "token") || value == srv.Token {
			t.Fatalf("metrics expose the token: %s=%v", key, value)
		}
	}
	if svc.Client() == nil || svc.Client().ServerURL() != srv.URL {
		t.Fatalf("client not shared")
	}

	if err := svc.Restart(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := svc.Stop(nil); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if checkStatus(t, svc) != health.StatusDown {
		t.Fatalf("a stopped input must be down")
	}
}

func TestMattermostInputServiceReportsDisconnectedStream(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	svc := newTestMattermostInput(t, srv, srv.Token)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	srv.WaitConnected(t, 2*time.Second)
	srv.Close()
	deadline := time.Now().Add(2 * time.Second)
	for checkStatus(t, svc) != health.StatusDown {
		if time.Now().After(deadline) {
			t.Fatalf("health stayed up after the server went away")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMattermostInputServiceRejectsBadConfigAndToken(t *testing.T) {
	if _, err := NewMattermostInputService("mm", "wf", MattermostInputConfig{ServerURL: "nope", Token: "t"}, "test"); err == nil {
		t.Fatalf("expected URL error")
	}
	if _, err := NewMattermostInputService("mm", "wf", MattermostInputConfig{
		ServerURL: "http://localhost:1", Token: "t",
		Listener: mattermost.ListenerConfig{PingInterval: time.Minute, ReadTimeout: time.Second},
	}, "test"); err == nil {
		t.Fatalf("expected listener config error")
	}

	srv := mattermosttest.NewServer(t)
	svc := newTestMattermostInput(t, srv, "wrong")
	if err := svc.Start(context.Background()); err == nil {
		t.Fatalf("expected a rejected token error")
	}
	if svc.IsRunning(context.Background()) {
		t.Fatalf("service must not run after a failed start")
	}
	if err := svc.Restart(context.Background()); err == nil {
		t.Fatalf("restart with a rejected token should fail")
	}
}

func TestMattermostInputServiceStopHonorsContext(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	svc := newTestMattermostInput(t, srv, srv.Token)
	entered := make(chan struct{})
	release := make(chan struct{})
	svc.OnMessage(func(context.Context, ruleengine.Message) {
		close(entered)
		<-release
	})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	srv.WaitConnected(t, 2*time.Second)
	srv.EmitPost(mattermosttest.PostEvent{Message: "block"})
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := svc.Stop(ctx); err == nil {
		t.Fatalf("stop should report the timeout")
	}
	close(release)
}
