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
func (c *MemoryUsageChecker) Check(ctx context.Context) CheckResult {
	result := NewCheckResult("memory-usage", CategoryResources, LevelWarning)

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate memory usage as a percentage of total available
	totalMem := float64(memStats.Sys)
	usedMem := float64(memStats.Alloc)
	memUsagePercent := (usedMem / totalMem) * 100.0

	result.AddDetail("total_memory_bytes", fmt.Sprintf("%d", memStats.Sys))
	result.AddDetail("used_memory_bytes", fmt.Sprintf("%d", memStats.Alloc))
	result.AddDetail("usage_percent", fmt.Sprintf("%.2f%%", memUsagePercent))

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

// GoroutineCountChecker checks the number of goroutines
type GoroutineCountChecker struct {
	ThresholdWarning  int
	ThresholdCritical int
}

// Check implements the Checker interface
func (c *GoroutineCountChecker) Check(ctx context.Context) CheckResult {
	result := NewCheckResult("goroutine-count", CategoryResources, LevelWarning)

	count := runtime.NumGoroutine()
	result.AddDetail("count", fmt.Sprintf("%d", count))

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

// ConnectivityChecker checks connectivity to a host
type ConnectivityChecker struct {
	Host            string
	Port            string
	TimeoutMs       int
	ExpectedLatency int // in milliseconds
}

// Check implements the Checker interface
func (c *ConnectivityChecker) Check(ctx context.Context) CheckResult {
	result := NewCheckResult("connectivity-"+c.Host, CategoryConnectivity, LevelCritical)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMs)*time.Millisecond)
	defer cancel()

	startTime := time.Now()

	// Try to connect to the host
	var d net.Dialer
	conn, err := d.DialContext(timeoutCtx, "tcp", net.JoinHostPort(c.Host, c.Port))

	latency := time.Since(startTime).Milliseconds()
	result.AddDetail("latency_ms", fmt.Sprintf("%d", latency))

	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Failed to connect to %s:%s: %v", c.Host, c.Port, err))
	} else {
		defer conn.Close()

		if latency > int64(c.ExpectedLatency) {
			result.SetStatus(StatusDegraded, fmt.Sprintf("High latency connecting to %s:%s: %dms", c.Host, c.Port, latency))
		} else {
			result.SetStatus(StatusUp, fmt.Sprintf("Successfully connected to %s:%s in %dms", c.Host, c.Port, latency))
		}
	}

	result.Complete()
	return result
}

// NewConnectivityChecker creates a new connectivity checker with default timeout
func NewConnectivityChecker(host, port string) *ConnectivityChecker {
	return &ConnectivityChecker{
		Host:            host,
		Port:            port,
		TimeoutMs:       1000,
		ExpectedLatency: 200,
	}
}

// HTTPEndpointChecker checks connectivity to an HTTP endpoint
type HTTPEndpointChecker struct {
	URL             string
	Method          string
	TimeoutMs       int
	ExpectedStatus  int
	ExpectedLatency int // in milliseconds
}

// Check implements the Checker interface
func (c *HTTPEndpointChecker) Check(ctx context.Context) CheckResult {
	result := NewCheckResult("http-"+c.URL, CategoryConnectivity, LevelCritical)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, c.Method, c.URL, nil)
	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Failed to create request for %s: %v", c.URL, err))
		result.Complete()
		return result
	}

	startTime := time.Now()
	client := &http.Client{}
	resp, err := client.Do(req)

	latency := time.Since(startTime).Milliseconds()
	result.AddDetail("latency_ms", fmt.Sprintf("%d", latency))

	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Failed to connect to %s: %v", c.URL, err))
	} else {
		defer resp.Body.Close()

		result.AddDetail("status_code", fmt.Sprintf("%d", resp.StatusCode))

		if resp.StatusCode != c.ExpectedStatus {
			result.SetStatus(StatusDegraded, fmt.Sprintf("Unexpected status code from %s: expected %d, got %d",
				c.URL, c.ExpectedStatus, resp.StatusCode))
		} else if latency > int64(c.ExpectedLatency) {
			result.SetStatus(StatusDegraded, fmt.Sprintf("High latency from %s: %dms", c.URL, latency))
		} else {
			result.SetStatus(StatusUp, fmt.Sprintf("Successfully connected to %s in %dms with status %d",
				c.URL, latency, resp.StatusCode))
		}
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
		ExpectedLatency: 500,
	}
}

// DependencyChecker checks if a dependency service is healthy
type DependencyChecker struct {
	ServiceName string
	Reporter    Reporter
	Level       Level
}

// Check implements the Checker interface
func (c *DependencyChecker) Check(ctx context.Context) CheckResult {
	result := NewCheckResult("dependency-"+c.ServiceName, CategoryDependency, c.Level)

	// Get health report from the dependency
	report := c.Reporter.HealthCheck(ctx)

	// Add details from dependency
	result.AddDetail("dependency_status", string(report.Status))
	result.AddDetail("dependency_checks", fmt.Sprintf("%d", len(report.CheckResults)))

	// Set status based on dependency status
	switch report.Status {
	case StatusUp:
		result.SetStatus(StatusUp, fmt.Sprintf("Dependency %s is healthy", c.ServiceName))
	case StatusDegraded:
		result.SetStatus(StatusDegraded, fmt.Sprintf("Dependency %s is degraded", c.ServiceName))
	case StatusDown:
		if c.Level == LevelCritical {
			result.SetStatus(StatusDown, fmt.Sprintf("Critical dependency %s is down", c.ServiceName))
		} else {
			result.SetStatus(StatusDegraded, fmt.Sprintf("Dependency %s is down", c.ServiceName))
		}
	default:
		result.SetStatus(StatusUnknown, fmt.Sprintf("Dependency %s health status is unknown", c.ServiceName))
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

// DataAccessChecker checks data access functionality
type DataAccessChecker struct {
	Name        string
	AccessFn    func(ctx context.Context) error
	TimeoutMs   int
	Description string
}

// Check implements the Checker interface
func (c *DataAccessChecker) Check(ctx context.Context) CheckResult {
	result := NewCheckResult("data-access-"+c.Name, CategoryData, LevelCritical)

	// Set a timeout for the data access operation
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMs)*time.Millisecond)
	defer cancel()

	startTime := time.Now()
	err := c.AccessFn(timeoutCtx)
	latency := time.Since(startTime).Milliseconds()

	result.AddDetail("latency_ms", fmt.Sprintf("%d", latency))

	if err != nil {
		result.SetStatus(StatusDown, fmt.Sprintf("Data access failed: %v", err))
	} else {
		result.SetStatus(StatusUp, c.Description)
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
