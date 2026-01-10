package health

import (
	"context"
	appctx "project/pkg/context"
	"project/pkg/logger"
	"sync"
	"time"
)

// HealthManager manages system health checks
type HealthManager struct {
	mu               sync.RWMutex
	checkers         []Checker
	reporters        []Reporter
	refreshInterval  time.Duration
	lastRefresh      time.Time
	lastReport       *Report
	version          string
	serviceInstances map[string]ServiceChecker
	startTime        time.Time
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
	h.mu.Lock()
	defer h.mu.Unlock()

	h.checkers = append(h.checkers, checker)
}

// RegisterReporter registers a health reporter
func (h *HealthManager) RegisterReporter(reporter Reporter) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.reporters = append(h.reporters, reporter)
}

// RegisterService registers a service for health checking
func (h *HealthManager) RegisterService(service ServiceChecker) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.serviceInstances[service.GetName()] = service
}

// GetHealthReport retrieves the health report
func (h *HealthManager) GetHealthReport(ctx context.Context) *Report {
	h.mu.RLock()

	// If the last refresh was within the refresh interval, return the cached report
	if time.Since(h.lastRefresh) < h.refreshInterval && h.lastReport != nil {
		report := h.lastReport
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

	h.mu.Lock()
	defer h.mu.Unlock()

	// Create new report
	report := &Report{
		ServiceName:  "system",
		Status:       StatusUp,
		CheckResults: []CheckResult{},
		StartTime:    h.startTime,
		Version:      h.version,
		RefreshedAt:  time.Now(),
	}

	// Run all checkers
	results := h.runCheckers(ctx)

	// Add results to report
	for _, result := range results {
		checkResult := *result // Convert to value type
		report.CheckResults = append(report.CheckResults, checkResult)
	}

	// Calculate overall status
	report.Status = h.determineOverallStatus(report.CheckResults)

	// Calculate refresh time
	report.RefreshElapsed = time.Since(startTime)

	// Save report
	h.lastReport = report
	h.lastRefresh = time.Now()

	// Notify reporters
	for _, reporter := range h.reporters {
		go reporter.ReportHealth(ctx, report)
	}

	return report
}

// determineOverallStatus determines the overall status based on check results
func (h *HealthManager) determineOverallStatus(results []CheckResult) Status {
	var criticalCount, warningCount, upCount, unknownCount int

	for _, result := range results {
		switch result.Status {
		case StatusUp:
			upCount++
		case StatusDegraded:
			if result.Level == LevelCritical {
				criticalCount++
			} else {
				warningCount++
			}
		case StatusDown:
			criticalCount++
		case StatusUnknown:
			unknownCount++
		}
	}

	// Determine overall status
	if criticalCount > 0 {
		return StatusDegraded
	} else if warningCount > 0 {
		return StatusDegraded
	} else if upCount > 0 && upCount == len(results) {
		return StatusUp
	} else if unknownCount > 0 && unknownCount == len(results) {
		return StatusUnknown
	}

	return StatusUnknown
}

// runCheckers runs all health checkers
func (h *HealthManager) runCheckers(ctx context.Context) []*CheckResult {
	ctx = appctx.WithOperationName(ctx, "run_health_checks")

	// Collect all checks
	var checks []func(context.Context) *CheckResult

	// Add system checks
	checks = append(checks, h.systemChecks()...)

	// Add service checks
	checks = append(checks, h.serviceChecks()...)

	// Add registered checkers
	for _, checker := range h.checkers {
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
	checks := []func(context.Context) *CheckResult{
		h.checkMemory,
		h.checkCPU,
		h.checkDiskSpace,
	}

	return checks
}

// serviceChecks returns service-level health checks
func (h *HealthManager) serviceChecks() []func(context.Context) *CheckResult {
	var checks []func(context.Context) *CheckResult

	for name, svc := range h.serviceInstances {
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
	}

	return checks
}

// System health checks

// checkMemory checks system memory usage
func (h *HealthManager) checkMemory(ctx context.Context) *CheckResult {
	ctx = appctx.WithOperationName(ctx, "check_memory")
	result := NewCheckResult("system.memory", CategoryResources)
	result.Level = LevelWarning

	// Simulate memory check - in actual implementation, use system API to get real data
	memoryUsage := 0.6 // 60% usage

	result.AddDetail("usage_percent", memoryUsage*100)

	if memoryUsage > DefaultThresholds.MemoryCritical {
		result.SetStatus(StatusDegraded, "Memory usage is critical")
		result.Level = LevelCritical
	} else if memoryUsage > DefaultThresholds.MemoryWarning {
		result.SetStatus(StatusDegraded, "Memory usage is high")
	} else {
		result.SetStatus(StatusUp, "Memory usage is normal")
	}

	result.Complete()
	return result
}

// checkCPU checks CPU usage
func (h *HealthManager) checkCPU(ctx context.Context) *CheckResult {
	ctx = appctx.WithOperationName(ctx, "check_cpu")
	result := NewCheckResult("system.cpu", CategoryResources)
	result.Level = LevelWarning

	// Simulate CPU check - in actual implementation, use system API to get real data
	cpuUsage := 0.3 // 30% usage

	result.AddDetail("usage_percent", cpuUsage*100)

	if cpuUsage > DefaultThresholds.CPUCritical {
		result.SetStatus(StatusDegraded, "CPU usage is critical")
		result.Level = LevelCritical
	} else if cpuUsage > DefaultThresholds.CPUWarning {
		result.SetStatus(StatusDegraded, "CPU usage is high")
	} else {
		result.SetStatus(StatusUp, "CPU usage is normal")
	}

	result.Complete()
	return result
}

// checkDiskSpace checks disk space usage
func (h *HealthManager) checkDiskSpace(ctx context.Context) *CheckResult {
	ctx = appctx.WithOperationName(ctx, "check_disk")
	result := NewCheckResult("system.disk", CategoryResources)
	result.Level = LevelWarning

	// Simulate disk check - in actual implementation, use system API to get real data
	diskUsage := 0.7 // 70% usage

	result.AddDetail("usage_percent", diskUsage*100)

	if diskUsage > DefaultThresholds.DiskCritical {
		result.SetStatus(StatusDegraded, "Disk usage is critical")
		result.Level = LevelCritical
	} else if diskUsage > DefaultThresholds.DiskWarning {
		result.SetStatus(StatusDegraded, "Disk usage is high")
	} else {
		result.SetStatus(StatusUp, "Disk usage is normal")
	}

	result.Complete()
	return result
}
