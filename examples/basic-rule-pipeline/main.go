package main

import (
	"context"
	"fmt"
	"log"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func main() {
	ctx := context.Background()
	provider := ruleengine.NewStaticProvider("local", ruleengine.RuleSet{Rules: []ruleengine.Rule{
		{
			ID:       "greet-event",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "GREETING"},
			},
			Actions: []ruleengine.Action{
				{
					ID:       "log-greeting",
					Executor: "log",
					Params: map[string]interface{}{
						"level":    "info",
						"template": "Greeting from {{.UserID}}: {{.Content}}",
					},
				},
			},
		},
	}})

	config := ruleengine.DefaultEngineConfig()
	config.Workflows[0].Providers = []string{"local"}
	config.Workflows[0].PipelineOrder = []string{"local"}
	engine, err := pipeline.New(config, []ruleengine.RuleProvider{provider}, []executor.Executor{executor.NewLogExecutor()})
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.Start(ctx); err != nil {
		log.Fatal(err)
	}

	plan, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, ruleengine.Message{
		ID:      "example-1",
		Type:    "GREETING",
		Content: "hello from the framework",
		UserID:  "local-user",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("matched_rules=%d executed_actions=%d\n", len(plan.Rules), len(plan.Actions))
}
