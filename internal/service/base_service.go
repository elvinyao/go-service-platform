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
func (s *BaseService) IsRunning(ctx context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// internal/service/base_service.go
func (s *BaseService) setRunningLocked(r bool) {
	if r && !s.running {
		s.startTime = time.Now()
	}
	s.running = r
}

// Thread-safe setter
func (s *BaseService) setRunning(r bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setRunningLocked(r)
}

func (s *BaseService) LockRunning(r bool) {
	s.setRunning(r)
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

// GetMetrics returns basic metrics for the service
func (s *BaseService) GetMetrics(ctx context.Context) map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := make(map[string]interface{})
	metrics["running"] = s.running
	metrics["type"] = s.serviceType
	metrics["workflow"] = s.workflow

	if !s.startTime.IsZero() {
		metrics["uptime_seconds"] = time.Since(s.startTime).Seconds()
		metrics["start_time"] = s.startTime.Format(time.RFC3339)
	}

	return metrics
}

// ReportHealth reports health status to the provided report
func (s *BaseService) ReportHealth(ctx context.Context, report *health.Report) {
	ctx = appctx.WithServiceName(ctx, s.name)
	ctx = appctx.WithOperationName(ctx, "health_report")

	logger.DebugfWithContext(ctx, "Reporting health for service: %s", s.name)

	// Get all registered health checkers
	checkers := s.RegisterHealthChecks()

	// Convert checkers to check functions
	checkFuncs := make([]func(context.Context) *health.CheckResult, 0, len(checkers))
	for _, checker := range checkers {
		checkFuncs = append(checkFuncs, func(ctx context.Context) *health.CheckResult {
			return checker.Check(ctx)
		})
	}

	// Run checks in parallel
	results := health.RunChecksParallel(ctx, checkFuncs)

	// Add results to report
	for _, result := range results {
		checkResult := *result // Convert to value type
		report.CheckResults = append(report.CheckResults, checkResult)
	}
}

// serviceRunningChecker checks if a service is running
type serviceRunningChecker struct {
	service *BaseService
}

// Check implements the Checker interface
func (c *serviceRunningChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("service-running", health.CategoryConnectivity)
	result.Level = health.LevelCritical

	if c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusUp, fmt.Sprintf("Service %s is running", c.service.GetName()))
	} else {
		result.SetStatus(health.StatusDown, fmt.Sprintf("Service %s is not running", c.service.GetName()))
	}

	result.Complete()
	return result
}
