package ruleengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type YAMLProvider struct {
	name     string
	path     string
	workflow string

	mu       sync.RWMutex
	snapshot RuleSet
}

type yamlRulesFile struct {
	Version string     `yaml:"version"`
	Rules   []yamlRule `yaml:"rules"`
}

type yamlRule struct {
	ID         string      `yaml:"id"`
	Workflow   string      `yaml:"workflow"`
	Enabled    *bool       `yaml:"enabled"`
	Priority   int         `yaml:"priority"`
	Conditions []Condition `yaml:"conditions"`
	Actions    []Action    `yaml:"actions"`
}

func NewYAMLProvider(name, path, workflow string) *YAMLProvider {
	return &YAMLProvider{
		name:     name,
		path:     path,
		workflow: workflow,
		snapshot: RuleSet{
			Source:  name,
			Version: "0",
			Rules:   []Rule{},
		},
	}
}

func (p *YAMLProvider) Name() string {
	return p.name
}

func (p *YAMLProvider) Start(ctx context.Context) error {
	snapshot, err := p.load(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.snapshot = snapshot
	p.mu.Unlock()
	return nil
}

func (p *YAMLProvider) Snapshot(ctx context.Context) RuleSet {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneRuleSet(p.snapshot)
}

func (p *YAMLProvider) load(ctx context.Context) (RuleSet, error) {
	if p.path == "" {
		return RuleSet{
			Source:   p.name,
			Version:  "1",
			LoadedAt: time.Now(),
			Rules:    []Rule{},
		}, nil
	}

	data, err := os.ReadFile(p.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RuleSet{
				Source:   p.name,
				Version:  "1",
				LoadedAt: time.Now(),
				Rules:    []Rule{},
			}, nil
		}
		return RuleSet{}, err
	}

	var file yamlRulesFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return RuleSet{}, err
	}

	rules := make([]Rule, 0, len(file.Rules))
	for i, r := range file.Rules {
		if err := validateRule(r, i); err != nil {
			return RuleSet{}, err
		}

		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}

		workflow := r.Workflow
		if workflow == "" {
			workflow = p.workflow
		}

		rule := Rule{
			ID:         r.ID,
			Workflow:   workflow,
			Source:     p.name,
			Enabled:    enabled,
			Priority:   r.Priority,
			Conditions: append([]Condition(nil), r.Conditions...),
			Actions:    append([]Action(nil), r.Actions...),
		}
		rules = append(rules, rule)
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})

	version := file.Version
	if version == "" {
		version = "1"
	}

	return RuleSet{
		Source:   p.name,
		Version:  version,
		LoadedAt: time.Now(),
		Rules:    rules,
	}, nil
}

func validateRule(rule yamlRule, idx int) error {
	if rule.ID == "" {
		return fmt.Errorf("rules[%d].id is required", idx)
	}
	if len(rule.Actions) == 0 {
		return fmt.Errorf("rules[%d].actions is required", idx)
	}
	for ci, c := range rule.Conditions {
		if c.Field == "" {
			return fmt.Errorf("rules[%d].conditions[%d].field is required", idx, ci)
		}
		if c.Op != OpEq && c.Op != OpContains && c.Op != OpRegex {
			return fmt.Errorf("rules[%d].conditions[%d].op is invalid", idx, ci)
		}
		if c.Op == OpRegex {
			if _, err := regexp.Compile(normalizeValue(c.Value)); err != nil {
				return fmt.Errorf("rules[%d].conditions[%d].value regex invalid: %w", idx, ci, err)
			}
		}
	}

	for ai, a := range rule.Actions {
		if a.Executor == "" {
			return fmt.Errorf("rules[%d].actions[%d].executor is required", idx, ai)
		}
	}

	return nil
}

func cloneRuleSet(in RuleSet) RuleSet {
	out := in
	out.Rules = append([]Rule(nil), in.Rules...)
	for i := range out.Rules {
		out.Rules[i].Conditions = append([]Condition(nil), in.Rules[i].Conditions...)
		out.Rules[i].Actions = append([]Action(nil), in.Rules[i].Actions...)
	}
	return out
}
