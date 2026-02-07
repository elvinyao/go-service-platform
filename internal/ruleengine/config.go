package ruleengine

import (
	"errors"
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
				Providers:     []string{"yaml", "confluence"},
				Mode:          CompositionPipeline,
				PipelineOrder: []string{"yaml", "confluence"},
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
		Confluence: ConfluencePolicy{
			RefreshInterval:  "5m",
			OnRefreshFailure: "use_last_snapshot",
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
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return EngineConfig{}, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return EngineConfig{}, err
	}

	normalizeEngineConfig(&cfg)
	return cfg, nil
}

func normalizeEngineConfig(cfg *EngineConfig) {
	if cfg.ActionMerge.Order == "" {
		cfg.ActionMerge.Order = ActionOrderPriority
	}

	if len(cfg.Workflows) == 0 {
		cfg.Workflows = DefaultEngineConfig().Workflows
		return
	}

	for i := range cfg.Workflows {
		wf := &cfg.Workflows[i]
		if wf.Name == "" {
			wf.Name = DefaultWorkflowName
		}
		if wf.Mode == "" {
			wf.Mode = CompositionPipeline
		}
		if len(wf.Providers) == 0 {
			wf.Providers = []string{"yaml"}
		}
		if len(wf.PipelineOrder) == 0 {
			wf.PipelineOrder = append([]string(nil), wf.Providers...)
		}
		if wf.ActionMerge.Order == "" {
			wf.ActionMerge.Order = cfg.ActionMerge.Order
		}
		if !wf.ActionMerge.Dedup && cfg.ActionMerge.Dedup {
			wf.ActionMerge.Dedup = true
		}
	}
}

func (c EngineConfig) PolicyForWorkflow(workflow string) WorkflowPolicy {
	for _, policy := range c.Workflows {
		if policy.Name == workflow {
			return policy
		}
	}

	if len(c.Workflows) > 0 {
		return c.Workflows[0]
	}

	return DefaultEngineConfig().Workflows[0]
}
