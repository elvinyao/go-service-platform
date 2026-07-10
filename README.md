# Go Service Platform

Go Service Platform is a configurable, rule-driven framework for building event-processing services. It separates reusable rule and action orchestration from transport and business integrations, while keeping a complete reference runtime that can be started locally with one command.

The platform provides:

- strict YAML configuration with environment overrides
- composable rule providers and deterministic action plans
- pluggable action executors
- lifecycle-managed services with startup rollback and graceful shutdown
- structured logging and health, readiness, liveness, and admin APIs
- runnable examples that use only public framework packages

Confluence, Mattermost, and BadgeDB are optional demo adapters. They are not required by the framework SDK or the default YAML-first profile.

## Requirements

- Go 1.26 or later
- `curl` for the verification commands
- Docker with Compose only for the containerized path

## Quick Start

Start the fake external APIs and the reference runtime with the full demo profile:

```bash
make dev
```

In another terminal, verify the runtime and send an event:

```bash
curl -s http://localhost:18080/health/readiness
curl -s http://localhost:18080/services
curl -s http://localhost:18080/rule-engine

curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"quickstart-1","type":"AAA","content":"hello from README","user_id":"local-user"}'
```

The runtime log should report a matched YAML rule and action. Stop both processes with `Ctrl+C` in the `make dev` terminal.

## Runtime Profiles

The repository intentionally has two local modes:

| Mode | Command | Providers | Executors | Purpose |
| --- | --- | --- | --- | --- |
| YAML-first | `make fake-server`, then `make run` | YAML | `log`, `http` | Learn and build without business-specific adapters |
| Full demo | `make dev` | YAML, Confluence | `log`, `http`, Mattermost, BadgeDB | Exercise all reference integrations |

`make run` loads `config/runtime.yaml`, `config/rule-engine.yaml`, and `config/workflow-rules.yaml`. `make dev` selects `config/profiles/demo/runtime.yaml` and `config/profiles/demo/rule-engine.yaml` while using the same YAML rules.

Run the equivalent full demo with Docker:

```bash
docker compose up --build
```

Compose publishes the admin API at `http://localhost:18080` on the host loopback interface. Use `ADMIN_PORT=8080 docker compose up --build` to choose another host port.

## Processing Model

```text
input event
  -> ruleengine.RuleProvider snapshots
  -> ruleengine.Composer
  -> ruleengine.ExecutionPlan
  -> pipeline.Engine
  -> executor.Executor implementations
```

A provider supplies rules. The composer matches and merges those rules according to a workflow policy. The pipeline executes the resulting actions in priority order and returns both the plan and any action failures.

The smallest in-process pipeline looks like this:

```go
provider := ruleengine.NewStaticProvider("local", ruleengine.RuleSet{Rules: rules})

cfg := ruleengine.DefaultEngineConfig()
cfg.Workflows[0].Providers = []string{"local"}
cfg.Workflows[0].PipelineOrder = []string{"local"}

engine, err := pipeline.New(
    cfg,
    []ruleengine.RuleProvider{provider},
    []executor.Executor{executor.NewLogExecutor()},
)
if err != nil {
    return err
}
if err := engine.Start(ctx); err != nil {
    return err
}

plan, err := engine.Process(ctx, ruleengine.DefaultWorkflowName, message)
```

See `examples/basic-rule-pipeline` for the complete runnable program.

## Configuration

Configuration is split by responsibility:

- `config/runtime.yaml`: admin server, input, and adapter wiring
- `config/rule-engine.yaml`: provider composition and action merge policy
- `config/workflow-rules.yaml`: local YAML rules
- `config/profiles/demo/`: opt-in configuration for all demo adapters

Select files with `RUNTIME_CONFIG`, `RULE_ENGINE_CONFIG`, and `WORKFLOW_RULES`. Operational values such as `ADMIN_ADDR`, integration URLs, and logging options can be overridden through environment variables.

Configuration loading is strict. Unknown YAML fields, multiple YAML documents, invalid durations or URLs, duplicate names, invalid composition modes, missing explicitly selected files, unavailable providers, and rules that reference unregistered executors fail startup with a descriptive error.

See [Configuration](docs/configuration.md) for every field and override.

## Rules

The default rule matches an `AAA` event and logs it:

```yaml
rules:
  - id: yaml_log_aaa
    workflow: WorkflowEngine
    enabled: true
    priority: 100
    conditions:
      - field: type
        op: eq
        value: AAA
    actions:
      - id: log_aaa
        executor: log
        priority: 100
        params:
          level: info
          template: "YAML matched event type={{.Type}} id={{.ID}}"
```

Rules support `eq`, `contains`, and `regex` conditions. Built-in executors are `log` and `http`; applications can register their own `executor.Executor` implementations.

## Public Packages

- `pkg/ruleengine`: messages, rules, matching, composition, static and YAML providers
- `pkg/executor`: executor API, registry, templates, and built-in `log` and `http` executors
- `pkg/pipeline`: provider startup, configuration preflight, plan execution, and snapshots
- `pkg/config`: strict runtime configuration loading and validation
- `pkg/runtime`: fail-fast admin HTTP server lifecycle
- `pkg/health`: health checks and HTTP handlers
- `pkg/logger`: structured logging and context fields
- `pkg/errors`: typed application errors
- `pkg/concurrency`: worker-pool and concurrency helpers

Packages below `internal/` implement the reference application and demo adapters. External applications cannot and should not import them.

## Admin API

The reference runtime exposes:

- `GET /health`
- `GET /health/readiness`
- `GET /health/liveness`
- `GET /health/service?service=<name>`
- `GET /services`
- `GET /workflows`
- `GET /rule-engine`

Critical service failures make health and readiness return HTTP 503. Warning-level degradation remains ready. The current runtime does not expose `/metrics`; defining and implementing a stable metrics contract is future work.

Liveness reports only whether the process and admin server can respond; dependency failures do not turn liveness into 503 and therefore do not create orchestrator restart loops. The repository profiles bind the unauthenticated admin API to `127.0.0.1` by default. Containers opt in to an all-interface listener explicitly.

## Examples

```bash
go run ./examples/basic-rule-pipeline
go run ./examples/websocket-to-log
go run ./examples/websocket-to-mattermost
```

The WebSocket examples require `make fake-server` in another terminal. See [Runnable Examples](examples/README.md) for inputs and environment variables.

## Development

```bash
make verify
make test-race
make coverage
```

`make coverage` enforces at least 90% statement coverage for every package that contains tests in both Go modules.

The full developer workflow, debugger setup, architecture tour, troubleshooting steps, and six realistic use cases are in the [Development Guidebook](docs/development.md).

## Project Layout

```text
cmd/service-workflow/       Reference runtime entry point
config/                     Base and demo-profile configuration
docs/                       Architecture and developer documentation
examples/                   Programs using public framework packages
fake-server/                Local Confluence-, Mattermost-, and WebSocket-like APIs
internal/adapters/          Optional demo provider and executors
internal/app/               Reference runtime wiring and admin API
internal/di/                Reference runtime dependency wiring
internal/service/           Lifecycle-managed reference services
pkg/config/                 Public runtime configuration package
pkg/executor/               Public executor SDK
pkg/pipeline/               Public rule-to-action orchestration
pkg/ruleengine/             Public rule model and composition SDK
pkg/runtime/                Public runtime helpers
```

## Further Reading

- [Configuration](docs/configuration.md)
- [Rule Engine](docs/rule-engine.md)
- [Executors](docs/executors.md)
- [Bounded Concurrency](docs/concurrency.md)
- [Adapters](docs/adapters.md)
- [Development Guidebook](docs/development.md)
- [Framework Design](docs/superpowers/specs/2026-06-20-go-service-framework-design.md)
