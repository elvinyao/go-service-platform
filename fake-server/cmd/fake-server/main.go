// Package main provides a standalone runner for fake API servers.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fake-server/internal/fakeapi"
)

func main() {
	confluencePort := flag.Int("confluence-port", fakeapi.DefaultConfluencePort, "Confluence API port")
	mmHTTPPort := flag.Int("mm-http-port", fakeapi.DefaultMattermostHTTPPort, "Mattermost HTTP API port")
	mmWSPort := flag.Int("mm-ws-port", fakeapi.DefaultMattermostWSPort, "Mattermost WebSocket port")
	wsPort := flag.Int("ws-port", fakeapi.DefaultWebSocketPort, "WebSocket server port")
	flag.Parse()

	log.Println("Fake API Server Runner for Development")
	log.Println("====================================")

	manager := fakeapi.NewFakeAPIManagerWithPorts(*confluencePort, *mmHTTPPort, *mmWSPort, *wsPort)

	if err := manager.Start(); err != nil {
		log.Fatalf("Failed to start fake API servers: %v", err)
	}

	manager.PrintEndpoints()

	log.Println("Tips:")
	log.Println("  - Set MATTERMOST_SERVER_URL=http://localhost:8091")
	log.Println("  - Set MATTERMOST_WS_URL=ws://localhost:8092")
	log.Println("  - Set CONFLUENCE_API_ENDPOINT=http://localhost:8090")
	log.Println("  - Use test-token-123 as MATTERMOST_API_TOKEN")
	log.Println("Press Ctrl+C to stop...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	manager.Stop()
	log.Println("Goodbye")
}
