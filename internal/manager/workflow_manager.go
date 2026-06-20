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

// WorkflowFactory defines the function type for creating workflows
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

// NewWorkflowManagerWithDI creates a workflow manager using dependency injection and registers default workflows
func NewWorkflowManagerWithDI(ctx context.Context, serviceManager *ServiceManager, factories ...WorkflowFactory) (*WorkflowManager, error) {
	ctx = appctx.WithOperationName(ctx, "create_workflow_manager")
	logger.InfoWithContext(ctx, "Creating workflow manager with DI")

	manager := NewWorkflowManager()

	// Register all workflows provided by factories
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

// GetWorkflow retrieves a workflow by name
func (wm *WorkflowManager) GetWorkflow(name string) (interfaces.Workflow, bool) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	workflow, exists := wm.workflows[name]
	return workflow, exists
}

// DispatchMessage dispatches a message to the appropriate workflow
func (wm *WorkflowManager) DispatchMessage(ctx context.Context, msg model.Message) error {
	ctx = appctx.WithOperationName(ctx, "dispatch_message")
	logger.InfofWithContext(ctx, "Dispatching message of type: %s", msg.Type)

	// Dispatch to registered workflows until one handles the message.

	wm.mu.Lock()
	workflows := make([]interfaces.Workflow, 0, len(wm.workflows))
	for _, wf := range wm.workflows {
		workflows = append(workflows, wf)
	}
	wm.mu.Unlock()

	if len(workflows) == 0 {
		return errors.New(errors.TypeNotFound, "No workflows registered to process messages", nil)
	}

	// Handle errors
	var lastErr error
	for _, wf := range workflows {
		wfCtx := appctx.WithServiceName(ctx, wf.GetName())
		if err := wf.ProcessMessage(wfCtx, msg); err != nil {
			logger.WithContextError(wfCtx, err).Errorf(
				"Workflow %s failed to process message", wf.GetName())
			lastErr = err
		} else {
			// At least one workflow successfully processed the message
			logger.InfofWithContext(wfCtx, "Workflow %s successfully processed message", wf.GetName())
			return nil
		}
	}

	// If we reach here, all workflows failed
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

// ListWorkflows returns the list of all registered workflows
func (wm *WorkflowManager) ListWorkflows(ctx context.Context) []string {
	ctx = appctx.WithOperationName(ctx, "list_workflows")
	logger.DebugWithContext(ctx, "Listing all registered workflows")

	wm.mu.Lock()
	defer wm.mu.Unlock()

	workflowNames := make([]string, 0, len(wm.workflows))
	for name := range wm.workflows {
		workflowNames = append(workflowNames, name)
	}

	logger.InfofWithContext(ctx, "Found %d registered workflows", len(workflowNames))
	return workflowNames
}

// GetAllWorkflows returns all workflow instances
func (wm *WorkflowManager) GetAllWorkflows(ctx context.Context) map[string]interfaces.Workflow {
	ctx = appctx.WithOperationName(ctx, "get_all_workflows")
	logger.DebugWithContext(ctx, "Getting all workflow instances")

	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Create a copy of the workflows map to avoid concurrent access issues
	result := make(map[string]interfaces.Workflow, len(wm.workflows))
	for name, workflow := range wm.workflows {
		result[name] = workflow
	}

	logger.InfofWithContext(ctx, "Retrieved %d workflow instances", len(result))
	return result
}
