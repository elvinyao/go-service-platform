package workflow

import (
	"context"
	"fmt"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/logger"
)

// WorkflowC coordinates event processing between WebSocket events,
// Confluence settings lookup, and Mattermost notifications.
//
// Flow:
// 1. Receive message from WebSocket
// 2. Check if message type is "AAA"
// 3. Query ConfluenceSettingsService for matching rules
// 4. For each matching rule, send notification via MattermostService
type WorkflowC struct {
	name           string
	serviceManager *manager.ServiceManager
}

// NewWorkflowC creates a new WorkflowC instance
func NewWorkflowC(sm *manager.ServiceManager) *WorkflowC {
	return &WorkflowC{
		name:           "WorkflowC",
		serviceManager: sm,
	}
}

// GetName returns the workflow name
func (w *WorkflowC) GetName() string {
	return w.name
}

// ProcessMessage implements the Workflow interface
// It coordinates the event-driven notification flow
func (w *WorkflowC) ProcessMessage(ctx context.Context, msg model.Message) error {
	// Create workflow-specific context
	ctx = appctx.WithServiceName(ctx, w.name)
	ctx = appctx.WithOperationName(ctx, "process_message")

	logger.InfofWithContext(ctx, "WorkflowC processing message: type=%s, id=%s", msg.Type, msg.ID)

	// Step 1: Filter for "AAA" event type
	if msg.Type != "AAA" {
		logger.DebugfWithContext(ctx, "Ignoring message with type %s (not AAA)", msg.Type)
		return nil
	}

	logger.InfofWithContext(ctx, "Processing AAA event: %s", msg.ID)

	// Step 2: Get ConfluenceSettingsService to lookup matching rules
	settingsService, err := w.getConfluenceSettingsService(ctx)
	if err != nil {
		return errors.Wrap(err, "Failed to get settings service", errors.TypeServiceUnavailable)
	}

	// Step 3: Match event against settings rules
	matchedRules := settingsService.MatchEvent(ctx, msg.Type, msg.Content)

	if len(matchedRules) == 0 {
		logger.InfofWithContext(ctx, "No matching rules found for event type %s", msg.Type)
		return nil
	}

	logger.InfofWithContext(ctx, "Found %d matching rules for event", len(matchedRules))

	// Step 4: Get MattermostService to send notifications
	mattermostService, err := w.getMattermostService(ctx)
	if err != nil {
		return errors.Wrap(err, "Failed to get Mattermost service", errors.TypeServiceUnavailable)
	}

	// Step 5: Send notification for each matching rule
	var lastErr error
	successCount := 0

	for _, rule := range matchedRules {
		// Format the message using the template
		message := formatNotificationMessage(rule.MessageTmpl, msg)

		// Determine which channel to send to
		channelID := rule.ChannelID
		if channelID == "" {
			channelID = mattermostService.GetChannelID()
		}

		logger.DebugfWithContext(ctx, "Sending notification to channel %s", channelID)

		if err := mattermostService.SendMessageToChannel(ctx, channelID, message); err != nil {
			logger.WithContextError(ctx, err).Errorf("Failed to send notification to channel %s", channelID)
			lastErr = err
			continue
		}

		successCount++
		logger.InfofWithContext(ctx, "Successfully sent notification to channel %s", channelID)
	}

	if successCount == 0 && lastErr != nil {
		return errors.Wrap(lastErr, "Failed to send any notifications", errors.TypeServiceUnavailable)
	}

	logger.InfofWithContext(ctx, "WorkflowC completed: sent %d/%d notifications", successCount, len(matchedRules))
	return nil
}

// getConfluenceSettingsService retrieves the ConfluenceSettingsService from the manager
func (w *WorkflowC) getConfluenceSettingsService(ctx context.Context) (*service.ConfluenceSettingsService, error) {
	svc, ok := w.serviceManager.GetServiceByName(ctx, "ConfluenceSettingsService")
	if !ok {
		return nil, errors.New(errors.TypeNotFound, "ConfluenceSettingsService not found", nil)
	}

	settingsService, ok := svc.(*service.ConfluenceSettingsService)
	if !ok {
		return nil, errors.New(errors.TypeInternal, "Service is not ConfluenceSettingsService", nil).
			WithField("actual_type", fmt.Sprintf("%T", svc))
	}

	return settingsService, nil
}

// getMattermostService retrieves the MattermostService from the manager
func (w *WorkflowC) getMattermostService(ctx context.Context) (*service.MattermostService, error) {
	svc, ok := w.serviceManager.GetServiceByName(ctx, "MattermostService")
	if !ok {
		return nil, errors.New(errors.TypeNotFound, "MattermostService not found", nil)
	}

	mattermostService, ok := svc.(*service.MattermostService)
	if !ok {
		return nil, errors.New(errors.TypeInternal, "Service is not MattermostService", nil).
			WithField("actual_type", fmt.Sprintf("%T", svc))
	}

	return mattermostService, nil
}

// formatNotificationMessage formats the notification message using the template
func formatNotificationMessage(template string, msg model.Message) string {
	if template == "" {
		return fmt.Sprintf("Event received: Type=%s, ID=%s, Content=%s", msg.Type, msg.ID, msg.Content)
	}

	// Simple template substitution - in production, use text/template
	return fmt.Sprintf(template, msg.Content)
}
