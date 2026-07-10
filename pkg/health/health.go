package health

import (
	"context"
	"fmt"
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

// ServiceChecker defines the interface for services that can be health checked
type ServiceChecker interface {
	// IsRunning returns whether the service is running
	IsRunning(ctx context.Context) bool

	// GetName returns the service name
	GetName() string

	// GetMetrics returns service metrics
	GetMetrics(ctx context.Context) map[string]interface{}
}

// ServiceHealthProvider exposes service-specific health checks.
type ServiceHealthProvider interface {
	RegisterHealthChecks() []Checker
}

// CheckResult represents the result of a single health check
type CheckResult struct {
	Name        string                 `json:"name"`
	Status      Status                 `json:"status"`
	Description string                 `json:"description"`
	Category    Category               `json:"category"`
	Level       Level                  `json:"level"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
	Duration    time.Duration          `json:"duration"`
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
	Check(ctx context.Context) *CheckResult
}

// Reporter contributes health check results to a report.
type Reporter interface {
	// ReportHealth runs synchronously and must return after updating the report.
	ReportHealth(ctx context.Context, report *Report)
}

// DefaultThresholds defines default thresholds for various metrics
var DefaultThresholds = struct {
	// CPU thresholds
	CPUWarning  float64
	CPUCritical float64

	// Memory thresholds
	MemoryWarning  float64
	MemoryCritical float64

	// Disk thresholds
	DiskWarning  float64
	DiskCritical float64

	// Response time thresholds (ms)
	ResponseTimeWarning  int64
	ResponseTimeCritical int64

	// Error rate thresholds (%)
	ErrorRateWarning  float64
	ErrorRateCritical float64
}{
	CPUWarning:  0.7, // 70%
	CPUCritical: 0.9, // 90%

	MemoryWarning:  0.8,  // 80%
	MemoryCritical: 0.95, // 95%

	DiskWarning:  0.8,  // 80%
	DiskCritical: 0.95, // 95%

	ResponseTimeWarning:  500,  // 500ms
	ResponseTimeCritical: 1000, // 1s

	ErrorRateWarning:  5,  // 5%
	ErrorRateCritical: 10, // 10%
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
		return StatusUnknown
	}

	return StatusUp
}

// NewCheckResult creates a new check result with default values
func NewCheckResult(name string, category Category) *CheckResult {
	return &CheckResult{
		Name:      name,
		Category:  category,
		Level:     LevelInfo,
		Status:    StatusUnknown,
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}
}

// SetStatus updates the status and optionally adds a description
func (cr *CheckResult) SetStatus(status Status, description string) {
	cr.Status = status
	if description != "" {
		cr.Description = description
	}
}

// AddDetail adds a detail to the check result
func (cr *CheckResult) AddDetail(key string, value interface{}) {
	if cr.Details == nil {
		cr.Details = make(map[string]interface{})
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

const defaultParallelCheckTimeout = 10 * time.Second

type indexedCheckResult struct {
	index  int
	result *CheckResult
}

// RunChecksParallel runs health checks concurrently and returns results in input order.
func RunChecksParallel(ctx context.Context, checks []func(context.Context) *CheckResult) []*CheckResult {
	return runChecksParallel(ctx, defaultParallelCheckTimeout, checks)
}

func runChecksParallel(
	ctx context.Context,
	timeout time.Duration,
	checks []func(context.Context) *CheckResult,
) []*CheckResult {
	if len(checks) == 0 {
		return []*CheckResult{}
	}
	ctx = nonNilContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if ctx.Err() != nil {
		return []*CheckResult{newControlCheckResult("cancelled", "Health check was cancelled")}
	}

	results := make([]*CheckResult, len(checks))
	resultCh := make(chan indexedCheckResult, len(checks))

	// Start all checks in parallel
	for index, check := range checks {
		go func(index int, check func(context.Context) *CheckResult) {
			resultCh <- indexedCheckResult{index: index, result: runHealthCheck(ctx, check)}
		}(index, check)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	completed := 0
	for completed < len(checks) {
		select {
		case indexed := <-resultCh:
			results[indexed.index] = indexed.result
			completed++
		case <-timer.C:
			cancel()
			return append(compactCheckResults(results), newControlCheckResult("timeout", "Health check timed out"))
		case <-ctx.Done():
			return append(compactCheckResults(results), newControlCheckResult("cancelled", "Health check was cancelled"))
		}
	}

	return results
}

func runHealthCheck(ctx context.Context, check func(context.Context) *CheckResult) (result *CheckResult) {
	if check == nil {
		return newControlCheckResult("invalid", "Health check function is nil")
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			result = newControlCheckResult("panic", fmt.Sprintf("Health check panicked: %v", recovered))
		}
		if result == nil {
			result = newControlCheckResult("invalid", "Health check returned no result")
		}
	}()
	return check(ctx)
}

func newControlCheckResult(name, description string) *CheckResult {
	result := NewCheckResult(name, CategoryConnectivity)
	result.Level = LevelCritical
	result.SetStatus(StatusUnknown, description)
	result.Complete()
	return result
}

func compactCheckResults(results []*CheckResult) []*CheckResult {
	compacted := make([]*CheckResult, 0, len(results))
	for _, result := range results {
		if result != nil {
			compacted = append(compacted, result)
		}
	}
	return compacted
}
