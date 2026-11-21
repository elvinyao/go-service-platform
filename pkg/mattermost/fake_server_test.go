package mattermost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

const (
	testToken     = "bot-token"
	testBotID     = "bot-user-id"
	testChannelID = "channel-1"
)

// fakeMattermost emulates the parts of the Mattermost API the package uses.
type fakeMattermost struct {
	t      *testing.T
	server *httptest.Server

	mu           sync.Mutex
	meStatus     int
	meCalls      int
	channelCalls int
	posts        []Post
	postsByID    map[string]Post
	reactions    []map[string]string
	postStatus   []int // statuses returned by successive POST /posts calls; empty means 201
	wsQueries    []string
	onConnect    func(conn *websocket.Conn, connection int)
	connections  int
	wsStatus     int // non-zero rejects WebSocket upgrades with this status
}

func newFakeMattermost(t *testing.T) *fakeMattermost {
	t.Helper()
	f := &fakeMattermost{t: t, meStatus: http.StatusOK, postsByID: map[string]Post{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/users/me", f.handleMe)
	mux.HandleFunc("/api/v4/posts", f.handleCreatePost)
	mux.HandleFunc("/api/v4/posts/", f.handleGetPost)
	mux.HandleFunc("/api/v4/channels/", f.handleGetChannel)
	mux.HandleFunc("/api/v4/reactions", f.handleReaction)
	mux.HandleFunc("/api/v4/websocket", f.handleWebSocket)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeMattermost) client(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(f.server.URL, testToken, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func (f *fakeMattermost) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		writeAPIError(w, http.StatusUnauthorized, "api.context.session_expired.app_error", "Invalid or expired session.")
		return false
	}
	return true
}

func writeAPIError(w http.ResponseWriter, status int, id, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "message": message, "status_code": status})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (f *fakeMattermost) handleMe(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.meCalls++
	status := f.meStatus
	f.mu.Unlock()
	if status != http.StatusOK {
		writeAPIError(w, status, "me.error", "me failed")
		return
	}
	if !f.authorized(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, User{ID: testBotID, Username: "gsp-bot", IsBot: true})
}

func (f *fakeMattermost) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(w, r) {
		return
	}
	var post Post
	if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad.json", err.Error())
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.postStatus) > 0 {
		status := f.postStatus[0]
		f.postStatus = f.postStatus[1:]
		if status >= 300 {
			writeAPIError(w, status, "post.error", "post failed")
			return
		}
	}
	post.ID = "created-" + post.PendingPostID
	post.UserID = testBotID
	f.posts = append(f.posts, post)
	writeJSON(w, http.StatusCreated, post)
}

func (f *fakeMattermost) handleGetPost(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v4/posts/")
	f.mu.Lock()
	post, ok := f.postsByID[id]
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "post.not_found", "not found")
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (f *fakeMattermost) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v4/channels/")
	f.mu.Lock()
	f.channelCalls++
	f.mu.Unlock()
	if id != testChannelID {
		writeAPIError(w, http.StatusNotFound, "channel.not_found", "not found")
		return
	}
	writeJSON(w, http.StatusOK, Channel{ID: id, TeamID: "team-1", Name: "deploy-approvals", DisplayName: "Deploy Approvals", Type: "O"})
}

func (f *fakeMattermost) handleReaction(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(w, r) {
		return
	}
	var reaction map[string]string
	_ = json.NewDecoder(r.Body).Decode(&reaction)
	f.mu.Lock()
	f.reactions = append(f.reactions, reaction)
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, reaction)
}

func (f *fakeMattermost) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(w, r) {
		return
	}
	f.mu.Lock()
	wsStatus := f.wsStatus
	f.mu.Unlock()
	if wsStatus != 0 {
		writeAPIError(w, wsStatus, "ws.unavailable", "unavailable")
		return
	}
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		f.t.Errorf("upgrade: %v", err)
		return
	}
	defer conn.Close()
	f.mu.Lock()
	f.connections++
	connection := f.connections
	f.wsQueries = append(f.wsQueries, r.URL.RawQuery)
	onConnect := f.onConnect
	f.mu.Unlock()
	if onConnect != nil {
		onConnect(conn, connection)
		return
	}
	readUntilClosed(conn)
}

func (f *fakeMattermost) queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.wsQueries...)
}

// readUntilClosed keeps reading so the server answers pings.
func readUntilClosed(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func sendEvent(t *testing.T, conn *websocket.Conn, event map[string]interface{}) {
	t.Helper()
	if err := conn.WriteJSON(event); err != nil {
		t.Errorf("send event: %v", err)
	}
}

func helloEvent(connectionID string) map[string]interface{} {
	return map[string]interface{}{
		"event": "hello", "seq": 0,
		"data":      map[string]interface{}{"connection_id": connectionID},
		"broadcast": map[string]interface{}{"user_id": testBotID},
	}
}

// postedEvent mirrors a Mattermost 12 "posted" frame: post and mentions are
// JSON-encoded strings.
func postedEvent(seq int, post Post, mentions []string) map[string]interface{} {
	encodedPost, _ := json.Marshal(post)
	data := map[string]interface{}{
		"channel_display_name": "Bot Test",
		"channel_name":         "bot-test",
		"channel_type":         "O",
		"post":                 string(encodedPost),
		"sender_name":          "@alice",
		"team_id":              "team-1",
	}
	if mentions != nil {
		encodedMentions, _ := json.Marshal(mentions)
		data["mentions"] = string(encodedMentions)
	}
	return map[string]interface{}{
		"event": "posted", "seq": seq, "data": data,
		"broadcast": map[string]interface{}{"channel_id": post.ChannelID},
	}
}

func reactionEvent(seq int, event, userID, postID, emoji string) map[string]interface{} {
	encoded, _ := json.Marshal(map[string]interface{}{
		"user_id": userID, "post_id": postID, "emoji_name": emoji, "create_at": 1795266862964, "channel_id": testChannelID,
	})
	return map[string]interface{}{
		"event": event, "seq": seq,
		"data":      map[string]interface{}{"reaction": string(encoded)},
		"broadcast": map[string]interface{}{"channel_id": testChannelID},
	}
}
