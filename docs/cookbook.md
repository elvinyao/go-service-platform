# Framework Cookbook

This cookbook contains focused recipes for embedding and extending Go Service Platform. Complete the `docs/getting-started.md` tutorial first if the message, provider, workflow, and executor model is new to you.

All examples use public packages under `pkg/`. The repository's `internal/` packages belong to the reference application and are not framework APIs.

## Recipe: Build a Pipeline From YAML

Use this shape when rules should change independently from Go code:

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
```

`Start` loads provider snapshots and checks every enabled rule against the executor registry. Call it before processing messages and treat an error as a startup failure.

## Recipe: Process an Event and Inspect Partial Failures

The pipeline attempts every selected action. It returns the execution plan even when one or more actions fail:

```go
plan, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, message)
if err != nil {
    var executionErr *pipeline.ExecutionError
    if errors.As(err, &executionErr) {
        for _, failure := range executionErr.Failures {
            logger.Printf(
                "action=%s executor=%s error=%v",
                failure.ActionID,
                failure.Executor,
                failure.Err,
            )
        }
    }
    return fmt.Errorf("process message %s: %w", message.ID, err)
}

logger.Printf("rules=%d actions=%d", len(plan.Rules), len(plan.Actions))
```

An empty plan is not an error. It means the event matched no rules under the selected composition policy.

## Recipe: Match Structured Metadata

Keep stable routing fields in `metadata` instead of encoding them into `content`:

```json
{
  "id": "incident-101",
  "type": "INCIDENT",
  "content": "checkout latency exceeded the threshold",
  "user_id": "monitoring",
  "metadata": {
    "severity": "critical",
    "service": "checkout",
    "deployment": {
      "environment": "production"
    }
  }
}
```

Nested maps use dot-separated condition fields:

```yaml
conditions:
  - field: type
    op: eq
    value: INCIDENT
  - field: metadata.severity
    op: regex
    value: "^(critical|high)$"
  - field: metadata.deployment.environment
    op: eq
    value: production
```

All conditions in one rule must match. Missing fields do not match. Supported operators are `eq`, `contains`, and `regex`.

## Recipe: Let Multiple Rules Contribute Actions

Rules are evaluated independently. This is useful when one event needs both a generic audit action and a domain-specific action:

```yaml
rules:
  - id: audit-all-deployments
    workflow: WorkflowEngine
    enabled: true
    priority: 100
    conditions:
      - field: type
        op: eq
        value: DEPLOYMENT
    actions:
      - id: deployment-audit
        executor: log
        priority: 100
        params:
          level: info
          template: "audit deployment={{.ID}}"
  - id: alert-failed-deployments
    workflow: WorkflowEngine
    enabled: true
    priority: 200
    conditions:
      - field: type
        op: eq
        value: DEPLOYMENT
      - field: metadata.status
        op: eq
        value: failed
    actions:
      - id: failed-deployment-alert
        executor: http
        priority: 200
        params:
          url: "http://127.0.0.1:9000/hooks/deployment"
          body_template: '{"event_id":"{{.ID}}","status":"failed"}'
```

Lower priority numbers execute first. Action deduplication uses the action ID when one is present. Give distinct actions distinct IDs.

## Recipe: Send a Webhook

The built-in HTTP executor supports method, URL, headers, timeout, and a body template:

```yaml
actions:
  - id: notify-operations-webhook
    executor: http
    priority: 100
    params:
      method: POST
      url: "https://hooks.example.test/events"
      timeout_ms: 3000
      headers:
        Authorization: "Bearer replace-at-deployment-time"
        X-Event-Source: go-service-platform
      body_template: '{"id":"{{.ID}}","type":"{{.Type}}","content":"{{.Content}}"}'
```

Production applications should not commit secrets in rule files. Build rules from a trusted source, inject secret-bearing clients through a custom executor, or substitute secrets at a controlled configuration boundary.

Template substitution does not automatically JSON-escape strings. Prefer stable identifiers in inline JSON templates. Use a custom executor with `encoding/json` when arbitrary user content must be sent as JSON.

## Recipe: Register a Custom Executor

An executor is application-owned behavior selected by an action's `executor` field:

```go
type publishExecutor struct {
    publisher Publisher
}

func (e *publishExecutor) Type() string {
    return "publish"
}

func (e *publishExecutor) Execute(
    ctx context.Context,
    message ruleengine.Message,
    action ruleengine.Action,
) error {
    topic, ok := action.Params["topic"].(string)
    if !ok || topic == "" {
        return fmt.Errorf("publish action requires params.topic")
    }
    return e.publisher.Publish(ctx, topic, message)
}
```

Register the same instance that owns application dependencies:

```go
publisher := &publishExecutor{publisher: kafkaPublisher}
engine, err := pipeline.New(config, providers, []executor.Executor{
    executor.NewLogExecutor(),
    publisher,
})
```

Executor checklist:

- return a stable, unique type name
- validate action params before using them
- honor context cancellation and deadlines
- return errors with operation context
- synchronize mutable shared state
- make side effects idempotent when retries can happen upstream
- test success, invalid params, dependency errors, cancellation, and concurrency

Run `go run ./examples/custom-executor` for a complete in-memory implementation.

## Recipe: Implement a Rule Provider

Use a custom provider when rules come from a database, configuration API, feature service, or another trusted source:

```go
type RuleRepository interface {
    LoadRules(context.Context) ([]ruleengine.Rule, string, error)
}

type databaseProvider struct {
    repository RuleRepository
    mu         sync.RWMutex
    snapshot   ruleengine.RuleSet
}

func (p *databaseProvider) Name() string {
    return "database"
}

func (p *databaseProvider) Start(ctx context.Context) error {
    rules, version, err := p.repository.LoadRules(ctx)
    if err != nil {
        return fmt.Errorf("load database rules: %w", err)
    }
    set := ruleengine.RuleSet{
        Source:   p.Name(),
        Version:  version,
        LoadedAt: time.Now().UTC(),
        Rules:    rules,
    }
    if err := ruleengine.ValidateRuleSet(set); err != nil {
        return fmt.Errorf("validate database rules: %w", err)
    }
    p.mu.Lock()
    p.snapshot = set
    p.mu.Unlock()
    return nil
}

func (p *databaseProvider) Snapshot(context.Context) ruleengine.RuleSet {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return ruleengine.CloneRuleSet(p.snapshot)
}
```

A provider snapshot may be read concurrently. Publish complete immutable snapshots rather than mutating a shared rule slice in place. The pipeline validates snapshots again before composition.

## Recipe: Choose a Composition Mode

Composition applies across providers, not across conditions or rules inside one provider.

| Mode | Result |
| --- | --- |
| `single` | Use matches from the first configured provider only |
| `or` | Combine matches from every provider that matched |
| `and` | Produce actions only when every provider matched at least one rule |
| `pipeline` | Require every provider to match and collect results in `pipeline_order` |

Example with a local baseline and a remote override provider:

```yaml
workflows:
  - name: WorkflowEngine
    providers:
      - yaml
      - remote
    mode: or
    pipeline_order:
      - yaml
      - remote
    action_merge:
      dedup: true
      order: priority
action_merge:
  dedup: true
  order: priority
```

Use `single` for one authoritative source, `or` for additive rules, and `and` only when a match from every source is a deliberate gate. Use `pipeline` when the provider collection order is part of the policy. The current pipeline composes snapshots; one provider cannot mutate the message for the next provider.

## Recipe: Add an HTTP Input

Input adapters are application code. Their responsibility is to decode and validate a transport payload, map it to `ruleengine.Message`, and call `Engine.Process`:

```go
func handleEvent(w http.ResponseWriter, r *http.Request) {
    var message ruleengine.Message
    if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
        http.Error(w, "invalid event", http.StatusBadRequest)
        return
    }

    plan, err := engine.Process(
        r.Context(),
        ruleengine.DefaultWorkflowName,
        message,
    )
    if err != nil {
        http.Error(w, "event processing failed", http.StatusBadGateway)
        return
    }
    json.NewEncoder(w).Encode(plan)
}
```

`examples/http-event-gateway` is the complete version. It adds strict JSON decoding, a request-size limit, timeouts, graceful shutdown, health output, and a pipeline snapshot endpoint.

For Kafka, NATS, SQS, Pub/Sub, or a database outbox, keep transport acknowledgment outside the pipeline. Acknowledge only after applying the delivery policy your application needs.

## Recipe: Test a YAML Rule File

Load the real file through `YAMLProvider`, start a pipeline with recording executors, and assert the plan:

```go
func TestIncidentRules(t *testing.T) {
    provider := ruleengine.NewYAMLProvider(
        "yaml",
        "testdata/incident-rules.yaml",
        ruleengine.DefaultWorkflowName,
    )
    recorder := &recordingExecutor{}
    engine, err := pipeline.New(
        ruleengine.DefaultEngineConfig(),
        []ruleengine.RuleProvider{provider},
        []executor.Executor{recorder},
    )
    require.NoError(t, err)
    require.NoError(t, engine.Start(context.Background()))

    plan, err := engine.Process(
        context.Background(),
        ruleengine.DefaultWorkflowName,
        ruleengine.Message{
            ID:   "incident-1",
            Type: "INCIDENT",
            Metadata: map[string]interface{}{
                "severity": "critical",
            },
        },
    )
    require.NoError(t, err)
    require.Equal(t, []string{"route-critical"}, ruleIDs(plan.Rules))
}
```

Keep rule fixtures in `testdata/` next to the owning package. Add table cases for positive matches, near misses, disabled rules, missing metadata, and action ordering.

## Recipe: Test an HTTP Executor

Use `httptest.Server` and inject an application-owned HTTP client:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    require.Equal(t, http.MethodPost, r.Method)
    require.Equal(t, "test-value", r.Header.Get("X-Test"))
    w.WriteHeader(http.StatusNoContent)
}))
defer server.Close()

actionExecutor := executor.NewHTTPExecutorWithClient(server.Client())
err := actionExecutor.Execute(ctx, message, ruleengine.Action{
    Executor: "http",
    Params: map[string]interface{}{
        "url": server.URL,
        "headers": map[string]interface{}{
            "X-Test": "test-value",
        },
    },
})
require.NoError(t, err)
```

Also test non-2xx responses, timeouts, cancellation, malformed templates, and the exact serialized request body.

## Recipe: Separate Local and Deployment Configuration

Keep rule behavior and operational wiring separate:

```text
config/
  runtime.yaml
  rule-engine.yaml
  workflow-rules.yaml
  profiles/
    demo/
      runtime.yaml
      rule-engine.yaml
```

Select files explicitly in deployment manifests:

```bash
RUNTIME_CONFIG=/etc/service/runtime.yaml \
RULE_ENGINE_CONFIG=/etc/service/rule-engine.yaml \
WORKFLOW_RULES=/etc/service/workflow-rules.yaml \
./service-workflow
```

The reference runtime supports environment overrides documented in `docs/configuration.md`. An application embedding only `pkg/pipeline` owns its own configuration layer.

## Recipe: Diagnose an Event That Did Not Act

Check the system in this order:

1. Confirm the input decoded the expected `id`, `type`, and `metadata`.
2. Inspect the pipeline or `/rule-engine` snapshot for the loaded provider version.
3. Confirm the requested workflow has an exact configured policy.
4. Confirm the rule is enabled and belongs to that workflow.
5. Evaluate every condition; conditions use AND semantics.
6. Check the provider composition mode, especially `and` and `pipeline`.
7. Confirm the action executor is registered.
8. Inspect the returned `ExecutionError` for action-level failures.

Do not treat an empty plan as a transport or framework failure. Decide at the application boundary whether unmatched events should be logged, counted, stored, or rejected.

## Recipe: Prepare for Production

Before exposing an application outside a local machine:

- authenticate and authorize input and admin endpoints
- validate request size and transport-specific fields
- restrict outbound HTTP destinations to trusted hosts
- keep credentials out of rule files and diagnostic snapshots
- define retry, backoff, dead-letter, and acknowledgment policies
- make external actions idempotent
- bound input and action concurrency
- add metrics and tracing around match and action outcomes
- define rule rollout, versioning, rollback, and approval procedures
- use readiness for dependencies and liveness only for process health
- run `make verify`, `make test-race`, and `make coverage`

The framework intentionally leaves durable delivery and distributed coordination to the application. This keeps the core deterministic while allowing each deployment to choose the queue, database, and reliability model it actually needs.

## Runnable Scenario Index

| Scenario | Rule File | Sample Input |
| --- | --- | --- |
| Incident routing | `examples/rules/incident-routing.yaml` | Critical checkout incident |
| Release gate | `examples/rules/release-gate.yaml` | Failed or approved production release |
| Support triage | `examples/rules/support-triage.yaml` | P0 refund ticket |
| Webhook chain | `examples/rules/webhook-chain.yaml` | Raw deployment transformed into an audit event |

Use `examples/README.md` for complete commands. Use the six longer scenario guidebooks in `docs/development.md` for event-contract design, debugging paths, extension ideas, and common failure modes.
