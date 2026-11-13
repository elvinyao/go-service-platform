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

// ManagerState represents the explicit lifecycle state of ServiceManager.
type ManagerState string

const (
	ManagerStateIdle           ManagerState = "idle"
	ManagerStateStarting       ManagerState = "starting"
	ManagerStateRunning        ManagerState = "running"
	ManagerStatePartialFailure ManagerState = "partial_failure"
	ManagerStateStopping       ManagerState = "stopping"
	ManagerStateStopped        ManagerState = "stopped"
)

type ServiceManager struct {
	services  map[string]service.Service
	mu        sync.RWMutex
	state     ManagerState
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
		state:     ManagerStateIdle,
		version:   version,
		startTime: time.Now(),
		healthMgr: healthManager,
	}

	return manager
}

// State returns the current explicit lifecycle state of the ServiceManager.
func (sm *ServiceManager) State() ManagerState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *ServiceManager) setState(next ManagerState) {
	sm.mu.Lock()
	sm.state = next
	sm.mu.Unlock()
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

func (sm *ServiceManager) StartAll(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_all_services")
	logger.InfoWithContext(ctx, "Starting all services")

	sm.mu.Lock()
	services := make([]service.Service, 0, len(sm.services))
	for _, svc := range sm.services {
		services = append(services, svc)
	}
	if len(services) == 0 {
		sm.mu.Unlock()
		logger.ErrorWithContext(ctx, "No services registered to start")
		return errors.New(errors.TypeInvalidInput, "No services registered to start", nil)
	}
	sm.state = ManagerStateStarting
	sm.mu.Unlock()

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
		if len(rollbackErrors) > 0 {
			sm.setState(ManagerStatePartialFailure)
		} else {
			sm.setState(ManagerStateStopped)
		}
		logger.ErrorfWithContext(ctx, "Failed to start %d services", len(startErrors))
		err := errors.New(errors.TypeServiceUnavailable, "Failed to start some services", nil).
			WithField("failed_services", startErrors)
		if len(rollbackErrors) > 0 {
			err.WithField("rollback_failures", rollbackErrors)
		}
		return err
	}

	sm.setState(ManagerStateRunning)
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

// checkServices checks all services and restarts any that are down when supervision is active.
func (sm *ServiceManager) checkServices(ctx context.Context) {
	ctx = appctx.WithOperationName(ctx, "check_services")
	logger.DebugWithContext(ctx, "Checking service health")

	sm.mu.RLock()
	switch sm.state {
	case ManagerStateStarting, ManagerStateStopping, ManagerStateStopped:
		sm.mu.RUnlock()
		return
	}
	serviceNames := make([]string, 0, len(sm.services))
	for name := range sm.services {
		serviceNames = append(serviceNames, name)
	}
	sm.mu.RUnlock()
	sort.Strings(serviceNames)

	for _, serviceName := range serviceNames {
		serviceCtx := appctx.WithServiceName(ctx, serviceName)

		sm.mu.RLock()
		if sm.state == ManagerStateStarting || sm.state == ManagerStateStopping || sm.state == ManagerStateStopped {
			sm.mu.RUnlock()
			return
		}
		svc, exists := sm.services[serviceName]
		sm.mu.RUnlock()

		if !exists || svc.IsRunning(serviceCtx) {
			continue
		}

		logger.WarnfWithContext(serviceCtx, "Service %s is not running, attempting to restart...", serviceName)
		if err := svc.Restart(serviceCtx); err != nil {
			logger.WithContextError(serviceCtx, err).Error("Failed to restart service")
		} else {
			logger.InfofWithContext(serviceCtx, "Successfully restarted service %s", serviceName)
		}
	}
}

// StopAll stops all registered services with proper error handling
func (sm *ServiceManager) StopAll(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_all_services")
	logger.InfoWithContext(ctx, "Stopping all services")

	sm.mu.Lock()
	sm.state = ManagerStateStopping
	services := make([]service.Service, 0, len(sm.services))
	for _, svc := range sm.services {
		services = append(services, svc)
	}
	sm.mu.Unlock()

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
		sm.setState(ManagerStatePartialFailure)
		logger.ErrorfWithContext(ctx, "Failed to stop %d services", len(stopErrors))
		return errors.New(errors.TypeServiceUnavailable, "Failed to stop some services", nil).
			WithField("failed_services", stopErrors)
	}

	sm.setState(ManagerStateStopped)
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
