package executor

import (
	"context"

	"project/internal/model"
	"project/pkg/ruleengine"
)

type Executor interface {
	Type() string
	Execute(ctx context.Context, msg model.Message, action ruleengine.Action) error
}
