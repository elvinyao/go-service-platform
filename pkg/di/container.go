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

// Container Provides dependency injection container functionality
type Container struct {
	// Mutex for thread-safe operations, protects the container state
	// RWMutex is a reader/writer mutex that allows multiple readers or a single writer
	// It is different from Mutex, which allows only one writer or multiple readers
	mu sync.RWMutex

	// Configuration
	version string

	// Singleton instances
	dataAccessor    dataaccess.DataAccessor
	serviceManager  *manager.ServiceManager
	workflowManager *manager.WorkflowManager
	healthManager   *health.HealthManager

	// Service instance mapping
	services map[string]service.Service
}

// NewContainer Creates a new dependency injection container
func NewContainer(version string) *Container {
	return &Container{
		version:  version,
		services: make(map[string]service.Service),
	}
}

// GetVersion Returns the application version
func (c *Container) GetVersion() string {
	return c.version
}

// GetDataAccessor Returns the data accessor instance, creating it if it doesn't exist
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

	// Double-check to avoid creating the instance during the write lock
	if c.dataAccessor != nil {
		return c.dataAccessor
	}

	c.dataAccessor = dataaccess.NewCacheDataAccessor()
	return c.dataAccessor
}

// GetServiceManager Returns the service manager instance, creating it if it doesn't exist
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

	// Double-check
	if c.serviceManager != nil {
		return c.serviceManager
	}

	c.serviceManager = manager.NewServiceManager(c.version)
	return c.serviceManager
}

// GetHealthManager Returns the health check manager instance, creating it if it doesn't exist
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

	// Double-check
	if c.healthManager != nil {
		return c.healthManager
	}

	c.healthManager = health.NewHealthManager(30*time.Second, c.version)
	return c.healthManager
}

// GetWorkflowManager Returns the workflow manager instance, creating it if it doesn't exist
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

	// Double-check
	if c.workflowManager != nil {
		return c.workflowManager, nil
	}

	// Workflow manager needs service manager
	serviceManager := c.GetServiceManager(ctx)

	// Get workflow factories
	factories := workflow.RegisterWorkflowFactories()

	// Create workflow manager
	workflowManager, err := manager.NewWorkflowManagerWithDI(ctx, serviceManager, factories...)
	if err != nil {
		return nil, err
	}

	c.workflowManager = workflowManager
	return workflowManager, nil
}

// RegisterServices Registers all services to the manager
func (c *Container) RegisterServices(ctx context.Context) error {
	// Get dependencies
	sm := c.GetServiceManager(ctx)
	da := c.GetDataAccessor(ctx)

	// Create services
	confluenceService := service.NewConfluenceService("ConfluenceServiceA", "A", da, c.version)
	websocketService := service.NewWebSocketService("WebSocketServiceA", "A", c.version)
	badgeDBService := service.NewBadgeDBService("BadgeDBService", "Global", "/tmp/badges.db", c.version)

	// Register services
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

// GetService Returns the service instance with the specified name
func (c *Container) GetService(name string) (service.Service, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	svc, ok := c.services[name]
	return svc, ok
}
