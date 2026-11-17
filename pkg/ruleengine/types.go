package ruleengine

import "time"

// Condition operators. A leaf condition whose field is absent from the message
// never matches, except OpNotExists, which matches only then.
const (
	OpEq          = "eq"
	OpNe          = "ne"
	OpContains    = "contains"
	OpNotContains = "not_contains"
	OpPrefix      = "prefix"
	OpSuffix      = "suffix"
	OpRegex       = "regex"
	OpIn          = "in"
	OpNotIn       = "not_in"
	OpExists      = "exists"
	OpNotExists   = "not_exists"
	OpGt          = "gt"
	OpGte         = "gte"
	OpLt          = "lt"
	OpLte         = "lte"
)

const (
	CompositionSingle   = "single"
	CompositionPipeline = "pipeline"
	CompositionOr       = "or"
	CompositionAnd      = "and"
)

const (
	ActionOrderPriority = "priority"
)

// Condition is either a leaf comparison (Field, Op, Value) or exactly one
// logical group (All, Any, or Not) over nested conditions.
type Condition struct {
	Field string      `yaml:"field,omitempty" json:"field,omitempty"`
	Op    string      `yaml:"op,omitempty" json:"op,omitempty"`
	Value interface{} `yaml:"value,omitempty" json:"value,omitempty"`
	// IgnoreCase makes string comparisons of a leaf condition case-insensitive.
	IgnoreCase bool `yaml:"ignore_case,omitempty" json:"ignore_case,omitempty"`

	All []Condition `yaml:"all,omitempty" json:"all,omitempty"`
	Any []Condition `yaml:"any,omitempty" json:"any,omitempty"`
	Not *Condition  `yaml:"not,omitempty" json:"not,omitempty"`
}

// RetryPolicy controls how the pipeline retries a failed action.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts, including the first one.
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	// Backoff is the wait before the second attempt; it doubles on each retry.
	Backoff string `yaml:"backoff,omitempty" json:"backoff,omitempty"`
	// MaxBackoff caps the doubled backoff. Empty means uncapped.
	MaxBackoff string `yaml:"max_backoff,omitempty" json:"max_backoff,omitempty"`
}

// ExecutionConfig holds engine-wide defaults for running actions.
type ExecutionConfig struct {
	// Timeout bounds each attempt of an action. Empty or "0s" means no limit.
	Timeout string      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry   RetryPolicy `yaml:"retry" json:"retry"`
}

type Action struct {
	ID       string                 `yaml:"id" json:"id"`
	Executor string                 `yaml:"executor" json:"executor"`
	Priority int                    `yaml:"priority" json:"priority"`
	Params   map[string]interface{} `yaml:"params" json:"params"`
	// Timeout overrides the engine execution timeout for this action.
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	// Retry replaces the engine retry policy for this action.
	Retry *RetryPolicy `yaml:"retry,omitempty" json:"retry,omitempty"`
}

type Rule struct {
	ID         string      `yaml:"id" json:"id"`
	Workflow   string      `yaml:"workflow" json:"workflow"`
	Source     string      `yaml:"source" json:"source"`
	Enabled    bool        `yaml:"enabled" json:"enabled"`
	Priority   int         `yaml:"priority" json:"priority"`
	Conditions []Condition `yaml:"conditions" json:"conditions"`
	Actions    []Action    `yaml:"actions" json:"actions"`
}

type RuleSet struct {
	Source    string    `json:"source"`
	Version   string    `json:"version"`
	LoadedAt  time.Time `json:"loaded_at"`
	Rules     []Rule    `json:"rules"`
	LastError string    `json:"last_error,omitempty"`
}

// Message is the event evaluated by rules. The fixed fields cover the common
// envelope; Payload carries any event-specific data (for example an IM event's
// channel, mentions, or thread) and is addressable as "payload.<path>" in
// conditions and as .Payload in templates.
type Message struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	UserID    string                 `json:"user_id"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

type ActionMergeConfig struct {
	Dedup bool   `yaml:"dedup" json:"dedup"`
	Order string `yaml:"order" json:"order"`

	dedupConfigured bool
}

type WorkflowPolicy struct {
	Name          string            `yaml:"name" json:"name"`
	Providers     []string          `yaml:"providers" json:"providers"`
	Mode          string            `yaml:"mode" json:"mode"`
	PipelineOrder []string          `yaml:"pipeline_order" json:"pipeline_order"`
	ActionMerge   ActionMergeConfig `yaml:"action_merge" json:"action_merge"`
}

type EngineConfig struct {
	Workflows   []WorkflowPolicy  `yaml:"workflows" json:"workflows"`
	ActionMerge ActionMergeConfig `yaml:"action_merge" json:"action_merge"`
	Execution   ExecutionConfig   `yaml:"execution" json:"execution"`
}

type ExecutionPlan struct {
	Workflow string
	Rules    []Rule
	Actions  []Action
}
