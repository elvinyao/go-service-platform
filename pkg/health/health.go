package health

import (
	"context"
	"time"
)

// Status represents the health status of a service or component
type Status string

const (
	// StatusUp indicates the service is healthy and operational
	StatusUp Status = "UP"

	// StatusDown indicates the service is completely non-operational
	StatusDown Status = "DOWN"

	// StatusDegraded indicates the service is operational but with reduced capabilities
	StatusDegraded Status = "DEGRADED"

	// StatusUnknown indicates the health status could not be determined
	StatusUnknown Status = "UNKNOWN"
)

// Level represents the importance of a health check
type Level string

const (
	// LevelCritical indicates a critical health check that must pass for the service to be considered healthy
	LevelCritical Level = "CRITICAL"

	// LevelWarning indicates a health check that if failed may degrade service performance
	LevelWarning Level = "WARNING"

	// LevelInfo indicates an informational health check
	LevelInfo Level = "INFO"
)

// Category represents the type of check
type Category string

const (
	// CategoryConnectivity checks external connectivity
	CategoryConnectivity Category = "CONNECTIVITY"

	// CategoryResources checks resource availability
	CategoryResources Category = "RESOURCES"

	// CategoryDependency checks dependencies
	CategoryDependency Category = "DEPENDENCY"

	// CategoryPerformance checks performance metrics
	CategoryPerformance Category = "PERFORMANCE"

	// CategoryData checks data availability and integrity
	CategoryData Category = "DATA"

	// CategorySecurity checks security features
	CategorySecurity Category = "SECURITY"
)

// CheckResult represents the result of a single health check
type CheckResult struct {
	Name        string            `json:"name"`
	Status      Status            `json:"status"`
	Description string            `json:"description"`
	Category    Category          `json:"category"`
	Level       Level             `json:"level"`
	Details     map[string]string `json:"details,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Duration    time.Duration     `json:"duration"`
}

// Report represents the overall health of a service
type Report struct {
	ServiceName    string                 `json:"service_name"`
	Status         Status                 `json:"status"`
	CheckResults   []CheckResult          `json:"check_results"`
	StartTime      time.Time              `json:"start_time"`
	Uptime         time.Duration          `json:"uptime"`
	Version        string                 `json:"version,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	RefreshedAt    time.Time              `json:"refreshed_at"`
	RefreshElapsed time.Duration          `json:"refresh_elapsed"`
}

// Checker defines the interface for components that can perform health checks
type Checker interface {
	// Check performs the health check and returns the result
	Check(ctx context.Context) CheckResult
}

// Reporter defines the interface for services that can report their health
type Reporter interface {
	// HealthCheck performs all health checks and returns a consolidated report
	HealthCheck(ctx context.Context) Report
}

// DefaultThresholds defines default thresholds for various metrics
var DefaultThresholds = struct {
	ResponseTimeMs   int64
	CPUUsagePercent  float64
	MemUsagePercent  float64
	ErrorRatePercent float64
	MinConnections   int
}{
	ResponseTimeMs:   500,  // 500ms
	CPUUsagePercent:  80.0, // 80%
	MemUsagePercent:  80.0, // 80%
	ErrorRatePercent: 5.0,  // 5%
	MinConnections:   1,    // At least 1 connection
}

// DetermineStatus determines the overall status based on check results
func DetermineStatus(results []CheckResult) Status {
	// If no results, status is unknown
	if len(results) == 0 {
		return StatusUnknown
	}

	hasDown := false
	hasDegraded := false
	hasUnknown := false

	for _, result := range results {
		// If any critical check is down, the service is down
		if result.Level == LevelCritical && result.Status == StatusDown {
			return StatusDown
		}

		if result.Status == StatusDown {
			hasDown = true
		} else if result.Status == StatusDegraded {
			hasDegraded = true
		} else if result.Status == StatusUnknown {
			hasUnknown = true
		}
	}

	// Determine overall status based on check results
	if hasDown {
		return StatusDegraded
	} else if hasDegraded {
		return StatusDegraded
	} else if hasUnknown {
		return StatusDegraded
	}

	return StatusUp
}

// NewCheckResult creates a new CheckResult with standard fields set
func NewCheckResult(name string, category Category, level Level) CheckResult {
	return CheckResult{
		Name:      name,
		Category:  category,
		Level:     level,
		Status:    StatusUnknown,
		Timestamp: time.Now(),
		Details:   make(map[string]string),
	}
}

// SetStatus updates the status and optionally adds a description
func (cr *CheckResult) SetStatus(status Status, description string) {
	cr.Status = status
	if description != "" {
		cr.Description = description
	}
}

// AddDetail adds a key-value detail to the check result
func (cr *CheckResult) AddDetail(key, value string) {
	if cr.Details == nil {
		cr.Details = make(map[string]string)
	}
	cr.Details[key] = value
}

// Complete marks the check as complete and sets the duration
func (cr *CheckResult) Complete() {
	cr.Duration = time.Since(cr.Timestamp)
}

// IsHealthy returns true if the status is UP
func (cr *CheckResult) IsHealthy() bool {
	return cr.Status == StatusUp
}

// NewReport creates a new health report for a service
func NewReport(serviceName string, startTime time.Time, version string) Report {
	now := time.Now()
	return Report{
		ServiceName:  serviceName,
		Status:       StatusUnknown,
		CheckResults: []CheckResult{},
		StartTime:    startTime,
		Uptime:       now.Sub(startTime),
		Version:      version,
		Metadata:     make(map[string]interface{}),
		RefreshedAt:  now,
	}
}

// AddResult adds a check result to the report
func (r *Report) AddResult(result CheckResult) {
	r.CheckResults = append(r.CheckResults, result)
	r.Status = DetermineStatus(r.CheckResults)
}

// Complete finalizes the report with timing information
func (r *Report) Complete() {
	now := time.Now()
	r.RefreshElapsed = now.Sub(r.RefreshedAt)
	r.RefreshedAt = now
	r.Uptime = now.Sub(r.StartTime)
}

// IsHealthy returns true if the overall status is UP
func (r *Report) IsHealthy() bool {
	return r.Status == StatusUp
}

// RunChecksParallel runs all checkers in parallel and collects results
func RunChecksParallel(ctx context.Context, checkers []Checker) []CheckResult {
	results := make([]CheckResult, len(checkers))

	// Create a channel to collect results
	resultCh := make(chan struct {
		index  int
		result CheckResult
	})

	// Run each check in its own goroutine
	for i, checker := range checkers {
		go func(idx int, chk Checker) {
			result := chk.Check(ctx)
			resultCh <- struct {
				index  int
				result CheckResult
			}{idx, result}
		}(i, checker)
	}

	// Collect all results
	for i := 0; i < len(checkers); i++ {
		select {
		case <-ctx.Done():
			// Context was cancelled, mark remaining checks as unknown
			res := NewCheckResult("cancelled", CategoryConnectivity, LevelCritical)
			res.SetStatus(StatusUnknown, "Health check was cancelled")
			res.Complete()
			return []CheckResult{res}
		case r := <-resultCh:
			results[r.index] = r.result
		}
	}

	return results
}
