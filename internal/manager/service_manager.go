package manager

import (
	"context"
	"project/internal/interfaces"
	"project/internal/service"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
	"sync"
	"time"
)

type ServiceManager struct {
	services map[string]service.Service
	mu       sync.Mutex
}

// 确保 ServiceManager 实现了 interfaces.ServiceManager 接口
var _ interfaces.ServiceManager = (*ServiceManager)(nil)

func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		services: make(map[string]service.Service),
	}
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

// GetServiceByName returns a service by name
func (sm *ServiceManager) GetServiceByName(ctx context.Context, name string) (service.Service, bool) {
	ctx = appctx.WithOperationName(ctx, "get_service_by_name")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if name == "" {
		logger.ErrorWithContext(ctx, "Attempted to get service with empty name")
		return nil, false
	}

	s, ok := sm.services[name]
	if !ok {
		logger.DebugfWithContext(ctx, "Service not found: %s", name)
	} else {
		logger.DebugfWithContext(ctx, "Service found: %s", name)
	}
	return s, ok
}

// GetServicesByWorkflowAndType returns services by workflow and type
func (sm *ServiceManager) GetServicesByWorkflowAndType(ctx context.Context, workflow string, serviceType string) []service.Service {
	ctx = appctx.WithOperationName(ctx, "get_services_by_workflow_and_type")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if workflow == "" || serviceType == "" {
		logger.ErrorWithContext(ctx, "Invalid workflow or service type")
		return nil
	}

	var result []service.Service
	for _, s := range sm.services {
		if s.GetWorkflow() == workflow && s.GetType() == serviceType {
			result = append(result, s)
		}
	}

	if len(result) == 0 {
		logger.DebugfWithContext(ctx, "No services found for workflow=%s, type=%s", workflow, serviceType)
	} else {
		logger.DebugfWithContext(ctx, "Found %d services for workflow=%s, type=%s", len(result), workflow, serviceType)
	}

	return result
}

func (sm *ServiceManager) MonitorServices(ctx context.Context) {
	ctx = appctx.WithOperationName(ctx, "monitor_services")
	logger.InfoWithContext(ctx, "Starting service monitoring")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.InfoWithContext(ctx, "Service monitoring stopped: context cancelled")
			return
		case <-ticker.C:
			sm.checkServiceHealth(ctx)
		}
	}
}

func (sm *ServiceManager) checkServiceHealth(ctx context.Context) {
	monitorCtx := appctx.WithOperationName(ctx, "check_service_health")

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, service := range sm.services {
		serviceName := service.GetName()
		serviceCtx := appctx.WithServiceName(monitorCtx, serviceName)

		if !service.IsRunning() {
			logger.WarnfWithContext(serviceCtx, "Service %s is not running, attempting to restart...", serviceName)

			if err := service.Restart(serviceCtx); err != nil {
				logger.WithContextError(serviceCtx, err).Error("Failed to restart service")

				// Log detailed error information using our custom error type
				if appErr, ok := err.(*errors.AppError); ok {
					logger.FromContext(serviceCtx).
						WithField("error_type", appErr.Type).
						WithField("error_fields", appErr.Fields).
						Errorf("Service restart error details")
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

// 其他方法，例如根据工作流和类型获取服务
