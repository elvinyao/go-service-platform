package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type funcExecutor struct {
	calls atomic.Int32
	fn    func(ctx context.Context, call int) error
}

func (e *funcExecutor) Type() string { return "fn" }

func (e *funcExecutor) Execute(ctx context.Context, _ ruleengine.Message, _ ruleengine.Action) error {
	return e.fn(ctx, int(e.calls.Add(1)))
}

func newExecutionEngine(t *testing.T, execution ruleengine.ExecutionConfig, action ruleengine.Action, exec executor.Executor) *Engine {
	t.Helper()
	action.Executor = exec.Type()
	if action.ID == "" {
		action.ID = "a1"
	}
	config := ruleengine.DefaultEngineConfig()
	config.Workflows[0].Providers = []string{"local"}
	config.Workflows[0].PipelineOrder = []string{"local"}
	config.Execution = execution
	provider := ruleengine.NewStaticProvider("local", ruleengine.RuleSet{Rules: []ruleengine.Rule{{
		ID:       "r1",
		Workflow: ruleengine.DefaultWorkflowName,
		Enabled:  true,
		Actions:  []ruleengine.Action{action},
	}}})
	engine, err := New(config, []ruleengine.RuleProvider{provider}, []executor.Executor{exec})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return engine
}

func processOne(t *testing.T, engine *Engine, ctx context.Context) *ExecutionError {
	t.Helper()
	_, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, ruleengine.Message{ID: "m1"})
	if err == nil {
		return nil
	}
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("error = %v, want ExecutionError", err)
	}
	return executionErr
}

func TestEngineRetriesUntilSuccess(t *testing.T) {
	exec := &funcExecutor{fn: func(_ context.Context, call int) error {
		if call < 3 {
			return errors.New("transient")
		}
		return nil
	}}
	engine := newExecutionEngine(t,
		ruleengine.ExecutionConfig{Retry: ruleengine.RetryPolicy{MaxAttempts: 3, Backoff: "1ms", MaxBackoff: "1ms"}},
		ruleengine.Action{}, exec)

	if failure := processOne(t, engine, context.Background()); failure != nil {
		t.Fatalf("unexpected failure: %v", failure)
	}
	if got := exec.calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestEngineReportsAttemptsWhenRetriesAreExhausted(t *testing.T) {
	exec := &funcExecutor{fn: func(context.Context, int) error { return errors.New("still failing") }}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{},
		ruleengine.Action{Retry: &ruleengine.RetryPolicy{MaxAttempts: 2}}, exec)

	failure := processOne(t, engine, context.Background())
	if failure == nil || len(failure.Failures) != 1 || failure.Failures[0].Attempts != 2 {
		t.Fatalf("failure = %+v, want one failure after 2 attempts", failure)
	}
	if got := exec.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

func TestEngineDoesNotRetryByDefaultOrOnPermanentErrors(t *testing.T) {
	transient := &funcExecutor{fn: func(context.Context, int) error { return errors.New("boom") }}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{}, ruleengine.Action{}, transient)
	if failure := processOne(t, engine, context.Background()); failure == nil || failure.Failures[0].Attempts != 1 {
		t.Fatalf("failure = %+v, want a single attempt", failure)
	}

	permanent := &funcExecutor{fn: func(context.Context, int) error { return executor.Permanent(errors.New("bad params")) }}
	engine = newExecutionEngine(t,
		ruleengine.ExecutionConfig{Retry: ruleengine.RetryPolicy{MaxAttempts: 5}}, ruleengine.Action{}, permanent)
	if failure := processOne(t, engine, context.Background()); failure == nil || failure.Failures[0].Attempts != 1 {
		t.Fatalf("failure = %+v, want a single attempt", failure)
	}
	if got := permanent.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestEngineAppliesPerAttemptTimeout(t *testing.T) {
	exec := &funcExecutor{fn: func(ctx context.Context, _ int) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{Timeout: "1h"},
		ruleengine.Action{Timeout: "5ms", Retry: &ruleengine.RetryPolicy{MaxAttempts: 2}}, exec)

	failure := processOne(t, engine, context.Background())
	if failure == nil || !errors.Is(failure, context.DeadlineExceeded) {
		t.Fatalf("failure = %v, want deadline exceeded", failure)
	}
	if got := exec.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 (timeouts are retried)", got)
	}
}

func TestEngineStopsRetryingWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	exec := &funcExecutor{fn: func(context.Context, int) error {
		cancel()
		return errors.New("transient")
	}}
	engine := newExecutionEngine(t,
		ruleengine.ExecutionConfig{Retry: ruleengine.RetryPolicy{MaxAttempts: 5, Backoff: "1h"}}, ruleengine.Action{}, exec)

	if failure := processOne(t, engine, ctx); failure == nil || failure.Failures[0].Attempts != 1 {
		t.Fatalf("failure = %+v, want a single attempt", failure)
	}
}

func TestEngineStopsBackoffWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	exec := &funcExecutor{fn: func(context.Context, int) error {
		time.AfterFunc(5*time.Millisecond, cancel)
		return errors.New("transient")
	}}
	engine := newExecutionEngine(t,
		ruleengine.ExecutionConfig{Retry: ruleengine.RetryPolicy{MaxAttempts: 5, Backoff: "1h"}}, ruleengine.Action{}, exec)

	if failure := processOne(t, engine, ctx); failure == nil || failure.Failures[0].Attempts != 1 {
		t.Fatalf("failure = %+v, want a single attempt", failure)
	}
}

func TestEngineConvertsExecutorPanicToFailure(t *testing.T) {
	exec := &funcExecutor{fn: func(context.Context, int) error { panic("kaboom") }}
	engine := newExecutionEngine(t,
		ruleengine.ExecutionConfig{Retry: ruleengine.RetryPolicy{MaxAttempts: 3}}, ruleengine.Action{}, exec)

	failure := processOne(t, engine, context.Background())
	if failure == nil || !strings.Contains(failure.Error(), "executor panicked: kaboom") {
		t.Fatalf("failure = %v, want panic failure", failure)
	}
	if got := exec.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (panics are not retried)", got)
	}
}

func TestEngineReportsInvalidDynamicExecutionSettings(t *testing.T) {
	exec := &funcExecutor{fn: func(context.Context, int) error { return nil }}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{}, ruleengine.Action{}, exec)

	attempts, err := engine.executeAction(context.Background(), exec, ruleengine.Message{}, ruleengine.Action{Timeout: "soon"})
	if err == nil || attempts != 0 || !executor.IsPermanent(err) {
		t.Fatalf("attempts = %d err = %v, want permanent resolution error", attempts, err)
	}
}
