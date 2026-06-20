# Go Service Framework Design

## Summary

This project will be reshaped from a business-specific service workflow demo into a general-purpose, rule-driven Go service framework. The reference runtime will remain runnable out of the box, but Mattermost, Confluence, and BadgeDB will become demo adapters instead of the core product narrative.

The target experience is:

- A reusable SDK under `pkg/` for rule matching, rule composition, executor registration, runtime lifecycle, logging, health checks, and configuration.
- A reference application under `cmd/service-workflow` that loads configuration, wires inputs, providers, and executors, and exposes admin endpoints.
- A local developer path with `make dev`.
- A containerized developer path with `docker compose up`.
- Accurate English documentation that matches the actual commands, package layout, configuration files, and runtime behavior.

## Goals

- Make the platform a configurable, locally runnable, rule-driven Go service framework.
- Move framework-level rule engine and executor APIs from `internal/` to stable `pkg/` packages.
- Keep business integrations as optional demo adapters.
- Replace hard-coded runtime values with validated configuration.
- Fail fast when critical runtime components, such as the admin server listener, cannot start.
- Remove or migrate legacy `WorkflowA` and `WorkflowC` behavior into rules and executors.
- Make README and supporting docs accurate, current, and entirely English.
- Keep all code comments and configuration comments in English.

## Non-Goals

- Building a production-grade plugin marketplace or dynamic binary plugin system.
- Replacing all demo integrations with real production service implementations.
- Introducing a database dependency for the core framework.
- Making Mattermost, Confluence, or BadgeDB required for framework users.
- Preserving the old Go workflow extension model as the primary user-facing API.

## Proposed Package Layout

```text
cmd/
  service-workflow/

pkg/
  config/
  context/
  concurrency/
  errors/
  executor/
  health/
  logger/
  resilience/
  ruleengine/
  runtime/

internal/
  adapters/
    badgedb/
    confluence/
    mattermost/
    websocket/
  app/

examples/
  basic-rule-pipeline/
  websocket-to-log/
  websocket-to-mattermost/

config/
  runtime.yaml
  rule-engine.yaml
  workflow-rules.yaml
```

`pkg/` contains reusable framework APIs. `internal/adapters` contains demo integrations used by the reference runtime. `internal/app` contains application wiring that should not be imported by external projects.

## Core SDK Design

### Rule Engine

`pkg/ruleengine` exposes the framework's rule model and composition behavior:

- `Message`
- `Condition`
- `Action`
- `Rule`
- `RuleSet`
- `RuleProvider`
- `WorkflowPolicy`
- `EngineConfig`
- `ExecutionPlan`
- YAML rule provider
- condition matcher
- composer
- validation helpers

Supported condition operators:

- `eq`
- `contains`
- `regex`

Supported composition modes:

- `single`
- `or`
- `and`
- `pipeline`

Supported action merge behavior:

- deduplicate actions by ID or executor-plus-params
- order actions by priority

### Executor SDK

`pkg/executor` exposes the executor model:

```go
type Executor interface {
    Type() string
    Execute(context.Context, ruleengine.Message, ruleengine.Action) error
}
```

It also provides:

- executor registry
- template rendering
- built-in `log` executor
- built-in `http` executor

Demo executors remain outside the core package:

- Mattermost executor
- BadgeDB executor

This keeps the SDK usable without pulling business-specific dependencies into the core framework path.

## Runtime Design

The reference runtime under `cmd/service-workflow` will:

- load runtime, rule engine, and local rule configuration
- initialize logging
- create lifecycle-managed inputs, providers, and executors
- start the admin server
- connect configured inputs to the rule engine
- execute matched actions
- expose health and admin endpoints
- shut down gracefully on OS signals

Critical startup failures must stop the process and clean up already-started components. The admin HTTP listener is critical. If it cannot bind its configured address, startup fails instead of leaving the process in a partially running state.

## Configuration Design

The project keeps multiple configuration files, but each file has a clear role:

```text
config/runtime.yaml
config/rule-engine.yaml
config/workflow-rules.yaml
```

`config/runtime.yaml` configures the reference app:

- admin server address and timeouts
- enabled inputs
- enabled providers
- enabled executors
- demo adapter settings
- local fake-server defaults

`config/rule-engine.yaml` configures composition policy:

- provider list
- workflow policy
- pipeline ordering
- action merge behavior
- provider refresh behavior

`config/workflow-rules.yaml` configures local YAML rules.

Environment variables are an override layer for operational values such as log level, admin address, and integration URLs. The README should document the common overrides, but YAML remains the primary documented configuration surface.

## Demo Adapter Strategy

The existing business-oriented components are retained only as optional adapters:

- WebSocket input adapter
- Confluence rule provider adapter
- Mattermost executor adapter
- BadgeDB demo executor or store adapter

These adapters demonstrate how to extend the framework, but framework users should be able to run simple rule pipelines without understanding these integrations.

## Legacy Workflow Migration

`WorkflowA` and `WorkflowC` will be removed from the default programming model.

The previous `WorkflowA` badge behavior becomes a YAML rule plus demo database executor.

The previous `WorkflowC` AAA-event notification behavior becomes:

- Confluence provider rules, when enabled
- Mattermost executor actions, when enabled
- YAML examples for local development

The README should teach users to add rules, providers, and executors, not to add Go workflow structs.

## Developer Experience

### `make dev`

`make dev` starts the fake server and reference runtime with the default development configuration. It should print the admin endpoint and example verification commands.

### Docker Compose

`docker compose up` starts an equivalent local environment. It should use the same default behavior as `make dev` unless overridden through Compose environment variables.

### Verification Flow

The documented local verification path should include:

- start dependencies and runtime
- call health/readiness endpoints
- send a sample message
- observe a matched rule through logs or admin output
- stop the environment cleanly

## Admin Endpoints

The reference runtime keeps or adds these endpoints:

- `/health`
- `/health/readiness`
- `/health/liveness`
- `/services`
- `/rule-engine`

`/metrics` can be added if the existing metrics package can be integrated cleanly during implementation. If not, it should be documented as future work rather than presented as supported.

## Documentation Plan

Primary docs:

```text
README.md
docs/configuration.md
docs/rule-engine.md
docs/executors.md
docs/adapters.md
docs/development.md
```

README should focus on:

- what the framework is
- quick start
- package layout
- configuration files
- rule examples
- extension points
- local verification

Business-specific integrations should appear only in adapter and example sections.

## Testing Strategy

The implementation should keep tests close to the migrated packages:

- rule matching tests
- rule composition tests
- YAML provider tests
- executor registry tests
- template rendering tests
- runtime configuration validation tests
- admin server startup failure test
- adapter tests where practical

Existing tests should be updated rather than removed unless they only verify deleted legacy workflows.

## Acceptance Criteria

- `go test ./...` passes for the root module.
- Fake-server tests pass, either in its current module or after being merged into the root module.
- `go build ./cmd/service-workflow` passes.
- `make dev` starts fake-server and the reference runtime.
- `docker compose up` starts an equivalent local environment.
- A documented curl command can send a sample message and produce an observable rule execution result.
- README is fully English and matches the actual project.
- Code comments and configuration comments are English.
- Admin server address is configurable.
- Admin server bind failure fails startup and cleans up started services.
- `WorkflowA` and `WorkflowC` are no longer part of the default runtime model.
- Mattermost, Confluence, and BadgeDB are documented as demo adapters, not core framework requirements.

## Risks and Mitigations

- Moving packages from `internal/` to `pkg/` can break imports. Mitigation: migrate package-by-package and run tests after each major move.
- Business-specific dependencies may leak into core packages. Mitigation: keep Mattermost, Confluence, and BadgeDB under internal adapters and verify `pkg/ruleengine` and `pkg/executor` have minimal dependencies.
- Runtime rewrite could temporarily reduce local run reliability. Mitigation: add startup and configuration tests before changing runtime wiring.
- Docker Compose may duplicate Makefile behavior. Mitigation: share the same default config files and use environment overrides only where necessary.

## Implementation Phases

1. Extract rule engine and executor SDK packages.
2. Add validated runtime configuration.
3. Rewrite reference runtime wiring around the SDK.
4. Convert business-specific code into adapters.
5. Migrate legacy workflow behavior into rules and demo executors.
6. Add `make dev` and Docker Compose support.
7. Rewrite README and supporting docs.
8. Run full verification and remove stale code.
