package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type collectExecutor struct {
	mu      sync.Mutex
	entries []string
}

func (e *collectExecutor) Type() string {
	return "collect"
}

func (e *collectExecutor) Execute(_ context.Context, message ruleengine.Message, action ruleengine.Action) error {
	template, ok := action.Params["template"].(string)
	if !ok || template == "" {
		return fmt.Errorf("collect action requires params.template")
	}
	entry, err := executor.RenderTemplate(template, message)
	if err != nil {
		return fmt.Errorf("render collect template: %w", err)
	}

	e.mu.Lock()
	e.entries = append(e.entries, entry)
	e.mu.Unlock()
	return nil
}

func (e *collectExecutor) Entries() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.entries...)
}

func main() {
	ctx := context.Background()
	collector := &collectExecutor{}
	provider := ruleengine.NewStaticProvider("local", ruleengine.RuleSet{Rules: []ruleengine.Rule{
		{
			ID:       "collect-audit-entry",
			Workflow: ruleengine.DefaultWorkflowName,
			Enabled:  true,
			Conditions: []ruleengine.Condition{
				{Field: "type", Op: ruleengine.OpEq, Value: "AUDIT"},
			},
			Actions: []ruleengine.Action{
				{
					ID:       "store-audit-entry",
					Executor: collector.Type(),
					Params: map[string]interface{}{
						"template": "event={{.ID}} user={{.UserID}} content={{.Content}}",
					},
				},
			},
		},
	}})

	config := ruleengine.DefaultEngineConfig()
	config.Workflows[0].Providers = []string{"local"}
	config.Workflows[0].PipelineOrder = []string{"local"}
	engine, err := pipeline.New(config, []ruleengine.RuleProvider{provider}, []executor.Executor{collector})
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.Start(ctx); err != nil {
		log.Fatal(err)
	}

	_, err = engine.Process(ctx, ruleengine.DefaultWorkflowName, ruleengine.Message{
		ID:      "audit-1",
		Type:    "AUDIT",
		Content: "configuration changed",
		UserID:  "local-user",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range collector.Entries() {
		fmt.Println(entry)
	}
}
