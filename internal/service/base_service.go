package service

import (
	"context"
	"fmt"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
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

func (s *BaseService) setRunning(r bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r && !s.running {
		s.startTime = time.Now()
	}
	s.running = r
}

// LockRunning updates running state for service implementations and tests.
func (s *BaseService) LockRunning(r bool) {
	s.setRunning(r)
}

// AddHealthChecker adds a custom health checker
func (s *BaseService) AddHealthChecker(checker health.Checker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customCheckers = append(s.customCheckers, checker)
}

// RegisterHealthChecks returns service-specific health checkers.
func (s *BaseService) RegisterHealthChecks() []health.Checker {
	s.mu.RLock()
	defer s.mu.RUnlock()

	checkers := append([]health.Checker(nil), s.customCheckers...)
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

	checkers := append([]health.Checker{serviceRunningHealthChecker{service: s}}, s.RegisterHealthChecks()...)

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

type serviceRunningHealthChecker struct {
	service *BaseService
}

func (c serviceRunningHealthChecker) Check(ctx context.Context) *health.CheckResult {
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
