package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"
)

// MemoryUsageChecker checks memory usage
type MemoryUsageChecker struct {
	ThresholdWarningPercent  float64
	ThresholdCriticalPercent float64
}

// Check implements the Checker interface
func (c *MemoryUsageChecker) Check(ctx context.Context) *CheckResult {
	result := NewCheckResult("memory-usage", CategoryResources)
	result.Level = LevelWarning

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate memory usage as a percentage of total available
	totalMem := float64(memStats.Sys)
	usedMem := float64(memStats.Alloc)
	memUsagePercent := (usedMem / totalMem) * 100.0

	result.AddDetail("total_memory_bytes", memStats.Sys)
	result.AddDetail("used_memory_bytes", memStats.Alloc)
	result.AddDetail("usage_percent", memUsagePercent)

	if memUsagePercent >= c.ThresholdCriticalPercent {
		result.SetStatus(StatusDegraded, fmt.Sprintf("Memory usage critical: %.2f%%", memUsagePercent))
	} else if memUsagePercent >= c.ThresholdWarningPercent {
		result.SetStatus(StatusDegraded, fmt.Sprintf("Memory usage high: %.2f%%", memUsagePercent))
	} else {
		result.SetStatus(StatusUp, fmt.Sprintf("Memory usage normal: %.2f%%", memUsagePercent))
	}

	result.Complete()
	return result
}

// NewMemoryUsageChecker creates a new memory usage checker with default thresholds
func NewMemoryUsageChecker() *MemoryUsageChecker {
	return &MemoryUsageChecker{
		ThresholdWarningPercent:  70.0,
		ThresholdCriticalPercent: 85.0,
	}
}

// GoroutineCountChecker checks the number of active goroutines
type GoroutineCountChecker struct {
	ThresholdWarning  int
	ThresholdCritical int
}

// Check implements the Checker interface
func (c *GoroutineCountChecker) Check(ctx context.Context) *CheckResult {
	result := NewCheckResult("goroutine-count", CategoryResources)
	result.Level = LevelWarning

	count := runtime.NumGoroutine()
	result.AddDetail("count", count)

	if count >= c.ThresholdCritical {
		result.SetStatus(StatusDegraded, fmt.Sprintf("Goroutine count critical: %d", count))
	} else if count >= c.ThresholdWarning {
		result.SetStatus(StatusDegraded, fmt.Sprintf("Goroutine count high: %d", count))
	} else {
		result.SetStatus(StatusUp, fmt.Sprintf("Goroutine count normal: %d", count))
	}

	result.Complete()
	return result
}

// NewGoroutineCountChecker creates a new goroutine count checker with default thresholds
func NewGoroutineCountChecker() *GoroutineCountChecker {
	return &GoroutineCountChecker{
		ThresholdWarning:  1000,
		ThresholdCritical: 5000,
	}
}

// ConnectivityChecker checks connectivity to a host:port
type ConnectivityChecker struct {
	Host            string
	Port            string
	TimeoutMs       int
	ExpectedLatency int // in milliseconds
}

// Check implements the Checker interface
func (c *ConnectivityChecker) Check(ctx context.Context) *CheckResult {
	result := NewCheckResult("connectivity-"+c.Host+":"+c.Port, CategoryConnectivity)
	result.Level = LevelCritical

	timeout := time.Duration(c.TimeoutMs) * time.Millisecond
	start := time.Now()

	// Try to connect
	conn, err := net.DialTimeout("tcp", c.Host+":"+c.Port, timeout)
	elapsed := time.Since(start)

	result.AddDetail("latency_ms", elapsed.Milliseconds())

	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Failed to connect to %s:%s: %v", c.Host, c.Port, err))
		result.Complete()
		return result
	}
	defer conn.Close()

	// Check latency
	if elapsed.Milliseconds() > int64(c.ExpectedLatency) {
		result.SetStatus(StatusDegraded, fmt.Sprintf("High latency connecting to %s:%s: %dms", c.Host, c.Port, elapsed.Milliseconds()))
	} else {
		result.SetStatus(StatusUp, fmt.Sprintf("Successfully connected to %s:%s in %dms", c.Host, c.Port, elapsed.Milliseconds()))
	}

	result.Complete()
	return result
}

// NewConnectivityChecker creates a new connectivity checker with default settings
func NewConnectivityChecker(host, port string) *ConnectivityChecker {
	return &ConnectivityChecker{
		Host:            host,
		Port:            port,
		TimeoutMs:       1000,
		ExpectedLatency: 500,
	}
}

// HTTPEndpointChecker checks an HTTP endpoint
type HTTPEndpointChecker struct {
	URL             string
	Method          string
	TimeoutMs       int
	ExpectedStatus  int
	ExpectedLatency int // in milliseconds
}

// Check implements the Checker interface
func (c *HTTPEndpointChecker) Check(ctx context.Context) *CheckResult {
	result := NewCheckResult("http-"+c.URL, CategoryConnectivity)
	result.Level = LevelWarning

	client := &http.Client{
		Timeout: time.Duration(c.TimeoutMs) * time.Millisecond,
	}

	req, err := http.NewRequestWithContext(ctx, c.Method, c.URL, nil)
	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Failed to create request for %s: %v", c.URL, err))
		result.Complete()
		return result
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	result.AddDetail("latency_ms", elapsed.Milliseconds())

	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Failed to connect to %s: %v", c.URL, err))
		result.Complete()
		return result
	}
	defer resp.Body.Close()

	result.AddDetail("status_code", resp.StatusCode)

	// Check status code
	if resp.StatusCode != c.ExpectedStatus {
		result.SetStatus(StatusDegraded, fmt.Sprintf("Unexpected status code from %s: %d (expected %d)", c.URL, resp.StatusCode, c.ExpectedStatus))
	} else if elapsed.Milliseconds() > int64(c.ExpectedLatency) {
		result.SetStatus(StatusDegraded, fmt.Sprintf("High latency from %s: %dms", c.URL, elapsed.Milliseconds()))
	} else {
		result.SetStatus(StatusUp, fmt.Sprintf("Successfully connected to %s in %dms", c.URL, elapsed.Milliseconds()))
	}

	result.Complete()
	return result
}

// NewHTTPEndpointChecker creates a new HTTP endpoint checker with default settings
func NewHTTPEndpointChecker(url string) *HTTPEndpointChecker {
	return &HTTPEndpointChecker{
		URL:             url,
		Method:          http.MethodGet,
		TimeoutMs:       2000,
		ExpectedStatus:  http.StatusOK,
		ExpectedLatency: 1000,
	}
}

// DependencyChecker checks the health of a dependency service
type DependencyChecker struct {
	ServiceName string
	Reporter    Reporter
	Level       Level
}

// Check implements the Checker interface
func (c *DependencyChecker) Check(ctx context.Context) *CheckResult {
	result := NewCheckResult("dependency-"+c.ServiceName, CategoryDependency)
	result.Level = c.Level

	// Create a report for the dependency
	report := &Report{
		ServiceName: c.ServiceName,
		Status:      StatusUnknown,
		RefreshedAt: time.Now(),
	}

	// Get the health report from the reporter
	c.Reporter.ReportHealth(ctx, report)

	// Map the dependency status to our result
	result.AddDetail("dependency_status", string(report.Status))

	switch report.Status {
	case StatusUp:
		result.SetStatus(StatusUp, fmt.Sprintf("Dependency %s is healthy", c.ServiceName))
	case StatusDegraded:
		result.SetStatus(StatusDegraded, fmt.Sprintf("Dependency %s is degraded", c.ServiceName))
	case StatusDown:
		result.SetStatus(StatusDown, fmt.Sprintf("Dependency %s is down", c.ServiceName))
	default:
		result.SetStatus(StatusUnknown, fmt.Sprintf("Dependency %s status is unknown", c.ServiceName))
	}

	result.Complete()
	return result
}

// NewDependencyChecker creates a new dependency checker
func NewDependencyChecker(serviceName string, reporter Reporter, level Level) *DependencyChecker {
	return &DependencyChecker{
		ServiceName: serviceName,
		Reporter:    reporter,
		Level:       level,
	}
}

// DataAccessChecker checks data access operations
type DataAccessChecker struct {
	Name        string
	AccessFn    func(ctx context.Context) error
	TimeoutMs   int
	Description string
}

// Check implements the Checker interface
func (c *DataAccessChecker) Check(ctx context.Context) *CheckResult {
	result := NewCheckResult("data-access-"+c.Name, CategoryData)
	result.Level = LevelCritical

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMs)*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.AccessFn(timeoutCtx)
	elapsed := time.Since(start)

	result.AddDetail("latency_ms", elapsed.Milliseconds())
	result.AddDetail("description", c.Description)

	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			result.SetStatus(StatusDegraded, fmt.Sprintf("Data access to %s timed out after %dms", c.Name, c.TimeoutMs))
		} else {
			result.SetStatus(StatusDown, fmt.Sprintf("Data access to %s failed: %v", c.Name, err))
		}
	} else {
		result.SetStatus(StatusUp, fmt.Sprintf("Data access to %s successful in %dms", c.Name, elapsed.Milliseconds()))
	}

	result.Complete()
	return result
}

// NewDataAccessChecker creates a new data access checker
func NewDataAccessChecker(name string, accessFn func(ctx context.Context) error, description string) *DataAccessChecker {
	return &DataAccessChecker{
		Name:        name,
		AccessFn:    accessFn,
		TimeoutMs:   1000,
		Description: description,
	}
}
