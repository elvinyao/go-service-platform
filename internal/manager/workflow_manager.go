package manager

import (
	"context"
	"project/internal/interfaces"
	"project/internal/model"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
	"sync"
)

// WorkflowFactory 定义了创建工作流的函数类型
type WorkflowFactory func(serviceManager *ServiceManager) (interfaces.Workflow, error)

type WorkflowManager struct {
	workflows map[string]interfaces.Workflow
	mu        sync.Mutex
}

func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		workflows: make(map[string]interfaces.Workflow),
	}
}

// NewWorkflowManagerWithDI 使用依赖注入创建工作流管理器并注册默认工作流
func NewWorkflowManagerWithDI(ctx context.Context, serviceManager *ServiceManager, factories ...WorkflowFactory) (*WorkflowManager, error) {
	ctx = appctx.WithOperationName(ctx, "create_workflow_manager")
	logger.InfoWithContext(ctx, "Creating workflow manager with DI")

	manager := NewWorkflowManager()

	// 注册所有工厂提供的工作流
	for _, factory := range factories {
		workflow, err := factory(serviceManager)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to create workflow", errors.TypeInternal)
		}

		if err := manager.RegisterWorkflow(workflow); err != nil {
			return nil, errors.Wrap(err, "Failed to register workflow", errors.TypeInternal).
				WithField("workflow", workflow.GetName())
		}

		logger.InfofWithContext(ctx, "Registered workflow: %s", workflow.GetName())
	}

	return manager, nil
}

func (wm *WorkflowManager) RegisterWorkflow(w interfaces.Workflow) error {
	if w == nil {
		return errors.New(errors.TypeInvalidInput, "Cannot register nil workflow", nil)
	}

	workflowName := w.GetName()
	if workflowName == "" {
		return errors.New(errors.TypeInvalidInput, "Cannot register workflow with empty name", nil)
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if _, exists := wm.workflows[workflowName]; exists {
		logger.Warnf("Workflow with name %s already registered, overwriting", workflowName)
	}

	wm.workflows[workflowName] = w
	logger.Infof("Registered workflow: %s", workflowName)
	return nil
}

// GetWorkflow 根据名称获取工作流
func (wm *WorkflowManager) GetWorkflow(name string) (interfaces.Workflow, bool) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	workflow, exists := wm.workflows[name]
	return workflow, exists
}

// DispatchMessage 分发消息到适当的工作流
func (wm *WorkflowManager) DispatchMessage(ctx context.Context, msg model.Message) error {
	ctx = appctx.WithOperationName(ctx, "dispatch_message")
	logger.InfofWithContext(ctx, "Dispatching message of type: %s", msg.Type)

	// 简单版本，将消息分发到所有工作流
	// 在真实实现中，可能需要更复杂的路由逻辑

	wm.mu.Lock()
	workflows := make([]interfaces.Workflow, 0, len(wm.workflows))
	for _, wf := range wm.workflows {
		workflows = append(workflows, wf)
	}
	wm.mu.Unlock()

	if len(workflows) == 0 {
		return errors.New(errors.TypeNotFound, "No workflows registered to process messages", nil)
	}

	// 处理错误
	var lastErr error
	for _, wf := range workflows {
		wfCtx := appctx.WithServiceName(ctx, wf.GetName())
		if err := wf.ProcessMessage(wfCtx, msg); err != nil {
			logger.WithContextError(wfCtx, err).Errorf(
				"Workflow %s failed to process message", wf.GetName())
			lastErr = err
		} else {
			// 至少有一个工作流成功处理了消息
			logger.InfofWithContext(wfCtx, "Workflow %s successfully processed message", wf.GetName())
			return nil
		}
	}

	// 如果执行到这里，表示所有工作流都失败了
	return errors.Wrap(lastErr, "All workflows failed to process message", errors.TypeServiceUnavailable)
}

// GetWorkflowByName retrieves a workflow by name with proper error handling
func (wm *WorkflowManager) GetWorkflowByName(ctx context.Context, name string) (interfaces.Workflow, error) {
	ctx = appctx.WithOperationName(ctx, "get_workflow")

	if name == "" {
		logger.ErrorWithContext(ctx, "Workflow name cannot be empty")
		return nil, errors.New(errors.TypeInvalidInput, "Workflow name cannot be empty", nil)
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	logger.DebugfWithContext(ctx, "Looking up workflow: %s", name)

	workflow, exists := wm.workflows[name]
	if !exists {
		logger.WarnfWithContext(ctx, "Workflow not found: %s", name)
		return nil, errors.New(errors.TypeNotFound, "Workflow not found", nil).
			WithField("workflow_name", name)
	}

	return workflow, nil
}
