# Development Guidebook

This guidebook is for new developers who want to run, debug, read, and extend the Go Service Framework locally.

The most important local rule is simple: the admin API address comes from `config/runtime.yaml`, unless `ADMIN_ADDR` is set. In this working tree the local admin address may be `:18080`; if your file says `:8080`, use port `8080` in the curl examples below.

## What This Project Is

The repository contains a configurable, rule-driven service runtime:

```text
event input
-> service adapter
-> workflow manager
-> workflow engine
-> rule composer
-> executor registry
-> action executor
```

The reference application is intentionally small but complete. It receives messages from a WebSocket input, matches rules from configured providers, and executes actions such as logging, HTTP calls, demo database writes, or demo Mattermost notifications.

The framework-oriented packages live under `pkg/`. The runnable reference application and demo integrations live under `cmd/`, `internal/`, and `fake-server/`.

## Mental Model

Read the runtime as four layers:

| Layer | Main Files | Responsibility |
| --- | --- | --- |
| Runtime | `cmd/service-workflow/main.go`, `internal/app/app.go` | Load config, start services, wire managers, expose admin API |
| Services | `internal/service/*` | Connect to inputs and demo dependencies |
| Rules | `pkg/ruleengine/*`, `internal/workflow/workflow_engine.go` | Build execution plans from rule providers |
| Actions | `pkg/executor/*`, `internal/adapters/*` | Execute selected actions |

The central file for learning is `internal/workflow/workflow_engine.go`. From there you can step upward to see where messages come from, or downward to see how rules and executors work.

## Startup Flow

The main entry point is `cmd/service-workflow/main.go`.

Startup does this:

1. Load runtime config from `RUNTIME_CONFIG` or `config/runtime.yaml`.
2. Load rule engine config from `RULE_ENGINE_CONFIG` or `config/rule-engine.yaml`.
3. Load local YAML rules from `WORKFLOW_RULES` or `config/workflow-rules.yaml`.
4. Create `internal/app.App`.
5. Register and start services through `pkg/di` and `internal/manager`.
6. Create `internal/workflow.WorkflowEngine`.
7. Bind WebSocket messages to `WorkflowManager.DispatchMessage`.
8. Start the admin HTTP server.

The default message path is:

```text
fake WebSocket message
-> internal/service.WebSocketService
-> internal/app.setupMessageListeners
-> internal/manager.WorkflowManager.DispatchMessage
-> internal/workflow.WorkflowEngine.ProcessMessage
-> pkg/ruleengine.Composer.BuildExecutionPlan
-> pkg/executor.Registry
-> executor.Execute
```

## Prerequisites

Install:

- Go 1.26 or the Go version used by `go.mod`
- `make`
- optional: Docker and Docker Compose
- optional: Delve for step debugging
- optional: VS Code with the Go extension

Install Delve if needed:

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
```

## First Run

From the repository root:

```bash
make test
make test-fake
make build
```

Start the fake APIs:

```bash
make fake-server
```

In another terminal, start the reference runtime:

```bash
make run
```

Verify the admin API. Use the port from `config/runtime.yaml`; these examples use `18080`:

```bash
curl -s http://localhost:18080/health
curl -s http://localhost:18080/health/readiness
curl -s http://localhost:18080/health/liveness
curl -s http://localhost:18080/services
curl -s http://localhost:18080/workflows
curl -s http://localhost:18080/rule-engine
```

Send a sample message through the fake WebSocket HTTP helper:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m1","type":"AAA","content":"hello debug","user_id":"u1","timestamp":"2026-06-22T00:00:00Z"}'
```

Expected behavior:

- The fake WebSocket server broadcasts the message.
- `WebSocketService` receives it.
- `WorkflowEngine` builds an execution plan.
- Matching actions run through registered executors.
- Runtime logs show matched rules and executed actions.

## One-Terminal Development

You can run fake APIs and the runtime together:

```bash
make dev
```

This is convenient for quick smoke testing. For debugging, use two terminals so you can restart the runtime without restarting fake dependencies.

## Docker Compose

```bash
docker compose up --build
```

Verify:

```bash
curl -s http://localhost:18080/health
curl -s http://localhost:18080/rule-engine
```

Compose publishes the admin API on host port `18080` by default to avoid local `8080` conflicts. Override it with:

```bash
ADMIN_PORT=8080 docker compose up --build
```

Stop:

```bash
docker compose down
```

## Debugging With Delve

Start fake APIs first:

```bash
make fake-server
```

Run the runtime under Delve:

```bash
dlv debug ./cmd/service-workflow
```

If you need a different admin address:

```bash
ADMIN_ADDR=:18081 dlv debug ./cmd/service-workflow
```

Useful breakpoints:

| Goal | Breakpoint |
| --- | --- |
| Understand config loading | `cmd/service-workflow/main.go:run` |
| Understand app lifecycle | `internal/app/app.go:Start` |
| Understand listener wiring | `internal/app/app.go:setupMessageListeners` |
| See message dispatch | `internal/manager/workflow_manager.go:DispatchMessage` |
| See rule matching and execution | `internal/workflow/workflow_engine.go:ProcessMessage` |
| See provider composition | `pkg/ruleengine/composer.go:BuildExecutionPlan` |
| See condition matching | `pkg/ruleengine/matcher.go:MatchRules` |
| See log actions | `pkg/executor/log_executor.go:Execute` |
| See HTTP actions | `pkg/executor/http_executor.go:Execute` |
| See admin endpoints | `internal/app/admin.go:newAdminMux` |

## VS Code Debugging

Create `.vscode/launch.json` if you want editor debugging:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug service-workflow",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/cmd/service-workflow",
      "env": {
        "RUNTIME_CONFIG": "config/runtime.yaml",
        "RULE_ENGINE_CONFIG": "config/rule-engine.yaml",
        "WORKFLOW_RULES": "config/workflow-rules.yaml",
        "ADMIN_ADDR": ":18080"
      }
    },
    {
      "name": "Debug fake-server",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/fake-server/cmd/fake-server",
      "cwd": "${workspaceFolder}/fake-server"
    }
  ]
}
```

Recommended order:

1. Start `Debug fake-server`.
2. Start `Debug service-workflow`.
3. Send a message with `/api/send`.
4. Step through `WorkflowEngine.ProcessMessage`.

## Learning Path

### Stage 1: Run It

Goal: understand the moving parts without editing code.

1. Run `make fake-server`.
2. Run `make run`.
3. Open `/health`, `/services`, `/workflows`, and `/rule-engine`.
4. Send one `AAA` message through `http://localhost:8093/api/send`.

Read:

- `cmd/service-workflow/main.go`
- `internal/app/app.go`
- `internal/app/admin.go`

### Stage 2: Read the Message Flow

Goal: understand how an event becomes an execution plan.

Read in this order:

1. `internal/service/websocket_service.go`
2. `internal/app/app.go`
3. `internal/manager/workflow_manager.go`
4. `internal/workflow/workflow_engine.go`
5. `pkg/ruleengine/composer.go`
6. `pkg/executor/executor.go`

### Stage 3: Change a Rule

Goal: understand YAML rules and condition matching.

Edit `config/workflow-rules.yaml` and change a condition:

```yaml
conditions:
  - field: content
    op: contains
    value: urgent
```

Send:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m2","type":"AAA","content":"urgent deploy","user_id":"u1"}'
```

Then send:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m3","type":"AAA","content":"normal deploy","user_id":"u1"}'
```

Compare the logs and step through `pkg/ruleengine/matcher.go`.

### Stage 4: Change Provider Composition

Goal: understand how multiple providers combine.

Open `config/rule-engine.yaml`.

Try:

```yaml
mode: or
```

Then try:

```yaml
mode: single
```

Call:

```bash
curl -s http://localhost:18080/rule-engine
```

Watch how provider snapshots and selected rules change.

### Stage 5: Add an Action

Goal: understand executors and action params.

Add an HTTP action to a rule in `config/workflow-rules.yaml`:

```yaml
      - id: http_debug
        executor: http
        priority: 200
        params:
          method: POST
          url: "http://localhost:8093/api/send"
          timeout_ms: 3000
          body_template: '{"id":"echo-{{.ID}}","type":"DEBUG","content":"echo {{.Content}}","user_id":"system"}'
```

Read:

- `pkg/executor/http_executor.go`
- `pkg/executor/template.go`
- `pkg/executor/executor.go`

### Stage 6: Add a New Executor

Goal: learn the framework extension model.

Create an executor that implements:

```go
type Executor interface {
    Type() string
    Execute(context.Context, ruleengine.Message, ruleengine.Action) error
}
```

Then register it in `internal/workflow/workflow_engine.go` next to the built-in and demo executors.

Minimum checklist:

- Return a stable `Type()`.
- Validate required action params.
- Render templates with `executor.RenderTemplate` when useful.
- Return errors instead of panicking.
- Add package-level tests for success and failure paths.

## Scenario Guidebooks

These scenarios show what the platform is especially good at: local-first event automation where rules change more often than code. Each scenario follows the same development loop:

1. Define the incoming event contract.
2. Add or adjust YAML rules.
3. Run fake dependencies locally.
4. Send sample events through `/api/send`.
5. Inspect `/rule-engine`, logs, and health endpoints.
6. Step through the rule engine and executor path.

The examples below are intentionally written as learning scenarios. They use the current local runtime, fake WebSocket input, YAML rules, log executor, HTTP executor, Mattermost demo executor, and BadgeDB demo executor.

The default `config/rule-engine.yaml` is YAML-first, so these scenarios work by adding YAML rules and restarting the runtime. To experiment with the Confluence demo provider, enable `adapters.confluence.enabled` in `config/runtime.yaml`, add `confluence` to the provider list, and use `mode: or` while learning.

### Scenario 1: Incident Alert Router

#### What It Demonstrates

This scenario routes operational alerts by severity, service, and environment. It is a strong fit for this platform because alert routing rules change frequently, while the runtime should stay stable.

You learn:

- metadata-based rule matching
- multiple actions for one matched event
- notification-style executors
- partial failure behavior when one action fails

#### Event Contract

```json
{
  "id": "alert-001",
  "type": "ALERT",
  "content": "payment api latency p95 > 2s",
  "user_id": "datadog",
  "metadata": {
    "severity": "critical",
    "service": "payment-api",
    "env": "prod",
    "team": "payments"
  }
}
```

Required fields:

- `type`: use `ALERT`.
- `content`: human-readable alert text.
- `metadata.severity`: `critical`, `warning`, or `info`.
- `metadata.service`: service name.
- `metadata.env`: `prod`, `staging`, or `dev`.

#### Example Rule

Add a rule to `config/workflow-rules.yaml`:

```yaml
  - id: alert_prod_critical
    workflow: WorkflowEngine
    enabled: true
    priority: 10
    conditions:
      - field: type
        op: eq
        value: ALERT
      - field: metadata.severity
        op: eq
        value: critical
      - field: metadata.env
        op: eq
        value: prod
    actions:
      - id: log_critical_alert
        executor: log
        priority: 100
        params:
          level: error
          template: "Critical prod alert service={{index .Metadata \"service\"}} content={{.Content}}"
      - id: notify_alert_channel
        executor: mattermost
        priority: 200
        params:
          channel_id: test-channel-1
          template: "CRITICAL {{index .Metadata \"service\"}}: {{.Content}}"
```

#### Local Exercise

Start fake APIs and the runtime:

```bash
make fake-server
make run
```

Send a critical alert:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"alert-001","type":"ALERT","content":"payment api latency p95 > 2s","user_id":"datadog","metadata":{"severity":"critical","service":"payment-api","env":"prod","team":"payments"}}'
```

Inspect:

```bash
curl -s http://localhost:18080/rule-engine
curl -s http://localhost:18080/services
```

Expected result:

- The rule matches only critical prod alerts.
- The log executor writes an error-level message.
- The Mattermost demo executor posts to the fake Mattermost API.

#### Debug Path

Set breakpoints in this order:

1. `internal/service/websocket_service.go:ProcessIncomingMessage`
2. `internal/manager/workflow_manager.go:DispatchMessage`
3. `internal/workflow/workflow_engine.go:ProcessMessage`
4. `pkg/ruleengine/matcher.go:MatchRules`
5. `internal/adapters/mattermost/executor.go:Execute`

#### Extension Ideas

- Add a warning alert rule with lower priority.
- Add a `metadata.team` route.
- Add an HTTP action that calls an incident management webhook.
- Add a dedup key in metadata and create a future dedup executor.

#### Common Failure Modes

- The message does not include `metadata.severity`.
- The rule uses `metadata.service.name` but the event sends `metadata.service`.
- The Mattermost service is not running or the fake server is not started.
- `mode: pipeline` requires all configured providers to match; use `or` while learning if you combine YAML and Confluence provider rules.

### Scenario 2: Release Gatekeeper

#### What It Demonstrates

This scenario handles deployment events and turns rules into lightweight release policy. It is a good fit because release rules are operational policy: they should be visible, reviewable, and testable without recompiling code.

You learn:

- policy-style conditions
- environment-specific actions
- HTTP executor usage
- startup and admin verification before sending production-like events

#### Event Contract

```json
{
  "id": "deploy-001",
  "type": "DEPLOY_REQUEST",
  "content": "deploy checkout-api to prod",
  "user_id": "github-actions",
  "metadata": {
    "service": "checkout-api",
    "env": "prod",
    "risk": "high",
    "branch": "main",
    "sha": "abc123"
  }
}
```

Required fields:

- `type`: use `DEPLOY_REQUEST`.
- `metadata.env`: target environment.
- `metadata.risk`: `low`, `medium`, or `high`.
- `metadata.branch`: source branch.
- `metadata.service`: deployed service.

#### Example Rule

```yaml
  - id: deployment_high_risk_prod
    workflow: WorkflowEngine
    enabled: true
    priority: 20
    conditions:
      - field: type
        op: eq
        value: DEPLOY_REQUEST
      - field: metadata.env
        op: eq
        value: prod
      - field: metadata.risk
        op: eq
        value: high
    actions:
      - id: log_deploy_gate
        executor: log
        priority: 100
        params:
          level: warn
          template: "High-risk prod deploy requested service={{index .Metadata \"service\"}} branch={{index .Metadata \"branch\"}} sha={{index .Metadata \"sha\"}}"
      - id: call_approval_webhook
        executor: http
        priority: 200
        params:
          method: POST
          url: "http://localhost:8093/api/send"
          timeout_ms: 3000
          headers:
            X-Source: go-service-framework
          body_template: '{"id":"approval-{{.ID}}","type":"APPROVAL_REQUIRED","content":"approval required for {{index .Metadata \"service\"}}","user_id":"release-gatekeeper"}'
```

#### Local Exercise

Send a high-risk production deploy:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"deploy-001","type":"DEPLOY_REQUEST","content":"deploy checkout-api to prod","user_id":"github-actions","metadata":{"service":"checkout-api","env":"prod","risk":"high","branch":"main","sha":"abc123"}}'
```

Then inspect messages received by the fake WebSocket server:

```bash
curl -s http://localhost:8093/api/messages
```

Expected result:

- The deploy request matches.
- The HTTP executor sends a synthetic `APPROVAL_REQUIRED` event back to the fake WebSocket server.
- You can see the generated approval event in `/api/messages`.

#### Debug Path

Set breakpoints in:

1. `pkg/ruleengine/composer.go:BuildExecutionPlan`
2. `pkg/ruleengine/matcher.go:matchCondition`
3. `pkg/executor/http_executor.go:Execute`
4. `pkg/executor/template.go:RenderTemplate`

#### Extension Ideas

- Add a rule that allows low-risk staging deploys.
- Add a branch policy: `metadata.branch == main` for prod.
- Add a custom executor that writes approval records to a real system.
- Add a rule that logs deploy success and failure events separately.

#### Common Failure Modes

- The HTTP executor body template is invalid JSON after rendering.
- The target endpoint returns non-2xx and the action is reported as failed.
- The event uses `metadata.environment` but the rule expects `metadata.env`.
- The rule priority is higher than expected and runs after another action.

### Scenario 3: Customer Support Ticket Triage

#### What It Demonstrates

This scenario routes support tickets by customer tier, product area, and keywords. It is a good fit because support triage changes as teams and products change.

You learn:

- `contains` and `regex` matching
- routing by metadata
- notification templates
- using fake services to simulate a SaaS webhook

#### Event Contract

```json
{
  "id": "ticket-123",
  "type": "SUPPORT_TICKET",
  "content": "Enterprise customer cannot login after SSO change",
  "user_id": "zendesk",
  "metadata": {
    "tier": "enterprise",
    "product": "auth",
    "region": "jp",
    "priority": "urgent"
  }
}
```

Required fields:

- `type`: use `SUPPORT_TICKET`.
- `content`: ticket summary.
- `metadata.tier`: customer tier.
- `metadata.product`: affected product.
- `metadata.region`: support region.

#### Example Rule

```yaml
  - id: support_enterprise_auth
    workflow: WorkflowEngine
    enabled: true
    priority: 30
    conditions:
      - field: type
        op: eq
        value: SUPPORT_TICKET
      - field: metadata.tier
        op: eq
        value: enterprise
      - field: content
        op: regex
        value: "(?i)(sso|login|auth)"
    actions:
      - id: log_support_triage
        executor: log
        priority: 100
        params:
          level: info
          template: "Enterprise auth ticket region={{index .Metadata \"region\"}} content={{.Content}}"
      - id: notify_support
        executor: mattermost
        priority: 200
        params:
          channel_id: test-channel-1
          template: "Support ticket {{.ID}} tier={{index .Metadata \"tier\"}} product={{index .Metadata \"product\"}}: {{.Content}}"
```

#### Local Exercise

Send a ticket that should match:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"ticket-123","type":"SUPPORT_TICKET","content":"Enterprise customer cannot login after SSO change","user_id":"zendesk","metadata":{"tier":"enterprise","product":"auth","region":"jp","priority":"urgent"}}'
```

Send a ticket that should not match:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"ticket-124","type":"SUPPORT_TICKET","content":"How do I update my profile picture?","user_id":"zendesk","metadata":{"tier":"free","product":"profile","region":"us","priority":"normal"}}'
```

Expected result:

- The enterprise auth ticket triggers actions.
- The free-tier profile ticket does not match the rule.

#### Debug Path

Set breakpoints in:

1. `pkg/ruleengine/matcher.go:getFieldValue`
2. `pkg/ruleengine/matcher.go:matchCondition`
3. `internal/adapters/mattermost/executor.go:Execute`

#### Extension Ideas

- Add separate rules for `region == jp` and `region == us`.
- Add a custom executor that creates an internal escalation record.
- Add a rule for high-priority free-tier tickets that only logs.
- Add a metadata normalization step in a future input adapter.

#### Common Failure Modes

- Regex is case-sensitive unless you use `(?i)`.
- A ticket can fail to match when metadata keys are missing.
- Broad regex patterns can route too many tickets.
- `pipeline` mode can suppress YAML matches if another provider has no matching rule.

### Scenario 4: Webhook Automation Hub

#### What It Demonstrates

This scenario turns the platform into a generic webhook automation gateway. It receives events from many systems, normalizes them into the framework message shape, and dispatches actions by rules.

You learn:

- generic event design
- source-based routing
- safe fan-out to multiple actions
- how a custom input adapter could fit later

#### Event Contract

```json
{
  "id": "webhook-001",
  "type": "WEBHOOK_EVENT",
  "content": "github pull request opened",
  "user_id": "github",
  "metadata": {
    "source": "github",
    "event": "pull_request",
    "repository": "platform/api",
    "action": "opened"
  }
}
```

Required fields:

- `type`: use `WEBHOOK_EVENT`.
- `metadata.source`: external system name.
- `metadata.event`: external event type.
- `metadata.action`: event action.

#### Example Rule

```yaml
  - id: webhook_github_pr_opened
    workflow: WorkflowEngine
    enabled: true
    priority: 40
    conditions:
      - field: type
        op: eq
        value: WEBHOOK_EVENT
      - field: metadata.source
        op: eq
        value: github
      - field: metadata.event
        op: eq
        value: pull_request
      - field: metadata.action
        op: eq
        value: opened
    actions:
      - id: log_github_pr
        executor: log
        priority: 100
        params:
          level: info
          template: "GitHub PR opened repo={{index .Metadata \"repository\"}} id={{.ID}}"
      - id: fanout_pr_event
        executor: http
        priority: 200
        params:
          method: POST
          url: "http://localhost:8093/api/send"
          timeout_ms: 3000
          body_template: '{"id":"fanout-{{.ID}}","type":"INTERNAL_REVIEW_EVENT","content":"review needed for {{index .Metadata \"repository\"}}","user_id":"webhook-hub"}'
```

#### Local Exercise

Send a GitHub-like webhook:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"webhook-001","type":"WEBHOOK_EVENT","content":"github pull request opened","user_id":"github","metadata":{"source":"github","event":"pull_request","repository":"platform/api","action":"opened"}}'
```

Inspect fan-out:

```bash
curl -s http://localhost:8093/api/messages
```

Expected result:

- The original webhook matches.
- The HTTP action creates a derived internal event.
- The fake WebSocket server records the derived event.

#### Debug Path

Set breakpoints in:

1. `internal/service/websocket_service.go:connectLoop`
2. `internal/workflow/workflow_engine.go:ProcessMessage`
3. `pkg/executor/http_executor.go:Execute`

#### Extension Ideas

- Add a custom HTTP input adapter that receives raw webhooks directly.
- Add signature verification before dispatching external events.
- Add source-specific normalizers for GitHub, Stripe, Jira, and Datadog.
- Add retry or dead-letter behavior for failed HTTP actions.

#### Common Failure Modes

- External systems use different field names; normalize before matching.
- Fan-out rules can accidentally create loops if generated events match the same rule.
- Missing source metadata makes routing ambiguous.
- Non-2xx webhook targets correctly fail the HTTP action.

### Scenario 5: Compliance Audit Event Processor

#### What It Demonstrates

This scenario tracks sensitive system events and writes audit-like records. It is a good fit because audit policies should be explicit, testable, and easy to review.

You learn:

- matching high-risk operations
- using the BadgeDB demo executor
- building an audit trail style action
- using health and service metrics to confirm stateful behavior

#### Event Contract

```json
{
  "id": "audit-001",
  "type": "USER_PERMISSION_CHANGED",
  "content": "admin role granted to user u123",
  "user_id": "iam-service",
  "metadata": {
    "actor": "ops-user",
    "target": "u123",
    "role": "admin",
    "env": "prod"
  }
}
```

Required fields:

- `type`: security-sensitive event type.
- `content`: human-readable audit message.
- `metadata.actor`: initiator.
- `metadata.target`: affected identity or resource.
- `metadata.role`: role or permission.
- `metadata.env`: environment.

#### Example Rule

```yaml
  - id: audit_prod_admin_grant
    workflow: WorkflowEngine
    enabled: true
    priority: 50
    conditions:
      - field: type
        op: eq
        value: USER_PERMISSION_CHANGED
      - field: metadata.role
        op: eq
        value: admin
      - field: metadata.env
        op: eq
        value: prod
    actions:
      - id: log_admin_grant
        executor: log
        priority: 100
        params:
          level: warn
          template: "Admin grant actor={{index .Metadata \"actor\"}} target={{index .Metadata \"target\"}} content={{.Content}}"
      - id: save_audit_badge
        executor: db
        priority: 200
        params:
          name: audit-record
          description: "prod admin permission change"
          badge_type: compliance_audit
```

#### Local Exercise

Send an audit event:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"audit-001","type":"USER_PERMISSION_CHANGED","content":"admin role granted to user u123","user_id":"iam-service","metadata":{"actor":"ops-user","target":"u123","role":"admin","env":"prod"}}'
```

Inspect services:

```bash
curl -s http://localhost:18080/services
```

Expected result:

- The event matches the audit rule.
- The log action records the sensitive operation.
- The `db` action writes a demo BadgeDB record.
- `BadgeDBService` metrics show increased `db_entries`.

#### Debug Path

Set breakpoints in:

1. `internal/adapters/badgedb/executor.go:Execute`
2. `internal/service/badgedb_service.go:SaveBadge`
3. `internal/service/badgedb_service.go:GetMetrics`

#### Extension Ideas

- Replace BadgeDB with a durable audit store executor.
- Add an allowlist rule for trusted automation users.
- Add high-risk regex matching for `delete`, `drop`, `grant`, and `admin`.
- Add immutable event IDs and deduplication.

#### Common Failure Modes

- The BadgeDB service is not running.
- `metadata.role` is missing or uses a different value such as `administrator`.
- Stateful tests need to account for async backup timing.
- Audit rules should avoid broad matches that create noisy records.

### Scenario 6: Local Learning Sandbox

#### What It Demonstrates

This scenario treats the repository as a backend engineering training environment. It is not about one business domain; it is about learning service lifecycle, observability, configuration, rules, concurrency, and integration testing.

You learn:

- how the runtime starts and stops
- how health checks are composed
- how fake dependencies support local development
- how to write tests for rules and services
- how to use race tests to find concurrency bugs

#### Event Contract

Use a simple learning event:

```json
{
  "id": "learn-001",
  "type": "LEARNING_EVENT",
  "content": "trace this event end to end",
  "user_id": "student",
  "metadata": {
    "lesson": "rule-engine",
    "step": "first-trace"
  }
}
```

#### Example Rule

```yaml
  - id: learning_trace
    workflow: WorkflowEngine
    enabled: true
    priority: 900
    conditions:
      - field: type
        op: eq
        value: LEARNING_EVENT
    actions:
      - id: log_learning_event
        executor: log
        priority: 100
        params:
          level: debug
          template: "Learning event lesson={{index .Metadata \"lesson\"}} step={{index .Metadata \"step\"}} content={{.Content}}"
```

#### Local Exercise

Run with debug logging:

```bash
LOG_LEVEL=debug make run
```

Send the learning event:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"learn-001","type":"LEARNING_EVENT","content":"trace this event end to end","user_id":"student","metadata":{"lesson":"rule-engine","step":"first-trace"}}'
```

Expected result:

- The message travels through the same production-like path as the other scenarios.
- Debug logs make each layer easier to observe.
- You can add breakpoints without changing business code.

#### Debug Path

Step through:

1. `cmd/service-workflow/main.go:run`
2. `internal/app/app.go:Start`
3. `internal/service/websocket_service.go:ProcessIncomingMessage`
4. `internal/workflow/workflow_engine.go:ProcessMessage`
5. `pkg/ruleengine/composer.go:BuildExecutionPlan`
6. `pkg/executor/log_executor.go:Execute`

#### Extension Ideas

- Write a new unit test for one matcher branch.
- Write an integration test that sends a fake WebSocket event.
- Add a new executor with one success test and three failure tests.
- Run `go test -race ./...` before and after changing goroutine code.

#### Common Failure Modes

- Debug logs are hidden because `LOG_LEVEL` is not set to `debug`.
- Breakpoints are set in code paths that are not reached by the current rule.
- Config changes are not picked up because the process was not restarted.
- Race tests fail when tests leave background goroutines running.

## Use Cases

### Use Case: Inspect Runtime Health

```bash
curl -s http://localhost:18080/health
curl -s http://localhost:18080/health/readiness
curl -s http://localhost:18080/health/liveness
curl -s http://localhost:18080/services
```

Read:

- `pkg/health/manager.go`
- `pkg/health/http.go`
- `internal/manager/service_manager.go`
- `internal/app/admin.go`

### Use Case: Debug a Port Conflict

Run two instances on the same admin address:

```bash
ADMIN_ADDR=:18080 make run
```

In another terminal:

```bash
ADMIN_ADDR=:18080 make run
```

The second instance should fail fast. Read:

- `pkg/runtime/admin_server.go`
- `internal/app/app.go:Start`
- `internal/app/app.go:Stop`

### Use Case: Debug WebSocket Reconnects

Start the runtime before the fake server:

```bash
make run
```

Then start the fake server:

```bash
make fake-server
```

Watch the runtime retry until the WebSocket server is available.

Read:

- `internal/service/websocket_service.go`
- `internal/service/websocket_service_test.go`

### Use Case: Debug Confluence-Backed Rules

Start fake APIs and inspect the fake Confluence page:

```bash
curl -s http://localhost:8090/rest/api/content/settings-page-1
```

Then inspect composed providers:

```bash
curl -s http://localhost:18080/rule-engine
```

Read:

- `internal/adapters/confluence/provider.go`
- `internal/service/confluence_settings_service.go`
- `fake-server/internal/fakeapi/fake_confluence.go`

### Use Case: Debug Mattermost Actions

Send a matching message and inspect fake Mattermost posts by watching fake-server logs.

Read:

- `internal/adapters/mattermost/executor.go`
- `internal/service/mattermost_service.go`
- `fake-server/internal/fakeapi/fake_mattermost.go`

### Use Case: Debug HTTP Executor Failures

Point an HTTP action at a non-2xx endpoint or an unreachable port. The executor should return an action error and the workflow engine should report partial failure without stopping the runtime.

Read:

- `pkg/executor/http_executor.go`
- `internal/workflow/workflow_engine.go`

## Testing Guide

Run normal tests:

```bash
make test
make test-fake
```

Run race tests:

```bash
go test -race ./...
cd fake-server && go test -race ./...
```

Run coverage:

```bash
go test -cover ./...
cd fake-server && go test -cover ./...
```

Current project expectation: packages with tests should stay at or above 90% statement coverage.

When adding behavior:

1. Add package-level unit tests for local branches.
2. Add integration tests only when behavior crosses service boundaries.
3. Run race tests for code that uses goroutines, channels, locks, WebSockets, or HTTP servers.
4. Prefer testing real code paths with `httptest` instead of mocks when the boundary is HTTP.

## Troubleshooting

### Admin curl returns connection refused

Check the configured admin address:

```bash
cat config/runtime.yaml
```

Or force it:

```bash
ADMIN_ADDR=:18080 make run
```

### `/rule-engine` returns no expected rules

Check:

- `RULE_ENGINE_CONFIG`
- `WORKFLOW_RULES`
- `config/rule-engine.yaml`
- `config/workflow-rules.yaml`
- provider mode: `single`, `or`, `and`, or `pipeline`

### Messages do not trigger actions

Check:

- Is fake-server running?
- Does `/api/send` return success?
- Does the message `type` or `content` match rule conditions?
- Is the rule `enabled: true`?
- Is the action executor registered?

### Mattermost action fails

Check:

- `MATTERMOST_SERVER_URL`
- `MATTERMOST_WS_URL`
- `MATTERMOST_API_TOKEN`
- `adapters.mattermost.channel`
- fake-server logs

### Tests fail because a port is busy

Most tests use dynamic ports. For manual runs, override the admin address:

```bash
ADMIN_ADDR=:18081 make run
```

### Race tests fail

Treat race failures as real bugs. Common causes:

- concurrent WebSocket writes
- unsynchronized service lifecycle state
- reading shared fields without locks
- goroutines that outlive tests

## Where to Go Next

- Configuration details: `docs/configuration.md`
- Rule engine concepts: `docs/rule-engine.md`
- Executor SDK: `docs/executors.md`
- Demo adapters: `docs/adapters.md`
- Fake API server: `fake-server/README.md`
