package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	"github.com/gorilla/websocket"
)

// newWSTestServer starts a WebSocket server that runs serve for every
// connection and counts connections.
func newWSTestServer(t *testing.T, serve func(conn *websocket.Conn)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var connections atomic.Int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		connections.Add(1)
		serve(conn)
	}))
	t.Cleanup(server.Close)
	return server, &connections
}

func startWSTestService(t *testing.T, serverURL string, cfg WebSocketConfig) *WebSocketService {
	t.Helper()
	cfg.ServerURL = httpToWS(serverURL)
	cfg.Path = "/ws"
	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = 10 * time.Millisecond
	}
	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", cfg, "test")
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start websocket service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	return svc
}

// readUntilClosed keeps reading so the server answers pings automatically.
func readUntilClosed(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func TestWebSocketServiceSkipsMalformedMessagesWithoutReconnecting(t *testing.T) {
	server, connections := newWSTestServer(t, func(conn *websocket.Conn) {
		for _, frame := range []string{
			`not json`,
			`{"id":"bad","type":123}`,
			`{"id":"good","type":"message","content":"hello"}`,
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				return
			}
		}
		readUntilClosed(conn)
	})

	received := make(chan ruleengine.Message, 4)
	svc := NewWebSocketService(WebSocketInputServiceName, "WorkflowEngine", WebSocketConfig{
		ServerURL:         httpToWS(server.URL),
		Path:              "/ws",
		ReconnectInterval: 10 * time.Millisecond,
	}, "test")
	svc.OnMessage(func(msg ruleengine.Message) { received <- msg })
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	select {
	case msg := <-received:
		if msg.ID != "good" {
			t.Fatalf("received %q, want the valid event", msg.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("valid event after malformed ones was not delivered")
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1 (malformed messages must not force a reconnect)", got)
	}
	if got := svc.GetMetrics(context.Background())["malformed_messages"]; got != int64(2) {
		t.Fatalf("malformed_messages = %v, want 2", got)
	}
}

func TestWebSocketServiceRedialsSilentConnection(t *testing.T) {
	// The server never reads, so it never answers pings: the connection looks
	// open but is dead from the client's point of view.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv, conns := newWSTestServer(t, func(*websocket.Conn) { <-release })
	startWSTestService(t, srv.URL, WebSocketConfig{
		PingInterval: 20 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
	})

	deadline := time.Now().Add(3 * time.Second)
	for conns.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("silent connection was not redialed (connections = %d)", conns.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketServiceKeepsQuietConnectionThatAnswersPings(t *testing.T) {
	srv, conns := newWSTestServer(t, readUntilClosed)
	svc := startWSTestService(t, srv.URL, WebSocketConfig{
		PingInterval: 20 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
	})

	// Several read timeouts pass without any event.
	time.Sleep(400 * time.Millisecond)
	if got := conns.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1: pongs must keep a quiet connection alive", got)
	}
	if state := svc.ConnectionState(); state != WSStateConnected {
		t.Fatalf("connection state = %s, want connected", state)
	}
}
