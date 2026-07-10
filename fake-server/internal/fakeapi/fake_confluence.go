package fakeapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ConfluenceSettings represents the settings stored in a Confluence page
type ConfluenceSettings struct {
	Rules []SettingRule `json:"rules"`
}

// SettingRule matches the internal service definition
type SettingRule struct {
	EventType   string `json:"event_type"`
	Pattern     string `json:"pattern"`
	ChannelID   string `json:"channel_id"`
	MessageTmpl string `json:"message_template"`
	Enabled     bool   `json:"enabled"`
}

// ConfluencePage represents a Confluence page
type ConfluencePage struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	SpaceKey string `json:"spaceKey"`
	Body     struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

// FakeConfluenceServer provides a mock Confluence API
type FakeConfluenceServer struct {
	mu       sync.RWMutex
	pages    map[string]*ConfluencePage
	settings map[string]*ConfluenceSettings
	port     int
	server   *http.Server
	listen   listenFunc
}

// NewFakeConfluenceServer creates a new fake Confluence server
func NewFakeConfluenceServer(port int) *FakeConfluenceServer {
	s := &FakeConfluenceServer{
		pages:    make(map[string]*ConfluencePage),
		settings: make(map[string]*ConfluenceSettings),
		port:     port,
	}

	// Add default test page with settings
	s.AddPage("settings-page-1", "Workflow Settings", "TEST", &ConfluenceSettings{
		Rules: []SettingRule{
			{
				EventType:   "AAA",
				Pattern:     ".*",
				ChannelID:   "test-channel-1",
				MessageTmpl: "Event AAA received: {{.Content}}",
				Enabled:     true,
			},
			{
				EventType:   "BBB",
				Pattern:     "important.*",
				ChannelID:   "alerts-channel",
				MessageTmpl: "Important event: {{.Content}}",
				Enabled:     true,
			},
			{
				EventType:   "CCC",
				Pattern:     ".*",
				ChannelID:   "general-channel",
				MessageTmpl: "Event CCC: {{.Content}}",
				Enabled:     false, // Disabled rule
			},
		},
	})

	return s
}

// AddPage adds a page with settings to the fake server
func (s *FakeConfluenceServer) AddPage(id, title, spaceKey string, settings *ConfluenceSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Convert settings to JSON for storage in page body
	settingsJSON, _ := json.Marshal(settings)

	page := &ConfluencePage{
		ID:       id,
		Title:    title,
		SpaceKey: spaceKey,
	}
	page.Body.Storage.Value = string(settingsJSON)
	page.Version.Number = 1

	s.pages[id] = page
	s.settings[id] = settings
}

// UpdateSettings updates the settings for a page
func (s *FakeConfluenceServer) UpdateSettings(pageID string, settings *ConfluenceSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, exists := s.pages[pageID]
	if !exists {
		return fmt.Errorf("page not found: %s", pageID)
	}

	settingsJSON, _ := json.Marshal(settings)
	page.Body.Storage.Value = string(settingsJSON)
	page.Version.Number++

	s.settings[pageID] = settings
	return nil
}

// Start starts the fake Confluence server
func (s *FakeConfluenceServer) Start() error {
	mux := http.NewServeMux()

	// GET /rest/api/content/{pageId}
	mux.HandleFunc("/rest/api/content/", s.handleGetPage)

	// GET /rest/api/space - List spaces
	mux.HandleFunc("/rest/api/space", s.handleListSpaces)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Starting Fake Confluence API server on port %d", s.port)
	if err := startHTTPServer(s.server, "Fake Confluence", s.listen); err != nil {
		s.server = nil
		return err
	}
	return nil
}

// Stop stops the fake Confluence server
func (s *FakeConfluenceServer) Stop() error {
	server := s.server
	s.server = nil
	if server != nil {
		return server.Close()
	}
	return nil
}

// handleGetPage handles GET requests for page content
func (s *FakeConfluenceServer) handleGetPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract page ID from URL: /rest/api/content/{pageId}
	pageID := r.URL.Path[len("/rest/api/content/"):]
	if pageID == "" {
		http.Error(w, "Page ID required", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	page, exists := s.pages[pageID]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// handleListSpaces handles GET requests for listing spaces
func (s *FakeConfluenceServer) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"results": []map[string]string{
			{"key": "TEST", "name": "Test Space"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetSettings returns the current settings for a page (for testing)
func (s *FakeConfluenceServer) GetSettings(pageID string) (*ConfluenceSettings, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	settings, exists := s.settings[pageID]
	return settings, exists
}

// GetPort returns the server port
func (s *FakeConfluenceServer) GetPort() int {
	return s.port
}
