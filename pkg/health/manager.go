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

// LifecycleState represents the operational lifecycle phase of a HealthManager.
type LifecycleState uint32

const (
	// LifecycleActive indicates normal health and readiness evaluation.
	LifecycleActive LifecycleState = iota
	// LifecycleShuttingDown indicates the process is draining and not ready for traffic.
	LifecycleShuttingDown
)

type reportCacheState uint8

const (
	reportCacheCold reportCacheState = iota
	reportCacheFresh
	reportCacheExpired
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
	lifecycle        atomic.Uint32
}

// NewHealthManager creates a new health check manager
func NewHealthManager(refreshInterval time.Duration, version string) *HealthManager {
	mgr := &HealthManager{
		checkers:         make([]Checker, 0),
		reporters:        make([]Reporter, 0),
		refreshInterval:  refreshInterval,
		version:          version,
		serviceInstances: make(map[string]ServiceChecker),
		startTime:        time.Now(),
	}
	mgr.lifecycle.Store(uint32(LifecycleActive))
	return mgr
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

// Lifecycle returns the current manager lifecycle state.
func (h *HealthManager) Lifecycle() LifecycleState {
	return LifecycleState(h.lifecycle.Load())
}

// SetShuttingDown transitions the manager to LifecycleShuttingDown, causing readiness checks to fail.
func (h *HealthManager) SetShuttingDown() {
	h.lifecycle.Store(uint32(LifecycleShuttingDown))
}

// SetActive transitions the manager back to LifecycleActive.
func (h *HealthManager) SetActive() {
	h.lifecycle.Store(uint32(LifecycleActive))
}

// IsShuttingDown returns true if the system is in the process of shutting down.
func (h *HealthManager) IsShuttingDown() bool {
	return h.Lifecycle() == LifecycleShuttingDown
}

func (h *HealthManager) cacheStateLocked(now time.Time) reportCacheState {
	if h.lastReport == nil {
		return reportCacheCold
	}
	if h.refreshInterval > 0 && now.Sub(h.lastRefresh) < h.refreshInterval {
		return reportCacheFresh
	}
	return reportCacheExpired
}

func (h *HealthManager) cachedReportIfFresh() (*Report, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.cacheStateLocked(time.Now()) == reportCacheFresh {
		return CloneReport(h.lastReport), true
	}
	return nil, false
}

// GetHealthReport retrieves a cached health report when fresh or refreshes it single-flight.
func (h *HealthManager) GetHealthReport(ctx context.Context) *Report {
	if report, ok := h.cachedReportIfFresh(); ok {
		return report
	}

	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()

	if report, ok := h.cachedReportIfFresh(); ok {
		return report
	}

	return h.refreshReportLocked(ctx)
}

// RefreshReport forces a fresh health report evaluation.
func (h *HealthManager) RefreshReport(ctx context.Context) *Report {
	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()
	return h.refreshReportLocked(ctx)
}

func (h *HealthManager) refreshReportLocked(ctx context.Context) *Report {
	ctx = appctx.WithOperationName(ctx, "health_refresh")
	startTime := time.Now()

	logger.DebugWithContext(ctx, "Refreshing health report")

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
