package manager

import (
	"context"
	stderrors "errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/internal/interfaces"
	"github.com/elvinyao/go-service-platform/internal/model"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/logger"
)

const workflowCleanupTimeout = 10 * time.Second

// WorkflowFactory defines the function type for creating workflows
type WorkflowFactory func(serviceManager *ServiceManager) (interfaces.Workflow, error)

type WorkflowManager struct {
	workflows map[string]interfaces.Workflow
	mu        sync.RWMutex
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
		if factory == nil {
			initializationErr := errors.New(errors.TypeInvalidInput, "Cannot use nil workflow factory", nil)
			return nil, withWorkflowInitializationCleanup(initializationErr, manager, nil)
		}
		workflow, err := factory(serviceManager)
		if err != nil {
			initializationErr := errors.Wrap(err, "Failed to create workflow", errors.TypeInternal)
			return nil, withWorkflowInitializationCleanup(initializationErr, manager, nil)
		}
		if isNilRegistration(workflow) {
			initializationErr := errors.New(errors.TypeInvalidInput, "Workflow factory returned nil", nil)
			return nil, withWorkflowInitializationCleanup(initializationErr, manager, nil)
		}

		if err := manager.RegisterWorkflow(workflow); err != nil {
			initializationErr := errors.Wrap(err, "Failed to register workflow", errors.TypeInternal).
				WithField("workflow", workflow.GetName())
			return nil, withWorkflowInitializationCleanup(initializationErr, manager, workflow)
		}

		logger.InfofWithContext(ctx, "Registered workflow: %s", workflow.GetName())
	}

	return manager, nil
}

func withWorkflowInitializationCleanup(
	initializationErr error,
	manager *WorkflowManager,
	unregistered interfaces.Workflow,
) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), workflowCleanupTimeout)
	defer cancel()

	var cleanupErrors []error
	if !isNilRegistration(unregistered) {
		if stopper, ok := unregistered.(interface {
			Stop(context.Context) error
		}); ok {
			if err := stopper.Stop(cleanupCtx); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("stop unregistered workflow: %w", err))
			}
		}
	}
	if err := manager.StopAll(cleanupCtx); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	if cleanupErr := stderrors.Join(cleanupErrors...); cleanupErr != nil {
		return stderrors.Join(
			initializationErr,
			fmt.Errorf("clean up workflow initialization: %w", cleanupErr),
		)
	}
	return initializationErr
}

func (wm *WorkflowManager) RegisterWorkflow(w interfaces.Workflow) error {
	if isNilRegistration(w) {
		return errors.New(errors.TypeInvalidInput, "Cannot register nil workflow", nil)
	}

	workflowName := w.GetName()
	if workflowName == "" {
		return errors.New(errors.TypeInvalidInput, "Cannot register workflow with empty name", nil)
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if _, exists := wm.workflows[workflowName]; exists {
		return errors.New(errors.TypeInvalidInput, "Workflow is already registered", nil).
			WithField("workflow_name", workflowName)
	}

	wm.workflows[workflowName] = w
	logger.Infof("Registered workflow: %s", workflowName)
	return nil
}

// GetWorkflow retrieves a workflow by name
func (wm *WorkflowManager) GetWorkflow(name string) (interfaces.Workflow, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	workflow, exists := wm.workflows[name]
	return workflow, exists
}

// DispatchMessage dispatches a message to the appropriate workflow
func (wm *WorkflowManager) DispatchMessage(ctx context.Context, msg model.Message) error {
	ctx = appctx.WithOperationName(ctx, "dispatch_message")
	logger.InfofWithContext(ctx, "Dispatching message of type: %s", msg.Type)

	// Dispatch to registered workflows until one handles the message.

	wm.mu.RLock()
	workflows := make([]interfaces.Workflow, 0, len(wm.workflows))
	for _, wf := range wm.workflows {
		workflows = append(workflows, wf)
	}
	wm.mu.RUnlock()
	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].GetName() < workflows[j].GetName()
	})

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

// StopAll stops lifecycle-aware workflows in reverse name order.
func (wm *WorkflowManager) StopAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	wm.mu.RLock()
	names := make([]string, 0, len(wm.workflows))
	workflows := make(map[string]interfaces.Workflow, len(wm.workflows))
	for name, workflow := range wm.workflows {
		names = append(names, name)
		workflows[name] = workflow
	}
	wm.mu.RUnlock()
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	var stopErrors []error
	for _, name := range names {
		stopper, ok := workflows[name].(interface {
			Stop(context.Context) error
		})
		if !ok {
			continue
		}
		if err := stopper.Stop(ctx); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("stop workflow %s: %w", name, err))
		}
	}
	return stderrors.Join(stopErrors...)
}

// GetWorkflowByName retrieves a workflow by name with proper error handling
func (wm *WorkflowManager) GetWorkflowByName(ctx context.Context, name string) (interfaces.Workflow, error) {
	ctx = appctx.WithOperationName(ctx, "get_workflow")

	if name == "" {
		logger.ErrorWithContext(ctx, "Workflow name cannot be empty")
		return nil, errors.New(errors.TypeInvalidInput, "Workflow name cannot be empty", nil)
	}

	wm.mu.RLock()
	defer wm.mu.RUnlock()

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

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	workflowNames := make([]string, 0, len(wm.workflows))
	for name := range wm.workflows {
		workflowNames = append(workflowNames, name)
	}
	sort.Strings(workflowNames)

	logger.InfofWithContext(ctx, "Found %d registered workflows", len(workflowNames))
	return workflowNames
}

// GetAllWorkflows returns all workflow instances
func (wm *WorkflowManager) GetAllWorkflows(ctx context.Context) map[string]interfaces.Workflow {
	ctx = appctx.WithOperationName(ctx, "get_all_workflows")
	logger.DebugWithContext(ctx, "Getting all workflow instances")

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	// Create a copy of the workflows map to avoid concurrent access issues
	result := make(map[string]interfaces.Workflow, len(wm.workflows))
	for name, workflow := range wm.workflows {
		result[name] = workflow
	}

	logger.InfofWithContext(ctx, "Retrieved %d workflow instances", len(result))
	return result
}
