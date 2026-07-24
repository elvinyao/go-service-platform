package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/model"
	runtimeconfig "github.com/elvinyao/go-service-platform/pkg/config"
	coreexecutor "github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
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

func TestWorkflowEngineAdminSnapshot(t *testing.T) {
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

func TestNewWorkflowEngineHonorsRuntimeConfigForDemoProvidersAndExecutors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "rule-engine.yaml")
	rulesPath := filepath.Join(dir, "workflow-rules.yaml")

	writeFile(t, configPath, `
workflows:
  - name: WorkflowEngine
    providers: [yaml, confluence]
    mode: pipeline
    pipeline_order: [yaml, confluence]
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
          template: "handled {{.Type}}"
`)

	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Adapters.Confluence.Enabled = false
	cfg.Adapters.Mattermost.Enabled = false
	cfg.Adapters.BadgeDB.Enabled = false

	engine, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, rulesPath, cfg)
	if err != nil {
		t.Fatalf("new workflow engine: %v", err)
	}

	snapshot := engine.AdminSnapshot(context.Background())
	if _, ok := snapshot.Composer.Providers["yaml"]; !ok {
		t.Fatalf("yaml provider missing from snapshot: %+v", snapshot.Composer.Providers)
	}
	if _, ok := snapshot.Composer.Providers["confluence"]; ok {
		t.Fatalf("confluence provider should be disabled: %+v", snapshot.Composer.Providers)
	}
	if len(snapshot.Composer.Config.Workflows[0].Providers) != 1 || snapshot.Composer.Config.Workflows[0].Providers[0] != "yaml" {
		t.Fatalf("workflow providers = %+v, want [yaml]", snapshot.Composer.Config.Workflows[0].Providers)
	}
	if !slices.Contains(snapshot.Executors, "log") || !slices.Contains(snapshot.Executors, "http") {
		t.Fatalf("core executors missing: %+v", snapshot.Executors)
	}
	if slices.Contains(snapshot.Executors, "mattermost") {
		t.Fatalf("mattermost executor should be disabled: %+v", snapshot.Executors)
	}
	if slices.Contains(snapshot.Executors, "db") {
		t.Fatalf("db executor should be disabled: %+v", snapshot.Executors)
	}

	err = engine.ProcessMessage(context.Background(), model.Message{
		ID:        "m1",
		Type:      "AAA",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("process message with yaml-only runtime config: %v", err)
	}
}

func TestNewWorkflowEngineRegistersEnabledDemoCapabilities(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "rule-engine.yaml")
	writeFile(t, configPath, `workflows:
  - name: WorkflowEngine
    providers: [confluence]
    mode: single
`)

	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Adapters.Confluence.Enabled = true
	cfg.Adapters.Mattermost.Enabled = true
	cfg.Adapters.BadgeDB.Enabled = true

	engine, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, "", cfg)
	if err != nil {
		t.Fatalf("new workflow engine: %v", err)
	}
	snapshot := engine.AdminSnapshot(context.Background())
	if _, ok := snapshot.Composer.Providers["confluence"]; !ok {
		t.Fatalf("confluence provider missing: %+v", snapshot.Composer.Providers)
	}
	for _, executorType := range []string{"log", "http", "mattermost", "db"} {
		if !slices.Contains(snapshot.Executors, executorType) {
			t.Fatalf("executor %q missing: %+v", executorType, snapshot.Executors)
		}
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

func TestNewWorkflowEngineRejectsInvalidRuntimeConfig(t *testing.T) {
	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Admin.Address = "invalid"

	_, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), "", "", cfg)
	if err == nil || !strings.Contains(err.Error(), "invalid runtime config") {
		t.Fatalf("runtime config error = %v", err)
	}
}

func TestNewWorkflowEngineRejectsUnknownProviderAndDisabledExecutor(t *testing.T) {
	t.Run("unknown provider", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "rule-engine.yaml")
		rulesPath := filepath.Join(dir, "workflow-rules.yaml")
		writeFile(t, configPath, `workflows:
  - name: WorkflowEngine
    providers: [unknown]
    mode: single
`)
		writeFile(t, rulesPath, "version: test\n")

		_, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, rulesPath)
		if err == nil || !strings.Contains(err.Error(), "provider unknown is not registered") {
			t.Fatalf("provider error = %v", err)
		}
	})

	t.Run("only disabled provider", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "rule-engine.yaml")
		writeFile(t, configPath, `workflows:
  - name: WorkflowEngine
    providers: [confluence]
    mode: single
`)
		cfg := runtimeconfig.DefaultRuntimeConfig()
		cfg.Adapters.Confluence.Enabled = false

		_, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, "", cfg)
		if err == nil || !strings.Contains(err.Error(), "no enabled providers") {
			t.Fatalf("disabled provider error = %v", err)
		}
	})

	t.Run("disabled executor", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "rule-engine.yaml")
		rulesPath := filepath.Join(dir, "workflow-rules.yaml")
		writeFile(t, configPath, `workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
`)
		writeFile(t, rulesPath, `rules:
  - id: store
    actions:
      - executor: db
`)
		cfg := runtimeconfig.DefaultRuntimeConfig()
		cfg.Adapters.BadgeDB.Enabled = false

		_, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, rulesPath, cfg)
		if err == nil || !strings.Contains(err.Error(), "unregistered executor db") {
			t.Fatalf("executor error = %v", err)
		}
	})

	t.Run("unknown rule workflow", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "rule-engine.yaml")
		rulesPath := filepath.Join(dir, "workflow-rules.yaml")
		writeFile(t, configPath, `workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
`)
		writeFile(t, rulesPath, `rules:
  - id: typo
    workflow: WorkfloEngine
    actions:
      - executor: log
`)

		_, err := NewWorkflowEngine(
			context.Background(),
			manager.NewServiceManager("test"),
			configPath,
			rulesPath,
		)
		if err == nil || !strings.Contains(err.Error(), "has no configured policy") {
			t.Fatalf("workflow error = %v", err)
		}
	})

	t.Run("confluence requires mattermost executor", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "rule-engine.yaml")
		writeFile(t, configPath, `workflows:
  - name: WorkflowEngine
    providers: [confluence]
    mode: single
`)
		cfg := runtimeconfig.DefaultRuntimeConfig()
		cfg.Adapters.Confluence.Enabled = true
		cfg.Adapters.Mattermost.Enabled = false

		_, err := NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), configPath, "", cfg)
		if err == nil || !strings.Contains(err.Error(), "requires the mattermost executor") {
			t.Fatalf("adapter dependency error = %v", err)
		}
	})
}

func TestWorkflowEngineWrapsPipelineErrors(t *testing.T) {
	provider := ruleengine.NewStaticProvider("test", ruleengine.RuleSet{})
	engine, err := pipeline.New(testEngineConfig("test"), []ruleengine.RuleProvider{provider}, nil)
	if err != nil {
		t.Fatalf("new pipeline: %v", err)
	}
	workflowEngine := &WorkflowEngine{name: ruleengine.DefaultWorkflowName, engine: engine}

	err = workflowEngine.ProcessMessage(context.Background(), model.Message{ID: "m1"})
	if err == nil || !strings.Contains(err.Error(), "failed to process rule pipeline") {
		t.Fatalf("process error = %v", err)
	}
}

func TestRuntimeProviderFilteringHelpers(t *testing.T) {
	cfg := ruleengine.DefaultEngineConfig()
	cfg.Workflows[0].Providers = []string{"yaml", "confluence", "custom"}
	cfg.Workflows[0].PipelineOrder = []string{"yaml", "confluence", "custom"}
	runtimeCfg := runtimeconfig.DefaultRuntimeConfig()

	filtered, err := filterEngineConfigForRuntime(cfg, runtimeCfg)
	if err != nil {
		t.Fatalf("filter config: %v", err)
	}
	if got := filtered.Workflows[0].Providers; !slices.Equal(got, []string{"yaml", "custom"}) {
		t.Fatalf("filtered providers = %+v", got)
	}
	if !engineUsesProvider(filtered, "custom") || engineUsesProvider(filtered, "confluence") {
		t.Fatalf("provider detection failed for %+v", filtered.Workflows[0].Providers)
	}

	invalid := ruleengine.DefaultEngineConfig()
	invalid.ActionMerge.Order = "invalid"
	if _, err := filterEngineConfigForRuntime(invalid, runtimeCfg); err == nil || !strings.Contains(err.Error(), "validate filtered") {
		t.Fatalf("filtered validation error = %v", err)
	}
	if err := validateRuntimeAdapterDependencies(filtered, runtimeCfg); err != nil {
		t.Fatalf("unexpected adapter dependency error: %v", err)
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
	engine, err := pipeline.New(cfg, []ruleengine.RuleProvider{provider}, executors)
	if err != nil {
		t.Fatalf("new pipeline: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start pipeline: %v", err)
	}

	return &WorkflowEngine{
		name:   ruleengine.DefaultWorkflowName,
		engine: engine,
	}
}

func testEngineConfig(provider string) ruleengine.EngineConfig {
	cfg := ruleengine.DefaultEngineConfig()
	cfg.Workflows[0].Providers = []string{provider}
	cfg.Workflows[0].PipelineOrder = []string{provider}
	return cfg
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
