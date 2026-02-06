package config

import "time"

// WebSocketConfig holds configuration for the WebSocket service connection
type WebSocketConfig struct {
	// ServerURL is the WebSocket server URL (e.g. "ws://localhost:8093")
	ServerURL string

	// Path is the WebSocket endpoint path (e.g. "/ws")
	Path string

	// ReconnectInterval is how long to wait before reconnecting on failure
	ReconnectInterval time.Duration
}
