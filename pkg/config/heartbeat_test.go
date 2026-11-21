package config

import (
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
