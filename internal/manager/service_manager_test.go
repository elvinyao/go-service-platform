package manager

import (
	"context"
	"fmt"
	"testing"
	"time"

	apperrors "github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/health"
)

type testService struct {
	name      string
	workflow  string
	kind      string
	startErr  error
	stopErr   error
	restarted int
	running   bool
	stopCalls int
}

func (s *testService) Start(ctx context.Context) error {
	if s.startErr != nil {
		return s.startErr
	}
	s.running = true
	return nil
}

func (s *testService) Stop(ctx context.Context) error {
	s.stopCalls++
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
func (s *testService) RegisterHealthChecks() []health.Checker { return nil }

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
	if ok.running {
		t.Fatalf("ok service running = true, want rollback to stop it")
	}
	if ok.stopCalls != 1 {
		t.Fatalf("ok service stop calls = %d, want 1 rollback", ok.stopCalls)
	}
	if bad.running {
		t.Fatalf("bad service running = true, want false")
	}
}

func TestServiceManagerStartAllReportsRollbackFailure(t *testing.T) {
	sm := NewServiceManager("test")
	rollbackFailure := &testService{name: "rollback-failure", workflow: "wf", kind: "test", stopErr: fmt.Errorf("stop boom")}
	startFailure := &testService{name: "start-failure", workflow: "wf", kind: "test", startErr: fmt.Errorf("start boom")}
	sm.RegisterService(rollbackFailure)
	sm.RegisterService(startFailure)

	err := sm.StartAll(context.Background())
	appErr, ok := err.(*apperrors.AppError)
	if !ok || appErr.Fields["rollback_failures"] == nil {
		t.Fatalf("start error = %v, want rollback failure details", err)
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

func TestServiceManagerDuplicateRegistrationIsRejected(t *testing.T) {
	sm := NewServiceManager("test")
	first := &testService{name: "same", workflow: "wf", kind: "old"}
	second := &testService{name: "same", workflow: "wf", kind: "new"}
	if err := sm.RegisterService(first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := sm.RegisterService(second); err == nil {
		t.Fatalf("duplicate registration error = nil")
	}

	got, ok := sm.GetServiceByName(context.Background(), "same")
	if !ok {
		t.Fatalf("service not found")
	}
	if got != first {
		t.Fatalf("registered service = %v, want first instance", got)
	}
}

func TestServiceManagerListMethods(t *testing.T) {
	sm := NewServiceManager("test")
	running := &testService{name: "running", workflow: "wf", kind: "kind", running: true}
	down := &testService{name: "down", workflow: "wf", kind: "kind", running: false}
	sm.RegisterService(running)
	sm.RegisterService(down)

	services := sm.ListServices(context.Background())
	if len(services) != 2 || services[0].GetName() != "down" || services[1].GetName() != "running" {
		t.Fatalf("services = %+v, want [down running]", services)
	}
}

func TestServiceManagerRegistrationAndLookupBranches(t *testing.T) {
	sm := NewServiceManager("test")
	if err := sm.RegisterService(nil); err == nil {
		t.Fatalf("nil service registration error = nil")
	}
	var typedNil *testService
	if err := sm.RegisterService(typedNil); err == nil {
		t.Fatalf("typed nil service registration error = nil")
	}
	if err := sm.RegisterService(&testService{name: "", workflow: "wf", kind: "kind"}); err == nil {
		t.Fatalf("empty service name registration error = nil")
	}

	first := &testService{name: "first", workflow: "wf", kind: "kind", running: false}
	sm.RegisterService(first)

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
