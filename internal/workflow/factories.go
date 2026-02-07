package workflow

import (
	"context"
	"project/internal/interfaces"
	"project/internal/manager"
)

// CreateWorkflowEngine creates the unified rule-driven workflow engine.
func CreateWorkflowEngine(serviceManager *manager.ServiceManager) (interfaces.Workflow, error) {
	ctx := context.Background()
	return NewWorkflowEngine(ctx, serviceManager, "config/rule-engine.yaml", "config/workflows.yaml")
}

// RegisterWorkflowFactories returns all workflow factory functions
func RegisterWorkflowFactories() []manager.WorkflowFactory {
	return []manager.WorkflowFactory{
		CreateWorkflowEngine,
	}
}
