# Configuration

The reference runtime uses three strict YAML files. Runtime wiring, rule composition, and rule content are deliberately separate so that operational changes do not require recompiling the service.

## Configuration Layers

Values are resolved in this order:

1. Built-in defaults from Go code.
2. The selected YAML file.
3. Supported environment-variable overrides.

The entry point selects files with:

| Variable | Default |
| --- | --- |
| `RUNTIME_CONFIG` | `config/runtime.yaml` |
| `RULE_ENGINE_CONFIG` | `config/rule-engine.yaml` |
| `WORKFLOW_RULES` | `config/workflow-rules.yaml` |

An explicitly selected file must exist. YAML decoding rejects unknown fields and multiple-document files. Invalid configuration fails startup before the admin server begins accepting traffic.

## Base and Demo Profiles

The base profile is YAML-first and disables business-specific adapters:

```bash
make fake-server
# Run this in another terminal.
make run
```

The full demo profile enables Confluence, Mattermost, and BadgeDB:

```bash
make dev
```

The demo profile uses a five-second Confluence refresh interval so local and Compose startup can recover quickly if the fake dependency becomes ready slightly later than the runtime.

The equivalent explicit command is:

```bash
RUNTIME_CONFIG=config/profiles/demo/runtime.yaml \
RULE_ENGINE_CONFIG=config/profiles/demo/rule-engine.yaml \
go run ./cmd/service-workflow
```

## Runtime Configuration

`config/runtime.yaml` configures the reference application. The repository default is:

```yaml
admin:
  address: "127.0.0.1:18080"
  read_header_timeout: "5s"
  read_timeout: "10s"
  write_timeout: "10s"
  idle_timeout: "120s"

inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
    reconnect_interval: "5s"

adapters:
  confluence:
    enabled: false
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
    refresh_interval: "5m"
  mattermost:
    enabled: false
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
  badgedb:
    enabled: false
    path: "/tmp/service-workflow-badges.db"
```

### Admin Fields

| Field | Meaning | Validation |
| --- | --- | --- |
| `admin.address` | Admin HTTP listen address | Required `host:port` form |
| `admin.read_header_timeout` | Maximum time to read request headers | Positive Go duration |
| `admin.read_timeout` | Maximum time to read a request | Positive Go duration |
| `admin.write_timeout` | Maximum response write time | Positive Go duration |
| `admin.idle_timeout` | Keep-alive idle timeout | Positive Go duration |

The admin listener is critical. A bind failure stops startup and rolls back services that already started.

### WebSocket Input Fields

| Field | Meaning | Validation when enabled |
| --- | --- | --- |
| `enabled` | Registers and starts the input service | Boolean |
| `server_url` | WebSocket server base URL | Absolute `ws` or `wss` URL |
| `path` | WebSocket endpoint path | Must start with `/` |
| `reconnect_interval` | Delay between reconnect attempts | Positive Go duration |

The final local endpoint is `server_url + path`, for example `ws://localhost:8093/ws`.

### Demo Adapter Fields

| Field | Meaning | Validation when enabled |
| --- | --- | --- |
| `adapters.confluence.api_endpoint` | Confluence-like HTTP base URL | Absolute `http` or `https` URL |
| `adapters.confluence.settings_page_id` | Settings page identifier | Required |
| `adapters.confluence.refresh_interval` | Refresh interval | Positive Go duration |
| `adapters.mattermost.server_url` | Mattermost-like HTTP base URL | Absolute `http` or `https` URL |
| `adapters.mattermost.websocket_url` | Optional Mattermost-like event WebSocket URL | Empty, or an absolute `ws` or `wss` URL |
| `adapters.mattermost.api_token` | Bearer token used by the demo client | Optional at schema level |
| `adapters.mattermost.channel` | Default target channel | Required |
| `adapters.badgedb.path` | Demo database identity/path | Required |

Runtime `enabled` values control registration. A disabled provider or executor is not available to the rule pipeline. If filtering disabled providers leaves a workflow with no provider, startup fails instead of silently changing the workflow policy.

The local profiles bind the unauthenticated admin API to loopback. Set `ADMIN_ADDR=:18080` only when an all-interface listener is intentionally required. The container image sets `ADMIN_ADDR=:8080` explicitly because published container ports must be reachable outside the container network namespace.

In the reference demo wiring, a workflow that uses the enabled `confluence` provider also requires the Mattermost adapter because every generated Confluence rule targets the `mattermost` executor. This dependency is validated before provider startup.

## Runtime Environment Overrides

| Variable | Field |
| --- | --- |
| `ADMIN_ADDR` | `admin.address` |
| `ADMIN_READ_HEADER_TIMEOUT` | `admin.read_header_timeout` |
| `ADMIN_READ_TIMEOUT` | `admin.read_timeout` |
| `ADMIN_WRITE_TIMEOUT` | `admin.write_timeout` |
| `ADMIN_IDLE_TIMEOUT` | `admin.idle_timeout` |
| `WEBSOCKET_SERVER_URL` | `inputs.websocket.server_url` |
| `WEBSOCKET_PATH` | `inputs.websocket.path` |
| `CONFLUENCE_API_ENDPOINT` | `adapters.confluence.api_endpoint` |
| `CONFLUENCE_SETTINGS_PAGE_ID` | `adapters.confluence.settings_page_id` |
| `MATTERMOST_SERVER_URL` | `adapters.mattermost.server_url` |
| `MATTERMOST_WS_URL` | `adapters.mattermost.websocket_url` |
| `MATTERMOST_API_TOKEN` | `adapters.mattermost.api_token` |
| `MATTERMOST_CHANNEL` | `adapters.mattermost.channel` |
| `BADGEDB_PATH` | `adapters.badgedb.path` |

The demo wiring also reads `CONFLUENCE_SPACE_KEY`, `MATTERMOST_USERNAME`, and `MATTERMOST_PASSWORD`. They are compatibility options for the reference services, not fields in the public runtime schema.

Logging is configured independently with `LOG_LEVEL`, `LOG_FORMAT`, `LOG_TIME_FORMAT`, `LOG_CALLER_INFO`, and `LOG_OUTPUT`.

## Rule Engine Configuration

`config/rule-engine.yaml` selects providers and composition behavior:

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

action_merge:
  dedup: true
  order: priority
```

The demo profile adds the `confluence` provider and uses `or` composition. Supported modes are:

- `single`: use the first configured provider.
- `or`: accept matches from any configured provider.
- `and`: require every configured provider to match.
- `pipeline`: require every provider to match in `pipeline_order`.

Provider names and workflow names must be unique. `pipeline_order` must contain every configured provider exactly once when mode is `pipeline`. A rule with an explicit `workflow` must name an existing policy, and its provider must be configured by that policy. The only action order currently supported is `priority`.

Omitted workflow and provider fields receive documented defaults. Explicit empty `workflows: []` or `providers: []` values are configuration errors; they are never silently replaced with defaults.

A workflow-level `action_merge` block overrides the global block. Explicit `dedup: false` disables deduplication for that workflow; an omitted `dedup` value inherits the global setting. Runtime processing requires an exact workflow policy and never silently falls back to the first configured workflow.

The reference runtime recognizes `yaml` and, when enabled by runtime configuration, `confluence`. Applications built with `pkg/pipeline` can register any provider name they own.

## YAML Rules

`config/workflow-rules.yaml` defines local rules:

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

Rule behavior:

- `id` is required and unique within the file.
- `workflow` defaults to `WorkflowEngine` when omitted.
- `enabled` defaults to `true` when omitted.
- Lower numeric priorities run first.
- A rule with no conditions matches every message for its workflow.
- Every rule requires at least one action.
- Every action requires an `executor`.
- Referencing an executor that was not registered fails pipeline startup.

## Strict Validation Examples

These errors intentionally stop the process:

```text
parse runtime config: field reconnect_intervl not found
inputs.websocket.server_url must use one of these schemes: ws, wss
workflows[0].mode "orr" is invalid
provider yaml rule duplicate-id references unregistered executor custom
```

This fail-fast behavior keeps misspelled or partially wired policies from running with surprising semantics.
