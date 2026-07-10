package app

import (
	"context"
	"strings"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/config"
)

func TestNewUsesRuntimeConfigAdminAddress(t *testing.T) {
	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"

	application := New("test", cfg, "config/rule-engine.yaml", "config/workflow-rules.yaml")
	if application.AdminAddress() != "127.0.0.1:0" {
		t.Fatalf("admin address = %q, want 127.0.0.1:0", application.AdminAddress())
	}
}

func TestAppRejectsDuplicateStartBeforeChangingResources(t *testing.T) {
	cfg := config.DefaultRuntimeConfig()
	application := New("test", cfg, "missing-engine.yaml", "missing-rules.yaml")
	application.started = true

	err := application.Start(context.Background())

	if err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("duplicate start error = %v", err)
	}
	if application.container != nil || application.serviceManager != nil || application.adminServer != nil {
		t.Fatalf("duplicate start changed application resources")
	}
}

func TestAppSuccessfulStopClearsLifecycleState(t *testing.T) {
	application := &App{started: true}

	if err := application.Stop(nil); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if application.started {
		t.Fatalf("application remains started after successful stop")
	}
}
