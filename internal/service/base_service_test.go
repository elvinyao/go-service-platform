package service

import (
	"context"
	"testing"
	"time"

	"project/pkg/health"

	"github.com/stretchr/testify/assert"
)

func TestNewBaseService(t *testing.T) {
	// Arrange
	name := "TestService"
	workflow := "TestWorkflow"
	serviceType := "TestType"
	version := "1.0.0"

	// Act
	service := NewBaseService(name, workflow, serviceType, version)

	// Assert
	assert.NotNil(t, service)
	assert.Equal(t, name, service.GetName())
	assert.Equal(t, workflow, service.GetWorkflow())
	assert.Equal(t, serviceType, service.GetType())
	assert.Equal(t, version, service.version)
	assert.False(t, service.running)
	assert.NotNil(t, service.customCheckers)
	assert.Empty(t, service.customCheckers)
}

func TestGetName(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")

	// Act
	name := service.GetName()

	// Assert
	assert.Equal(t, "TestService", name)
}

func TestGetWorkflow(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")

	// Act
	workflow := service.GetWorkflow()

	// Assert
	assert.Equal(t, "TestWorkflow", workflow)
}

func TestGetType(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")

	// Act
	svcType := service.GetType()

	// Assert
	assert.Equal(t, "TestType", svcType)
}

func TestIsRunning(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")
	ctx := context.Background()

	// Initial state should be not running
	assert.False(t, service.IsRunning(ctx))

	// Set running to true
	service.setRunning(true)

	// Act & Assert
	assert.True(t, service.IsRunning(ctx))

	// Set running to false
	service.setRunning(false)

	// Act & Assert
	assert.False(t, service.IsRunning(ctx))
}

func TestAddHealthChecker(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")
	mockChecker := &mockHealthChecker{}

	// Initial state should have no checkers
	assert.Empty(t, service.customCheckers)

	// Act
	service.AddHealthChecker(mockChecker)

	// Assert
	assert.Len(t, service.customCheckers, 1)
	assert.Contains(t, service.customCheckers, mockChecker)
}

func TestRegisterHealthChecks(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")
	mockChecker := &mockHealthChecker{}
	service.AddHealthChecker(mockChecker)

	// Act
	checkers := service.RegisterHealthChecks()

	// Assert - should contain both the default running checker and our custom one
	assert.Len(t, checkers, 2)
	// One should be the service running checker
	foundRunningChecker := false
	foundCustomChecker := false
	for _, checker := range checkers {
		if _, ok := checker.(*serviceRunningChecker); ok {
			foundRunningChecker = true
		}
		if checker == mockChecker {
			foundCustomChecker = true
		}
	}
	assert.True(t, foundRunningChecker, "Should contain the service running checker")
	assert.True(t, foundCustomChecker, "Should contain the custom checker")
}

func TestGetMetrics(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")
	ctx := context.Background()
	service.startTime = time.Now().Add(-10 * time.Second) // Started 10 seconds ago

	// Act
	metrics := service.GetMetrics(ctx)

	// Assert
	assert.NotNil(t, metrics)
	assert.Equal(t, service.GetName(), metrics["name"])
	assert.Equal(t, service.GetType(), metrics["type"])
	assert.Equal(t, service.IsRunning(ctx), metrics["running"])
	assert.InDelta(t, 10.0, metrics["uptime_seconds"].(float64), 1.0)
}

func TestReportHealth(t *testing.T) {
	// Arrange
	service := NewBaseService("TestService", "TestWorkflow", "TestType", "1.0.0")
	ctx := context.Background()
	report := &health.Report{
		ServiceName:  "TestService",
		StartTime:    time.Now(),
		Version:      "1.0.0",
		CheckResults: []health.CheckResult{},
	}

	// Act
	service.ReportHealth(ctx, report)

	// Assert
	assert.NotEmpty(t, report.CheckResults)
	// Should have at least the service running check
	foundRunningCheck := false
	for _, result := range report.CheckResults {
		if result.Name == "service-running" {
			foundRunningCheck = true
			break
		}
	}
	assert.True(t, foundRunningCheck, "Report should contain the service running check")
}

// Mock implementation of health.Checker for testing
type mockHealthChecker struct{}

func (m *mockHealthChecker) Check(ctx context.Context) *health.CheckResult {
	return health.NewCheckResult("mock-check", health.CategoryResources)
}
