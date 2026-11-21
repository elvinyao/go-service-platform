package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
	"github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/mattermost/mattermosttest"
)

func mattermostRuntimeConfig(serverURL, token string) config.RuntimeConfig {
	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"
	cfg.Inputs.WebSocket.Enabled = false
	cfg.Inputs.Mattermost.Enabled = true
	cfg.Inputs.Mattermost.ServerURL = serverURL
	cfg.Inputs.Mattermost.Token = token
	cfg.Inputs.Mattermost.ReconnectInterval = 20 * time.Millisecond
	return cfg
}

// TestAppRunsMattermostProfileRules boots the runtime with the repository's
// Mattermost profile rules against a fake Mattermost and checks the bot's
// answers.
func TestAppRunsMattermostProfileRules(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	application := New("test-bot", mattermostRuntimeConfig(srv.URL, srv.Token),
		"../../config/profiles/mattermost/rule-engine.yaml", "../../config/profiles/mattermost/workflow-rules.yaml")
	if err := application.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Stop(stopCtx); err != nil {
			t.Fatalf("stop: %v", err)
		}
	}()
	srv.WaitConnected(t, 3*time.Second)

	pingID := srv.EmitPost(mattermosttest.PostEvent{Message: "!ping"})
	posts := srv.WaitForPosts(t, 1, 3*time.Second)
	if posts[0].RootID != pingID || !strings.Contains(posts[0].Message, "pong") || !strings.Contains(posts[0].Message, "@alice") {
		t.Fatalf("ping reply = %+v", posts[0])
	}
	if reactions := srv.WaitForReactions(t, 1, 3*time.Second); reactions[0].PostID != pingID || reactions[0].EmojiName != "white_check_mark" {
		t.Fatalf("ping reaction = %+v", reactions)
	}

	// A mention from another bot is ignored; one from a person gets help.
	srv.EmitPost(mattermosttest.PostEvent{Message: "@test-bot hi", Mentions: []string{srv.BotID}, FromBot: true})
	helpID := srv.EmitPost(mattermosttest.PostEvent{Message: "@test-bot hi", Mentions: []string{srv.BotID}})
	posts = srv.WaitForPosts(t, 2, 3*time.Second)
	if posts[1].RootID != helpID || !strings.Contains(posts[1].Message, "!ping") {
		t.Fatalf("help reply = %+v", posts[1])
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(srv.Posts()); got != 2 {
		t.Fatalf("bot posted %d times, want 2", got)
	}
}

func TestRegisterServicesRejectsInvalidMattermostInput(t *testing.T) {
	cfg := mattermostRuntimeConfig("not a url", "token")
	err := registerServices(manager.NewServiceManager("test"), cfg, "test")
	if err == nil || !strings.Contains(err.Error(), "configure Mattermost input") {
		t.Fatalf("error = %v", err)
	}
}

func TestAppStartFailsWhenMattermostRejectsTheToken(t *testing.T) {
	srv := mattermosttest.NewServer(t)
	application := New("test-bot", mattermostRuntimeConfig(srv.URL, "wrong-token"),
		"../../config/profiles/mattermost/rule-engine.yaml", "../../config/profiles/mattermost/workflow-rules.yaml")
	// The manager logs the cause ("mattermost rejected the bot token").
	if err := application.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "start services") {
		t.Fatalf("start error = %v", err)
	}
	if application.State() != AppStateStopped {
		t.Fatalf("state = %s, want stopped after rollback", application.State())
	}
}

func TestSetupMattermostListenerReturnsMissingAndTypeMismatchErrors(t *testing.T) {
	ctx := context.Background()
	if err := setupMattermostListener(ctx, manager.NewServiceManager("test"), nil); err == nil || !strings.Contains(err.Error(), service.MattermostInputServiceName) {
		t.Fatalf("missing service error = %v", err)
	}
	wrongType := manager.NewServiceManager("test")
	wrongType.RegisterService(appTestService{name: service.MattermostInputServiceName})
	if err := setupMattermostListener(ctx, wrongType, nil); err == nil || !strings.Contains(err.Error(), "not of type") {
		t.Fatalf("wrong type error = %v", err)
	}
}

func TestStopInputsStopsEveryInputAndJoinsErrors(t *testing.T) {
	recorder := &stopRecorder{}
	sm := manager.NewServiceManager("test")
	sm.RegisterService(recordingStopService{appTestService: appTestService{name: service.WebSocketInputServiceName}, recorder: recorder, err: errors.New("ws boom")})
	sm.RegisterService(recordingStopService{appTestService: appTestService{name: service.MattermostInputServiceName}, recorder: recorder, err: errors.New("mm boom")})
	err := stopInputs(context.Background(), sm)
	if err == nil || !strings.Contains(err.Error(), "ws boom") || !strings.Contains(err.Error(), "mm boom") {
		t.Fatalf("error = %v, want both input errors", err)
	}
	if len(recorder.order) != 2 {
		t.Fatalf("stopped %v, want both inputs", recorder.order)
	}
}
