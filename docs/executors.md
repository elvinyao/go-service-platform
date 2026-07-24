# Executors

Executors perform actions selected by an execution plan. They are small, application-owned capabilities registered with `pkg/pipeline`.

## Interface

```go
type Executor interface {
    Type() string
    Execute(context.Context, ruleengine.Message, ruleengine.Action) error
}
```

Executor types must be non-empty and unique. Both `executor.Registry.Register` and `pipeline.New` reject nil, typed-nil, empty, and duplicate registrations without replacing an existing executor. `pipeline.Engine.Start` rejects enabled provider rules that reference an unregistered type, so wiring mistakes fail before messages are accepted.

An executor that owns a client, connection, or worker can additionally implement:

```go
type Starter interface {
    Start(context.Context) error
}

type Stopper interface {
    Stop(context.Context) error
}
```

These lifecycle interfaces are optional and live in `pkg/pipeline`. Lifecycle-aware executors start after provider preflight. On failure, the engine stops started executors and providers in reverse order. Normal `Engine.Stop` uses the same reverse order and waits for in-flight processing first.

## Registration

```go
engine, err := pipeline.New(
    config,
    providers,
    []executor.Executor{
        executor.NewLogExecutor(),
        executor.NewHTTPExecutor(),
        &PublishExecutor{client: client},
    },
)
```

The lower-level `executor.Registry` is public for applications that need custom orchestration, but most applications should register executors through `pipeline.New`.

## Templates

Built-in executors use Go templates:

```yaml
template: "type={{.Type}} id={{.ID}} content={{.Content}}"
```

Available values are `.ID`, `.Type`, `.Content`, `.UserID`, `.Timestamp`, and `.Metadata`. Invalid template syntax is returned as an action error.

## Log Executor

```yaml
actions:
  - id: log_event
    executor: log
    priority: 100
    params:
      level: info
      template: "Matched {{.Type}} message {{.ID}}"
```

Supported levels are `debug`, `info`, `warn`, and `error`. An empty template produces a default message. Unknown levels use `info`.

## HTTP Executor

```yaml
actions:
  - id: post_event
    executor: http
    priority: 100
    params:
      method: POST
      url: "http://localhost:9000/events"
      timeout_ms: 3000
      headers:
        X-Source: service-workflow
      body_template: '{"id":"{{.ID}}","type":"{{.Type}}"}'
```

Behavior:

- `url` is required.
- `method` defaults to `POST`.
- `timeout_ms` defaults to 3000 and accepts positive YAML numeric values.
- `Content-Type` defaults to `application/json` unless supplied.
- Caller-provided headers are preserved.
- Context cancellation and timeout errors are returned.
- Any response outside 200-299 is an action error; up to 1024 bytes of response body are included for diagnosis.

Use `executor.NewHTTPExecutorWithClient(client)` to supply an application-owned `*http.Client` with custom transport, TLS, proxy, tracing, or connection-pool behavior. The action timeout still applies through the request context.

Use a dedicated custom executor when authentication, retries, idempotency, rate limiting, or response parsing is part of the domain contract.

## Custom Executor Example

```go
type PublishExecutor struct {
    client EventPublisher
}

func (e *PublishExecutor) Type() string {
    return "publish"
}

func (e *PublishExecutor) Execute(
    ctx context.Context,
    message ruleengine.Message,
    action ruleengine.Action,
) error {
    topic, _ := action.Params["topic"].(string)
    if topic == "" {
        return fmt.Errorf("publish action requires params.topic")
    }
    return e.client.Publish(ctx, topic, message)
}
```

Keep executors stateless when practical. If an executor owns shared state, make it concurrency-safe because different input callbacks may process messages concurrently.

## Failure Semantics

The pipeline attempts every action in the plan even if an earlier action fails. It then returns one `pipeline.ExecutionError` containing all failures. This is useful for independent notifications, but it is not a transaction.

For operations that must be atomic, place the transaction inside one executor or build a domain-specific executor that owns compensation and idempotency.

## Demo Executors

The reference runtime optionally registers:

- `mattermost` from `internal/adapters/mattermost`
- `db` from `internal/adapters/badgedb`

They demonstrate extension points and are intentionally excluded from the public core packages. Enable them with `config/profiles/demo/runtime.yaml` or `make dev`.
