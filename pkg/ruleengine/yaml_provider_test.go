package ruleengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestYAMLProviderStartLoadsRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow-rules.yaml")
	content := `
version: "1"
rules:
  - id: r1
    workflow: WorkflowEngine
    enabled: true
    priority: 10
    conditions:
      - field: type
        op: eq
        value: AAA
    actions:
      - id: a1
        executor: log
        priority: 10
        params:
          level: info
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	p := NewYAMLProvider("yaml", path, DefaultWorkflowName)
	require.NoError(t, p.Start(context.Background()))
	snap := p.Snapshot(context.Background())
	require.Len(t, snap.Rules, 1)
	require.Equal(t, "r1", snap.Rules[0].ID)
}

func TestYAMLProviderStartFailsOnInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow-rules.yaml")
	content := `
version: "1"
rules:
  - id: r1
    enabled: true
    conditions:
      - field: content
        op: regex
        value: "*invalid"
    actions:
      - executor: log
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	p := NewYAMLProvider("yaml", path, DefaultWorkflowName)
	require.Error(t, p.Start(context.Background()))
}

func TestYAMLProviderNameAndStartWithMissingOrEmptyPath(t *testing.T) {
	provider := NewYAMLProvider("yaml", "", DefaultWorkflowName)
	if provider.Name() != "yaml" {
		t.Fatalf("name = %q, want yaml", provider.Name())
	}
	if err := provider.Start(context.Background()); err != nil {
		t.Fatalf("start empty path: %v", err)
	}
	if len(provider.Snapshot(context.Background()).Rules) != 0 {
		t.Fatalf("empty path rules not empty")
	}

	provider = NewYAMLProvider("yaml", filepath.Join(t.TempDir(), "missing.yaml"), DefaultWorkflowName)
	if err := provider.Start(context.Background()); err == nil {
		t.Fatalf("start missing path error = nil")
	}
}

func TestYAMLProviderRejectsUnknownFieldsAndDuplicateRuleIDs(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "unknown field",
			data: `rules:
  - id: r1
    actons:
      - executor: log
`,
		},
		{
			name: "duplicate rule id",
			data: `rules:
  - id: r1
    actions:
      - executor: log
  - id: r1
    actions:
      - executor: log
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rules.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.data), 0o600))
			require.Error(t, NewYAMLProvider("yaml", path, DefaultWorkflowName).Start(context.Background()))
		})
	}
}

func TestYAMLProviderRejectsMultipleYAMLDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.yaml")
	data := `rules:
  - id: r1
    actions:
      - executor: log
---
rules: []
`
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	err := NewYAMLProvider("yaml", path, DefaultWorkflowName).Start(context.Background())
	require.ErrorContains(t, err, "multiple YAML documents")
}

func TestYAMLProviderValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing id", data: `rules:
  - actions:
      - executor: log
`},
		{name: "missing actions", data: `rules:
  - id: r1
`},
		{name: "missing condition field", data: `rules:
  - id: r1
    conditions:
      - op: eq
        value: AAA
    actions:
      - executor: log
`},
		{name: "invalid condition op", data: `rules:
  - id: r1
    conditions:
      - field: type
        op: nope
        value: AAA
    actions:
      - executor: log
`},
		{name: "unsupported condition field", data: `rules:
  - id: r1
    conditions:
      - field: payload.status
        op: eq
        value: ready
    actions:
      - executor: log
`},
		{name: "missing executor", data: `rules:
  - id: r1
    actions:
      - id: a1
`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rules.yaml")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatalf("write rules: %v", err)
			}
			if err := NewYAMLProvider("yaml", path, DefaultWorkflowName).Start(context.Background()); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestStaticProviderNameAndStart(t *testing.T) {
	provider := NewStaticProvider("static", RuleSet{Rules: []Rule{{ID: "r1"}}})
	if provider.Name() != "static" {
		t.Fatalf("name = %q, want static", provider.Name())
	}
	if err := provider.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
}
