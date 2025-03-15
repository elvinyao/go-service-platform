package service

import (
	"context"
	"fmt"
	"project/internal/dataaccess"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"time"
	// 其他必要的导入
)

type ConfluenceService struct {
	*BaseService
	dataAccessor dataaccess.DataAccessor
	mu           sync.Mutex
	apiEndpoint  string
	lastFetch    time.Time
}

func NewConfluenceService(name, workflow string, da dataaccess.DataAccessor, version string) *ConfluenceService {
	s := &ConfluenceService{
		BaseService:  NewBaseService(name, workflow, "confluence", version),
		dataAccessor: da,
		apiEndpoint:  "https://confluence.example.com/api",
		lastFetch:    time.Time{},
	}

	// Add custom health checkers
	s.AddHealthChecker(&confluenceApiChecker{service: s})

	return s
}

func (s *ConfluenceService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRunning() {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// Initialize any resources needed

	// Mark as running
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

func (s *ConfluenceService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	// Cleanup any resources

	// Mark as not running
	s.LockRunning(false)

	return nil
}

func (s *ConfluenceService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")
	logger.InfofWithContext(ctx, "Restarting service: %s", s.GetName())

	if err := s.Stop(ctx); err != nil {
		return errors.Wrap(err, "Failed to stop service during restart", errors.TypeServiceUnavailable)
	}

	return s.Start(ctx)
}

// FetchData retrieves data from the Confluence API
func (s *ConfluenceService) FetchData(ctx context.Context) (string, error) {
	ctx = appctx.WithOperationName(ctx, "fetch_data")
	logger.DebugfWithContext(ctx, "Fetching data from Confluence API")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return "", errors.New(errors.TypeServiceUnavailable, "Service is not running", nil)
	}

	// Example of using the data accessor
	key := "confluence_data_" + s.GetName()
	cachedData, err := s.dataAccessor.GetData(key)
	if err == nil && cachedData != nil {
		// Return cached data if available
		if strData, ok := cachedData.(string); ok {
			return strData, nil
		}
	}

	// Simulate fetching data
	fetchedData := "Data from " + s.GetName()

	// Update last fetch time
	s.lastFetch = time.Now()

	// Cache the fetched data
	_ = s.dataAccessor.SetData(key, fetchedData)

	return fetchedData, nil
}

// GetMetrics implements Service interface with context support
func (s *ConfluenceService) GetMetrics(ctx context.Context) map[string]interface{} {
	ctx = appctx.WithOperationName(ctx, "get_metrics")
	logger.DebugfWithContext(ctx, "Getting metrics for service: %s", s.GetName())

	s.mu.Lock()
	defer s.mu.Unlock()

	metrics := map[string]interface{}{
		"running":      s.IsRunning(),
		"workflow":     s.GetWorkflow(),
		"api_endpoint": s.apiEndpoint,
	}

	if !s.lastFetch.IsZero() {
		metrics["last_fetch"] = s.lastFetch.Format(time.RFC3339)
		metrics["last_fetch_age_seconds"] = time.Since(s.lastFetch).Seconds()
	}

	return metrics
}

// Configure implements Service interface with context support
func (s *ConfluenceService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")
	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	s.mu.Lock()
	defer s.mu.Unlock()

	// Handle configuration based on type
	if configMap, ok := config.(map[string]interface{}); ok {
		if endpoint, ok := configMap["api_endpoint"].(string); ok && endpoint != "" {
			s.apiEndpoint = endpoint
			logger.DebugfWithContext(ctx, "Updated API endpoint to: %s", s.apiEndpoint)
		}
		return nil
	}

	return fmt.Errorf("unsupported configuration type")
}

// confluenceApiChecker checks Confluence API connectivity
type confluenceApiChecker struct {
	service *ConfluenceService
}

// Check implements the health.Checker interface
func (c *confluenceApiChecker) Check(ctx context.Context) health.CheckResult {
	result := health.NewCheckResult("confluence-api", health.CategoryConnectivity, health.LevelCritical)

	c.service.mu.Lock()
	apiEndpoint := c.service.apiEndpoint
	isRunning := c.service.IsRunning()
	lastFetch := c.service.lastFetch
	c.service.mu.Unlock()

	// Add details
	result.AddDetail("api_endpoint", apiEndpoint)
	result.AddDetail("is_running", fmt.Sprintf("%v", isRunning))

	if !isRunning {
		result.SetStatus(health.StatusDown, "Confluence service is not running")
		result.Complete()
		return result
	}

	// Check last fetch time if available
	if !lastFetch.IsZero() {
		fetchAge := time.Since(lastFetch)
		result.AddDetail("last_fetch", lastFetch.Format(time.RFC3339))
		result.AddDetail("fetch_age_minutes", fmt.Sprintf("%.2f", fetchAge.Minutes()))

		// If last fetch was recent enough, consider the API accessible
		if fetchAge < 10*time.Minute {
			result.SetStatus(health.StatusUp, "Recently connected to Confluence API")
			result.Complete()
			return result
		}
	}

	// Try to connect to the API
	// In a real implementation, this would make a test API call
	// For demo purposes, we'll simulate a successful connection

	// Simulate an API call
	time.Sleep(50 * time.Millisecond)

	// For demo purposes, assume connection is successful
	result.SetStatus(health.StatusUp, "Successfully connected to Confluence API")

	result.Complete()
	return result
}
