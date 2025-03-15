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

	if s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// 初始化服务
	data, err := s.fetchData(ctx)
	if err != nil {
		logger.WithContextError(ctx, err).Errorf("Failed to fetch initial data for service %s", s.GetName())
		return errors.Wrap(err, "Failed to fetch initial data", errors.TypeServiceUnavailable)
	}

	// 缓存初始数据
	err = s.dataAccessor.SetData("confluence_data", data)
	if err != nil {
		logger.WithContextError(ctx, err).Warnf("Failed to cache initial data for service %s", s.GetName())
	}

	s.setRunning(true)
	logger.InfofWithContext(ctx, "Service %s started successfully", s.GetName())
	return nil
}

func (s *ConfluenceService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())
	s.setRunning(false)
	logger.InfofWithContext(ctx, "Service %s stopped successfully", s.GetName())
	return nil
}

func (s *ConfluenceService) Restart(ctx context.Context) error {
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

func (s *ConfluenceService) FetchData(ctx context.Context) (map[string]interface{}, error) {
	ctx = appctx.WithOperationName(ctx, "fetch_data")

	if !s.IsRunning(ctx) {
		return nil, fmt.Errorf("service not running")
	}

	logger.InfofWithContext(ctx, "Fetching data from Confluence API")

	// 检查缓存
	cacheKey := "confluence_data"
	cachedData, err := s.dataAccessor.GetData(cacheKey)
	if err == nil && cachedData != nil {
		logger.DebugfWithContext(ctx, "Using cached Confluence data")
		if data, ok := cachedData.(map[string]interface{}); ok {
			return data, nil
		}
	}

	// 从API获取数据
	data, err := s.fetchData(ctx)
	if err != nil {
		return nil, err
	}

	// 缓存数据
	err = s.dataAccessor.SetData(cacheKey, data)
	if err != nil {
		logger.WithContextError(ctx, err).Warn("Failed to cache Confluence data")
	}

	return data, nil
}

func (s *ConfluenceService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")

	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	// 解析配置
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return errors.New(errors.TypeInvalidInput, "Invalid configuration format", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 更新API端点
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

// 内部方法

func (s *ConfluenceService) fetchData(ctx context.Context) (map[string]interface{}, error) {
	// 模拟从Confluence API获取数据
	// 在实际实现中，这里会调用真实的API

	// 模拟API延迟
	time.Sleep(100 * time.Millisecond)

	// 更新最后获取时间
	s.mu.Lock()
	s.lastFetch = time.Now()
	s.mu.Unlock()

	// 返回模拟数据
	return map[string]interface{}{
		"pages":  []string{"Page1", "Page2", "Page3"},
		"users":  []string{"User1", "User2"},
		"spaces": []string{"Space1", "Space2"},
	}, nil
}

// confluenceApiChecker 检查Confluence API连接
type confluenceApiChecker struct {
	service *ConfluenceService
}

// Check 实现Checker接口
func (c *confluenceApiChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("confluence-api", health.CategoryConnectivity)
	result.Level = health.LevelCritical

	// 检查服务是否运行
	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "Confluence service is not running")
		result.Complete()
		return result
	}

	// 尝试获取数据
	_, err := c.service.fetchData(ctx)
	if err != nil {
		result.SetStatus(health.StatusDown, fmt.Sprintf("Failed to connect to Confluence API: %v", err))
		result.Complete()
		return result
	}

	// 检查上次获取时间
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
