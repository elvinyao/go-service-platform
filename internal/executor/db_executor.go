package executor

import (
	"context"
	"fmt"
	"time"

	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/ruleengine"
)

type BadgeDBExecutor struct {
	serviceManager *manager.ServiceManager
}

func NewBadgeDBExecutor(serviceManager *manager.ServiceManager) *BadgeDBExecutor {
	return &BadgeDBExecutor{serviceManager: serviceManager}
}

func (e *BadgeDBExecutor) Type() string {
	return "db"
}

func (e *BadgeDBExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	svc, ok := e.serviceManager.GetServiceByName(ctx, "BadgeDBService")
	if !ok {
		return fmt.Errorf("BadgeDBService not found")
	}

	badgeDBService, ok := svc.(*service.BadgeDBService)
	if !ok {
		return fmt.Errorf("BadgeDBService type mismatch: %T", svc)
	}

	name, _ := action.Params["name"].(string)
	if name == "" {
		name = "workflow-badge"
	}
	description, _ := action.Params["description"].(string)
	if description == "" {
		description = "created by workflow executor"
	}
	badgeType, _ := action.Params["badge_type"].(string)
	if badgeType == "" {
		badgeType = msg.Type
	}

	badge := model.Badge{
		ID:          fmt.Sprintf("wf_%s_%d", msg.ID, time.Now().UnixNano()),
		Name:        name,
		Description: description,
		UserID:      msg.UserID,
		AwardedAt:   time.Now(),
		Type:        badgeType,
		Attributes: map[string]interface{}{
			"source_message_id": msg.ID,
			"action_id":         action.ID,
		},
	}

	return badgeDBService.SaveBadge(ctx, badge)
}
