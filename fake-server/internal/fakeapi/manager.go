package fakeapi

import (
	"errors"
	"fmt"
	"log"
	"sync"
)

// FakeAPIManager manages all fake API servers
type FakeAPIManager struct {
	confluence *FakeConfluenceServer
	mattermost *FakeMattermostServer
	websocket  *FakeWebSocketServer
	running    bool
	mu         sync.Mutex
}

// DefaultPorts for fake servers
const (
	DefaultConfluencePort     = 8090
	DefaultMattermostHTTPPort = 8091
	DefaultMattermostWSPort   = 8092
	DefaultWebSocketPort      = 8093
)

// NewFakeAPIManager creates a new manager with default ports
func NewFakeAPIManager() *FakeAPIManager {
	return &FakeAPIManager{
		confluence: NewFakeConfluenceServer(DefaultConfluencePort),
		mattermost: NewFakeMattermostServer(DefaultMattermostHTTPPort, DefaultMattermostWSPort),
		websocket:  NewFakeWebSocketServer(DefaultWebSocketPort),
	}
}

// NewFakeAPIManagerWithPorts creates a manager with custom ports
func NewFakeAPIManagerWithPorts(confluencePort, mmHTTPPort, mmWSPort, wsPort int) *FakeAPIManager {
	return &FakeAPIManager{
		confluence: NewFakeConfluenceServer(confluencePort),
		mattermost: NewFakeMattermostServer(mmHTTPPort, mmWSPort),
		websocket:  NewFakeWebSocketServer(wsPort),
	}
}

// Start starts all fake API servers
func (m *FakeAPIManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	log.Println("Starting all fake API servers...")

	if err := m.confluence.Start(); err != nil {
		return fmt.Errorf("start fake Confluence server: %w", err)
	}

	if err := m.mattermost.Start(); err != nil {
		startErr := fmt.Errorf("start fake Mattermost server: %w", err)
		if cleanupErr := m.confluence.Stop(); cleanupErr != nil {
			return errors.Join(startErr, fmt.Errorf("stop fake Confluence server after startup failure: %w", cleanupErr))
		}
		return startErr
	}

	if err := m.websocket.Start(); err != nil {
		errs := []error{fmt.Errorf("start fake WebSocket server: %w", err)}
		if cleanupErr := m.mattermost.Stop(); cleanupErr != nil {
			errs = append(errs, fmt.Errorf("stop fake Mattermost server after startup failure: %w", cleanupErr))
		}
		if cleanupErr := m.confluence.Stop(); cleanupErr != nil {
			errs = append(errs, fmt.Errorf("stop fake Confluence server after startup failure: %w", cleanupErr))
		}
		return errors.Join(errs...)
	}

	m.running = true

	log.Println("All fake API servers started:")
	log.Printf("   Confluence API: http://localhost:%d", m.confluence.GetPort())
	log.Printf("   Mattermost HTTP: http://localhost:%d", m.mattermost.GetHTTPPort())
	log.Printf("   Mattermost WS: ws://localhost:%d", m.mattermost.GetWSPort())
	log.Printf("   WebSocket: ws://localhost:%d/ws", m.websocket.GetPort())

	return nil
}

// Stop stops all fake API servers
func (m *FakeAPIManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	log.Println("Stopping all fake API servers...")

	var errs []error
	if err := m.websocket.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("stop fake WebSocket server: %w", err))
	}
	if err := m.mattermost.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("stop fake Mattermost server: %w", err))
	}
	if err := m.confluence.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("stop fake Confluence server: %w", err))
	}

	m.running = false
	log.Println("All fake API servers stopped")

	return errors.Join(errs...)
}

// GetConfluence returns the Confluence server
func (m *FakeAPIManager) GetConfluence() *FakeConfluenceServer {
	return m.confluence
}

// GetMattermost returns the Mattermost server
func (m *FakeAPIManager) GetMattermost() *FakeMattermostServer {
	return m.mattermost
}

// GetWebSocket returns the WebSocket server
func (m *FakeAPIManager) GetWebSocket() *FakeWebSocketServer {
	return m.websocket
}

// IsRunning returns whether servers are running
func (m *FakeAPIManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// PrintEndpoints prints all available endpoints
func (m *FakeAPIManager) PrintEndpoints() {
	log.Println("\nAvailable Fake API Endpoints:")
	log.Println("")
	log.Println("Confluence API:")
	log.Printf("  GET  http://localhost:%d/rest/api/content/{pageId}", m.confluence.GetPort())
	log.Printf("  GET  http://localhost:%d/rest/api/space", m.confluence.GetPort())
	log.Printf("  GET  http://localhost:%d/health", m.confluence.GetPort())
	log.Println("")
	log.Println("Mattermost API:")
	log.Printf("  POST http://localhost:%d/api/v4/users/login", m.mattermost.GetHTTPPort())
	log.Printf("  POST http://localhost:%d/api/v4/posts", m.mattermost.GetHTTPPort())
	log.Printf("  GET  http://localhost:%d/api/v4/channels/{channelId}", m.mattermost.GetHTTPPort())
	log.Printf("  GET  http://localhost:%d/api/v4/system/ping", m.mattermost.GetHTTPPort())
	log.Printf("  WS   ws://localhost:%d/api/v4/websocket", m.mattermost.GetWSPort())
	log.Println("")
	log.Println("WebSocket Test Server:")
	log.Printf("  WS   ws://localhost:%d/ws", m.websocket.GetPort())
	log.Printf("  POST http://localhost:%d/api/send (send test message)", m.websocket.GetPort())
	log.Printf("  GET  http://localhost:%d/api/messages (list received)", m.websocket.GetPort())
	log.Printf("  GET  http://localhost:%d/health", m.websocket.GetPort())
	log.Println("")
}
