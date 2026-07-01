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
	if cfg.Workflows[0].Name != DefaultWorkflowName {
		t.Fatalf("workflow = %q", cfg.Workflows[0].Name)
	}

	cfg, err = LoadEngineConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load missing config: %v", err)
	}
	if cfg.Workflows[0].Name != DefaultWorkflowName {
		t.Fatalf("workflow = %q", cfg.Workflows[0].Name)
	}
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
