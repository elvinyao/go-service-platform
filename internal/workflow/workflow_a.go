package workflow

import (
	"context"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
	// 其他必要的导入
)

type WorkflowA struct {
	name           string
	serviceManager *manager.ServiceManager
}

func NewWorkflowA(sm *manager.ServiceManager) *WorkflowA {
	return &WorkflowA{
		name:           "WorkflowA",
		serviceManager: sm,
	}
}

func (w *WorkflowA) GetName() string {
	return w.name
}

func (w *WorkflowA) ProcessMessage(ctx context.Context, msg model.Message) error {
	// Create a workflow-specific context
	ctx = appctx.WithServiceName(ctx, w.name)
	ctx = appctx.WithOperationName(ctx, "process_message")

	logger.InfofWithContext(ctx, "Processing message of type: %s", msg.Type)

	// Get required services with context
	confluenceServices := w.serviceManager.GetServicesByWorkflowAndType(ctx, "A", "confluence")
	if len(confluenceServices) == 0 {
		return errors.New(errors.TypeNotFound, "No confluence services found for workflow A", nil)
	}

	badgeDBService, ok := w.serviceManager.GetServiceByName(ctx, "BadgeDBService")
	if !ok {
		return errors.New(errors.TypeNotFound, "BadgeDB service not found", nil)
	}

	// Query BadgeDB for processing method
	badgeDBSvc, ok := badgeDBService.(*service.BadgeDBService)
	if !ok {
		return errors.New(errors.TypeInternal, "Invalid service type", nil).
			WithField("expected", "BadgeDBService").
			WithField("actual", badgeDBService.GetType())
	}

	processingMethod, err := badgeDBSvc.GetProcessingMethod(msg.Type)
	if err != nil {
		return errors.Wrap(err, "Failed to get processing method", errors.TypeServiceUnavailable)
	}

	logger.DebugfWithContext(ctx, "Using processing method: %s", processingMethod)

	// Use the processing method to call the appropriate ConfluenceService
	var processedData interface{}
	var processErr error

	for _, s := range confluenceServices {
		if s.GetName() == processingMethod {
			confluenceService, ok := s.(*service.ConfluenceService)
			if !ok {
				logger.WarnfWithContext(ctx, "Service %s is not a ConfluenceService", s.GetName())
				continue
			}

			logger.DebugfWithContext(ctx, "Fetching data from confluence service: %s", s.GetName())
			processedData, processErr = confluenceService.FetchData()
			if processErr != nil {
				return errors.Wrap(processErr, "Failed to fetch data", errors.TypeServiceUnavailable).
					WithField("service", s.GetName())
			}

			// Log the result
			logger.InfofWithContext(ctx, "Data fetched successfully from %s", s.GetName())
			break
		}
	}

	if processedData == nil && processErr == nil {
		return errors.New(errors.TypeNotFound,
			"No matching confluence service found for processing method", nil).
			WithField("processing_method", processingMethod)
	}

	return nil
}
