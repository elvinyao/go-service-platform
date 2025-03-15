package service

import (
	"context"
	"fmt"
	appctx "project/pkg/context"
	appErrors "project/pkg/errors"
	"project/pkg/logger"
	"sync"
	"time"
	// 其他必要的导入
)

type BadgeDBService struct {
	name     string
	workflow string
	running  bool
	mu       sync.Mutex
	// 数据库连接等字段
	processingMethods map[string]string // 缓存处理方法
}

func NewBadgeDBService(name, workflow string) *BadgeDBService {
	return &BadgeDBService{
		name:              name,
		workflow:          workflow,
		processingMethods: make(map[string]string),
	}
}

func (s *BadgeDBService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return appErrors.New(appErrors.TypeInvalidInput,
			fmt.Sprintf("Service %s is already running", s.name), nil)
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.name)

	// Initialize database connection or other resources
	if err := s.initializeResources(); err != nil {
		return appErrors.Wrap(err, "Failed to initialize resources", appErrors.TypeServiceUnavailable).
			WithField("service", s.name)
	}

	s.running = true

	// Setup cleanup when context is canceled
	go func() {
		<-ctx.Done()
		if err := s.Stop(ctx); err != nil {
			logger.WithError(err).Errorf("Error stopping service %s during context cancellation", s.name)
		}
	}()

	// Start any background processes
	go s.backgroundProcessing(ctx)

	return nil
}

// initializeResources sets up any resources needed by the service
func (s *BadgeDBService) initializeResources() error {
	// Simulate database connection or other initialization
	time.Sleep(100 * time.Millisecond)

	// Pre-load some processing methods
	s.processingMethods["default"] = "DefaultProcessingMethod"
	s.processingMethods["urgent"] = "UrgentProcessingMethod"

	return nil
}

// backgroundProcessing handles any recurring tasks
func (s *BadgeDBService) backgroundProcessing(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refreshProcessingMethods(); err != nil {
				logger.WithError(err).Errorf("Failed to refresh processing methods")
			}
		}
	}
}

// refreshProcessingMethods updates the processing methods from the database
func (s *BadgeDBService) refreshProcessingMethods() error {
	// In a real implementation, this would query a database
	// Simulate some processing time
	time.Sleep(50 * time.Millisecond)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update with some new values
	s.processingMethods["special"] = "SpecialProcessingMethod"

	return nil
}

// Stop implements Service interface with context support
func (s *BadgeDBService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return appErrors.New(appErrors.TypeInvalidInput,
			fmt.Sprintf("Service %s is not running", s.name), nil)
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.name)

	// Close any resources

	s.running = false
	return nil
}

func (s *BadgeDBService) Restart(ctx context.Context) error {
	logger.InfofWithContext(ctx, "Restarting service: %s", s.name)

	if err := s.Stop(ctx); err != nil {
		return appErrors.Wrap(err, "Failed to stop service during restart", appErrors.TypeServiceUnavailable).
			WithField("service", s.name)
	}

	return s.Start(ctx)
}

func (s *BadgeDBService) GetName() string {
	return s.name
}

func (s *BadgeDBService) GetWorkflow() string {
	return s.workflow
}

func (s *BadgeDBService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *BadgeDBService) GetType() string {
	return "badgedb"
}

// GetProcessingMethod retrieves the processing method for a given message type
func (s *BadgeDBService) GetProcessingMethod(messageType string) (string, error) {
	if messageType == "" {
		return "", appErrors.New(appErrors.TypeInvalidInput, "Message type cannot be empty", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	method, exists := s.processingMethods[messageType]
	if !exists {
		// Check for default fallback
		method, exists = s.processingMethods["default"]
		if !exists {
			return "", appErrors.New(appErrors.TypeNotFound,
				fmt.Sprintf("No processing method found for message type '%s'", messageType), nil).
				WithField("message_type", messageType)
		}

		// Log we're using the default
		logger.Warnf("Using default processing method for unknown message type: %s", messageType)
	}

	return method, nil
}

// GetMetrics implements Service interface with context support
func (s *BadgeDBService) GetMetrics(ctx context.Context) map[string]interface{} {
	ctx = appctx.WithOperationName(ctx, "get_metrics")
	logger.DebugfWithContext(ctx, "Getting metrics for %s", s.name)

	s.mu.Lock()
	defer s.mu.Unlock()

	methodCount := len(s.processingMethods)

	return map[string]interface{}{
		"running":            s.running,
		"processing_methods": methodCount,
		"has_default_method": s.hasDefaultMethod(),
	}
}

// hasDefaultMethod checks if the default method exists
func (s *BadgeDBService) hasDefaultMethod() bool {
	_, exists := s.processingMethods["default"]
	return exists
}

// Configure implements Service interface with context support
func (s *BadgeDBService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")
	logger.DebugfWithContext(ctx, "Configuring service %s", s.name)

	// Type assert the config to ensure it's the correct type
	badgeConfig, ok := config.(map[string]interface{})
	if !ok {
		return appErrors.New(appErrors.TypeInvalidInput,
			"Invalid configuration type for BadgeDB service", nil).
			WithField("config_type", fmt.Sprintf("%T", config))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Process configuration updates
	if methods, ok := badgeConfig["processing_methods"].(map[string]string); ok {
		for k, v := range methods {
			s.processingMethods[k] = v
		}
		logger.InfofWithContext(ctx, "Updated %d processing methods for BadgeDB service", len(methods))
	}

	return nil
}
