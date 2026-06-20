package workflow

import (
	"context"
	"fmt"

	"project/internal/adapters/badgedb"
	"project/internal/adapters/confluence"
	"project/internal/adapters/mattermost"
	"project/internal/manager"
	"project/internal/model"
	appctx "project/pkg/context"
	"project/pkg/errors"
	coreexecutor "project/pkg/executor"
	"project/pkg/logger"
	"project/pkg/ruleengine"
)

type WorkflowEngine struct {
	name      string
	composer  *ruleengine.Composer
	executors *coreexecutor.Registry
}

type EngineAdminSnapshot struct {
	Name      string                      `json:"name"`
	Composer  ruleengine.ComposerSnapshot `json:"composer"`
	Executors []string                    `json:"executors"`
}

func NewWorkflowEngine(ctx context.Context, sm *manager.ServiceManager, configPath, rulesPath string) (*WorkflowEngine, error) {
	cfg, err := ruleengine.LoadEngineConfig(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load rule engine config", errors.TypeInvalidInput)
	}

	providers := map[string]ruleengine.RuleProvider{
		"yaml":       ruleengine.NewYAMLProvider("yaml", rulesPath, ruleengine.DefaultWorkflowName),
		"confluence": confluence.NewProvider("confluence", ruleengine.DefaultWorkflowName, sm),
	}

	for _, provider := range providers {
		if err := provider.Start(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to start rule provider", errors.TypeServiceUnavailable).
				WithField("provider", provider.Name())
		}
	}

	registry := coreexecutor.NewRegistry()
	allExecutors := []coreexecutor.Executor{
		coreexecutor.NewLogExecutor(),
		badgedb.NewExecutor(sm),
		coreexecutor.NewHTTPExecutor(),
		mattermost.NewExecutor(sm),
	}
	for _, exe := range allExecutors {
		if err := registry.Register(exe); err != nil {
			return nil, errors.Wrap(err, "failed to register executor", errors.TypeInternal)
		}
	}

	return &WorkflowEngine{
		name:      ruleengine.DefaultWorkflowName,
		composer:  ruleengine.NewComposer(cfg, providers),
		executors: registry,
	}, nil
}

func (w *WorkflowEngine) GetName() string {
	return w.name
}

func (w *WorkflowEngine) AdminSnapshot(ctx context.Context) EngineAdminSnapshot {
	return EngineAdminSnapshot{
		Name:      w.name,
		Composer:  w.composer.Snapshot(ctx),
		Executors: w.executors.ListTypes(),
	}
}

func (w *WorkflowEngine) ProcessMessage(ctx context.Context, msg model.Message) error {
	ctx = appctx.WithServiceName(ctx, w.name)
	ctx = appctx.WithOperationName(ctx, "process_message")

	plan, err := w.composer.BuildExecutionPlan(ctx, w.name, toRuleEngineMessage(msg))
	if err != nil {
		return errors.Wrap(err, "failed to build execution plan", errors.TypeInternal)
	}

	if len(plan.Actions) == 0 {
		logger.DebugfWithContext(ctx, "No matching rules for message type=%s id=%s", msg.Type, msg.ID)
		return nil
	}

	logger.InfofWithContext(ctx, "WorkflowEngine matched %d rules and %d actions", len(plan.Rules), len(plan.Actions))

	var execErrs []string
	for idx, action := range plan.Actions {
		exe, ok := w.executors.Get(action.Executor)
		if !ok {
			execErrs = append(execErrs, fmt.Sprintf("action[%d] executor=%s not registered", idx, action.Executor))
			continue
		}

		if err := exe.Execute(ctx, toRuleEngineMessage(msg), action); err != nil {
			execErrs = append(execErrs, fmt.Sprintf("action[%d] executor=%s failed: %v", idx, action.Executor, err))
		}
	}

	if len(execErrs) > 0 {
		return errors.New(errors.TypePartialFailure, "workflow execution had partial failures", nil).
			WithField("errors", execErrs).
			WithField("message_id", msg.ID).
			WithField("message_type", msg.Type)
	}

	return nil
}

func toRuleEngineMessage(msg model.Message) ruleengine.Message {
	return ruleengine.Message{
		ID:        msg.ID,
		Type:      msg.Type,
		Content:   msg.Content,
		UserID:    msg.UserID,
		Timestamp: msg.Timestamp,
		Metadata:  msg.Metadata,
	}
}
