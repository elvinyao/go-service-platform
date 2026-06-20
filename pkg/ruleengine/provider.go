package ruleengine

import "context"

type RuleProvider interface {
	Name() string
	Start(ctx context.Context) error
	Snapshot(ctx context.Context) RuleSet
}
