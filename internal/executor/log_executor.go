package executor

import (
	"context"
	"fmt"

	"project/internal/model"
	"project/pkg/logger"
	"project/pkg/ruleengine"
)

type LogExecutor struct{}

func NewLogExecutor() *LogExecutor {
	return &LogExecutor{}
}

func (e *LogExecutor) Type() string {
	return "log"
}

func (e *LogExecutor) Execute(ctx context.Context, msg model.Message, action ruleengine.Action) error {
	level, _ := action.Params["level"].(string)
	templateVal, _ := action.Params["template"].(string)

	message, err := renderTemplate(templateVal, msg)
	if err != nil {
		return fmt.Errorf("render log template: %w", err)
	}
	if message == "" {
		message = fmt.Sprintf("workflow action log message=%s type=%s id=%s", msg.Content, msg.Type, msg.ID)
	}

	entry := logger.WithFields(map[string]interface{}{
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
