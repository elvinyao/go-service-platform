package mattermost

import (
	"context"
	"net/http"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func postMessage(id, threadID string) ruleengine.Message {
	return ruleengine.Message{
		ID:      id,
		Type:    TypeMessage,
		Content: `say "hi"`,
		UserID:  "alice-id",
		Payload: map[string]interface{}{
			"post":    map[string]interface{}{"id": id, "thread_id": threadID},
			"channel": map[string]interface{}{"id": testChannelID},
			"sender":  map[string]interface{}{"username": "alice"},
		},
	}
}

func TestPostExecutorRepliesInChannelAndThread(t *testing.T) {
	fake := newFakeMattermost(t)
	exec := NewPostExecutor(fake.client(t))
	if exec.Type() != PostExecutorType {
		t.Fatalf("type = %s", exec.Type())
	}

	message := postMessage("p2", "p1")
	action := ruleengine.Action{ID: "reply", Params: map[string]interface{}{
		"template": `@{{field "payload.sender.username"}}: {{.Content}}`,
	}}
	if err := exec.Execute(context.Background(), message, action); err != nil {
		t.Fatalf("post: %v", err)
	}
	action.Params["thread"] = true
	action.Params["channel_id"] = "other-channel"
	if err := exec.Execute(context.Background(), message, action); err != nil {
		t.Fatalf("thread post: %v", err)
	}

	first, second := fake.posts[0], fake.posts[1]
	if first.ChannelID != testChannelID || first.RootID != "" || first.Message != `@alice: say "hi"` {
		t.Fatalf("channel post = %+v", first)
	}
	if second.ChannelID != "other-channel" || second.RootID != "p1" {
		t.Fatalf("thread post = %+v", second)
	}
	if first.PendingPostID == "" || first.PendingPostID == second.PendingPostID {
		t.Fatalf("pending post IDs should be set and differ per channel: %q %q", first.PendingPostID, second.PendingPostID)
	}
	if pendingPostID("m", "a", "c") != pendingPostID("m", "a", "c") || pendingPostID("", "a", "c") != "" {
		t.Fatalf("pending post ID must be stable and empty without a message ID")
	}
}

func TestPostExecutorFindsThreadRootForReactions(t *testing.T) {
	fake := newFakeMattermost(t)
	fake.postsByID["reply"] = Post{ID: "reply", RootID: "root"}
	fake.postsByID["root"] = Post{ID: "root"}
	exec := NewPostExecutor(fake.client(t))
	action := ruleengine.Action{ID: "a", Params: map[string]interface{}{"template": "ok", "thread": true}}

	for postID, wantRoot := range map[string]string{"reply": "root", "root": "root"} {
		message := postMessage("reaction-"+postID, "")
		message.Payload["post"] = map[string]interface{}{"id": postID}
		if err := exec.Execute(context.Background(), message, action); err != nil {
			t.Fatalf("thread reply to %s: %v", postID, err)
		}
		if got := fake.posts[len(fake.posts)-1].RootID; got != wantRoot {
			t.Fatalf("root for %s = %s, want %s", postID, got, wantRoot)
		}
	}

	missing := postMessage("x", "")
	missing.Payload["post"] = map[string]interface{}{"id": "unknown"}
	if err := exec.Execute(context.Background(), missing, action); !executor.IsPermanent(err) {
		t.Fatalf("unknown post error = %v, want permanent 404", err)
	}
}

func TestPostExecutorRejectsBadParamsPermanently(t *testing.T) {
	fake := newFakeMattermost(t)
	exec := NewPostExecutor(fake.client(t))
	noChannel := ruleengine.Message{ID: "m"}
	noPost := ruleengine.Message{ID: "m", Payload: map[string]interface{}{"channel": map[string]interface{}{"id": "c"}}}
	cases := map[string]struct {
		message ruleengine.Message
		params  map[string]interface{}
	}{
		"no template":       {message: postMessage("m", "m"), params: map[string]interface{}{}},
		"bad template":      {message: postMessage("m", "m"), params: map[string]interface{}{"template": "{{.Payload.nope}}"}},
		"no channel":        {message: noChannel, params: map[string]interface{}{"template": "x"}},
		"thread needs post": {message: noPost, params: map[string]interface{}{"template": "x", "thread": true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := exec.Execute(context.Background(), tc.message, ruleengine.Action{ID: "a", Params: tc.params})
			if !executor.IsPermanent(err) {
				t.Fatalf("error = %v, want permanent", err)
			}
		})
	}
	if len(fake.posts) != 0 {
		t.Fatalf("nothing should have been posted: %+v", fake.posts)
	}
}

func TestPostExecutorSurfacesRetryableFailures(t *testing.T) {
	fake := newFakeMattermost(t)
	fake.postStatus = []int{http.StatusServiceUnavailable}
	err := NewPostExecutor(fake.client(t)).Execute(context.Background(), postMessage("m", "m"),
		ruleengine.Action{ID: "a", Params: map[string]interface{}{"template": "x"}})
	if err == nil || executor.IsPermanent(err) {
		t.Fatalf("error = %v, want retryable", err)
	}
}

func TestReactionExecutor(t *testing.T) {
	fake := newFakeMattermost(t)
	exec := NewReactionExecutor(fake.client(t))
	if exec.Type() != ReactionExecutorType {
		t.Fatalf("type = %s", exec.Type())
	}
	ctx := context.Background()
	if err := exec.Execute(ctx, postMessage("p1", "p1"), ruleengine.Action{Params: map[string]interface{}{"emoji": "eyes"}}); err != nil {
		t.Fatalf("react: %v", err)
	}
	if err := exec.Execute(ctx, ruleengine.Message{}, ruleengine.Action{Params: map[string]interface{}{"emoji": "+1", "post_id": "p9"}}); err != nil {
		t.Fatalf("react to explicit post: %v", err)
	}
	if fake.reactions[0]["post_id"] != "p1" || fake.reactions[1]["post_id"] != "p9" {
		t.Fatalf("reactions = %v", fake.reactions)
	}
	if err := exec.Execute(ctx, postMessage("p1", "p1"), ruleengine.Action{Params: map[string]interface{}{}}); !executor.IsPermanent(err) {
		t.Fatalf("missing emoji error = %v", err)
	}
	if err := exec.Execute(ctx, ruleengine.Message{}, ruleengine.Action{Params: map[string]interface{}{"emoji": "x"}}); !executor.IsPermanent(err) {
		t.Fatalf("missing post error = %v", err)
	}
}
