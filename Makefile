.PHONY: test test-fake build run fake-server dev clean

test:
	go test ./...

test-fake:
	cd fake-server && go test ./...

build:
	go build -o bin/service-workflow ./cmd/service-workflow

run:
	go run ./cmd/service-workflow

fake-server:
	cd fake-server && go run ./cmd/fake-server

dev:
	@set -e; \
	(cd fake-server && go run ./cmd/fake-server) & \
	FAKE_PID=$$!; \
	trap 'kill $$FAKE_PID 2>/dev/null || true' INT TERM EXIT; \
	sleep 2; \
	go run ./cmd/service-workflow

clean:
	rm -rf bin
