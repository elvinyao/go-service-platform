package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type testExecutor struct {
	name  string
	err   error
	calls int
}

func (e *testExecutor) Type() string {
	return e.name
}

func (e *testExecutor) Execute(ctx context.Context, message ruleengine.Message, action ruleengine.Action) error {
	e.calls++
	return e.err
}

type failingProvider struct {
	name string
	err  error
}

func (p failingProvider) Name() string {
	return p.name
}

func (p failingProvider) Start(ctx context.Context) error {
	return p.err
}

func (p failingProvider) Snapshot(ctx context.Context) ruleengine.RuleSet {
	return ruleengine.RuleSet{Source: p.name}
}

type mutableProvider struct {
	name     string
	snapshot ruleengine.RuleSet
}

func (p *mutableProvider) Name() string {
	return p.name
}

func (p *mutableProvider) Start(context.Context) error {
	return nil
}

func (p *mutableProvider) Snapshot(context.Context) ruleengine.RuleSet {
	return p.snapshot
}

func TestEngineProcessesActionsAndReportsPartialFailures(t *testing.T) {
	success := &testExecutor{name: "success"}
	failureCause := errors.New("action failed")
	failure := &testExecutor{name: "failure", err: failureCause}
	provider := ruleengine.NewStaticProvider("rules", ruleengine.RuleSet{Rules: []ruleengine.Rule{
		{
			ID:       "rule",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Actions: []ruleengine.Action{
				{ID: "success-action", Executor: "success"},
				{ID: "failure-action", Executor: "failure"},
			},
		},
	}})
	engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, []executor.Executor{success, failure})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{}); err == nil {
		t.Fatalf("process before start error = nil")
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("second start: %v", err)
	}

	plan, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{})
	if len(plan.Actions) != 2 || success.calls != 1 || failure.calls != 1 {
		t.Fatalf("plan/calls = %d/%d/%d", len(plan.Actions), success.calls, failure.calls)
	}
	if !IsExecutionError(err) || !errors.Is(err, failureCause) {
		t.Fatalf("process error = %v, want execution error wrapping cause", err)
	}
	var executionError *ExecutionError
	if !errors.As(err, &executionError) || len(executionError.Failures) != 1 {
		t.Fatalf("execution failures = %+v", executionError)
	}
	if got := executionError.Error(); !strings.Contains(got, "failure-action") || !strings.Contains(got, "action failed") {
		t.Fatalf("execution error text = %q", got)
	}

	snapshot := engine.Snapshot(context.Background())
	if len(snapshot.Executors) != 2 || len(snapshot.Composer.Providers) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestEngineValidatesProvidersAndExecutors(t *testing.T) {
	validProvider := ruleengine.NewStaticProvider("rules", ruleengine.RuleSet{})
	validExecutor := &testExecutor{name: "log"}
	var typedNilProvider *mutableProvider
	var typedNilExecutor *testExecutor
	tests := []struct {
		name      string
		config    ruleengine.EngineConfig
		providers []ruleengine.RuleProvider
		executors []executor.Executor
		want      string
	}{
		{name: "invalid config", config: ruleengine.EngineConfig{}, want: "validate engine config"},
		{name: "nil provider", config: testConfig("rules"), providers: []ruleengine.RuleProvider{nil}, want: "provider is nil"},
		{name: "typed nil provider", config: testConfig("rules"), providers: []ruleengine.RuleProvider{typedNilProvider}, want: "provider is nil"},
		{name: "empty provider name", config: testConfig("rules"), providers: []ruleengine.RuleProvider{failingProvider{}}, want: "provider name is empty"},
		{name: "missing provider", config: testConfig("missing"), providers: []ruleengine.RuleProvider{validProvider}, want: "not registered"},
		{name: "duplicate provider", config: testConfig("rules"), providers: []ruleengine.RuleProvider{validProvider, validProvider}, want: "more than once"},
		{name: "nil executor", config: testConfig("rules"), providers: []ruleengine.RuleProvider{validProvider}, executors: []executor.Executor{nil}, want: "executor is nil"},
		{name: "typed nil executor", config: testConfig("rules"), providers: []ruleengine.RuleProvider{validProvider}, executors: []executor.Executor{typedNilExecutor}, want: "executor is nil"},
		{name: "empty executor type", config: testConfig("rules"), providers: []ruleengine.RuleProvider{validProvider}, executors: []executor.Executor{&testExecutor{}}, want: "executor type is empty"},
		{name: "duplicate executor", config: testConfig("rules"), providers: []ruleengine.RuleProvider{validProvider}, executors: []executor.Executor{validExecutor, validExecutor}, want: "more than once"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.config, tt.providers, tt.executors)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("new error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEngineStartValidatesProviderState(t *testing.T) {
	t.Run("provider start failure", func(t *testing.T) {
		provider := failingProvider{name: "rules", err: errors.New("load failed")}
		engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		if err := engine.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "load failed") {
			t.Fatalf("start error = %v", err)
		}
	})

	t.Run("unregistered action executor", func(t *testing.T) {
		provider := ruleengine.NewStaticProvider("rules", ruleengine.RuleSet{Rules: []ruleengine.Rule{
			{ID: "rule", Enabled: true, Actions: []ruleengine.Action{{Executor: "missing"}}},
		}})
		engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		if err := engine.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "unregistered executor") {
			t.Fatalf("start error = %v", err)
		}
	})

	t.Run("invalid provider snapshot", func(t *testing.T) {
		provider := ruleengine.NewStaticProvider("rules", ruleengine.RuleSet{Rules: []ruleengine.Rule{
			{ID: "duplicate", Actions: []ruleengine.Action{{Executor: "log"}}},
			{ID: "duplicate", Actions: []ruleengine.Action{{Executor: "log"}}},
		}})
		engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, []executor.Executor{&testExecutor{name: "log"}})
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		if err := engine.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "published invalid rules") {
			t.Fatalf("start error = %v, want invalid rules error", err)
		}
	})

	t.Run("disabled rule may reference unavailable executor", func(t *testing.T) {
		provider := ruleengine.NewStaticProvider("rules", ruleengine.RuleSet{Rules: []ruleengine.Rule{
			{ID: "disabled", Enabled: false, Actions: []ruleengine.Action{{Executor: "missing"}}},
		}})
		engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		if err := engine.Start(context.Background()); err != nil {
			t.Fatalf("start engine with disabled rule: %v", err)
		}
	})
}

func TestEngineReportsExecutorMissingAfterProviderSnapshotChanges(t *testing.T) {
	provider := &mutableProvider{name: "rules"}
	engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	provider.snapshot = ruleengine.RuleSet{Rules: []ruleengine.Rule{
		{
			ID:       "late-rule",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Actions:  []ruleengine.Action{{ID: "late-action", Executor: "late-executor"}},
		},
	}}

	plan, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{})
	if len(plan.Actions) != 1 || !IsExecutionError(err) {
		t.Fatalf("plan/error = %+v/%v", plan, err)
	}
	var executionError *ExecutionError
	if !errors.As(err, &executionError) || len(executionError.Failures) != 1 {
		t.Fatalf("execution error = %+v", executionError)
	}
	if executionError.Failures[0].Executor != "late-executor" {
		t.Fatalf("failure executor = %q", executionError.Failures[0].Executor)
	}
}

func TestEngineRejectsInvalidProviderSnapshotAfterStart(t *testing.T) {
	provider := &mutableProvider{name: "rules"}
	engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	provider.snapshot = ruleengine.RuleSet{Rules: []ruleengine.Rule{{
		ID:      "late-invalid-rule",
		Enabled: true,
		Conditions: []ruleengine.Condition{{
			Field: "content",
			Op:    ruleengine.OpRegex,
			Value: "[",
		}},
		Actions: []ruleengine.Action{{Executor: "log"}},
	}}}

	plan, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{})
	if len(plan.Actions) != 0 || err == nil || !strings.Contains(err.Error(), "published invalid rules") {
		t.Fatalf("plan/error = %+v/%v, want empty plan and invalid rules error", plan, err)
	}
}

func TestEngineReturnsComposerErrors(t *testing.T) {
	provider := ruleengine.NewStaticProvider("rules", ruleengine.RuleSet{})
	engine, err := New(testConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	invalidConfig := testConfig("rules")
	invalidConfig.Workflows[0].Mode = "invalid-after-start"
	engine.composer = ruleengine.NewComposer(invalidConfig, map[string]ruleengine.RuleProvider{"rules": provider})
	if _, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{}); err == nil || !strings.Contains(err.Error(), "invalid composition mode") {
		t.Fatalf("composer error = %v", err)
	}
}

func testConfig(provider string) ruleengine.EngineConfig {
	return ruleengine.EngineConfig{
		Workflows: []ruleengine.WorkflowPolicy{
			{
				Name:      ruleengine.DefaultWorkflowName,
				Providers: []string{provider},
				Mode:      ruleengine.CompositionSingle,
				ActionMerge: ruleengine.ActionMergeConfig{
					Dedup: true,
					Order: ruleengine.ActionOrderPriority,
				},
			},
		},
		ActionMerge: ruleengine.ActionMergeConfig{Dedup: true, Order: ruleengine.ActionOrderPriority},
	}
}
