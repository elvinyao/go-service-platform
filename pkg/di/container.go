package di

import (
	"context"
	"project/internal/dataaccess"
	"project/internal/manager"
	"project/internal/service"
	"project/internal/workflow"
	"project/pkg/health"
	"sync"
	"time"
)

// Container 提供依赖注入容器功能
type Container struct {
	mu sync.RWMutex

	// 配置
	version string

	// 单例实例
	dataAccessor    dataaccess.DataAccessor
	serviceManager  *manager.ServiceManager
	workflowManager *manager.WorkflowManager
	healthManager   *health.HealthManager

	// 服务实例映射
	services map[string]service.Service
}

// NewContainer 创建一个新的依赖注入容器
func NewContainer(version string) *Container {
	return &Container{
		version:  version,
		services: make(map[string]service.Service),
	}
}

// GetVersion 返回应用版本
func (c *Container) GetVersion() string {
	return c.version
}

// GetDataAccessor 返回数据访问器实例，如果不存在则创建
func (c *Container) GetDataAccessor(ctx context.Context) dataaccess.DataAccessor {
	c.mu.RLock()
	if c.dataAccessor != nil {
		da := c.dataAccessor
		c.mu.RUnlock()
		return da
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查，避免在获取写锁期间其他goroutine已创建
	if c.dataAccessor != nil {
		return c.dataAccessor
	}

	c.dataAccessor = dataaccess.NewCacheDataAccessor()
	return c.dataAccessor
}

// GetServiceManager 返回服务管理器实例，如果不存在则创建
func (c *Container) GetServiceManager(ctx context.Context) *manager.ServiceManager {
	c.mu.RLock()
	if c.serviceManager != nil {
		sm := c.serviceManager
		c.mu.RUnlock()
		return sm
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查
	if c.serviceManager != nil {
		return c.serviceManager
	}

	c.serviceManager = manager.NewServiceManager(c.version)
	return c.serviceManager
}

// GetHealthManager 返回健康检查管理器实例，如果不存在则创建
func (c *Container) GetHealthManager(ctx context.Context) *health.HealthManager {
	c.mu.RLock()
	if c.healthManager != nil {
		hm := c.healthManager
		c.mu.RUnlock()
		return hm
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查
	if c.healthManager != nil {
		return c.healthManager
	}

	c.healthManager = health.NewHealthManager(30*time.Second, c.version)
	return c.healthManager
}

// GetWorkflowManager 返回工作流管理器实例，如果不存在则创建
func (c *Container) GetWorkflowManager(ctx context.Context) (*manager.WorkflowManager, error) {
	c.mu.RLock()
	if c.workflowManager != nil {
		wm := c.workflowManager
		c.mu.RUnlock()
		return wm, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查
	if c.workflowManager != nil {
		return c.workflowManager, nil
	}

	// 工作流管理器需要服务管理器
	serviceManager := c.GetServiceManager(ctx)

	// 获取工作流工厂
	factories := workflow.RegisterWorkflowFactories()

	// 创建工作流管理器
	workflowManager, err := manager.NewWorkflowManagerWithDI(ctx, serviceManager, factories...)
	if err != nil {
		return nil, err
	}

	c.workflowManager = workflowManager
	return workflowManager, nil
}

// RegisterServices 注册所有服务到管理器
func (c *Container) RegisterServices(ctx context.Context) error {
	// 获取依赖
	sm := c.GetServiceManager(ctx)
	da := c.GetDataAccessor(ctx)

	// 创建服务
	confluenceService := service.NewConfluenceService("ConfluenceServiceA", "A", da, c.version)
	websocketService := service.NewWebSocketService("WebSocketServiceA", "A", c.version)
	badgeDBService := service.NewBadgeDBService("BadgeDBService", "Global", "/tmp/badges.db", c.version)

	// 注册服务
	services := []service.Service{
		confluenceService,
		websocketService,
		badgeDBService,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, svc := range services {
		sm.RegisterService(svc)
		c.services[svc.GetName()] = svc
	}

	return nil
}

// GetService 返回指定名称的服务实例
func (c *Container) GetService(name string) (service.Service, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	svc, ok := c.services[name]
	return svc, ok
}
