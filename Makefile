.PHONY: test test-fake test-race coverage vet vet-fake verify build build-fake run fake-server dev clean

test:
	go test ./...

test-fake:
	cd fake-server && go test ./...

test-race:
	go test -race ./...
	cd fake-server && go test -race ./...

coverage:
	./scripts/check-coverage.sh
	cd fake-server && ../scripts/check-coverage.sh

vet:
	go vet ./...

vet-fake:
	cd fake-server && go vet ./...

verify: test test-fake vet vet-fake build build-fake

build:
	mkdir -p bin
	go build -o bin/service-workflow ./cmd/service-workflow

build-fake:
	mkdir -p bin
	cd fake-server && go build -o ../bin/fake-server ./cmd/fake-server

run:
	go run ./cmd/service-workflow

fake-server:
	cd fake-server && go run ./cmd/fake-server

dev: build-fake
	@set -e; \
	command -v curl >/dev/null 2>&1 || { printf '%s\n' 'make dev requires curl' >&2; exit 1; }; \
	./bin/fake-server & \
	FAKE_PID=$$!; \
	cleanup() { kill $$FAKE_PID 2>/dev/null || true; wait $$FAKE_PID 2>/dev/null || true; }; \
	trap cleanup INT TERM EXIT; \
	attempts=0; \
	until curl -fsS http://localhost:8093/health >/dev/null 2>&1; do \
		kill -0 $$FAKE_PID 2>/dev/null || { wait $$FAKE_PID; exit 1; }; \
		attempts=$$((attempts + 1)); \
		if [ $$attempts -ge 100 ]; then printf '%s\n' 'Fake server readiness timed out' >&2; exit 1; fi; \
		sleep 0.1; \
	done; \
	printf '%s\n' 'Admin API: http://localhost:18080' \
	  'Health: curl -s http://localhost:18080/health' \
	  'Send event: curl -s -X POST http://localhost:8093/api/send -H "Content-Type: application/json" -d '\''{"id":"m1","type":"AAA","content":"hello","user_id":"u1"}'\'''; \
	RUNTIME_CONFIG=config/profiles/demo/runtime.yaml \
	RULE_ENGINE_CONFIG=config/profiles/demo/rule-engine.yaml \
	go run ./cmd/service-workflow

clean:
	rm -rf bin
