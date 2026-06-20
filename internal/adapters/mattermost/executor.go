package mattermost

import (
	"context"
	"fmt"

	"project/internal/manager"
	"project/internal/service"
	coreexecutor "project/pkg/executor"
	"project/pkg/ruleengine"
)

type Executor struct {
	serviceManager *manager.ServiceManager
}

func NewExecutor(serviceManager *manager.ServiceManager) *Executor {
	return &Executor{serviceManager: serviceManager}
}

func (e *Executor) Type() string {
	return "mattermost"
}

func (e *Executor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
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
	message, err := coreexecutor.RenderTemplate(templateVal, msg)
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
