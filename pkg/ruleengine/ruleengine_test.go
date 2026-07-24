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

func TestStaticProviderSnapshotsDeepCloneRuleValues(t *testing.T) {
	headers := map[string]interface{}{"X-Test": "original"}
	values := []interface{}{"first", map[string]interface{}{"nested": "original"}}
	provider := NewStaticProvider("static", RuleSet{Rules: []Rule{{
		ID:         "r1",
		Conditions: []Condition{{Field: "metadata", Op: OpEq, Value: values}},
		Actions: []Action{{
			ID:       "a1",
			Executor: "http",
			Params:   map[string]interface{}{"headers": headers},
		}},
	}}})

	headers["X-Test"] = "input changed"
	values[1].(map[string]interface{})["nested"] = "input changed"
	first := provider.Snapshot(context.Background())
	if got := first.Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"]; got != "original" {
		t.Fatalf("header after input mutation = %v, want original", got)
	}
	if got := first.Rules[0].Conditions[0].Value.([]interface{})[1].(map[string]interface{})["nested"]; got != "original" {
		t.Fatalf("condition after input mutation = %v, want original", got)
	}

	first.Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"] = "snapshot changed"
	first.Rules[0].Conditions[0].Value.([]interface{})[1].(map[string]interface{})["nested"] = "snapshot changed"
	second := provider.Snapshot(context.Background())
	if got := second.Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"]; got != "original" {
		t.Fatalf("header after snapshot mutation = %v, want original", got)
	}
	if got := second.Rules[0].Conditions[0].Value.([]interface{})[1].(map[string]interface{})["nested"]; got != "original" {
		t.Fatalf("condition after snapshot mutation = %v, want original", got)
	}
}

func TestComposerClonesCallerOwnedConfig(t *testing.T) {
	config := DefaultEngineConfig()
	provider := NewStaticProvider("yaml", RuleSet{})
	composer := NewComposer(config, map[string]RuleProvider{"yaml": provider})

	config.Workflows[0].Name = "mutated"
	config.Workflows[0].Providers[0] = "mutated"
	config.Workflows[0].PipelineOrder[0] = "mutated"

	snapshot := composer.Snapshot(context.Background())
	workflow := snapshot.Config.Workflows[0]
	if workflow.Name != DefaultWorkflowName ||
		workflow.Providers[0] != "yaml" ||
		workflow.PipelineOrder[0] != "yaml" {
		t.Fatalf("composer config changed through caller mutation: %+v", workflow)
	}
	if _, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{}); err != nil {
		t.Fatalf("build plan after caller mutation: %v", err)
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
