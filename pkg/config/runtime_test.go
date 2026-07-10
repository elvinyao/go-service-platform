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
	if cfg.Admin.ReadTimeout != 10*time.Second {
		t.Fatalf("admin read timeout = %v, want default 10s", cfg.Admin.ReadTimeout)
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
	t.Setenv("WEBSOCKET_PATH", "/events")
	t.Setenv("CONFLUENCE_API_ENDPOINT", "http://confluence.local")
	t.Setenv("CONFLUENCE_SETTINGS_PAGE_ID", "settings-page-env")
	t.Setenv("MATTERMOST_SERVER_URL", "http://mattermost.local")
	t.Setenv("MATTERMOST_WS_URL", "ws://mattermost.local/ws")
	t.Setenv("MATTERMOST_API_TOKEN", "env-token")
	t.Setenv("MATTERMOST_CHANNEL", "env-channel")
	t.Setenv("BADGEDB_PATH", "/tmp/env-badges.db")

	cfg, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  websocket:
    server_url: "ws://localhost:8093"
    path: "/ws"
adapters:
  confluence:
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-yaml"
  mattermost:
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "yaml-token"
    channel: "yaml-channel"
  badgedb:
    path: "/tmp/yaml-badges.db"
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
	if cfg.Inputs.WebSocket.Path != "/events" {
		t.Fatalf("websocket path = %q, want /events", cfg.Inputs.WebSocket.Path)
	}
	if cfg.Adapters.Confluence.APIEndpoint != "http://confluence.local" {
		t.Fatalf("confluence endpoint = %q", cfg.Adapters.Confluence.APIEndpoint)
	}
	if cfg.Adapters.Confluence.SettingsPageID != "settings-page-env" {
		t.Fatalf("confluence settings page = %q, want settings-page-env", cfg.Adapters.Confluence.SettingsPageID)
	}
	if cfg.Adapters.Mattermost.ServerURL != "http://mattermost.local" {
		t.Fatalf("mattermost server URL = %q", cfg.Adapters.Mattermost.ServerURL)
	}
	if cfg.Adapters.Mattermost.WebsocketURL != "ws://mattermost.local/ws" {
		t.Fatalf("mattermost websocket URL = %q", cfg.Adapters.Mattermost.WebsocketURL)
	}
	if cfg.Adapters.Mattermost.APIToken != "env-token" {
		t.Fatalf("mattermost token = %q, want env-token", cfg.Adapters.Mattermost.APIToken)
	}
	if cfg.Adapters.Mattermost.Channel != "env-channel" {
		t.Fatalf("mattermost channel = %q, want env-channel", cfg.Adapters.Mattermost.Channel)
	}
	if cfg.Adapters.BadgeDB.Path != "/tmp/env-badges.db" {
		t.Fatalf("badgedb path = %q, want /tmp/env-badges.db", cfg.Adapters.BadgeDB.Path)
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

func TestLoadRuntimeConfigReturnsErrorForMalformedYAML(t *testing.T) {
	_, err := LoadRuntimeConfig(writeRuntimeConfig(t, "admin: ["))
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if got := err.Error(); !strings.Contains(got, "parse runtime config") {
		t.Fatalf("error = %q, want parse runtime config", got)
	}
}

func TestLoadRuntimeConfigRejectsUnknownFields(t *testing.T) {
	_, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
  read_timout: "2s"
`))
	if err == nil || !strings.Contains(err.Error(), "field read_timout not found") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestLoadRuntimeConfigRejectsMultipleYAMLDocuments(t *testing.T) {
	_, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
---
admin:
  address: ":19090"
`))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("multiple document error = %v", err)
	}
}

func TestRuntimeConfigRejectsUnsafeEnabledComponentSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuntimeConfig)
		want   string
	}{
		{
			name: "negative websocket reconnect",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Inputs.WebSocket.ReconnectInterval = -time.Second
			},
			want: "reconnect_interval must be greater than zero",
		},
		{
			name: "invalid websocket scheme",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Inputs.WebSocket.ServerURL = "http://localhost:8093"
			},
			want: "must use one of these schemes",
		},
		{
			name: "invalid websocket path",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Inputs.WebSocket.Path = "events"
			},
			want: "path must start with /",
		},
		{
			name: "missing confluence page",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Adapters.Confluence.Enabled = true
				cfg.Adapters.Confluence.SettingsPageID = ""
			},
			want: "settings_page_id is required",
		},
		{
			name: "missing mattermost channel",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Adapters.Mattermost.Enabled = true
				cfg.Adapters.Mattermost.Channel = ""
			},
			want: "channel is required",
		},
		{
			name: "missing badgedb path",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Adapters.BadgeDB.Enabled = true
				cfg.Adapters.BadgeDB.Path = ""
			},
			want: "badgedb.path is required",
		},
		{
			name: "invalid admin timeout",
			mutate: func(cfg *RuntimeConfig) {
				cfg.Admin.WriteTimeout = 0
			},
			want: "write_timeout must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultRuntimeConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validation error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRuntimeConfigAppliesAdminTimeoutEnvironmentOverrides(t *testing.T) {
	t.Setenv("ADMIN_READ_HEADER_TIMEOUT", "1s")
	t.Setenv("ADMIN_READ_TIMEOUT", "2s")
	t.Setenv("ADMIN_WRITE_TIMEOUT", "3s")
	t.Setenv("ADMIN_IDLE_TIMEOUT", "4s")

	cfg := DefaultRuntimeConfig()
	cfg.ApplyEnv()
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	if cfg.Admin.ReadHeaderTimeout != time.Second || cfg.Admin.ReadTimeout != 2*time.Second ||
		cfg.Admin.WriteTimeout != 3*time.Second || cfg.Admin.IdleTimeout != 4*time.Second {
		t.Fatalf("admin timeouts = %+v", cfg.Admin)
	}
}

func TestDefaultRuntimeConfigMatchesRepositoryAdminAddress(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	if cfg.Admin.Address != "127.0.0.1:18080" {
		t.Fatalf("admin address = %q, want 127.0.0.1:18080", cfg.Admin.Address)
	}
}

func TestRuntimeConfigAllowsMattermostWithoutWebSocket(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	cfg.Adapters.Mattermost.Enabled = true
	cfg.Adapters.Mattermost.WebsocketURL = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate HTTP-only Mattermost config: %v", err)
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
