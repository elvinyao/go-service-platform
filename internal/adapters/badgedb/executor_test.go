package badgedb

import "testing"

func TestExecutorType(t *testing.T) {
	exe := NewExecutor(nil)
	if exe.Type() != "db" {
		t.Fatalf("type = %q, want db", exe.Type())
	}
}
