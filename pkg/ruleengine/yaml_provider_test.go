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
