package di

import (
	"context"
	internalconfig "github.com/elvinyao/go-service-platform/internal/config"
	"github.com/elvinyao/go-service-platform/internal/dataaccess"
	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
	runtimeconfig "github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	"os"
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
	version       string
	runtimeConfig runtimeconfig.RuntimeConfig

	// Singleton instances
	dataAccessor   dataaccess.DataAccessor
	serviceManager *manager.ServiceManager
	healthManager  *health.HealthManager

	// Service instance mapping
	services map[string]service.Service
}

// NewContainer Creates a new dependency injection container
func NewContainer(version string, configs ...runtimeconfig.RuntimeConfig) *Container {
	cfg := runtimeconfig.DefaultRuntimeConfig()
	if len(configs) > 0 {
		cfg = configs[0]
	}

	return &Container{
		version:       version,
		runtimeConfig: cfg,
		services:      make(map[string]service.Service),
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

	c.dataAccessor = dataaccess.NewCacheDataAccessor(ctx)
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

	healthManager := c.GetHealthManager(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check
	if c.serviceManager != nil {
		return c.serviceManager
	}

	c.serviceManager = manager.NewServiceManager(c.version, healthManager)
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

// RegisterServices Registers all services to the manager
func (c *Container) RegisterServices(ctx context.Context) error {
	// Get dependencies
	sm := c.GetServiceManager(ctx)
	cfg := c.runtimeConfig
	if err := cfg.Validate(); err != nil {
		return err
	}

	services := make([]service.Service, 0, 4)

	if cfg.Inputs.WebSocket.Enabled {
		wsConfig := internalconfig.WebSocketConfig{
			ServerURL:         cfg.Inputs.WebSocket.ServerURL,
			Path:              cfg.Inputs.WebSocket.Path,
			ReconnectInterval: cfg.Inputs.WebSocket.ReconnectInterval,
		}
		websocketService := service.NewWebSocketService(service.WebSocketInputServiceName, ruleengine.DefaultWorkflowName, wsConfig, c.version)
		services = append(services, websocketService)
	}

	if cfg.Adapters.BadgeDB.Enabled {
		badgeDBService := service.NewBadgeDBService(service.BadgeDBServiceName, ruleengine.DefaultWorkflowName, cfg.Adapters.BadgeDB.Path, c.version)
		services = append(services, badgeDBService)
	}

	if cfg.Adapters.Mattermost.Enabled {
		mmConfig := internalconfig.MattermostConfig{
			ServerURL:    cfg.Adapters.Mattermost.ServerURL,
			APIToken:     cfg.Adapters.Mattermost.APIToken,
			Channel:      cfg.Adapters.Mattermost.Channel,
			Username:     getEnvOrDefault("MATTERMOST_USERNAME", ""),
			Password:     getEnvOrDefault("MATTERMOST_PASSWORD", ""),
			WebsocketURL: cfg.Adapters.Mattermost.WebsocketURL,
		}
		mattermostService := service.NewMattermostService(service.MattermostServiceName, ruleengine.DefaultWorkflowName, mmConfig, c.version)
		services = append(services, mattermostService)
	}

	if cfg.Adapters.Confluence.Enabled {
		da := c.GetDataAccessor(ctx)
		settingsConfig := internalconfig.ConfluenceSettingsConfig{
			PageID:          cfg.Adapters.Confluence.SettingsPageID,
			RefreshInterval: cfg.Adapters.Confluence.RefreshInterval,
			APIEndpoint:     cfg.Adapters.Confluence.APIEndpoint,
			SpaceKey:        getEnvOrDefault("CONFLUENCE_SPACE_KEY", "TEST"),
		}
		confluenceSettingsService := service.NewConfluenceSettingsService(service.ConfluenceSettingsServiceName, ruleengine.DefaultWorkflowName, da, settingsConfig, c.version)
		services = append(services, confluenceSettingsService)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, svc := range services {
		if err := sm.RegisterService(svc); err != nil {
			return err
		}
		c.services[svc.GetName()] = svc
	}

	return nil
}

// getEnvOrDefault returns the environment variable value or a default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetService Returns the service instance with the specified name
func (c *Container) GetService(name string) (service.Service, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	svc, ok := c.services[name]
	return svc, ok
}
