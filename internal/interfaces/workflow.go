package interfaces

import (
	"context"
	"project/internal/model"
)

type Workflow interface {
	GetName() string
	ProcessMessage(ctx context.Context, msg model.Message) error
}
