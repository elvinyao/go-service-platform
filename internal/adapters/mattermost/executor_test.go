package mattermost

import "testing"

func TestExecutorType(t *testing.T) {
	exe := NewExecutor(nil)
	if exe.Type() != "mattermost" {
		t.Fatalf("type = %q, want mattermost", exe.Type())
	}
}
