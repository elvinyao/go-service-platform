package service

import (
	"context"
	"github.com/elvinyao/go-service-platform/pkg/health"
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
