package executor

import (
	"context"
	"fmt"

	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type LogExecutor struct{}

func NewLogExecutor() *LogExecutor {
	return &LogExecutor{}
}

func (e *LogExecutor) Type() string {
	return "log"
}

func (e *LogExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	level, _ := action.Params["level"].(string)
	templateVal, _ := action.Params["template"].(string)

	message, err := RenderTemplate(templateVal, msg)
	if err != nil {
		return fmt.Errorf("render log template: %w", err)
	}
	if message == "" {
		message = fmt.Sprintf("workflow action log message=%s type=%s id=%s", msg.Content, msg.Type, msg.ID)
	}

	entry := logger.WithContextFields(ctx, logger.Fields{
		"executor": "log",
		"action":   action.ID,
		"type":     msg.Type,
		"id":       msg.ID,
	})

	switch level {
	case "debug":
		entry.Debug(message)
	case "warn":
		entry.Warn(message)
	case "error":
		entry.Error(message)
	default:
		entry.Info(message)
	}
	return nil
}
