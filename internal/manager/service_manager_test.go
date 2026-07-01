package manager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"project/pkg/health"
)

type testService struct {
	name      string
	workflow  string
	kind      string
	startErr  error
	stopErr   error
	restarted int
	running   bool
}

func (s *testService) Start(ctx context.Context) error {
	if s.startErr != nil {
		return s.startErr
	}
	s.running = true
	return nil
}

func (s *testService) Stop(ctx context.Context) error {
	if s.stopErr != nil {
		return s.stopErr
	}
	s.running = false
	return nil
}

func (s *testService) Restart(ctx context.Context) error {
	s.restarted++
	_ = s.Stop(ctx)
	return s.Start(ctx)
}

func (s *testService) GetName() string                { return s.name }
func (s *testService) GetWorkflow() string            { return s.workflow }
func (s *testService) GetType() string                { return s.kind }
func (s *testService) IsRunning(context.Context) bool { return s.running }
func (s *testService) GetMetrics(context.Context) map[string]interface{} {
	return map[string]interface{}{"running": s.running}
}
func (s *testService) Configure(context.Context, interface{}) error { return nil }
func (s *testService) RegisterHealthChecks() []health.Checker       { return nil }
func (s *testService) ReportHealth(context.Context, *health.Report) {}

func TestServiceManagerStartAllReturnsErrorWhenNoServicesRegistered(t *testing.T) {
	sm := NewServiceManager("test")

	err := sm.StartAll(context.Background())
	if err == nil {
		t.Fatalf("expected no services error")
	}
}

func TestServiceManagerStartAllReturnsPartialFailure(t *testing.T) {
	sm := NewServiceManager("test")
	ok := &testService{name: "ok", workflow: "wf", kind: "test"}
	bad := &testService{name: "bad", workflow: "wf", kind: "test", startErr: fmt.Errorf("boom")}
	sm.RegisterService(ok)
	sm.RegisterService(bad)

	err := sm.StartAll(context.Background())
	if err == nil {
		t.Fatalf("expected start failure")
	}
	if !ok.running {
		t.Fatalf("ok service running = false, want true")
	}
	if bad.running {
		t.Fatalf("bad service running = true, want false")
	}
}

func TestServiceManagerStopAllReturnsPartialFailure(t *testing.T) {
	sm := NewServiceManager("test")
	ok := &testService{name: "ok", workflow: "wf", kind: "test", running: true}
	bad := &testService{name: "bad", workflow: "wf", kind: "test", running: true, stopErr: fmt.Errorf("boom")}
	sm.RegisterService(ok)
	sm.RegisterService(bad)

	err := sm.StopAll(context.Background())
	if err == nil {
		t.Fatalf("expected stop failure")
	}
	if ok.running {
		t.Fatalf("ok service running = true, want false")
	}
	if !bad.running {
		t.Fatalf("bad service running = false, want still true after stop error")
	}
}

func TestServiceManagerGetServicesByWorkflowAndType(t *testing.T) {
	sm := NewServiceManager("test")
	sm.RegisterService(&testService{name: "match", workflow: "wf", kind: "kind"})
	sm.RegisterService(&testService{name: "wrong-workflow", workflow: "other", kind: "kind"})
	sm.RegisterService(&testService{name: "wrong-kind", workflow: "wf", kind: "other"})

	got := sm.GetServicesByWorkflowAndType(context.Background(), "wf", "kind")
	if len(got) != 1 || got[0].GetName() != "match" {
		t.Fatalf("matched services = %+v, want only match", got)
	}
}

func TestServiceManagerDuplicateRegistrationOverwritesService(t *testing.T) {
	sm := NewServiceManager("test")
	first := &testService{name: "same", workflow: "wf", kind: "old"}
	second := &testService{name: "same", workflow: "wf", kind: "new"}
	sm.RegisterService(first)
	sm.RegisterService(second)

	got, ok := sm.GetServiceByName(context.Background(), "same")
	if !ok {
		t.Fatalf("service not found")
	}
	if got.GetType() != "new" {
		t.Fatalf("service type = %q, want new", got.GetType())
	}
}

func TestServiceManagerHealthAndListMethods(t *testing.T) {
	sm := NewServiceManager("test")
	running := &testService{name: "running", workflow: "wf", kind: "kind", running: true}
	down := &testService{name: "down", workflow: "wf", kind: "kind", running: false}
	sm.RegisterService(running)
	sm.RegisterService(down)

	report, ok := sm.GetServiceHealth(context.Background(), "running")
	if !ok {
		t.Fatalf("running service health not found")
	}
	if report.Status != health.StatusUp {
		t.Fatalf("running status = %s, want UP", report.Status)
	}
	report, ok = sm.GetServiceHealth(context.Background(), "missing")
	if ok {
		t.Fatalf("missing service health found: %+v", report)
	}

	all := sm.GetAllServicesHealth(context.Background())
	if len(all) != 2 {
		t.Fatalf("all health len = %d, want 2", len(all))
	}
	system := sm.GetSystemHealth(context.Background())
	if system.Status != health.StatusDegraded {
		t.Fatalf("system status = %s, want DEGRADED", system.Status)
	}
	if system.Metadata["service_count"] != 2 {
		t.Fatalf("service_count = %v, want 2", system.Metadata["service_count"])
	}

	services := sm.ListServices(context.Background())
	if len(services) != 2 {
		t.Fatalf("services len = %d, want 2", len(services))
	}
}

func TestServiceManagerRegistrationAndSystemHealthBranches(t *testing.T) {
	sm := NewServiceManager("test")
	sm.RegisterService(nil)
	sm.RegisterService(&testService{name: "", workflow: "wf", kind: "kind"})

	if report := sm.GetSystemHealth(context.Background()); report.Status != health.StatusUnknown {
		t.Fatalf("empty system status = %s, want UNKNOWN", report.Status)
	}

	first := &testService{name: "first", workflow: "wf", kind: "kind", running: false}
	second := &testService{name: "second", workflow: "wf", kind: "kind", running: false}
	sm.RegisterService(first)
	sm.RegisterService(second)

	if report := sm.GetSystemHealth(context.Background()); report.Status != health.StatusDown {
		t.Fatalf("all-down system status = %s, want DOWN", report.Status)
	}

	if svc, ok := sm.GetServiceByName(context.Background(), "first"); !ok || svc != first {
		t.Fatalf("first service lookup = %v %v, want registered service", svc, ok)
	}
	if _, ok := sm.GetServiceByName(context.Background(), "missing"); ok {
		t.Fatalf("missing service lookup succeeded")
	}
}

func TestServiceManagerCheckServicesRestartsDownService(t *testing.T) {
	sm := NewServiceManager("test")
	down := &testService{name: "down", workflow: "wf", kind: "kind", running: false}
	sm.RegisterService(down)

	sm.checkServices(context.Background())

	if down.restarted != 1 {
		t.Fatalf("restart count = %d, want 1", down.restarted)
	}
	if !down.running {
		t.Fatalf("down service running = false, want true after restart")
	}
}

func TestServiceManagerMonitorServicesStopsOnContextCancel(t *testing.T) {
	sm := NewServiceManager("test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		sm.MonitorServices(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("monitor did not stop after context cancellation")
	}
}
