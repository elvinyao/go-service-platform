package workflow

import (
	"project/internal/interfaces"
	"project/internal/manager"
)

// CreateWorkflowA creates a WorkflowA instance
func CreateWorkflowA(serviceManager *manager.ServiceManager) (interfaces.Workflow, error) {
	return NewWorkflowA(serviceManager), nil
}

// CreateWorkflowC creates a WorkflowC instance for event-driven notifications
func CreateWorkflowC(serviceManager *manager.ServiceManager) (interfaces.Workflow, error) {
	return NewWorkflowC(serviceManager), nil
}

// RegisterWorkflowFactories returns all workflow factory functions
func RegisterWorkflowFactories() []manager.WorkflowFactory {
	return []manager.WorkflowFactory{
		CreateWorkflowA,
		CreateWorkflowC,
	}
}
