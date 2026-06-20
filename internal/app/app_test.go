package app

import (
	"testing"

	"project/pkg/config"
)

func TestNewUsesRuntimeConfigAdminAddress(t *testing.T) {
	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"

	application := New("test", cfg, "config/rule-engine.yaml", "config/workflow-rules.yaml")
	if application.AdminAddress() != "127.0.0.1:0" {
		t.Fatalf("admin address = %q, want 127.0.0.1:0", application.AdminAddress())
	}
}
