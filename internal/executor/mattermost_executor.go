package executor

import (
	"context"
	"fmt"

	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/ruleengine"
)

type MattermostExecutor struct {
	serviceManager *manager.ServiceManager
}

func NewMattermostExecutor(serviceManager *manager.ServiceManager) *MattermostExecutor {
	return &MattermostExecutor{serviceManager: serviceManager}
}

func (e *MattermostExecutor) Type() string {
	return "mattermost"
}

func (e *MattermostExecutor) Execute(ctx context.Context, msg model.Message, action ruleengine.Action) error {
	svc, ok := e.serviceManager.GetServiceByName(ctx, "MattermostService")
	if !ok {
		return fmt.Errorf("MattermostService not found")
	}

	mattermostService, ok := svc.(*service.MattermostService)
	if !ok {
		return fmt.Errorf("MattermostService type mismatch: %T", svc)
	}

	channelID, _ := action.Params["channel_id"].(string)
	templateVal, _ := action.Params["template"].(string)
	message, err := renderTemplate(templateVal, msg)
	if err != nil {
		return fmt.Errorf("render mattermost template: %w", err)
	}
	if message == "" {
		message = fmt.Sprintf("Event received: type=%s id=%s content=%s", msg.Type, msg.ID, msg.Content)
	}

	if channelID == "" {
		return mattermostService.SendMessage(ctx, message)
	}

	return mattermostService.SendMessageToChannel(ctx, channelID, message)
}
