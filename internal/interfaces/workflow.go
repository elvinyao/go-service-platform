package interfaces

import (
	"context"
	"github.com/elvinyao/go-service-platform/internal/model"
)

type Workflow interface {
	GetName() string
	ProcessMessage(ctx context.Context, msg model.Message) error
}
