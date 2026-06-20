# Go Service Framework

A configurable, rule-driven Go service framework for building event processing runtimes. The framework provides a reusable rule engine, executor SDK, runtime lifecycle helpers, structured logging, health checks, and a runnable reference application.

The reference app receives messages from a demo WebSocket input, matches rules from YAML and optional adapter providers, then executes configured actions such as logging, HTTP calls, demo database writes, or Mattermost notifications.

## Quick Start

Run the test suites:

```bash
make test
make test-fake
```

Build the reference runtime:

```bash
make build
```

Start the fake APIs and runtime in one terminal:

```bash
make dev
```

Or start them separately:

```bash
make fake-server
```

In another terminal:

```bash
make run
```

Verify the runtime:

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/rule-engine
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m1","type":"AAA","content":"hello from README","user_id":"u1","timestamp":"2026-06-20T00:00:00Z"}'
```

## Docker Compose

```bash
docker compose up --build
```

Then verify:

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/rule-engine
```

## Core Packages

- `pkg/ruleengine`: public rule model, matching, composition, YAML provider, and execution plans.
- `pkg/executor`: public executor interface, registry, template renderer, and built-in `log` and `http` executors.
- `pkg/runtime`: runtime helpers such as the fail-fast admin server.
- `pkg/config`: runtime configuration loading, defaults, environment overrides, and validation.
- `pkg/logger`: structured logging.
- `pkg/health`: health, readiness, and liveness checks.
- `pkg/errors`: typed application errors.

## Demo Adapters

Demo integrations live under `internal/adapters` and are used by the reference runtime:

- WebSocket input service, currently backed by `internal/service.WebSocketService`.
- Confluence settings rule provider.
- Mattermost action executor.
- BadgeDB demo action executor.

These adapters show how to integrate a real system without making those integrations part of the core framework SDK.

## Configuration

The reference runtime uses three configuration files:

- `config/runtime.yaml`: admin server, inputs, and demo adapter settings.
- `config/rule-engine.yaml`: rule provider composition and merge policy.
- `config/workflow-rules.yaml`: local YAML rules.

Common environment overrides:

- `RUNTIME_CONFIG`
- `RULE_ENGINE_CONFIG`
- `WORKFLOW_RULES`
- `ADMIN_ADDR`
- `WEBSOCKET_SERVER_URL`
- `CONFLUENCE_API_ENDPOINT`
- `MATTERMOST_SERVER_URL`
- `MATTERMOST_WS_URL`

## Documentation

- [Configuration](docs/configuration.md)
- [Rule Engine](docs/rule-engine.md)
- [Executors](docs/executors.md)
- [Adapters](docs/adapters.md)
- [Development](docs/development.md)

## Project Layout

```text
cmd/service-workflow/       Reference runtime entry point
config/                     Default local configuration
docs/                       Framework documentation
fake-server/                Local fake APIs for development
internal/adapters/          Demo integrations
internal/app/               Reference runtime wiring
internal/service/           Demo services used by adapters
pkg/config/                 Public runtime configuration package
pkg/executor/               Public executor SDK
pkg/ruleengine/             Public rule engine SDK
pkg/runtime/                Public runtime helpers
```

## Extension Model

Framework users extend behavior by adding:

- rules in YAML or custom `ruleengine.RuleProvider` implementations
- action executors implementing `executor.Executor`
- input adapters that translate external events into `ruleengine.Message`

The old Go workflow struct model is no longer the primary extension point. The reference app is rule-driven by default.
