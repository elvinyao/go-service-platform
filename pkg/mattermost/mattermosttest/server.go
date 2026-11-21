// Package mattermosttest provides an in-process fake Mattermost server for
// testing bots built on package mattermost.
//
// The fake answers the REST calls the integration makes and streams events
// over /api/v4/websocket in the same shape as a Mattermost 12 server, so a
// test can drive a real mattermost.Listener and inspect what the bot posted:
//
//	srv := mattermosttest.NewServer(t)
//	client, _ := mattermost.NewClient(srv.URL, srv.Token, nil)
//	// ... start a listener and engine ...
//	srv.WaitConnected(t, time.Second)
//	srv.EmitPost(mattermosttest.PostEvent{ChannelID: srv.ChannelID, Message: "!ping"})
//	posts := srv.WaitForPosts(t, 1, time.Second)
package mattermosttest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/mattermost"
	"github.com/gorilla/websocket"
)

// Defaults used by a new Server.
const (
	DefaultToken       = "test-bot-token"
	DefaultBotID       = "test-bot-id"
	DefaultBotUsername = "test-bot"
	DefaultChannelID   = "test-channel-id"
	DefaultChannelName = "town-square"
	DefaultUserID      = "test-user-id"
	DefaultUsername    = "alice"
)

// Reaction is a reaction the bot added.
type Reaction struct {
	UserID    string `json:"user_id"`
	PostID    string `json:"post_id"`
	EmojiName string `json:"emoji_name"`
}

// PostEvent describes a post to stream to the bot. Empty fields take the
// server defaults; ID is generated when empty.
type PostEvent struct {
	ID          string
	ChannelID   string
	ChannelName string
	ChannelType string // "O", "P", "D" or "G"
	UserID      string
	Username    string
	RootID      string
	Message     string
	Mentions    []string
	FromBot     bool
}

// Server is a fake Mattermost. Its exported fields are fixed at creation.
type Server struct {
	URL       string
	Token     string
	BotID     string
	ChannelID string

	server *httptest.Server

	mu        sync.Mutex
	conns     []*websocket.Conn
	seq       int64
	nextID    int
	posts     []mattermost.Post
	postsByID map[string]mattermost.Post
	reactions []Reaction
	channels  map[string]mattermost.Channel
}

// NewServer starts a fake server that is closed when the test ends.
func NewServer(t testing.TB) *Server {
	t.Helper()
	s := &Server{
		Token:     DefaultToken,
		BotID:     DefaultBotID,
		ChannelID: DefaultChannelID,
		postsByID: map[string]mattermost.Post{},
		channels: map[string]mattermost.Channel{
			DefaultChannelID: {ID: DefaultChannelID, TeamID: "test-team-id", Name: DefaultChannelName, DisplayName: "Town Square", Type: "O"},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/users/me", s.authorized(s.handleMe))
	mux.HandleFunc("/api/v4/posts", s.authorized(s.handleCreatePost))
	mux.HandleFunc("/api/v4/posts/", s.authorized(s.handleGetPost))
	mux.HandleFunc("/api/v4/channels/", s.authorized(s.handleGetChannel))
	mux.HandleFunc("/api/v4/reactions", s.authorized(s.handleReaction))
	mux.HandleFunc("/api/v4/websocket", s.authorized(s.handleWebSocket))
	s.server = httptest.NewServer(mux)
	s.URL = s.server.URL
	t.Cleanup(s.Close)
	return s
}

// Close disconnects clients and stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	s.server.Close()
}

// AddChannel makes a channel known to GET /channels/{id}.
func (s *Server) AddChannel(channel mattermost.Channel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels[channel.ID] = channel
}

// Connected reports how many event stream clients are connected.
func (s *Server) Connected() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// WaitConnected waits until at least one client is connected.
func (s *Server) WaitConnected(t testing.TB, timeout time.Duration) {
	t.Helper()
	waitUntil(t, timeout, "an event stream client", func() bool { return s.Connected() > 0 })
}

// EmitPost streams a "posted" event and returns the post ID. The post is also
// stored, so the bot can look it up.
func (s *Server) EmitPost(event PostEvent) string {
	s.mu.Lock()
	if event.ID == "" {
		s.nextID++
		event.ID = fmt.Sprintf("post-%d", s.nextID)
	}
	s.mu.Unlock()
	event = s.withDefaults(event)

	post := mattermost.Post{ID: event.ID, ChannelID: event.ChannelID, RootID: event.RootID, UserID: event.UserID,
		Message: event.Message, CreateAt: time.Now().UnixMilli(), Props: map[string]interface{}{}}
	if event.FromBot {
		post.Props["from_bot"] = "true"
	}
	s.mu.Lock()
	s.postsByID[post.ID] = post
	s.mu.Unlock()

	encodedPost, _ := json.Marshal(post)
	data := map[string]interface{}{
		"channel_display_name": event.ChannelName,
		"channel_name":         event.ChannelName,
		"channel_type":         event.ChannelType,
		"post":                 string(encodedPost),
		"sender_name":          "@" + event.Username,
		"team_id":              "test-team-id",
	}
	if len(event.Mentions) > 0 {
		encodedMentions, _ := json.Marshal(event.Mentions)
		data["mentions"] = string(encodedMentions)
	}
	s.Emit("posted", data, event.ChannelID)
	return event.ID
}

// EmitReaction streams a "reaction_added" event from userID (the default user
// when empty) on postID in the default channel.
func (s *Server) EmitReaction(userID, postID, emoji string) {
	if userID == "" {
		userID = DefaultUserID
	}
	encoded, _ := json.Marshal(map[string]interface{}{
		"user_id": userID, "post_id": postID, "emoji_name": emoji,
		"channel_id": s.ChannelID, "create_at": time.Now().UnixMilli(),
	})
	s.Emit(mattermost.TypeReactionAdded, map[string]interface{}{"reaction": string(encoded)}, s.ChannelID)
}

// Emit streams an arbitrary event to every connected client.
func (s *Server) Emit(event string, data map[string]interface{}, channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	frame := map[string]interface{}{
		"event": event, "data": data, "seq": s.seq,
		"broadcast": map[string]interface{}{"channel_id": channelID},
	}
	for _, conn := range s.conns {
		_ = conn.WriteJSON(frame)
	}
}

// Posts returns the posts the bot created, oldest first.
func (s *Server) Posts() []mattermost.Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]mattermost.Post(nil), s.posts...)
}

// Reactions returns the reactions the bot added, oldest first.
func (s *Server) Reactions() []Reaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Reaction(nil), s.reactions...)
}

// WaitForPosts waits until the bot has created at least n posts.
func (s *Server) WaitForPosts(t testing.TB, n int, timeout time.Duration) []mattermost.Post {
	t.Helper()
	waitUntil(t, timeout, fmt.Sprintf("%d bot posts", n), func() bool { return len(s.Posts()) >= n })
	return s.Posts()
}

// WaitForReactions waits until the bot has added at least n reactions.
func (s *Server) WaitForReactions(t testing.TB, n int, timeout time.Duration) []Reaction {
	t.Helper()
	waitUntil(t, timeout, fmt.Sprintf("%d bot reactions", n), func() bool { return len(s.Reactions()) >= n })
	return s.Reactions()
}

func (s *Server) withDefaults(event PostEvent) PostEvent {
	if event.ChannelID == "" {
		event.ChannelID = s.ChannelID
	}
	if event.ChannelName == "" {
		event.ChannelName = DefaultChannelName
	}
	if event.ChannelType == "" {
		event.ChannelType = "O"
	}
	if event.UserID == "" {
		event.UserID = DefaultUserID
	}
	if event.Username == "" {
		event.Username = DefaultUsername
	}
	return event
}

func (s *Server) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.Token {
			writeError(w, http.StatusUnauthorized, "Invalid or expired session.")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, mattermost.User{ID: s.BotID, Username: DefaultBotUsername, IsBot: true})
}

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	var post mattermost.Post
	if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Like Mattermost, a repeated pending_post_id returns the first post.
	if post.PendingPostID != "" {
		for _, existing := range s.posts {
			if existing.PendingPostID == post.PendingPostID {
				writeJSON(w, http.StatusCreated, existing)
				return
			}
		}
	}
	s.nextID++
	post.ID = fmt.Sprintf("bot-post-%d", s.nextID)
	post.UserID = s.BotID
	post.CreateAt = time.Now().UnixMilli()
	s.posts = append(s.posts, post)
	s.postsByID[post.ID] = post
	writeJSON(w, http.StatusCreated, post)
}

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	post, ok := s.postsByID[strings.TrimPrefix(r.URL.Path, "/api/v4/posts/")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (s *Server) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	channel, ok := s.channels[strings.TrimPrefix(r.URL.Path, "/api/v4/channels/")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	writeJSON(w, http.StatusOK, channel)
}

func (s *Server) handleReaction(w http.ResponseWriter, r *http.Request) {
	var reaction Reaction
	if err := json.NewDecoder(r.Body).Decode(&reaction); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	s.reactions = append(s.reactions, reaction)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, reaction)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.seq = 0
	hello := map[string]interface{}{"event": "hello", "seq": s.seq, "data": map[string]interface{}{"connection_id": fmt.Sprintf("conn-%d", s.nextID)}}
	_ = conn.WriteJSON(hello)
	s.conns = append(s.conns, conn)
	s.mu.Unlock()

	// Reading keeps pings answered until the client goes away.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	s.mu.Lock()
	for i, existing := range s.conns {
		if existing == conn {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	_ = conn.Close()
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{"id": "fake.error", "message": message, "status_code": status})
}

func waitUntil(t testing.TB, timeout time.Duration, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
