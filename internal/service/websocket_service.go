package service

import (
	"context"
	"fmt"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketConfig holds configuration for the WebSocket service connection
type WebSocketConfig struct {
	ServerURL         string
	Path              string
	ReconnectInterval time.Duration
}

// WSConnectionState describes the explicit connection state of WebSocketService.
type WSConnectionState string

const (
	WSStateDisconnected WSConnectionState = "disconnected"
	WSStateDialing      WSConnectionState = "dialing"
	WSStateConnected    WSConnectionState = "connected"
	WSStateBackoff      WSConnectionState = "backoff"
)

// WSMessageFreshnessState describes message activity freshness on an active connection.
type WSMessageFreshnessState string

const (
	WSFreshnessIdle  WSMessageFreshnessState = "idle"
	WSFreshnessFresh WSMessageFreshnessState = "fresh"
	WSFreshnessStale WSMessageFreshnessState = "stale"
)

const wsStaleMessageThreshold = 30 * time.Minute

type wsSession struct {
	generation uint64
	stopChan   chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
}

func newWSSession(generation uint64) *wsSession {
	return &wsSession{
		generation: generation,
		stopChan:   make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (ws *wsSession) signalStop() {
	ws.stopOnce.Do(func() {
		close(ws.stopChan)
	})
}

type WebSocketService struct {
	*BaseService
	mu             sync.Mutex
	connState      WSConnectionState
	generation     uint64
	session        *wsSession
	messageHandler func(msg ruleengine.Message)
	connections    int
	lastMessage    time.Time
	config         WebSocketConfig
	conn           *websocket.Conn
}

func NewWebSocketService(name, workflow string, cfg WebSocketConfig, version string) *WebSocketService {
	s := &WebSocketService{
		BaseService: NewBaseService(name, workflow, "websocket", version),
		connState:   WSStateDisconnected,
		session:     newWSSession(0),
		connections: 0,
		lastMessage: time.Time{},
		config:      cfg,
	}

	// Add custom health checkers
	s.AddHealthChecker(&websocketConnectionChecker{service: s})

	return s
}

// ConnectionState returns the current explicit WebSocket connection state.
func (s *WebSocketService) ConnectionState() WSConnectionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveConnStateLocked()
}

func (s *WebSocketService) effectiveConnStateLocked() WSConnectionState {
	if s.connections > 0 {
		return WSStateConnected
	}
	if s.connState == WSStateConnected {
		return WSStateDisconnected
	}
	return s.connState
}

func (s *WebSocketService) freshnessStateLocked(now time.Time) (WSMessageFreshnessState, time.Duration) {
	if s.lastMessage.IsZero() {
		return WSFreshnessIdle, 0
	}
	age := now.Sub(s.lastMessage)
	if age > wsStaleMessageThreshold {
		return WSFreshnessStale, age
	}
	return WSFreshnessFresh, age
}

func (s *WebSocketService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	s.generation++
	generation := s.generation
	session := newWSSession(generation)
	s.session = session
	s.connState = WSStateDialing
	s.TransitionState(ServiceStateRunning)
	cfg := s.config

	// Launch generation-fenced WebSocket connection goroutine
	go s.connectLoop(ctx, generation, session, cfg)

	logger.InfofWithContext(ctx, "Service %s started successfully", s.GetName())
	return nil
}

func (s *WebSocketService) transitionConnForGeneration(generation uint64, next WSConnectionState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || !s.IsRunning(context.Background()) {
		return false
	}
	s.connState = next
	return true
}

func (s *WebSocketService) connectLoop(ctx context.Context, generation uint64, session *wsSession, cfg WebSocketConfig) {
	defer close(session.done)
	stopChan := session.stopChan

	// Create a context that cancels when stopChan is closed.
	// This lets us cancel blocking Dial calls immediately on Stop().
	dialCtx, dialCancel := context.WithCancel(context.Background())
	go func(stop <-chan struct{}) {
		<-stop
		dialCancel()
	}(stopChan)

	dialer := websocket.Dialer{
		NetDialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
	}

	url := webSocketEndpoint(cfg)

	for {
		select {
		case <-stopChan:
			return
		default:
		}

		if !s.transitionConnForGeneration(generation, WSStateDialing) {
			return
		}

		logger.InfofWithContext(ctx, "WebSocket connecting to %s", url)
		conn, _, err := dialer.DialContext(dialCtx, url, nil)
		if err != nil {
			// If stopped, exit immediately
			select {
			case <-stopChan:
				return
			default:
			}
			if !s.transitionConnForGeneration(generation, WSStateBackoff) {
				return
			}
			logger.WarnfWithContext(ctx, "WebSocket dial error: %v, retrying in %v", err, cfg.ReconnectInterval)
			select {
			case <-stopChan:
				return
			case <-time.After(cfg.ReconnectInterval):
				continue
			}
		}

		s.mu.Lock()
		select {
		case <-stopChan:
			s.mu.Unlock()
			_ = conn.Close()
			return
		default:
		}
		if s.generation != generation || !s.IsRunning(ctx) {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conn = conn
		s.connections = 1
		s.connState = WSStateConnected
		s.mu.Unlock()

		logger.InfofWithContext(ctx, "WebSocket connected to %s", url)

		// Read loop
		stopped := false
		for {
			var msg ruleengine.Message
			err := conn.ReadJSON(&msg)
			if err != nil {
				select {
				case <-stopChan:
					logger.DebugfWithContext(ctx, "WebSocket read loop exiting during shutdown: %v", err)
					stopped = true
				default:
					logger.WarnfWithContext(ctx, "WebSocket read error: %v", err)
				}
				break
			}

			// Skip system messages (e.g. welcome message from fake server)
			if msg.Type == "system" {
				logger.DebugfWithContext(ctx, "Skipping system message: %s", msg.Content)
				continue
			}

			logger.DebugfWithContext(ctx, "WebSocket received message: type=%s", msg.Type)
			_ = s.ProcessIncomingMessage(ctx, msg)
		}

		// Clean up and retry after the connection is lost.
		_ = conn.Close()
		s.mu.Lock()
		if s.generation == generation && s.conn == conn {
			s.conn = nil
			s.connections = 0
			s.connState = WSStateDisconnected
		}
		s.mu.Unlock()

		if stopped {
			return
		}

		select {
		case <-stopChan:
			return
		default:
		}

		if !s.transitionConnForGeneration(generation, WSStateBackoff) {
			return
		}
		logger.InfofWithContext(ctx, "WebSocket disconnected, reconnecting in %v", cfg.ReconnectInterval)

		select {
		case <-stopChan:
			return
		case <-time.After(cfg.ReconnectInterval):
		}
	}
}

func webSocketEndpoint(cfg WebSocketConfig) string {
	return strings.TrimRight(cfg.ServerURL, "/") + cfg.Path
}

func (s *WebSocketService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()

	if !s.IsRunning(ctx) {
		s.mu.Unlock()
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	s.generation++
	session := s.session
	conn := s.conn
	s.conn = nil
	s.connections = 0
	s.connState = WSStateDisconnected
	s.TransitionState(ServiceStateStopping)
	s.mu.Unlock()

	if session != nil {
		session.signalStop()
	}
	if conn != nil {
		_ = conn.Close()
	}

	if session != nil {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-session.done:
		case <-ctx.Done():
			s.TransitionState(ServiceStateStopped)
			return fmt.Errorf("wait for WebSocket shutdown: %w", ctx.Err())
		case <-timer.C:
			s.TransitionState(ServiceStateStopped)
			return fmt.Errorf("timed out waiting for WebSocket shutdown")
		}
	}

	s.TransitionState(ServiceStateStopped)
	logger.InfofWithContext(ctx, "Service %s stopped successfully", s.GetName())
	return nil
}

func (s *WebSocketService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")

	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running, starting it", s.GetName())
		return s.Start(ctx)
	}

	logger.InfofWithContext(ctx, "Restarting service: %s", s.GetName())

	if err := s.Stop(ctx); err != nil {
		return err
	}

	return s.Start(ctx)
}

func (s *WebSocketService) OnMessage(handler func(msg ruleengine.Message)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messageHandler = handler
}

func (s *WebSocketService) ProcessIncomingMessage(ctx context.Context, message ruleengine.Message) error {
	ctx = appctx.WithOperationName(ctx, "process_message")

	if !s.IsRunning(ctx) {
		return errors.New(errors.TypeServiceUnavailable, "Service is not running", nil)
	}

	logger.DebugfWithContext(ctx, "Processing incoming message: %+v", message)

	s.mu.Lock()
	handler := s.messageHandler
	s.lastMessage = time.Now()
	s.mu.Unlock()

	if handler != nil {
		handlerCtx := appctx.WithOperationName(ctx, "message_handler")
		logger.DebugfWithContext(handlerCtx, "Calling message handler for message: %+v", message)
		handler(message)
	} else {
		logger.WarnfWithContext(ctx, "No message handler registered for service %s", s.GetName())
	}

	return nil
}

func (s *WebSocketService) GetMetrics(ctx context.Context) map[string]interface{} {
	baseMetrics := s.BaseService.GetMetrics(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	connState := s.effectiveConnStateLocked()
	freshnessState, msgAge := s.freshnessStateLocked(now)

	metrics := make(map[string]interface{})
	for k, v := range baseMetrics {
		metrics[k] = v
	}

	metrics["connection_state"] = string(connState)
	metrics["message_freshness"] = string(freshnessState)
	metrics["handler_registered"] = s.messageHandler != nil
	metrics["connections"] = s.connections
	metrics["server_url"] = s.config.ServerURL
	metrics["path"] = s.config.Path
	metrics["reconnect_interval"] = s.config.ReconnectInterval.String()

	if freshnessState != WSFreshnessIdle {
		metrics["last_message"] = s.lastMessage.Format(time.RFC3339)
		metrics["last_message_age_seconds"] = msgAge.Seconds()
	}

	return metrics
}

// websocketConnectionChecker checks WebSocket connection status
type websocketConnectionChecker struct {
	service *WebSocketService
}

// Check implements the Checker interface
func (c *websocketConnectionChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("websocket-connections", health.CategoryConnectivity)
	result.Level = health.LevelCritical

	// Check if service is running
	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "WebSocket service is not running")
		result.Complete()
		return result
	}

	c.service.mu.Lock()
	connections := c.service.connections
	connState := c.service.effectiveConnStateLocked()
	freshnessState, messageAge := c.service.freshnessStateLocked(time.Now())
	c.service.mu.Unlock()

	result.AddDetail("connections", connections)
	result.AddDetail("connection_state", string(connState))
	result.AddDetail("message_freshness", string(freshnessState))

	switch connState {
	case WSStateConnected:
		result.SetStatus(health.StatusUp, fmt.Sprintf("Active WebSocket connections: %d", connections))
		if freshnessState != WSFreshnessIdle {
			result.AddDetail("last_message_age_minutes", messageAge.Minutes())
			if freshnessState == WSFreshnessStale {
				result.SetStatus(health.StatusDegraded, fmt.Sprintf("No messages received in %v", messageAge))
			}
		}
	default:
		result.SetStatus(health.StatusDown, "No active WebSocket connections")
	}

	result.Complete()
	return result
}
