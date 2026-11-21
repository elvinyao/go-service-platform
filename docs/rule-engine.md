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
    Payload   map[string]interface{}
}
```

The fixed fields are a small common envelope. `Payload` carries whatever else the event has, with no schema imposed by the framework. A chat event, for example, can put its channel, mentions, reaction, and thread there:

```json
{
  "id": "evt-1",
  "type": "message",
  "user_id": "alice",
  "content": "!deploy billing-api",
  "payload": {
    "channel": {"id": "C1", "name": "ops", "type": "public"},
    "mentions": ["bot"],
    "thread": {"reply_count": 3}
  }
}
```

Conditions can read:

- `id`
- `type`
- `content`
- `user_id`
- `timestamp`
- `payload.<path>` and `metadata.<path>`, where a path walks nested objects by key and lists by zero-based index, such as `payload.channel.type` or `payload.mentions.0`

Every path segment must be non-empty, so fields such as `payload..id` are rejected during rule validation.

## Conditions

All top-level conditions in one rule must match. A rule without conditions matches every message in its workflow.

A condition is either a comparison (`field`, `op`, `value`) or one logical group (`any`, `all`, `not`).

### Comparison Operators

| Operator | Matches when |
| --- | --- |
| `eq`, `ne` | the field equals / does not equal the value |
| `contains`, `not_contains` | a text field contains the value as a substring, or a list field has the value as a member |
| `prefix`, `suffix` | the field starts / ends with the value |
| `regex` | the field matches a Go regular expression |
| `in`, `not_in` | the field equals one of / none of the values in a list |
| `exists`, `not_exists` | the field is present / absent; `value` must be omitted |
| `gt`, `gte`, `lt`, `lte` | the field compares numerically with the value |

Values are compared in normalized text form, so the number `3` equals the text `"3"`. Numeric operators accept numbers and numeric text; anything else does not match.

Add `ignore_case: true` to compare text case-insensitively. It applies to every operator except the numeric and existence ones.

A field that is absent from the message never matches, including for `ne`, `not_contains`, and `not_in`. Only `not_exists` matches an absent field. Use a `not` group when "absent or different" is the intent.

```yaml
- field: content
  op: prefix
  value: "!deploy"
  ignore_case: true

- field: payload.mentions
  op: contains
  value: bot

- field: payload.channel.type
  op: in
  value: [direct, group]

- field: payload.thread.reply_count
  op: gte
  value: 20
```

### Logical Groups

`any` matches when at least one nested condition matches, `all` when every one does, and `not` inverts a single condition. Groups nest to any depth.

```yaml
conditions:
  - field: type
    op: eq
    value: message
  - any:
      - field: payload.mentions
        op: contains
        value: bot
      - field: payload.channel.type
        op: eq
        value: direct
  - not:
      field: content
      op: prefix
      value: "!"
```

A condition must be exactly one comparison or exactly one group. Invalid regular expressions, unsupported operators, mismatched value types, and mixed or empty groups are rejected when rules load.

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

## Timeouts and Retries

The pipeline runs each action under an execution policy. Engine-wide defaults live in the `execution` block of the engine configuration:

```yaml
execution:
  timeout: "10s"
  retry:
    max_attempts: 3
    backoff: "200ms"
    max_backoff: "2s"
```

An action can override them:

```yaml
actions:
  - id: reply
    executor: http
    timeout: "3s"
    retry:
      max_attempts: 2
      backoff: "100ms"
    params:
      url: "https://chat.example/api/posts"
```

- `timeout` bounds each attempt through the context passed to the executor. Empty or `"0s"` means no limit. An executor must honor context cancellation for the limit to take effect.
- `retry.max_attempts` is the total number of attempts including the first. The default is `1`, so nothing is retried unless configured.
- `retry.backoff` is the wait before the second attempt and doubles after each retry, capped by `retry.max_backoff` when set. An empty backoff retries immediately.
- An action-level `retry` block replaces the engine policy as a whole; an action-level `timeout` replaces only the timeout.

Retrying stops as soon as an attempt succeeds, the executor returns an error marked with `executor.Permanent`, or the caller's context is cancelled. A panic inside an executor is recovered and reported as a permanent action failure. Each `pipeline.ActionFailure` records how many attempts were made.

Retries can repeat side effects. Enable them only for actions that are safe to run more than once.

## Concurrent Dispatch and Deduplication

`Engine.Process` handles one event synchronously. A long-running input such as a chat connection should not call it directly, because one slow action would hold up every later event. `pipeline.Dispatcher` sits between the input and the engine:

```go
dispatcher, err := pipeline.NewDispatcher(config.Dispatch, func(ctx context.Context, message ruleengine.Message) {
    if _, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, message); err != nil {
        log.Printf("event %s failed: %v", message.ID, err)
    }
})
if err != nil {
    return err
}
defer dispatcher.Stop(context.Background())

// In the input's receive loop:
err = dispatcher.Submit(ctx, message)
```

It is configured by the `dispatch` block of the engine configuration:

```yaml
dispatch:
  workers: 4
  queue_size: 64
  ordering_key: "payload.channel.id"
  queue_full: block
  dedup:
    enabled: true
    ttl: "10m"
    max_entries: 10000
```

- `workers` is how many events are processed at the same time (default `4`). `queue_size` is how many events may wait per worker (default `64`).
- `ordering_key` names a message field. Events with the same value go to the same worker and are processed one at a time in arrival order, so two messages in one channel are never handled out of order. Events with different values run in parallel. When it is empty, or the field is absent from an event, that event has no ordering guarantee.
- `queue_full` decides what happens when a worker's queue is full. `block` (default) makes `Submit` wait, which pushes backpressure onto the input. `drop` makes `Submit` return `pipeline.ErrQueueFull` immediately.
- `dedup` drops an event whose `id` was already accepted within `ttl`; `Submit` returns `pipeline.ErrDuplicateEvent`. It is off by default in the SDK and enabled in the repository profiles. Events without an `id` are never deduplicated. At most `max_entries` IDs are remembered, oldest forgotten first.

An ID is remembered when the event is accepted, not when it succeeds, so a redelivered event is skipped even if its first run failed; use action retries for transient failures. A dropped event is not remembered and can be redelivered. Deduplication state is in memory and is lost on restart.

`Stop` rejects new events and waits for everything already queued to finish, up to the context deadline. If the deadline passes first, `Stop` cancels the context of the events being handled (so executors that honor it return promptly), makes the workers skip the events still queued, and returns an error. Handler contexts keep the submitter's values but ignore its cancellation; only this deadline cancels them. `Stats` reports workers, queue depth, and accepted, processed, duplicate, dropped, panic, and abandoned counts (`abandoned` is events skipped because `Stop` ran out of time). Handlers and executors run concurrently and must be safe for concurrent use.

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

Calling `Process` before `Start` or after `Stop` returns an error. `Stop` rejects new `Process` calls at once, waits for in-flight processing, then stops lifecycle-aware executors and providers in reverse startup order. A successful stop permits a later clean start. If the context ends while messages are still in flight, `Stop` returns an error and leaves the engine pending cleanup, so `Stop` can be called again; it does not block past the deadline even when an executor ignores cancellation.

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
