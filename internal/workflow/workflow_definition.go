package workflow

import "project/internal/interfaces"

type WorkflowDefinition struct {
	Name       string
	Steps      []interfaces.WorkflowStep
	Conditions map[string]func(interface{}) bool
}
