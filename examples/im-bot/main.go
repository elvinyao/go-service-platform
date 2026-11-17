// Command im-bot routes chat events (messages, mentions, reactions, threads)
// to bot actions with YAML rules. It uses a local "reply" executor that prints
// what a real bot would post, so it runs without any chat service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// replyExecutor stands in for a chat API client. Its first call fails with a
// transient error to show the engine retrying an action.
type replyExecutor struct {
	calls atomic.Int32
}

func (e *replyExecutor) Type() string {
	return "reply"
}

func (e *replyExecutor) Execute(_ context.Context, message ruleengine.Message, action ruleengine.Action) error {
	template, _ := action.Params["template"].(string)
	if template == "" {
		// A configuration mistake cannot be fixed by retrying.
		return executor.Permanent(errors.New("reply action requires params.template"))
	}
	text, err := executor.RenderTemplate(template, message)
	if err != nil {
		return executor.Permanent(fmt.Errorf("render reply template: %w", err))
	}
	if e.calls.Add(1) == 1 {
		fmt.Println("  reply attempt failed (chat API temporarily unavailable); the engine retries")
		return errors.New("chat API temporarily unavailable")
	}

	channel, _ := message.Payload["channel"].(map[string]interface{})
	fmt.Printf("  reply to #%v: %s\n", channel["name"], text)
	return nil
}

func channel(id, name, kind string) map[string]interface{} {
	return map[string]interface{}{"id": id, "name": name, "type": kind}
}

func sampleEvents() []ruleengine.Message {
	return []ruleengine.Message{
		{
			ID: "evt-1", Type: "message", UserID: "alice", Content: "!Deploy billing-api",
			Payload: map[string]interface{}{"channel": channel("C1", "ops", "public")},
		},
		{
			ID: "evt-2", Type: "message", UserID: "bob", Content: "hey bot, what can you do?",
			Payload: map[string]interface{}{
				"channel":  channel("C2", "general", "public"),
				"mentions": []interface{}{"bot"},
			},
		},
		{
			ID: "evt-3", Type: "message", UserID: "carol", Content: "hello",
			Payload: map[string]interface{}{"channel": channel("D1", "carol", "direct")},
		},
		{
			ID: "evt-4", Type: "reaction_added", UserID: "dave",
			Payload: map[string]interface{}{
				"channel":  channel("C3", "release-approvals", "public"),
				"reaction": "white_check_mark",
			},
		},
		{
			ID: "evt-5", Type: "message", UserID: "erin", Content: "any update?",
			Payload: map[string]interface{}{
				"channel": channel("C1", "ops", "public"),
				"thread":  map[string]interface{}{"reply_count": 24},
			},
		},
		{
			ID: "evt-6", Type: "message", UserID: "frank", Content: "lunch?",
			Payload: map[string]interface{}{"channel": channel("C2", "general", "public")},
		},
	}
}

func main() {
	ctx := context.Background()
	rulesPath := os.Getenv("WORKFLOW_RULES")
	if rulesPath == "" {
		rulesPath = "examples/rules/im-bot.yaml"
	}

	engine, err := pipeline.New(
		ruleengine.DefaultEngineConfig(),
		[]ruleengine.RuleProvider{ruleengine.NewYAMLProvider("yaml", rulesPath, ruleengine.DefaultWorkflowName)},
		[]executor.Executor{executor.NewLogExecutor(), &replyExecutor{}},
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer engine.Stop(ctx)

	for _, event := range sampleEvents() {
		fmt.Printf("%s %s from %s: %q\n", event.ID, event.Type, event.UserID, event.Content)
		plan, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, event)
		if err != nil {
			fmt.Printf("  failed: %v\n", err)
			continue
		}
		if len(plan.Rules) == 0 {
			fmt.Println("  no rule matched")
			continue
		}
		for _, rule := range plan.Rules {
			fmt.Printf("  matched rule %s\n", rule.ID)
		}
	}
}
