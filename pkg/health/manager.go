package health

import (
	"context"
	"sync"
	"time"
)

// HealthManager manages health checks across multiple services
type HealthManager struct {
	reporters     map[string]Reporter
	cachedReports map[string]Report
	cacheTTL      time.Duration
	mu            sync.RWMutex
	startTime     time.Time
	version       string
}

// NewHealthManager creates a new health manager
func NewHealthManager(cacheTTL time.Duration, version string) *HealthManager {
	if cacheTTL <= 0 {
		cacheTTL = 30 * time.Second // Default TTL
	}

	return &HealthManager{
		reporters:     make(map[string]Reporter),
		cachedReports: make(map[string]Report),
		cacheTTL:      cacheTTL,
		startTime:     time.Now(),
		version:       version,
	}
}

// RegisterReporter registers a service for health checks
func (m *HealthManager) RegisterReporter(serviceName string, reporter Reporter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reporters[serviceName] = reporter
}

// UnregisterReporter removes a service from health checks
func (m *HealthManager) UnregisterReporter(serviceName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reporters, serviceName)
	delete(m.cachedReports, serviceName)
}

// GetServiceHealth gets the health of a specific service
func (m *HealthManager) GetServiceHealth(ctx context.Context, serviceName string) (Report, bool) {
	m.mu.RLock()
	reporter, exists := m.reporters[serviceName]
	if !exists {
		m.mu.RUnlock()
		return Report{}, false
	}

	// Check if we have a cached report that's still valid
	cachedReport, hasCached := m.cachedReports[serviceName]
	m.mu.RUnlock()

	if hasCached && time.Since(cachedReport.RefreshedAt) < m.cacheTTL {
		return cachedReport, true
	}

	// Cache miss or expired, get a fresh report
	report := reporter.HealthCheck(ctx)

	// Cache the new report
	m.mu.Lock()
	m.cachedReports[serviceName] = report
	m.mu.Unlock()

	return report, true
}

// GetAllServicesHealth gets the health of all registered services
func (m *HealthManager) GetAllServicesHealth(ctx context.Context) map[string]Report {
	m.mu.RLock()
	serviceNames := make([]string, 0, len(m.reporters))
	for name := range m.reporters {
		serviceNames = append(serviceNames, name)
	}
	m.mu.RUnlock()

	result := make(map[string]Report)
	for _, name := range serviceNames {
		if report, exists := m.GetServiceHealth(ctx, name); exists {
			result[name] = report
		}
	}

	return result
}

// GetSystemHealth gets the overall system health
func (m *HealthManager) GetSystemHealth(ctx context.Context) Report {
	servicesHealth := m.GetAllServicesHealth(ctx)

	systemReport := NewReport("system", m.startTime, m.version)

	// Track overall counts
	var upCount, degradedCount, downCount int

	// Process each service's health
	for serviceName, report := range servicesHealth {
		// Add service status to report
		serviceResult := NewCheckResult(
			"service-"+serviceName,
			CategoryDependency,
			LevelCritical,
		)

		serviceResult.SetStatus(report.Status, "Service "+serviceName+" status: "+string(report.Status))
		systemReport.AddResult(serviceResult)

		// Track counts for system status determination
		switch report.Status {
		case StatusUp:
			upCount++
		case StatusDegraded:
			degradedCount++
		case StatusDown:
			downCount++
		}
	}

	// Add counts to metadata
	if systemReport.Metadata == nil {
		systemReport.Metadata = make(map[string]interface{})
	}
	systemReport.Metadata["service_count"] = len(servicesHealth)
	systemReport.Metadata["services_up"] = upCount
	systemReport.Metadata["services_degraded"] = degradedCount
	systemReport.Metadata["services_down"] = downCount

	// Determine overall system status
	if downCount > 0 {
		systemReport.Status = StatusDegraded
		if downCount == len(servicesHealth) {
			systemReport.Status = StatusDown
		}
	} else if degradedCount > 0 {
		systemReport.Status = StatusDegraded
	} else if upCount == len(servicesHealth) && upCount > 0 {
		systemReport.Status = StatusUp
	} else {
		systemReport.Status = StatusUnknown
	}

	systemReport.Complete()
	return systemReport
}

// RefreshCache forces a refresh of all cached health reports
func (m *HealthManager) RefreshCache(ctx context.Context) {
	m.mu.RLock()
	serviceNames := make([]string, 0, len(m.reporters))
	for name := range m.reporters {
		serviceNames = append(serviceNames, name)
	}
	m.mu.RUnlock()

	// Refresh all services in parallel
	var wg sync.WaitGroup
	for _, name := range serviceNames {
		wg.Add(1)
		go func(serviceName string) {
			defer wg.Done()
			m.mu.RLock()
			reporter, exists := m.reporters[serviceName]
			m.mu.RUnlock()

			if exists {
				report := reporter.HealthCheck(ctx)

				m.mu.Lock()
				m.cachedReports[serviceName] = report
				m.mu.Unlock()
			}
		}(name)
	}

	wg.Wait()
}
