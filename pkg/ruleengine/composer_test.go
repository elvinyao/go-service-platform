package ruleengine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticProvider struct {
	name string
	set  RuleSet
}

func (p *staticProvider) Name() string                         { return p.name }
func (p *staticProvider) Start(ctx context.Context) error      { return nil }
func (p *staticProvider) Snapshot(ctx context.Context) RuleSet { return p.set }

func TestComposerPipelineRequiresAllProvidersMatch(t *testing.T) {
	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:          DefaultWorkflowName,
				Providers:     []string{"yaml", "confluence"},
				Mode:          CompositionPipeline,
				PipelineOrder: []string{"yaml", "confluence"},
				ActionMerge:   ActionMergeConfig{Dedup: true, Order: ActionOrderPriority},
			},
		},
	}

	yamlProvider := &staticProvider{
		name: "yaml",
		set: RuleSet{
			Source: "yaml",
			Rules: []Rule{
				{
					ID:       "r-yaml",
					Workflow: DefaultWorkflowName,
					Enabled:  true,
					Priority: 10,
					Conditions: []Condition{
						{Field: "type", Op: OpEq, Value: "AAA"},
					},
					Actions: []Action{{ID: "a1", Executor: "log", Priority: 10}},
				},
			},
		},
	}
	cfProvider := &staticProvider{
		name: "confluence",
		set: RuleSet{
			Source: "confluence",
			Rules: []Rule{
				{
					ID:       "r-cf",
					Workflow: DefaultWorkflowName,
					Enabled:  true,
					Priority: 20,
					Conditions: []Condition{
						{Field: "content", Op: OpContains, Value: "urgent"},
					},
					Actions: []Action{{ID: "a2", Executor: "log", Priority: 20}},
				},
			},
		},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{
		"yaml":       yamlProvider,
		"confluence": cfProvider,
	})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA", Content: "normal"})
	require.NoError(t, err)
	assert.Empty(t, plan.Actions)

	plan, err = composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA", Content: "urgent event"})
	require.NoError(t, err)
	require.Len(t, plan.Actions, 2)
	assert.Equal(t, "a1", plan.Actions[0].ID)
	assert.Equal(t, "a2", plan.Actions[1].ID)
}

func TestComposerDedupsActionsByID(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.Workflows = []WorkflowPolicy{
		{
			Name:      DefaultWorkflowName,
			Providers: []string{"yaml"},
			Mode:      CompositionSingle,
			ActionMerge: ActionMergeConfig{
				Dedup: true,
				Order: ActionOrderPriority,
			},
		},
	}

	provider := &staticProvider{
		name: "yaml",
		set: RuleSet{
			Source: "yaml",
			Rules: []Rule{
				{
					ID:       "r1",
					Workflow: DefaultWorkflowName,
					Enabled:  true,
					Priority: 10,
					Conditions: []Condition{
						{Field: "type", Op: OpEq, Value: "AAA"},
					},
					Actions: []Action{
						{ID: "dup", Executor: "log", Priority: 10},
						{ID: "dup", Executor: "log", Priority: 20},
					},
				},
			},
		},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{"yaml": provider})
	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	require.Len(t, plan.Actions, 1)
	assert.Equal(t, "dup", plan.Actions[0].ID)
}

func TestComposerSnapshotReturnsProviders(t *testing.T) {
	cfg := DefaultEngineConfig()
	provider := &staticProvider{
		name: "yaml",
		set: RuleSet{
			Source: "yaml",
			Rules:  []Rule{{ID: "r1", Workflow: DefaultWorkflowName, Enabled: true}},
		},
	}
	composer := NewComposer(cfg, map[string]RuleProvider{"yaml": provider})

	snapshot := composer.Snapshot(context.Background())
	require.NotEmpty(t, snapshot.Config.Workflows)
	require.Contains(t, snapshot.Providers, "yaml")
	require.Len(t, snapshot.Providers["yaml"].Rules, 1)
}
