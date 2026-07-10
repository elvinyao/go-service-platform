package ruleengine

import "time"

const (
	OpEq       = "eq"
	OpContains = "contains"
	OpRegex    = "regex"
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

type Condition struct {
	Field string      `yaml:"field" json:"field"`
	Op    string      `yaml:"op" json:"op"`
	Value interface{} `yaml:"value" json:"value"`
}

type Action struct {
	ID       string                 `yaml:"id" json:"id"`
	Executor string                 `yaml:"executor" json:"executor"`
	Priority int                    `yaml:"priority" json:"priority"`
	Params   map[string]interface{} `yaml:"params" json:"params"`
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

type Message struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	UserID    string                 `json:"user_id"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
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
}

type ExecutionPlan struct {
	Workflow string
	Rules    []Rule
	Actions  []Action
}
