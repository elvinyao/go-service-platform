package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type RuntimeConfig struct {
	Admin    AdminConfig    `yaml:"admin" json:"admin"`
	Inputs   InputsConfig   `yaml:"inputs" json:"inputs"`
	Adapters AdaptersConfig `yaml:"adapters" json:"adapters"`
}

type AdminConfig struct {
	Address string `yaml:"address" json:"address"`
}

type InputsConfig struct {
	WebSocket WebSocketInputConfig `yaml:"websocket" json:"websocket"`
}

type WebSocketInputConfig struct {
	Enabled           bool          `yaml:"enabled" json:"enabled"`
	ServerURL         string        `yaml:"server_url" json:"server_url"`
	Path              string        `yaml:"path" json:"path"`
	ReconnectInterval time.Duration `yaml:"-" json:"reconnect_interval"`
	ReconnectRaw      string        `yaml:"reconnect_interval" json:"-"`
}

type AdaptersConfig struct {
	Confluence ConfluenceAdapterConfig `yaml:"confluence" json:"confluence"`
	Mattermost MattermostAdapterConfig `yaml:"mattermost" json:"mattermost"`
	BadgeDB    BadgeDBAdapterConfig    `yaml:"badgedb" json:"badgedb"`
}

type ConfluenceAdapterConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	APIEndpoint     string        `yaml:"api_endpoint" json:"api_endpoint"`
	SettingsPageID  string        `yaml:"settings_page_id" json:"settings_page_id"`
	RefreshInterval time.Duration `yaml:"-" json:"refresh_interval"`
	RefreshRaw      string        `yaml:"refresh_interval" json:"-"`
}

type MattermostAdapterConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	ServerURL    string `yaml:"server_url" json:"server_url"`
	WebsocketURL string `yaml:"websocket_url" json:"websocket_url"`
	APIToken     string `yaml:"api_token" json:"api_token"`
	Channel      string `yaml:"channel" json:"channel"`
}

type BadgeDBAdapterConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path" json:"path"`
}

func LoadRuntimeConfig(path string) (RuntimeConfig, error) {
	cfg := DefaultRuntimeConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("read runtime config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return RuntimeConfig{}, fmt.Errorf("parse runtime config: %w", err)
	}
	cfg.ApplyEnv()
	if err := cfg.Normalize(); err != nil {
		return RuntimeConfig{}, err
	}
	return cfg, nil
}

func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		Admin: AdminConfig{Address: ":8080"},
		Inputs: InputsConfig{WebSocket: WebSocketInputConfig{
			Enabled:           true,
			ServerURL:         "ws://localhost:8093",
			Path:              "/ws",
			ReconnectRaw:      "5s",
			ReconnectInterval: 5 * time.Second,
		}},
		Adapters: AdaptersConfig{
			Confluence: ConfluenceAdapterConfig{
				Enabled:         true,
				APIEndpoint:     "http://localhost:8090",
				SettingsPageID:  "settings-page-1",
				RefreshRaw:      "5m",
				RefreshInterval: 5 * time.Minute,
			},
			Mattermost: MattermostAdapterConfig{
				Enabled:      true,
				ServerURL:    "http://localhost:8091",
				WebsocketURL: "ws://localhost:8092",
				APIToken:     "test-token-123",
				Channel:      "test-channel-1",
			},
			BadgeDB: BadgeDBAdapterConfig{
				Enabled: true,
				Path:    "/tmp/service-workflow-badges.db",
			},
		},
	}
}

func (c *RuntimeConfig) ApplyEnv() {
	if v := os.Getenv("ADMIN_ADDR"); v != "" {
		c.Admin.Address = v
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

func (c *RuntimeConfig) Normalize() error {
	if c.Admin.Address == "" {
		return fmt.Errorf("admin.address is required")
	}
	if c.Inputs.WebSocket.ReconnectRaw != "" {
		d, err := time.ParseDuration(c.Inputs.WebSocket.ReconnectRaw)
		if err != nil {
			return fmt.Errorf("inputs.websocket.reconnect_interval is invalid: %w", err)
		}
		c.Inputs.WebSocket.ReconnectInterval = d
	}
	if c.Adapters.Confluence.RefreshRaw != "" {
		d, err := time.ParseDuration(c.Adapters.Confluence.RefreshRaw)
		if err != nil {
			return fmt.Errorf("adapters.confluence.refresh_interval is invalid: %w", err)
		}
		c.Adapters.Confluence.RefreshInterval = d
	}
	return nil
}
