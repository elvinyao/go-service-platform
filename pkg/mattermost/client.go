// Package mattermost connects the rule engine to a Mattermost server as a bot.
//
// A Listener reads the bot's WebSocket event stream and turns posts and
// reactions into ruleengine.Message values. PostExecutor and
// ReactionExecutor answer from rules. Client is the small REST client both
// use. Authenticate with a bot access token; nothing here needs a password.
package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
)

const defaultRequestTimeout = 10 * time.Second

// User is the subset of a Mattermost user the integration needs.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

// Post is a Mattermost post. PendingPostID makes creation idempotent: the
// server returns the existing post when the same value is sent again shortly
// after, so a retried request does not post twice.
type Post struct {
	ID            string                 `json:"id,omitempty"`
	ChannelID     string                 `json:"channel_id"`
	RootID        string                 `json:"root_id,omitempty"`
	UserID        string                 `json:"user_id,omitempty"`
	Message       string                 `json:"message"`
	Type          string                 `json:"type,omitempty"`
	Props         map[string]interface{} `json:"props,omitempty"`
	PendingPostID string                 `json:"pending_post_id,omitempty"`
	CreateAt      int64                  `json:"create_at,omitempty"`
}

// Channel is the subset of a Mattermost channel the integration needs.
type Channel struct {
	ID          string `json:"id"`
	TeamID      string `json:"team_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
}

// APIError is a non-2xx response from the Mattermost API.
type APIError struct {
	StatusCode int
	ID         string
	Message    string
}

func (e *APIError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("mattermost API status %d (%s): %s", e.StatusCode, e.ID, e.Message)
	}
	return fmt.Sprintf("mattermost API status %d: %s", e.StatusCode, e.Message)
}

// Client calls the Mattermost REST API v4 as one bot. It is safe for
// concurrent use.
type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client

	meMu sync.Mutex
	me   *User
}

// NewClient validates serverURL (http or https) and token. A nil httpClient
// uses one with a 10s timeout.
func NewClient(serverURL, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(serverURL, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("mattermost server URL must be an absolute http or https URL, got %q", serverURL)
	}
	if token == "" {
		return nil, errors.New("mattermost bot token is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &Client{baseURL: parsed, token: token, http: httpClient}, nil
}

// ServerURL returns the server base URL.
func (c *Client) ServerURL() string {
	return c.baseURL.String()
}

// webSocketURL returns the event stream endpoint for the server.
func (c *Client) webSocketURL() string {
	ws := *c.baseURL
	if ws.Scheme == "https" {
		ws.Scheme = "wss"
	} else {
		ws.Scheme = "ws"
	}
	ws.Path = strings.TrimRight(ws.Path, "/") + "/api/v4/websocket"
	return ws.String()
}

// Me returns the authenticated bot user. A successful result is cached.
func (c *Client) Me(ctx context.Context) (User, error) {
	c.meMu.Lock()
	defer c.meMu.Unlock()
	if c.me != nil {
		return *c.me, nil
	}
	var me User
	if err := c.do(ctx, http.MethodGet, "/users/me", nil, &me); err != nil {
		return User{}, fmt.Errorf("get bot user: %w", err)
	}
	c.me = &me
	return me, nil
}

// CreatePost creates a post as the bot.
func (c *Client) CreatePost(ctx context.Context, post Post) (Post, error) {
	var created Post
	if err := c.do(ctx, http.MethodPost, "/posts", post, &created); err != nil {
		return Post{}, fmt.Errorf("create post: %w", err)
	}
	return created, nil
}

// GetPost returns a post by ID.
func (c *Client) GetPost(ctx context.Context, postID string) (Post, error) {
	var post Post
	if err := c.do(ctx, http.MethodGet, "/posts/"+url.PathEscape(postID), nil, &post); err != nil {
		return Post{}, fmt.Errorf("get post %s: %w", postID, err)
	}
	return post, nil
}

// GetChannel returns a channel by ID.
func (c *Client) GetChannel(ctx context.Context, channelID string) (Channel, error) {
	var channel Channel
	if err := c.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(channelID), nil, &channel); err != nil {
		return Channel{}, fmt.Errorf("get channel %s: %w", channelID, err)
	}
	return channel, nil
}

// AddReaction adds emoji (a name such as "white_check_mark", without colons)
// to a post as the bot. Adding a reaction that already exists succeeds.
func (c *Client) AddReaction(ctx context.Context, postID, emoji string) error {
	me, err := c.Me(ctx)
	if err != nil {
		return err
	}
	reaction := map[string]string{"user_id": me.ID, "post_id": postID, "emoji_name": strings.Trim(emoji, ":")}
	if err := c.do(ctx, http.MethodPost, "/reactions", reaction, nil); err != nil {
		return fmt.Errorf("add reaction %s: %w", emoji, err)
	}
	return nil
}

// do sends one API request. Client errors that a retry cannot fix (4xx other
// than 408 and 429) are wrapped with executor.Permanent.
func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return executor.Permanent(fmt.Errorf("encode request: %w", err))
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.ServerURL()+"/api/v4"+path, reader)
	if err != nil {
		return executor.Permanent(fmt.Errorf("build request: %w", err))
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: response.StatusCode}
		var payload struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Message != "" {
			apiErr.ID, apiErr.Message = payload.ID, payload.Message
		} else {
			apiErr.Message = strings.TrimSpace(string(data))
		}
		if isPermanentStatus(response.StatusCode) {
			return executor.Permanent(apiErr)
		}
		return apiErr
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func isPermanentStatus(status int) bool {
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return false
	}
	return status >= 400 && status < 500
}
