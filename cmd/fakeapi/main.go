// Package main provides a standalone runner for fake API servers
// This can be used for development and testing without the main application
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"project/internal/fakeapi"
)

func main() {
	// Parse command line flags
	confluencePort := flag.Int("confluence-port", fakeapi.DefaultConfluencePort, "Confluence API port")
	mmHTTPPort := flag.Int("mm-http-port", fakeapi.DefaultMattermostHTTPPort, "Mattermost HTTP API port")
	mmWSPort := flag.Int("mm-ws-port", fakeapi.DefaultMattermostWSPort, "Mattermost WebSocket port")
	wsPort := flag.Int("ws-port", fakeapi.DefaultWebSocketPort, "WebSocket server port")
	flag.Parse()

	log.Println("🔧 Fake API Server Runner for Development")
	log.Println("==========================================")

	// Create manager with specified ports
	manager := fakeapi.NewFakeAPIManagerWithPorts(*confluencePort, *mmHTTPPort, *mmWSPort, *wsPort)

	// Start all servers
	if err := manager.Start(); err != nil {
		log.Fatalf("Failed to start fake API servers: %v", err)
	}

	// Print available endpoints
	manager.PrintEndpoints()

	log.Println("💡 Tips:")
	log.Println("  - Set MATTERMOST_SERVER_URL=http://localhost:8091 to use fake Mattermost")
	log.Println("  - Set MATTERMOST_WS_URL=ws://localhost:8092 for Mattermost WebSocket")
	log.Println("  - Set CONFLUENCE_API_ENDPOINT=http://localhost:8090 for fake Confluence")
	log.Println("  - Use test-token-123 as MATTERMOST_API_TOKEN")
	log.Println("")
	log.Println("Press Ctrl+C to stop...")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down...")
	manager.Stop()
	log.Println("Goodbye!")
}
