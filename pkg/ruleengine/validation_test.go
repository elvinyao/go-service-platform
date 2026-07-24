package ruleengine

import (
	"strings"
	"testing"
)

func TestValidateRuleSetAcceptsValidAndEmptySnapshots(t *testing.T) {
	if err := ValidateRuleSet(RuleSet{}); err != nil {
		t.Fatalf("validate empty snapshot: %v", err)
	}
	valid := RuleSet{Rules: []Rule{{
		ID: "valid",
		Conditions: []Condition{
			{Field: "type", Op: OpEq, Value: "AAA"},
			{Field: "metadata.environment", Op: OpContains, Value: "prod"},
			{Field: "content", Op: OpRegex, Value: "^deploy"},
		},
		Actions: []Action{{Executor: "log"}},
	}}}
	if err := ValidateRuleSet(valid); err != nil {
		t.Fatalf("validate rule set: %v", err)
	}
}

func TestValidateRuleSetRejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name string
		set  RuleSet
		want string
	}{
		{name: "missing id", set: RuleSet{Rules: []Rule{{Actions: []Action{{Executor: "log"}}}}}, want: ".id is required"},
		{name: "duplicate id", set: RuleSet{Rules: []Rule{{ID: "same", Actions: []Action{{Executor: "log"}}}, {ID: "same", Actions: []Action{{Executor: "log"}}}}}, want: "duplicated"},
		{name: "missing actions", set: RuleSet{Rules: []Rule{{ID: "rule"}}}, want: ".actions is required"},
		{name: "invalid field", set: validationRule(Condition{Field: "metadata.", Op: OpEq}), want: ".field"},
		{name: "empty metadata segment", set: validationRule(Condition{Field: "metadata..status", Op: OpEq}), want: ".field"},
		{name: "trailing metadata segment", set: validationRule(Condition{Field: "metadata.status.", Op: OpEq}), want: ".field"},
		{name: "unknown field", set: validationRule(Condition{Field: "typo", Op: OpEq}), want: ".field"},
		{name: "invalid operator", set: validationRule(Condition{Field: "type", Op: "equals"}), want: ".op"},
		{name: "invalid regex", set: validationRule(Condition{Field: "content", Op: OpRegex, Value: "["}), want: "regex is invalid"},
		{name: "missing executor", set: RuleSet{Rules: []Rule{{ID: "rule", Actions: []Action{{}}}}}, want: ".executor is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRuleSet(test.set)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateProviderRuleSetChecksWorkflowWiring(t *testing.T) {
	config := EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:      "primary",
				Providers: []string{"yaml"},
				Mode:      CompositionSingle,
				ActionMerge: ActionMergeConfig{
					Dedup: true,
					Order: ActionOrderPriority,
				},
			},
			{
				Name:      "secondary",
				Providers: []string{"remote"},
				Mode:      CompositionSingle,
				ActionMerge: ActionMergeConfig{
					Dedup: true,
					Order: ActionOrderPriority,
				},
			},
		},
		ActionMerge: ActionMergeConfig{Dedup: true, Order: ActionOrderPriority},
	}
	ruleSet := func(workflow string) RuleSet {
		return RuleSet{Rules: []Rule{{
			ID:       "rule",
			Workflow: workflow,
			Actions:  []Action{{Executor: "log"}},
		}}}
	}

	for _, workflow := range []string{"", "primary"} {
		if err := ValidateProviderRuleSet(config, "yaml", ruleSet(workflow)); err != nil {
			t.Fatalf("validate workflow %q: %v", workflow, err)
		}
	}

	tests := []struct {
		name     string
		provider string
		workflow string
		want     string
	}{
		{name: "unknown workflow", provider: "yaml", workflow: "typo", want: "has no configured policy"},
		{name: "unreachable provider", provider: "yaml", workflow: "secondary", want: "does not configure provider"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateProviderRuleSet(config, test.provider, ruleSet(test.workflow))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func validationRule(condition Condition) RuleSet {
	return RuleSet{Rules: []Rule{{
		ID:         "rule",
		Conditions: []Condition{condition},
		Actions:    []Action{{Executor: "log"}},
	}}}
}
