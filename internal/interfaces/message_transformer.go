package interfaces

import (
	"context"
	"project/internal/model"
)

type MessageTransformer interface {
	Transform(ctx context.Context, source model.Message) (model.Message, error)
}
