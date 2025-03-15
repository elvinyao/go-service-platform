package interfaces

import (
	"context"
	"project/internal/model"
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, msg model.Message) (model.Message, error)
	GetPriority() int
	CanHandle(msg model.Message) bool
}
