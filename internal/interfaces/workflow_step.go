package interfaces

import "context"

type WorkflowStep interface {
	Execute(ctx context.Context, data interface{}) (interface{}, error)
	GetName() string
}
