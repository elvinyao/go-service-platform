package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLoadRuntimeConfigParsesWebSocketHeartbeat(t *testing.T) {
	cfg, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  websocket:
    ping_interval: "5s"
    read_timeout: "15s"
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Inputs.WebSocket.PingInterval != 5*time.Second || cfg.Inputs.WebSocket.ReadTimeout != 15*time.Second {
		t.Fatalf("heartbeat = %v/%v, want 5s/15s", cfg.Inputs.WebSocket.PingInterval, cfg.Inputs.WebSocket.ReadTimeout)
	}

	defaults := DefaultRuntimeConfig()
	if defaults.Inputs.WebSocket.PingInterval != 20*time.Second || defaults.Inputs.WebSocket.ReadTimeout != 60*time.Second {
		t.Fatalf("default heartbeat = %v/%v, want 20s/60s", defaults.Inputs.WebSocket.PingInterval, defaults.Inputs.WebSocket.ReadTimeout)
	}
}

func TestLoadRuntimeConfigAllowsDisablingWebSocketHeartbeat(t *testing.T) {
	cfg, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  websocket:
    ping_interval: "0s"
    read_timeout: "0s"
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Inputs.WebSocket.PingInterval != 0 || cfg.Inputs.WebSocket.ReadTimeout != 0 {
		t.Fatalf("heartbeat = %v/%v, want disabled", cfg.Inputs.WebSocket.PingInterval, cfg.Inputs.WebSocket.ReadTimeout)
	}
}

func TestLoadRuntimeConfigRejectsInvalidWebSocketHeartbeat(t *testing.T) {
	tests := map[string]struct {
		ping, read string
		want       string
	}{
		"unparsable ping":       {ping: "soon", read: "60s", want: "inputs.websocket.ping_interval is invalid"},
		"unparsable timeout":    {ping: "20s", read: "later", want: "inputs.websocket.read_timeout is invalid"},
		"negative ping":         {ping: "-1s", read: "0s", want: "ping_interval must not be negative"},
		"negative timeout":      {ping: "1s", read: "-1s", want: "read_timeout must not be negative"},
		"timeout without pings": {ping: "0s", read: "60s", want: "less than read_timeout"},
		"ping not shorter":      {ping: "60s", read: "60s", want: "less than read_timeout"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  websocket:
    ping_interval: "`+tt.ping+`"
    read_timeout: "`+tt.read+`"
`))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadRuntimeConfigEnablesMattermostInputFromEnvironment(t *testing.T) {
	t.Setenv("MATTERMOST_BOT_URL", "https://chat.example")
	t.Setenv("MATTERMOST_BOT_TOKEN", "secret-token")
	cfg, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  mattermost:
    enabled: true
    server_url: "http://ignored.example"
    reconnect_interval: "2s"
    ping_interval: "10s"
    read_timeout: "30s"
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mm := cfg.Inputs.Mattermost
	if !mm.Enabled || mm.ServerURL != "https://chat.example" || mm.Token != "secret-token" ||
		mm.ReconnectInterval != 2*time.Second || mm.PingInterval != 10*time.Second || mm.ReadTimeout != 30*time.Second {
		t.Fatalf("mattermost input = %+v", mm)
	}
	encoded, _ := json.Marshal(cfg)
	if strings.Contains(string(encoded), "secret-token") {
		t.Fatalf("token leaked into JSON: %s", encoded)
	}
}

func TestLoadRuntimeConfigRejectsInvalidMattermostInput(t *testing.T) {
	tests := map[string]struct {
		token, settings, want string
	}{
		"missing token":    {settings: `server_url: "http://localhost:8065"`, want: "MATTERMOST_BOT_TOKEN"},
		"bad URL":          {token: "t", settings: `server_url: "ws://localhost:8065"`, want: "inputs.mattermost.server_url"},
		"zero reconnect":   {token: "t", settings: `reconnect_interval: "0s"`, want: "reconnect_interval must be greater than zero"},
		"disabled ping":    {token: "t", settings: `ping_interval: "0s"`, want: "must be greater than zero"},
		"ping not shorter": {token: "t", settings: "ping_interval: \"1m\"\n    read_timeout: \"1m\"", want: "inputs.mattermost.ping_interval"},
		"bad duration":     {token: "t", settings: `read_timeout: "soon"`, want: "inputs.mattermost.read_timeout is invalid"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MATTERMOST_BOT_TOKEN", tt.token)
			_, err := LoadRuntimeConfig(writeRuntimeConfig(t, `admin:
  address: ":18080"
inputs:
  mattermost:
    enabled: true
    `+tt.settings+`
`))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
