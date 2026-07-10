package ruleengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

type Composer struct {
	config    EngineConfig
	providers map[string]RuleProvider
}

type ComposerSnapshot struct {
	Config    EngineConfig       `json:"config"`
	Providers map[string]RuleSet `json:"providers"`
}

func NewComposer(config EngineConfig, providers map[string]RuleProvider) *Composer {
	return &Composer{
		config:    config,
		providers: providers,
	}
}

func (c *Composer) Snapshot(ctx context.Context) ComposerSnapshot {
	providerNames := make([]string, 0, len(c.providers))
	for name := range c.providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)

	providers := make(map[string]RuleSet, len(c.providers))
	for _, name := range providerNames {
		providers[name] = CloneRuleSet(c.providers[name].Snapshot(ctx))
	}

	return ComposerSnapshot{
		Config:    cloneEngineConfig(c.config),
		Providers: providers,
	}
}

func (c *Composer) BuildExecutionPlan(ctx context.Context, workflow string, msg Message) (ExecutionPlan, error) {
	policy, exists := c.config.LookupPolicyForWorkflow(workflow)
	if !exists {
		return ExecutionPlan{}, fmt.Errorf("workflow %s has no configured policy", workflow)
	}
	if !validCompositionMode(policy.Mode) {
		return ExecutionPlan{}, fmt.Errorf("workflow %s has invalid composition mode %q", workflow, policy.Mode)
	}
	providerNames := providerOrder(policy)

	if len(providerNames) == 0 {
		return ExecutionPlan{}, fmt.Errorf("workflow %s has no rule providers configured", workflow)
	}

	matchedByProvider := make(map[string][]Rule, len(providerNames))
	for _, name := range providerNames {
		provider, ok := c.providers[name]
		if !ok {
			return ExecutionPlan{}, fmt.Errorf("rule provider %s not found", name)
		}
		rs := CloneRuleSet(provider.Snapshot(ctx))
		if err := ValidateRuleSet(rs); err != nil {
			return ExecutionPlan{}, fmt.Errorf("rule provider %s published invalid rules: %w", name, err)
		}
		workflowRules := filterRulesByWorkflow(rs.Rules, workflow)
		matchedByProvider[name] = MatchRules(workflowRules, msg)
	}

	selected, ok := selectRules(policy, providerNames, matchedByProvider)
	if !ok {
		return ExecutionPlan{
			Workflow: workflow,
			Rules:    []Rule{},
			Actions:  []Action{},
		}, nil
	}

	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].Priority < selected[j].Priority
	})

	actions := collectActions(selected)
	mergeCfg := policy.ActionMerge
	if mergeCfg.Order == "" {
		mergeCfg = c.config.ActionMerge
	}

	actions = mergeActions(actions, mergeCfg)

	return ExecutionPlan{
		Workflow: workflow,
		Rules:    selected,
		Actions:  actions,
	}, nil
}

func filterRulesByWorkflow(rules []Rule, workflow string) []Rule {
	filtered := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if rule.Workflow == "" || rule.Workflow == workflow {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

func providerOrder(policy WorkflowPolicy) []string {
	if policy.Mode == CompositionPipeline && len(policy.PipelineOrder) > 0 {
		return append([]string(nil), policy.PipelineOrder...)
	}
	return append([]string(nil), policy.Providers...)
}

func selectRules(policy WorkflowPolicy, order []string, matched map[string][]Rule) ([]Rule, bool) {
	switch policy.Mode {
	case CompositionSingle:
		first := order[0]
		rules := matched[first]
		return append([]Rule(nil), rules...), len(rules) > 0
	case CompositionAnd:
		all := make([]Rule, 0)
		for _, name := range order {
			rules := matched[name]
			if len(rules) == 0 {
				return nil, false
			}
			all = append(all, rules...)
		}
		return all, true
	case CompositionPipeline:
		all := make([]Rule, 0)
		for _, name := range order {
			rules := matched[name]
			if len(rules) == 0 {
				return nil, false
			}
			all = append(all, rules...)
		}
		return all, true
	case CompositionOr:
		all := make([]Rule, 0)
		for _, name := range order {
			all = append(all, matched[name]...)
		}
		return all, len(all) > 0
	default:
		return nil, false
	}
}

func collectActions(rules []Rule) []Action {
	out := make([]Action, 0)
	for _, r := range rules {
		for _, action := range r.Actions {
			out = append(out, cloneAction(action))
		}
	}
	return out
}

func mergeActions(actions []Action, cfg ActionMergeConfig) []Action {
	merged := append([]Action(nil), actions...)

	if cfg.Dedup {
		seen := make(map[string]struct{}, len(actions))
		deduped := make([]Action, 0, len(actions))
		for _, action := range merged {
			key := actionDedupKey(action)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			deduped = append(deduped, action)
		}
		merged = deduped
	}

	if cfg.Order == ActionOrderPriority {
		sort.SliceStable(merged, func(i, j int) bool {
			return merged[i].Priority < merged[j].Priority
		})
	}

	return merged
}

func actionDedupKey(action Action) string {
	if action.ID != "" {
		return action.ID
	}

	params := "{}"
	if len(action.Params) > 0 {
		if b, err := json.Marshal(action.Params); err == nil {
			params = string(b)
		}
	}

	return action.Executor + "|" + params
}

func cloneEngineConfig(cfg EngineConfig) EngineConfig {
	out := cfg
	out.Workflows = append([]WorkflowPolicy(nil), cfg.Workflows...)
	for i := range out.Workflows {
		out.Workflows[i].Providers = append([]string(nil), cfg.Workflows[i].Providers...)
		out.Workflows[i].PipelineOrder = append([]string(nil), cfg.Workflows[i].PipelineOrder...)
	}
	return out
}
