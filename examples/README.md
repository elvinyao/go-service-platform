# Runnable Examples

Run commands from the repository root. Every Go program in this directory imports only public framework packages.

| Example | Purpose | Dependencies |
| --- | --- | --- |
| `basic-rule-pipeline` | Smallest static rule pipeline | None |
| `http-event-gateway` | YAML rules behind a local HTTP input | None |
| `custom-executor` | Application-owned action implementation | None |
| `websocket-to-log` | YAML pipeline with WebSocket input | `fake-server` |
| `websocket-to-mattermost` | WebSocket input and HTTP action | `fake-server` |

## Basic Rule Pipeline

This example builds a pipeline from a static provider and the built-in log executor, then processes one message without external services.

```bash
go run ./examples/basic-rule-pipeline
```

Expected summary:

```text
matched_rules=1 executed_actions=1
```

## HTTP Event Gateway

Start a YAML-driven HTTP input with the incident-routing rules:

```bash
WORKFLOW_RULES=examples/rules/incident-routing.yaml \
go run ./examples/http-event-gateway
```

Inspect the process:

```bash
curl -s http://127.0.0.1:18081/health
curl -s http://127.0.0.1:18081/pipeline
```

Send a critical checkout incident:

```bash
curl -s -X POST http://127.0.0.1:18081/events \
  -H 'Content-Type: application/json' \
  -d '{"id":"incident-101","type":"INCIDENT","content":"checkout error rate exceeded 10 percent","user_id":"monitoring","metadata":{"severity":"critical","service":"checkout"}}'
```

The response contains two matched rules and two planned log actions. Override the loopback address with `EVENT_GATEWAY_ADDR`.

### Release Gate

```bash
WORKFLOW_RULES=examples/rules/release-gate.yaml \
go run ./examples/http-event-gateway
```

```bash
curl -s -X POST http://127.0.0.1:18081/events \
  -H 'Content-Type: application/json' \
  -d '{"id":"release-42","type":"RELEASE_CHECK","content":"integration tests failed","user_id":"ci","metadata":{"environment":"production","status":"failed"}}'
```

Change `status` to `approved` to select the approval rule instead.

### Support Triage

```bash
WORKFLOW_RULES=examples/rules/support-triage.yaml \
go run ./examples/http-event-gateway
```

```bash
curl -s -X POST http://127.0.0.1:18081/events \
  -H 'Content-Type: application/json' \
  -d '{"id":"ticket-77","type":"SUPPORT_TICKET","content":"customer requests a refund","user_id":"support-api","metadata":{"priority":"p0"}}'
```

Both the urgent and billing rules match, demonstrating independent rule selection and priority ordering.

### Local Webhook Chain

This rule set converts a raw deployment event into a second audit event by calling the same gateway through the built-in HTTP executor:

```bash
WORKFLOW_RULES=examples/rules/webhook-chain.yaml \
go run ./examples/http-event-gateway
```

```bash
curl -s -X POST http://127.0.0.1:18081/events \
  -H 'Content-Type: application/json' \
  -d '{"id":"deploy-9","type":"RAW_DEPLOY","content":"version 2.4.0","user_id":"ci"}'
```

The logs first report the raw deployment and then the generated `DEPLOY_AUDIT` event. This example assumes the default `127.0.0.1:18081` address.

## Custom Executor

Run an application-owned `collect` executor:

```bash
go run ./examples/custom-executor
```

Expected output:

```text
event=audit-1 user=local-user content=configuration changed
```

The implementation demonstrates parameter validation, template rendering, synchronized mutable state, executor registration, and startup preflight.

## WebSocket to Log

Start the fake server in terminal 1:

```bash
make fake-server
```

Run the reusable YAML-driven WebSocket consumer in terminal 2:

```bash
go run ./examples/websocket-to-log
```

Send a matching event from another terminal:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"example-2","type":"AAA","content":"hello from WebSocket","user_id":"local-user"}'
```

## WebSocket to Mattermost-Like HTTP

This example uses only public framework packages. It translates matching WebSocket messages into HTTP executor calls accepted by the fake Mattermost API.

```bash
make fake-server
go run ./examples/websocket-to-mattermost
```

Send the same `AAA` event and inspect the example output plus the fake-server log.

Environment variables:

- `WEBSOCKET_URL`
- `WORKFLOW_RULES`
- `MATTERMOST_POST_URL`
- `MATTERMOST_API_TOKEN`
- `MATTERMOST_CHANNEL`

## Learning Guides

- Start with `docs/getting-started.md` for a guided first session.
- Use `docs/cookbook.md` for extension and testing recipes.
- Use `docs/development.md` for full runtime debugging and realistic scenario guidebooks.
