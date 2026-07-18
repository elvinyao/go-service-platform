# Documentation Map

Use this page to choose the shortest document for the task at hand. The root `README.md` is the product overview and quick start; documents in this directory explain development and extension in depth.

## First Hour

1. Complete `getting-started.md` to run a static pipeline, a YAML-driven HTTP input, and a custom executor.
2. Keep `cookbook.md` open while changing rules or embedding public packages.
3. Use `development.md` when moving to the complete reference runtime and debugger.

## By Role

### Rule Author

Read in this order:

1. `getting-started.md`
2. `rule-engine.md`
3. `configuration.md`
4. `../examples/README.md`

Focus on message fields, condition semantics, action parameters, priority, and provider composition. Rule changes require a process restart in the current YAML provider.

### Framework Integrator

Read in this order:

1. `getting-started.md`
2. `cookbook.md`
3. `executors.md`
4. `rule-engine.md`
5. `concurrency.md`

Use only packages under `pkg/`. Own transport decoding, configuration, dependencies, delivery policy, retries, and observability in the integrating application.

### Reference Runtime Maintainer

Read in this order:

1. `development.md`
2. `configuration.md`
3. `adapters.md`
4. `superpowers/specs/2026-06-20-go-service-framework-design.md`

The reference runtime lives under `cmd/` and `internal/`. The design specification explains architectural intent; current code, tests, and user-facing configuration documents define current behavior.

## Document Index

| Document | Use It For |
| --- | --- |
| `getting-started.md` | Guided first run, first rule change, source reading order |
| `cookbook.md` | Copyable embedding, extension, testing, and production recipes |
| `development.md` | Complete local workflow, Delve, VS Code, troubleshooting, six scenario guidebooks |
| `configuration.md` | Runtime fields, profiles, environment overrides, strict validation |
| `rule-engine.md` | Messages, matching, provider composition, execution plans |
| `executors.md` | Built-in log and HTTP actions, custom executor contract |
| `concurrency.md` | Worker-pool contract and bounded concurrency decisions |
| `adapters.md` | Optional demo integrations and fake-server endpoints |

## Runnable Material

The `../examples/` directory is executable documentation:

- `basic-rule-pipeline`: smallest in-process example
- `http-event-gateway`: custom HTTP input with selectable YAML rules
- `custom-executor`: application-owned executor
- `websocket-to-log`: WebSocket input with built-in executors
- `websocket-to-mattermost`: WebSocket input with a Mattermost-like HTTP action
- `rules/`: incident, release, support, and webhook-chain scenarios

Run all commands from the repository root unless a document explicitly says otherwise.

## Verification

Before relying on a documentation example after changing framework APIs, run:

```bash
make verify
make test-race
make coverage
```

`make verify` tests, vets, and builds both Go modules. `make coverage` enforces at least 90% statement coverage for every package that has test files.
