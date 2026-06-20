# Development

## Commands

Run all root module tests:

```bash
make test
```

Run fake-server tests:

```bash
make test-fake
```

Build the reference runtime:

```bash
make build
```

Run the fake APIs:

```bash
make fake-server
```

Run the reference runtime:

```bash
make run
```

Run both in one terminal:

```bash
make dev
```

## Manual Verification

Start fake-server and the runtime, then run:

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/health/readiness
curl -s http://localhost:8080/health/liveness
curl -s http://localhost:8080/services
curl -s http://localhost:8080/rule-engine
```

Send a sample message:

```bash
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m1","type":"AAA","content":"hello from development docs","user_id":"u1","timestamp":"2026-06-20T00:00:00Z"}'
```

The runtime logs should show the message being dispatched and matched by configured rules.

## Docker Compose

```bash
docker compose up --build
```

Verify:

```bash
curl -s http://localhost:18080/health
curl -s http://localhost:18080/rule-engine
```

Compose keeps the container admin listener on `:8080` and publishes it on host port `18080` by default. Use `ADMIN_PORT=8080 docker compose up --build` to publish it on host port `8080`.

Stop:

```bash
docker compose down
```

## Port Conflicts

The admin server uses `ADMIN_ADDR` or `config/runtime.yaml`.

```bash
ADMIN_ADDR=:18080 make run
```

If the configured address is already in use, startup fails immediately instead of leaving services partially running.
