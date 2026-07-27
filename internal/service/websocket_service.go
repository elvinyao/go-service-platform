package service

import (
	"context"
	"fmt"
	"github.com/elvinyao/go-service-platform/internal/config"
	"github.com/elvinyao/go-service-platform/internal/model"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WebSocketService struct {
	*BaseService
	mu             sync.Mutex
	messageHandler func(msg model.Message)
	connections    int
	lastMessage    time.Time
	config         config.WebSocketConfig
	conn           *websocket.Conn
	stopChan       chan struct{}
	done           chan struct{}
}

func NewWebSocketService(name, workflow string, cfg config.WebSocketConfig, version string) *WebSocketService {
	s := &WebSocketService{
		BaseService: NewBaseService(name, workflow, "websocket", version),
		connections: 0,
		lastMessage: time.Time{},
		config:      cfg,
		stopChan:    make(chan struct{}),
		done:        make(chan struct{}),
	}

	// Add custom health checkers
	s.AddHealthChecker(&websocketConnectionChecker{service: s})

	return s
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

	// Mark as running first
	s.setRunning(true)

	// Reset channels for a fresh start
	s.stopChan = make(chan struct{})
	s.done = make(chan struct{})
	stopChan := s.stopChan
	done := s.done
	cfg := s.config

	// Launch WebSocket connection goroutine
	go s.connectLoop(ctx, stopChan, done, cfg)

	logger.InfofWithContext(ctx, "Service %s started successfully", s.GetName())
	return nil
}

func (s *WebSocketService) connectLoop(ctx context.Context, stopChan <-chan struct{}, done chan<- struct{}, cfg config.WebSocketConfig) {
	defer close(done)

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

		logger.InfofWithContext(ctx, "WebSocket connecting to %s", url)
		conn, _, err := dialer.DialContext(dialCtx, url, nil)
		if err != nil {
			// If stopped, exit immediately
			select {
			case <-stopChan:
				return
			default:
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
			conn.Close()
			return
		default:
		}
		s.conn = conn
		s.connections = 1
		s.mu.Unlock()

		logger.InfofWithContext(ctx, "WebSocket connected to %s", url)

		// Read loop
		stopped := false
		for {
			var msg model.Message
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
			s.ProcessIncomingMessage(ctx, msg)
		}

		// Clean up and retry after the connection is lost.
		conn.Close()
		s.mu.Lock()
		s.conn = nil
		s.connections = 0
		s.mu.Unlock()

		if stopped {
			return
		}

		select {
		case <-stopChan:
			return
		default:
		}

		logger.InfofWithContext(ctx, "WebSocket disconnected, reconnecting in %v", cfg.ReconnectInterval)

		select {
		case <-stopChan:
			return
		case <-time.After(cfg.ReconnectInterval):
		}
	}
}

func webSocketEndpoint(cfg config.WebSocketConfig) string {
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

	// Signal goroutine to stop
	stopChan := s.stopChan
	done := s.done
	close(stopChan)

	// Close connection if active
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	s.connections = 0
	s.setRunning(false)
	s.mu.Unlock()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("wait for WebSocket shutdown: %w", ctx.Err())
	case <-timer.C:
		return fmt.Errorf("timed out waiting for WebSocket shutdown")
	}

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

func (s *WebSocketService) OnMessage(handler func(msg model.Message)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messageHandler = handler
}

func (s *WebSocketService) ProcessIncomingMessage(ctx context.Context, message model.Message) error {
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

	metrics := make(map[string]interface{})
	for k, v := range baseMetrics {
		metrics[k] = v
	}

	metrics["connections"] = s.connections
	metrics["server_url"] = s.config.ServerURL
	metrics["path"] = s.config.Path
	metrics["reconnect_interval"] = s.config.ReconnectInterval.String()

	if !s.lastMessage.IsZero() {
		metrics["last_message"] = s.lastMessage.Format(time.RFC3339)
		metrics["last_message_age_seconds"] = time.Since(s.lastMessage).Seconds()
	}

	return metrics
}

func (s *WebSocketService) Configure(ctx context.Context, cfg interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")

	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	newConfig, ok := cfg.(config.WebSocketConfig)
	if !ok {
		return errors.New(errors.TypeInvalidInput, "Invalid configuration type for WebSocketService", nil)
	}
	if err := validateWebSocketServiceConfig(newConfig); err != nil {
		return errors.Wrap(err, "Invalid WebSocketService configuration", errors.TypeInvalidInput)
	}

	s.mu.Lock()
	s.config = newConfig
	s.mu.Unlock()
	logger.InfofWithContext(ctx, "Service %s configured successfully", s.GetName())
	return nil
}

func validateWebSocketServiceConfig(cfg config.WebSocketConfig) error {
	parsed, err := url.ParseRequestURI(cfg.ServerURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return fmt.Errorf("server URL must be an absolute ws or wss URL")
	}
	if cfg.Path == "" || !strings.HasPrefix(cfg.Path, "/") {
		return fmt.Errorf("path must start with /")
	}
	if cfg.ReconnectInterval <= 0 {
		return fmt.Errorf("reconnect interval must be greater than zero")
	}
	return nil
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

	// Get connection count
	c.service.mu.Lock()
	connections := c.service.connections
	lastMessage := c.service.lastMessage
	c.service.mu.Unlock()

	result.AddDetail("connections", connections)

	// Check connection count
	if connections == 0 {
		result.SetStatus(health.StatusDown, "No active WebSocket connections")
	} else {
		result.SetStatus(health.StatusUp, fmt.Sprintf("Active WebSocket connections: %d", connections))

		// Message freshness is meaningful only while the input remains connected.
		if !lastMessage.IsZero() {
			messageAge := time.Since(lastMessage)
			result.AddDetail("last_message_age_minutes", messageAge.Minutes())

			if messageAge > 30*time.Minute {
				result.SetStatus(health.StatusDegraded, fmt.Sprintf("No messages received in %v", messageAge))
			}
		}
	}

	result.Complete()
	return result
}
