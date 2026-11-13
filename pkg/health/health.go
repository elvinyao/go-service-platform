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

// DetermineStatus reduces individual check results through the health status lattice:
//  1. Empty results -> UNKNOWN
//  2. Critical DOWN -> DOWN
//  3. Critical UNKNOWN (e.g. timeout/panic/cancelled control checks) -> UNKNOWN
//  4. Non-critical DOWN or any DEGRADED -> DEGRADED
//  5. Non-critical UNKNOWN -> UNKNOWN
//  6. All UP -> UP
func DetermineStatus(results []CheckResult) Status {
	if len(results) == 0 {
		return StatusUnknown
	}

	hasCriticalUnknown := false
	hasDegradedOrNonCriticalDown := false
	hasNonCriticalUnknown := false

	for _, result := range results {
		switch result.Status {
		case StatusDown:
			if result.Level == LevelCritical {
				return StatusDown
			}
			hasDegradedOrNonCriticalDown = true
		case StatusUnknown:
			if result.Level == LevelCritical {
				hasCriticalUnknown = true
			} else {
				hasNonCriticalUnknown = true
			}
		case StatusDegraded:
			hasDegradedOrNonCriticalDown = true
		}
	}

	if hasCriticalUnknown {
		return StatusUnknown
	}
	if hasDegradedOrNonCriticalDown {
		return StatusDegraded
	}
	if hasNonCriticalUnknown {
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

type checkCollectorState uint8

const (
	collectorStateIdle checkCollectorState = iota
	collectorStateRunning
	collectorStateCompleted
	collectorStateTimedOut
	collectorStateCancelled
)

type parallelCheckCollector struct {
	state     checkCollectorState
	checks    []func(context.Context) *CheckResult
	timeout   time.Duration
	results   []*CheckResult
	completed int
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
	collector := &parallelCheckCollector{
		state:   collectorStateIdle,
		checks:  checks,
		timeout: timeout,
	}
	return collector.run(ctx)
}

func (c *parallelCheckCollector) run(ctx context.Context) []*CheckResult {
	if len(c.checks) == 0 {
		c.state = collectorStateCompleted
		return []*CheckResult{}
	}
	ctx = nonNilContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if ctx.Err() != nil {
		c.state = collectorStateCancelled
		return []*CheckResult{newControlCheckResult("cancelled", "Health check was cancelled")}
	}

	c.state = collectorStateRunning
	c.results = make([]*CheckResult, len(c.checks))
	resultCh := make(chan indexedCheckResult, len(c.checks))

	for index, check := range c.checks {
		go func(index int, check func(context.Context) *CheckResult) {
			resultCh <- indexedCheckResult{index: index, result: runHealthCheck(ctx, check)}
		}(index, check)
	}

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	for c.completed < len(c.checks) {
		select {
		case indexed := <-resultCh:
			c.results[indexed.index] = indexed.result
			c.completed++
		case <-timer.C:
			c.state = collectorStateTimedOut
			cancel()
			return append(compactCheckResults(c.results), newControlCheckResult("timeout", "Health check timed out"))
		case <-ctx.Done():
			c.state = collectorStateCancelled
			return append(compactCheckResults(c.results), newControlCheckResult("cancelled", "Health check was cancelled"))
		}
	}

	c.state = collectorStateCompleted
	return c.results
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
