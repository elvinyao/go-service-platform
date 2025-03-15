package service

import (
	"context"
	"fmt"
	appctx "project/pkg/context"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"time"
)

// BaseService provides common functionality for all services
type BaseService struct {
	name           string
	workflow       string
	serviceType    string
	running        bool
	mu             sync.RWMutex
	startTime      time.Time
	version        string
	customCheckers []health.Checker
}

// NewBaseService creates a new base service
func NewBaseService(name, workflow, serviceType, version string) *BaseService {
	return &BaseService{
		name:           name,
		workflow:       workflow,
		serviceType:    serviceType,
		version:        version,
		customCheckers: []health.Checker{},
	}
}

// GetName returns the service name
func (s *BaseService) GetName() string {
	return s.name
}

// GetWorkflow returns the service workflow
func (s *BaseService) GetWorkflow() string {
	return s.workflow
}

// GetType returns the service type
func (s *BaseService) GetType() string {
	return s.serviceType
}

// IsRunning returns whether the service is running
func (s *BaseService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// setRunning sets the running state (must be called with lock held)
func (s *BaseService) setRunning(running bool) {
	if running && !s.running {
		s.startTime = time.Now()
	}
	s.running = running
}

// LockRunning sets the running state with lock protection
func (s *BaseService) LockRunning(running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setRunning(running)
}

// AddHealthChecker adds a custom health checker
func (s *BaseService) AddHealthChecker(checker health.Checker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customCheckers = append(s.customCheckers, checker)
}

// RegisterHealthChecks returns default health checkers for this service
func (s *BaseService) RegisterHealthChecks() []health.Checker {
	// Base checkers that apply to all services
	baseCheckers := []health.Checker{
		// Memory usage check
		health.NewMemoryUsageChecker(),

		// Goroutine count check
		health.NewGoroutineCountChecker(),

		// Running status check
		&serviceRunningChecker{service: s},
	}

	// Add any custom checkers
	s.mu.RLock()
	defer s.mu.RUnlock()

	checkers := make([]health.Checker, 0, len(baseCheckers)+len(s.customCheckers))
	checkers = append(checkers, baseCheckers...)
	checkers = append(checkers, s.customCheckers...)

	return checkers
}

// HealthCheck performs all health checks and returns a consolidated report
func (s *BaseService) HealthCheck(ctx context.Context) health.Report {
	ctx = appctx.WithServiceName(ctx, s.name)
	ctx = appctx.WithOperationName(ctx, "health_check")

	logger.DebugfWithContext(ctx, "Running health check for service: %s", s.name)

	s.mu.RLock()
	version := s.version
	startTime := s.startTime
	s.mu.RUnlock()

	// Create new report
	report := health.NewReport(s.name, startTime, version)

	// Get all registered health checkers
	checkers := s.RegisterHealthChecks()

	// Run checks in parallel
	results := health.RunChecksParallel(ctx, checkers)

	// Add results to report
	for _, result := range results {
		report.AddResult(result)
	}

	report.Complete()

	// Log results
	if !report.IsHealthy() {
		logger.WarnfWithContext(ctx, "Health check for service %s returned status: %s", s.name, report.Status)
	} else {
		logger.DebugfWithContext(ctx, "Health check for service %s successful", s.name)
	}

	return report
}

// serviceRunningChecker checks if a service is running
type serviceRunningChecker struct {
	service *BaseService
}

// Check implements the Checker interface
func (c *serviceRunningChecker) Check(ctx context.Context) health.CheckResult {
	result := health.NewCheckResult("service-running", health.CategoryConnectivity, health.LevelCritical)

	isRunning := c.service.IsRunning()
	result.AddDetail("running", fmt.Sprintf("%v", isRunning))

	if isRunning {
		result.SetStatus(health.StatusUp, "Service is running")
	} else {
		result.SetStatus(health.StatusDown, "Service is not running")
	}

	result.Complete()
	return result
}
