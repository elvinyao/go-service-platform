package ruleengine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMatchCondition(t *testing.T) {
	msg := Message{
		ID:        "m1",
		Type:      "AAA",
		Content:   "urgent operation",
		UserID:    "u1",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"env": "prod",
			"nested": map[string]interface{}{
				"region": "us-east-1",
			},
		},
	}

	assert.True(t, MatchCondition(Condition{Field: "type", Op: OpEq, Value: "AAA"}, msg))
	assert.True(t, MatchCondition(Condition{Field: "content", Op: OpContains, Value: "urgent"}, msg))
	assert.True(t, MatchCondition(Condition{Field: "content", Op: OpRegex, Value: "^urgent"}, msg))
	assert.True(t, MatchCondition(Condition{Field: "metadata.env", Op: OpEq, Value: "prod"}, msg))
	assert.True(t, MatchCondition(Condition{Field: "metadata.nested.region", Op: OpEq, Value: "us-east-1"}, msg))
	assert.False(t, MatchCondition(Condition{Field: "metadata.nested.region", Op: OpEq, Value: "eu"}, msg))
}

func TestMatchRulesOrdersByPriority(t *testing.T) {
	msg := Message{Type: "AAA"}
	rules := []Rule{
		{ID: "r2", Enabled: true, Priority: 20, Conditions: []Condition{{Field: "type", Op: OpEq, Value: "AAA"}}},
		{ID: "r1", Enabled: true, Priority: 10, Conditions: []Condition{{Field: "type", Op: OpEq, Value: "AAA"}}},
	}

	matched := MatchRules(rules, msg)
	if assert.Len(t, matched, 2) {
		assert.Equal(t, "r1", matched[0].ID)
		assert.Equal(t, "r2", matched[1].ID)
	}
}

func TestMatchConditionSupportsMetadataAndRejectsMissingFields(t *testing.T) {
	msg := Message{
		Type: "AAA",
		Metadata: map[string]interface{}{
			"env": "prod",
			"nested": map[string]interface{}{
				"team": "platform",
			},
		},
	}

	if !MatchCondition(Condition{Field: "metadata.env", Op: OpEq, Value: "prod"}, msg) {
		t.Fatalf("metadata.env did not match")
	}
	if !MatchCondition(Condition{Field: "metadata.nested.team", Op: OpEq, Value: "platform"}, msg) {
		t.Fatalf("metadata.nested.team did not match")
	}
	if MatchCondition(Condition{Field: "metadata.nested.missing", Op: OpEq, Value: "x"}, msg) {
		t.Fatalf("missing metadata path matched")
	}
	if MatchCondition(Condition{Field: "unknown", Op: OpEq, Value: "x"}, msg) {
		t.Fatalf("unknown field matched")
	}
	if MatchCondition(Condition{Field: "type", Op: "unknown", Value: "AAA"}, msg) {
		t.Fatalf("unknown op matched")
	}
	if MatchCondition(Condition{Field: "type", Op: OpRegex, Value: "["}, msg) {
		t.Fatalf("invalid regex matched")
	}
}

func TestMatchRuleWithNoConditionsMatches(t *testing.T) {
	if !MatchRule(Rule{Enabled: true}, Message{}) {
		t.Fatalf("rule with no conditions should match")
	}
}
