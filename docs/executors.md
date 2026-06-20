# Executors

Executors run actions selected by the rule engine.

## Interface

```go
type Executor interface {
    Type() string
    Execute(context.Context, ruleengine.Message, ruleengine.Action) error
}
```

Register executors with:

```go
registry := executor.NewRegistry()
registry.Register(executor.NewLogExecutor())
registry.Register(executor.NewHTTPExecutor())
```

## Templates

Executors can render message fields with Go templates:

```yaml
template: "type={{.Type}} id={{.ID}} content={{.Content}}"
```

Available fields:

- `.ID`
- `.Type`
- `.Content`
- `.UserID`
- `.Timestamp`
- `.Metadata`

## Built-In `log` Executor

```yaml
actions:
  - id: log_aaa
    executor: log
    priority: 100
    params:
      level: info
      template: "Matched {{.Type}} message {{.ID}}"
```

Supported levels:

- `debug`
- `info`
- `warn`
- `error`

## Built-In `http` Executor

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

The executor treats any non-2xx response as an action error.

## Demo Executors

The reference runtime also registers demo executors:

- `mattermost` from `internal/adapters/mattermost`
- `db` from `internal/adapters/badgedb`

These demonstrate framework extension points and are not core SDK dependencies.
