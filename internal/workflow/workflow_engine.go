package workflow

import (
	"context"
	"fmt"

	"github.com/elvinyao/go-service-platform/internal/adapters/badgedb"
	"github.com/elvinyao/go-service-platform/internal/adapters/confluence"
	"github.com/elvinyao/go-service-platform/internal/adapters/mattermost"
	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/model"
	runtimeconfig "github.com/elvinyao/go-service-platform/pkg/config"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	coreexecutor "github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type WorkflowEngine struct {
	name   string
	engine *pipeline.Engine
}

type EngineAdminSnapshot struct {
	Name      string                      `json:"name"`
	Composer  ruleengine.ComposerSnapshot `json:"composer"`
	Executors []string                    `json:"executors"`
}

func NewWorkflowEngine(ctx context.Context, sm *manager.ServiceManager, configPath, rulesPath string, configs ...runtimeconfig.RuntimeConfig) (*WorkflowEngine, error) {
	cfg, err := ruleengine.LoadEngineConfig(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load rule engine config", errors.TypeInvalidInput)
	}

	runtimeCfg := runtimeconfig.DefaultRuntimeConfig()
	if len(configs) > 0 {
		runtimeCfg = configs[0]
	}
	if err := runtimeCfg.Validate(); err != nil {
		return nil, errors.Wrap(err, "invalid runtime config", errors.TypeInvalidInput)
	}
	cfg, err = filterEngineConfigForRuntime(cfg, runtimeCfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to apply runtime config to rule engine", errors.TypeInvalidInput)
	}
	if err := validateRuntimeAdapterDependencies(cfg, runtimeCfg); err != nil {
		return nil, errors.Wrap(err, "invalid runtime adapter wiring", errors.TypeInvalidInput)
	}

	providers := map[string]ruleengine.RuleProvider{
		"yaml": ruleengine.NewYAMLProvider("yaml", rulesPath, ruleengine.DefaultWorkflowName),
	}
	if runtimeCfg.Adapters.Confluence.Enabled && engineUsesProvider(cfg, "confluence") {
		providers["confluence"] = confluence.NewProvider("confluence", ruleengine.DefaultWorkflowName, sm)
	}
	allExecutors := []coreexecutor.Executor{
		coreexecutor.NewLogExecutor(),
		coreexecutor.NewHTTPExecutor(),
	}
	if runtimeCfg.Adapters.BadgeDB.Enabled {
		allExecutors = append(allExecutors, badgedb.NewExecutor(sm))
	}
	if runtimeCfg.Adapters.Mattermost.Enabled {
		allExecutors = append(allExecutors, mattermost.NewExecutor(sm))
	}
	providerList := make([]ruleengine.RuleProvider, 0, len(providers))
	for _, provider := range providers {
		providerList = append(providerList, provider)
	}
	engine, err := pipeline.New(cfg, providerList, allExecutors)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create rule pipeline", errors.TypeInvalidInput)
	}
	if err := engine.Start(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to start rule pipeline", errors.TypeInvalidInput)
	}

	return &WorkflowEngine{
		name:   ruleengine.DefaultWorkflowName,
		engine: engine,
	}, nil
}

func validateRuntimeAdapterDependencies(cfg ruleengine.EngineConfig, runtimeCfg runtimeconfig.RuntimeConfig) error {
	if engineUsesProvider(cfg, "confluence") && runtimeCfg.Adapters.Confluence.Enabled && !runtimeCfg.Adapters.Mattermost.Enabled {
		return fmt.Errorf("confluence provider requires the mattermost executor to be enabled")
	}
	return nil
}

func filterEngineConfigForRuntime(cfg ruleengine.EngineConfig, runtimeCfg runtimeconfig.RuntimeConfig) (ruleengine.EngineConfig, error) {
	enabledProviders := map[string]bool{
		"yaml":       true,
		"confluence": runtimeCfg.Adapters.Confluence.Enabled,
	}

	for i := range cfg.Workflows {
		wf := &cfg.Workflows[i]
		wf.Providers = filterProviderList(wf.Providers, enabledProviders)
		wf.PipelineOrder = filterProviderList(wf.PipelineOrder, enabledProviders)
		if len(wf.Providers) == 0 {
			return ruleengine.EngineConfig{}, fmt.Errorf("workflow %q has no enabled providers after applying runtime config", wf.Name)
		}
		if len(wf.PipelineOrder) == 0 {
			wf.PipelineOrder = append([]string(nil), wf.Providers...)
		}
	}

	if err := cfg.Validate(); err != nil {
		return ruleengine.EngineConfig{}, fmt.Errorf("validate filtered rule engine config: %w", err)
	}
	return cfg, nil
}

func filterProviderList(providers []string, enabled map[string]bool) []string {
	filtered := make([]string, 0, len(providers))
	for _, provider := range providers {
		providerEnabled, knownProvider := enabled[provider]
		if !knownProvider || providerEnabled {
			filtered = append(filtered, provider)
		}
	}
	return filtered
}

func engineUsesProvider(cfg ruleengine.EngineConfig, name string) bool {
	for _, workflow := range cfg.Workflows {
		for _, provider := range workflow.Providers {
			if provider == name {
				return true
			}
		}
	}
	return false
}

func (w *WorkflowEngine) GetName() string {
	return w.name
}

func (w *WorkflowEngine) AdminSnapshot(ctx context.Context) EngineAdminSnapshot {
	snapshot := w.engine.Snapshot(ctx)
	return EngineAdminSnapshot{
		Name:      w.name,
		Composer:  snapshot.Composer,
		Executors: snapshot.Executors,
	}
}

// Stop releases lifecycle-aware rule providers and executors.
func (w *WorkflowEngine) Stop(ctx context.Context) error {
	return w.engine.Stop(ctx)
}

func (w *WorkflowEngine) ProcessMessage(ctx context.Context, msg model.Message) error {
	ctx = appctx.WithServiceName(ctx, w.name)
	ctx = appctx.WithOperationName(ctx, "process_message")

	plan, processErr := w.engine.Process(ctx, w.name, toRuleEngineMessage(msg))
	var executionError *pipeline.ExecutionError
	if processErr != nil && !errors.As(processErr, &executionError) {
		return errors.Wrap(processErr, "failed to process rule pipeline", errors.TypeInternal)
	}

	if len(plan.Actions) == 0 {
		logger.DebugfWithContext(ctx, "No matching rules for message type=%s id=%s", msg.Type, msg.ID)
		return nil
	}

	logger.InfofWithContext(ctx, "WorkflowEngine matched %d rules and %d actions", len(plan.Rules), len(plan.Actions))

	if processErr != nil {
		execErrs := make([]string, 0, len(executionError.Failures))
		for idx, failure := range executionError.Failures {
			execErrs = append(execErrs, fmt.Sprintf("action[%d] id=%s executor=%s failed: %v", idx, failure.ActionID, failure.Executor, failure.Err))
		}
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
