package fakeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MattermostPost represents a message post
type MattermostPost struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	UserID    string    `json:"user_id"`
	Message   string    `json:"message"`
	CreateAt  int64     `json:"create_at"`
	Timestamp time.Time `json:"timestamp"`
}

// MattermostChannel represents a channel
type MattermostChannel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"` // O = open, P = private
}

// MattermostUser represents a user
type MattermostUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// FakeMattermostServer provides a mock Mattermost API
type FakeMattermostServer struct {
	mu          sync.RWMutex
	posts       []MattermostPost
	channels    map[string]*MattermostChannel
	users       map[string]*MattermostUser
	authTokens  map[string]string // token -> userID
	port        int
	wsPort      int
	server      *http.Server
	wsServer    *http.Server
	wsClients   map[*websocket.Conn]bool
	wsMu        sync.Mutex
	upgrader    websocket.Upgrader
	postCounter int
	listen      listenFunc
}

// NewFakeMattermostServer creates a new fake Mattermost server
func NewFakeMattermostServer(httpPort, wsPort int) *FakeMattermostServer {
	s := &FakeMattermostServer{
		posts:      make([]MattermostPost, 0),
		channels:   make(map[string]*MattermostChannel),
		users:      make(map[string]*MattermostUser),
		authTokens: make(map[string]string),
		wsClients:  make(map[*websocket.Conn]bool),
		port:       httpPort,
		wsPort:     wsPort,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for testing
			},
		},
	}

	// Add default test data
	s.addDefaultData()

	return s
}

func (s *FakeMattermostServer) addDefaultData() {
	// Add test user
	s.users["user-1"] = &MattermostUser{
		ID:       "user-1",
		Username: "testuser",
		Email:    "test@example.com",
	}

	// Add test channels
	s.channels["test-channel-1"] = &MattermostChannel{
		ID:          "test-channel-1",
		Name:        "test-channel",
		DisplayName: "Test Channel",
		Type:        "O",
	}
	s.channels["alerts-channel"] = &MattermostChannel{
		ID:          "alerts-channel",
		Name:        "alerts",
		DisplayName: "Alerts",
		Type:        "O",
	}
	s.channels["general-channel"] = &MattermostChannel{
		ID:          "general-channel",
		Name:        "general",
		DisplayName: "General",
		Type:        "O",
	}

	// Add valid auth token
	s.authTokens["test-token-123"] = "user-1"
}

// Start starts both HTTP and WebSocket servers
func (s *FakeMattermostServer) Start() error {
	// Start HTTP API server
	if err := s.startHTTPServer(); err != nil {
		return err
	}

	// Start WebSocket server
	if err := s.startWSServer(); err != nil {
		return err
	}

	return nil
}

func (s *FakeMattermostServer) startHTTPServer() error {
	mux := http.NewServeMux()

	// Authentication
	mux.HandleFunc("/api/v4/users/login", s.handleLogin)

	// Posts
	mux.HandleFunc("/api/v4/posts", s.handleCreatePost)

	// Channels
	mux.HandleFunc("/api/v4/channels/", s.handleGetChannel)

	// Users
	mux.HandleFunc("/api/v4/users/me", s.handleGetMe)

	// Health check
	mux.HandleFunc("/api/v4/system/ping", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
	})

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.authMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Starting Fake Mattermost HTTP API server on port %d", s.port)
	if err := startHTTPServer(s.server, "Fake Mattermost HTTP", s.listen); err != nil {
		s.server = nil
		return err
	}
	return nil
}

func (s *FakeMattermostServer) startWSServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/websocket", s.handleWebSocket)

	s.wsServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.wsPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Starting Fake Mattermost WebSocket server on port %d", s.wsPort)
	if err := startHTTPServer(s.wsServer, "Fake Mattermost WebSocket", s.listen); err != nil {
		s.wsServer = nil
		if s.server != nil {
			_ = s.server.Close()
			s.server = nil
		}
		return err
	}
	return nil
}

// Stop stops both servers
func (s *FakeMattermostServer) Stop() error {
	// Close all WebSocket connections
	s.wsMu.Lock()
	for conn := range s.wsClients {
		_ = conn.Close()
	}
	s.wsClients = make(map[*websocket.Conn]bool)
	s.wsMu.Unlock()

	wsServer := s.wsServer
	httpServer := s.server
	s.wsServer = nil
	s.server = nil

	var errs []error
	if wsServer != nil {
		if err := wsServer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close WebSocket server: %w", err))
		}
	}
	if httpServer != nil {
		if err := httpServer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close HTTP server: %w", err))
		}
	}
	return errors.Join(errs...)
}

// authMiddleware checks for valid auth token
func (s *FakeMattermostServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for login and ping
		if r.URL.Path == "/api/v4/users/login" || r.URL.Path == "/api/v4/system/ping" {
			next.ServeHTTP(w, r)
			return
		}

		token, ok := extractAuthToken(r.Header.Get("Authorization"), r.Header.Get("Token"))
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		s.mu.RLock()
		_, valid := s.authTokens[token]
		s.mu.RUnlock()

		if !valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractAuthToken(authHeader, tokenHeader string) (string, bool) {
	authHeader = strings.TrimSpace(authHeader)
	if authHeader != "" {
		parts := strings.Fields(authHeader)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			return parts[1], true
		}
		return "", false
	}

	tokenHeader = strings.TrimSpace(tokenHeader)
	if tokenHeader != "" {
		return tokenHeader, true
	}

	return "", false
}

// handleLogin handles user login
func (s *FakeMattermostServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var loginReq struct {
		LoginID  string `json:"login_id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Accept any login for testing
	w.Header().Set("Token", "test-token-123")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.users["user-1"])
}

// handleCreatePost handles creating a new post
func (s *FakeMattermostServer) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var post struct {
		ChannelID string `json:"channel_id"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.postCounter++
	newPost := MattermostPost{
		ID:        fmt.Sprintf("post-%d", s.postCounter),
		ChannelID: post.ChannelID,
		UserID:    "user-1",
		Message:   post.Message,
		CreateAt:  time.Now().UnixMilli(),
		Timestamp: time.Now(),
	}
	s.posts = append(s.posts, newPost)
	s.mu.Unlock()

	log.Printf("Fake Mattermost received post: channel=%s, message=%s", post.ChannelID, post.Message)

	// Broadcast to WebSocket clients
	s.broadcastWSEvent("posted", map[string]interface{}{
		"post": newPost,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newPost)
}

// handleGetChannel handles getting channel info
func (s *FakeMattermostServer) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	channelID := r.URL.Path[len("/api/v4/channels/"):]
	s.mu.RLock()
	channel, exists := s.channels[channelID]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channel)
}

// handleGetMe handles getting current user info
func (s *FakeMattermostServer) handleGetMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.users["user-1"])
}

// handleWebSocket handles WebSocket connections
func (s *FakeMattermostServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.wsMu.Lock()
	s.wsClients[conn] = true
	s.wsMu.Unlock()

	log.Printf("Fake Mattermost WebSocket client connected")

	// Send hello event
	s.wsMu.Lock()
	conn.WriteJSON(map[string]interface{}{
		"event": "hello",
		"data": map[string]interface{}{
			"server_version": "6.0.0-fake",
		},
	})
	s.wsMu.Unlock()

	// Keep connection open and read messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	s.wsMu.Lock()
	delete(s.wsClients, conn)
	s.wsMu.Unlock()
}

// broadcastWSEvent sends an event to all WebSocket clients
func (s *FakeMattermostServer) broadcastWSEvent(eventType string, data map[string]interface{}) {
	event := map[string]interface{}{
		"event": eventType,
		"data":  data,
	}

	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	for conn := range s.wsClients {
		conn.WriteJSON(event)
	}
}

// GetPosts returns all received posts (for testing)
func (s *FakeMattermostServer) GetPosts() []MattermostPost {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]MattermostPost, len(s.posts))
	copy(result, s.posts)
	return result
}

// GetPostCount returns the number of posts received
func (s *FakeMattermostServer) GetPostCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.posts)
}

// ClearPosts clears all received posts (for testing)
func (s *FakeMattermostServer) ClearPosts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.posts = make([]MattermostPost, 0)
}

// GetHTTPPort returns the HTTP port
func (s *FakeMattermostServer) GetHTTPPort() int {
	return s.port
}

// GetWSPort returns the WebSocket port
func (s *FakeMattermostServer) GetWSPort() int {
	return s.wsPort
}
