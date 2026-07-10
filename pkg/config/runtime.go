package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RuntimeConfig controls the reference application's runtime wiring.
type RuntimeConfig struct {
	Admin    AdminConfig    `yaml:"admin" json:"admin"`
	Inputs   InputsConfig   `yaml:"inputs" json:"inputs"`
	Adapters AdaptersConfig `yaml:"adapters" json:"adapters"`
}

// AdminConfig controls the admin HTTP listener and its timeouts.
type AdminConfig struct {
	Address              string        `yaml:"address" json:"address"`
	ReadHeaderTimeout    time.Duration `yaml:"-" json:"read_header_timeout"`
	ReadHeaderTimeoutRaw string        `yaml:"read_header_timeout" json:"-"`
	ReadTimeout          time.Duration `yaml:"-" json:"read_timeout"`
	ReadTimeoutRaw       string        `yaml:"read_timeout" json:"-"`
	WriteTimeout         time.Duration `yaml:"-" json:"write_timeout"`
	WriteTimeoutRaw      string        `yaml:"write_timeout" json:"-"`
	IdleTimeout          time.Duration `yaml:"-" json:"idle_timeout"`
	IdleTimeoutRaw       string        `yaml:"idle_timeout" json:"-"`
}

// InputsConfig contains event input configuration.
type InputsConfig struct {
	WebSocket WebSocketInputConfig `yaml:"websocket" json:"websocket"`
}

// WebSocketInputConfig controls the reference WebSocket input.
type WebSocketInputConfig struct {
	Enabled           bool          `yaml:"enabled" json:"enabled"`
	ServerURL         string        `yaml:"server_url" json:"server_url"`
	Path              string        `yaml:"path" json:"path"`
	ReconnectInterval time.Duration `yaml:"-" json:"reconnect_interval"`
	ReconnectRaw      string        `yaml:"reconnect_interval" json:"-"`
}

// AdaptersConfig contains optional reference adapter configuration.
type AdaptersConfig struct {
	Confluence ConfluenceAdapterConfig `yaml:"confluence" json:"confluence"`
	Mattermost MattermostAdapterConfig `yaml:"mattermost" json:"mattermost"`
	BadgeDB    BadgeDBAdapterConfig    `yaml:"badgedb" json:"badgedb"`
}

// ConfluenceAdapterConfig controls the optional Confluence-like rule provider.
type ConfluenceAdapterConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	APIEndpoint     string        `yaml:"api_endpoint" json:"api_endpoint"`
	SettingsPageID  string        `yaml:"settings_page_id" json:"settings_page_id"`
	RefreshInterval time.Duration `yaml:"-" json:"refresh_interval"`
	RefreshRaw      string        `yaml:"refresh_interval" json:"-"`
}

// MattermostAdapterConfig controls the optional Mattermost-like executor service.
type MattermostAdapterConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	ServerURL    string `yaml:"server_url" json:"server_url"`
	WebsocketURL string `yaml:"websocket_url" json:"websocket_url"`
	APIToken     string `yaml:"api_token" json:"api_token"`
	Channel      string `yaml:"channel" json:"channel"`
}

// BadgeDBAdapterConfig controls the optional in-memory BadgeDB demo service.
type BadgeDBAdapterConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path" json:"path"`
}

// LoadRuntimeConfig loads strict YAML, applies environment overrides, and validates the result.
func LoadRuntimeConfig(path string) (RuntimeConfig, error) {
	cfg := DefaultRuntimeConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("read runtime config: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return RuntimeConfig{}, fmt.Errorf("parse runtime config: %w", err)
	}
	var additional interface{}
	if err := decoder.Decode(&additional); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not supported")
		}
		return RuntimeConfig{}, fmt.Errorf("parse runtime config: %w", err)
	}
	cfg.ApplyEnv()
	if err := cfg.Normalize(); err != nil {
		return RuntimeConfig{}, err
	}
	return cfg, nil
}

// DefaultRuntimeConfig returns a valid YAML-first local configuration.
func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		Admin: AdminConfig{
			Address:              "127.0.0.1:18080",
			ReadHeaderTimeout:    5 * time.Second,
			ReadHeaderTimeoutRaw: "5s",
			ReadTimeout:          10 * time.Second,
			ReadTimeoutRaw:       "10s",
			WriteTimeout:         10 * time.Second,
			WriteTimeoutRaw:      "10s",
			IdleTimeout:          120 * time.Second,
			IdleTimeoutRaw:       "120s",
		},
		Inputs: InputsConfig{WebSocket: WebSocketInputConfig{
			Enabled:           true,
			ServerURL:         "ws://localhost:8093",
			Path:              "/ws",
			ReconnectRaw:      "5s",
			ReconnectInterval: 5 * time.Second,
		}},
		Adapters: AdaptersConfig{
			Confluence: ConfluenceAdapterConfig{
				Enabled:         false,
				APIEndpoint:     "http://localhost:8090",
				SettingsPageID:  "settings-page-1",
				RefreshRaw:      "5m",
				RefreshInterval: 5 * time.Minute,
			},
			Mattermost: MattermostAdapterConfig{
				Enabled:      false,
				ServerURL:    "http://localhost:8091",
				WebsocketURL: "ws://localhost:8092",
				APIToken:     "test-token-123",
				Channel:      "test-channel-1",
			},
			BadgeDB: BadgeDBAdapterConfig{
				Enabled: false,
				Path:    "/tmp/service-workflow-badges.db",
			},
		},
	}
}

// ApplyEnv applies supported operational environment overrides.
func (c *RuntimeConfig) ApplyEnv() {
	if v := os.Getenv("ADMIN_ADDR"); v != "" {
		c.Admin.Address = v
	}
	if v := os.Getenv("ADMIN_READ_HEADER_TIMEOUT"); v != "" {
		c.Admin.ReadHeaderTimeoutRaw = v
	}
	if v := os.Getenv("ADMIN_READ_TIMEOUT"); v != "" {
		c.Admin.ReadTimeoutRaw = v
	}
	if v := os.Getenv("ADMIN_WRITE_TIMEOUT"); v != "" {
		c.Admin.WriteTimeoutRaw = v
	}
	if v := os.Getenv("ADMIN_IDLE_TIMEOUT"); v != "" {
		c.Admin.IdleTimeoutRaw = v
	}
	if v := os.Getenv("WEBSOCKET_SERVER_URL"); v != "" {
		c.Inputs.WebSocket.ServerURL = v
	}
	if v := os.Getenv("WEBSOCKET_PATH"); v != "" {
		c.Inputs.WebSocket.Path = v
	}
	if v := os.Getenv("CONFLUENCE_API_ENDPOINT"); v != "" {
		c.Adapters.Confluence.APIEndpoint = v
	}
	if v := os.Getenv("CONFLUENCE_SETTINGS_PAGE_ID"); v != "" {
		c.Adapters.Confluence.SettingsPageID = v
	}
	if v := os.Getenv("MATTERMOST_SERVER_URL"); v != "" {
		c.Adapters.Mattermost.ServerURL = v
	}
	if v := os.Getenv("MATTERMOST_WS_URL"); v != "" {
		c.Adapters.Mattermost.WebsocketURL = v
	}
	if v := os.Getenv("MATTERMOST_API_TOKEN"); v != "" {
		c.Adapters.Mattermost.APIToken = v
	}
	if v := os.Getenv("MATTERMOST_CHANNEL"); v != "" {
		c.Adapters.Mattermost.Channel = v
	}
	if v := os.Getenv("BADGEDB_PATH"); v != "" {
		c.Adapters.BadgeDB.Path = v
	}
}

// Normalize parses duration fields and validates the complete configuration.
func (c *RuntimeConfig) Normalize() error {
	durations := []struct {
		name   string
		raw    string
		target *time.Duration
	}{
		{name: "admin.read_header_timeout", raw: c.Admin.ReadHeaderTimeoutRaw, target: &c.Admin.ReadHeaderTimeout},
		{name: "admin.read_timeout", raw: c.Admin.ReadTimeoutRaw, target: &c.Admin.ReadTimeout},
		{name: "admin.write_timeout", raw: c.Admin.WriteTimeoutRaw, target: &c.Admin.WriteTimeout},
		{name: "admin.idle_timeout", raw: c.Admin.IdleTimeoutRaw, target: &c.Admin.IdleTimeout},
		{name: "inputs.websocket.reconnect_interval", raw: c.Inputs.WebSocket.ReconnectRaw, target: &c.Inputs.WebSocket.ReconnectInterval},
		{name: "adapters.confluence.refresh_interval", raw: c.Adapters.Confluence.RefreshRaw, target: &c.Adapters.Confluence.RefreshInterval},
	}
	for _, duration := range durations {
		if duration.raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(duration.raw)
		if err != nil {
			return fmt.Errorf("%s is invalid: %w", duration.name, err)
		}
		*duration.target = parsed
	}

	return c.Validate()
}

// Validate checks all required and enabled runtime settings.
func (c RuntimeConfig) Validate() error {
	if c.Admin.Address == "" {
		return fmt.Errorf("admin.address is required")
	}
	if _, _, err := net.SplitHostPort(c.Admin.Address); err != nil {
		return fmt.Errorf("admin.address is invalid: %w", err)
	}
	adminDurations := []struct {
		name  string
		value time.Duration
	}{
		{name: "admin.read_header_timeout", value: c.Admin.ReadHeaderTimeout},
		{name: "admin.read_timeout", value: c.Admin.ReadTimeout},
		{name: "admin.write_timeout", value: c.Admin.WriteTimeout},
		{name: "admin.idle_timeout", value: c.Admin.IdleTimeout},
	}
	for _, duration := range adminDurations {
		if duration.value <= 0 {
			return fmt.Errorf("%s must be greater than zero", duration.name)
		}
	}

	if c.Inputs.WebSocket.Enabled {
		if err := validateServiceURL("inputs.websocket.server_url", c.Inputs.WebSocket.ServerURL, "ws", "wss"); err != nil {
			return err
		}
		if c.Inputs.WebSocket.Path == "" || !strings.HasPrefix(c.Inputs.WebSocket.Path, "/") {
			return fmt.Errorf("inputs.websocket.path must start with /")
		}
		if c.Inputs.WebSocket.ReconnectInterval <= 0 {
			return fmt.Errorf("inputs.websocket.reconnect_interval must be greater than zero")
		}
	}

	if c.Adapters.Confluence.Enabled {
		if err := validateServiceURL("adapters.confluence.api_endpoint", c.Adapters.Confluence.APIEndpoint, "http", "https"); err != nil {
			return err
		}
		if c.Adapters.Confluence.SettingsPageID == "" {
			return fmt.Errorf("adapters.confluence.settings_page_id is required when enabled")
		}
		if c.Adapters.Confluence.RefreshInterval <= 0 {
			return fmt.Errorf("adapters.confluence.refresh_interval must be greater than zero")
		}
	}

	if c.Adapters.Mattermost.Enabled {
		if err := validateServiceURL("adapters.mattermost.server_url", c.Adapters.Mattermost.ServerURL, "http", "https"); err != nil {
			return err
		}
		if c.Adapters.Mattermost.WebsocketURL != "" {
			if err := validateServiceURL("adapters.mattermost.websocket_url", c.Adapters.Mattermost.WebsocketURL, "ws", "wss"); err != nil {
				return err
			}
		}
		if c.Adapters.Mattermost.Channel == "" {
			return fmt.Errorf("adapters.mattermost.channel is required when enabled")
		}
	}

	if c.Adapters.BadgeDB.Enabled && c.Adapters.BadgeDB.Path == "" {
		return fmt.Errorf("adapters.badgedb.path is required when enabled")
	}

	return nil
}

func validateServiceURL(name, rawURL string, allowedSchemes ...string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" {
		if err == nil {
			err = fmt.Errorf("host is required")
		}
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	for _, scheme := range allowedSchemes {
		if parsed.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("%s must use one of these schemes: %s", name, strings.Join(allowedSchemes, ", "))
}
