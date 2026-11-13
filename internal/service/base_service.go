package service

import (
	"context"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"sync"
	"time"
)

// LifecycleState represents the explicit operational state of a Service.
type LifecycleState string

const (
	// ServiceStateNew indicates the service has never been started.
	ServiceStateNew LifecycleState = "new"
	// ServiceStateStarting indicates the service is initializing.
	ServiceStateStarting LifecycleState = "starting"
	// ServiceStateRunning indicates the service is active.
	ServiceStateRunning LifecycleState = "running"
	// ServiceStateStopping indicates the service is draining resources.
	ServiceStateStopping LifecycleState = "stopping"
	// ServiceStateStopped indicates the service has been stopped after running.
	ServiceStateStopped LifecycleState = "stopped"
)

// BaseService provides common functionality for all services
type BaseService struct {
	name           string
	workflow       string
	serviceType    string
	state          LifecycleState
	mu             sync.RWMutex
	startTime      time.Time
	stopTime       time.Time
	version        string
	customCheckers []health.Checker
}

// NewBaseService creates a new base service
func NewBaseService(name, workflow, serviceType, version string) *BaseService {
	return &BaseService{
		name:           name,
		workflow:       workflow,
		serviceType:    serviceType,
		state:          ServiceStateNew,
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

// State returns the current explicit lifecycle state of the service.
func (s *BaseService) State() LifecycleState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// TransitionState updates the explicit lifecycle state and records start/stop timestamps.
func (s *BaseService) TransitionState(next LifecycleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if next == ServiceStateRunning && s.state != ServiceStateRunning {
		s.startTime = now
		s.stopTime = time.Time{}
	} else if next == ServiceStateStopped && s.state != ServiceStateStopped && !s.startTime.IsZero() {
		s.stopTime = now
	}
	s.state = next
}

// IsRunning returns whether the service is running
func (s *BaseService) IsRunning(ctx context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == ServiceStateRunning
}

func (s *BaseService) setRunning(r bool) {
	if r {
		s.TransitionState(ServiceStateRunning)
		return
	}
	s.TransitionState(ServiceStateStopped)
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

	running := s.state == ServiceStateRunning
	metrics := make(map[string]interface{})
	metrics["running"] = running
	metrics["state"] = string(s.state)
	metrics["type"] = s.serviceType
	metrics["workflow"] = s.workflow

	if !s.startTime.IsZero() {
		end := time.Now()
		if !running && !s.stopTime.IsZero() {
			end = s.stopTime
		}
		metrics["uptime_seconds"] = end.Sub(s.startTime).Seconds()
		metrics["start_time"] = s.startTime.Format(time.RFC3339)
	}

	return metrics
}
