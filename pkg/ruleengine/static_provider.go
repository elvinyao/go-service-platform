package ruleengine

import "context"

type StaticProvider struct {
	name string
	set  RuleSet
}

func NewStaticProvider(name string, set RuleSet) *StaticProvider {
	set.Source = name
	return &StaticProvider{name: name, set: cloneRuleSet(set)}
}

func (p *StaticProvider) Name() string {
	return p.name
}

func (p *StaticProvider) Start(ctx context.Context) error {
	return nil
}

func (p *StaticProvider) Snapshot(ctx context.Context) RuleSet {
	return cloneRuleSet(p.set)
}
