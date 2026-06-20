package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRuntimeConfigAppliesDefaultsAndYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.yaml")
	data := []byte(`admin:
  address: ":18080"
inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
    reconnect_interval: "2s"
adapters:
  confluence:
    enabled: true
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
    refresh_interval: "30s"
  mattermost:
    enabled: true
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
  badgedb:
    enabled: true
    path: "/tmp/service-workflow-badges.db"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if cfg.Admin.Address != ":18080" {
		t.Fatalf("admin address = %q, want :18080", cfg.Admin.Address)
	}
	if cfg.Inputs.WebSocket.ReconnectInterval != 2*time.Second {
		t.Fatalf("reconnect interval = %v, want 2s", cfg.Inputs.WebSocket.ReconnectInterval)
	}
}

func TestLoadRuntimeConfigRejectsEmptyAdminAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.yaml")
	data := []byte(`admin:
  address: ""
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadRuntimeConfig(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}
