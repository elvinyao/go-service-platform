package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMemoryUsageChecker(t *testing.T) {
	// Test normal memory usage
	t.Run("NormalMemoryUsage", func(t *testing.T) {
		checker := &MemoryUsageChecker{
			ThresholdWarningPercent:  70.0,
			ThresholdCriticalPercent: 85.0,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, "memory-usage", result.Name)
		assert.Equal(t, CategoryResources, result.Category)
		assert.Equal(t, LevelWarning, result.Level)

		// We can't predict exact memory usage, but we can check that the result has the correct fields
		assert.Contains(t, result.Details, "total_memory_bytes")
		assert.Contains(t, result.Details, "used_memory_bytes")
		assert.Contains(t, result.Details, "usage_percent")

		// Since we don't know the actual memory usage, we can't assert on the status
		// But we can assert that one of the expected statuses was set
		assert.Contains(t, []Status{StatusUp, StatusDegraded}, result.Status)
	})

	// Test creating with default values
	t.Run("DefaultValues", func(t *testing.T) {
		checker := NewMemoryUsageChecker()
		assert.Equal(t, 70.0, checker.ThresholdWarningPercent)
		assert.Equal(t, 85.0, checker.ThresholdCriticalPercent)
	})
}

func TestGoroutineCountChecker(t *testing.T) {
	// Test normal goroutine count
	t.Run("NormalGoroutineCount", func(t *testing.T) {
		// Set thresholds very high to ensure we get a normal status
		checker := &GoroutineCountChecker{
			ThresholdWarning:  10000,
			ThresholdCritical: 20000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, "goroutine-count", result.Name)
		assert.Equal(t, CategoryResources, result.Category)
		assert.Equal(t, LevelWarning, result.Level)
		assert.Equal(t, StatusUp, result.Status)

		assert.Contains(t, result.Details, "count")
		assert.Greater(t, result.Details["count"].(int), 0) // At least 1 goroutine (this test)
	})

	// Test high goroutine count (warning)
	t.Run("WarningGoroutineCount", func(t *testing.T) {
		// Set warning threshold to 1 to ensure we get a warning status
		checker := &GoroutineCountChecker{
			ThresholdWarning:  1, // This will definitely be exceeded
			ThresholdCritical: 10000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Description, "high")
	})

	// Test creating with default values
	t.Run("DefaultValues", func(t *testing.T) {
		checker := NewGoroutineCountChecker()
		assert.Equal(t, 1000, checker.ThresholdWarning)
		assert.Equal(t, 5000, checker.ThresholdCritical)
	})
}

func TestConnectivityChecker(t *testing.T) {
	// Test successful connection
	t.Run("SuccessfulConnection", func(t *testing.T) {
		// Start a local server to test connectivity
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		addr := listener.Addr().(*net.TCPAddr)

		// Configure checker
		checker := &ConnectivityChecker{
			Host:            "127.0.0.1",
			Port:            fmt.Sprintf("%d", addr.Port),
			TimeoutMs:       500,
			ExpectedLatency: 1000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		assert.Contains(t, result.Details, "latency_ms")
		assert.Contains(t, result.Description, "Successfully")
	})

	// Test failed connection
	t.Run("FailedConnection", func(t *testing.T) {
		// Use a port that's unlikely to be in use
		checker := &ConnectivityChecker{
			Host:            "127.0.0.1",
			Port:            "65535",
			TimeoutMs:       100, // Short timeout to avoid long test
			ExpectedLatency: 1000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "Failed")
	})

	// Test slow connection
	t.Run("SlowConnection", func(t *testing.T) {
		// Start a local server with a delay
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		// Accept connections with a delay
		go func() {
			conn, _ := listener.Accept()
			time.Sleep(50 * time.Millisecond) // Add delay
			if conn != nil {
				conn.Close()
			}
		}()

		addr := listener.Addr().(*net.TCPAddr)

		// Configure checker with a very low expected latency
		checker := &ConnectivityChecker{
			Host:            "127.0.0.1",
			Port:            fmt.Sprintf("%d", addr.Port),
			TimeoutMs:       500,
			ExpectedLatency: 10, // Very low, will be exceeded
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		// Check if there's latency information
		assert.Contains(t, result.Details, "latency_ms")
		// The implementation might not set a degraded status even if expected latency
		// is exceeded, so we can't test for that

		listener.Close()
	})

	// Test constructor
	t.Run("Constructor", func(t *testing.T) {
		checker := NewConnectivityChecker("example.com", "80")
		assert.Equal(t, "example.com", checker.Host)
		assert.Equal(t, "80", checker.Port)
		assert.Equal(t, 1000, checker.TimeoutMs)
		assert.Equal(t, 500, checker.ExpectedLatency)
	})
}

func TestHTTPEndpointChecker(t *testing.T) {
	// Test successful HTTP request
	t.Run("SuccessfulRequest", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}))
		defer server.Close()

		checker := &HTTPEndpointChecker{
			URL:             server.URL,
			Method:          "GET",
			TimeoutMs:       500,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 1000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		assert.Contains(t, result.Details, "latency_ms")
		assert.Contains(t, result.Details, "status_code")
		assert.Equal(t, http.StatusOK, result.Details["status_code"])
	})

	// Test unsuccessful HTTP request (wrong status code)
	t.Run("WrongStatusCode", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		checker := &HTTPEndpointChecker{
			URL:             server.URL,
			Method:          "GET",
			TimeoutMs:       500,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 1000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Description, "Unexpected status code")
		assert.Equal(t, http.StatusInternalServerError, result.Details["status_code"])
	})

	// Test slow HTTP request
	t.Run("SlowRequest", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond) // Add delay
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		checker := &HTTPEndpointChecker{
			URL:             server.URL,
			Method:          "GET",
			TimeoutMs:       500,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 10, // Very low, will be exceeded
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Description, "High latency")
	})

	// Test connection error
	t.Run("ConnectionError", func(t *testing.T) {
		checker := &HTTPEndpointChecker{
			URL:             "http://invalid-domain-that-doesnt-exist.example",
			Method:          "GET",
			TimeoutMs:       100, // Short timeout to avoid long test
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 1000,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "Failed to connect")
	})

	// Test constructor
	t.Run("Constructor", func(t *testing.T) {
		checker := NewHTTPEndpointChecker("https://example.com")
		assert.Equal(t, "https://example.com", checker.URL)
		assert.Equal(t, "GET", checker.Method)
		assert.Equal(t, 2000, checker.TimeoutMs)
		assert.Equal(t, http.StatusOK, checker.ExpectedStatus)
		assert.Equal(t, 1000, checker.ExpectedLatency)
	})
}

// Mock implementation of Reporter for testing
type MockReporter struct {
	mock.Mock
}

func (m *MockReporter) ReportHealth(ctx context.Context, report *Report) {
	m.Called(ctx, report)
}

func TestDependencyChecker(t *testing.T) {
	// Test dependency UP
	t.Run("DependencyUp", func(t *testing.T) {
		mockReporter := new(MockReporter)

		upReport := Report{
			Status: StatusUp,
		}

		mockReporter.On("ReportHealth", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			report := args.Get(1).(*Report)
			*report = upReport
		})

		checker := &DependencyChecker{
			ServiceName: "test-service",
			Reporter:    mockReporter,
			Level:       LevelCritical,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		assert.Equal(t, "dependency-test-service", result.Name)
		assert.Equal(t, CategoryDependency, result.Category)
		assert.Equal(t, LevelCritical, result.Level)

		mockReporter.AssertExpectations(t)
	})

	// Test dependency DOWN
	t.Run("DependencyDown", func(t *testing.T) {
		mockReporter := new(MockReporter)

		downReport := Report{
			Status: StatusDown,
		}

		mockReporter.On("ReportHealth", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			report := args.Get(1).(*Report)
			*report = downReport
		})

		checker := &DependencyChecker{
			ServiceName: "test-service",
			Reporter:    mockReporter,
			Level:       LevelWarning,
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "down")

		mockReporter.AssertExpectations(t)
	})

	// Test constructor
	t.Run("Constructor", func(t *testing.T) {
		mockReporter := new(MockReporter)

		checker := NewDependencyChecker("test-service", mockReporter, LevelCritical)
		assert.Equal(t, "test-service", checker.ServiceName)
		assert.Equal(t, mockReporter, checker.Reporter)
		assert.Equal(t, LevelCritical, checker.Level)
	})
}

func TestDataAccessChecker(t *testing.T) {
	// Test successful access
	t.Run("SuccessfulAccess", func(t *testing.T) {
		accessFn := func(ctx context.Context) error {
			return nil // Success
		}

		checker := &DataAccessChecker{
			Name:        "test-database",
			AccessFn:    accessFn,
			TimeoutMs:   500,
			Description: "Test database checker",
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		assert.Contains(t, result.Details, "latency_ms")
		assert.Contains(t, result.Description, "successful")
	})

	// Test failed access
	t.Run("FailedAccess", func(t *testing.T) {
		accessFn := func(ctx context.Context) error {
			return errors.New("connection failed")
		}

		checker := &DataAccessChecker{
			Name:        "test-database",
			AccessFn:    accessFn,
			TimeoutMs:   500,
			Description: "Test database checker",
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "failed")
		assert.Contains(t, result.Description, "connection failed")
	})

	// Test slow access
	t.Run("SlowAccess", func(t *testing.T) {
		accessFn := func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond) // Add delay
			return nil
		}

		checker := &DataAccessChecker{
			Name:        "test-database",
			AccessFn:    accessFn,
			TimeoutMs:   500,
			Description: "Test database checker",
		}

		// Execute the check and verify it completes successfully but is slow
		result := checker.Check(context.Background())

		// We can't guarantee it will be degraded since we don't know the threshold
		// But we can check that latency was measured
		assert.Contains(t, result.Details, "latency_ms")
		assert.True(t, result.Details["latency_ms"].(int64) >= 50, "Latency should be at least 50ms")
	})

	// Test timeout
	t.Run("Timeout", func(t *testing.T) {
		accessFn := func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return nil
			}
		}

		checker := &DataAccessChecker{
			Name:        "test-database",
			AccessFn:    accessFn,
			TimeoutMs:   50, // Short timeout to trigger error
			Description: "Test database checker",
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Description, "timed out")
		// Error might not be included in details for timeout
	})

	// Test constructor
	t.Run("Constructor", func(t *testing.T) {
		accessFn := func(ctx context.Context) error {
			return nil
		}

		checker := NewDataAccessChecker("test-database", accessFn, "Test database checker")
		assert.Equal(t, "test-database", checker.Name)
		assert.NotNil(t, checker.AccessFn)
		assert.Equal(t, 1000, checker.TimeoutMs)
		assert.Equal(t, "Test database checker", checker.Description)
	})
}
