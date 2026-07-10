package ruleengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEngineConfigReturnsDefaultForEmptyPath(t *testing.T) {
	cfg, err := LoadEngineConfig("")
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	assertYAMLFirstDefault(t, cfg)

}

func TestLoadEngineConfigRejectsMissingPath(t *testing.T) {
	_, err := LoadEngineConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil || !strings.Contains(err.Error(), "read rule engine config") {
		t.Fatalf("missing config error = %v", err)
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
	if wf.Mode != CompositionSingle {
		t.Fatalf("mode = %q", wf.Mode)
	}
	if len(wf.Providers) != 1 || wf.Providers[0] != "yaml" {
		t.Fatalf("providers = %+v", wf.Providers)
	}
	if !wf.ActionMerge.Dedup || wf.ActionMerge.Order != ActionOrderPriority {
		t.Fatalf("action merge = %+v", wf.ActionMerge)
	}
}

func TestLoadEngineConfigRejectsUnknownFieldsAndInvalidPolicies(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "unknown field",
			data: "workflos: []\n",
			want: "field workflos not found",
		},
		{
			name: "invalid mode",
			data: `workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: adn
`,
			want: "mode \"adn\" is invalid",
		},
		{
			name: "duplicate provider",
			data: `workflows:
  - name: WorkflowEngine
    providers: [yaml, yaml]
    mode: or
`,
			want: "duplicate value \"yaml\"",
		},
		{
			name: "explicitly empty workflows",
			data: "workflows: []\n",
			want: "workflows must contain at least one policy",
		},
		{
			name: "explicitly empty providers",
			data: `workflows:
  - name: WorkflowEngine
    providers: []
    mode: single
`,
			want: "providers must contain at least one value",
		},
		{
			name: "incomplete pipeline",
			data: `workflows:
  - name: WorkflowEngine
    providers: [yaml, remote]
    mode: pipeline
    pipeline_order: [yaml]
`,
			want: "must contain every configured provider",
		},
		{
			name: "invalid action order",
			data: `workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
    action_merge:
      order: random
`,
			want: "order \"random\" is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rule-engine.yaml")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := LoadEngineConfig(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("config error = %v, want %q", err, tt.want)
			}
		})
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

func TestLoadEngineConfigRejectsMultipleYAMLDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-engine.yaml")
	if err := os.WriteFile(path, []byte(`workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
---
workflows: []
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadEngineConfig(path); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("multiple document error = %v", err)
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
	if _, exists := cfg.LookupPolicyForWorkflow("missing"); exists {
		t.Fatalf("missing workflow lookup unexpectedly succeeded")
	}
	if got, exists := cfg.LookupPolicyForWorkflow("second"); !exists || got.Name != "second" {
		t.Fatalf("exact workflow lookup = %+v/%v", got, exists)
	}
}

func TestLoadEngineConfigPreservesExplicitWorkflowDedupFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-engine.yaml")
	data := []byte(`workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
    action_merge:
      dedup: false
      order: priority
action_merge:
  dedup: true
  order: priority
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadEngineConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Workflows[0].ActionMerge.Dedup {
		t.Fatalf("workflow dedup = true, want explicit false override")
	}
}

func TestLoadEngineConfigRejectsUnknownAndDuplicateActionMergeFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown", body: "dedupe: true", want: "field dedupe not found"},
		{name: "duplicate", body: "dedup: true\n  dedup: false", want: "duplicate field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rule-engine.yaml")
			if err := os.WriteFile(path, []byte("action_merge:\n  "+test.body+"\n"), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := LoadEngineConfig(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("load error = %v, want %q", err, test.want)
			}
		})
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
