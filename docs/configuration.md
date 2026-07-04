# Configuration

The reference runtime uses separate configuration files for runtime wiring, rule composition, and local rules.

## `config/runtime.yaml`

`runtime.yaml` configures the reference application.

```yaml
admin:
  address: ":18080"

inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
    reconnect_interval: "5s"

adapters:
  confluence:
    enabled: true
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
    refresh_interval: "5m"
  mattermost:
    enabled: true
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
  badgedb:
    enabled: true
    path: "/tmp/service-workflow-badges.db"
```

Fields:

- `admin.address`: HTTP admin listener address. Startup fails if this address cannot bind. Override with `ADMIN_ADDR` for local debugging.
- `inputs.websocket.enabled`: enables the demo WebSocket input.
- `inputs.websocket.server_url`: WebSocket server base URL.
- `inputs.websocket.path`: WebSocket endpoint path.
- `inputs.websocket.reconnect_interval`: reconnect delay after connection loss.
- `adapters.confluence.enabled`: enables the demo Confluence rule provider.
- `adapters.confluence.api_endpoint`: Confluence API base URL.
- `adapters.confluence.settings_page_id`: page ID for rule settings.
- `adapters.confluence.refresh_interval`: settings refresh interval.
- `adapters.mattermost.enabled`: enables the demo Mattermost executor.
- `adapters.mattermost.server_url`: Mattermost HTTP API base URL.
- `adapters.mattermost.websocket_url`: Mattermost WebSocket URL.
- `adapters.mattermost.api_token`: demo API token.
- `adapters.mattermost.channel`: default channel ID.
- `adapters.badgedb.enabled`: enables the demo BadgeDB executor.
- `adapters.badgedb.path`: local demo database path.

Environment overrides:

- `ADMIN_ADDR`
- `WEBSOCKET_SERVER_URL`
- `CONFLUENCE_API_ENDPOINT`
- `MATTERMOST_SERVER_URL`
- `MATTERMOST_WS_URL`

## `config/rule-engine.yaml`

`rule-engine.yaml` configures provider composition and action merge behavior.

```yaml
workflows:
  - name: WorkflowEngine
    providers:
      - yaml
    mode: single
    pipeline_order:
      - yaml
    action_merge:
      dedup: true
      order: priority
```

The default runtime is YAML-first so new rules can run locally without Confluence, Mattermost, or BadgeDB knowledge. To use the Confluence demo provider, enable `adapters.confluence.enabled` in `runtime.yaml` and add `confluence` to the provider list:

```yaml
workflows:
  - name: WorkflowEngine
    providers:
      - yaml
      - confluence
    mode: or
    pipeline_order:
      - yaml
      - confluence
```

Composition modes:

- `single`: use the first provider only.
- `or`: use rules from any provider that matches.
- `and`: all providers must match.
- `pipeline`: providers run in the configured order and all must match.

Runtime `enabled` flags are applied before the workflow engine starts. If an adapter is disabled in `runtime.yaml`, its provider or executor is not registered even if it appears in `rule-engine.yaml`.

## `config/workflow-rules.yaml`

`workflow-rules.yaml` defines local YAML rules.

```yaml
version: "1"
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

Rules contain:

- `id`: unique rule ID.
- `workflow`: workflow policy name, usually `WorkflowEngine`.
- `enabled`: whether the rule can match.
- `priority`: lower values run first.
- `conditions`: predicates over message fields.
- `actions`: executor actions to run when the rule matches.
