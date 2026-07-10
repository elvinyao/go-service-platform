package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type staticChecker struct {
	result *CheckResult
}

func (c staticChecker) Check(ctx context.Context) *CheckResult {
	out := *c.result
	return &out
}

type recordingReporter struct {
	called bool
}

func (r *recordingReporter) ReportHealth(ctx context.Context, report *Report) {
	r.called = true
	report.Metadata = map[string]interface{}{
		"reporter": map[string]interface{}{"state": "original"},
	}
	report.AddResult(CheckResult{
		Name:      "reporter.check",
		Status:    StatusUp,
		Level:     LevelInfo,
		Category:  CategoryDependency,
		Timestamp: time.Now(),
	})
}

type panicHealthReporter struct{}

func (panicHealthReporter) ReportHealth(context.Context, *Report) {
	panic("reporter failed")
}

type healthTestService struct {
	name     string
	running  bool
	metrics  map[string]interface{}
	checkers []Checker
}

func (s healthTestService) IsRunning(ctx context.Context) bool {
	return s.running
}

func (s healthTestService) GetName() string {
	return s.name
}

func (s healthTestService) GetMetrics(ctx context.Context) map[string]interface{} {
	return s.metrics
}

func (s healthTestService) RegisterHealthChecks() []Checker {
	return s.checkers
}

func TestHealthManagerRefreshCachesAndReports(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test-version")
	reporter := &recordingReporter{}
	manager.RegisterReporter(reporter)
	manager.RegisterChecker(staticChecker{result: &CheckResult{
		Name:      "custom.warning",
		Status:    StatusDegraded,
		Level:     LevelWarning,
		Category:  CategoryDependency,
		Timestamp: time.Now(),
	}})
	manager.RegisterService(healthTestService{
		name:    "worker",
		running: true,
		metrics: map[string]interface{}{"queue_depth": 2},
		checkers: []Checker{staticChecker{result: &CheckResult{
			Name:      "dependency",
			Status:    StatusUp,
			Level:     LevelCritical,
			Category:  CategoryDependency,
			Timestamp: time.Now(),
		}}},
	})

	report := manager.RefreshReport(context.Background())
	if report.Version != "test-version" {
		t.Fatalf("version = %q, want test-version", report.Version)
	}
	if report.Uptime <= 0 {
		t.Fatalf("uptime = %v, want positive duration", report.Uptime)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("status = %s, want DEGRADED", report.Status)
	}
	if len(report.CheckResults) < 5 {
		t.Fatalf("check results len = %d, want system, service, and custom checks", len(report.CheckResults))
	}
	if !hasCheckResult(report.CheckResults, "service.worker.dependency", StatusUp) {
		t.Fatalf("service custom health check missing: %+v", report.CheckResults)
	}

	if !reporter.called {
		t.Fatalf("reporter was not called")
	}
	if !hasCheckResult(report.CheckResults, "reporter.check", StatusUp) {
		t.Fatalf("reporter result missing: %+v", report.CheckResults)
	}

	report.Metadata["reporter"].(map[string]interface{})["state"] = "mutated"
	cached := manager.GetHealthReport(context.Background())
	if cached == report {
		t.Fatalf("cached report must be returned as an independent snapshot")
	}
	if state := cached.Metadata["reporter"].(map[string]interface{})["state"]; state != "original" {
		t.Fatalf("cached nested metadata state = %v, want original", state)
	}
}

func TestHealthManagerIgnoresNilRegistrations(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test")
	manager.RegisterChecker(nil)
	manager.RegisterReporter(nil)
	manager.RegisterService(nil)

	report := manager.RefreshReport(context.Background())
	if len(report.CheckResults) != 2 {
		t.Fatalf("check results len = %d, want only two system checks", len(report.CheckResults))
	}
}

func TestHealthManagerContainsReporterPanic(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test")
	manager.RegisterReporter(panicHealthReporter{})

	report := manager.RefreshReport(context.Background())

	if !hasCheckResult(report.CheckResults, "reporter.0", StatusUnknown) {
		t.Fatalf("reporter panic result missing: %+v", report.CheckResults)
	}
}

func TestHealthManagerServiceAndStatusBranches(t *testing.T) {
	manager := NewHealthManager(0, "test")
	manager.RegisterService(healthTestService{name: "down", running: false})

	report := manager.RefreshReport(context.Background())
	foundServiceDown := false
	for _, result := range report.CheckResults {
		if result.Name == "service.down" && result.Status == StatusDown {
			foundServiceDown = true
		}
	}
	if !foundServiceDown {
		t.Fatalf("service.down status was not reported as DOWN: %+v", report.CheckResults)
	}
	if report.Status != StatusDown {
		t.Fatalf("overall status = %s, want DOWN", report.Status)
	}

	if got := manager.determineOverallStatus([]CheckResult{}); got != StatusUnknown {
		t.Fatalf("empty status = %s, want UNKNOWN", got)
	}
	if got := manager.determineOverallStatus([]CheckResult{{Status: StatusUnknown}}); got != StatusUnknown {
		t.Fatalf("all unknown status = %s, want UNKNOWN", got)
	}
}

func TestHealthManagerSystemChecksUseRuntimeMetrics(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test")
	results := RunChecksParallel(context.Background(), manager.systemChecks())
	if len(results) != 2 {
		t.Fatalf("system checks len = %d, want 2", len(results))
	}
	if !hasCheckResultPointers(results, "memory-usage") || !hasCheckResultPointers(results, "goroutine-count") {
		t.Fatalf("system checks = %+v, want runtime memory and goroutine checks", results)
	}
}

func TestRunChecksParallelHandlesEmptyAndCanceledContext(t *testing.T) {
	if results := RunChecksParallel(context.Background(), nil); len(results) != 0 {
		t.Fatalf("empty checks len = %d, want 0", len(results))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := RunChecksParallel(ctx, []func(context.Context) *CheckResult{
		func(ctx context.Context) *CheckResult {
			result := NewCheckResult("after-cancel", CategoryDependency)
			result.SetStatus(StatusUp, "")
			return result
		},
	})
	if len(results) != 1 || results[0].Name != "cancelled" || results[0].Status != StatusUnknown {
		t.Fatalf("canceled context results = %+v, want cancelled UNKNOWN", results)
	}
}

func TestHealthHandlerEndpoints(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test")
	manager.RegisterService(healthTestService{
		name:    "api",
		running: true,
		metrics: map[string]interface{}{"requests": 3},
	})
	handler := NewHealthHandler(manager)

	system := httptest.NewRecorder()
	handler.HandleSystemHealth(system, httptest.NewRequest(http.MethodGet, "/health", nil))
	if system.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200", system.Code)
	}
	var report Report
	if err := json.NewDecoder(system.Body).Decode(&report); err != nil {
		t.Fatalf("decode system report: %v", err)
	}
	if report.ServiceName != "system" {
		t.Fatalf("system service name = %q", report.ServiceName)
	}

	missingParam := httptest.NewRecorder()
	handler.HandleServiceHealth(missingParam, httptest.NewRequest(http.MethodGet, "/health/service", nil))
	if missingParam.Code != http.StatusBadRequest {
		t.Fatalf("missing service status = %d, want 400", missingParam.Code)
	}

	missingService := httptest.NewRecorder()
	handler.HandleServiceHealth(missingService, httptest.NewRequest(http.MethodGet, "/health/service?service=missing", nil))
	if missingService.Code != http.StatusNotFound {
		t.Fatalf("missing service lookup status = %d, want 404", missingService.Code)
	}

	service := httptest.NewRecorder()
	handler.HandleServiceHealth(service, httptest.NewRequest(http.MethodGet, "/health/service?service=api", nil))
	if service.Code != http.StatusOK {
		t.Fatalf("service status = %d, want 200", service.Code)
	}
	if !strings.Contains(service.Body.String(), `"requests":3`) {
		t.Fatalf("service response missing metrics: %s", service.Body.String())
	}

	ready := httptest.NewRecorder()
	handler.HandleReadinessCheck(ready, httptest.NewRequest(http.MethodGet, "/health/readiness", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("readiness status = %d, want 200", ready.Code)
	}

	live := httptest.NewRecorder()
	handler.HandleLivenessCheck(live, httptest.NewRequest(http.MethodGet, "/health/liveness", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200", live.Code)
	}
}

func TestHealthHandlerUnhealthyReadinessDoesNotFailLiveness(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test")
	manager.SetShuttingDown()
	if !manager.IsShuttingDown() {
		t.Fatalf("manager should be shutting down")
	}
	handler := NewHealthHandler(manager)

	ready := httptest.NewRecorder()
	handler.HandleReadinessCheck(ready, httptest.NewRequest(http.MethodGet, "/health/readiness", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutting-down readiness status = %d, want 503", ready.Code)
	}

	manager.shuttingDown = 0
	manager.lastRefresh = time.Now()
	manager.lastReport = &Report{Status: StatusDown}
	live := httptest.NewRecorder()
	handler.HandleLivenessCheck(live, httptest.NewRequest(http.MethodGet, "/health/liveness", nil))
	if live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"UP"`) {
		t.Fatalf("down-dependency liveness response = %d/%s, want 200 UP", live.Code, live.Body.String())
	}

	unknownReady := httptest.NewRecorder()
	manager.lastReport = &Report{Status: StatusUnknown}
	handler.HandleReadinessCheck(unknownReady, httptest.NewRequest(http.MethodGet, "/health/readiness", nil))
	if unknownReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("unknown readiness status = %d, want 503", unknownReady.Code)
	}
}

func TestHealthHandlerReadinessFailsForCriticalService(t *testing.T) {
	manager := NewHealthManager(0, "test")
	manager.RegisterService(healthTestService{name: "critical", running: false})
	handler := NewHealthHandler(manager)

	ready := httptest.NewRecorder()
	handler.HandleReadinessCheck(ready, httptest.NewRequest(http.MethodGet, "/health/readiness", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("critical service readiness status = %d, want 503", ready.Code)
	}
}

func hasCheckResult(results []CheckResult, name string, status Status) bool {
	for _, result := range results {
		if result.Name == name && result.Status == status {
			return true
		}
	}
	return false
}

func hasCheckResultPointers(results []*CheckResult, name string) bool {
	for _, result := range results {
		if result != nil && result.Name == name {
			return true
		}
	}
	return false
}

func TestHealthHandlerRegistersRoutes(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test")
	handler := NewHealthHandler(manager)
	mux := http.NewServeMux()
	handler.RegisterHTTPHandlers(mux)

	for _, path := range []string{"/health", "/health/readiness", "/health/liveness"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s was not registered", path)
		}
	}
}
