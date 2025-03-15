package manager

import (
	"context"
	"project/internal/interfaces"
	"project/internal/service"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"time"
)

type ServiceManager struct {
	services  map[string]service.Service
	mu        sync.RWMutex
	healthMgr *health.HealthManager
	version   string
	startTime time.Time
}

// 确保 ServiceManager 实现了 interfaces.ServiceManager 接口
var _ interfaces.ServiceManager = (*ServiceManager)(nil)

func NewServiceManager(version string) *ServiceManager {
	manager := &ServiceManager{
		services:  make(map[string]service.Service),
		version:   version,
		startTime: time.Now(),
	}

	// Initialize health manager
	manager.healthMgr = health.NewHealthManager(30*time.Second, version)

	return manager
}

func (sm *ServiceManager) RegisterService(s service.Service) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s == nil {
		logger.Errorf("Attempted to register nil service")
		return
	}

	serviceName := s.GetName()
	if serviceName == "" {
		logger.Errorf("Attempted to register service with empty name")
		return
	}

	if _, exists := sm.services[serviceName]; exists {
		logger.Warnf("Service with name %s already registered, overwriting", serviceName)
	}

	sm.services[serviceName] = s

	// 将服务注册到健康检查管理器
	sm.healthMgr.RegisterService(s)
}

// GetServiceHealth 获取特定服务的健康信息
func (sm *ServiceManager) GetServiceHealth(ctx context.Context, serviceName string) (health.Report, bool) {
	ctx = appctx.WithOperationName(ctx, "get_service_health")

	// 创建服务健康报告
	report := health.Report{
		ServiceName: serviceName,
		StartTime:   sm.startTime,
		Version:     sm.version,
		RefreshedAt: time.Now(),
	}

	// 获取服务
	sm.mu.Lock()
	svc, exists := sm.services[serviceName]
	sm.mu.Unlock()

	if !exists {
		return report, false
	}

	// 添加服务健康检查结果
	result := health.CheckResult{
		Name:        "service." + serviceName,
		Status:      health.StatusUnknown,
		Category:    health.CategoryDependency,
		Level:       health.LevelCritical,
		Timestamp:   time.Now(),
		Description: "Service status",
	}

	if svc.IsRunning(ctx) {
		result.Status = health.StatusUp
		result.Description = "Service is running"
	} else {
		result.Status = health.StatusDown
		result.Description = "Service is not running"
	}

	report.CheckResults = append(report.CheckResults, result)
	report.Status = result.Status

	return report, true
}

// GetAllServicesHealth 获取所有服务的健康信息
func (sm *ServiceManager) GetAllServicesHealth(ctx context.Context) map[string]health.Report {
	ctx = appctx.WithOperationName(ctx, "get_all_services_health")

	result := make(map[string]health.Report)

	sm.mu.Lock()
	serviceNames := make([]string, 0, len(sm.services))
	for name := range sm.services {
		serviceNames = append(serviceNames, name)
	}
	sm.mu.Unlock()

	for _, name := range serviceNames {
		if report, exists := sm.GetServiceHealth(ctx, name); exists {
			result[name] = report
		}
	}

	return result
}

// GetSystemHealth 获取系统整体健康状态
func (sm *ServiceManager) GetSystemHealth(ctx context.Context) health.Report {
	ctx = appctx.WithOperationName(ctx, "get_system_health")

	reports := sm.GetAllServicesHealth(ctx)

	// 创建系统健康报告
	report := health.Report{
		ServiceName: "system",
		Status:      health.StatusUp,
		StartTime:   sm.startTime,
		Version:     sm.version,
		RefreshedAt: time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	// 处理服务健康状态
	var upCount, degradedCount, downCount int

	for serviceName, serviceReport := range reports {
		// 添加服务状态到报告
		result := health.CheckResult{
			Name:        "service." + serviceName,
			Status:      serviceReport.Status,
			Category:    health.CategoryDependency,
			Level:       health.LevelCritical,
			Description: "Service " + serviceName,
			Timestamp:   time.Now(),
		}

		report.CheckResults = append(report.CheckResults, result)

		// 统计服务状态
		switch serviceReport.Status {
		case health.StatusUp:
			upCount++
		case health.StatusDegraded:
			degradedCount++
		case health.StatusDown:
			downCount++
		}
	}

	// 添加统计信息到元数据
	report.Metadata["service_count"] = len(reports)
	report.Metadata["services_up"] = upCount
	report.Metadata["services_degraded"] = degradedCount
	report.Metadata["services_down"] = downCount

	// 确定整体系统状态
	if downCount > 0 {
		report.Status = health.StatusDegraded
		if downCount == len(reports) {
			report.Status = health.StatusDown
		}
	} else if degradedCount > 0 {
		report.Status = health.StatusDegraded
	} else if upCount == len(reports) && upCount > 0 {
		report.Status = health.StatusUp
	} else {
		report.Status = health.StatusUnknown
	}

	return report
}

func (sm *ServiceManager) StartAll(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_all_services")
	logger.InfoWithContext(ctx, "Starting all services")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.services) == 0 {
		logger.ErrorWithContext(ctx, "No services registered to start")
		return errors.New(errors.TypeInvalidInput, "No services registered to start", nil)
	}

	var startErrors []string
	wg := sync.WaitGroup{}

	for _, svc := range sm.services {
		wg.Add(1)
		go func(s service.Service) {
			defer wg.Done()
			// Create service-specific context
			serviceCtx := appctx.WithServiceName(ctx, s.GetName())

			if err := sm.startService(serviceCtx, s); err != nil {
				logger.WithContextError(serviceCtx, err).Errorf("Failed to start service")
				startErrors = append(startErrors, s.GetName())
			}
		}(svc)
	}

	wg.Wait()

	if len(startErrors) > 0 {
		logger.ErrorfWithContext(ctx, "Failed to start %d services", len(startErrors))
		return errors.New(errors.TypeServiceUnavailable, "Failed to start some services", nil).
			WithField("failed_services", startErrors)
	}

	logger.InfoWithContext(ctx, "All services started successfully")
	return nil
}

func (sm *ServiceManager) startService(ctx context.Context, s service.Service) error {
	ctx = appctx.WithOperationName(ctx, "start_service")

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	return logger.LogTimingOperation(ctx, "service_start", func(opCtx context.Context) error {
		select {
		case <-opCtx.Done():
			return errors.Wrap(opCtx.Err(), "Context cancelled before service could start", errors.TypeTimeout)
		default:
			if err := s.Start(opCtx); err != nil {
				return errors.Wrap(err, "Failed to start service", errors.TypeServiceUnavailable).
					WithField("service_name", s.GetName())
			}
			logger.DebugfWithContext(opCtx, "Service %s started successfully", s.GetName())
			return nil
		}
	})
}

// GetServiceByName gets a service by name
func (sm *ServiceManager) GetServiceByName(ctx context.Context, name string) (service.Service, bool) {
	ctx = appctx.WithOperationName(ctx, "get_service_by_name")

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	svc, exists := sm.services[name]
	if !exists {
		return nil, false
	}

	if !svc.IsRunning(ctx) {
		logger.DebugfWithContext(ctx, "Service %s exists but is not running", name)
	}

	return svc, true
}

// GetServicesByWorkflowAndType gets services by workflow and type
func (sm *ServiceManager) GetServicesByWorkflowAndType(ctx context.Context, workflow string, serviceType string) []service.Service {
	ctx = appctx.WithOperationName(ctx, "get_services_by_workflow_and_type")

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []service.Service

	for _, s := range sm.services {
		if s.GetWorkflow() == workflow && s.GetType() == serviceType {
			result = append(result, s)
		}
	}

	logger.DebugfWithContext(ctx, "Found %d services with workflow=%s and type=%s", len(result), workflow, serviceType)
	return result
}

// MonitorServices continuously monitors services and restarts them if needed
func (sm *ServiceManager) MonitorServices(ctx context.Context) {
	ctx = appctx.WithOperationName(ctx, "monitor_services")
	logger.InfoWithContext(ctx, "Starting service monitoring")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.InfoWithContext(ctx, "Service monitoring stopped due to context cancellation")
			return
		case <-ticker.C:
			sm.checkServices(ctx)
		}
	}
}

// checkServices checks all services and restarts any that are down
func (sm *ServiceManager) checkServices(ctx context.Context) {
	ctx = appctx.WithOperationName(ctx, "check_services")
	logger.DebugWithContext(ctx, "Checking service health")

	sm.mu.RLock()
	serviceNames := make([]string, 0, len(sm.services))
	for name := range sm.services {
		serviceNames = append(serviceNames, name)
	}
	sm.mu.RUnlock()

	for _, serviceName := range serviceNames {
		serviceCtx := appctx.WithServiceName(ctx, serviceName)

		sm.mu.RLock()
		service, exists := sm.services[serviceName]
		sm.mu.RUnlock()

		if !exists {
			continue
		}

		// First try to get health report
		report, found := sm.GetServiceHealth(serviceCtx, serviceName)
		if found {
			if report.Status == health.StatusDown || report.Status == health.StatusDegraded {
				logger.WarnfWithContext(serviceCtx, "Service %s is in %s state, attempting to restart...",
					serviceName, report.Status)

				if err := service.Restart(serviceCtx); err != nil {
					logger.WithContextError(serviceCtx, err).Error("Failed to restart service")
				} else {
					logger.InfofWithContext(serviceCtx, "Successfully restarted service %s", serviceName)
				}
			}
		} else if !service.IsRunning(serviceCtx) {
			// Fallback to basic IsRunning check if no health report
			logger.WarnfWithContext(serviceCtx, "Service %s is not running, attempting to restart...", serviceName)

			if err := service.Restart(serviceCtx); err != nil {
				logger.WithContextError(serviceCtx, err).Error("Failed to restart service")

				// Log detailed error information using our custom error type
				if appErr, ok := err.(*errors.AppError); ok {
					logger.FromContext(serviceCtx).
						WithField("error_type", appErr.Type).
						WithField("error_fields", appErr.Fields).
						Error("Detailed restart error")
				}
			} else {
				logger.InfofWithContext(serviceCtx, "Successfully restarted service %s", serviceName)
			}
		}
	}
}

// StopAll stops all registered services with proper error handling
func (sm *ServiceManager) StopAll(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_all_services")
	logger.InfoWithContext(ctx, "Stopping all services")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	var stopErrors []string
	wg := sync.WaitGroup{}

	for _, svc := range sm.services {
		wg.Add(1)
		go func(s service.Service) {
			defer wg.Done()
			serviceName := s.GetName()
			serviceCtx := appctx.WithServiceName(ctx, serviceName)

			logger.InfofWithContext(serviceCtx, "Stopping service: %s", serviceName)

			if err := s.Stop(serviceCtx); err != nil {
				logger.WithContextError(serviceCtx, err).Error("Failed to stop service")
				stopErrors = append(stopErrors, serviceName)
			} else {
				logger.DebugfWithContext(serviceCtx, "Service %s stopped successfully", serviceName)
			}
		}(svc)
	}

	wg.Wait()

	if len(stopErrors) > 0 {
		logger.ErrorfWithContext(ctx, "Failed to stop %d services", len(stopErrors))
		return errors.New(errors.TypeServiceUnavailable, "Failed to stop some services", nil).
			WithField("failed_services", stopErrors)
	}

	logger.InfoWithContext(ctx, "All services stopped successfully")
	return nil
}

// ListServices 返回所有注册的服务
func (sm *ServiceManager) ListServices(ctx context.Context) []service.Service {
	ctx = appctx.WithOperationName(ctx, "list_services")
	logger.DebugWithContext(ctx, "Listing all services")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	services := make([]service.Service, 0, len(sm.services))
	for _, s := range sm.services {
		services = append(services, s)
	}

	return services
}

// 其他方法，例如根据工作流和类型获取服务
