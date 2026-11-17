package ruleengine

import (
	"strings"
	"testing"
	"time"
)

func imMessage() Message {
	return Message{
		ID:      "evt-1",
		Type:    "message",
		Content: "!Deploy api to prod",
		UserID:  "u1",
		Payload: map[string]interface{}{
			"channel":  map[string]interface{}{"id": "C1", "type": "direct"},
			"mentions": []interface{}{"bot", "alice"},
			"tags":     []string{"ops"},
			"replies":  float64(3),
			"score":    "7.5",
			"thread":   nil,
		},
		Metadata: map[string]interface{}{"tenant": "acme"},
	}
}

func TestMatchConditionOperators(t *testing.T) {
	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{"eq", Condition{Field: "type", Op: OpEq, Value: "message"}, true},
		{"eq ignore case", Condition{Field: "type", Op: OpEq, Value: "MESSAGE", IgnoreCase: true}, true},
		{"eq case sensitive", Condition{Field: "type", Op: OpEq, Value: "MESSAGE"}, false},
		{"eq number", Condition{Field: "payload.replies", Op: OpEq, Value: 3}, true},
		{"ne", Condition{Field: "type", Op: OpNe, Value: "reaction"}, true},
		{"ne missing field", Condition{Field: "payload.absent", Op: OpNe, Value: "x"}, false},
		{"contains substring", Condition{Field: "content", Op: OpContains, Value: "api"}, true},
		{"contains ignore case", Condition{Field: "content", Op: OpContains, Value: "DEPLOY", IgnoreCase: true}, true},
		{"contains list member", Condition{Field: "payload.mentions", Op: OpContains, Value: "bot"}, true},
		{"contains typed list member", Condition{Field: "payload.tags", Op: OpContains, Value: "OPS", IgnoreCase: true}, true},
		{"contains list is not substring", Condition{Field: "payload.mentions", Op: OpContains, Value: "bo"}, false},
		{"not_contains", Condition{Field: "payload.mentions", Op: OpNotContains, Value: "bob"}, true},
		{"prefix", Condition{Field: "content", Op: OpPrefix, Value: "!deploy", IgnoreCase: true}, true},
		{"prefix case sensitive", Condition{Field: "content", Op: OpPrefix, Value: "!deploy"}, false},
		{"suffix", Condition{Field: "content", Op: OpSuffix, Value: "prod"}, true},
		{"regex", Condition{Field: "content", Op: OpRegex, Value: `^!\w+ api`}, true},
		{"regex ignore case", Condition{Field: "content", Op: OpRegex, Value: `^!deploy`, IgnoreCase: true}, true},
		{"regex invalid", Condition{Field: "content", Op: OpRegex, Value: `(`}, false},
		{"in", Condition{Field: "payload.channel.type", Op: OpIn, Value: []interface{}{"direct", "group"}}, true},
		{"in not a list", Condition{Field: "payload.channel.type", Op: OpIn, Value: "direct"}, false},
		{"not_in", Condition{Field: "payload.channel.type", Op: OpNotIn, Value: []string{"public"}}, true},
		{"exists", Condition{Field: "payload.channel.id", Op: OpExists}, true},
		{"exists null value", Condition{Field: "payload.thread", Op: OpExists}, true},
		{"exists missing", Condition{Field: "payload.absent", Op: OpExists}, false},
		{"not_exists", Condition{Field: "payload.absent", Op: OpNotExists}, true},
		{"not_exists nil payload path", Condition{Field: "payload.channel.id.deeper", Op: OpNotExists}, true},
		{"gt", Condition{Field: "payload.replies", Op: OpGt, Value: 2}, true},
		{"gte", Condition{Field: "payload.replies", Op: OpGte, Value: 3.0}, true},
		{"lt numeric string", Condition{Field: "payload.score", Op: OpLt, Value: uint(8)}, true},
		{"lte", Condition{Field: "payload.score", Op: OpLte, Value: "7.5"}, true},
		{"gt non numeric field", Condition{Field: "content", Op: OpGt, Value: 1}, false},
		{"gt non numeric value", Condition{Field: "payload.replies", Op: OpGt, Value: true}, false},
		{"list index", Condition{Field: "payload.mentions.1", Op: OpEq, Value: "alice"}, true},
		{"list index out of range", Condition{Field: "payload.mentions.5", Op: OpExists}, false},
		{"list index not a number", Condition{Field: "payload.mentions.first", Op: OpExists}, false},
		{"metadata", Condition{Field: "metadata.tenant", Op: OpEq, Value: "acme"}, true},
		{"unknown op", Condition{Field: "type", Op: "nope", Value: "message"}, false},
		{"unknown field", Condition{Field: "body", Op: OpEq, Value: "x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchCondition(tt.cond, imMessage()); got != tt.want {
				t.Fatalf("MatchCondition = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchConditionGroups(t *testing.T) {
	isMessage := Condition{Field: "type", Op: OpEq, Value: "message"}
	isReaction := Condition{Field: "type", Op: OpEq, Value: "reaction"}
	mentionsBot := Condition{Field: "payload.mentions", Op: OpContains, Value: "bot"}

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{"any one matches", Condition{Any: []Condition{isReaction, mentionsBot}}, true},
		{"any none match", Condition{Any: []Condition{isReaction, isReaction}}, false},
		{"all match", Condition{All: []Condition{isMessage, mentionsBot}}, true},
		{"all one fails", Condition{All: []Condition{isMessage, isReaction}}, false},
		{"not", Condition{Not: &isReaction}, true},
		{"nested", Condition{Any: []Condition{isReaction, {All: []Condition{isMessage, {Not: &isReaction}}}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchCondition(tt.cond, imMessage()); got != tt.want {
				t.Fatalf("MatchCondition = %v, want %v", got, tt.want)
			}
		})
	}

	rule := Rule{Enabled: true, Conditions: []Condition{isMessage, {Any: []Condition{isReaction, mentionsBot}}}}
	if !MatchRule(rule, imMessage()) {
		t.Fatalf("rule with nested group should match")
	}
}

func TestValidateRuleSetConditions(t *testing.T) {
	leaf := Condition{Field: "type", Op: OpEq, Value: "a"}
	tests := []struct {
		name    string
		cond    Condition
		wantErr string
	}{
		{"payload field", Condition{Field: "payload.channel.id", Op: OpEq, Value: "C1"}, ""},
		{"exists", Condition{Field: "payload.thread", Op: OpExists}, ""},
		{"in list", Condition{Field: "type", Op: OpIn, Value: []interface{}{"a", "b"}}, ""},
		{"numeric", Condition{Field: "payload.n", Op: OpGte, Value: 3}, ""},
		{"nested groups", Condition{Any: []Condition{leaf, {Not: &leaf}, {All: []Condition{leaf}}}}, ""},
		{"empty payload segment", Condition{Field: "payload..id", Op: OpEq, Value: "x"}, "is invalid"},
		{"exists with value", Condition{Field: "type", Op: OpNotExists, Value: "x"}, "must be omitted"},
		{"in scalar", Condition{Field: "type", Op: OpIn, Value: "a"}, "non-empty list"},
		{"in empty list", Condition{Field: "type", Op: OpNotIn, Value: []interface{}{}}, "non-empty list"},
		{"numeric with text", Condition{Field: "payload.n", Op: OpLt, Value: "many"}, "must be a number"},
		{"two groups", Condition{Any: []Condition{leaf}, All: []Condition{leaf}}, "only one of"},
		{"group with not", Condition{Any: []Condition{leaf}, Not: &leaf}, "only one of"},
		{"group with leaf fields", Condition{Field: "type", Any: []Condition{leaf}}, "must not combine"},
		{"invalid nested any", Condition{Any: []Condition{{Field: "type", Op: "nope"}}}, "any[0].op"},
		{"invalid nested all", Condition{All: []Condition{{Op: OpEq}}}, "all[0].field is required"},
		{"invalid nested not", Condition{Not: &Condition{Field: "type", Op: OpRegex, Value: "("}}, "not.value regex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRuleSet(RuleSet{Rules: []Rule{{
				ID:         "r1",
				Conditions: []Condition{tt.cond},
				Actions:    []Action{{Executor: "log"}},
			}}})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRuleSetActionExecutionSettings(t *testing.T) {
	tests := []struct {
		name    string
		action  Action
		wantErr string
	}{
		{"valid", Action{Executor: "log", Timeout: "2s", Retry: &RetryPolicy{MaxAttempts: 3, Backoff: "100ms", MaxBackoff: "1s"}}, ""},
		{"bad timeout", Action{Executor: "log", Timeout: "soon"}, "actions[0].timeout"},
		{"negative timeout", Action{Executor: "log", Timeout: "-1s"}, "must not be negative"},
		{"negative attempts", Action{Executor: "log", Retry: &RetryPolicy{MaxAttempts: -1}}, "max_attempts"},
		{"bad backoff", Action{Executor: "log", Retry: &RetryPolicy{Backoff: "x"}}, "retry.backoff"},
		{"bad max backoff", Action{Executor: "log", Retry: &RetryPolicy{MaxBackoff: "x"}}, "retry.max_backoff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRuleSet(RuleSet{Rules: []Rule{{ID: "r1", Actions: []Action{tt.action}}}})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveExecution(t *testing.T) {
	cfg := DefaultEngineConfig()

	policy, err := cfg.ResolveExecution(Action{})
	if err != nil {
		t.Fatalf("resolve defaults: %v", err)
	}
	if policy != (ExecutionPolicy{MaxAttempts: 1}) {
		t.Fatalf("default policy = %+v", policy)
	}

	cfg.Execution = ExecutionConfig{Timeout: "5s", Retry: RetryPolicy{MaxAttempts: 3, Backoff: "100ms", MaxBackoff: "1s"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	policy, err = cfg.ResolveExecution(Action{})
	if err != nil {
		t.Fatalf("resolve engine policy: %v", err)
	}
	want := ExecutionPolicy{Timeout: 5 * time.Second, MaxAttempts: 3, Backoff: 100 * time.Millisecond, MaxBackoff: time.Second}
	if policy != want {
		t.Fatalf("engine policy = %+v, want %+v", policy, want)
	}

	policy, err = cfg.ResolveExecution(Action{Timeout: "1s", Retry: &RetryPolicy{MaxAttempts: 2}})
	if err != nil {
		t.Fatalf("resolve action policy: %v", err)
	}
	if policy != (ExecutionPolicy{Timeout: time.Second, MaxAttempts: 2}) {
		t.Fatalf("action policy = %+v", policy)
	}

	for _, action := range []Action{
		{Timeout: "x"},
		{Retry: &RetryPolicy{Backoff: "x"}},
		{Retry: &RetryPolicy{MaxBackoff: "x"}},
	} {
		if _, err := cfg.ResolveExecution(action); err == nil {
			t.Fatalf("expected error for %+v", action)
		}
	}

	cfg.Execution.Timeout = "never"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "execution.timeout") {
		t.Fatalf("validate error = %v", err)
	}
}

func TestCloneRuleSetDeepCopiesNestedConditionsAndRetry(t *testing.T) {
	leaf := Condition{Field: "type", Op: OpIn, Value: []interface{}{"a"}}
	original := RuleSet{Rules: []Rule{{
		ID: "r1",
		Conditions: []Condition{
			{Any: []Condition{leaf}},
			{All: []Condition{leaf}},
			{Not: &Condition{Field: "type", Op: OpEq, Value: "a"}},
		},
		Actions: []Action{{Executor: "log", Retry: &RetryPolicy{MaxAttempts: 2}}},
	}}}

	cloned := CloneRuleSet(original)
	cloned.Rules[0].Conditions[0].Any[0].Value.([]interface{})[0] = "changed"
	cloned.Rules[0].Conditions[1].All[0].Field = "changed"
	cloned.Rules[0].Conditions[2].Not.Value = "changed"
	cloned.Rules[0].Actions[0].Retry.MaxAttempts = 9

	rule := original.Rules[0]
	if rule.Conditions[0].Any[0].Value.([]interface{})[0] != "a" ||
		rule.Conditions[1].All[0].Field != "type" ||
		rule.Conditions[2].Not.Value != "a" ||
		rule.Actions[0].Retry.MaxAttempts != 2 {
		t.Fatalf("clone mutated original: %+v", rule)
	}
}

func TestDispatchConfigDefaultsAndValidation(t *testing.T) {
	defaults := DispatchConfig{}.WithDefaults()
	want := DispatchConfig{
		Workers: 4, QueueSize: 64, QueueFull: QueueFullBlock,
		Dedup: DedupConfig{TTL: "10m", MaxEntries: 10000},
	}
	if defaults != want {
		t.Fatalf("defaults = %+v, want %+v", defaults, want)
	}
	if DefaultEngineConfig().Dispatch != want {
		t.Fatalf("engine default dispatch = %+v", DefaultEngineConfig().Dispatch)
	}
	if ttl, err := defaults.DedupTTL(); err != nil || ttl != 10*time.Minute {
		t.Fatalf("ttl = %v err = %v", ttl, err)
	}

	valid := DispatchConfig{Workers: 2, QueueSize: 8, OrderingKey: "payload.channel.id", QueueFull: QueueFullDrop}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	tests := []struct {
		name    string
		config  DispatchConfig
		wantErr string
	}{
		{"workers", DispatchConfig{Workers: -1}, "dispatch.workers"},
		{"queue size", DispatchConfig{QueueSize: -1}, "dispatch.queue_size"},
		{"queue full", DispatchConfig{QueueFull: "panic"}, "dispatch.queue_full"},
		{"ordering key", DispatchConfig{OrderingKey: "channel"}, "dispatch.ordering_key"},
		{"ttl syntax", DispatchConfig{Dedup: DedupConfig{TTL: "soon"}}, "dispatch.dedup.ttl"},
		{"ttl zero", DispatchConfig{Dedup: DedupConfig{TTL: "0s"}}, "greater than zero"},
		{"max entries", DispatchConfig{Dedup: DedupConfig{MaxEntries: -1}}, "dispatch.dedup.max_entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			engine := DefaultEngineConfig()
			engine.Dispatch = tt.config
			if err := engine.Validate(); err == nil {
				t.Fatalf("engine config should reject invalid dispatch settings")
			}
		})
	}
}

func TestMessageFieldAccessor(t *testing.T) {
	value, ok := imMessage().Field("payload.channel.id")
	if !ok || value != "C1" {
		t.Fatalf("field = %v, %v", value, ok)
	}
	if _, ok := imMessage().Field("payload.missing"); ok {
		t.Fatalf("missing field should not be present")
	}
}
