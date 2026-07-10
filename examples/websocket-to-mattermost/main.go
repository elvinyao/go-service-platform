package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	"github.com/gorilla/websocket"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	provider := ruleengine.NewStaticProvider("notifications", ruleengine.RuleSet{Rules: []ruleengine.Rule{
		{
			ID:       "notify-aaa",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AAA"},
			},
			Actions: []ruleengine.Action{
				{
					ID:       "post-notification",
					Executor: "http",
					Params: map[string]interface{}{
						"method": "POST",
						"url":    getenv("MATTERMOST_POST_URL", "http://localhost:8091/api/v4/posts"),
						"headers": map[string]interface{}{
							"Authorization": "Bearer " + getenv("MATTERMOST_API_TOKEN", "test-token-123"),
						},
						"body_template": `{"channel_id":"` + getenv("MATTERMOST_CHANNEL", "test-channel-1") + `","message":"Received {{.Type}}: {{.Content}}"}`,
					},
				},
			},
		},
	}})
	config := ruleengine.DefaultEngineConfig()
	config.Workflows[0].Providers = []string{"notifications"}
	config.Workflows[0].PipelineOrder = []string{"notifications"}
	engine, err := pipeline.New(config, []ruleengine.RuleProvider{provider}, []executor.Executor{executor.NewHTTPExecutor()})
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.Start(ctx); err != nil {
		log.Fatal(err)
	}

	connection, _, err := websocket.DefaultDialer.DialContext(ctx, getenv("WEBSOCKET_URL", "ws://localhost:8093/ws"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()
	go func() {
		<-ctx.Done()
		connection.Close()
	}()

	for {
		var message ruleengine.Message
		if err := connection.ReadJSON(&message); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatal(err)
		}
		if message.Type == "system" {
			continue
		}
		plan, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, message)
		if err != nil {
			log.Printf("message %s failed: %v", message.ID, err)
			continue
		}
		log.Printf("message=%s matched_rules=%d actions=%d", message.ID, len(plan.Rules), len(plan.Actions))
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
