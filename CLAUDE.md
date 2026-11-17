# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

This repository contains **two independent Go 1.26 modules**: the root module (`github.com/elvinyao/go-service-platform`) and `fake-server/` (`module fake-server`). Root `./...` commands do not cover `fake-server/`.

When containerized execution is required, prefix commands with `bash .agent/run.sh` (runs inside `golang:1.26-bookworm` mounted at `/workspace`).

### Build, Vet, and Test

```bash
# Full verification: tests, go vet, and builds across both Go modules
make verify

# Run unit tests
make test          # root module: go test ./...
make test-fake     # fake-server module: cd fake-server && go test ./...

# Run a single test
go test -v ./pkg/ruleengine -run '^TestMatchRules$'
go test -C fake-server -v ./internal/fakeapi -run '^TestFakeWebSocketServer$'

# Run with the race detector across both modules (required when touching concurrency/lifecycle/HTTP/WS)
make test-race

# Enforce statement coverage gate across both modules (>= 90% per package with test files; override via COVERAGE_MIN=95)
make coverage

# Static analysis (go vet is the repository's standard check)
make vet
make vet-fake

# Build binaries into ./bin/go-service-platform and ./bin/fake-server
make build
make build-fake
```

### Running Locally

- **Full demo mode** (`yaml` + `confluence` providers; `log`, `http`, `mattermost`, and `db` executors against `fake-server`):
  ```bash
  make dev
  ```
- **YAML-first mode** (no business-specific demo adapters; only `yaml` provider + `log`/`http` executors):
  ```bash
  make fake-server   # Terminal 1: listens on :8090 (Confluence), :8091/:8092 (Mattermost), :8093 (WebSocket)
  make run           # Terminal 2: admin API on 127.0.0.1:18080
  ```
- **Containerized full demo**:
  ```bash
  docker compose up --build          # Override host admin port with ADMIN_PORT=8080
  ```
- **Send a sample event and inspect state**:
  ```bash
  curl -s -X POST http://localhost:8093/api/send \
    -H 'Content-Type: application/json' \
    -d '{"id":"m1","type":"AAA","content":"hello","user_id":"u1"}'
  curl -s http://localhost:18080/health/readiness
  curl -s http://localhost:18080/rule-engine
  ```

## Architecture

### SDK vs. Reference Runtime Boundary

- **`pkg/` (Public Framework SDK)**: Domain-agnostic packages (`pkg/ruleengine`, `pkg/executor`, `pkg/pipeline`, `pkg/config`, `pkg/runtime`, `pkg/health`, `pkg/logger`, `pkg/errors`, `pkg/concurrency`, `pkg/context`). Must never depend on `internal/` or demo integrations. Programs under `examples/` import only `pkg/` packages and exclude tests so `make coverage` skips them.
- **`cmd/go-service-platform/` & `internal/` (Reference Application)**: Wires `pkg/` into a runnable service runtime (`internal/app`, `internal/workflow`, `internal/manager`, `internal/service`) alongside optional demo adapters (`internal/adapters/{confluence,mattermost,badgedb}`).
- **`fake-server/`**: Standalone local server emulating Confluence (`:8090`), Mattermost HTTP/WS (`:8091`/`:8092`), and an HTTP-to-WebSocket event broadcaster (`:8093`) for local runs and integration tests.

### Event Processing & Rule Composition

In the reference runtime, events flow through:

```text
service.WebSocketService -> app.setupMessageListeners -> workflow.WorkflowEngine.ProcessMessage
  -> pipeline.Engine.Process -> ruleengine.Composer.BuildExecutionPlan -> executor.Registry -> Executor.Execute
```

Key engine invariants:
- **Provider composition modes** (`pkg/ruleengine/composer.go`):
  - `single`: evaluates only the first provider in `providers`.
  - `or`: combines matched rules across all configured providers.
  - `and` / `pipeline`: **every** configured provider must match at least one rule or the resulting `ExecutionPlan` has zero actions (`pipeline` uses `pipeline_order` when non-empty).
- **Deterministic ordering & deduplication**: Matched rules sort by ascending `Rule.Priority` (lower runs first). Actions are collected in rule order, optionally deduplicated (by `Action.ID`, or `Executor|JSON(Params)` when `ID` is empty), and sorted by ascending `Action.Priority`.
- **Snapshot isolation**: `Composer` deep-clones configs, provider `RuleSet` snapshots, and actions so concurrent evaluation or callers cannot mutate provider state.
- **Timeouts and retries**: `pipeline.Engine.executeAction` runs each action under the policy from `EngineConfig.ResolveExecution` (engine `execution` block, overridden per action by `timeout`/`retry`). Defaults are no timeout and one attempt. Errors wrapped with `executor.Permanent`, executor panics, and caller cancellation are never retried.
- **Partial failure semantics**: `pipeline.Engine.Process` executes **all** planned actions even if earlier actions fail, returning both the `ExecutionPlan` and an aggregated `*pipeline.ExecutionError` (which `WorkflowEngine` wraps as `errors.TypePartialFailure` without crashing the runtime).

### Configuration & Startup Preflight

Configuration is split across three files (selected via `RUNTIME_CONFIG`, `RULE_ENGINE_CONFIG`, and `WORKFLOW_RULES`):
- `config/runtime.yaml` (or `config/profiles/demo/runtime.yaml`): admin server timeouts/address, WebSocket input, and adapter enablement. (`config/config.yaml` is an unused migration stub.)
- `config/rule-engine.yaml` (or `config/profiles/demo/rule-engine.yaml`): workflow policies, provider lists, composition `mode`, and `action_merge`.
- `config/workflow-rules.yaml` (or `config/profiles/demo/workflow-rules.yaml`, which adds a `BADGE` rule for the `db` executor): local rule definitions (comparison operators plus `any`/`all`/`not` groups over fixed fields and `payload.<path>`/`metadata.<path>`, validated by `validateCondition` in `pkg/ruleengine/validation.go`; Go `text/template` rendering via `executor.RenderTemplate` in `log` and `http` executors).

Validation is fail-fast and strict:
- YAML loaders enforce `KnownFields(true)` and reject multi-document YAML, invalid URLs/durations, and duplicate IDs. Do not relax config validation to work around profile mismatches.
- `WorkflowEngine` filters out disabled optional providers from `EngineConfig` at startup, and requires the `mattermost` adapter to be enabled whenever the `confluence` provider is active.
- During `pipeline.Engine.Start`, providers start first, then `validateProviderExecutors` inspects every **enabled** rule across provider snapshots and fails startup if any action references an unregistered executor.
- Engine config and YAML rules are read at startup; changing YAML files requires restarting the process.

### Lifecycle Rollback & Health Semantics

- **Transactional startup**: `app.App.Start`, `manager.ServiceManager.StartAll`, and `pipeline.Engine.Start` all roll back already-started services, lifecycle-aware executors (`Starter`/`Stopper`), and providers in reverse/cleanup order if any step fails during startup (including admin port bind failures in `pkg/runtime/admin_server.go`).
- **Health, Readiness, and Liveness** (`internal/app/admin.go`, `pkg/health`):
  - Critical check failures return HTTP `503` on `/health` and `/health/readiness` (`NOT_READY`). Every registered service gets a critical running check, and an enabled `WebSocketService` reports critical failure whenever it has zero active connections (even while its background reconnect loop is running). Warning-level checks stay `200` / `READY`.
  - Calling `App.Stop` immediately marks `HealthManager` as shutting down (`NOT_READY`) before draining listeners and services.
  - `/health/liveness` reports `200 UP` whenever the process and admin server can respond, independent of external dependency health.
