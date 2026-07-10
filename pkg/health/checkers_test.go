package health

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func (f dialContextFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func staticHTTPClient(status int, delay time.Duration) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if delay > 0 {
			time.Sleep(delay)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("response")),
			Request:    request,
		}, nil
	})}
}

func pipeConnection() net.Conn {
	client, server := net.Pipe()
	server.Close()
	return client
}

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
	t.Run("CanceledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		checker := NewConnectivityChecker("203.0.113.1", "65535")

		started := time.Now()
		result := checker.Check(ctx)

		assert.Equal(t, StatusDown, result.Status)
		assert.Less(t, time.Since(started), 100*time.Millisecond)
		assert.Contains(t, result.Description, "canceled")
	})

	// Test successful connection
	t.Run("SuccessfulConnection", func(t *testing.T) {
		var address string
		checker := &ConnectivityChecker{
			Host:            "2001:db8::1",
			Port:            "443",
			TimeoutMs:       500,
			ExpectedLatency: 1000,
			Dialer: dialContextFunc(func(_ context.Context, _, gotAddress string) (net.Conn, error) {
				address = gotAddress
				return pipeConnection(), nil
			}),
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		assert.Contains(t, result.Details, "latency_ms")
		assert.Contains(t, result.Description, "Successfully")
		assert.Equal(t, "[2001:db8::1]:443", address)
	})

	// Test failed connection
	t.Run("FailedConnection", func(t *testing.T) {
		checker := &ConnectivityChecker{
			Host:            "127.0.0.1",
			Port:            "65535",
			TimeoutMs:       100,
			ExpectedLatency: 1000,
			Dialer: dialContextFunc(func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("connection refused")
			}),
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "Failed")
	})

	// Test slow connection
	t.Run("SlowConnection", func(t *testing.T) {
		checker := &ConnectivityChecker{
			Host:            "127.0.0.1",
			Port:            "443",
			TimeoutMs:       500,
			ExpectedLatency: 5,
			Dialer: dialContextFunc(func(context.Context, string, string) (net.Conn, error) {
				time.Sleep(20 * time.Millisecond)
				return pipeConnection(), nil
			}),
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Details, "latency_ms")
		assert.Contains(t, result.Description, "High latency")
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
		checker := &HTTPEndpointChecker{
			URL:             "https://service.example/health",
			Method:          "GET",
			TimeoutMs:       500,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 1000,
			HTTPClient:      staticHTTPClient(http.StatusOK, 0),
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUp, result.Status)
		assert.Contains(t, result.Details, "latency_ms")
		assert.Contains(t, result.Details, "status_code")
		assert.Equal(t, http.StatusOK, result.Details["status_code"])
	})

	// Test unsuccessful HTTP request (wrong status code)
	t.Run("WrongStatusCode", func(t *testing.T) {
		checker := &HTTPEndpointChecker{
			URL:             "https://service.example/health",
			Method:          "GET",
			TimeoutMs:       500,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 1000,
			HTTPClient:      staticHTTPClient(http.StatusInternalServerError, 0),
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Description, "Unexpected status code")
		assert.Equal(t, http.StatusInternalServerError, result.Details["status_code"])
	})

	// Test slow HTTP request
	t.Run("SlowRequest", func(t *testing.T) {
		checker := &HTTPEndpointChecker{
			URL:             "https://service.example/health",
			Method:          "GET",
			TimeoutMs:       500,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 10,
			HTTPClient:      staticHTTPClient(http.StatusOK, 50*time.Millisecond),
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Contains(t, result.Description, "High latency")
	})

	// Test connection error
	t.Run("ConnectionError", func(t *testing.T) {
		checker := &HTTPEndpointChecker{
			URL:             "https://service.example/health",
			Method:          "GET",
			TimeoutMs:       100,
			ExpectedStatus:  http.StatusOK,
			ExpectedLatency: 1000,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network unavailable")
			})},
		}

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "Failed to connect")
	})

	t.Run("InvalidURL", func(t *testing.T) {
		checker := NewHTTPEndpointChecker("://invalid")

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "Failed to create request")
	})

	t.Run("ClientTimeout", func(t *testing.T) {
		client := &http.Client{
			Timeout: 10 * time.Millisecond,
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			}),
		}
		checker := NewHTTPEndpointChecker("https://service.example/health")
		checker.HTTPClient = client

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "deadline exceeded")
	})

	// Test constructor
	t.Run("Constructor", func(t *testing.T) {
		checker := NewHTTPEndpointChecker("https://example.com")
		assert.Equal(t, "https://example.com", checker.URL)
		assert.Equal(t, "GET", checker.Method)
		assert.Equal(t, 2000, checker.TimeoutMs)
		assert.Equal(t, http.StatusOK, checker.ExpectedStatus)
		assert.Equal(t, 1000, checker.ExpectedLatency)
		assert.Nil(t, checker.HTTPClient)
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
	t.Run("MissingReporter", func(t *testing.T) {
		checker := NewDependencyChecker("missing", nil, LevelCritical)

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnknown, result.Status)
		assert.Contains(t, result.Description, "no health reporter")
	})

	t.Run("ReporterPanic", func(t *testing.T) {
		checker := NewDependencyChecker("panic", panicHealthReporter{}, LevelCritical)

		result := checker.Check(context.Background())

		assert.Equal(t, StatusUnknown, result.Status)
		assert.Contains(t, result.Description, "panicked")
	})

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
	t.Run("MissingAccessFunction", func(t *testing.T) {
		checker := NewDataAccessChecker("missing", nil, "missing function")

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "no access function")
	})

	t.Run("TimeoutDoesNotRequireFunctionCooperation", func(t *testing.T) {
		checker := &DataAccessChecker{
			Name: "blocking",
			AccessFn: func(context.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			},
			TimeoutMs: 10,
		}

		started := time.Now()
		result := checker.Check(context.Background())

		assert.Equal(t, StatusDegraded, result.Status)
		assert.Less(t, time.Since(started), 80*time.Millisecond)
	})

	t.Run("PanicBecomesFailure", func(t *testing.T) {
		checker := NewDataAccessChecker("panic", func(context.Context) error {
			panic("boom")
		}, "panic test")

		result := checker.Check(context.Background())

		assert.Equal(t, StatusDown, result.Status)
		assert.Contains(t, result.Description, "panicked")
	})

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
