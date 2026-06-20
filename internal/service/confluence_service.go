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
)

type ConfluenceService struct {
	*BaseService
	dataAccessor dataaccess.DataAccessor
	mu           sync.Mutex
	lifecycleMu  sync.Mutex
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

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	return s.startInternal(ctx)
}

func (s *ConfluenceService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	return s.stopInternal(ctx)
}

func (s *ConfluenceService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running, starting it", s.GetName())
		return s.startInternal(ctx)
	}

	logger.InfofWithContext(ctx, "Restarting service: %s", s.GetName())

	if err := s.stopInternal(ctx); err != nil {
		return err
	}

	return s.startInternal(ctx)
}

func (s *ConfluenceService) FetchData(ctx context.Context) (map[string]interface{}, error) {
	ctx = appctx.WithOperationName(ctx, "fetch_data")

	if !s.IsRunning(ctx) {
		return nil, fmt.Errorf("service not running")
	}

	logger.InfofWithContext(ctx, "Fetching data from Confluence API")

	// Check cache
	cacheKey := "confluence_data"
	cachedData, err := s.dataAccessor.GetData(cacheKey)
	if err == nil && cachedData != nil {
		logger.DebugfWithContext(ctx, "Using cached Confluence data")
		if data, ok := cachedData.(map[string]interface{}); ok {
			return data, nil
		}
	}

	// Fetch data from API
	data, err := s.fetchData(ctx)
	if err != nil {
		return nil, err
	}

	// Cache data
	err = s.dataAccessor.SetData(cacheKey, data)
	if err != nil {
		logger.WithContextError(ctx, err).Warn("Failed to cache Confluence data")
	}

	return data, nil
}

func (s *ConfluenceService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")

	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	// Parse configuration
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return errors.New(errors.TypeInvalidInput, "Invalid configuration format", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update API endpoint
	if endpoint, ok := cfg["api_endpoint"].(string); ok && endpoint != "" {
		s.apiEndpoint = endpoint
		logger.DebugfWithContext(ctx, "Updated API endpoint to: %s", endpoint)
	}

	logger.InfofWithContext(ctx, "Service %s configured successfully", s.GetName())
	return nil
}

func (s *ConfluenceService) GetMetrics(ctx context.Context) map[string]interface{} {
	baseMetrics := s.BaseService.GetMetrics(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	metrics := make(map[string]interface{})
	for k, v := range baseMetrics {
		metrics[k] = v
	}

	metrics["api_endpoint"] = s.apiEndpoint

	if !s.lastFetch.IsZero() {
		metrics["last_fetch"] = s.lastFetch.Format(time.RFC3339)
		metrics["last_fetch_age_seconds"] = time.Since(s.lastFetch).Seconds()
	}

	return metrics
}

// Internal methods

func (s *ConfluenceService) fetchData(ctx context.Context) (map[string]interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Demo data fetch with latency to exercise cache and shutdown behavior.
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "Confluence fetch cancelled", errors.TypeTimeout)
	}

	// Update last fetch time
	s.mu.Lock()
	s.lastFetch = time.Now()
	s.mu.Unlock()

	return map[string]interface{}{
		"pages":  []string{"Page1", "Page2", "Page3"},
		"users":  []string{"User1", "User2"},
		"spaces": []string{"Space1", "Space2"},
	}, nil
}

func (s *ConfluenceService) startInternal(ctx context.Context) error {
	if s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// Initialize service
	data, err := s.fetchData(ctx)
	if err != nil {
		logger.WithContextError(ctx, err).Errorf("Failed to fetch initial data for service %s", s.GetName())
		return errors.Wrap(err, "Failed to fetch initial data", errors.TypeServiceUnavailable)
	}

	// Cache initial data
	err = s.dataAccessor.SetData("confluence_data", data)
	if err != nil {
		logger.WithContextError(ctx, err).Warnf("Failed to cache initial data for service %s", s.GetName())
	}

	s.setRunning(true)
	logger.InfofWithContext(ctx, "Service %s started successfully", s.GetName())
	return nil
}

func (s *ConfluenceService) stopInternal(ctx context.Context) error {
	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())
	s.setRunning(false)
	logger.InfofWithContext(ctx, "Service %s stopped successfully", s.GetName())
	return nil
}

// confluenceApiChecker checks Confluence API connection
type confluenceApiChecker struct {
	service *ConfluenceService
}

// Check implements the Checker interface
func (c *confluenceApiChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("confluence-api", health.CategoryConnectivity)
	result.Level = health.LevelCritical

	// Check if service is running
	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "Confluence service is not running")
		result.Complete()
		return result
	}

	// Try to fetch data
	_, err := c.service.fetchData(ctx)
	if err != nil {
		result.SetStatus(health.StatusDown, fmt.Sprintf("Failed to connect to Confluence API: %v", err))
		result.Complete()
		return result
	}

	// Check last fetch time
	c.service.mu.Lock()
	lastFetch := c.service.lastFetch
	c.service.mu.Unlock()

	if lastFetch.IsZero() {
		result.SetStatus(health.StatusDegraded, "No data has been fetched from Confluence API")
	} else {
		fetchAge := time.Since(lastFetch)
		result.AddDetail("last_fetch_age_minutes", fetchAge.Minutes())

		if fetchAge > 60*time.Minute {
			result.SetStatus(health.StatusDegraded, fmt.Sprintf("Data is stale: last fetch was %v ago", fetchAge))
		} else {
			result.SetStatus(health.StatusUp, fmt.Sprintf("Successfully connected to Confluence API, last fetch %v ago", fetchAge))
		}
	}

	result.Complete()
	return result
}
