package fakeapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestFakeConfluenceGetPageAndUpdateSettings(t *testing.T) {
	server := NewFakeConfluenceServer(0)
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/content/", server.handleGetPage)
	mux.HandleFunc("/rest/api/space", server.handleListSpaces)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/rest/api/content/settings-page-1")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var page ConfluencePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.ID != "settings-page-1" {
		t.Fatalf("page ID = %q, want settings-page-1", page.ID)
	}
	if page.Version.Number != 1 {
		t.Fatalf("version = %d, want 1", page.Version.Number)
	}

	err = server.UpdateSettings("settings-page-1", &ConfluenceSettings{
		Rules: []SettingRule{{EventType: "DDD", Pattern: ".*", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	resp, err = http.Get(httpServer.URL + "/rest/api/content/settings-page-1")
	if err != nil {
		t.Fatalf("get updated page: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode updated page: %v", err)
	}
	if page.Version.Number != 2 {
		t.Fatalf("updated version = %d, want 2", page.Version.Number)
	}
}

func TestFakeConfluenceReturnsNotFoundAndHealth(t *testing.T) {
	server := NewFakeConfluenceServer(0)
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/content/", server.handleGetPage)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/rest/api/content/missing")
	if err != nil {
		t.Fatalf("get missing page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", resp.StatusCode)
	}

	resp, err = http.Get(httpServer.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestFakeWebSocketSendEndpointBroadcastsToClients(t *testing.T) {
	fake := NewFakeWebSocketServer(0)
	httpServer := newFakeWebSocketHTTPServer(fake)
	defer httpServer.Close()

	conn, _, err := websocket.DefaultDialer.Dial(httpToWS(httpServer.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var welcome WebSocketMessage
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "system" {
		t.Fatalf("welcome type = %q, want system", welcome.Type)
	}

	body := []byte(`{"type":"AAA","content":"hello","user_id":"u1"}`)
	resp, err := http.Post(httpServer.URL+"/api/send", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post send: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send status = %d, want 200", resp.StatusCode)
	}

	var broadcast WebSocketMessage
	if err := conn.ReadJSON(&broadcast); err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if broadcast.Type != "AAA" || broadcast.Content != "hello" || broadcast.ID == "" {
		t.Fatalf("broadcast = %+v", broadcast)
	}

	var sendResponse struct {
		Success bool             `json:"success"`
		Message WebSocketMessage `json:"message"`
		Clients int              `json:"clients"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sendResponse); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if !sendResponse.Success || sendResponse.Clients != 1 {
		t.Fatalf("send response = %+v, want success with 1 client", sendResponse)
	}
}

func TestFakeWebSocketListsReceivedClientMessagesAndHealth(t *testing.T) {
	fake := NewFakeWebSocketServer(0)
	httpServer := newFakeWebSocketHTTPServer(fake)
	defer httpServer.Close()

	conn, _, err := websocket.DefaultDialer.Dial(httpToWS(httpServer.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	var welcome WebSocketMessage
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	err = conn.WriteJSON(WebSocketMessage{ID: "client-msg", Type: "CLIENT", Content: "from client"})
	if err != nil {
		t.Fatalf("write client message: %v", err)
	}
	waitForReceivedMessages(t, fake, 1)

	resp, err := http.Get(httpServer.URL + "/api/messages")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	defer resp.Body.Close()
	var messages []WebSocketMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != "client-msg" {
		t.Fatalf("messages = %+v, want client-msg", messages)
	}

	resp, err = http.Get(httpServer.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health["status"] != "ok" {
		t.Fatalf("health = %+v", health)
	}
}

func newFakeWebSocketHTTPServer(fake *FakeWebSocketServer) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", fake.handleWebSocket)
	mux.HandleFunc("/api/send", fake.handleSendMessage)
	mux.HandleFunc("/api/messages", fake.handleListMessages)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "ok",
			"clients":       fake.GetClientCount(),
			"message_count": len(fake.GetReceivedMessages()),
		})
	})
	return httptest.NewServer(mux)
}

func waitForReceivedMessages(t *testing.T, fake *FakeWebSocketServer, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fake.GetReceivedMessages()) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("received messages len = %d, want at least %d", len(fake.GetReceivedMessages()), want)
}

func httpToWS(rawURL string) string {
	return "ws" + rawURL[len("http"):]
}
