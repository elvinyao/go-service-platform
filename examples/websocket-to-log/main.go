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

	provider := ruleengine.NewYAMLProvider("yaml", getenv("WORKFLOW_RULES", "config/workflow-rules.yaml"), ruleengine.DefaultWorkflowName)
	engine, err := pipeline.New(
		ruleengine.DefaultEngineConfig(),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{executor.NewLogExecutor(), executor.NewHTTPExecutor()},
	)
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
