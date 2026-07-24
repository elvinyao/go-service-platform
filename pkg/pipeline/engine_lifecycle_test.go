package pipeline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type lifecycleProvider struct {
	name      string
	snapshot  ruleengine.RuleSet
	startErr  error
	stopErrs  []error
	events    *[]string
	startCall int
	stopCall  int
}

func (p *lifecycleProvider) Name() string {
	return p.name
}

func (p *lifecycleProvider) Start(context.Context) error {
	p.startCall++
	p.record("start provider " + p.name)
	return p.startErr
}

func (p *lifecycleProvider) Stop(context.Context) error {
	p.stopCall++
	p.record("stop provider " + p.name)
	return nextLifecycleError(&p.stopErrs)
}

func (p *lifecycleProvider) Snapshot(context.Context) ruleengine.RuleSet {
	return p.snapshot
}

func (p *lifecycleProvider) record(event string) {
	if p.events != nil {
		*p.events = append(*p.events, event)
	}
}

type lifecycleExecutor struct {
	name      string
	startErr  error
	stopErrs  []error
	events    *[]string
	startCall int
	stopCall  int
}

func (e *lifecycleExecutor) Type() string {
	return e.name
}

func (e *lifecycleExecutor) Start(context.Context) error {
	e.startCall++
	e.record("start executor " + e.name)
	return e.startErr
}

func (e *lifecycleExecutor) Stop(context.Context) error {
	e.stopCall++
	e.record("stop executor " + e.name)
	return nextLifecycleError(&e.stopErrs)
}

func (e *lifecycleExecutor) Execute(context.Context, ruleengine.Message, ruleengine.Action) error {
	return nil
}

func (e *lifecycleExecutor) record(event string) {
	if e.events != nil {
		*e.events = append(*e.events, event)
	}
}

type blockingLifecycleExecutor struct {
	lifecycleExecutor
	executing chan struct{}
	release   chan struct{}
}

func (e *blockingLifecycleExecutor) Execute(context.Context, ruleengine.Message, ruleengine.Action) error {
	close(e.executing)
	<-e.release
	return nil
}

type stopOnlyExecutor struct {
	testExecutor
	stopCalls int
}

func (e *stopOnlyExecutor) Stop(context.Context) error {
	e.stopCalls++
	return nil
}

func nextLifecycleError(errors *[]error) error {
	if len(*errors) == 0 {
		return nil
	}
	err := (*errors)[0]
	*errors = (*errors)[1:]
	return err
}

func TestEngineRollsBackProvidersWhenStartupFails(t *testing.T) {
	var events []string
	first := &lifecycleProvider{name: "a", events: &events}
	second := &lifecycleProvider{name: "b", startErr: errors.New("load failed"), events: &events}
	engine, err := New(lifecycleEngineConfig("a", "b"), []ruleengine.RuleProvider{second, first}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	err = engine.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "load failed") {
		t.Fatalf("start error = %v, want provider failure", err)
	}
	want := []string{
		"start provider a",
		"start provider b",
		"stop provider b",
		"stop provider a",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestEngineRollsBackAfterPreflightValidation(t *testing.T) {
	var events []string
	provider := &lifecycleProvider{
		name:   "rules",
		events: &events,
		snapshot: ruleengine.RuleSet{Rules: []ruleengine.Rule{{
			ID:       "bad-workflow",
			Workflow: "WorkfloEngine",
			Actions:  []ruleengine.Action{{Executor: "log"}},
		}}},
	}
	engine, err := New(
		lifecycleEngineConfig("rules"),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{&testExecutor{name: "log"}},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	err = engine.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "has no configured policy") {
		t.Fatalf("start error = %v, want workflow validation error", err)
	}
	want := []string{"start provider rules", "stop provider rules"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestEngineRollsBackLifecycleExecutorsInReverseOrder(t *testing.T) {
	var events []string
	provider := &lifecycleProvider{name: "rules", events: &events}
	first := &lifecycleExecutor{name: "a", events: &events}
	second := &lifecycleExecutor{name: "b", startErr: errors.New("connect failed"), events: &events}
	engine, err := New(
		lifecycleEngineConfig("rules"),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{second, first},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	err = engine.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "connect failed") {
		t.Fatalf("start error = %v, want executor failure", err)
	}
	want := []string{
		"start provider rules",
		"start executor a",
		"start executor b",
		"stop executor b",
		"stop executor a",
		"stop provider rules",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestEngineStopRetriesFailedComponentsAndAllowsRestart(t *testing.T) {
	var events []string
	stopErr := errors.New("busy")
	provider := &lifecycleProvider{name: "rules", events: &events}
	actionExecutor := &lifecycleExecutor{
		name:     "log",
		stopErrs: []error{stopErr, nil},
		events:   &events,
	}
	engine, err := New(
		lifecycleEngineConfig("rules"),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{actionExecutor},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(nil); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	if err := engine.Stop(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("first stop error = %v, want %v", err, stopErr)
	}
	if _, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{}); err == nil {
		t.Fatalf("process after failed stop error = nil")
	}
	if err := engine.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "pending cleanup") {
		t.Fatalf("start before cleanup error = %v", err)
	}
	if err := engine.Stop(nil); err != nil {
		t.Fatalf("retry stop: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("restart engine: %v", err)
	}
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("final stop: %v", err)
	}

	if provider.startCall != 2 || provider.stopCall != 2 {
		t.Fatalf("provider calls = start:%d stop:%d, want 2/2", provider.startCall, provider.stopCall)
	}
	if actionExecutor.startCall != 2 || actionExecutor.stopCall != 3 {
		t.Fatalf("executor calls = start:%d stop:%d, want 2/3", actionExecutor.startCall, actionExecutor.stopCall)
	}
}

func TestEngineStopsExecutorWithoutStarter(t *testing.T) {
	provider := &lifecycleProvider{name: "rules"}
	actionExecutor := &stopOnlyExecutor{testExecutor: testExecutor{name: "log"}}
	engine, err := New(
		lifecycleEngineConfig("rules"),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{actionExecutor},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("stop engine: %v", err)
	}
	if actionExecutor.stopCalls != 1 {
		t.Fatalf("stop calls = %d, want 1", actionExecutor.stopCalls)
	}
}

func TestEngineStopWaitsForInFlightProcess(t *testing.T) {
	provider := &lifecycleProvider{
		name: "rules",
		snapshot: ruleengine.RuleSet{Rules: []ruleengine.Rule{{
			ID:       "rule",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Actions:  []ruleengine.Action{{Executor: "blocking"}},
		}}},
	}
	actionExecutor := &blockingLifecycleExecutor{
		lifecycleExecutor: lifecycleExecutor{name: "blocking"},
		executing:         make(chan struct{}),
		release:           make(chan struct{}),
	}
	engine, err := New(
		lifecycleEngineConfig("rules"),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{actionExecutor},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	processDone := make(chan error, 1)
	go func() {
		_, processErr := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{})
		processDone <- processErr
	}()
	<-actionExecutor.executing

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- engine.Stop(context.Background())
	}()
	select {
	case err := <-stopDone:
		t.Fatalf("stop returned before process completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(actionExecutor.release)
	if err := <-processDone; err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := <-stopDone; err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestEngineRejectsDynamicWorkflowMismatch(t *testing.T) {
	provider := &mutableProvider{name: "rules"}
	engine, err := New(lifecycleEngineConfig("rules"), []ruleengine.RuleProvider{provider}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}
	provider.snapshot = ruleengine.RuleSet{Rules: []ruleengine.Rule{{
		ID:       "late-rule",
		Workflow: "typo",
		Actions:  []ruleengine.Action{{Executor: "log"}},
	}}}

	_, err = engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{})
	if err == nil || !strings.Contains(err.Error(), "has no configured policy") {
		t.Fatalf("process error = %v, want workflow validation error", err)
	}
}

func lifecycleEngineConfig(providerNames ...string) ruleengine.EngineConfig {
	return ruleengine.EngineConfig{
		Workflows: []ruleengine.WorkflowPolicy{{
			Name:      ruleengine.DefaultWorkflowName,
			Providers: append([]string(nil), providerNames...),
			Mode:      ruleengine.CompositionOr,
			ActionMerge: ruleengine.ActionMergeConfig{
				Dedup: true,
				Order: ruleengine.ActionOrderPriority,
			},
		}},
		ActionMerge: ruleengine.ActionMergeConfig{
			Dedup: true,
			Order: ruleengine.ActionOrderPriority,
		},
	}
}
