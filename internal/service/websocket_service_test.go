package service

import (
	"context"
	"testing"
	"time"

	"project/internal/config"
	"project/internal/model"
	"project/pkg/health"

	"github.com/stretchr/testify/assert"
)

func testWebSocketConfig() config.WebSocketConfig {
	return config.WebSocketConfig{
		ServerURL:         "ws://localhost:0",
		Path:              "/ws",
		ReconnectInterval: 1 * time.Second,
	}
}

func TestNewWebSocketService(t *testing.T) {
	// Arrange
	name := "TestWebSocketService"
	workflow := "TestWorkflow"
	version := "1.0.0"

	// Act
	service := NewWebSocketService(name, workflow, testWebSocketConfig(), version)

	// Assert
	assert.NotNil(t, service)
	assert.Equal(t, name, service.GetName())
	assert.Equal(t, workflow, service.GetWorkflow())
	assert.Equal(t, "websocket", service.GetType())
	assert.Equal(t, version, service.BaseService.version)
	assert.Equal(t, 0, service.connections)
	assert.True(t, service.lastMessage.IsZero())

	// Check that the custom health checker was added
	healthCheckers := service.RegisterHealthChecks()
	assert.Greater(t, len(healthCheckers), 1)

	// One should be the websocket connection checker
	foundConnectionChecker := false
	for _, checker := range healthCheckers {
		if _, ok := checker.(*websocketConnectionChecker); ok {
			foundConnectionChecker = true
			break
		}
	}
	assert.True(t, foundConnectionChecker, "Should contain the websocket connection checker")
}

func TestWebSocketService_StartStop(t *testing.T) {
	// Arrange
	service := NewWebSocketService("TestWebSocketService", "TestWorkflow", testWebSocketConfig(), "1.0.0")
	ctx := context.Background()

	// Act - Start
	err := service.Start(ctx)

	// Assert
	assert.NoError(t, err)
	assert.True(t, service.IsRunning(ctx))

	// Act - Stop
	err = service.Stop(ctx)

	// Assert
	assert.NoError(t, err)
	assert.False(t, service.IsRunning(ctx))
}

func TestWebSocketService_Restart(t *testing.T) {
	// Arrange
	service := NewWebSocketService("TestWebSocketService", "TestWorkflow", testWebSocketConfig(), "1.0.0")
	ctx := context.Background()

	// Act - Start first
	err := service.Start(ctx)
	assert.NoError(t, err)

	// Act - Restart
	err = service.Restart(ctx)

	// Assert
	assert.NoError(t, err)
	assert.True(t, service.IsRunning(ctx))

	// Clean up
	service.Stop(ctx)
}

func TestWebSocketService_OnMessage(t *testing.T) {
	// Arrange
	service := NewWebSocketService("TestWebSocketService", "TestWorkflow", testWebSocketConfig(), "1.0.0")
	ctx := context.Background()

	// Start the service so ProcessIncomingMessage doesn't reject
	err := service.Start(ctx)
	assert.NoError(t, err)
	defer service.Stop(ctx)

	received := make(chan model.Message, 1)

	// Create a message handler
	handler := func(msg model.Message) {
		received <- msg
	}

	// Act - Register message handler
	service.OnMessage(handler)

	// Create a test message
	message := model.Message{
		ID:        "test_id",
		Type:      "test_type",
		Content:   "test_content",
		UserID:    "test_user",
		Timestamp: time.Now(),
	}

	// Act - Process a message
	err = service.ProcessIncomingMessage(ctx, message)

	// Assert
	assert.NoError(t, err)

	select {
	case msg := <-received:
		assert.Equal(t, "test_type", msg.Type)
		assert.Equal(t, "test_content", msg.Content)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message handler")
	}
	assert.False(t, service.lastMessage.IsZero())
}

func TestWebSocketService_GetMetrics(t *testing.T) {
	// Arrange
	service := NewWebSocketService("TestWebSocketService", "TestWorkflow", testWebSocketConfig(), "1.0.0")
	ctx := context.Background()

	// Start the service to get meaningful metrics
	service.Start(ctx)
	defer service.Stop(ctx)

	// Set some connection count for testing
	service.connections = 5
	service.lastMessage = time.Now().Add(-10 * time.Second)

	// Act
	metrics := service.GetMetrics(ctx)

	// Assert
	assert.NotNil(t, metrics)
	// Check base metrics
	assert.Equal(t, service.GetType(), metrics["type"])
	assert.Equal(t, service.GetWorkflow(), metrics["workflow"])
	assert.Equal(t, service.IsRunning(ctx), metrics["running"])

	// Check websocket specific metrics
	assert.Equal(t, 5, metrics["connections"].(int))
	assert.InDelta(t, 10.0, metrics["last_message_age_seconds"].(float64), 1.0)
}

func TestWebSocketService_Configure(t *testing.T) {
	// Arrange
	service := NewWebSocketService("TestWebSocketService", "TestWorkflow", testWebSocketConfig(), "1.0.0")
	ctx := context.Background()

	// Simple config object
	config := map[string]interface{}{
		"max_connections": 100,
	}

	// Act
	err := service.Configure(ctx, config)

	// Assert
	assert.NoError(t, err)
	// In a real test, we would check that the configuration was applied correctly
}

func TestWebSocketService_ReportHealth(t *testing.T) {
	// Arrange
	service := NewWebSocketService("TestWebSocketService", "TestWorkflow", testWebSocketConfig(), "1.0.0")
	ctx := context.Background()
	report := &health.Report{
		ServiceName:  "TestWebSocketService",
		StartTime:    time.Now(),
		Version:      "1.0.0",
		CheckResults: []health.CheckResult{},
	}

	// Act
	service.ReportHealth(ctx, report)

	// Assert
	assert.NotEmpty(t, report.CheckResults)

	// Should have websocket-specific health checks
	foundConnectionCheck := false
	for _, result := range report.CheckResults {
		if result.Name == "websocket-connections" {
			foundConnectionCheck = true
			break
		}
	}
	assert.True(t, foundConnectionCheck, "Report should contain websocket connection health check")
}
