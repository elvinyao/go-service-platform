package ruleengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"project/internal/manager"
	"project/internal/service"
	"project/pkg/logger"
	"project/pkg/ruleengine"
)

type ConfluenceProvider struct {
	name           string
	workflow       string
	serviceManager *manager.ServiceManager

	mu       sync.RWMutex
	lastGood ruleengine.RuleSet
}

func NewConfluenceProvider(name, workflow string, serviceManager *manager.ServiceManager) *ConfluenceProvider {
	return &ConfluenceProvider{
		name:           name,
		workflow:       workflow,
		serviceManager: serviceManager,
		lastGood: ruleengine.RuleSet{
			Source:  name,
			Version: "1",
			Rules:   []ruleengine.Rule{},
		},
	}
}

func (p *ConfluenceProvider) Name() string {
	return p.name
}

func (p *ConfluenceProvider) Start(ctx context.Context) error {
	rs := p.Snapshot(ctx)
	p.mu.Lock()
	p.lastGood = rs
	p.mu.Unlock()
	return nil
}

func (p *ConfluenceProvider) Snapshot(ctx context.Context) ruleengine.RuleSet {
	svc, err := p.getService(ctx)
	if err != nil {
		p.mu.RLock()
		defer p.mu.RUnlock()
		rs := cloneRuleSet(p.lastGood)
		rs.LastError = err.Error()
		return rs
	}

	settings := svc.GetSettings(ctx)
	rules := make([]ruleengine.Rule, 0, len(settings))
	for idx, rule := range settings {
		conditions := []ruleengine.Condition{
			{
				Field: "type",
				Op:    ruleengine.OpEq,
				Value: rule.EventType,
			},
		}

		if rule.Pattern != "" {
			conditions = append(conditions, ruleengine.Condition{
				Field: "content",
				Op:    ruleengine.OpRegex,
				Value: rule.Pattern,
			})
		}

		actions := []ruleengine.Action{
			{
				ID:       fmt.Sprintf("cf_action_%d", idx),
				Executor: "mattermost",
				Priority: 100,
				Params: map[string]interface{}{
					"channel_id": rule.ChannelID,
					"template":   rule.MessageTmpl,
				},
			},
		}

		rules = append(rules, ruleengine.Rule{
			ID:         fmt.Sprintf("cf_rule_%d_%s", idx, rule.EventType),
			Workflow:   p.workflow,
			Source:     p.name,
			Enabled:    rule.Enabled,
			Priority:   idx + 1000,
			Conditions: conditions,
			Actions:    actions,
		})
	}

	rs := ruleengine.RuleSet{
		Source:   p.name,
		Version:  "1",
		LoadedAt: time.Now(),
		Rules:    rules,
	}

	p.mu.Lock()
	p.lastGood = cloneRuleSet(rs)
	p.mu.Unlock()

	return rs
}

func cloneRuleSet(in ruleengine.RuleSet) ruleengine.RuleSet {
	out := in
	out.Rules = append([]ruleengine.Rule(nil), in.Rules...)
	for i := range out.Rules {
		out.Rules[i].Conditions = append([]ruleengine.Condition(nil), in.Rules[i].Conditions...)
		out.Rules[i].Actions = append([]ruleengine.Action(nil), in.Rules[i].Actions...)
	}
	return out
}

func (p *ConfluenceProvider) getService(ctx context.Context) (*service.ConfluenceSettingsService, error) {
	svc, ok := p.serviceManager.GetServiceByName(ctx, "ConfluenceSettingsService")
	if !ok {
		return nil, fmt.Errorf("service ConfluenceSettingsService not found")
	}

	settingsService, ok := svc.(*service.ConfluenceSettingsService)
	if !ok {
		return nil, fmt.Errorf("service ConfluenceSettingsService type mismatch: %T", svc)
	}

	if !settingsService.IsRunning(ctx) {
		logger.WarnfWithContext(ctx, "ConfluenceSettingsService is not running; using last snapshot")
	}

	return settingsService, nil
}
