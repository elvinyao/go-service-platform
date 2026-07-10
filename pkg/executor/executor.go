package executor

import (
	"context"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type Executor interface {
	Type() string
	Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error
}
