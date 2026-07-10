package pipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// Engine composes rules from providers and executes the resulting actions.
type Engine struct {
	composer  *ruleengine.Composer
	providers map[string]ruleengine.RuleProvider
	executors *executor.Registry

	mu      sync.RWMutex
	started bool
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
	for _, actionExecutor := range executors {
		if err := registry.Register(actionExecutor); err != nil {
			return nil, err
		}
	}

	return &Engine{
		composer:  ruleengine.NewComposer(config, providerMap),
		providers: providerMap,
		executors: registry,
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
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return nil
	}

	names := make([]string, 0, len(e.providers))
	for name := range e.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := e.providers[name].Start(ctx); err != nil {
			return fmt.Errorf("start rule provider %s: %w", name, err)
		}
	}
	if err := validateProviderExecutors(ctx, e.providers, e.executors); err != nil {
		return err
	}

	e.started = true
	return nil
}

// Process builds and executes a plan for one message.
//
// It attempts every action and returns both the plan and an ExecutionError when
// one or more independent actions fail.
func (e *Engine) Process(ctx context.Context, workflow string, message ruleengine.Message) (ruleengine.ExecutionPlan, error) {
	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()
	if !started {
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

func validateProviderExecutors(ctx context.Context, providers map[string]ruleengine.RuleProvider, registry *executor.Registry) error {
	providerNames := make([]string, 0, len(providers))
	for providerName := range providers {
		providerNames = append(providerNames, providerName)
	}
	sort.Strings(providerNames)
	for _, providerName := range providerNames {
		snapshot := providers[providerName].Snapshot(ctx)
		if err := ruleengine.ValidateRuleSet(snapshot); err != nil {
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
