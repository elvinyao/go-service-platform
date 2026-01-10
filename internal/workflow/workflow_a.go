package workflow

import (
	"context"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
	// Other necessary imports
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
			data, fetchErr := confluenceService.FetchData(ctx)
			if fetchErr != nil {
				logger.WithContextError(ctx, fetchErr).Errorf("Failed to fetch data from service %s", s.GetName())
				processErr = fetchErr
				continue
			}

			processedData = data
			processErr = nil
			break
		}
	}

	if processErr != nil {
		return errors.Wrap(processErr, "All confluence services failed to process message", errors.TypeServiceUnavailable)
	}

	if processedData == nil {
		return errors.New(errors.TypeNotFound, "No service found to process this message type", nil).
			WithField("message_type", msg.Type).
			WithField("processing_method", processingMethod)
	}

	// Create a badge based on the processed data
	badge := model.Badge{
		ID:          "badge_" + msg.ID,
		Name:        "Example Badge",
		Description: "Created from " + msg.Type,
		UserID:      msg.UserID,
		AwardedAt:   msg.Timestamp,
		Type:        msg.Type,
		Attributes: map[string]interface{}{
			"processed_data": processedData,
			"source_message": msg.ID,
		},
	}

	// Save the badge to BadgeDB
	if err := badgeDBSvc.SaveBadge(ctx, badge); err != nil {
		return errors.Wrap(err, "Failed to save badge", errors.TypeServiceUnavailable)
	}

	logger.InfofWithContext(ctx, "Successfully processed message and created badge: %s", badge.ID)
	return nil
}
