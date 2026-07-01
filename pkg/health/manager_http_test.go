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
	reports chan *Report
}

func (r recordingReporter) ReportHealth(ctx context.Context, report *Report) {
	r.reports <- report
}

type healthTestService struct {
	name    string
	running bool
	metrics map[string]interface{}
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

func TestHealthManagerRefreshCachesAndReports(t *testing.T) {
	manager := NewHealthManager(time.Hour, "test-version")
	reporter := recordingReporter{reports: make(chan *Report, 1)}
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
	})

	report := manager.RefreshReport(context.Background())
	if report.Version != "test-version" {
		t.Fatalf("version = %q, want test-version", report.Version)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("status = %s, want DEGRADED", report.Status)
	}
	if len(report.CheckResults) < 5 {
		t.Fatalf("check results len = %d, want system, service, and custom checks", len(report.CheckResults))
	}

	select {
	case got := <-reporter.reports:
		if got != report {
			t.Fatalf("reported pointer differs from refreshed report")
		}
	case <-time.After(time.Second):
		t.Fatalf("reporter was not notified")
	}

	cached := manager.GetHealthReport(context.Background())
	if cached != report {
		t.Fatalf("cached report pointer differs from refreshed report")
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
	if report.Status != StatusDegraded {
		t.Fatalf("overall status = %s, want DEGRADED", report.Status)
	}

	if got := manager.determineOverallStatus([]CheckResult{}); got != StatusUnknown {
		t.Fatalf("empty status = %s, want UNKNOWN", got)
	}
	if got := manager.determineOverallStatus([]CheckResult{{Status: StatusUnknown}}); got != StatusUnknown {
		t.Fatalf("all unknown status = %s, want UNKNOWN", got)
	}
}

func TestHealthManagerSystemCheckThresholdBranches(t *testing.T) {
	original := DefaultThresholds
	defer func() { DefaultThresholds = original }()

	manager := NewHealthManager(time.Hour, "test")

	DefaultThresholds.MemoryCritical = 0.5
	memory := manager.checkMemory(context.Background())
	if memory.Status != StatusDegraded || memory.Level != LevelCritical {
		t.Fatalf("memory status/level = %s/%s, want DEGRADED/CRITICAL", memory.Status, memory.Level)
	}

	DefaultThresholds.CPUCritical = 0.2
	cpu := manager.checkCPU(context.Background())
	if cpu.Status != StatusDegraded || cpu.Level != LevelCritical {
		t.Fatalf("cpu status/level = %s/%s, want DEGRADED/CRITICAL", cpu.Status, cpu.Level)
	}

	DefaultThresholds.DiskCritical = 0.8
	DefaultThresholds.DiskWarning = 0.6
	disk := manager.checkDiskSpace(context.Background())
	if disk.Status != StatusDegraded || disk.Level != LevelWarning {
		t.Fatalf("disk status/level = %s/%s, want DEGRADED/WARNING", disk.Status, disk.Level)
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

func TestHealthHandlerUnhealthyReadinessAndLiveness(t *testing.T) {
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
	if live.Code != http.StatusServiceUnavailable {
		t.Fatalf("down liveness status = %d, want 503", live.Code)
	}

	unknownReady := httptest.NewRecorder()
	manager.lastReport = &Report{Status: StatusUnknown}
	handler.HandleReadinessCheck(unknownReady, httptest.NewRequest(http.MethodGet, "/health/readiness", nil))
	if unknownReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("unknown readiness status = %d, want 503", unknownReady.Code)
	}
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
