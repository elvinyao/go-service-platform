package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/internal/model"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"

	"github.com/gorilla/websocket"
)

func TestBadgeDBServiceOperationsAndHealthChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := NewBadgeDBService("BadgeDBService", "Global", "", "test")
	if _, err := svc.GetBadge(ctx, "missing"); err == nil {
		t.Fatalf("expected get before start to fail")
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("second start: %v", err)
	}

	badge := model.Badge{
		ID:        "badge-1",
		Name:      "Coverage",
		UserID:    "u1",
		AwardedAt: time.Now(),
		Type:      "debug",
	}
	if err := svc.SaveBadge(ctx, badge); err != nil {
		t.Fatalf("save badge: %v", err)
	}
	got, err := svc.GetBadge(ctx, "badge-1")
	if err != nil {
		t.Fatalf("get badge: %v", err)
	}
	if got.ID != badge.ID {
		t.Fatalf("badge ID = %q, want %q", got.ID, badge.ID)
	}
	if _, err := svc.GetBadge(ctx, "missing"); err == nil {
		t.Fatalf("expected missing badge error")
	}

	access := (&badgeDBAccessChecker{service: svc}).Check(ctx)
	if access.Status != health.StatusUp {
		t.Fatalf("access status = %s, want UP", access.Status)
	}

	svc.mu.Lock()
	svc.lastBackup = time.Time{}
	svc.mu.Unlock()
	noBackup := (&badgeDBBackupChecker{service: svc}).Check(ctx)
	if noBackup.Status != health.StatusDegraded {
		t.Fatalf("no-backup status = %s, want DEGRADED", noBackup.Status)
	}
	svc.mu.Lock()
	svc.lastBackup = time.Now().Add(-25 * time.Hour)
	svc.mu.Unlock()
	staleBackup := (&badgeDBBackupChecker{service: svc}).Check(ctx)
	if staleBackup.Status != health.StatusDegraded {
		t.Fatalf("stale-backup status = %s, want DEGRADED", staleBackup.Status)
	}
	svc.mu.Lock()
	svc.lastBackup = time.Now()
	svc.mu.Unlock()
	freshBackup := (&badgeDBBackupChecker{service: svc}).Check(ctx)
	if freshBackup.Status != health.StatusUp {
		t.Fatalf("fresh-backup status = %s, want UP", freshBackup.Status)
	}

	if err := svc.Restart(ctx); err != nil {
		t.Fatalf("restart running: %v", err)
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := svc.SaveBadge(ctx, badge); err == nil {
		t.Fatalf("expected save after stop to fail")
	}
	downAccess := (&badgeDBAccessChecker{service: svc}).Check(ctx)
	if downAccess.Status != health.StatusDown {
		t.Fatalf("down access status = %s, want DOWN", downAccess.Status)
	}
	downBackup := (&badgeDBBackupChecker{service: svc}).Check(ctx)
	if downBackup.Status != health.StatusDown {
		t.Fatalf("down backup status = %s, want DOWN", downBackup.Status)
	}
	if err := svc.Restart(ctx); err != nil {
		t.Fatalf("restart stopped: %v", err)
	}
}

func TestConfluenceSettingsServiceLifecycleAndHealth(t *testing.T) {
	settingsJSON := mustSettingsJSON(t, []map[string]interface{}{
		{
			"event_type":       "AAA",
			"pattern":          ".*",
			"channel_id":       "alerts",
			"message_template": "Event {{.Content}}",
			"enabled":          true,
		},
		{
			"event_type":       "AAA",
			"channel_id":       "disabled",
			"message_template": "Disabled",
			"enabled":          false,
		},
	})
	client := testHTTPClient(http.StatusOK, mustConfluencePageJSON(t, settingsJSON))

	ctx := context.Background()
	svc := NewConfluenceSettingsService("ConfluenceSettingsService", "B", ConfluenceSettingsConfig{
		PageID:          "settings-page-1",
		RefreshInterval: 20 * time.Millisecond,
		APIEndpoint:     "http://confluence.test",
	}, "test", client)

	down := (&settingsRefreshChecker{service: svc}).Check(ctx)
	if down.Status != health.StatusDown {
		t.Fatalf("down settings checker = %s, want DOWN", down.Status)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("second start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	settings := svc.GetSettings(ctx)
	if len(settings) != 2 {
		t.Fatalf("settings len = %d, want 2", len(settings))
	}
	if svc.GetLastRefreshTime().IsZero() {
		t.Fatalf("last refresh is zero")
	}
	if metrics := svc.GetMetrics(ctx); metrics["settings_count"] != 2 {
		t.Fatalf("settings_count = %v, want 2", metrics["settings_count"])
	}

	fresh := (&settingsRefreshChecker{service: svc}).Check(ctx)
	if fresh.Status != health.StatusUp {
		t.Fatalf("fresh checker = %s, want UP", fresh.Status)
	}
	svc.mu.Lock()
	svc.lastRefresh = time.Now().Add(-time.Hour)
	svc.mu.Unlock()
	stale := (&settingsRefreshChecker{service: svc}).Check(ctx)
	if stale.Status != health.StatusDegraded {
		t.Fatalf("stale checker = %s, want DEGRADED", stale.Status)
	}

	if err := svc.Restart(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestConfluenceSettingsServiceErrorBranches(t *testing.T) {
	ctx := context.Background()

	badRequest := NewConfluenceSettingsService("ConfluenceSettingsService", "B", ConfluenceSettingsConfig{
		PageID:          "settings-page-1",
		RefreshInterval: time.Hour,
		APIEndpoint:     "http://[::1",
	}, "test")
	if _, err := badRequest.fetchSettingsFromConfluence(ctx); err == nil {
		t.Fatalf("expected request creation error")
	}

	doError := NewConfluenceSettingsService("ConfluenceSettingsService", "B", ConfluenceSettingsConfig{
		PageID:          "settings-page-1",
		RefreshInterval: time.Hour,
		APIEndpoint:     "http://confluence.test",
	}, "test", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("request failed")
	})})
	if _, err := doError.fetchSettingsFromConfluence(ctx); err == nil {
		t.Fatalf("expected fetch error")
	}

	neverRefreshed := NewConfluenceSettingsService("ConfluenceSettingsService", "B", ConfluenceSettingsConfig{
		RefreshInterval: time.Hour,
	}, "test")
	neverRefreshed.LockRunning(true)
	check := (&settingsRefreshChecker{service: neverRefreshed}).Check(ctx)
	if check.Status != health.StatusDegraded {
		t.Fatalf("never-refreshed status = %s, want DEGRADED", check.Status)
	}
}

func TestMattermostServiceOperationsAndHealth(t *testing.T) {
	var posts []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v4/posts" {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
		}
		var post mattermostPost
		if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
			return nil, err
		}
		posts = append(posts, post.Message)
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":"post-id"}`)), Header: make(http.Header)}, nil
	})}

	ctx, cancel := context.WithCancel(context.Background())
	svc := NewMattermostService("MattermostService", "Global", MattermostConfig{
		ServerURL: "http://mattermost.test",
		APIToken:  "token",
		Channel:   "default-channel",
	}, "test", client)

	if err := svc.SendMessage(ctx, "before start"); err == nil {
		t.Fatalf("expected send before start error")
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := svc.SendMessage(ctx, "hello default"); err != nil {
		t.Fatalf("send default: %v", err)
	}
	if err := svc.SendMessageToChannel(ctx, "", "missing channel"); err == nil {
		t.Fatalf("expected missing channel error")
	}
	if svc.GetChannelID() != "default-channel" {
		t.Fatalf("channel ID = %q", svc.GetChannelID())
	}
	checkers := svc.RegisterHealthChecks()
	if len(checkers) == 0 {
		t.Fatalf("expected mattermost health checkers")
	}
	cancel()
	if err := svc.Restart(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if len(posts) != 1 || !strings.Contains(posts[0], "hello default") {
		t.Fatalf("posts = %+v, want hello default", posts)
	}
}

func TestMattermostServiceLoginProcessMessagesAndHealthUpBranches(t *testing.T) {
	ctx := context.Background()
	svc := NewMattermostService("MattermostService", "Global", MattermostConfig{
		ServerURL: "http://mattermost.test",
		Username:  "user",
		Password:  "bad",
		Channel:   "default-channel",
	}, "test", testHTTPClient(http.StatusUnauthorized, "login failed"))
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start with failed username login should continue: %v", err)
	}

	svc.mu.Lock()
	svc.wsClient = &websocket.Conn{}
	svc.mu.Unlock()
	result := (&mattermostConnectionChecker{service: svc}).Check(ctx)
	if result.Status != health.StatusUp {
		t.Fatalf("mattermost health did not report UP: %+v", result)
	}
	svc.mu.Lock()
	svc.wsClient = nil
	svc.mu.Unlock()
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestWebSocketServiceHealthCheckerBranches(t *testing.T) {
	ctx := context.Background()
	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", WebSocketConfig{
		ServerURL:         "ws://127.0.0.1:1",
		Path:              "/ws",
		ReconnectInterval: time.Millisecond,
	}, "test")
	checker := &websocketConnectionChecker{service: svc}

	down := checker.Check(ctx)
	if down.Status != health.StatusDown {
		t.Fatalf("down status = %s, want DOWN", down.Status)
	}
	if err := svc.Restart(ctx); err != nil {
		t.Fatalf("restart stopped: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("second start: %v", err)
	}
	defer svc.Stop(context.Background())

	noConnections := checker.Check(ctx)
	if noConnections.Status != health.StatusDown || noConnections.Level != health.LevelCritical {
		t.Fatalf(
			"no-connection result = status:%s level:%s, want DOWN/CRITICAL",
			noConnections.Status,
			noConnections.Level,
		)
	}

	svc.mu.Lock()
	svc.connections = 1
	svc.lastMessage = time.Now().Add(-31 * time.Minute)
	svc.mu.Unlock()
	staleMessage := checker.Check(ctx)
	if staleMessage.Status != health.StatusDegraded {
		t.Fatalf("stale-message status = %s, want DEGRADED", staleMessage.Status)
	}

	svc.mu.Lock()
	svc.connections = 0
	svc.mu.Unlock()
	staleAndDisconnected := checker.Check(ctx)
	if staleAndDisconnected.Status != health.StatusDown {
		t.Fatalf("stale disconnected status = %s, want DOWN", staleAndDisconnected.Status)
	}

	svc.mu.Lock()
	svc.connections = 1
	svc.lastMessage = time.Now()
	svc.mu.Unlock()
	connected := checker.Check(ctx)
	if connected.Status != health.StatusUp {
		t.Fatalf("connected status = %s, want UP", connected.Status)
	}
}

func TestDisconnectedWebSocketFailsReadinessButNotLiveness(t *testing.T) {
	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", WebSocketConfig{
		ServerURL:         "ws://127.0.0.1:1",
		Path:              "/ws",
		ReconnectInterval: time.Millisecond,
	}, "test")
	svc.setRunning(true)
	defer svc.setRunning(false)
	svc.mu.Lock()
	svc.lastMessage = time.Now().Add(-31 * time.Minute)
	svc.mu.Unlock()

	manager := health.NewHealthManager(0, "test")
	manager.RegisterService(svc)
	handler := health.NewHealthHandler(manager)

	readiness := httptest.NewRecorder()
	handler.HandleReadinessCheck(
		readiness,
		httptest.NewRequest(http.MethodGet, "/health/readiness", nil),
	)
	if readiness.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", readiness.Code)
	}

	liveness := httptest.NewRecorder()
	handler.HandleLivenessCheck(
		liveness,
		httptest.NewRequest(http.MethodGet, "/health/liveness", nil),
	)
	if liveness.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200", liveness.Code)
	}
}

func TestWebSocketServiceStopAndProcessMessageBranches(t *testing.T) {
	ctx := context.Background()
	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", WebSocketConfig{
		ServerURL:         "ws://127.0.0.1:1",
		Path:              "/ws",
		ReconnectInterval: time.Millisecond,
	}, "test")
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
	if err := svc.ProcessIncomingMessage(ctx, ruleengine.Message{ID: "m1"}); err == nil {
		t.Fatalf("expected process before start error")
	}
}

func mustSettingsJSON(t *testing.T, rules []map[string]interface{}) string {
	t.Helper()

	data, err := json.Marshal(map[string]interface{}{"rules": rules})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	return string(data)
}

func mustConfluencePageJSON(t *testing.T, settingsJSON string) string {
	t.Helper()

	data, err := json.Marshal(map[string]interface{}{
		"body": map[string]interface{}{
			"storage": map[string]interface{}{
				"value": settingsJSON,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal Confluence page: %v", err)
	}
	return string(data)
}
