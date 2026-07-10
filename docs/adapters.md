# Reference Adapters

Adapters connect transports and external systems to the generic rule pipeline. The reusable framework lives in `pkg/`; the adapters in `internal/adapters` and services in `internal/service` belong only to the reference runtime.

The base configuration enables the WebSocket input and disables all business-specific demo adapters. Run `make dev` to select the profile that enables every adapter below.

## WebSocket Input

The reference input connects to a WebSocket endpoint, decodes incoming JSON into a message, and dispatches it to `WorkflowEngine`:

```yaml
inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
    reconnect_interval: "5s"
```

Expected payload:

```json
{
  "id": "event-1",
  "type": "AAA",
  "content": "hello",
  "user_id": "local-user",
  "timestamp": "2026-07-10T12:00:00Z",
  "metadata": {
    "environment": "development"
  }
}
```

The service reconnects after connection loss using the configured interval. It exposes connection and message counters through `/services` and contributes running/connection checks to `/health`.

Applications normally replace this reference service with their own HTTP, queue, stream, cron, or database-change input. Translate the external payload into `ruleengine.Message`, then call `pipeline.Engine.Process`.

## Confluence-Like Rule Provider

The demo provider reads settings from a Confluence-compatible fake endpoint:

```yaml
adapters:
  confluence:
    enabled: true
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
    refresh_interval: "5m"
```

The runtime registers this provider only when both conditions are true:

- `adapters.confluence.enabled` is `true`
- a workflow policy includes provider `confluence`

The reference Confluence provider emits `mattermost` actions, so this profile also requires `adapters.mattermost.enabled: true`. Startup rejects the incomplete combination even when the initial remote rule snapshot is empty.

Each enabled setting becomes a rule. Event type becomes a `type eq` condition and an optional pattern becomes a `content regex` condition. The settings service refreshes on the configured interval; the provider reads its latest in-memory snapshot.

This adapter is useful for studying remote policy sources. A production provider should define authentication, cache age, stale-data behavior, change versioning, and atomic snapshot publication explicitly.

## Mattermost-Like Executor

The demo executor sends selected actions through the lifecycle-managed Mattermost service:

```yaml
adapters:
  mattermost:
    enabled: true
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
```

Rule action:

```yaml
actions:
  - id: notify
    executor: mattermost
    params:
      channel_id: test-channel-1
      template: "Received {{.Type}}: {{.Content}}"
```

The executor is available only when `adapters.mattermost.enabled` is true. A rule that references `mattermost` while it is disabled causes startup to fail during executor preflight.

For a generic integration, prefer the built-in `http` executor. Create a dedicated executor when the target needs richer domain behavior.

## BadgeDB Demo Executor

BadgeDB is a concurrency-safe in-memory demo store. Its configured path is an identity used by the reference service; the current implementation does not provide durable on-disk persistence.

```yaml
adapters:
  badgedb:
    enabled: true
    path: "/tmp/service-workflow-badges.db"
```

Rule action:

```yaml
actions:
  - id: save_badge
    executor: db
    params:
      name: workflow-badge
      description: created by a rule action
```

The service exposes entry counts through `/services`. Replace it with a durable, idempotent executor before using this pattern for audit records or business state.

## Fake Server Endpoints

`make fake-server` starts one development process with:

| Port | Purpose |
| --- | --- |
| `8090` | Confluence-like settings API |
| `8091` | Mattermost-like HTTP API |
| `8092` | Mattermost-like WebSocket API |
| `8093` | Generic WebSocket input and `/api/send` |

These APIs are deterministic test fixtures, not production-compatible emulators.

## Adding an Input Adapter

1. Decode and validate the external event.
2. Map it to `ruleengine.Message` without leaking transport types into rules.
3. Own connection lifecycle and cancellation in the adapter.
4. Call `pipeline.Engine.Process` with a bounded context.
5. Expose meaningful health checks and counters.
6. Test reconnect, malformed input, cancellation, and backpressure behavior.

## Adding a Provider or Executor

Use the public interfaces from `pkg/ruleengine` and `pkg/executor`, then register the implementations with `pkg/pipeline`. Keep reusable integrations in your own importable package; do not copy the reference runtime's `internal` package boundary into an external application.
