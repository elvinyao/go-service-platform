# Adapters

Adapters connect the framework to external systems. The core SDK stays generic; the reference runtime uses demo adapters under `internal/adapters` and demo services under `internal/service`.

## WebSocket Input

The WebSocket input connects to the fake WebSocket server by default:

```yaml
inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
```

Incoming JSON payloads are decoded into framework messages and dispatched to the rule engine.

## Confluence Rule Provider

The Confluence adapter reads settings from a Confluence-like API and maps them into framework rules.

Default local endpoint:

```yaml
adapters:
  confluence:
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
```

Each enabled setting becomes a rule. The event type maps to a `type eq` condition. A pattern maps to a `content regex` condition.

## Mattermost Executor

The Mattermost adapter sends selected actions to a Mattermost-like API.

```yaml
adapters:
  mattermost:
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
```

Rule action example:

```yaml
actions:
  - id: notify
    executor: mattermost
    params:
      channel_id: test-channel-1
      template: "Received {{.Type}}: {{.Content}}"
```

## BadgeDB Executor

The BadgeDB adapter is an in-memory demo store that writes a badge-like record for matched messages.

```yaml
actions:
  - id: save_badge
    executor: db
    params:
      name: workflow-badge
      description: created by a rule action
```

It is useful for demonstrating stateful actions without adding a real database dependency to the core framework.
