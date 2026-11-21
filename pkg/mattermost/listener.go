package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	"github.com/gorilla/websocket"
)

const (
	defaultReconnectInterval = 5 * time.Second
	defaultPingInterval      = 20 * time.Second
	defaultReadTimeout       = 60 * time.Second
	controlWriteWait         = time.Second
	channelLookupTimeout     = 5 * time.Second
)

// ListenerConfig controls the event stream connection. Zero values use the
// defaults: reconnect every 5s, ping every 20s, and treat 60s without any
// frame as a dead connection.
type ListenerConfig struct {
	ReconnectInterval time.Duration
	PingInterval      time.Duration
	ReadTimeout       time.Duration
}

func (c ListenerConfig) withDefaults() ListenerConfig {
	if c.ReconnectInterval == 0 {
		c.ReconnectInterval = defaultReconnectInterval
	}
	if c.PingInterval == 0 {
		c.PingInterval = defaultPingInterval
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	return c
}

// Handler receives each routed event. It runs on the listener's read loop,
// so a slow handler applies backpressure to the stream; hand work to a
// pipeline.Dispatcher to process events concurrently.
type Handler func(ctx context.Context, message ruleengine.Message)

// ListenerStats is a point-in-time view of the listener.
type ListenerStats struct {
	Connected    bool      `json:"connected"`
	BotUsername  string    `json:"bot_username,omitempty"`
	ConnectionID string    `json:"connection_id,omitempty"`
	Connects     int64     `json:"connects"`
	Routed       int64     `json:"routed"`
	Ignored      int64     `json:"ignored"`
	Malformed    int64     `json:"malformed"`
	LastEventAt  time.Time `json:"last_event_at,omitempty"`
}

// Listener reads the bot's Mattermost event stream and passes posts and
// reactions to a Handler. It reconnects after failures, detects silent dead
// connections with pings, and resumes the stream after a reconnect when the
// server still holds the missed events.
type Listener struct {
	client  *Client
	config  ListenerConfig
	handler Handler
	dialer  websocket.Dialer

	channels sync.Map // channel ID -> Channel

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	done    chan struct{}
	conn    *websocket.Conn
	bot     User
	stats   ListenerStats
	nextSeq int64
}

// NewListener validates its arguments and applies config defaults.
func NewListener(client *Client, config ListenerConfig, handler Handler) (*Listener, error) {
	if client == nil {
		return nil, errors.New("mattermost listener needs a client")
	}
	if handler == nil {
		return nil, errors.New("mattermost listener needs a handler")
	}
	config = config.withDefaults()
	if config.ReconnectInterval < 0 || config.PingInterval < 0 || config.ReadTimeout < 0 {
		return nil, errors.New("mattermost listener intervals must not be negative")
	}
	if config.PingInterval >= config.ReadTimeout {
		return nil, fmt.Errorf("mattermost listener ping interval (%v) must be less than read timeout (%v)", config.PingInterval, config.ReadTimeout)
	}
	return &Listener{
		client:  client,
		config:  config,
		handler: handler,
		dialer:  websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 10 * time.Second},
	}, nil
}

// Start checks the bot token and starts reading events in the background.
// A rejected token fails Start; an unreachable server does not, and the
// listener keeps retrying. ctx supplies values for handler contexts; Stop,
// not ctx, ends the listener.
func (l *Listener) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return nil
	}

	if bot, err := l.client.Me(ctx); err != nil {
		if executor.IsPermanent(err) {
			return fmt.Errorf("mattermost rejected the bot token: %w", err)
		}
		logger.WarnfWithContext(ctx, "Mattermost is not reachable yet, will keep retrying: %v", err)
	} else {
		l.bot = bot
		l.stats.BotUsername = bot.Username
	}

	l.running = true
	l.stop = make(chan struct{})
	l.done = make(chan struct{})
	go l.run(context.WithoutCancel(ctx), l.stop, l.done)
	return nil
}

// Stop closes the stream and waits for the read loop to exit until ctx ends.
func (l *Listener) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.running = false
	close(l.stop)
	conn, done := l.conn, l.done
	l.mu.Unlock()

	if conn != nil {
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(controlWriteWait))
		_ = conn.Close()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for mattermost listener: %w", ctx.Err())
	}
}

// Stats returns current counters and connection state.
func (l *Listener) Stats() ListenerStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	stats := l.stats
	stats.Connected = l.conn != nil
	return stats
}

// Bot returns the bot user once the listener has authenticated.
func (l *Listener) Bot() (User, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bot, l.bot.ID != ""
}

func (l *Listener) run(ctx context.Context, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-dialCtx.Done():
		}
	}()

	for {
		if err := l.session(dialCtx, ctx, stop); err != nil {
			select {
			case <-stop:
				return
			default:
			}
			logger.WarnfWithContext(ctx, "Mattermost event stream: %v; reconnecting in %v", err, l.config.ReconnectInterval)
		}
		select {
		case <-stop:
			return
		case <-time.After(l.config.ReconnectInterval):
		}
	}
}

// session connects once and reads until the connection ends.
func (l *Listener) session(dialCtx, ctx context.Context, stop <-chan struct{}) error {
	bot, err := l.ensureBot(dialCtx)
	if err != nil {
		return err
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+l.client.token)
	conn, response, err := l.dialer.DialContext(dialCtx, l.resumeURL(), header)
	if err != nil {
		if response != nil {
			return fmt.Errorf("connect: %w (HTTP %d)", err, response.StatusCode)
		}
		return fmt.Errorf("connect: %w", err)
	}

	l.mu.Lock()
	select {
	case <-stop:
		l.mu.Unlock()
		_ = conn.Close()
		return nil
	default:
	}
	l.conn = conn
	l.stats.Connects++
	l.mu.Unlock()
	logger.InfofWithContext(ctx, "Mattermost event stream connected to %s as @%s", l.client.ServerURL(), bot.Username)

	err = l.read(ctx, conn, bot, stop)

	l.mu.Lock()
	if l.conn == conn {
		l.conn = nil
	}
	l.mu.Unlock()
	_ = conn.Close()
	return err
}

func (l *Listener) ensureBot(ctx context.Context) (User, error) {
	l.mu.Lock()
	bot := l.bot
	l.mu.Unlock()
	if bot.ID != "" {
		return bot, nil
	}
	bot, err := l.client.Me(ctx)
	if err != nil {
		return User{}, err
	}
	l.mu.Lock()
	l.bot = bot
	l.stats.BotUsername = bot.Username
	l.mu.Unlock()
	return bot, nil
}

// resumeURL asks the server to replay events missed since the last one
// received on the previous connection.
func (l *Listener) resumeURL() string {
	endpoint := l.client.webSocketURL()
	l.mu.Lock()
	connectionID, nextSeq := l.stats.ConnectionID, l.nextSeq
	l.mu.Unlock()
	if connectionID == "" {
		return endpoint
	}
	query := url.Values{}
	query.Set("connection_id", connectionID)
	query.Set("sequence_number", strconv.FormatInt(nextSeq, 10))
	return endpoint + "?" + query.Encode()
}

func (l *Listener) read(ctx context.Context, conn *websocket.Conn, bot User, stop <-chan struct{}) error {
	extend := func() { _ = conn.SetReadDeadline(time.Now().Add(l.config.ReadTimeout)) }
	extend()
	conn.SetPongHandler(func(string) error {
		extend()
		return nil
	})
	conn.SetPingHandler(func(data string) error {
		extend()
		err := conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(controlWriteWait))
		if err == nil || errors.Is(err, websocket.ErrCloseSent) {
			return nil
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil
		}
		return err
	})

	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(l.config.PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(controlWriteWait)); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-stop:
				return nil
			default:
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return fmt.Errorf("no frames for %v, treating the connection as dead", l.config.ReadTimeout)
			}
			return fmt.Errorf("read: %w", err)
		}
		extend()
		l.handleFrame(ctx, data, bot)
	}
}

func (l *Listener) handleFrame(ctx context.Context, data []byte, bot User) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		l.count(func(s *ListenerStats) { s.Malformed++ })
		logger.WarnfWithContext(ctx, "Skipping Mattermost frame that is not a valid event (%d bytes): %v", len(data), err)
		return
	}
	if event.Event == "" {
		return // a reply to a client action
	}
	l.trackSequence(ctx, event)

	message, routed, err := ToMessage(event, bot)
	if err != nil {
		l.count(func(s *ListenerStats) { s.Malformed++ })
		logger.WarnfWithContext(ctx, "Skipping malformed Mattermost %s event: %v", event.Event, err)
		return
	}
	if !routed {
		l.count(func(s *ListenerStats) { s.Ignored++ })
		return
	}
	l.enrichChannel(ctx, message)
	l.count(func(s *ListenerStats) {
		s.Routed++
		s.LastEventAt = time.Now().UTC()
	})
	l.handler(ctx, message)
}

func (l *Listener) trackSequence(ctx context.Context, event Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if event.Event == "hello" {
		connectionID, _ := event.Data["connection_id"].(string)
		if previous := l.stats.ConnectionID; previous != "" && previous != connectionID {
			logger.WarnfWithContext(ctx, "Mattermost could not resume the previous event stream; events sent while disconnected were not received")
		}
		l.stats.ConnectionID = connectionID
	}
	l.nextSeq = event.Seq + 1
}

// enrichChannel fills in channel name and type for events that carry only the
// channel ID, such as reactions. Lookups are cached; a failed lookup leaves
// the fields empty.
func (l *Listener) enrichChannel(ctx context.Context, message ruleengine.Message) {
	channel, _ := message.Payload["channel"].(map[string]interface{})
	if channel == nil {
		return
	}
	id, _ := channel["id"].(string)
	if name, _ := channel["name"].(string); id == "" || name != "" {
		return
	}

	var info Channel
	if cached, ok := l.channels.Load(id); ok {
		info = cached.(Channel)
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, channelLookupTimeout)
		defer cancel()
		fetched, err := l.client.GetChannel(lookupCtx, id)
		if err != nil {
			logger.DebugfWithContext(ctx, "Could not look up Mattermost channel %s: %v", id, err)
			return
		}
		info = fetched
		l.channels.Store(id, info)
	}
	channel["name"] = info.Name
	channel["display_name"] = info.DisplayName
	channel["type"] = channelKind(info.Type)
	channel["team_id"] = info.TeamID
}

func (l *Listener) count(update func(*ListenerStats)) {
	l.mu.Lock()
	update(&l.stats)
	l.mu.Unlock()
}
