package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadRuntimeConfigAppliesEnvironmentOverrides(t *testing.T) {
	t.Setenv("ADMIN_ADDR", ":19090")
	t.Setenv("WEBSOCKET_SERVER_URL", "ws://example.local:9999")
	t.Setenv("CONFLUENCE_API_ENDPOINT", "http://confluence.local")
	t.Setenv("MATTERMOST_SERVER_URL", "http://mattermost.local")
	t.Setenv("MATTERMOST_WS_URL", "ws://mattermost.local/ws")

	cfg, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  websocket:
    server_url: "ws://localhost:8093"
adapters:
  confluence:
    api_endpoint: "http://localhost:8090"
  mattermost:
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
`))
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if cfg.Admin.Address != ":19090" {
		t.Fatalf("admin address = %q, want :19090", cfg.Admin.Address)
	}
	if cfg.Inputs.WebSocket.ServerURL != "ws://example.local:9999" {
		t.Fatalf("websocket server URL = %q", cfg.Inputs.WebSocket.ServerURL)
	}
	if cfg.Adapters.Confluence.APIEndpoint != "http://confluence.local" {
		t.Fatalf("confluence endpoint = %q", cfg.Adapters.Confluence.APIEndpoint)
	}
	if cfg.Adapters.Mattermost.ServerURL != "http://mattermost.local" {
		t.Fatalf("mattermost server URL = %q", cfg.Adapters.Mattermost.ServerURL)
	}
	if cfg.Adapters.Mattermost.WebsocketURL != "ws://mattermost.local/ws" {
		t.Fatalf("mattermost websocket URL = %q", cfg.Adapters.Mattermost.WebsocketURL)
	}
}

func TestLoadRuntimeConfigPreservesDisabledAdapters(t *testing.T) {
	cfg, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  websocket:
    enabled: false
adapters:
  confluence:
    enabled: false
  mattermost:
    enabled: false
  badgedb:
    enabled: false
`))
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if cfg.Inputs.WebSocket.Enabled {
		t.Fatalf("websocket enabled = true, want false")
	}
	if cfg.Adapters.Confluence.Enabled || cfg.Adapters.Mattermost.Enabled || cfg.Adapters.BadgeDB.Enabled {
		t.Fatalf("adapters enabled = %+v, want all false", cfg.Adapters)
	}
}

func TestLoadRuntimeConfigRejectsInvalidDurations(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "websocket reconnect",
			data: `admin:
  address: ":18080"
inputs:
  websocket:
    reconnect_interval: "not-a-duration"
`,
			want: "inputs.websocket.reconnect_interval is invalid",
		},
		{
			name: "confluence refresh",
			data: `admin:
  address: ":18080"
adapters:
  confluence:
    refresh_interval: "not-a-duration"
`,
			want: "adapters.confluence.refresh_interval is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadRuntimeConfig(writeRuntimeConfig(t, tt.data))
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("error = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestLoadRuntimeConfigReturnsErrorForMissingFile(t *testing.T) {
	_, err := LoadRuntimeConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatalf("expected missing file error")
	}
	if got := err.Error(); !strings.Contains(got, "read runtime config") {
		t.Fatalf("error = %q, want read runtime config", got)
	}
}

func writeRuntimeConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "runtime.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
