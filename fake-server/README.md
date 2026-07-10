# Fake Server

Standalone fake API project for local development and integration tests.

## Includes

- Fake Confluence API server
- Fake Mattermost HTTP + WebSocket server
- Fake WebSocket test server

## Run

```bash
cd fake-server
go run ./cmd/fake-server
```

All listeners are bound before startup reports success. A port conflict exits the process with a descriptive error.

## Flags

```bash
go run ./cmd/fake-server -h
```

Default ports:

- Confluence: `8090`
- Mattermost HTTP: `8091`
- Mattermost WebSocket: `8092`
- WebSocket test server: `8093`

## WebSocket Test API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `GET` | `/ws` | Upgrade to the generic WebSocket event stream |
| `POST` | `/api/send` | Validate, store, and broadcast one event |
| `GET` | `/api/messages` | List events accepted by HTTP or received from clients |
| `GET` | `/health` | Report client and message counts |

Example:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"demo-1","type":"AAA","content":"hello","user_id":"local-user"}'

curl -s http://localhost:8093/api/messages
```

## Test

```bash
cd fake-server
go test ./...
```
