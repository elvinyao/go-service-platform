package config

import (
	"testing"
	"time"
)

func TestDefaultConfluenceSettingsConfig(t *testing.T) {
	cfg := DefaultConfluenceSettingsConfig()
	if cfg.RefreshInterval != 5*time.Minute {
		t.Fatalf("refresh interval = %v, want 5m", cfg.RefreshInterval)
	}
	if cfg.APIEndpoint != "https://confluence.example.com/api" {
		t.Fatalf("api endpoint = %q", cfg.APIEndpoint)
	}
}
