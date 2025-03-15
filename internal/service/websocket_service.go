package service

import (
	"context"
	"project/internal/model"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
	"sync"
)

type WebSocketService struct {
	name     string
	workflow string
	running  bool
	mu       sync.Mutex
	// Other fields like connection pool
	messageHandler func(msg model.Message)
}

func NewWebSocketService(name, workflow string) *WebSocketService {
	return &WebSocketService{
		name:     name,
		workflow: workflow,
	}
}

func (s *WebSocketService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return errors.New(errors.TypeInvalidInput, "Service is already running", nil).
			WithField("service", s.name)
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.name)
	s.running = true

	// Setup cleanup on context cancellation
	go func() {
		<-ctx.Done()
		stopCtx := appctx.NewContext(context.Background())
		if err := s.Stop(stopCtx); err != nil {
			logger.WithContextError(stopCtx, err).Error("Error stopping service on context cancellation")
		}
	}()

	// Implement actual startup logic
	return nil
}

func (s *WebSocketService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return errors.New(errors.TypeInvalidInput, "Service is not running", nil).
			WithField("service", s.name)
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.name)
	s.running = false

	// Implement actual shutdown logic
	return nil
}

func (s *WebSocketService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")
	logger.InfofWithContext(ctx, "Restarting service: %s", s.name)

	if err := s.Stop(ctx); err != nil {
		return errors.Wrap(err, "Failed to stop service during restart", errors.TypeServiceUnavailable)
	}

	return s.Start(ctx)
}

func (s *WebSocketService) GetName() string {
	return s.name
}

func (s *WebSocketService) GetWorkflow() string {
	return s.workflow
}

func (s *WebSocketService) GetType() string {
	return "websocket"
}

func (s *WebSocketService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
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
	logger.DebugfWithContext(ctx, "Getting metrics for service: %s", s.name)

	s.mu.Lock()
	defer s.mu.Unlock()

	return map[string]interface{}{
		"running":             s.running,
		"has_message_handler": s.messageHandler != nil,
	}
}

// Configure implements Service interface with context support
func (s *WebSocketService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")
	logger.InfofWithContext(ctx, "Configuring service: %s", s.name)

	// Add configuration logic here

	return nil
}
