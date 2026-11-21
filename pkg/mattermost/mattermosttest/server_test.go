package mattermosttest_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/mattermost"
	"github.com/elvinyao/go-service-platform/pkg/mattermost/mattermosttest"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// TestBotAnswersThroughTheFakeServer shows how to test bot rules end to end:
// fake server -> Listener -> Engine -> Mattermost executors -> fake server.
func TestBotAnswersThroughTheFakeServer(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	client, err := mattermost.NewClient(srv.URL, srv.Token, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	rules := ruleengine.RuleSet{Rules: []ruleengine.Rule{{
		ID: "ping", Workflow: ruleengine.DefaultWorkflowName, Enabled: true,
		Conditions: []ruleengine.Condition{
			{Field: "type", Op: ruleengine.OpEq, Value: mattermost.TypeMessage},
			{Field: "content", Op: ruleengine.OpEq, Value: "!ping"},
		},
		Actions: []ruleengine.Action{
			{ID: "ack", Executor: mattermost.ReactionExecutorType, Priority: 1, Params: map[string]interface{}{"emoji": "eyes"}},
			{ID: "pong", Executor: mattermost.PostExecutorType, Priority: 2, Params: map[string]interface{}{
				"thread": true, "template": `pong @{{field "payload.sender.username"}}`,
			}},
		},
	}, {
		ID: "reaction", Workflow: ruleengine.DefaultWorkflowName, Enabled: true,
		Conditions: []ruleengine.Condition{{Field: "type", Op: ruleengine.OpEq, Value: mattermost.TypeReactionAdded}},
		Actions: []ruleengine.Action{{ID: "thanks", Executor: mattermost.PostExecutorType, Params: map[string]interface{}{
			"thread": true, "template": `thanks for :{{.Content}}: in ~{{field "payload.channel.name"}}`,
		}}},
	}}}
	config := ruleengine.DefaultEngineConfig()
	config.Workflows[0].Providers = []string{"rules"}
	config.Workflows[0].PipelineOrder = []string{"rules"}
	engine, err := pipeline.New(config,
		[]ruleengine.RuleProvider{ruleengine.NewStaticProvider("rules", rules)},
		[]executor.Executor{mattermost.NewPostExecutor(client), mattermost.NewReactionExecutor(client)})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}
	defer engine.Stop(context.Background())

	listener, err := mattermost.NewListener(client, mattermost.ListenerConfig{}, func(ctx context.Context, message ruleengine.Message) {
		if _, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, message); err != nil {
			t.Errorf("process %s: %v", message.ID, err)
		}
	})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	if err := listener.Start(context.Background()); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	defer listener.Stop(context.Background())
	srv.WaitConnected(t, 2*time.Second)

	pingID := srv.EmitPost(mattermosttest.PostEvent{Message: "!ping"})
	srv.EmitPost(mattermosttest.PostEvent{Message: "!ping", UserID: srv.BotID}) // the bot's own post is ignored
	posts := srv.WaitForPosts(t, 1, 2*time.Second)
	if posts[0].Message != "pong @alice" || posts[0].RootID != pingID || posts[0].ChannelID != srv.ChannelID {
		t.Fatalf("reply = %+v", posts[0])
	}
	if reactions := srv.WaitForReactions(t, 1, 2*time.Second); reactions[0].PostID != pingID || reactions[0].EmojiName != "eyes" || reactions[0].UserID != srv.BotID {
		t.Fatalf("reactions = %+v", reactions)
	}

	srv.EmitReaction("", pingID, "+1")
	posts = srv.WaitForPosts(t, 2, 2*time.Second)
	if posts[1].Message != "thanks for :+1: in ~town-square" || posts[1].RootID != pingID {
		t.Fatalf("reaction reply = %+v", posts[1])
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(srv.Posts()); got != 2 {
		t.Fatalf("bot posted %d times, want 2", got)
	}
}

func TestServerEmulatesTheRESTAPI(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	client, _ := mattermost.NewClient(srv.URL, srv.Token, nil)
	ctx := context.Background()

	first, err := client.CreatePost(ctx, mattermost.Post{ChannelID: "c", Message: "x", PendingPostID: "p"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	again, _ := client.CreatePost(ctx, mattermost.Post{ChannelID: "c", Message: "x", PendingPostID: "p"})
	if again.ID != first.ID || len(srv.Posts()) != 1 {
		t.Fatalf("repeated pending_post_id should return the first post")
	}
	if got, err := client.GetPost(ctx, first.ID); err != nil || got.Message != "x" {
		t.Fatalf("get post = %+v, %v", got, err)
	}
	if _, err := client.GetPost(ctx, "missing"); !executor.IsPermanent(err) {
		t.Fatalf("missing post error = %v", err)
	}

	srv.AddChannel(mattermost.Channel{ID: "ops", Name: "ops"})
	if channel, err := client.GetChannel(ctx, "ops"); err != nil || channel.Name != "ops" {
		t.Fatalf("channel = %+v, %v", channel, err)
	}
	if _, err := client.GetChannel(ctx, "missing"); err == nil {
		t.Fatalf("expected a missing channel error")
	}

	bad, _ := mattermost.NewClient(srv.URL, "wrong", nil)
	if _, err := bad.Me(ctx); !executor.IsPermanent(err) {
		t.Fatalf("wrong token error = %v", err)
	}
	for _, path := range []string{"/api/v4/posts", "/api/v4/reactions"} {
		request, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewBufferString("{"))
		request.Header.Set("Authorization", "Bearer "+srv.Token)
		response, err := http.DefaultClient.Do(request)
		if err != nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s with bad JSON = %v, %v", path, response, err)
		}
		response.Body.Close()
	}
	if srv.Connected() != 0 {
		t.Fatalf("no client should be connected")
	}
	srv.Close()
}

func TestServerEmitsOptionalPostFields(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	client, _ := mattermost.NewClient(srv.URL, srv.Token, nil)
	received := make(chan ruleengine.Message, 1)
	listener, _ := mattermost.NewListener(client, mattermost.ListenerConfig{}, func(_ context.Context, message ruleengine.Message) {
		received <- message
	})
	if err := listener.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Stop(context.Background())
	srv.WaitConnected(t, 2*time.Second)

	srv.EmitPost(mattermosttest.PostEvent{ID: "dm-1", ChannelType: "D", Message: "hi", Mentions: []string{srv.BotID}, FromBot: true})
	select {
	case message := <-received:
		if mentioned, _ := message.Field("payload.bot_mentioned"); mentioned != true {
			t.Fatalf("bot_mentioned = %v", mentioned)
		}
		if kind, _ := message.Field("payload.channel.type"); kind != "direct" {
			t.Fatalf("channel type = %v", kind)
		}
		if isBot, _ := message.Field("payload.sender.is_bot"); isBot != true {
			t.Fatalf("sender.is_bot = %v", isBot)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no event received")
	}
}
