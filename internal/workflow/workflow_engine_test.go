package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/manager"
	"project/internal/model"
	coreexecutor "project/pkg/executor"
	"project/pkg/ruleengine"
)

type recordingWorkflowExecutor struct {
	executorType string
	err          error

	mu      sync.Mutex
	records []workflowExecutionRecord
}

type workflowExecutionRecord struct {
	ActionID string
	Type     string
	Content  string
	UserID   string
}

func (e *recordingWorkflowExecutor) Type() string {
	return e.executorType
}

func (e *recordingWorkflowExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	e.mu.Lock()
	e.records = append(e.records, workflowExecutionRecord{
		ActionID: action.ID,
		Type:     msg.Type,
		Content:  msg.Content,
		UserID:   msg.UserID,
	})
	e.mu.Unlock()

	return e.err
}

func (e *recordingWorkflowExecutor) Records() []workflowExecutionRecord {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]workflowExecutionRecord, len(e.records))
	copy(out, e.records)
	return out
}

func TestWorkflowEngineProcessMessageExecutesMatchedActionsInPriorityOrder(t *testing.T) {
	recorder := &recordingWorkflowExecutor{executorType: "record"}
	engine := newTestWorkflowEngine(t, []ruleengine.Rule{
		{
			ID:       "rule-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Priority: 100,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AAA"},
			},
			Actions: []ruleengine.Action{
				{ID: "second", Executor: "record", Priority: 200},
				{ID: "first", Executor: "record", Priority: 100},
			},
		},
	}, recorder)

	err := engine.ProcessMessage(context.Background(), model.Message{
		ID:        "m1",
		Type:      "AAA",
		Content:   "hello",
		UserID:    "u1",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("process message: %v", err)
	}

	records := recorder.Records()
	if len(records) != 2 {
		t.Fatalf("records len = %d, want 2", len(records))
	}
	if records[0].ActionID != "first" || records[1].ActionID != "second" {
		t.Fatalf("action order = [%s %s], want [first second]", records[0].ActionID, records[1].ActionID)
	}
	if records[0].Type != "AAA" || records[0].Content != "hello" || records[0].UserID != "u1" {
		t.Fatalf("recorded message = %+v", records[0])
	}
}

func TestWorkflowEngineProcessMessageReturnsNilWhenNoRulesMatch(t *testing.T) {
	recorder := &recordingWorkflowExecutor{executorType: "record"}
	engine := newTestWorkflowEngine(t, []ruleengine.Rule{
		{
			ID:       "rule-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AAA"},
			},
			Actions: []ruleengine.Action{{ID: "record", Executor: "record"}},
		},
	}, recorder)

	err := engine.ProcessMessage(context.Background(), model.Message{ID: "m1", Type: "BBB"})
	if err != nil {
		t.Fatalf("process message: %v", err)
	}
	if got := len(recorder.Records()); got != 0 {
		t.Fatalf("records len = %d, want 0", got)
	}
}

func TestWorkflowEngineProcessMessageReportsMissingExecutor(t *testing.T) {
	engine := newTestWorkflowEngine(t, []ruleengine.Rule{
		{
			ID:       "rule-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AAA"},
			},
			Actions: []ruleengine.Action{{ID: "missing-action", Executor: "missing"}},
		},
	})

	err := engine.ProcessMessage(context.Background(), model.Message{ID: "m1", Type: "AAA"})
	if err == nil {
		t.Fatalf("process message error = nil, want partial failure")
	}
	if !strings.Contains(err.Error(), "partial failures") {
		t.Fatalf("error = %q, want partial failure", err.Error())
	}
}

func TestWorkflowEngineProcessMessageContinuesAfterExecutorFailure(t *testing.T) {
	failing := &recordingWorkflowExecutor{executorType: "fail", err: errors.New("boom")}
	success := &recordingWorkflowExecutor{executorType: "success"}
	engine := newTestWorkflowEngine(t, []ruleengine.Rule{
		{
			ID:       "rule-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AAA"},
			},
			Actions: []ruleengine.Action{
				{ID: "fails", Executor: "fail", Priority: 100},
				{ID: "succeeds", Executor: "success", Priority: 200},
			},
		},
	}, failing, success)

	err := engine.ProcessMessage(context.Background(), model.Message{ID: "m1", Type: "AAA"})
	if err == nil {
		t.Fatalf("process message error = nil, want partial failure")
	}
	if len(failing.Records()) != 1 {
		t.Fatalf("failing executor records len = %d, want 1", len(failing.Records()))
	}
	if len(success.Records()) != 1 {
		t.Fatalf("success executor records len = %d, want 1", len(success.Records()))
	}
}

func TestWorkflowManagerDispatchMessageRunsWorkflowEngineIntegration(t *testing.T) {
	recorder := &recordingWorkflowExecutor{executorType: "record"}
	engine := newTestWorkflowEngine(t, []ruleengine.Rule{
		{
			ID:       "rule-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AAA"},
			},
			Actions: []ruleengine.Action{{ID: "record", Executor: "record"}},
		},
	}, recorder)

	workflowManager := manager.NewWorkflowManager()
	if err := workflowManager.RegisterWorkflow(engine); err != nil {
		t.Fatalf("register workflow: %v", err)
	}

	err := workflowManager.DispatchMessage(context.Background(), model.Message{
		ID:      "m1",
		Type:    "AAA",
		Content: "integration",
		UserID:  "u1",
	})
	if err != nil {
		t.Fatalf("dispatch message: %v", err)
	}

	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	if records[0].Content != "integration" {
		t.Fatalf("recorded content = %q, want integration", records[0].Content)
	}
}

func TestWorkflowEngineAdminSnapshotAndFactories(t *testing.T) {
	recorder := &recordingWorkflowExecutor{executorType: "record"}
	engine := newTestWorkflowEngine(t, []ruleengine.Rule{
		{
			ID:       "rule-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Actions:  []ruleengine.Action{{ID: "record", Executor: "record"}},
		},
	}, recorder)

	snapshot := engine.AdminSnapshot(context.Background())
	if snapshot.Name != ruleengine.DefaultWorkflowName {
		t.Fatalf("snapshot name = %q", snapshot.Name)
	}
	if len(snapshot.Executors) != 1 || snapshot.Executors[0] != "record" {
		t.Fatalf("executors = %+v", snapshot.Executors)
	}
	if _, ok := snapshot.Composer.Providers["test"]; !ok {
		t.Fatalf("snapshot providers = %+v", snapshot.Composer.Providers)
	}

	factories := RegisterWorkflowFactories()
	if len(factories) != 1 {
		t.Fatalf("factories len = %d, want 1", len(factories))
	}
}

func TestNewWorkflowEngineLoadsConfiguredProvidersAndExecutors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "rule-engine.yaml")
	rulesPath := filepath.Join(dir, "workflow-rules.yaml")

	writeFile(t, configPath, `
workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
    action_merge:
      dedup: true
      order: priority
action_merge:
  dedup: true
  order: priority
`)
	writeFile(t, rulesPath, `
version: "test"
rules:
  - id: log-rule
    workflow: WorkflowEngine
    enabled: true
    priority: 10
    conditions:
      - field: type
        op: eq
        value: AAA
    actions:
      - id: log-action
        executor: log
        params:
          message: "handled {{.Type}}"
`)

	engine, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, rulesPath)
	if err != nil {
		t.Fatalf("new workflow engine: %v", err)
	}
	if engine.GetName() != ruleengine.DefaultWorkflowName {
		t.Fatalf("name = %q", engine.GetName())
	}

	snapshot := engine.AdminSnapshot(context.Background())
	if _, ok := snapshot.Composer.Providers["yaml"]; !ok {
		t.Fatalf("yaml provider missing from snapshot: %+v", snapshot.Composer.Providers)
	}
	if len(snapshot.Composer.Providers["yaml"].Rules) != 1 {
		t.Fatalf("yaml rules len = %d, want 1", len(snapshot.Composer.Providers["yaml"].Rules))
	}

	err = engine.ProcessMessage(context.Background(), model.Message{
		ID:        "m1",
		Type:      "AAA",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("process message with configured log executor: %v", err)
	}
}

func TestNewWorkflowEngineReturnsConfigError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad-rule-engine.yaml")
	writeFile(t, configPath, "workflows: [")

	_, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, "")
	if err == nil {
		t.Fatalf("expected config load error")
	}
}

func TestCreateWorkflowEngineFactoryReturnsWorkflow(t *testing.T) {
	created, err := CreateWorkflowEngine(manager.NewServiceManager("test"))
	if err != nil {
		t.Fatalf("create workflow engine: %v", err)
	}
	if created.GetName() != ruleengine.DefaultWorkflowName {
		t.Fatalf("created workflow name = %q", created.GetName())
	}
}

func newTestWorkflowEngine(t *testing.T, rules []ruleengine.Rule, executors ...coreexecutor.Executor) *WorkflowEngine {
	t.Helper()

	cfg := ruleengine.EngineConfig{
		Workflows: []ruleengine.WorkflowPolicy{
			{
				Name:      ruleengine.DefaultWorkflowName,
				Providers: []string{"test"},
				Mode:      ruleengine.CompositionSingle,
				ActionMerge: ruleengine.ActionMergeConfig{
					Dedup: true,
					Order: ruleengine.ActionOrderPriority,
				},
			},
		},
		ActionMerge: ruleengine.ActionMergeConfig{
			Dedup: true,
			Order: ruleengine.ActionOrderPriority,
		},
	}
	provider := ruleengine.NewStaticProvider("test", ruleengine.RuleSet{Rules: rules})
	registry := coreexecutor.NewRegistry()
	for _, executor := range executors {
		if err := registry.Register(executor); err != nil {
			t.Fatalf("register executor: %v", err)
		}
	}

	return &WorkflowEngine{
		name:      ruleengine.DefaultWorkflowName,
		composer:  ruleengine.NewComposer(cfg, map[string]ruleengine.RuleProvider{"test": provider}),
		executors: registry,
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
