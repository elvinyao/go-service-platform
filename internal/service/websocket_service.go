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

	if s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// Initialize websocket server
	// In a real implementation, you would set up websocket listeners here

	// Mark as running
	s.setRunning(true)
	logger.InfofWithContext(ctx, "Service %s started successfully", s.GetName())
	return nil
}

func (s *WebSocketService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	// Close websocket connections
	// In a real implementation, you would close all connections here
	s.connections = 0

	// Mark as not running
	s.setRunning(false)
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
		// Call the handler in a goroutine to avoid blocking
		go func() {
			handlerCtx := appctx.WithOperationName(ctx, "message_handler")
			logger.DebugfWithContext(handlerCtx, "Calling message handler for message: %+v", message)
			handler(message)
		}()
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

	if !s.lastMessage.IsZero() {
		metrics["last_message"] = s.lastMessage.Format(time.RFC3339)
		metrics["last_message_age_seconds"] = time.Since(s.lastMessage).Seconds()
	}

	return metrics
}

func (s *WebSocketService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")

	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	// 解析配置
	_, ok := config.(map[string]interface{})
	if !ok {
		return errors.New(errors.TypeInvalidInput, "Invalid configuration format", nil)
	}

	// 在实际实现中，这里会处理WebSocket服务的配置
	// 例如：最大连接数、心跳间隔等

	logger.InfofWithContext(ctx, "Service %s configured successfully", s.GetName())
	return nil
}

// websocketConnectionChecker 检查WebSocket连接状态
type websocketConnectionChecker struct {
	service *WebSocketService
}

// Check 实现Checker接口
func (c *websocketConnectionChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("websocket-connections", health.CategoryConnectivity)
	result.Level = health.LevelWarning

	// 检查服务是否运行
	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "WebSocket service is not running")
		result.Complete()
		return result
	}

	// 获取连接数
	c.service.mu.Lock()
	connections := c.service.connections
	lastMessage := c.service.lastMessage
	c.service.mu.Unlock()

	result.AddDetail("connections", connections)

	// 检查连接数
	if connections == 0 {
		result.SetStatus(health.StatusDegraded, "No active WebSocket connections")
	} else {
		result.SetStatus(health.StatusUp, fmt.Sprintf("Active WebSocket connections: %d", connections))
	}

	// 检查最后消息时间
	if !lastMessage.IsZero() {
		messageAge := time.Since(lastMessage)
		result.AddDetail("last_message_age_minutes", messageAge.Minutes())

		if messageAge > 30*time.Minute {
			result.SetStatus(health.StatusDegraded, fmt.Sprintf("No messages received in %v", messageAge))
		}
	}

	result.Complete()
	return result
}
