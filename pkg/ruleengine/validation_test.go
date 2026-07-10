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

func validationRule(condition Condition) RuleSet {
	return RuleSet{Rules: []Rule{{
		ID:         "rule",
		Conditions: []Condition{condition},
		Actions:    []Action{{Executor: "log"}},
	}}}
}
