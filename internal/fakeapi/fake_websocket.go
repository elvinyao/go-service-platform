package fakeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketMessage represents a message sent/received over WebSocket
type WebSocketMessage struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	UserID    string                 `json:"user_id"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// FakeWebSocketServer provides a mock WebSocket server for testing
type FakeWebSocketServer struct {
	mu              sync.RWMutex
	port            int
	server          *http.Server
	clients         map[*websocket.Conn]bool
	upgrader        websocket.Upgrader
	receivedMsgs    []WebSocketMessage
	messageHandlers []func(msg WebSocketMessage)
	msgCounter      int
}

// NewFakeWebSocketServer creates a new fake WebSocket server
func NewFakeWebSocketServer(port int) *FakeWebSocketServer {
	return &FakeWebSocketServer{
		port:            port,
		clients:         make(map[*websocket.Conn]bool),
		receivedMsgs:    make([]WebSocketMessage, 0),
		messageHandlers: make([]func(msg WebSocketMessage), 0),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

// Start starts the WebSocket server
func (s *FakeWebSocketServer) Start() error {
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// HTTP endpoint to send test messages
	mux.HandleFunc("/api/send", s.handleSendMessage)

	// HTTP endpoint to list received messages
	mux.HandleFunc("/api/messages", s.handleListMessages)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "ok",
			"clients":       len(s.clients),
			"message_count": len(s.receivedMsgs),
		})
	})

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Starting Fake WebSocket server on port %d", s.port)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Fake WebSocket server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the server
func (s *FakeWebSocketServer) Stop() error {
	s.mu.Lock()
	for conn := range s.clients {
		conn.Close()
	}
	s.mu.Unlock()

	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleWebSocket handles WebSocket connections
func (s *FakeWebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	s.mu.Lock()
	s.clients[conn] = true
	clientCount := len(s.clients)
	s.mu.Unlock()

	log.Printf("Fake WebSocket client connected (total: %d)", clientCount)

	// Send welcome message
	welcomeMsg := WebSocketMessage{
		ID:        "welcome",
		Type:      "system",
		Content:   "Connected to Fake WebSocket Server",
		Timestamp: time.Now(),
	}
	conn.WriteJSON(welcomeMsg)

	// Read messages from client
	go s.readMessages(conn)
}

func (s *FakeWebSocketServer) readMessages(conn *websocket.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
		log.Printf("Fake WebSocket client disconnected")
	}()

	for {
		var msg WebSocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		// Set timestamp if not provided
		if msg.Timestamp.IsZero() {
			msg.Timestamp = time.Now()
		}

		// Store received message
		s.mu.Lock()
		s.receivedMsgs = append(s.receivedMsgs, msg)
		s.mu.Unlock()

		log.Printf("📥 Fake WebSocket received: type=%s, content=%s", msg.Type, msg.Content)

		// Call registered handlers
		s.mu.RLock()
		handlers := make([]func(WebSocketMessage), len(s.messageHandlers))
		copy(handlers, s.messageHandlers)
		s.mu.RUnlock()

		for _, handler := range handlers {
			handler(msg)
		}
	}
}

// handleSendMessage handles HTTP requests to send test messages
func (s *FakeWebSocketServer) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg WebSocketMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Generate ID if not provided
	if msg.ID == "" {
		s.mu.Lock()
		s.msgCounter++
		msg.ID = fmt.Sprintf("msg-%d", s.msgCounter)
		s.mu.Unlock()
	}

	// Set timestamp
	msg.Timestamp = time.Now()

	// Broadcast to all clients
	s.Broadcast(msg)

	log.Printf("📤 Fake WebSocket broadcast: type=%s, content=%s", msg.Type, msg.Content)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": msg,
		"clients": len(s.clients),
	})
}

// handleListMessages returns all received messages
func (s *FakeWebSocketServer) handleListMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	msgs := make([]WebSocketMessage, len(s.receivedMsgs))
	copy(msgs, s.receivedMsgs)
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

// Broadcast sends a message to all connected clients
func (s *FakeWebSocketServer) Broadcast(msg WebSocketMessage) {
	s.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		clients = append(clients, conn)
	}
	s.mu.RUnlock()

	for _, conn := range clients {
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("Error sending to client: %v", err)
		}
	}
}

// SendTestEvent sends a test event to all clients
func (s *FakeWebSocketServer) SendTestEvent(eventType, content string) {
	s.mu.Lock()
	s.msgCounter++
	msgID := fmt.Sprintf("test-msg-%d", s.msgCounter)
	s.mu.Unlock()

	msg := WebSocketMessage{
		ID:        msgID,
		Type:      eventType,
		Content:   content,
		UserID:    "test-user",
		Timestamp: time.Now(),
	}

	s.Broadcast(msg)
	log.Printf("📤 Sent test event: type=%s, content=%s", eventType, content)
}

// OnMessage registers a handler for incoming messages
func (s *FakeWebSocketServer) OnMessage(handler func(msg WebSocketMessage)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messageHandlers = append(s.messageHandlers, handler)
}

// GetReceivedMessages returns all received messages
func (s *FakeWebSocketServer) GetReceivedMessages() []WebSocketMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]WebSocketMessage, len(s.receivedMsgs))
	copy(result, s.receivedMsgs)
	return result
}

// ClearMessages clears all received messages
func (s *FakeWebSocketServer) ClearMessages() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receivedMsgs = make([]WebSocketMessage, 0)
}

// GetClientCount returns the number of connected clients
func (s *FakeWebSocketServer) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// GetPort returns the server port
func (s *FakeWebSocketServer) GetPort() int {
	return s.port
}
