package health

import (
	"context"
	appctx "project/pkg/context"
	"project/pkg/logger"
	"sync"
	"time"
)

// HealthManager 管理系统健康检查
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

// NewHealthManager 创建一个新的健康检查管理器
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

// RegisterChecker 注册一个健康检查器
func (h *HealthManager) RegisterChecker(checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.checkers = append(h.checkers, checker)
}

// RegisterReporter 注册一个健康报告器
func (h *HealthManager) RegisterReporter(reporter Reporter) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.reporters = append(h.reporters, reporter)
}

// RegisterService 注册一个服务进行健康检查
func (h *HealthManager) RegisterService(service ServiceChecker) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.serviceInstances[service.GetName()] = service
}

// GetHealthReport 获取健康报告
func (h *HealthManager) GetHealthReport(ctx context.Context) *Report {
	h.mu.RLock()

	// 如果上次刷新时间在刷新间隔内，直接返回上次的报告
	if time.Since(h.lastRefresh) < h.refreshInterval && h.lastReport != nil {
		report := h.lastReport
		h.mu.RUnlock()
		return report
	}

	h.mu.RUnlock()

	// 需要刷新报告
	return h.RefreshReport(ctx)
}

// RefreshReport 刷新健康报告
func (h *HealthManager) RefreshReport(ctx context.Context) *Report {
	ctx = appctx.WithOperationName(ctx, "health_refresh")
	startTime := time.Now()

	logger.DebugWithContext(ctx, "Refreshing health report")

	h.mu.Lock()
	defer h.mu.Unlock()

	// 创建新的报告
	report := &Report{
		ServiceName:  "system",
		Status:       StatusUp,
		CheckResults: []CheckResult{},
		StartTime:    h.startTime,
		Version:      h.version,
		RefreshedAt:  time.Now(),
	}

	// 运行所有检查器
	results := h.runCheckers(ctx)

	// 添加结果到报告
	for _, result := range results {
		checkResult := *result // 转换为值类型
		report.CheckResults = append(report.CheckResults, checkResult)
	}

	// 计算总体状态
	report.Status = h.determineOverallStatus(report.CheckResults)

	// 计算刷新时间
	report.RefreshElapsed = time.Since(startTime)

	// 保存报告
	h.lastReport = report
	h.lastRefresh = time.Now()

	// 通知报告器
	for _, reporter := range h.reporters {
		go reporter.ReportHealth(ctx, report)
	}

	return report
}

// determineOverallStatus 根据检查结果确定整体状态
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

	// 确定整体状态
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

// runCheckers 运行所有健康检查器
func (h *HealthManager) runCheckers(ctx context.Context) []*CheckResult {
	ctx = appctx.WithOperationName(ctx, "run_health_checks")

	// 收集所有检查
	var checks []func(context.Context) *CheckResult

	// 添加系统检查
	checks = append(checks, h.systemChecks()...)

	// 添加服务检查
	checks = append(checks, h.serviceChecks()...)

	// 添加已注册的检查器
	for _, checker := range h.checkers {
		checkFunc := func(checker Checker) func(context.Context) *CheckResult {
			return func(ctx context.Context) *CheckResult {
				return checker.Check(ctx)
			}
		}(checker)

		checks = append(checks, checkFunc)
	}

	// 并行运行所有检查
	return RunChecksParallel(ctx, checks)
}

// systemChecks 返回系统级别的健康检查
func (h *HealthManager) systemChecks() []func(context.Context) *CheckResult {
	checks := []func(context.Context) *CheckResult{
		h.checkMemory,
		h.checkCPU,
		h.checkDiskSpace,
	}

	return checks
}

// serviceChecks 返回服务级别的健康检查
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

					// 添加指标
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

// 系统健康检查

// checkMemory 检查系统内存使用情况
func (h *HealthManager) checkMemory(ctx context.Context) *CheckResult {
	ctx = appctx.WithOperationName(ctx, "check_memory")
	result := NewCheckResult("system.memory", CategoryResources)
	result.Level = LevelWarning

	// 模拟内存检查 - 实际实现中应使用系统API获取真实数据
	memoryUsage := 0.6 // 60% 使用率

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

// checkCPU 检查CPU使用情况
func (h *HealthManager) checkCPU(ctx context.Context) *CheckResult {
	ctx = appctx.WithOperationName(ctx, "check_cpu")
	result := NewCheckResult("system.cpu", CategoryResources)
	result.Level = LevelWarning

	// 模拟CPU检查 - 实际实现中应使用系统API获取真实数据
	cpuUsage := 0.3 // 30% 使用率

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

// checkDiskSpace 检查磁盘空间使用情况
func (h *HealthManager) checkDiskSpace(ctx context.Context) *CheckResult {
	ctx = appctx.WithOperationName(ctx, "check_disk")
	result := NewCheckResult("system.disk", CategoryResources)
	result.Level = LevelWarning

	// 模拟磁盘检查 - 实际实现中应使用系统API获取真实数据
	diskUsage := 0.7 // 70% 使用率

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
