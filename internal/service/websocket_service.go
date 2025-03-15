package service

import (
	"context"
	"project/internal/model"
	"project/pkg/logger"
	// 其他必要的导入
)

type WebSocketService struct {
	name     string
	workflow string
	running  bool // 新增字段
	// 其他需要的字段，例如连接池
	messageHandler func(msg model.Message)
}

func NewWebSocketService(name, workflow string) *WebSocketService {
	return &WebSocketService{
		name:     name,
		workflow: workflow,
	}
}

func (s *WebSocketService) Start(ctx context.Context) error {
	logger.Infof("Starting service: %s", s.name)
	s.running = true
	go func() {
		<-ctx.Done()
		err := s.Stop()
		if err != nil {
			return
		}
	}()
	// 实现启动逻辑，例如定时从Confluence获取数据
	return nil
}

func (s *WebSocketService) Stop() error {
	s.running = false
	logger.Infof("Stopping service: %s", s.name)
	// 实现停止逻辑
	return nil
}

func (s *WebSocketService) Restart(ctx context.Context) error {
	logger.Infof("Restarting service: %s", s.name)
	err := s.Stop()
	if err != nil {
		return err
	}
	return s.Start(ctx)
}

func (s *WebSocketService) GetName() string {
	return s.name
}

func (s *WebSocketService) GetWorkflow() string {
	return s.workflow
}

func (s *WebSocketService) OnMessage(handler func(msg model.Message)) {
	s.messageHandler = handler
}
func (s *WebSocketService) IsRunning() bool {
	return s.running
}
func (s *WebSocketService) GetType() string {
	return s.name
}

// GetMetrics implements Service interface
func (s *WebSocketService) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"running": s.running,
	}
}

// Configure implements Service interface
func (s *WebSocketService) Configure(config interface{}) error {
	// Implementation for dynamic configuration
	return nil
}

// 在接收到消息时调用s.messageHandler(msg)
