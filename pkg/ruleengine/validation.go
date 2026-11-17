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
			if err := validateCondition(fmt.Sprintf("%s.conditions[%d]", prefix, conditionIndex), condition); err != nil {
				return err
			}
		}
		for actionIndex, action := range rule.Actions {
			actionPrefix := fmt.Sprintf("%s.actions[%d]", prefix, actionIndex)
			if action.Executor == "" {
				return fmt.Errorf("%s.executor is required", actionPrefix)
			}
			if err := validateExecutionSettings(actionPrefix, action.Timeout, action.Retry); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateCondition checks one leaf comparison or logical group, recursively.
func validateCondition(prefix string, condition Condition) error {
	groups := 0
	if len(condition.All) > 0 {
		groups++
	}
	if len(condition.Any) > 0 {
		groups++
	}
	if condition.Not != nil {
		groups++
	}
	if groups > 1 {
		return fmt.Errorf("%s must use only one of all, any, or not", prefix)
	}
	if groups == 1 {
		if condition.Field != "" || condition.Op != "" || condition.Value != nil || condition.IgnoreCase {
			return fmt.Errorf("%s must not combine all, any, or not with field, op, value, or ignore_case", prefix)
		}
		for i, nested := range condition.All {
			if err := validateCondition(fmt.Sprintf("%s.all[%d]", prefix, i), nested); err != nil {
				return err
			}
		}
		for i, nested := range condition.Any {
			if err := validateCondition(fmt.Sprintf("%s.any[%d]", prefix, i), nested); err != nil {
				return err
			}
		}
		if condition.Not != nil {
			return validateCondition(prefix+".not", *condition.Not)
		}
		return nil
	}

	if condition.Field == "" {
		return fmt.Errorf("%s.field is required", prefix)
	}
	if !validConditionField(condition.Field) {
		return fmt.Errorf("%s.field %q is invalid", prefix, condition.Field)
	}
	switch condition.Op {
	case OpEq, OpNe, OpContains, OpNotContains, OpPrefix, OpSuffix:
	case OpExists, OpNotExists:
		if condition.Value != nil {
			return fmt.Errorf("%s.value must be omitted for op %q", prefix, condition.Op)
		}
	case OpRegex:
		if _, err := regexp.Compile(normalizeValue(condition.Value)); err != nil {
			return fmt.Errorf("%s.value regex is invalid: %w", prefix, err)
		}
	case OpIn, OpNotIn:
		if elements, ok := listElements(condition.Value); !ok || len(elements) == 0 {
			return fmt.Errorf("%s.value must be a non-empty list for op %q", prefix, condition.Op)
		}
	case OpGt, OpGte, OpLt, OpLte:
		if _, ok := toNumber(condition.Value); !ok {
			return fmt.Errorf("%s.value must be a number for op %q", prefix, condition.Op)
		}
	default:
		return fmt.Errorf("%s.op %q is invalid", prefix, condition.Op)
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

	path, ok := strings.CutPrefix(field, "metadata.")
	if !ok {
		if path, ok = strings.CutPrefix(field, "payload."); !ok {
			return false
		}
	}
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return false
		}
	}
	return true
}
