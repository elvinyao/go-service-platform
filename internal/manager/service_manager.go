package manager

import (
	"context"
	"github.com/elvinyao/go-service-platform/internal/service"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"sort"
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

func NewServiceManager(version string, healthManagers ...*health.HealthManager) *ServiceManager {
	healthManager := health.NewHealthManager(30*time.Second, version)
	if len(healthManagers) > 0 && healthManagers[0] != nil {
		healthManager = healthManagers[0]
	}
	manager := &ServiceManager{
		services:  make(map[string]service.Service),
		version:   version,
		startTime: time.Now(),
		healthMgr: healthManager,
	}

	return manager
}

func (sm *ServiceManager) RegisterService(s service.Service) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if isNilRegistration(s) {
		return errors.New(errors.TypeInvalidInput, "Cannot register nil service", nil)
	}

	serviceName := s.GetName()
	if serviceName == "" {
		return errors.New(errors.TypeInvalidInput, "Cannot register service with empty name", nil)
	}

	if _, exists := sm.services[serviceName]; exists {
		return errors.New(errors.TypeInvalidInput, "Service is already registered", nil).
			WithField("service_name", serviceName)
	}

	sm.services[serviceName] = s

	// Register the service with the health check manager
	sm.healthMgr.RegisterService(s)
	return nil
}

// GetServiceHealth retrieves health information for a specific service
func (sm *ServiceManager) GetServiceHealth(ctx context.Context, serviceName string) (health.Report, bool) {
	ctx = appctx.WithOperationName(ctx, "get_service_health")

	// Create service health report
	report := health.Report{
		ServiceName: serviceName,
		StartTime:   sm.startTime,
		Version:     sm.version,
		RefreshedAt: time.Now(),
	}

	// Get service
	sm.mu.RLock()
	svc, exists := sm.services[serviceName]
	sm.mu.RUnlock()

	if !exists {
		return report, false
	}

	// Add service health check results
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

// GetAllServicesHealth retrieves health information for all services
func (sm *ServiceManager) GetAllServicesHealth(ctx context.Context) map[string]health.Report {
	ctx = appctx.WithOperationName(ctx, "get_all_services_health")

	result := make(map[string]health.Report)

	sm.mu.RLock()
	serviceNames := make([]string, 0, len(sm.services))
	for name := range sm.services {
		serviceNames = append(serviceNames, name)
	}
	sm.mu.RUnlock()

	for _, name := range serviceNames {
		if report, exists := sm.GetServiceHealth(ctx, name); exists {
			result[name] = report
		}
	}

	return result
}

// GetSystemHealth retrieves overall system health status
func (sm *ServiceManager) GetSystemHealth(ctx context.Context) health.Report {
	ctx = appctx.WithOperationName(ctx, "get_system_health")

	reports := sm.GetAllServicesHealth(ctx)

	// Create system health report
	report := health.Report{
		ServiceName: "system",
		Status:      health.StatusUp,
		StartTime:   sm.startTime,
		Version:     sm.version,
		RefreshedAt: time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	// Process service health status
	var upCount, degradedCount, downCount int

	for serviceName, serviceReport := range reports {
		// Add service status to report
		result := health.CheckResult{
			Name:        "service." + serviceName,
			Status:      serviceReport.Status,
			Category:    health.CategoryDependency,
			Level:       health.LevelCritical,
			Description: "Service " + serviceName,
			Timestamp:   time.Now(),
		}

		report.CheckResults = append(report.CheckResults, result)

		// Count service status
		switch serviceReport.Status {
		case health.StatusUp:
			upCount++
		case health.StatusDegraded:
			degradedCount++
		case health.StatusDown:
			downCount++
		}
	}

	// Add statistics to metadata
	report.Metadata["service_count"] = len(reports)
	report.Metadata["services_up"] = upCount
	report.Metadata["services_degraded"] = degradedCount
	report.Metadata["services_down"] = downCount

	// Determine overall system status
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

	sm.mu.RLock()
	services := make([]service.Service, 0, len(sm.services))
	for _, svc := range sm.services {
		services = append(services, svc)
	}
	sm.mu.RUnlock()

	if len(services) == 0 {
		logger.ErrorWithContext(ctx, "No services registered to start")
		return errors.New(errors.TypeInvalidInput, "No services registered to start", nil)
	}

	type startResult struct {
		service       service.Service
		err           error
		startedByCall bool
	}
	results := make(chan startResult, len(services))
	wg := sync.WaitGroup{}

	for _, svc := range services {
		wg.Add(1)
		go func(s service.Service) {
			defer wg.Done()
			serviceCtx := appctx.WithServiceName(ctx, s.GetName())
			wasRunning := s.IsRunning(serviceCtx)
			err := sm.startService(serviceCtx, s)
			startedByCall := !wasRunning && s.IsRunning(context.WithoutCancel(serviceCtx))
			if err != nil {
				logger.WithContextError(serviceCtx, err).Errorf("Failed to start service")
			}
			results <- startResult{service: s, err: err, startedByCall: startedByCall}
		}(svc)
	}

	wg.Wait()
	close(results)

	var startErrors []string
	var startedServices []service.Service
	for result := range results {
		if result.err != nil {
			startErrors = append(startErrors, result.service.GetName())
		}
		if result.startedByCall {
			startedServices = append(startedServices, result.service)
		}
	}

	if len(startErrors) > 0 {
		sort.Strings(startErrors)
		rollbackErrors := rollbackStartedServices(ctx, startedServices)
		sort.Strings(rollbackErrors)
		logger.ErrorfWithContext(ctx, "Failed to start %d services", len(startErrors))
		err := errors.New(errors.TypeServiceUnavailable, "Failed to start some services", nil).
			WithField("failed_services", startErrors)
		if len(rollbackErrors) > 0 {
			err.WithField("rollback_failures", rollbackErrors)
		}
		return err
	}

	logger.InfoWithContext(ctx, "All services started successfully")
	return nil
}

func rollbackStartedServices(ctx context.Context, services []service.Service) []string {
	if len(services) == 0 {
		return nil
	}

	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	var rollbackErrors []string
	var errMu sync.Mutex
	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		go func(s service.Service) {
			defer wg.Done()
			serviceCtx := appctx.WithServiceName(rollbackCtx, s.GetName())
			if err := s.Stop(serviceCtx); err != nil {
				logger.WithContextError(serviceCtx, err).Error("Failed to roll back started service")
				errMu.Lock()
				rollbackErrors = append(rollbackErrors, s.GetName())
				errMu.Unlock()
			}
		}(svc)
	}
	wg.Wait()
	return rollbackErrors
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
	sort.Slice(result, func(i, j int) bool {
		return result[i].GetName() < result[j].GetName()
	})

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

	sm.mu.RLock()
	services := make([]service.Service, 0, len(sm.services))
	for _, svc := range sm.services {
		services = append(services, svc)
	}
	sm.mu.RUnlock()

	var stopErrors []string
	var errMu sync.Mutex
	wg := sync.WaitGroup{}

	for _, svc := range services {
		wg.Add(1)
		go func(s service.Service) {
			defer wg.Done()
			serviceName := s.GetName()
			serviceCtx := appctx.WithServiceName(ctx, serviceName)

			logger.InfofWithContext(serviceCtx, "Stopping service: %s", serviceName)

			if err := s.Stop(serviceCtx); err != nil {
				logger.WithContextError(serviceCtx, err).Error("Failed to stop service")
				errMu.Lock()
				stopErrors = append(stopErrors, serviceName)
				errMu.Unlock()
			} else {
				logger.DebugfWithContext(serviceCtx, "Service %s stopped successfully", serviceName)
			}
		}(svc)
	}

	wg.Wait()

	if len(stopErrors) > 0 {
		sort.Strings(stopErrors)
		logger.ErrorfWithContext(ctx, "Failed to stop %d services", len(stopErrors))
		return errors.New(errors.TypeServiceUnavailable, "Failed to stop some services", nil).
			WithField("failed_services", stopErrors)
	}

	logger.InfoWithContext(ctx, "All services stopped successfully")
	return nil
}

// ListServices returns all registered services
func (sm *ServiceManager) ListServices(ctx context.Context) []service.Service {
	ctx = appctx.WithOperationName(ctx, "list_services")
	logger.DebugWithContext(ctx, "Listing all services")

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	services := make([]service.Service, 0, len(sm.services))
	for _, s := range sm.services {
		services = append(services, s)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].GetName() < services[j].GetName()
	})

	return services
}

// Other methods, such as getting services by workflow and type
