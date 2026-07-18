# Getting Started

This tutorial is the shortest path from cloning the repository to changing a rule and understanding why it ran. It assumes basic Go knowledge but no previous experience with this project.

## What You Will Learn

By the end of the tutorial, you will be able to:

- run a rule pipeline without external services
- accept events over HTTP and inspect the resulting execution plan
- change behavior in YAML without changing Go code
- identify where to add an input, rule provider, or executor
- choose between the framework examples and the full reference runtime

## Choose a Starting Path

| Goal | Start Here | External Services |
| --- | --- | --- |
| See the smallest pipeline | `examples/basic-rule-pipeline` | None |
| Experiment with YAML rules over HTTP | `examples/http-event-gateway` | None |
| Learn how to add an action type | `examples/custom-executor` | None |
| Study service lifecycle and admin APIs | `cmd/service-workflow` | `fake-server` |
| Exercise all demo adapters | `make dev` | Started by the command |

New users should complete the HTTP event gateway exercise before reading the reference runtime. It exposes the same public pipeline with fewer moving parts.

## Prerequisites

From the repository root, confirm the toolchain and project state:

```bash
go version
make verify
```

The project requires Go 1.26 or later. `make verify` runs tests, static analysis, and build checks for both Go modules. Use the following command when you also want a direct formatting report:

```bash
find . -name '*.go' -type f -exec gofmt -l {} +
```

## Step 1: Run the Smallest Pipeline

Run:

```bash
go run ./examples/basic-rule-pipeline
```

Expected output includes a rendered log message and this summary:

```text
matched_rules=1 executed_actions=1
```

Open `examples/basic-rule-pipeline/main.go` and find these four objects:

1. `StaticProvider` owns one in-memory rule.
2. `EngineConfig` selects that provider for a workflow.
3. `LogExecutor` supplies the action named `log`.
4. `pipeline.Engine` composes the rule and executes its action.

This is the minimum application boundary. Code outside this repository should import packages under `pkg/`, not packages under `internal/`.

## Step 2: Start an HTTP Event Gateway

Run a standalone HTTP input with the incident-routing rule set:

```bash
WORKFLOW_RULES=examples/rules/incident-routing.yaml \
go run ./examples/http-event-gateway
```

The server binds to `127.0.0.1:18081`. In another terminal, check it:

```bash
curl -s http://127.0.0.1:18081/health
curl -s http://127.0.0.1:18081/pipeline
```

Send a matching event:

```bash
curl -s -X POST http://127.0.0.1:18081/events \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "incident-101",
    "type": "INCIDENT",
    "content": "checkout error rate exceeded 10 percent",
    "user_id": "monitoring",
    "metadata": {
      "severity": "critical",
      "service": "checkout"
    }
  }'
```

The response should show two matched rules and two planned actions:

```json
{
  "message_id": "incident-101",
  "workflow": "WorkflowEngine",
  "matched_rules": [
    "route-high-severity-incident",
    "route-checkout-incident"
  ],
  "planned_actions": [
    "log-high-severity-incident",
    "log-checkout-owner"
  ]
}
```

Send a non-matching event and observe that the request still succeeds with empty arrays:

```bash
curl -s -X POST http://127.0.0.1:18081/events \
  -H 'Content-Type: application/json' \
  -d '{"id":"heartbeat-1","type":"HEARTBEAT","content":"ok","user_id":"monitoring"}'
```

No match is a valid business result. A malformed event is an HTTP 400, and an action execution failure is an HTTP 502 in this learning gateway.

## Step 3: Change Behavior Without Go Code

Copy `examples/rules/incident-routing.yaml` to a working file, or edit it directly while experimenting. Change:

```yaml
value: "^(critical|high)$"
```

to:

```yaml
value: "^(critical|high|medium)$"
```

Restart the gateway and send an event whose `metadata.severity` is `medium`. YAML rule files are loaded at startup; the current provider does not hot reload files.

Useful experiments:

- set `enabled: false` and confirm the rule disappears from matching
- change `priority` and inspect action order in the response
- add a `content` condition with `op: contains`
- misspell a field such as `conditions` and observe strict startup validation
- reference an unknown executor and observe pipeline preflight validation

## Step 4: Try the Runnable Rule Sets

Each rule set can be selected with the same command:

```bash
WORKFLOW_RULES=<rule-file> go run ./examples/http-event-gateway
```

| Rule File | Demonstrates |
| --- | --- |
| `examples/rules/incident-routing.yaml` | Nested metadata, regex matching, and multiple matching rules |
| `examples/rules/release-gate.yaml` | Multi-condition release decisions |
| `examples/rules/support-triage.yaml` | Independent rules and action priority |
| `examples/rules/webhook-chain.yaml` | An HTTP action that emits a second local event |

The exact commands and event payloads are in `examples/README.md`.

## Step 5: Add an Executor

Run:

```bash
go run ./examples/custom-executor
```

The example implements the two-method `executor.Executor` interface:

```go
type Executor interface {
    Type() string
    Execute(context.Context, ruleengine.Message, ruleengine.Action) error
}
```

Trace the example in this order:

1. `collectExecutor.Type` defines the stable YAML action name.
2. `collectExecutor.Execute` validates parameters and renders a template.
3. `pipeline.New` registers the application-owned executor.
4. Engine startup verifies that enabled rules reference registered executors.
5. `Engine.Process` invokes the executor for a matching message.

Use a mutex or another synchronization mechanism when an executor owns mutable state. The same executor can be called from multiple input goroutines in a real application.

## Step 6: Run the Reference Runtime

The reference runtime adds WebSocket input, lifecycle management, configuration layering, health checks, and admin diagnostics.

Start fake dependencies:

```bash
make fake-server
```

In another terminal, start the YAML-first runtime:

```bash
make run
```

Inspect it:

```bash
curl -s http://127.0.0.1:18080/health/readiness
curl -s http://127.0.0.1:18080/services
curl -s http://127.0.0.1:18080/workflows
curl -s http://127.0.0.1:18080/rule-engine
```

Send an event through the fake WebSocket broadcaster:

```bash
curl -s -X POST http://127.0.0.1:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"runtime-1","type":"AAA","content":"hello runtime","user_id":"local-user"}'
```

Use `make dev` instead when you want the optional Confluence-, Mattermost-, and BadgeDB-like demo integrations.

## Core Concepts

| Concept | Question It Answers | Main API |
| --- | --- | --- |
| Message | What event entered the system? | `ruleengine.Message` |
| Condition | Does one field match? | `ruleengine.Condition` |
| Rule | Which actions should this event produce? | `ruleengine.Rule` |
| Provider | Where do rules come from? | `ruleengine.RuleProvider` |
| Composer | How are provider results combined? | `ruleengine.Composer` |
| Execution plan | Which rules and actions were selected? | `ruleengine.ExecutionPlan` |
| Executor | How is one action performed? | `executor.Executor` |
| Pipeline | How are composition and execution coordinated? | `pipeline.Engine` |

Conditions within one rule use AND semantics. Multiple matching rules can all contribute actions. The workflow composition mode determines how results from multiple providers are combined.

## Read the Source in This Order

For the public framework:

1. `pkg/ruleengine/types.go`
2. `pkg/ruleengine/matcher.go`
3. `pkg/ruleengine/provider.go`
4. `pkg/ruleengine/composer.go`
5. `pkg/executor/executor.go`
6. `pkg/pipeline/engine.go`

For the reference runtime, continue with:

1. `cmd/service-workflow/main.go`
2. `internal/app/app.go`
3. `internal/di/container.go`
4. `internal/service/websocket_service.go`
5. `internal/manager/workflow_manager.go`
6. `internal/workflow/workflow_engine.go`

## Debugging Checkpoints

Place breakpoints at:

- `pipeline.Engine.Start` to inspect preflight validation
- `ruleengine.Composer.BuildExecutionPlan` to inspect provider results
- `ruleengine.MatchCondition` to inspect actual and expected values
- `pipeline.Engine.Process` to inspect action ordering and failures
- your executor's `Execute` method to inspect rendered parameters

For full Delve and VS Code setup, use `docs/development.md`.

## Important Runtime Boundaries

The framework deliberately does not provide these policies automatically:

- durable queues or event persistence
- action retries or backoff
- idempotency storage
- distributed workflow state
- authentication for the example HTTP gateway
- hot reload for YAML files
- metrics or tracing contracts

Applications should add these at their ownership boundary when required. Bind learning servers to loopback, do not place untrusted values into URLs, validate executor parameters, and make externally visible actions idempotent before using retries.

## Next Steps

- Use `docs/cookbook.md` for copyable implementation recipes.
- Use `docs/development.md` for the complete local debugging guide and six scenario guidebooks.
- Use `docs/rule-engine.md` for matching and composition semantics.
- Use `docs/executors.md` for built-in and custom action behavior.
- Use `docs/configuration.md` for the reference runtime configuration contract.
