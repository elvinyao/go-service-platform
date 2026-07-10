# Runnable Examples

Run commands from the repository root.

## Basic Rule Pipeline

This example builds a pipeline from a static provider and the built-in log executor, then processes one message without external services.

```bash
go run ./examples/basic-rule-pipeline
```

## WebSocket to Log

Start the fake server, then run a reusable YAML-driven WebSocket consumer:

```bash
make fake-server
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
