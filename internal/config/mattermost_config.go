package config

// MattermostConfig holds configuration for connecting to Mattermost
type MattermostConfig struct {
	ServerURL    string
	APIToken     string
	Team         string
	Channel      string
	Username     string
	Password     string
	WebsocketURL string
}
