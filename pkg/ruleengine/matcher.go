package ruleengine

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// MatchRule reports whether every top-level condition of the rule matches.
// A rule without conditions matches every message.
func MatchRule(rule Rule, msg Message) bool {
	return matchAll(rule.Conditions, msg)
}

func matchAll(conditions []Condition, msg Message) bool {
	for _, cond := range conditions {
		if !MatchCondition(cond, msg) {
			return false
		}
	}
	return true
}

// MatchCondition evaluates a leaf comparison or a logical group.
func MatchCondition(cond Condition, msg Message) bool {
	switch {
	case cond.Not != nil:
		return !MatchCondition(*cond.Not, msg)
	case len(cond.All) > 0:
		return matchAll(cond.All, msg)
	case len(cond.Any) > 0:
		for _, nested := range cond.Any {
			if MatchCondition(nested, msg) {
				return true
			}
		}
		return false
	}

	actual, ok := getMessageField(msg, cond.Field)
	switch cond.Op {
	case OpExists:
		return ok
	case OpNotExists:
		return !ok
	}
	if !ok {
		return false
	}

	switch cond.Op {
	case OpEq:
		return valuesEqual(actual, cond.Value, cond.IgnoreCase)
	case OpNe:
		return !valuesEqual(actual, cond.Value, cond.IgnoreCase)
	case OpContains:
		return valueContains(actual, cond.Value, cond.IgnoreCase)
	case OpNotContains:
		return !valueContains(actual, cond.Value, cond.IgnoreCase)
	case OpPrefix:
		return strings.HasPrefix(foldCase(normalizeValue(actual), cond.IgnoreCase), foldCase(normalizeValue(cond.Value), cond.IgnoreCase))
	case OpSuffix:
		return strings.HasSuffix(foldCase(normalizeValue(actual), cond.IgnoreCase), foldCase(normalizeValue(cond.Value), cond.IgnoreCase))
	case OpRegex:
		re, err := compileConditionRegex(cond)
		if err != nil {
			return false
		}
		return re.MatchString(normalizeValue(actual))
	case OpIn:
		return valueIn(actual, cond.Value, cond.IgnoreCase)
	case OpNotIn:
		return !valueIn(actual, cond.Value, cond.IgnoreCase)
	case OpGt, OpGte, OpLt, OpLte:
		return compareNumbers(cond.Op, actual, cond.Value)
	default:
		return false
	}
}

func valuesEqual(actual, expected interface{}, ignoreCase bool) bool {
	actualStr, expectedStr := normalizeValue(actual), normalizeValue(expected)
	if ignoreCase {
		return strings.EqualFold(actualStr, expectedStr)
	}
	return actualStr == expectedStr
}

// valueContains checks list membership when the field is a list and substring
// containment otherwise.
func valueContains(actual, expected interface{}, ignoreCase bool) bool {
	if elements, ok := listElements(actual); ok {
		for _, element := range elements {
			if valuesEqual(element, expected, ignoreCase) {
				return true
			}
		}
		return false
	}
	return strings.Contains(foldCase(normalizeValue(actual), ignoreCase), foldCase(normalizeValue(expected), ignoreCase))
}

func valueIn(actual, candidates interface{}, ignoreCase bool) bool {
	elements, ok := listElements(candidates)
	if !ok {
		return false
	}
	for _, candidate := range elements {
		if valuesEqual(actual, candidate, ignoreCase) {
			return true
		}
	}
	return false
}

func compareNumbers(op string, actual, expected interface{}) bool {
	left, ok := toNumber(actual)
	if !ok {
		return false
	}
	right, ok := toNumber(expected)
	if !ok {
		return false
	}
	switch op {
	case OpGt:
		return left > right
	case OpGte:
		return left >= right
	case OpLt:
		return left < right
	default:
		return left <= right
	}
}

func toNumber(value interface{}) (float64, bool) {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	case reflect.String:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(reflected.String()), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func listElements(value interface{}) ([]interface{}, bool) {
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return nil, false
	}
	elements := make([]interface{}, reflected.Len())
	for i := range elements {
		elements[i] = reflected.Index(i).Interface()
	}
	return elements, true
}

func foldCase(value string, ignoreCase bool) string {
	if ignoreCase {
		return strings.ToLower(value)
	}
	return value
}

var conditionRegexCache sync.Map // pattern string -> *regexp.Regexp

func compileConditionRegex(cond Condition) (*regexp.Regexp, error) {
	pattern := normalizeValue(cond.Value)
	if cond.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	if cached, ok := conditionRegexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	conditionRegexCache.Store(pattern, compiled)
	return compiled, nil
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

	if keyPath, ok := strings.CutPrefix(field, "metadata."); ok {
		return getPath(msg.Metadata, keyPath)
	}
	if keyPath, ok := strings.CutPrefix(field, "payload."); ok {
		return getPath(msg.Payload, keyPath)
	}

	return nil, false
}

// getPath walks nested maps by key and lists by zero-based index.
func getPath(root map[string]interface{}, keyPath string) (interface{}, bool) {
	if root == nil {
		return nil, false
	}

	var cur interface{} = root
	for _, part := range strings.Split(keyPath, ".") {
		switch node := cur.(type) {
		case map[string]interface{}:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = next
		default:
			elements, ok := listElements(cur)
			if !ok {
				return nil, false
			}
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(elements) {
				return nil, false
			}
			cur = elements[index]
		}
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
