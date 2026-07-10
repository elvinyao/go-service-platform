package confluence

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type Provider struct {
	name           string
	workflow       string
	serviceManager *manager.ServiceManager

	mu       sync.RWMutex
	lastGood ruleengine.RuleSet
}

func NewProvider(name, workflow string, serviceManager *manager.ServiceManager) *Provider {
	return &Provider{
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

func (p *Provider) Name() string {
	return p.name
}

func (p *Provider) Start(ctx context.Context) error {
	rs := p.Snapshot(ctx)
	p.mu.Lock()
	p.lastGood = rs
	p.mu.Unlock()
	return nil
}

func (p *Provider) Snapshot(ctx context.Context) ruleengine.RuleSet {
	svc, err := p.getService(ctx)
	if err != nil {
		p.mu.RLock()
		defer p.mu.RUnlock()
		rs := ruleengine.CloneRuleSet(p.lastGood)
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
	if err := ruleengine.ValidateRuleSet(rs); err != nil {
		p.mu.RLock()
		fallback := ruleengine.CloneRuleSet(p.lastGood)
		p.mu.RUnlock()
		fallback.LastError = fmt.Sprintf("validate Confluence rules: %v", err)
		return fallback
	}

	p.mu.Lock()
	p.lastGood = ruleengine.CloneRuleSet(rs)
	p.mu.Unlock()

	return rs
}

func (p *Provider) getService(ctx context.Context) (*service.ConfluenceSettingsService, error) {
	svc, ok := p.serviceManager.GetServiceByName(ctx, service.ConfluenceSettingsServiceName)
	if !ok {
		return nil, fmt.Errorf("service ConfluenceSettingsService not found")
	}

	settingsService, ok := svc.(*service.ConfluenceSettingsService)
	if !ok {
		return nil, fmt.Errorf("service ConfluenceSettingsService type mismatch: %T", svc)
	}

	if !settingsService.IsRunning(ctx) {
		logger.WarnfWithContext(ctx, "ConfluenceSettingsService is not running; using last snapshot")
		return nil, fmt.Errorf("service ConfluenceSettingsService is not running")
	}

	return settingsService, nil
}
