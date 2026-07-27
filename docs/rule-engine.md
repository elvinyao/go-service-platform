# Rule Engine

The rule engine turns provider snapshots and an input message into a deterministic execution plan. `pkg/ruleengine` owns matching and composition; `pkg/pipeline` owns provider startup, executor preflight, and action execution.

## Message Model

```go
type Message struct {
    ID        string
    Type      string
    Content   string
    UserID    string
    Timestamp time.Time
    Metadata  map[string]interface{}
}
```

Conditions can read:

- `id`
- `type`
- `content`
- `user_id`
- `timestamp`
- `metadata.<key>`
- nested values such as `metadata.deployment.environment`

A missing field does not match. Every segment in a metadata path must be non-empty, so fields such as `metadata..environment` are rejected during rule validation.

## Conditions

All conditions in one rule must match. A rule without conditions matches every message in its workflow.

Exact normalized value:

```yaml
- field: type
  op: eq
  value: DEPLOYMENT
```

Substring:

```yaml
- field: content
  op: contains
  value: urgent
```

Go regular expression:

```yaml
- field: content
  op: regex
  value: "^deploy:[[:space:]]+production$"
```

Invalid regular expressions and unsupported operators are rejected when YAML rules load.

## Rule Providers

A provider owns a named snapshot of rules:

```go
type RuleProvider interface {
    Name() string
    Start(context.Context) error
    Snapshot(context.Context) RuleSet
}
```

Public providers:

- `ruleengine.NewStaticProvider` for programmatic rules and tests
- `ruleengine.NewYAMLProvider` for strict YAML files

The reference runtime also contains an optional Confluence-like provider under `internal/adapters/confluence`. It is a demonstration, not part of the public SDK.

Provider names must be unique and must match the names in `EngineConfig`. `pipeline.Engine.Start` starts every provider and then verifies that every rule with an explicit workflow is reachable through that workflow policy and that every enabled rule references a registered executor.

Use `ruleengine.ValidateRuleSet` in custom providers before publishing a snapshot. Applications that validate a snapshot together with engine wiring can use `ruleengine.ValidateProviderRuleSet`. The pipeline performs both checks at startup and again while composing each message, so dynamic providers cannot silently introduce unknown workflows, unreachable provider/workflow combinations, duplicate rule IDs, unsupported fields or operators, invalid regular expressions, missing actions, or empty executor types.

## Composition Policies

Each workflow has one policy:

```yaml
workflows:
  - name: WorkflowEngine
    providers: [yaml, remote]
    mode: or
    pipeline_order: [yaml, remote]
    action_merge:
      dedup: true
      order: priority
```

Modes:

- `single`: match one authoritative provider; exactly one provider is required.
- `or`: collect matches from any provider.
- `and`: return a plan only when every provider has at least one match.
- `pipeline`: apply the same all-providers requirement using `pipeline_order`.

For `single`, exactly one provider is required. For `pipeline`, `pipeline_order` must contain every configured provider exactly once. Unknown modes, duplicate providers, duplicate workflow names, and incomplete pipeline orders are configuration errors.

## Execution Plans

`Composer.BuildExecutionPlan` returns:

```go
type ExecutionPlan struct {
    Workflow string
    Rules    []Rule
    Actions  []Action
}
```

Matched rules and actions are sorted by ascending priority. Stable sorting preserves source order when priorities are equal.

Action deduplication uses `action.id` when it is present. Without an ID, the key is the executor type plus serialized params. Give important actions explicit IDs so deduplication behavior remains obvious during review.

Workflow-level `action_merge` values override the global block. An explicit `dedup: false` is preserved; omitting `dedup` inherits the global value. Processing a workflow with no exact configured policy returns an error instead of falling back to another workflow.

No match is a successful empty plan, not an error.

## Running a Pipeline

```go
provider := ruleengine.NewYAMLProvider(
    "yaml",
    "config/workflow-rules.yaml",
    ruleengine.DefaultWorkflowName,
)

engine, err := pipeline.New(
    ruleengine.DefaultEngineConfig(),
    []ruleengine.RuleProvider{provider},
    []executor.Executor{
        executor.NewLogExecutor(),
        executor.NewHTTPExecutor(),
    },
)
if err != nil {
    return err
}
if err := engine.Start(ctx); err != nil {
    return err
}
defer engine.Stop(context.Background())

plan, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, message)
```

Calling `Process` before `Start` or after `Stop` returns an error. `Stop` waits for in-flight processing, then stops lifecycle-aware executors and providers in reverse startup order. A successful stop permits a later clean start.

The engine continues executing later actions when one action fails, then returns the plan together with `*pipeline.ExecutionError`. Each failure includes the action ID, executor type, and original error:

```go
plan, err := engine.Process(ctx, workflow, message)
var executionErr *pipeline.ExecutionError
if errors.As(err, &executionErr) {
    for _, failure := range executionErr.Failures {
        log.Printf("action=%s executor=%s: %v", failure.ActionID, failure.Executor, failure.Err)
    }
}
```

Use `engine.Snapshot(ctx)` for an immutable admin/debug view of composition configuration, provider rule sets, and registered executor types.

## Custom Provider Example

```go
type FeatureFlagProvider struct {
    rules ruleengine.RuleSet
    stop  func(context.Context) error
}

func (p *FeatureFlagProvider) Name() string {
    return "feature-flags"
}

func (p *FeatureFlagProvider) Start(ctx context.Context) error {
    p.rules = loadRulesFromYourSystem(ctx)
    return nil
}

func (p *FeatureFlagProvider) Snapshot(context.Context) ruleengine.RuleSet {
    return ruleengine.CloneRuleSet(p.rules)
}

func (p *FeatureFlagProvider) Stop(ctx context.Context) error {
    if p.stop == nil {
        return nil
    }
    return p.stop(ctx)
}
```

`Stop(context.Context) error` is optional. When present, the pipeline invokes it during startup rollback and normal shutdown. Production providers should publish snapshots atomically, avoid network calls from `Snapshot`, use `ruleengine.CloneRuleSet` to isolate nested condition and action values, and return a startup error when no trustworthy initial snapshot can be loaded.

## YAML Loading Guarantees

The YAML provider:

- rejects unknown fields
- requires unique non-empty rule IDs
- requires at least one action per rule
- validates condition operators and regular expressions
- defaults omitted `enabled` to `true`
- defaults omitted `workflow` to the provider's configured workflow
- returns cloned snapshots so callers cannot mutate provider state

An explicitly configured missing rules file is a startup error. Pass an empty path only when an intentionally empty rule set is desired in an embedded application.
