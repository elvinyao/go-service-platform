package mattermost

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	"github.com/gorilla/websocket"
)

var fastListener = ListenerConfig{
	ReconnectInterval: 10 * time.Millisecond,
	PingInterval:      20 * time.Millisecond,
	ReadTimeout:       200 * time.Millisecond,
}

type collector struct {
	mu       sync.Mutex
	messages []ruleengine.Message
	got      chan struct{}
}

func newCollector() *collector { return &collector{got: make(chan struct{}, 100)} }

func (c *collector) handle(_ context.Context, message ruleengine.Message) {
	c.mu.Lock()
	c.messages = append(c.messages, message)
	c.mu.Unlock()
	c.got <- struct{}{}
}

func (c *collector) wait(t *testing.T, n int) []ruleengine.Message {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-c.got:
		case <-time.After(3 * time.Second):
			t.Fatalf("received %d of %d messages", i, n)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ruleengine.Message(nil), c.messages...)
}

func startListener(t *testing.T, fake *fakeMattermost, handler Handler) *Listener {
	t.Helper()
	listener, err := NewListener(fake.client(t), fastListener, handler)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	if err := listener.Start(context.Background()); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop(context.Background()) })
	return listener
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNewListenerValidatesArguments(t *testing.T) {
	client, _ := NewClient("http://localhost:1", "t", nil)
	handler := func(context.Context, ruleengine.Message) {}
	if _, err := NewListener(nil, ListenerConfig{}, handler); err == nil {
		t.Fatalf("expected nil client error")
	}
	if _, err := NewListener(client, ListenerConfig{}, nil); err == nil {
		t.Fatalf("expected nil handler error")
	}
	if _, err := NewListener(client, ListenerConfig{PingInterval: time.Minute, ReadTimeout: time.Minute}, handler); err == nil {
		t.Fatalf("expected ping interval error")
	}
	if _, err := NewListener(client, ListenerConfig{ReconnectInterval: -1}, handler); err == nil {
		t.Fatalf("expected negative interval error")
	}
	listener, err := NewListener(client, ListenerConfig{}, handler)
	if err != nil || listener.config != (ListenerConfig{ReconnectInterval: 5 * time.Second, PingInterval: 20 * time.Second, ReadTimeout: 60 * time.Second}) {
		t.Fatalf("defaults = %+v, %v", listener.config, err)
	}
}

func TestListenerStartRejectsInvalidToken(t *testing.T) {
	fake := newFakeMattermost(t)
	client, _ := NewClient(fake.server.URL, "wrong", nil)
	listener, _ := NewListener(client, fastListener, func(context.Context, ruleengine.Message) {})
	err := listener.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rejected the bot token") {
		t.Fatalf("start error = %v", err)
	}
	if err := listener.Stop(context.Background()); err != nil {
		t.Fatalf("stop of a listener that never started: %v", err)
	}
}

func TestListenerRoutesEventsAndSkipsTheRest(t *testing.T) {
	fake := newFakeMattermost(t)
	fake.onConnect = func(conn *websocket.Conn, _ int) {
		sendEvent(t, conn, helloEvent("conn-1"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("not json"))
		sendEvent(t, conn, map[string]interface{}{"status": "OK", "seq_reply": 1})
		sendEvent(t, conn, map[string]interface{}{"event": "typing", "seq": 1})
		sendEvent(t, conn, map[string]interface{}{"event": "posted", "seq": 2, "data": map[string]interface{}{"post": "{"}})
		sendEvent(t, conn, postedEvent(3, Post{ID: "own", ChannelID: testChannelID, UserID: testBotID}, nil))
		sendEvent(t, conn, postedEvent(4, Post{ID: "p1", ChannelID: testChannelID, UserID: "alice-id", Message: "!ping"}, nil))
		sendEvent(t, conn, reactionEvent(5, TypeReactionAdded, "alice-id", "p1", "+1"))
		sendEvent(t, conn, reactionEvent(6, TypeReactionAdded, "bob-id", "p1", "eyes"))
		readUntilClosed(conn)
	}
	got := newCollector()
	listener := startListener(t, fake, got.handle)

	messages := got.wait(t, 3)
	if messages[0].ID != "p1" || messages[1].Content != "+1" || messages[2].Content != "eyes" {
		t.Fatalf("messages = %+v", messages)
	}
	// Reactions carry only the channel ID; the listener looks the name up once.
	for _, message := range messages[1:] {
		if name, _ := message.Field("payload.channel.name"); name != "deploy-approvals" {
			t.Fatalf("reaction channel name = %v", name)
		}
		if kind, _ := message.Field("payload.channel.type"); kind != "public" {
			t.Fatalf("reaction channel type = %v", kind)
		}
	}
	if fake.channelCalls != 1 {
		t.Fatalf("channel lookups = %d, want 1 (cached)", fake.channelCalls)
	}

	stats := listener.Stats()
	if !stats.Connected || stats.Routed != 3 || stats.Malformed != 2 || stats.Ignored != 3 || stats.BotUsername != "gsp-bot" || stats.ConnectionID != "conn-1" || stats.LastEventAt.IsZero() {
		t.Fatalf("stats = %+v", stats)
	}
	if bot, ok := listener.Bot(); !ok || bot.ID != testBotID {
		t.Fatalf("bot = %+v, %v", bot, ok)
	}
}

func TestListenerLeavesChannelFieldsWhenLookupFails(t *testing.T) {
	fake := newFakeMattermost(t)
	fake.onConnect = func(conn *websocket.Conn, _ int) {
		encoded := `{"user_id":"alice-id","post_id":"p1","emoji_name":"+1","channel_id":"unknown"}`
		sendEvent(t, conn, map[string]interface{}{"event": TypeReactionAdded, "seq": 1, "data": map[string]interface{}{"reaction": encoded}})
		readUntilClosed(conn)
	}
	got := newCollector()
	startListener(t, fake, got.handle)
	message := got.wait(t, 1)[0]
	if _, ok := message.Field("payload.channel.name"); ok {
		t.Fatalf("channel name should stay absent after a failed lookup: %v", message.Payload["channel"])
	}
}

func TestListenerResumesStreamAfterReconnect(t *testing.T) {
	fake := newFakeMattermost(t)
	fake.onConnect = func(conn *websocket.Conn, connection int) {
		switch connection {
		case 1:
			sendEvent(t, conn, helloEvent("conn-A"))
			sendEvent(t, conn, postedEvent(1, Post{ID: "p1", ChannelID: testChannelID, UserID: "alice-id"}, nil))
			sendEvent(t, conn, postedEvent(2, Post{ID: "p2", ChannelID: testChannelID, UserID: "alice-id"}, nil))
			// Drop the connection without a close frame.
		case 2:
			// The server could not resume and starts a new stream.
			sendEvent(t, conn, helloEvent("conn-B"))
			sendEvent(t, conn, postedEvent(1, Post{ID: "p3", ChannelID: testChannelID, UserID: "alice-id"}, nil))
			readUntilClosed(conn)
		}
	}
	got := newCollector()
	listener := startListener(t, fake, got.handle)
	got.wait(t, 3)

	queries := fake.queries()
	if queries[0] != "" {
		t.Fatalf("first connection should not ask to resume: %q", queries[0])
	}
	if !strings.Contains(queries[1], "connection_id=conn-A") || !strings.Contains(queries[1], "sequence_number=3") {
		t.Fatalf("reconnect query = %q, want connection_id=conn-A and sequence_number=3", queries[1])
	}
	if stats := listener.Stats(); stats.ConnectionID != "conn-B" || stats.Connects != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestListenerRedialsSilentConnectionAndAnswersServerPings(t *testing.T) {
	fake := newFakeMattermost(t)
	pong := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fake.onConnect = func(conn *websocket.Conn, connection int) {
		if connection == 1 {
			conn.SetPongHandler(func(string) error {
				select {
				case pong <- struct{}{}:
				default:
				}
				return nil
			})
			_ = conn.WriteControl(websocket.PingMessage, []byte("srv"), time.Now().Add(time.Second))
			// Read briefly so the pong is processed, then go silent: once
			// reading stops, the client's pings are never answered.
			_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			go func() { _, _, _ = conn.ReadMessage() }()
			<-release
			return
		}
		readUntilClosed(conn)
	}
	listener := startListener(t, fake, func(context.Context, ruleengine.Message) {})

	select {
	case <-pong:
	case <-time.After(3 * time.Second):
		t.Fatalf("listener did not answer the server ping")
	}
	waitFor(t, "a redial of the silent connection", func() bool { return listener.Stats().Connects >= 2 })
}

func TestListenerWaitsForUnreachableServer(t *testing.T) {
	fake := newFakeMattermost(t)
	fake.meStatus = http.StatusBadGateway
	listener := startListener(t, fake, func(context.Context, ruleengine.Message) {})
	time.Sleep(50 * time.Millisecond)
	if listener.Stats().Connected {
		t.Fatalf("should not connect before the bot user is known")
	}

	fake.mu.Lock()
	fake.meStatus = http.StatusOK
	fake.wsStatus = http.StatusServiceUnavailable
	fake.mu.Unlock()
	waitFor(t, "the bot user", func() bool { _, ok := listener.Bot(); return ok })
	time.Sleep(50 * time.Millisecond) // a few rejected upgrades

	fake.mu.Lock()
	fake.wsStatus = 0
	fake.mu.Unlock()
	waitFor(t, "a connection", func() bool { return listener.Stats().Connected })
}

func TestListenerStartAndStopAreIdempotent(t *testing.T) {
	fake := newFakeMattermost(t)
	listener := startListener(t, fake, func(context.Context, ruleengine.Message) {})
	if err := listener.Start(context.Background()); err != nil {
		t.Fatalf("second start: %v", err)
	}
	waitFor(t, "a connection", func() bool { return listener.Stats().Connected })
	if err := listener.Stop(nil); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := listener.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if listener.Stats().Connected {
		t.Fatalf("still connected after stop")
	}
	if err := listener.Start(nil); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitFor(t, "a reconnection after restart", func() bool { return listener.Stats().Connected })
}

func TestListenerStopHonorsContext(t *testing.T) {
	fake := newFakeMattermost(t)
	block := make(chan struct{})
	entered := make(chan struct{})
	fake.onConnect = func(conn *websocket.Conn, _ int) {
		sendEvent(t, conn, postedEvent(1, Post{ID: "p1", ChannelID: testChannelID, UserID: "alice-id"}, nil))
		readUntilClosed(conn)
	}
	listener := startListener(t, fake, func(context.Context, ruleengine.Message) {
		close(entered)
		<-block
	})
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := listener.Stop(ctx); err == nil {
		t.Fatalf("stop should time out while the handler is blocked")
	}
	close(block)
}
