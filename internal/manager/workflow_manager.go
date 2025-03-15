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

type WorkflowManager struct {
	workflows map[string]interfaces.Workflow
	mu        sync.Mutex
}

func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		workflows: make(map[string]interfaces.Workflow),
	}
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

func (wm *WorkflowManager) DispatchMessage(ctx context.Context, msg model.Message) error {
	if msg.Type == "" {
		return errors.New(errors.TypeInvalidInput, "Message type cannot be empty", nil)
	}

	// Create a context specifically for this message processing
	msgCtx := appctx.NewContext(ctx)
	msgCtx = appctx.WithOperationName(msgCtx, "dispatch_message")

	logger.InfofWithContext(msgCtx, "Dispatching message of type: %s", msg.Type)

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if len(wm.workflows) == 0 {
		logger.ErrorWithContext(msgCtx, "No workflows registered to handle messages")
		return errors.New(errors.TypeNotFound, "No workflows registered to handle messages", nil)
	}

	var dispatchErrors []*errors.AppError
	for name, workflow := range wm.workflows {
		// Create a workflow-specific context for this processing attempt
		workflowCtx := appctx.WithServiceName(msgCtx, name)

		// Log message dispatch attempt
		logger.DebugfWithContext(workflowCtx, "Dispatching message to workflow")

		// Attempt to process message with the workflow
		err := logger.LogTimingOperation(workflowCtx, "process_message", func(opCtx context.Context) error {
			return workflow.ProcessMessage(opCtx, msg)
		})

		if err != nil {
			// Create a structured error with context
			appErr := errors.Wrap(err, "Error processing message", errors.TypeInternal).
				WithFields(map[string]interface{}{
					"workflow":     name,
					"message_type": msg.Type,
				})

			// Log the error in a structured way
			logger.WithContextError(workflowCtx, err).Error("Failed to process message")

			// Add to collection of errors
			dispatchErrors = append(dispatchErrors, appErr)
		}
	}

	// If we encountered errors but some workflows succeeded, log a warning
	if len(dispatchErrors) > 0 && len(dispatchErrors) < len(wm.workflows) {
		logger.WarnfWithContext(msgCtx, "Message processed with %d errors out of %d workflows",
			len(dispatchErrors), len(wm.workflows))
		return errors.New(errors.TypePartialFailure, "Message processed with some errors", nil).
			WithField("errors", dispatchErrors)
	}

	// If all workflows failed, return an error
	if len(dispatchErrors) > 0 && len(dispatchErrors) == len(wm.workflows) {
		logger.ErrorWithContext(msgCtx, "All workflows failed to process message")
		return errors.New(errors.TypeInternal, "All workflows failed to process message", nil).
			WithField("errors", dispatchErrors)
	}

	// Success case
	logger.DebugfWithContext(msgCtx, "Message successfully processed by all workflows")
	return nil
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
