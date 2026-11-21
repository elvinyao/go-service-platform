package mattermost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/executor"
)

func TestNewClientValidatesArguments(t *testing.T) {
	cases := map[string]struct{ url, token string }{
		"relative URL": {url: "/api", token: "t"},
		"bad scheme":   {url: "ftp://mm.example", token: "t"},
		"bad URL":      {url: "http://[::1", token: "t"},
		"no token":     {url: "http://mm.example", token: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewClient(tc.url, tc.token, nil); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}

func TestClientWebSocketURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8065":       "ws://localhost:8065/api/v4/websocket",
		"https://chat.example/mm/":    "wss://chat.example/mm/api/v4/websocket",
		"https://chat.example/prefix": "wss://chat.example/prefix/api/v4/websocket",
	}
	for serverURL, want := range cases {
		client, err := NewClient(serverURL, "t", nil)
		if err != nil {
			t.Fatalf("new client %s: %v", serverURL, err)
		}
		if got := client.webSocketURL(); got != want {
			t.Fatalf("webSocketURL(%s) = %s, want %s", serverURL, got, want)
		}
	}
}

func TestClientMeIsCached(t *testing.T) {
	fake := newFakeMattermost(t)
	client := fake.client(t)
	for i := 0; i < 3; i++ {
		me, err := client.Me(context.Background())
		if err != nil || me.ID != testBotID || me.Username != "gsp-bot" || !me.IsBot {
			t.Fatalf("me = %+v, %v", me, err)
		}
	}
	if fake.meCalls != 1 {
		t.Fatalf("me calls = %d, want 1", fake.meCalls)
	}
}

func TestClientClassifiesErrors(t *testing.T) {
	fake := newFakeMattermost(t)
	client := fake.client(t)

	fake.postStatus = []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError}
	for _, wantPermanent := range []bool{true, false, false} {
		_, err := client.CreatePost(context.Background(), Post{ChannelID: "c", Message: "m"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want APIError", err)
		}
		if executor.IsPermanent(err) != wantPermanent {
			t.Fatalf("status %d permanent = %v, want %v", apiErr.StatusCode, !wantPermanent, wantPermanent)
		}
		if apiErr.ID != "post.error" || !strings.Contains(apiErr.Error(), "post failed") {
			t.Fatalf("api error = %+v", apiErr)
		}
	}

	bad, _ := NewClient(fake.server.URL, "wrong", nil)
	if _, err := bad.Me(context.Background()); !executor.IsPermanent(err) {
		t.Fatalf("rejected token error = %v, want permanent", err)
	}
}

func TestClientReportsNonJSONErrorsAndTransportFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/posts/garbled" {
			_, _ = w.Write([]byte("{not json"))
			return
		}
		http.Error(w, "gateway down", http.StatusBadGateway)
	}))
	client, _ := NewClient(server.URL, "t", nil)

	_, err := client.GetChannel(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "gateway down" || executor.IsPermanent(err) {
		t.Fatalf("error = %v, want retryable plain-text API error", err)
	}
	if !strings.Contains(apiErr.Error(), "status 502: gateway down") {
		t.Fatalf("error text = %q", apiErr.Error())
	}
	if _, err := client.GetPost(context.Background(), "garbled"); err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("garbled body error = %v", err)
	}

	server.Close()
	if _, err := client.GetPost(nil, "x"); err == nil || executor.IsPermanent(err) {
		t.Fatalf("transport error = %v, want retryable", err)
	}
	if err := client.do(context.Background(), http.MethodPost, "/x", func() {}, nil); !executor.IsPermanent(err) {
		t.Fatalf("encode error = %v, want permanent", err)
	}
	if err := client.do(context.Background(), "BAD METHOD", "/x", nil, nil); !executor.IsPermanent(err) {
		t.Fatalf("request build error = %v, want permanent", err)
	}
}

func TestClientPostsReactsAndReads(t *testing.T) {
	fake := newFakeMattermost(t)
	client := fake.client(t)
	ctx := context.Background()

	created, err := client.CreatePost(ctx, Post{ChannelID: "c1", Message: "hi", RootID: "r1", PendingPostID: "p1"})
	if err != nil || created.ID != "created-p1" || created.RootID != "r1" {
		t.Fatalf("create post = %+v, %v", created, err)
	}
	fake.postsByID["p1"] = Post{ID: "p1", RootID: "root"}
	if post, err := client.GetPost(ctx, "p1"); err != nil || post.RootID != "root" {
		t.Fatalf("get post = %+v, %v", post, err)
	}
	if channel, err := client.GetChannel(ctx, testChannelID); err != nil || channel.Name != "deploy-approvals" {
		t.Fatalf("get channel = %+v, %v", channel, err)
	}
	if err := client.AddReaction(ctx, "p1", ":white_check_mark:"); err != nil {
		t.Fatalf("add reaction: %v", err)
	}
	if got := fake.reactions[0]; got["emoji_name"] != "white_check_mark" || got["user_id"] != testBotID || got["post_id"] != "p1" {
		t.Fatalf("reaction = %v", got)
	}

	bad, _ := NewClient(fake.server.URL, "wrong", nil)
	if err := bad.AddReaction(ctx, "p1", "x"); err == nil {
		t.Fatalf("expected reaction to fail without a valid token")
	}
	if client.ServerURL() != fake.server.URL {
		t.Fatalf("server URL = %s", client.ServerURL())
	}
}
