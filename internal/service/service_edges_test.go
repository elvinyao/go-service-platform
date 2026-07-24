package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/elvinyao/go-service-platform/internal/config"
	"github.com/elvinyao/go-service-platform/internal/dataaccess"
	"github.com/elvinyao/go-service-platform/internal/model"
	"github.com/elvinyao/go-service-platform/pkg/health"

	"github.com/gorilla/websocket"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testHTTPClient(status int, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
}

func TestMattermostHTTPProtocolWithoutNetwork(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v4/posts" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var post mattermostPost
		if err := json.NewDecoder(request.Body).Decode(&post); err != nil {
			t.Fatalf("decode post: %v", err)
		}
		if post.ChannelID != "channel" || post.Message != "hello" {
			t.Fatalf("post = %+v", post)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(strings.NewReader(`{"id":"post-1"}`)),
			Header:     make(http.Header),
		}, nil
	})

	svc := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{
		ServerURL: "http://mattermost.test",
		Channel:   "channel",
	}, "test")
	svc.mu.Lock()
	svc.httpClient = &http.Client{Transport: transport}
	svc.authToken = "test-token"
	svc.mu.Unlock()
	svc.LockRunning(true)

	if err := svc.SendMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func TestMattermostLoginProtocolWithoutNetwork(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v4/users/login" {
			t.Fatalf("login path = %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     http.Header{"Token": []string{"login-token"}},
		}, nil
	})}

	token, err := loginMattermost(context.Background(), client, "http://mattermost.test/", "user", "password")
	if err != nil || token != "login-token" {
		t.Fatalf("login token/error = %q/%v", token, err)
	}

	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("unauthorized")),
			Header:     make(http.Header),
		}, nil
	})
	if _, err := loginMattermost(context.Background(), client, "http://mattermost.test", "user", "bad"); err == nil {
		t.Fatalf("non-2xx login error = nil")
	}

	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})
	if _, err := loginMattermost(context.Background(), client, "http://mattermost.test", "user", "password"); err == nil {
		t.Fatalf("missing login token error = nil")
	}

	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed")
	})
	if _, err := loginMattermost(context.Background(), client, "http://mattermost.test", "user", "password"); err == nil {
		t.Fatalf("login transport error = nil")
	}

	if got := mattermostWebSocketURL("ws://mattermost.test/"); got != "ws://mattermost.test/api/v4/websocket" {
		t.Fatalf("websocket URL = %q", got)
	}
	full := "ws://mattermost.test/api/v4/websocket"
	if got := mattermostWebSocketURL(full); got != full {
		t.Fatalf("full websocket URL = %q", got)
	}
}

func TestMattermostErrorBranchesWithoutNetwork(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{}, "test")
	if err := svc.Start(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start error = %v", err)
	}

	svc.LockRunning(true)
	if err := svc.SendMessageToChannel(context.Background(), "channel", "message"); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("uninitialized client error = %v", err)
	}
	svc.LockRunning(false)

	emptyWebSocket := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{
		ServerURL: "http://mattermost.test",
		APIToken:  "token",
	}, "test")
	if err := emptyWebSocket.Start(context.Background()); err != nil {
		t.Fatalf("start without websocket URL: %v", err)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := emptyWebSocket.Stop(stopCtx); err != nil {
		t.Fatalf("stop without websocket URL: %v", err)
	}
}

func TestMattermostStartAcceptsNilContext(t *testing.T) {
	svc := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{}, "test")
	if err := svc.Start(nil); err != nil {
		t.Fatalf("start with nil context: %v", err)
	}
	if err := svc.Stop(nil); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestMattermostWebSocketLifecycle(t *testing.T) {
	upgrader := websocket.Upgrader{}
	connections := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/websocket" {
			http.NotFound(w, r)
			return
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer connection.Close()
		connections <- struct{}{}
		if err := connection.WriteJSON(map[string]string{"event": "hello"}); err != nil {
			return
		}
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	svc := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{
		ServerURL:    server.URL,
		WebsocketURL: httpToWS(server.URL),
		APIToken:     "token",
	}, "test")
	for attempt := 0; attempt < 2; attempt++ {
		if err := svc.Start(context.Background()); err != nil {
			t.Fatalf("start attempt %d: %v", attempt, err)
		}
		select {
		case <-connections:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for WebSocket attempt %d", attempt)
		}

		deadline := time.Now().Add(time.Second)
		for {
			result := (&mattermostConnectionChecker{service: svc}).Check(context.Background())
			if result.Status == health.StatusUp {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("WebSocket health = %+v", result)
			}
			time.Sleep(10 * time.Millisecond)
		}

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := svc.Stop(stopCtx); err != nil {
			stopCancel()
			t.Fatalf("stop attempt %d: %v", attempt, err)
		}
		stopCancel()
	}
}

func TestMattermostWebSocketReconnectsAfterDisconnect(t *testing.T) {
	upgrader := websocket.Upgrader{}
	connections := make(chan int, 2)
	var connectionCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer connection.Close()

		count := int(connectionCount.Add(1))
		connections <- count
		if count == 1 {
			return
		}
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	svc := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{
		ServerURL:    server.URL,
		WebsocketURL: httpToWS(server.URL),
		APIToken:     "token",
	}, "test")
	svc.wsRetry = 10 * time.Millisecond
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop(context.Background())

	for want := 1; want <= 2; want++ {
		select {
		case got := <-connections:
			if got != want {
				t.Fatalf("connection number = %d, want %d", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for connection %d", want)
		}
	}
}

func TestMattermostConnectionCheckerUsesWarningLevel(t *testing.T) {
	svc := NewMattermostService(MattermostServiceName, "WorkflowEngine", internalconfig.MattermostConfig{
		ServerURL:    "http://mattermost.test",
		WebsocketURL: "ws://mattermost.test",
	}, "test")
	checker := &mattermostConnectionChecker{service: svc}

	degraded := checker.Check(context.Background())
	if degraded.Status != health.StatusDegraded || degraded.Level != health.LevelWarning {
		t.Fatalf("degraded result = %+v", degraded)
	}

	svc.mu.Lock()
	svc.httpClient = &http.Client{}
	svc.wsClient = &websocket.Conn{}
	svc.mu.Unlock()
	up := checker.Check(context.Background())
	if up.Status != health.StatusUp || up.Level != health.LevelWarning {
		t.Fatalf("up result = %+v", up)
	}
}

func TestServiceStopsHonorCanceledContext(t *testing.T) {
	t.Run("WebSocket", func(t *testing.T) {
		svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", internalconfig.WebSocketConfig{}, "test")
		svc.LockRunning(true)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := svc.Stop(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("stop error = %v, want context canceled", err)
		}
	})

	t.Run("Confluence", func(t *testing.T) {
		svc := NewConfluenceSettingsService(
			ConfluenceSettingsServiceName,
			"WorkflowEngine",
			dataaccess.NewCacheDataAccessor(context.Background()),
			internalconfig.ConfluenceSettingsConfig{},
			"test",
		)
		svc.LockRunning(true)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := svc.Stop(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("stop error = %v, want context canceled", err)
		}
	})
}

func TestConfluencePeriodicRefreshCancelsWithoutHoldingServiceLock(t *testing.T) {
	settingsJSON := mustSettingsJSON(t, []map[string]interface{}{{
		"event_type":       "AAA",
		"channel_id":       "alerts",
		"message_template": "Event {{.Content}}",
		"enabled":          true,
	}})
	pageJSON, err := json.Marshal(map[string]interface{}{
		"body": map[string]interface{}{
			"storage": map[string]interface{}{"value": settingsJSON},
		},
	})
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}

	refreshStarted := make(chan struct{})
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(pageJSON))),
				Header:     make(http.Header),
			}, nil
		}
		close(refreshStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	svc := NewConfluenceSettingsService(
		ConfluenceSettingsServiceName,
		"WorkflowEngine",
		dataaccess.NewCacheDataAccessor(context.Background()),
		internalconfig.ConfluenceSettingsConfig{
			APIEndpoint:     "http://confluence.test",
			PageID:          "settings-page-1",
			RefreshInterval: 10 * time.Millisecond,
		},
		"test",
	)
	svc.httpClient = client
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		t.Fatalf("periodic refresh did not start")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("stop during refresh: %v", err)
	}
}

func TestWebSocketMessageHandlerAppliesBackpressure(t *testing.T) {
	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", internalconfig.WebSocketConfig{}, "test")
	svc.LockRunning(true)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	svc.OnMessage(func(model.Message) {
		close(handlerStarted)
		<-releaseHandler
	})

	processed := make(chan error, 1)
	go func() {
		processed <- svc.ProcessIncomingMessage(context.Background(), model.Message{ID: "m1"})
	}()

	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatalf("message handler did not start")
	}
	select {
	case err := <-processed:
		t.Fatalf("message processing returned before handler release: %v", err)
	default:
	}

	close(releaseHandler)
	select {
	case err := <-processed:
		if err != nil {
			t.Fatalf("process message: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("message processing did not finish")
	}
}

func TestServiceEndpointsNormalizeTrailingSlashes(t *testing.T) {
	if got := webSocketEndpoint(internalconfig.WebSocketConfig{ServerURL: "ws://example.test/", Path: "/ws"}); got != "ws://example.test/ws" {
		t.Fatalf("WebSocket endpoint = %q", got)
	}

	var requestedURL string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURL = request.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"body":{"storage":{"value":"{\"rules\":[]}"}}}`)),
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := fetchConfluenceSettings(context.Background(), client, "http://example.test/", "settings-page-1"); err != nil {
		t.Fatalf("fetch settings: %v", err)
	}
	if requestedURL != "http://example.test/rest/api/content/settings-page-1" {
		t.Fatalf("Confluence endpoint = %q", requestedURL)
	}
}

func TestConfluenceSettingsServiceFetchReturnsErrorForNon200(t *testing.T) {
	svc := newTestConfluenceSettingsService(t, testHTTPClient(http.StatusServiceUnavailable, "unavailable"))
	_, err := svc.fetchSettingsFromConfluence(context.Background())
	if err == nil {
		t.Fatalf("expected non-200 error")
	}
}

func TestConfluenceSettingsServiceFetchReturnsErrorForBadJSON(t *testing.T) {
	svc := newTestConfluenceSettingsService(t, testHTTPClient(http.StatusOK, `{"body":{"storage":{"value":"not-json"}}}`))
	_, err := svc.fetchSettingsFromConfluence(context.Background())
	if err == nil {
		t.Fatalf("expected bad settings JSON error")
	}
}

func TestMattermostServiceReturnsErrorForCreatePostFailure(t *testing.T) {
	svc := NewMattermostService("MattermostService", "Global", internalconfig.MattermostConfig{
		ServerURL: "http://mattermost.test",
		APIToken:  "test-token",
		Channel:   "test-channel",
	}, "test", testHTTPClient(http.StatusInternalServerError, "post failed"))
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

	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", internalconfig.WebSocketConfig{
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

func newTestConfluenceSettingsService(t *testing.T, client *http.Client) *ConfluenceSettingsService {
	t.Helper()

	return NewConfluenceSettingsService(
		"ConfluenceSettingsService",
		"WorkflowEngine",
		dataaccess.NewCacheDataAccessor(context.Background()),
		internalconfig.ConfluenceSettingsConfig{
			PageID:          "settings-page-1",
			RefreshInterval: time.Hour,
			APIEndpoint:     "http://confluence.test",
			SpaceKey:        "TEST",
		},
		"test",
		client,
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
	svc := newTestConfluenceSettingsService(t, testHTTPClient(http.StatusOK, mustConfluencePageJSON(t, string(settingsJSON))))
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
