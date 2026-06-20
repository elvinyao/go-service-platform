package ruleengine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

func MatchRules(rules []Rule, msg Message) []Rule {
	matched := make([]Rule, 0)
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if MatchRule(rule, msg) {
			matched = append(matched, rule)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Priority < matched[j].Priority
	})

	return matched
}

func MatchRule(rule Rule, msg Message) bool {
	if len(rule.Conditions) == 0 {
		return true
	}

	for _, cond := range rule.Conditions {
		if !MatchCondition(cond, msg) {
			return false
		}
	}
	return true
}

func MatchCondition(cond Condition, msg Message) bool {
	actual, ok := getMessageField(msg, cond.Field)
	if !ok {
		return false
	}

	actualStr := normalizeValue(actual)
	expectedStr := normalizeValue(cond.Value)

	switch cond.Op {
	case OpEq:
		return actualStr == expectedStr
	case OpContains:
		return strings.Contains(actualStr, expectedStr)
	case OpRegex:
		re, err := regexp.Compile(expectedStr)
		if err != nil {
			return false
		}
		return re.MatchString(actualStr)
	default:
		return false
	}
}

func getMessageField(msg Message, field string) (interface{}, bool) {
	switch field {
	case "id":
		return msg.ID, true
	case "type":
		return msg.Type, true
	case "content":
		return msg.Content, true
	case "user_id":
		return msg.UserID, true
	case "timestamp":
		return msg.Timestamp, true
	}

	if strings.HasPrefix(field, "metadata.") {
		keyPath := strings.TrimPrefix(field, "metadata.")
		return getPath(msg.Metadata, keyPath)
	}

	return nil, false
}

func getPath(root map[string]interface{}, keyPath string) (interface{}, bool) {
	if root == nil {
		return nil, false
	}

	parts := strings.Split(keyPath, ".")
	var cur interface{} = root

	for _, p := range parts {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := asMap[p]
		if !ok {
			return nil, false
		}
		cur = next
	}

	return cur, true
}

func normalizeValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", v)
	}
}
