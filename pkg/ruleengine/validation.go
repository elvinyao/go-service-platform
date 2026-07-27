package ruleengine

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidateRuleSet validates rules published by static and dynamic providers.
func ValidateRuleSet(ruleSet RuleSet) error {
	ids := make(map[string]struct{}, len(ruleSet.Rules))
	for ruleIndex, rule := range ruleSet.Rules {
		prefix := fmt.Sprintf("rules[%d]", ruleIndex)
		if rule.ID == "" {
			return fmt.Errorf("%s.id is required", prefix)
		}
		if _, exists := ids[rule.ID]; exists {
			return fmt.Errorf("%s.id %q is duplicated", prefix, rule.ID)
		}
		ids[rule.ID] = struct{}{}

		if len(rule.Actions) == 0 {
			return fmt.Errorf("%s.actions is required", prefix)
		}
		for conditionIndex, condition := range rule.Conditions {
			conditionPrefix := fmt.Sprintf("%s.conditions[%d]", prefix, conditionIndex)
			if !validConditionField(condition.Field) {
				return fmt.Errorf("%s.field %q is invalid", conditionPrefix, condition.Field)
			}
			switch condition.Op {
			case OpEq, OpContains:
			case OpRegex:
				if _, err := regexp.Compile(normalizeValue(condition.Value)); err != nil {
					return fmt.Errorf("%s.value regex is invalid: %w", conditionPrefix, err)
				}
			default:
				return fmt.Errorf("%s.op %q is invalid", conditionPrefix, condition.Op)
			}
		}
		for actionIndex, action := range rule.Actions {
			if action.Executor == "" {
				return fmt.Errorf("%s.actions[%d].executor is required", prefix, actionIndex)
			}
		}
	}
	return nil
}

// ValidateProviderRuleSet validates a provider snapshot against engine wiring.
func ValidateProviderRuleSet(config EngineConfig, providerName string, ruleSet RuleSet) error {
	if err := ValidateRuleSet(ruleSet); err != nil {
		return err
	}

	for ruleIndex, rule := range ruleSet.Rules {
		if rule.Workflow == "" {
			continue
		}

		policy, exists := config.LookupPolicyForWorkflow(rule.Workflow)
		if !exists {
			return fmt.Errorf("rules[%d].workflow %q has no configured policy", ruleIndex, rule.Workflow)
		}
		if !workflowUsesProvider(policy, providerName) {
			return fmt.Errorf(
				"rules[%d].workflow %q cannot use provider %q",
				ruleIndex,
				rule.Workflow,
				providerName,
			)
		}
	}
	return nil
}

func workflowUsesProvider(policy WorkflowPolicy, providerName string) bool {
	if policy.Mode == CompositionSingle {
		return len(policy.Providers) > 0 && policy.Providers[0] == providerName
	}
	return containsName(policy.Providers, providerName)
}

func containsName(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validConditionField(field string) bool {
	switch field {
	case "id", "type", "content", "user_id", "timestamp":
		return true
	}

	if !strings.HasPrefix(field, "metadata.") {
		return false
	}
	for _, segment := range strings.Split(strings.TrimPrefix(field, "metadata."), ".") {
		if segment == "" {
			return false
		}
	}
	return true
}
