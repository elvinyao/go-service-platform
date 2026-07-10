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

func TestComposerDedupsActionsWithoutIDByExecutorAndParams(t *testing.T) {
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

	rule := testRule("r1", "", 10, "log")
	rule.Actions = []Action{
		{Executor: "log", Priority: 20, Params: map[string]interface{}{"template": "same"}},
		{Executor: "log", Priority: 10, Params: map[string]interface{}{"template": "same"}},
	}
	composer := NewComposer(cfg, map[string]RuleProvider{"yaml": NewStaticProvider("yaml", RuleSet{Rules: []Rule{rule}})})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	require.Len(t, plan.Actions, 1)
}

func TestComposerSingleUsesOnlyFirstProvider(t *testing.T) {
	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:      DefaultWorkflowName,
				Providers: []string{"first", "second"},
				Mode:      CompositionSingle,
				ActionMerge: ActionMergeConfig{
					Dedup: true,
					Order: ActionOrderPriority,
				},
			},
		},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{
		"first":  NewStaticProvider("first", RuleSet{Rules: []Rule{testRule("first-rule", "first-action", 20, "log")}}),
		"second": NewStaticProvider("second", RuleSet{Rules: []Rule{testRule("second-rule", "second-action", 10, "log")}}),
	})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	require.Len(t, plan.Actions, 1)
	assert.Equal(t, "first-action", plan.Actions[0].ID)
}

func TestComposerOrCombinesAnyMatchingProvider(t *testing.T) {
	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:      DefaultWorkflowName,
				Providers: []string{"yaml", "confluence"},
				Mode:      CompositionOr,
				ActionMerge: ActionMergeConfig{
					Dedup: true,
					Order: ActionOrderPriority,
				},
			},
		},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{
		"yaml":       NewStaticProvider("yaml", RuleSet{Rules: []Rule{testRule("yaml-rule", "yaml-action", 20, "log")}}),
		"confluence": NewStaticProvider("confluence", RuleSet{Rules: []Rule{testRule("cf-rule", "cf-action", 10, "log")}}),
	})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	require.Len(t, plan.Actions, 2)
	assert.Equal(t, []string{"cf-action", "yaml-action"}, []string{plan.Actions[0].ID, plan.Actions[1].ID})
}

func TestComposerAndRequiresEveryProviderToMatch(t *testing.T) {
	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:      DefaultWorkflowName,
				Providers: []string{"yaml", "confluence"},
				Mode:      CompositionAnd,
				ActionMerge: ActionMergeConfig{
					Dedup: true,
					Order: ActionOrderPriority,
				},
			},
		},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{
		"yaml":       NewStaticProvider("yaml", RuleSet{Rules: []Rule{testRule("yaml-rule", "yaml-action", 10, "log")}}),
		"confluence": NewStaticProvider("confluence", RuleSet{Rules: []Rule{testRuleForType("cf-rule", "cf-action", 20, "log", "BBB")}}),
	})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	assert.Empty(t, plan.Actions)
}

func TestComposerReturnsErrorForUnknownProvider(t *testing.T) {
	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:      DefaultWorkflowName,
				Providers: []string{"missing"},
				Mode:      CompositionSingle,
			},
		},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{})
	_, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule provider missing not found")
}

func TestComposerReturnsErrorForUnknownWorkflow(t *testing.T) {
	composer := NewComposer(DefaultEngineConfig(), map[string]RuleProvider{
		"yaml": NewStaticProvider("yaml", RuleSet{}),
	})

	_, err := composer.BuildExecutionPlan(context.Background(), "missing", Message{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no configured policy")
}

func TestComposerIgnoresDisabledRules(t *testing.T) {
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

	disabled := testRule("disabled-rule", "disabled-action", 10, "log")
	disabled.Enabled = false
	composer := NewComposer(cfg, map[string]RuleProvider{
		"yaml": NewStaticProvider("yaml", RuleSet{Rules: []Rule{disabled}}),
	})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	assert.Empty(t, plan.Actions)
}

func TestComposerOrdersActionsByPriorityAcrossMatchedRules(t *testing.T) {
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

	composer := NewComposer(cfg, map[string]RuleProvider{
		"yaml": NewStaticProvider("yaml", RuleSet{Rules: []Rule{
			testRule("low-priority-rule", "third", 300, "log"),
			testRule("high-priority-rule", "first", 100, "log"),
			testRule("middle-priority-rule", "second", 200, "log"),
		}}),
	})

	plan, err := composer.BuildExecutionPlan(context.Background(), DefaultWorkflowName, Message{Type: "AAA"})
	require.NoError(t, err)
	require.Len(t, plan.Actions, 3)
	assert.Equal(t, []string{"first", "second", "third"}, []string{plan.Actions[0].ID, plan.Actions[1].ID, plan.Actions[2].ID})
}

func TestComposerSnapshotReturnsProviders(t *testing.T) {
	cfg := DefaultEngineConfig()
	provider := &staticProvider{
		name: "yaml",
		set: RuleSet{
			Source: "yaml",
			Rules: []Rule{{
				ID:       "r1",
				Workflow: DefaultWorkflowName,
				Enabled:  true,
				Actions: []Action{{
					ID:       "a1",
					Executor: "http",
					Params: map[string]interface{}{
						"headers": map[string]interface{}{"X-Test": "original"},
					},
				}},
			}},
		},
	}
	composer := NewComposer(cfg, map[string]RuleProvider{"yaml": provider})

	snapshot := composer.Snapshot(context.Background())
	require.NotEmpty(t, snapshot.Config.Workflows)
	require.Contains(t, snapshot.Providers, "yaml")
	require.Len(t, snapshot.Providers["yaml"].Rules, 1)
	snapshot.Providers["yaml"].Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"] = "changed"
	assert.Equal(t, "original", provider.set.Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"])
}

func testRule(id, actionID string, priority int, executor string) Rule {
	return testRuleForType(id, actionID, priority, executor, "AAA")
}

func testRuleForType(id, actionID string, priority int, executor string, msgType string) Rule {
	return Rule{
		ID:       id,
		Workflow: DefaultWorkflowName,
		Enabled:  true,
		Priority: priority,
		Conditions: []Condition{
			{Field: "type", Op: OpEq, Value: msgType},
		},
		Actions: []Action{{ID: actionID, Executor: executor, Priority: priority}},
	}
}
