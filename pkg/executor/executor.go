package executor

import (
	"context"

	"project/pkg/ruleengine"
)

type Executor interface {
	Type() string
	Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error
}
