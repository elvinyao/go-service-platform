package health

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/logger"
)

// HealthManager manages system health checks
type HealthManager struct {
	mu               sync.RWMutex
	refreshMu        sync.Mutex
	checkers         []Checker
	reporters        []Reporter
	refreshInterval  time.Duration
	lastRefresh      time.Time
	lastReport       *Report
	version          string
	serviceInstances map[string]ServiceChecker
	startTime        time.Time
	shuttingDown     int32 // atomic: 1 = shutting down
}

// NewHealthManager creates a new health check manager
func NewHealthManager(refreshInterval time.Duration, version string) *HealthManager {
	return &HealthManager{
		checkers:         make([]Checker, 0),
		reporters:        make([]Reporter, 0),
		refreshInterval:  refreshInterval,
		version:          version,
		serviceInstances: make(map[string]ServiceChecker),
		startTime:        time.Now(),
	}
}

// RegisterChecker registers a health checker
func (h *HealthManager) RegisterChecker(checker Checker) {
	if isNilValue(checker) {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.checkers = append(h.checkers, checker)
}

// RegisterReporter registers a health reporter
func (h *HealthManager) RegisterReporter(reporter Reporter) {
	if isNilValue(reporter) {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.reporters = append(h.reporters, reporter)
}

// RegisterService registers a service for health checking
func (h *HealthManager) RegisterService(service ServiceChecker) {
	if isNilValue(service) {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.serviceInstances[service.GetName()] = service
}

// SetShuttingDown marks the system as shutting down, causing readiness checks to fail
func (h *HealthManager) SetShuttingDown() {
	atomic.StoreInt32(&h.shuttingDown, 1)
}

// IsShuttingDown returns true if the system is in the process of shutting down
func (h *HealthManager) IsShuttingDown() bool {
	return atomic.LoadInt32(&h.shuttingDown) == 1
}

// GetHealthReport retrieves the health report
func (h *HealthManager) GetHealthReport(ctx context.Context) *Report {
	h.mu.RLock()

	// If the last refresh was within the refresh interval, return the cached report
	if time.Since(h.lastRefresh) < h.refreshInterval && h.lastReport != nil {
		report := CloneReport(h.lastReport)
		h.mu.RUnlock()
		return report
	}

	h.mu.RUnlock()

	// Need to refresh the report
	return h.RefreshReport(ctx)
}

// RefreshReport refreshes the health report
func (h *HealthManager) RefreshReport(ctx context.Context) *Report {
	ctx = appctx.WithOperationName(ctx, "health_refresh")
	startTime := time.Now()

	logger.DebugWithContext(ctx, "Refreshing health report")

	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()

	checkers, reporters, services, managerStartTime, version := h.snapshot()

	// Create new report
	report := &Report{
		ServiceName:  "system",
		Status:       StatusUp,
		CheckResults: []CheckResult{},
		StartTime:    managerStartTime,
		Uptime:       time.Since(managerStartTime),
		Version:      version,
		RefreshedAt:  time.Now(),
	}

	// Run all checkers
	results := h.runCheckers(ctx, checkers, services)

	// Add results to report
	for _, result := range results {
		if result == nil {
			continue
		}
		checkResult := *result // Convert to value type
		report.CheckResults = append(report.CheckResults, checkResult)
	}

	// Reporters synchronously contribute results before status calculation and caching.
	for index, reporter := range reporters {
		if recovered := callReporter(ctx, reporter, report); recovered != nil {
			logger.ErrorfWithContext(ctx, "Health reporter %T panicked: %v", reporter, recovered)
			result := newControlCheckResult(
				fmt.Sprintf("reporter.%d", index),
				"Health reporter panicked",
			)
			result.AddDetail("reporter_type", fmt.Sprintf("%T", reporter))
			report.CheckResults = append(report.CheckResults, *result)
		}
	}

	// Calculate overall status
	report.Status = h.determineOverallStatus(report.CheckResults)

	// Calculate refresh time
	report.RefreshElapsed = time.Since(startTime)

	// Save report
	h.mu.Lock()
	h.lastReport = CloneReport(report)
	h.lastRefresh = time.Now()
	h.mu.Unlock()

	return CloneReport(report)
}

func (h *HealthManager) snapshot() ([]Checker, []Reporter, map[string]ServiceChecker, time.Time, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	checkers := append([]Checker(nil), h.checkers...)
	reporters := append([]Reporter(nil), h.reporters...)
	services := make(map[string]ServiceChecker, len(h.serviceInstances))
	for name, service := range h.serviceInstances {
		services[name] = service
	}

	return checkers, reporters, services, h.startTime, h.version
}

// determineOverallStatus determines the overall status based on check results
func (h *HealthManager) determineOverallStatus(results []CheckResult) Status {
	return DetermineStatus(results)
}

// runCheckers runs all health checkers
func (h *HealthManager) runCheckers(
	ctx context.Context,
	checkers []Checker,
	services map[string]ServiceChecker,
) []*CheckResult {
	ctx = appctx.WithOperationName(ctx, "run_health_checks")

	// Collect all checks
	var checks []func(context.Context) *CheckResult

	// Add system checks
	checks = append(checks, h.systemChecks()...)

	// Add service checks
	checks = append(checks, h.serviceChecks(services)...)

	// Add registered checkers
	for _, checker := range checkers {
		checkFunc := func(checker Checker) func(context.Context) *CheckResult {
			return func(ctx context.Context) *CheckResult {
				return checker.Check(ctx)
			}
		}(checker)

		checks = append(checks, checkFunc)
	}

	// Run all checks in parallel
	return RunChecksParallel(ctx, checks)
}

// systemChecks returns system-level health checks
func (h *HealthManager) systemChecks() []func(context.Context) *CheckResult {
	memoryChecker := NewMemoryUsageChecker()
	goroutineChecker := NewGoroutineCountChecker()
	checks := []func(context.Context) *CheckResult{
		memoryChecker.Check,
		goroutineChecker.Check,
	}

	return checks
}

// serviceChecks returns service-level health checks
func (h *HealthManager) serviceChecks(services map[string]ServiceChecker) []func(context.Context) *CheckResult {
	var checks []func(context.Context) *CheckResult

	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := services[name]
		checkFunc := func(name string, svc ServiceChecker) func(context.Context) *CheckResult {
			return func(ctx context.Context) *CheckResult {
				ctx = appctx.WithOperationName(ctx, "check_service_"+name)
				result := NewCheckResult("service."+name, CategoryDependency)
				result.Level = LevelCritical

				if !svc.IsRunning(ctx) {
					result.SetStatus(StatusDown, "Service is not running")
				} else {
					result.SetStatus(StatusUp, "Service is running")

					// Add metrics
					metrics := svc.GetMetrics(ctx)
					for k, v := range metrics {
						result.AddDetail(k, v)
					}
				}

				result.Complete()
				return result
			}
		}(name, svc)

		checks = append(checks, checkFunc)

		if provider, ok := svc.(ServiceHealthProvider); ok {
			for _, checker := range provider.RegisterHealthChecks() {
				if checker == nil {
					continue
				}
				customCheck := func(serviceName string, checker Checker) func(context.Context) *CheckResult {
					return func(ctx context.Context) *CheckResult {
						result := checker.Check(ctx)
						if result == nil {
							result = NewCheckResult("invalid", CategoryDependency)
							result.Level = LevelCritical
							result.SetStatus(StatusUnknown, "Health checker returned no result")
							result.Complete()
						}
						result.Name = "service." + serviceName + "." + result.Name
						return result
					}
				}(name, checker)
				checks = append(checks, customCheck)
			}
		}
	}

	return checks
}
