package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "project/internal/config"
	"project/internal/dataaccess"
	"project/internal/model"

	"github.com/gorilla/websocket"
)

func TestConfluenceSettingsServiceFetchReturnsErrorForNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	svc := newTestConfluenceSettingsService(t, server.URL)
	_, err := svc.fetchSettingsFromConfluence(context.Background())
	if err == nil {
		t.Fatalf("expected non-200 error")
	}
}

func TestConfluenceSettingsServiceFetchReturnsErrorForBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":{"storage":{"value":"not-json"}}}`))
	}))
	defer server.Close()

	svc := newTestConfluenceSettingsService(t, server.URL)
	_, err := svc.fetchSettingsFromConfluence(context.Background())
	if err == nil {
		t.Fatalf("expected bad settings JSON error")
	}
}

func TestMattermostServiceReturnsErrorForCreatePostFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "post failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	svc := NewMattermostService("MattermostService", "Global", internalconfig.MattermostConfig{
		ServerURL:    server.URL,
		APIToken:     "test-token",
		Channel:      "test-channel",
		WebsocketURL: "ws://127.0.0.1:1",
	}, "test")
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start mattermost service: %v", err)
	}
	defer svc.Stop(context.Background())

	err := svc.SendMessageToChannel(context.Background(), "test-channel", "hello")
	if err == nil {
		t.Fatalf("expected send failure")
	}
}

func TestBadgeDBServiceConcurrentSaveBadge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := NewBadgeDBService("BadgeDBService", "Global", "", "test")
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start badgedb service: %v", err)
	}
	defer svc.Stop(context.Background())

	const total = 50
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := svc.SaveBadge(ctx, model.Badge{
				ID:          fmt.Sprintf("badge-%d", i),
				Name:        "Concurrent Badge",
				Description: "Saved concurrently",
				UserID:      "u1",
				AwardedAt:   time.Now(),
				Type:        "debug",
			})
			if err != nil {
				t.Errorf("save badge %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	metrics := svc.GetMetrics(ctx)
	if metrics["db_entries"] != total {
		t.Fatalf("db_entries = %v, want %d", metrics["db_entries"], total)
	}
}

func TestWebSocketServiceReconnectsAfterConnectionDrops(t *testing.T) {
	var connectionCount atomic.Int32
	secondConnection := make(chan struct{})
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		count := connectionCount.Add(1)
		if count == 1 {
			_ = conn.Close()
			return
		}
		close(secondConnection)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	svc := NewWebSocketService("WebSocketServiceA", "A", internalconfig.WebSocketConfig{
		ServerURL:         httpToWS(server.URL),
		Path:              "/ws",
		ReconnectInterval: 10 * time.Millisecond,
	}, "test")
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start websocket service: %v", err)
	}
	defer svc.Stop(context.Background())

	select {
	case <-secondConnection:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for reconnect")
	}
}

func newTestConfluenceSettingsService(t *testing.T, endpoint string) *ConfluenceSettingsService {
	t.Helper()

	return NewConfluenceSettingsService(
		"ConfluenceSettingsService",
		"WorkflowEngine",
		dataaccess.NewCacheDataAccessor(context.Background()),
		internalconfig.ConfluenceSettingsConfig{
			PageID:          "settings-page-1",
			RefreshInterval: time.Hour,
			APIEndpoint:     endpoint,
			SpaceKey:        "TEST",
		},
		"test",
	)
}

func TestConfluenceSettingsServiceFetchParsesSettings(t *testing.T) {
	settings := map[string]interface{}{
		"rules": []map[string]interface{}{
			{
				"event_type":       "AAA",
				"pattern":          ".*",
				"channel_id":       "test-channel",
				"message_template": "Event {{.Content}}",
				"enabled":          true,
			},
		},
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"body": map[string]interface{}{
				"storage": map[string]interface{}{
					"value": string(settingsJSON),
				},
			},
		})
	}))
	defer server.Close()

	svc := newTestConfluenceSettingsService(t, server.URL)
	got, err := svc.fetchSettingsFromConfluence(context.Background())
	if err != nil {
		t.Fatalf("fetch settings: %v", err)
	}
	if len(got) != 1 || got[0].EventType != "AAA" {
		t.Fatalf("settings = %+v, want AAA rule", got)
	}
}

func httpToWS(rawURL string) string {
	return "ws" + rawURL[len("http"):]
}
