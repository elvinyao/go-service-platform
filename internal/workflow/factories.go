package workflow

import (
	"project/internal/interfaces"
	"project/internal/manager"
)

// CreateWorkflowA 创建WorkflowA实例
func CreateWorkflowA(serviceManager *manager.ServiceManager) (interfaces.Workflow, error) {
	return NewWorkflowA(serviceManager), nil
}

// RegisterWorkflowFactories 返回所有工作流工厂函数
func RegisterWorkflowFactories() []manager.WorkflowFactory {
	return []manager.WorkflowFactory{
		CreateWorkflowA,
	}
}
