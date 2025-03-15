package interfaces

import "project/internal/model"

type Workflow interface {
	GetName() string
	ProcessMessage(msg model.Message) error
}
