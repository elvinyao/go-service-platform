package ruleengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestYAMLProviderLoadsEnabledRulesWithDefaultWorkflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	data := []byte(`version: "1"
rules:
  - id: log_aaa
    enabled: true
    priority: 10
    conditions:
      - field: type
        op: eq
        value: AAA
    actions:
      - id: log
        executor: log
        priority: 10
        params:
          level: info
          template: "type={{.Type}} id={{.ID}}"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write rules file: %v", err)
	}

	provider := NewYAMLProvider("yaml", path, "WorkflowEngine")
	if err := provider.Start(context.Background()); err != nil {
		t.Fatalf("start provider: %v", err)
	}

	snapshot := provider.Snapshot(context.Background())
	if len(snapshot.Rules) != 1 {
		t.Fatalf("rules length = %d, want 1", len(snapshot.Rules))
	}
	if snapshot.Rules[0].Workflow != "WorkflowEngine" {
		t.Fatalf("workflow = %q, want WorkflowEngine", snapshot.Rules[0].Workflow)
	}
}

func TestComposerBuildsPriorityOrderedDeduplicatedPlan(t *testing.T) {
	now := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	provider := NewStaticProvider("static", RuleSet{
		Source:   "static",
		Version:  "1",
		LoadedAt: now,
		Rules: []Rule{
			{
				ID:         "rule_b",
				Workflow:   "WorkflowEngine",
				Enabled:    true,
				Priority:   20,
				Conditions: []Condition{{Field: "type", Op: OpEq, Value: "AAA"}},
				Actions:    []Action{{ID: "same", Executor: "log", Priority: 20}},
			},
			{
				ID:         "rule_a",
				Workflow:   "WorkflowEngine",
				Enabled:    true,
				Priority:   10,
				Conditions: []Condition{{Field: "content", Op: OpContains, Value: "hello"}},
				Actions:    []Action{{ID: "same", Executor: "log", Priority: 10}},
			},
		},
	})

	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{{
			Name:      "WorkflowEngine",
			Providers: []string{"static"},
			Mode:      CompositionOr,
			ActionMerge: ActionMergeConfig{
				Dedup: true,
				Order: ActionOrderPriority,
			},
		}},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{"static": provider})
	plan, err := composer.BuildExecutionPlan(context.Background(), "WorkflowEngine", Message{
		ID: "m1", Type: "AAA", Content: "hello", Timestamp: now,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(plan.Rules) != 2 {
		t.Fatalf("rules length = %d, want 2", len(plan.Rules))
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(plan.Actions))
	}
	if plan.Actions[0].ID != "same" {
		t.Fatalf("action id = %q, want same", plan.Actions[0].ID)
	}
}
