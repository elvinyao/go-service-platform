package config

import "time"

// ConfluenceSettingsConfig holds configuration for the Confluence settings service
type ConfluenceSettingsConfig struct {
	// PageID is the Confluence page ID to monitor for settings
	PageID string

	// RefreshInterval is how often to refresh settings from Confluence
	RefreshInterval time.Duration

	// APIEndpoint is the Confluence API endpoint
	APIEndpoint string

	// SpaceKey is the Confluence space key
	SpaceKey string
}

// DefaultConfluenceSettingsConfig returns a default configuration
func DefaultConfluenceSettingsConfig() ConfluenceSettingsConfig {
	return ConfluenceSettingsConfig{
		RefreshInterval: 5 * time.Minute,
		APIEndpoint:     "https://confluence.example.com/api",
	}
}
