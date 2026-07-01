package manager

import (
	"context"
	"fmt"
	"testing"

	"project/internal/interfaces"
	"project/internal/model"
)

type testWorkflow struct {
	name       string
	err        error
	calls      int
	lastMsg    model.Message
	registered bool
}

func (w *testWorkflow) GetName() string {
	return w.name
}

func (w *testWorkflow) ProcessMessage(ctx context.Context, msg model.Message) error {
	w.calls++
	w.lastMsg = msg
	return w.err
}

func TestWorkflowManagerDispatchMessageReturnsErrorWhenNoWorkflowsRegistered(t *testing.T) {
	wm := NewWorkflowManager()

	err := wm.DispatchMessage(context.Background(), model.Message{ID: "m1", Type: "AAA"})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWorkflowManagerDispatchMessageReturnsErrorWhenAllWorkflowsFail(t *testing.T) {
	wm := NewWorkflowManager()
	first := &testWorkflow{name: "first", err: fmt.Errorf("first failed")}
	second := &testWorkflow{name: "second", err: fmt.Errorf("second failed")}
	if err := wm.RegisterWorkflow(first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := wm.RegisterWorkflow(second); err != nil {
		t.Fatalf("register second: %v", err)
	}

	err := wm.DispatchMessage(context.Background(), model.Message{ID: "m1", Type: "AAA"})
	if err == nil {
		t.Fatalf("expected all workflows failed error")
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("calls = first:%d second:%d, want both 1", first.calls, second.calls)
	}
}

func TestWorkflowManagerDispatchMessageStopsAfterFirstSuccessfulWorkflow(t *testing.T) {
	wm := NewWorkflowManager()
	first := &testWorkflow{name: "first"}
	second := &testWorkflow{name: "second"}
	if err := wm.RegisterWorkflow(first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := wm.RegisterWorkflow(second); err != nil {
		t.Fatalf("register second: %v", err)
	}

	err := wm.DispatchMessage(context.Background(), model.Message{ID: "m1", Type: "AAA", Content: "hello"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if first.calls+second.calls != 1 {
		t.Fatalf("total calls = %d, want 1", first.calls+second.calls)
	}
	called := first
	if second.calls == 1 {
		called = second
	}
	if called.lastMsg.Content != "hello" {
		t.Fatalf("last content = %q, want hello", called.lastMsg.Content)
	}
}

func TestWorkflowManagerRegisterWorkflowRejectsNilAndEmptyName(t *testing.T) {
	wm := NewWorkflowManager()
	if err := wm.RegisterWorkflow(nil); err == nil {
		t.Fatalf("expected nil workflow error")
	}
	if err := wm.RegisterWorkflow(&testWorkflow{}); err == nil {
		t.Fatalf("expected empty workflow name error")
	}
}

func TestWorkflowManagerDuplicateRegistrationOverwritesWorkflow(t *testing.T) {
	wm := NewWorkflowManager()
	first := &testWorkflow{name: "same", err: fmt.Errorf("old")}
	second := &testWorkflow{name: "same"}
	if err := wm.RegisterWorkflow(first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := wm.RegisterWorkflow(second); err != nil {
		t.Fatalf("register second: %v", err)
	}

	got, ok := wm.GetWorkflow("same")
	if !ok {
		t.Fatalf("workflow not found")
	}
	if got != second {
		t.Fatalf("duplicate registration did not overwrite")
	}
}

func TestNewWorkflowManagerWithDIRegistersFactoryWorkflows(t *testing.T) {
	sm := NewServiceManager("test")
	wf := &testWorkflow{name: "factory"}

	wm, err := NewWorkflowManagerWithDI(context.Background(), sm, func(serviceManager *ServiceManager) (interfaces.Workflow, error) {
		if serviceManager != sm {
			t.Fatalf("service manager was not passed to factory")
		}
		return wf, nil
	})
	if err != nil {
		t.Fatalf("new workflow manager with DI: %v", err)
	}

	got, err := wm.GetWorkflowByName(context.Background(), "factory")
	if err != nil {
		t.Fatalf("get workflow by name: %v", err)
	}
	if got != wf {
		t.Fatalf("got workflow = %v, want factory workflow", got)
	}
}

func TestNewWorkflowManagerWithDIReturnsFactoryError(t *testing.T) {
	_, err := NewWorkflowManagerWithDI(context.Background(), NewServiceManager("test"), func(serviceManager *ServiceManager) (interfaces.Workflow, error) {
		return nil, fmt.Errorf("factory failed")
	})
	if err == nil {
		t.Fatalf("expected factory error")
	}
}

func TestWorkflowManagerLookupAndListMethods(t *testing.T) {
	wm := NewWorkflowManager()
	first := &testWorkflow{name: "first"}
	second := &testWorkflow{name: "second"}
	if err := wm.RegisterWorkflow(first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := wm.RegisterWorkflow(second); err != nil {
		t.Fatalf("register second: %v", err)
	}

	if _, err := wm.GetWorkflowByName(context.Background(), ""); err == nil {
		t.Fatalf("expected empty name error")
	}
	if _, err := wm.GetWorkflowByName(context.Background(), "missing"); err == nil {
		t.Fatalf("expected missing workflow error")
	}
	names := wm.ListWorkflows(context.Background())
	if len(names) != 2 {
		t.Fatalf("names len = %d, want 2", len(names))
	}
	all := wm.GetAllWorkflows(context.Background())
	if len(all) != 2 || all["first"] != first || all["second"] != second {
		t.Fatalf("all workflows = %+v", all)
	}
}
