package fakeapi

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fakeMemoryAddr string

func (a fakeMemoryAddr) Network() string { return "memory" }
func (a fakeMemoryAddr) String() string  { return string(a) }

type fakeMemoryListener struct {
	address net.Addr
	closed  chan struct{}
	once    sync.Once
}

func (l *fakeMemoryListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}
func (l *fakeMemoryListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}
func (l *fakeMemoryListener) Addr() net.Addr { return l.address }

func fakeMemoryListen(_ string, address string) (net.Listener, error) {
	return &fakeMemoryListener{address: fakeMemoryAddr(address), closed: make(chan struct{})}, nil
}

func TestFakeConfluenceHandlersAndLifecycle(t *testing.T) {
	server := NewFakeConfluenceServer(0)
	server.listen = fakeMemoryListen
	if server.GetPort() != 0 {
		t.Fatalf("port = %d, want 0", server.GetPort())
	}
	if settings, ok := server.GetSettings("settings-page-1"); !ok || len(settings.Rules) == 0 {
		t.Fatalf("default settings = %+v %v, want rules", settings, ok)
	}
	if _, ok := server.GetSettings("missing"); ok {
		t.Fatalf("missing settings found")
	}
	if err := server.UpdateSettings("missing", &ConfluenceSettings{}); err == nil {
		t.Fatalf("expected missing page update error")
	}

	methodNotAllowed := httptest.NewRecorder()
	server.handleGetPage(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/rest/api/content/settings-page-1", nil))
	if methodNotAllowed.Code != http.StatusMethodNotAllowed {
		t.Fatalf("page wrong method = %d, want 405", methodNotAllowed.Code)
	}

	missingID := httptest.NewRecorder()
	server.handleGetPage(missingID, httptest.NewRequest(http.MethodGet, "/rest/api/content/", nil))
	if missingID.Code != http.StatusBadRequest {
		t.Fatalf("missing page ID status = %d, want 400", missingID.Code)
	}

	spacesWrongMethod := httptest.NewRecorder()
	server.handleListSpaces(spacesWrongMethod, httptest.NewRequest(http.MethodPost, "/rest/api/space", nil))
	if spacesWrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("spaces wrong method = %d, want 405", spacesWrongMethod.Code)
	}
	spaces := httptest.NewRecorder()
	server.handleListSpaces(spaces, httptest.NewRequest(http.MethodGet, "/rest/api/space", nil))
	if spaces.Code != http.StatusOK {
		t.Fatalf("spaces status = %d, want 200", spaces.Code)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("start confluence: %v", err)
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("stop confluence: %v", err)
	}
	if err := NewFakeConfluenceServer(0).Stop(); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
}

func TestFakeMattermostHandlersWebSocketAndLifecycle(t *testing.T) {
	server := NewFakeMattermostServer(0, 0)
	server.listen = fakeMemoryListen
	if server.GetHTTPPort() != 0 || server.GetWSPort() != 0 {
		t.Fatalf("ports = %d/%d, want 0/0", server.GetHTTPPort(), server.GetWSPort())
	}

	loginWrongMethod := httptest.NewRecorder()
	server.handleLogin(loginWrongMethod, httptest.NewRequest(http.MethodGet, "/api/v4/users/login", nil))
	if loginWrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("login wrong method = %d, want 405", loginWrongMethod.Code)
	}
	loginBadJSON := httptest.NewRecorder()
	server.handleLogin(loginBadJSON, httptest.NewRequest(http.MethodPost, "/api/v4/users/login", bytes.NewBufferString("{")))
	if loginBadJSON.Code != http.StatusBadRequest {
		t.Fatalf("login bad json = %d, want 400", loginBadJSON.Code)
	}
	loginOK := httptest.NewRecorder()
	server.handleLogin(loginOK, httptest.NewRequest(http.MethodPost, "/api/v4/users/login", bytes.NewBufferString(`{"login_id":"u","password":"p"}`)))
	if loginOK.Code != http.StatusOK || loginOK.Header().Get("Token") != "test-token-123" {
		t.Fatalf("login response = %d token=%q", loginOK.Code, loginOK.Header().Get("Token"))
	}

	postWrongMethod := httptest.NewRecorder()
	server.handleCreatePost(postWrongMethod, httptest.NewRequest(http.MethodGet, "/api/v4/posts", nil))
	if postWrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post wrong method = %d, want 405", postWrongMethod.Code)
	}
	postBadJSON := httptest.NewRecorder()
	server.handleCreatePost(postBadJSON, httptest.NewRequest(http.MethodPost, "/api/v4/posts", bytes.NewBufferString("{")))
	if postBadJSON.Code != http.StatusBadRequest {
		t.Fatalf("post bad json = %d, want 400", postBadJSON.Code)
	}
	postOK := httptest.NewRecorder()
	server.handleCreatePost(postOK, httptest.NewRequest(http.MethodPost, "/api/v4/posts", bytes.NewBufferString(`{"channel_id":"test-channel-1","message":"hello"}`)))
	if postOK.Code != http.StatusOK {
		t.Fatalf("post status = %d, want 200", postOK.Code)
	}
	if server.GetPostCount() != 1 || len(server.GetPosts()) != 1 {
		t.Fatalf("post count/posts = %d/%d, want 1/1", server.GetPostCount(), len(server.GetPosts()))
	}
	server.ClearPosts()
	if server.GetPostCount() != 0 {
		t.Fatalf("post count after clear = %d, want 0", server.GetPostCount())
	}

	channelWrongMethod := httptest.NewRecorder()
	server.handleGetChannel(channelWrongMethod, httptest.NewRequest(http.MethodPost, "/api/v4/channels/test-channel-1", nil))
	if channelWrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("channel wrong method = %d, want 405", channelWrongMethod.Code)
	}
	channelMissing := httptest.NewRecorder()
	server.handleGetChannel(channelMissing, httptest.NewRequest(http.MethodGet, "/api/v4/channels/missing", nil))
	if channelMissing.Code != http.StatusNotFound {
		t.Fatalf("missing channel = %d, want 404", channelMissing.Code)
	}
	channelOK := httptest.NewRecorder()
	server.handleGetChannel(channelOK, httptest.NewRequest(http.MethodGet, "/api/v4/channels/test-channel-1", nil))
	if channelOK.Code != http.StatusOK {
		t.Fatalf("channel status = %d, want 200", channelOK.Code)
	}
	me := httptest.NewRecorder()
	server.handleGetMe(me, httptest.NewRequest(http.MethodGet, "/api/v4/users/me", nil))
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200", me.Code)
	}

	wsHTTP := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer wsHTTP.Close()
	conn, _, err := websocket.DefaultDialer.Dial(httpToWS(wsHTTP.URL), nil)
	if err != nil {
		t.Fatalf("dial mattermost websocket: %v", err)
	}
	defer conn.Close()
	var hello map[string]interface{}
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	server.broadcastWSEvent("posted", map[string]interface{}{"ok": true})
	var event map[string]interface{}
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if event["event"] != "posted" {
		t.Fatalf("event = %+v, want posted", event)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("start mattermost: %v", err)
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("stop mattermost: %v", err)
	}
	if err := NewFakeMattermostServer(0, 0).Stop(); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
}

func TestFakeWebSocketUtilityMethodsAndLifecycle(t *testing.T) {
	server := NewFakeWebSocketServer(0)
	if server.GetPort() != 0 {
		t.Fatalf("port = %d, want 0", server.GetPort())
	}

	handled := make(chan WebSocketMessage, 1)
	server.OnMessage(func(msg WebSocketMessage) {
		handled <- msg
	})
	httpServer := newFakeWebSocketHTTPServer(server)
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
	if err := conn.WriteJSON(WebSocketMessage{Type: "CLIENT", Content: "handled"}); err != nil {
		t.Fatalf("write client: %v", err)
	}
	select {
	case msg := <-handled:
		if msg.Type != "CLIENT" {
			t.Fatalf("handled msg = %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatalf("handler was not called")
	}

	server.SendTestEvent("AAA", "hello")
	var event WebSocketMessage
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read test event: %v", err)
	}
	if event.Type != "AAA" || event.ID == "" {
		t.Fatalf("event = %+v, want AAA with ID", event)
	}

	server.ClearMessages()
	if got := server.GetReceivedMessages(); len(got) != 0 {
		t.Fatalf("messages after clear = %+v", got)
	}

	sendWrongMethod := httptest.NewRecorder()
	server.handleSendMessage(sendWrongMethod, httptest.NewRequest(http.MethodGet, "/api/send", nil))
	if sendWrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("send wrong method = %d, want 405", sendWrongMethod.Code)
	}
	sendBadJSON := httptest.NewRecorder()
	server.handleSendMessage(sendBadJSON, httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBufferString("{")))
	if sendBadJSON.Code != http.StatusBadRequest {
		t.Fatalf("send bad json = %d, want 400", sendBadJSON.Code)
	}
	listWrongMethod := httptest.NewRecorder()
	server.handleListMessages(listWrongMethod, httptest.NewRequest(http.MethodPost, "/api/messages", nil))
	if listWrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("list wrong method = %d, want 405", listWrongMethod.Code)
	}

	lifecycle := NewFakeWebSocketServer(0)
	lifecycle.listen = fakeMemoryListen
	if err := lifecycle.Start(); err != nil {
		t.Fatalf("start websocket server: %v", err)
	}
	if err := lifecycle.Stop(); err != nil {
		t.Fatalf("stop websocket server: %v", err)
	}
	if err := NewFakeWebSocketServer(0).Stop(); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
}

func TestFakeWebSocketHTTPMessagesWithoutNetwork(t *testing.T) {
	server := NewFakeWebSocketServer(0)
	send := httptest.NewRecorder()
	server.handleSendMessage(send, httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBufferString(`{"type":"AAA","content":"from HTTP"}`)))
	if send.Code != http.StatusOK {
		t.Fatalf("send status = %d, want 200", send.Code)
	}
	if got := server.GetReceivedMessages(); len(got) != 1 || got[0].ID == "" || got[0].Type != "AAA" {
		t.Fatalf("messages after HTTP send = %+v", got)
	}

	list := httptest.NewRecorder()
	server.handleListMessages(list, httptest.NewRequest(http.MethodGet, "/api/messages", nil))
	var messages []WebSocketMessage
	if err := json.NewDecoder(list.Body).Decode(&messages); err != nil {
		t.Fatalf("decode listed messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Type != "AAA" {
		t.Fatalf("listed messages = %+v", messages)
	}
}

func TestFakeServersRejectInvalidListenAddressesWithoutNetwork(t *testing.T) {
	if err := NewFakeConfluenceServer(-1).Start(); err == nil {
		t.Fatalf("invalid Confluence port error = nil")
	}
	if err := NewFakeMattermostServer(-1, -1).Start(); err == nil {
		t.Fatalf("invalid Mattermost port error = nil")
	}
	if err := NewFakeWebSocketServer(-1).Start(); err == nil {
		t.Fatalf("invalid WebSocket port error = nil")
	}

	manager := NewFakeAPIManagerWithPorts(-1, -1, -1, -1)
	if err := manager.Start(); err == nil {
		t.Fatalf("invalid manager port error = nil")
	}
	if manager.IsRunning() {
		t.Fatalf("manager should not be running after startup failure")
	}
}

func TestFakeAPIManagerLifecycleAndAccessors(t *testing.T) {
	manager := NewFakeAPIManager()
	if manager.GetConfluence() == nil || manager.GetMattermost() == nil || manager.GetWebSocket() == nil {
		t.Fatalf("default manager did not create all servers")
	}

	manager = NewFakeAPIManagerWithPorts(0, 0, 0, 0)
	manager.GetConfluence().listen = fakeMemoryListen
	manager.GetMattermost().listen = fakeMemoryListen
	manager.GetWebSocket().listen = fakeMemoryListen
	if manager.IsRunning() {
		t.Fatalf("manager should not be running before start")
	}
	manager.PrintEndpoints()
	if err := manager.Start(); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	if !manager.IsRunning() {
		t.Fatalf("manager should be running")
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("stop manager: %v", err)
	}
	if manager.IsRunning() {
		t.Fatalf("manager should not be running after stop")
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestFakeHTTPServerRoutes(t *testing.T) {
	mattermost := NewFakeMattermostServer(0, 0)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/users/login", mattermost.handleLogin)
	mux.HandleFunc("/api/v4/posts", mattermost.handleCreatePost)
	mux.HandleFunc("/api/v4/channels/", mattermost.handleGetChannel)
	mux.HandleFunc("/api/v4/users/me", mattermost.handleGetMe)
	mux.HandleFunc("/api/v4/system/ping", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
	})
	handler := mattermost.authMiddleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v4/system/ping", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ping status = %d, want 200", rec.Code)
	}
}
