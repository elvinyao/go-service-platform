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

## Flags
```bash
go run ./cmd/fake-server -h
```

Default ports:
- Confluence: `8090`
- Mattermost HTTP: `8091`
- Mattermost WebSocket: `8092`
- WebSocket test server: `8093`

## Test
```bash
cd fake-server
go test ./...
```
