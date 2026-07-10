package ruleengine

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultWorkflowName = "WorkflowEngine"
)

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Workflows: []WorkflowPolicy{
			{
				Name:          DefaultWorkflowName,
				Providers:     []string{"yaml"},
				Mode:          CompositionSingle,
				PipelineOrder: []string{"yaml"},
				ActionMerge: ActionMergeConfig{
					Dedup: true,
					Order: ActionOrderPriority,
				},
			},
		},
		ActionMerge: ActionMergeConfig{
			Dedup: true,
			Order: ActionOrderPriority,
		},
	}
}

func LoadEngineConfig(path string) (EngineConfig, error) {
	cfg := DefaultEngineConfig()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return EngineConfig{}, fmt.Errorf("read rule engine config: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return EngineConfig{}, fmt.Errorf("parse rule engine config: %w", err)
	}
	if err := rejectAdditionalYAMLDocuments(decoder); err != nil {
		return EngineConfig{}, fmt.Errorf("parse rule engine config: %w", err)
	}

	normalizeEngineConfig(&cfg)
	if err := cfg.Validate(); err != nil {
		return EngineConfig{}, err
	}
	return cfg, nil
}

func normalizeEngineConfig(cfg *EngineConfig) {
	if cfg.ActionMerge.Order == "" {
		cfg.ActionMerge.Order = ActionOrderPriority
	}

	if len(cfg.Workflows) == 0 {
		return
	}

	for i := range cfg.Workflows {
		wf := &cfg.Workflows[i]
		if wf.Name == "" {
			wf.Name = DefaultWorkflowName
		}
		if wf.Mode == "" {
			wf.Mode = CompositionSingle
		}
		if wf.Providers == nil {
			wf.Providers = []string{"yaml"}
		}
		if wf.PipelineOrder == nil {
			wf.PipelineOrder = append([]string(nil), wf.Providers...)
		}
		if wf.ActionMerge.Order == "" {
			wf.ActionMerge.Order = cfg.ActionMerge.Order
		}
		if !wf.ActionMerge.dedupConfigured {
			wf.ActionMerge.Dedup = cfg.ActionMerge.Dedup
		}
	}
}

// UnmarshalYAML preserves whether dedup was explicitly configured so false can
// override a true global default while retaining strict nested-field validation.
func (c *ActionMergeConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("action merge config must be a mapping")
	}

	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, exists := seen[key]; exists {
			return fmt.Errorf("action merge config contains duplicate field %q", key)
		}
		seen[key] = struct{}{}

		value := node.Content[i+1]
		switch key {
		case "dedup":
			if err := value.Decode(&c.Dedup); err != nil {
				return fmt.Errorf("decode action merge dedup: %w", err)
			}
			c.dedupConfigured = true
		case "order":
			if err := value.Decode(&c.Order); err != nil {
				return fmt.Errorf("decode action merge order: %w", err)
			}
		default:
			return fmt.Errorf("field %s not found in action merge config", key)
		}
	}
	return nil
}

func (c EngineConfig) Validate() error {
	if len(c.Workflows) == 0 {
		return fmt.Errorf("workflows must contain at least one policy")
	}
	if err := validateActionMerge("action_merge", c.ActionMerge); err != nil {
		return err
	}

	workflowNames := make(map[string]struct{}, len(c.Workflows))
	for i, workflow := range c.Workflows {
		prefix := fmt.Sprintf("workflows[%d]", i)
		if workflow.Name == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if _, exists := workflowNames[workflow.Name]; exists {
			return fmt.Errorf("%s.name %q is duplicated", prefix, workflow.Name)
		}
		workflowNames[workflow.Name] = struct{}{}

		if !validCompositionMode(workflow.Mode) {
			return fmt.Errorf("%s.mode %q is invalid", prefix, workflow.Mode)
		}
		if err := validateUniqueNames(prefix+".providers", workflow.Providers); err != nil {
			return err
		}
		if err := validateActionMerge(prefix+".action_merge", workflow.ActionMerge); err != nil {
			return err
		}

		if workflow.Mode == CompositionPipeline {
			if err := validateUniqueNames(prefix+".pipeline_order", workflow.PipelineOrder); err != nil {
				return err
			}
			providers := make(map[string]struct{}, len(workflow.Providers))
			for _, provider := range workflow.Providers {
				providers[provider] = struct{}{}
			}
			if len(workflow.PipelineOrder) != len(workflow.Providers) {
				return fmt.Errorf("%s.pipeline_order must contain every configured provider", prefix)
			}
			for _, provider := range workflow.PipelineOrder {
				if _, exists := providers[provider]; !exists {
					return fmt.Errorf("%s.pipeline_order contains unconfigured provider %q", prefix, provider)
				}
			}
		}
	}
	return nil
}

func validateActionMerge(name string, config ActionMergeConfig) error {
	if config.Order != ActionOrderPriority {
		return fmt.Errorf("%s.order %q is invalid", name, config.Order)
	}
	return nil
}

func validateUniqueNames(name string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must contain at least one value", name)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("%s contains an empty value", name)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate value %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validCompositionMode(mode string) bool {
	switch mode {
	case CompositionSingle, CompositionOr, CompositionAnd, CompositionPipeline:
		return true
	default:
		return false
	}
}

func (c EngineConfig) PolicyForWorkflow(workflow string) WorkflowPolicy {
	if policy, exists := c.LookupPolicyForWorkflow(workflow); exists {
		return policy
	}

	if len(c.Workflows) > 0 {
		return c.Workflows[0]
	}

	return DefaultEngineConfig().Workflows[0]
}

// LookupPolicyForWorkflow returns the exact policy configured for a workflow.
func (c EngineConfig) LookupPolicyForWorkflow(workflow string) (WorkflowPolicy, bool) {
	for _, policy := range c.Workflows {
		if policy.Name == workflow {
			return policy, true
		}
	}
	return WorkflowPolicy{}, false
}
