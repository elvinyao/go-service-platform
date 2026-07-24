package pipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

const lifecycleCleanupTimeout = 10 * time.Second

// Starter is an optional lifecycle capability for executors.
type Starter interface {
	Start(context.Context) error
}

// Stopper is an optional lifecycle capability for providers and executors.
type Stopper interface {
	Stop(context.Context) error
}

// Engine composes rules from providers and executes the resulting actions.
type Engine struct {
	config            ruleengine.EngineConfig
	composer          *ruleengine.Composer
	providers         map[string]ruleengine.RuleProvider
	executors         *executor.Registry
	executorInstances map[string]executor.Executor

	mu              sync.RWMutex
	started         bool
	activeProviders []string
	activeExecutors []string
}

// Snapshot is an immutable diagnostic view of an engine's current wiring.
type Snapshot struct {
	Composer  ruleengine.ComposerSnapshot `json:"composer"`
	Executors []string                    `json:"executors"`
}

// ActionFailure describes one action that could not be executed.
type ActionFailure struct {
	ActionID string
	Executor string
	Err      error
}

// ExecutionError aggregates action failures from one execution plan.
type ExecutionError struct {
	Failures []ActionFailure
}

// Error returns a diagnostic summary of all failed actions.
func (e *ExecutionError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		parts = append(parts, fmt.Sprintf("action=%s executor=%s: %v", failure.ActionID, failure.Executor, failure.Err))
	}
	return "pipeline execution failed: " + strings.Join(parts, "; ")
}

// Unwrap exposes the original action errors for errors.Is and errors.As.
func (e *ExecutionError) Unwrap() []error {
	errs := make([]error, 0, len(e.Failures))
	for _, failure := range e.Failures {
		errs = append(errs, failure.Err)
	}
	return errs
}

// New validates and constructs an engine from application-owned providers and executors.
func New(config ruleengine.EngineConfig, providers []ruleengine.RuleProvider, executors []executor.Executor) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate engine config: %w", err)
	}

	providerMap := make(map[string]ruleengine.RuleProvider, len(providers))
	for _, provider := range providers {
		if isNilExtension(provider) {
			return nil, fmt.Errorf("rule provider is nil")
		}
		name := provider.Name()
		if name == "" {
			return nil, fmt.Errorf("rule provider name is empty")
		}
		if _, exists := providerMap[name]; exists {
			return nil, fmt.Errorf("rule provider %s is registered more than once", name)
		}
		providerMap[name] = provider
	}
	if err := validateConfiguredProviders(config, providerMap); err != nil {
		return nil, err
	}

	registry := executor.NewRegistry()
	executorInstances := make(map[string]executor.Executor, len(executors))
	for _, actionExecutor := range executors {
		if err := registry.Register(actionExecutor); err != nil {
			return nil, err
		}
		executorInstances[actionExecutor.Type()] = actionExecutor
	}

	engineConfig := ruleengine.CloneEngineConfig(config)
	return &Engine{
		config:            engineConfig,
		composer:          ruleengine.NewComposer(engineConfig, providerMap),
		providers:         providerMap,
		executors:         registry,
		executorInstances: executorInstances,
	}, nil
}

func isNilExtension(value interface{}) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Start initializes every provider and verifies that enabled rules use registered executors.
func (e *Engine) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return nil
	}
	if len(e.activeProviders) > 0 || len(e.activeExecutors) > 0 {
		return fmt.Errorf("pipeline has components pending cleanup")
	}

	names := make([]string, 0, len(e.providers))
	for name := range e.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e.activeProviders = append(e.activeProviders, name)
		if err := e.providers[name].Start(ctx); err != nil {
			return e.rollbackStartup(fmt.Errorf("start rule provider %s: %w", name, err))
		}
	}
	if err := validateProviderExecutors(ctx, e.config, e.providers, e.executors); err != nil {
		return e.rollbackStartup(err)
	}

	executorTypes := e.executors.ListTypes()
	for _, executorType := range executorTypes {
		actionExecutor := e.executorInstances[executorType]
		starter, starts := actionExecutor.(Starter)
		_, stops := actionExecutor.(Stopper)
		if !starts && !stops {
			continue
		}
		e.activeExecutors = append(e.activeExecutors, executorType)
		if starts {
			if err := starter.Start(ctx); err != nil {
				return e.rollbackStartup(fmt.Errorf("start executor %s: %w", executorType, err))
			}
		}
	}

	e.started = true
	return nil
}

// Stop stops lifecycle-aware executors and providers in reverse startup order.
func (e *Engine) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started && len(e.activeProviders) == 0 && len(e.activeExecutors) == 0 {
		return nil
	}

	e.started = false
	return e.stopComponents(ctx)
}

func (e *Engine) rollbackStartup(startErr error) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), lifecycleCleanupTimeout)
	defer cancel()
	if cleanupErr := e.stopComponents(cleanupCtx); cleanupErr != nil {
		return errors.Join(startErr, fmt.Errorf("roll back pipeline startup: %w", cleanupErr))
	}
	return startErr
}

func (e *Engine) stopComponents(ctx context.Context) error {
	var stopErrors []error

	failedExecutors := make(map[string]struct{})
	for index := len(e.activeExecutors) - 1; index >= 0; index-- {
		executorType := e.activeExecutors[index]
		stopper, ok := e.executorInstances[executorType].(Stopper)
		if !ok {
			continue
		}
		if err := stopper.Stop(ctx); err != nil {
			failedExecutors[executorType] = struct{}{}
			stopErrors = append(stopErrors, fmt.Errorf("stop executor %s: %w", executorType, err))
		}
	}
	e.activeExecutors = retainNames(e.activeExecutors, failedExecutors)

	failedProviders := make(map[string]struct{})
	for index := len(e.activeProviders) - 1; index >= 0; index-- {
		providerName := e.activeProviders[index]
		stopper, ok := e.providers[providerName].(Stopper)
		if !ok {
			continue
		}
		if err := stopper.Stop(ctx); err != nil {
			failedProviders[providerName] = struct{}{}
			stopErrors = append(stopErrors, fmt.Errorf("stop rule provider %s: %w", providerName, err))
		}
	}
	e.activeProviders = retainNames(e.activeProviders, failedProviders)

	return errors.Join(stopErrors...)
}

func retainNames(names []string, retained map[string]struct{}) []string {
	result := make([]string, 0, len(retained))
	for _, name := range names {
		if _, exists := retained[name]; exists {
			result = append(result, name)
		}
	}
	return result
}

// Process builds and executes a plan for one message.
//
// It attempts every action and returns both the plan and an ExecutionError when
// one or more independent actions fail.
func (e *Engine) Process(ctx context.Context, workflow string, message ruleengine.Message) (ruleengine.ExecutionPlan, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.started {
		return ruleengine.ExecutionPlan{}, fmt.Errorf("pipeline is not started")
	}

	plan, err := e.composer.BuildExecutionPlan(ctx, workflow, message)
	if err != nil {
		return ruleengine.ExecutionPlan{}, err
	}

	failures := make([]ActionFailure, 0)
	for _, action := range plan.Actions {
		actionExecutor, exists := e.executors.Get(action.Executor)
		if !exists {
			failures = append(failures, ActionFailure{
				ActionID: action.ID,
				Executor: action.Executor,
				Err:      fmt.Errorf("executor is not registered"),
			})
			continue
		}
		if err := actionExecutor.Execute(ctx, message, action); err != nil {
			failures = append(failures, ActionFailure{
				ActionID: action.ID,
				Executor: action.Executor,
				Err:      err,
			})
		}
	}
	if len(failures) > 0 {
		return plan, &ExecutionError{Failures: failures}
	}
	return plan, nil
}

// Snapshot returns the current composition, provider snapshots, and executor types.
func (e *Engine) Snapshot(ctx context.Context) Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Snapshot{
		Composer:  e.composer.Snapshot(ctx),
		Executors: e.executors.ListTypes(),
	}
}

func validateConfiguredProviders(config ruleengine.EngineConfig, providers map[string]ruleengine.RuleProvider) error {
	for _, workflow := range config.Workflows {
		for _, provider := range workflow.Providers {
			if _, exists := providers[provider]; !exists {
				return fmt.Errorf("workflow %s provider %s is not registered", workflow.Name, provider)
			}
		}
	}
	return nil
}

func validateProviderExecutors(
	ctx context.Context,
	config ruleengine.EngineConfig,
	providers map[string]ruleengine.RuleProvider,
	registry *executor.Registry,
) error {
	providerNames := make([]string, 0, len(providers))
	for providerName := range providers {
		providerNames = append(providerNames, providerName)
	}
	sort.Strings(providerNames)
	for _, providerName := range providerNames {
		snapshot := providers[providerName].Snapshot(ctx)
		if err := ruleengine.ValidateProviderRuleSet(config, providerName, snapshot); err != nil {
			return fmt.Errorf("provider %s published invalid rules: %w", providerName, err)
		}
		for _, rule := range snapshot.Rules {
			if !rule.Enabled {
				continue
			}
			for _, action := range rule.Actions {
				if _, exists := registry.Get(action.Executor); !exists {
					return fmt.Errorf("provider %s rule %s references unregistered executor %s", providerName, rule.ID, action.Executor)
				}
			}
		}
	}
	return nil
}

// IsExecutionError reports whether err contains an ExecutionError.
func IsExecutionError(err error) bool {
	var executionError *ExecutionError
	return errors.As(err, &executionError)
}
