package service

import (
	"context"
	"project/internal/dataaccess"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
	"sync"
	// 其他必要的导入
)

type ConfluenceService struct {
	name         string
	workflow     string
	dataAccessor dataaccess.DataAccessor
	running      bool
	mu           sync.Mutex
	// 其他字段
}

func NewConfluenceService(name, workflow string, da dataaccess.DataAccessor) *ConfluenceService {
	return &ConfluenceService{
		name:         name,
		workflow:     workflow,
		dataAccessor: da,
	}
}

func (s *ConfluenceService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return errors.New(errors.TypeInvalidInput, "Service is already running", nil).
			WithField("service", s.name)
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.name)
	s.running = true

	go func() {
		<-ctx.Done()
		stopCtx := appctx.NewContext(context.Background())
		if err := s.Stop(stopCtx); err != nil {
			logger.WithContextError(stopCtx, err).Errorf("Error stopping service: %s", s.name)
		}
	}()

	return nil
}

func (s *ConfluenceService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return errors.New(errors.TypeInvalidInput, "Service is not running", nil).
			WithField("service", s.name)
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.name)
	s.running = false

	return nil
}

func (s *ConfluenceService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")
	logger.InfofWithContext(ctx, "Restarting service: %s", s.name)

	if err := s.Stop(ctx); err != nil {
		return errors.Wrap(err, "Failed to stop service during restart", errors.TypeServiceUnavailable).
			WithField("service", s.name)
	}

	return s.Start(ctx)
}

func (s *ConfluenceService) GetName() string {
	return s.name
}

func (s *ConfluenceService) GetWorkflow() string {
	return s.workflow
}

func (s *ConfluenceService) GetType() string {
	return "confluence"
}

func (s *ConfluenceService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// FetchData retrieves data from the Confluence API
func (s *ConfluenceService) FetchData() (string, error) {
	// In a real implementation, this would make API calls to Confluence
	// For demonstration purposes, we'll just return the service name
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return "", errors.New(errors.TypeServiceUnavailable, "Service is not running", nil).
			WithField("service", s.name)
	}

	// Example of using the data accessor
	key := "confluence_data_" + s.name
	cachedData, err := s.dataAccessor.GetData(key)
	if err == nil && cachedData != nil {
		// Return cached data if available
		if strData, ok := cachedData.(string); ok {
			return strData, nil
		}
	}

	// Simulate fetching data
	fetchedData := "Data from " + s.name

	// Cache the fetched data
	_ = s.dataAccessor.SetData(key, fetchedData)

	return fetchedData, nil
}

// GetMetrics implements Service interface with context support
func (s *ConfluenceService) GetMetrics(ctx context.Context) map[string]interface{} {
	ctx = appctx.WithOperationName(ctx, "get_metrics")
	logger.DebugfWithContext(ctx, "Getting metrics for service: %s", s.name)

	s.mu.Lock()
	defer s.mu.Unlock()

	return map[string]interface{}{
		"running":  s.running,
		"workflow": s.workflow,
	}
}

// Configure implements Service interface with context support
func (s *ConfluenceService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")
	logger.InfofWithContext(ctx, "Configuring service: %s", s.name)

	// Add configuration logic here

	return nil
}
