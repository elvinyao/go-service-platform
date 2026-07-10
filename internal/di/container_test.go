package di

import (
	"context"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/internal/service"
	runtimeconfig "github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock interfaces and types for testing
type MockDataAccessor struct {
	mock.Mock
}

type MockServiceManager struct {
	mock.Mock
	version string
}

type MockHealthManager struct {
	mock.Mock
	version string
}

type MockWorkflowManager struct {
	mock.Mock
}

type MockService struct {
	mock.Mock
	name    string
	region  string
	version string
}

func (m *MockService) GetName() string {
	return m.name
}

func (m *MockService) GetRegion() string {
	return m.region
}

func (m *MockService) GetVersion() string {
	return m.version
}

func (m *MockService) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockService) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockService) Status() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockService) Configure(ctx context.Context, config interface{}) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockService) GetMetrics(ctx context.Context) map[string]interface{} {
	args := m.Called(ctx)
	return args.Get(0).(map[string]interface{})
}

func (m *MockService) GetType() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockService) GetWorkflow() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockService) IsRunning(ctx context.Context) bool {
	args := m.Called(ctx)
	return args.Bool(0)
}

func (m *MockService) RegisterHealthChecks() []health.Checker {
	args := m.Called()
	if res := args.Get(0); res != nil {
		return res.([]health.Checker)
	}
	return nil
}

func (m *MockService) ReportHealth(ctx context.Context, report *health.Report) {
	m.Called(ctx, report)
}

func (m *MockService) Restart(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockServiceManager) RegisterService(service interface{}) {
	m.Called(service)
}

func (m *MockServiceManager) GetVersion() string {
	return m.version
}

func TestNewContainer(t *testing.T) {
	// Arrange
	version := "1.0.0"

	// Act
	container := NewContainer(version)

	// Assert
	assert.NotNil(t, container)
	assert.Equal(t, version, container.GetVersion())
	assert.NotNil(t, container.services)
	assert.Empty(t, container.services)
}

func TestGetVersion(t *testing.T) {
	// Arrange
	expected := "2.0.0"
	container := NewContainer(expected)

	// Act
	actual := container.GetVersion()

	// Assert
	assert.Equal(t, expected, actual)
}

func TestGetDataAccessor(t *testing.T) {
	// Arrange
	container := NewContainer("1.0.0")
	ctx := context.Background()

	// Act
	da1 := container.GetDataAccessor(ctx)

	// Assert
	assert.NotNil(t, da1)

	// Act again to test singleton behavior
	da2 := container.GetDataAccessor(ctx)

	// Assert they're the same instance
	assert.Same(t, da1, da2)
}

func TestGetServiceManager(t *testing.T) {
	// Arrange
	container := NewContainer("1.0.0")
	ctx := context.Background()

	// Act
	sm1 := container.GetServiceManager(ctx)

	// Assert
	assert.NotNil(t, sm1)

	// Act again to test singleton behavior
	sm2 := container.GetServiceManager(ctx)

	// Assert they're the same instance
	assert.Same(t, sm1, sm2)
}

func TestGetHealthManager(t *testing.T) {
	// Arrange
	container := NewContainer("1.0.0")
	ctx := context.Background()

	// Act
	hm1 := container.GetHealthManager(ctx)

	// Assert
	assert.NotNil(t, hm1)

	// Act again to test singleton behavior
	hm2 := container.GetHealthManager(ctx)

	// Assert they're the same instance
	assert.Same(t, hm1, hm2)
}

func TestGetService(t *testing.T) {
	// Arrange
	container := NewContainer("1.0.0")
	mockService := &MockService{name: "TestService", region: "TestRegion", version: "1.0.0"}
	container.services["TestService"] = mockService

	// Act
	service, ok := container.GetService("TestService")

	// Assert
	assert.True(t, ok)
	assert.Equal(t, mockService, service)

	// Test getting non-existent service
	service, ok = container.GetService("NonExistentService")
	assert.False(t, ok)
	assert.Nil(t, service)
}

func TestRegisterServices(t *testing.T) {
	t.Setenv("WEBSOCKET_SERVER_URL", "ws://example.test:8093")
	t.Setenv("WEBSOCKET_PATH", "/events")
	t.Setenv("MATTERMOST_SERVER_URL", "http://mattermost.test")
	t.Setenv("MATTERMOST_API_TOKEN", "token")
	t.Setenv("MATTERMOST_CHANNEL", "channel")
	t.Setenv("CONFLUENCE_SETTINGS_PAGE_ID", "settings-page")
	t.Setenv("CONFLUENCE_API_ENDPOINT", "http://confluence.test")
	t.Setenv("CONFLUENCE_SPACE_KEY", "SPACE")

	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Adapters.Confluence.Enabled = true
	cfg.Adapters.Mattermost.Enabled = true
	cfg.Adapters.BadgeDB.Enabled = true
	cfg.ApplyEnv()
	container := NewContainer("1.0.0", cfg)
	err := container.RegisterServices(context.Background())
	assert.NoError(t, err)

	expected := []string{
		service.WebSocketInputServiceName,
		"BadgeDBService",
		"MattermostService",
		"ConfluenceSettingsService",
	}
	for _, name := range expected {
		svc, ok := container.GetService(name)
		assert.True(t, ok, "expected service %s to be registered", name)
		assert.Equal(t, name, svc.GetName())
		assert.Equal(t, ruleengine.DefaultWorkflowName, svc.GetWorkflow())
	}

	registered := container.GetServiceManager(context.Background()).ListServices(context.Background())
	assert.Len(t, registered, len(expected))
}

func TestRegisterServicesUsesMinimalDefaults(t *testing.T) {
	container := NewContainer("1.0.0")
	err := container.RegisterServices(context.Background())
	assert.NoError(t, err)
	assert.Len(t, container.services, 1)
	_, websocketRegistered := container.GetService(service.WebSocketInputServiceName)
	assert.True(t, websocketRegistered)
	assert.Nil(t, container.dataAccessor)
}

func TestRegisterServicesHonorsRuntimeConfigEnabledFlags(t *testing.T) {
	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Inputs.WebSocket.Enabled = false
	cfg.Adapters.Confluence.Enabled = false
	cfg.Adapters.Mattermost.Enabled = false
	cfg.Adapters.BadgeDB.Enabled = false

	container := NewContainer("1.0.0", cfg)
	err := container.RegisterServices(context.Background())
	assert.NoError(t, err)

	disabledServices := []string{
		service.WebSocketInputServiceName,
		"BadgeDBService",
		"MattermostService",
		"ConfluenceSettingsService",
	}
	for _, name := range disabledServices {
		svc, ok := container.GetService(name)
		assert.False(t, ok, "service %s should not be registered", name)
		assert.Nil(t, svc)
	}

	registered := container.GetServiceManager(context.Background()).ListServices(context.Background())
	assert.Empty(t, registered)
}

func TestRegisterServicesUsesRuntimeConfigValues(t *testing.T) {
	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Adapters.Confluence.Enabled = true
	cfg.Adapters.Mattermost.Enabled = true
	cfg.Adapters.BadgeDB.Enabled = true
	cfg.Inputs.WebSocket.ServerURL = "ws://configured-websocket.test:8093"
	cfg.Inputs.WebSocket.Path = "/events"
	cfg.Inputs.WebSocket.ReconnectInterval = 17 * time.Second
	cfg.Adapters.Confluence.APIEndpoint = "http://configured-confluence.test"
	cfg.Adapters.Confluence.SettingsPageID = "configured-page"
	cfg.Adapters.Confluence.RefreshInterval = 42 * time.Second
	cfg.Adapters.Mattermost.ServerURL = "http://configured-mattermost.test"
	cfg.Adapters.Mattermost.WebsocketURL = "ws://configured-mattermost.test/ws"
	cfg.Adapters.Mattermost.APIToken = "configured-token"
	cfg.Adapters.Mattermost.Channel = "configured-channel"
	cfg.Adapters.BadgeDB.Path = "/tmp/configured-badges.db"

	container := NewContainer("1.0.0", cfg)
	err := container.RegisterServices(context.Background())
	assert.NoError(t, err)

	ctx := context.Background()
	wsSvc, ok := container.GetService(service.WebSocketInputServiceName)
	assert.True(t, ok)
	wsMetrics := wsSvc.GetMetrics(ctx)
	assert.Equal(t, cfg.Inputs.WebSocket.ServerURL, wsMetrics["server_url"])
	assert.Equal(t, cfg.Inputs.WebSocket.Path, wsMetrics["path"])
	assert.Equal(t, cfg.Inputs.WebSocket.ReconnectInterval.String(), wsMetrics["reconnect_interval"])

	confluenceSvc, ok := container.GetService("ConfluenceSettingsService")
	assert.True(t, ok)
	confluenceMetrics := confluenceSvc.GetMetrics(ctx)
	assert.Equal(t, cfg.Adapters.Confluence.APIEndpoint, confluenceMetrics["api_endpoint"])
	assert.Equal(t, cfg.Adapters.Confluence.SettingsPageID, confluenceMetrics["page_id"])
	assert.Equal(t, cfg.Adapters.Confluence.RefreshInterval.String(), confluenceMetrics["refresh_interval"])

	mattermostSvc, ok := container.GetService("MattermostService")
	assert.True(t, ok)
	mattermostMetrics := mattermostSvc.GetMetrics(ctx)
	assert.Equal(t, cfg.Adapters.Mattermost.ServerURL, mattermostMetrics["server_url"])
	assert.Equal(t, cfg.Adapters.Mattermost.WebsocketURL, mattermostMetrics["websocket_url"])
	assert.Equal(t, cfg.Adapters.Mattermost.Channel, mattermostMetrics["channel"])
	assert.Equal(t, true, mattermostMetrics["api_token_configured"])

	badgeSvc, ok := container.GetService("BadgeDBService")
	assert.True(t, ok)
	badgeMetrics := badgeSvc.GetMetrics(ctx)
	assert.Equal(t, cfg.Adapters.BadgeDB.Path, badgeMetrics["db_path"])
}

func TestGetEnvOrDefault(t *testing.T) {
	t.Setenv("GO_SERVICE_PLATFORM_TEST_KEY", "configured")

	assert.Equal(t, "configured", getEnvOrDefault("GO_SERVICE_PLATFORM_TEST_KEY", "default"))
	assert.Equal(t, "default", getEnvOrDefault("GO_SERVICE_PLATFORM_TEST_MISSING", "default"))
}
