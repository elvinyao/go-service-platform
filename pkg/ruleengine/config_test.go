package ruleengine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEngineConfigReturnsDefaultForEmptyOrMissingPath(t *testing.T) {
	cfg, err := LoadEngineConfig("")
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	assertYAMLFirstDefault(t, cfg)

	cfg, err = LoadEngineConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load missing config: %v", err)
	}
	assertYAMLFirstDefault(t, cfg)
}

func TestLoadEngineConfigNormalizesWorkflowDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-engine.yaml")
	data := []byte(`workflows:
  - {}
action_merge:
  dedup: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadEngineConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	wf := cfg.Workflows[0]
	if wf.Name != DefaultWorkflowName {
		t.Fatalf("name = %q", wf.Name)
	}
	if wf.Mode != CompositionPipeline {
		t.Fatalf("mode = %q", wf.Mode)
	}
	if len(wf.Providers) != 1 || wf.Providers[0] != "yaml" {
		t.Fatalf("providers = %+v", wf.Providers)
	}
	if !wf.ActionMerge.Dedup || wf.ActionMerge.Order != ActionOrderPriority {
		t.Fatalf("action merge = %+v", wf.ActionMerge)
	}
}

func TestLoadEngineConfigReturnsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(":"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadEngineConfig(path)
	if err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestPolicyForWorkflowFallbacks(t *testing.T) {
	cfg := EngineConfig{}
	if got := cfg.PolicyForWorkflow("missing"); got.Name != DefaultWorkflowName {
		t.Fatalf("fallback policy = %+v", got)
	}

	cfg = EngineConfig{Workflows: []WorkflowPolicy{{Name: "first"}, {Name: "second"}}}
	if got := cfg.PolicyForWorkflow("missing"); got.Name != "first" {
		t.Fatalf("fallback policy = %+v", got)
	}
	if got := cfg.PolicyForWorkflow("second"); got.Name != "second" {
		t.Fatalf("matched policy = %+v", got)
	}
}

func assertYAMLFirstDefault(t *testing.T, cfg EngineConfig) {
	t.Helper()

	if len(cfg.Workflows) != 1 {
		t.Fatalf("workflows len = %d, want 1", len(cfg.Workflows))
	}
	wf := cfg.Workflows[0]
	if wf.Name != DefaultWorkflowName {
		t.Fatalf("workflow = %q", wf.Name)
	}
	if wf.Mode != CompositionSingle {
		t.Fatalf("mode = %q, want %q", wf.Mode, CompositionSingle)
	}
	if len(wf.Providers) != 1 || wf.Providers[0] != "yaml" {
		t.Fatalf("providers = %+v, want [yaml]", wf.Providers)
	}
	if len(wf.PipelineOrder) != 1 || wf.PipelineOrder[0] != "yaml" {
		t.Fatalf("pipeline order = %+v, want [yaml]", wf.PipelineOrder)
	}
}
