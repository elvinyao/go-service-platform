package service

import (
	"context"
	"fmt"
	"project/internal/model"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"time"
)

type WebSocketService struct {
	*BaseService
	mu             sync.Mutex
	messageHandler func(msg model.Message)
	connections    int
	lastMessage    time.Time
}

func NewWebSocketService(name, workflow string, version string) *WebSocketService {
	s := &WebSocketService{
		BaseService: NewBaseService(name, workflow, "websocket", version),
		connections: 0,
		lastMessage: time.Time{},
	}

	// Add custom health checkers
	s.AddHealthChecker(&websocketConnectionChecker{service: s})

	return s
}

func (s *WebSocketService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRunning() {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// Initialize websocket server
	// In a real implementation, you would set up websocket listeners here

	// Mark service as running
	s.LockRunning(true)

	// Setup cleanup on context cancellation
	go func() {
		<-ctx.Done()
		stopCtx := appctx.NewContext(context.Background())
		if err := s.Stop(stopCtx); err != nil {
			logger.WithContextError(stopCtx, err).Error("Error stopping service on context cancellation")
		}
	}()

	return nil
}

func (s *WebSocketService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	// Close all websocket connections
	// In a real implementation, you would close active connections

	// Mark service as not running
	s.LockRunning(false)

	return nil
}

func (s *WebSocketService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")
	logger.InfofWithContext(ctx, "Restarting service: %s", s.GetName())

	if err := s.Stop(ctx); err != nil {
		return errors.Wrap(err, "Failed to stop service during restart", errors.TypeServiceUnavailable)
	}

	return s.Start(ctx)
}

// OnMessage sets the message handler for this service
func (s *WebSocketService) OnMessage(handler func(msg model.Message)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messageHandler = handler
}

// ProcessIncomingMessage processes an incoming message and routes it to the handler
func (s *WebSocketService) ProcessIncomingMessage(ctx context.Context, msg model.Message) {
	ctx = appctx.WithOperationName(ctx, "process_message")
	logger.DebugfWithContext(ctx, "Processing incoming message of type: %s", msg.Type)

	s.mu.Lock()
	handler := s.messageHandler
	s.lastMessage = time.Now()
	s.mu.Unlock()

	if handler != nil {
		handler(msg)
	} else {
		logger.WarnWithContext(ctx, "No message handler registered")
	}
}

// GetMetrics implements Service interface with context support
func (s *WebSocketService) GetMetrics(ctx context.Context) map[string]interface{} {
	ctx = appctx.WithOperationName(ctx, "get_metrics")
	logger.DebugfWithContext(ctx, "Getting metrics for service: %s", s.GetName())

	s.mu.Lock()
	defer s.mu.Unlock()

	metrics := map[string]interface{}{
		"running":             s.IsRunning(),
		"connections":         s.connections,
		"has_message_handler": s.messageHandler != nil,
	}

	if !s.lastMessage.IsZero() {
		metrics["last_message"] = s.lastMessage.Format(time.RFC3339)
		metrics["seconds_since_last_message"] = time.Since(s.lastMessage).Seconds()
	}

	return metrics
}

// Configure implements Service interface with context support
func (s *WebSocketService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")
	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	// Add configuration logic here as needed

	return nil
}

// SimulateConnection simulates adding a new websocket connection
// This is for demonstration only - in a real implementation this would be called
// when a new client connects
func (s *WebSocketService) SimulateConnection(ctx context.Context) {
	ctx = appctx.WithOperationName(ctx, "simulate_connection")
	logger.DebugfWithContext(ctx, "Adding simulated connection to: %s", s.GetName())

	s.mu.Lock()
	defer s.mu.Unlock()

	s.connections++
}

// SimulateDisconnection simulates removing a websocket connection
// This is for demonstration only - in a real implementation this would be called
// when a client disconnects
func (s *WebSocketService) SimulateDisconnection(ctx context.Context) {
	ctx = appctx.WithOperationName(ctx, "simulate_disconnection")
	logger.DebugfWithContext(ctx, "Removing simulated connection from: %s", s.GetName())

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connections > 0 {
		s.connections--
	}
}

// websocketConnectionChecker checks websocket connection health
type websocketConnectionChecker struct {
	service *WebSocketService
}

// Check implements the health.Checker interface
func (c *websocketConnectionChecker) Check(ctx context.Context) health.CheckResult {
	result := health.NewCheckResult("websocket-connections", health.CategoryConnectivity, health.LevelWarning)

	c.service.mu.Lock()
	connections := c.service.connections
	lastMessage := c.service.lastMessage
	isRunning := c.service.IsRunning()
	c.service.mu.Unlock()

	// Add details
	result.AddDetail("connections", fmt.Sprintf("%d", connections))
	result.AddDetail("is_running", fmt.Sprintf("%v", isRunning))

	if !isRunning {
		result.SetStatus(health.StatusDown, "WebSocket service is not running")
		result.Complete()
		return result
	}

	// Check connection count (in a real implementation, this might have thresholds)
	if connections == 0 {
		result.SetStatus(health.StatusDegraded, "No active WebSocket connections")
	} else {
		result.SetStatus(health.StatusUp, fmt.Sprintf("WebSocket service has %d connections", connections))
	}

	// Add last message time if available
	if !lastMessage.IsZero() {
		messageAge := time.Since(lastMessage)
		result.AddDetail("last_message", lastMessage.Format(time.RFC3339))
		result.AddDetail("message_age_minutes", fmt.Sprintf("%.2f", messageAge.Minutes()))

		// If no messages in a long time, service might be degraded
		if messageAge > 30*time.Minute {
			result.SetStatus(health.StatusDegraded, fmt.Sprintf("No messages in %.2f minutes", messageAge.Minutes()))
		}
	}

	result.Complete()
	return result
}
