// Package main provides a standalone runner for fake API servers.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fake-server/internal/fakeapi"
)

type fakeAPIManager interface {
	Start() error
	Stop() error
	PrintEndpoints()
}

var newFakeAPIManagerWithPorts = func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager {
	return fakeapi.NewFakeAPIManagerWithPorts(confluencePort, mmHTTPPort, mmWSPort, wsPort)
}

var fakeServerHealthCheck = checkFakeServerHealth

func run(args []string, sigChan <-chan os.Signal, flagOutput io.Writer) error {
	flags := flag.NewFlagSet("fake-server", flag.ContinueOnError)
	if flagOutput != nil {
		flags.SetOutput(flagOutput)
	}
	confluencePort := flags.Int("confluence-port", fakeapi.DefaultConfluencePort, "Confluence API port")
	mmHTTPPort := flags.Int("mm-http-port", fakeapi.DefaultMattermostHTTPPort, "Mattermost HTTP API port")
	mmWSPort := flags.Int("mm-ws-port", fakeapi.DefaultMattermostWSPort, "Mattermost WebSocket port")
	wsPort := flags.Int("ws-port", fakeapi.DefaultWebSocketPort, "WebSocket server port")
	healthcheck := flags.Bool("healthcheck", false, "Check whether the fake WebSocket server is ready")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *healthcheck {
		return fakeServerHealthCheck(*wsPort)
	}

	log.Println("Fake API Server Runner for Development")
	log.Println("====================================")

	manager := newFakeAPIManagerWithPorts(*confluencePort, *mmHTTPPort, *mmWSPort, *wsPort)

	if err := manager.Start(); err != nil {
		return fmt.Errorf("failed to start fake API servers: %w", err)
	}

	manager.PrintEndpoints()

	log.Println("Tips:")
	log.Println("  - Set MATTERMOST_SERVER_URL=http://localhost:8091")
	log.Println("  - Set MATTERMOST_WS_URL=ws://localhost:8092")
	log.Println("  - Set CONFLUENCE_API_ENDPOINT=http://localhost:8090")
	log.Println("  - Use test-token-123 as MATTERMOST_API_TOKEN")
	log.Println("Press Ctrl+C to stop...")

	<-sigChan

	log.Println("Shutting down...")
	if err := manager.Stop(); err != nil {
		return fmt.Errorf("failed to stop fake API servers: %w", err)
	}
	log.Println("Goodbye")
	return nil
}

func checkFakeServerHealth(port int) error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return fmt.Errorf("check fake server health: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("check fake server health: status %d", response.StatusCode)
	}
	return nil
}

func main() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	if err := run(os.Args[1:], sigChan, os.Stderr); err != nil {
		log.Fatalf("Failed to run fake API server: %v", err)
	}
}
