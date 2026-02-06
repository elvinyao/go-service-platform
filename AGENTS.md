# Repository Guidelines

## Project Structure & Module Organization
This repository is a Go service platform (`module project`) with one primary entrypoint:
- `cmd/main.go`: main service runtime.

The fake API servers are extracted into a separate Go project under `fake-server/`.

Core application code lives in `internal/` (`service`, `manager`, `workflow`, `messaging`, `config`, etc.). Reusable shared libraries live in `pkg/` (`logger`, `health`, `errors`, `di`, `context`). Runtime config is in `config/config.yaml`, and Kubernetes manifests are in `deploy/k8s/`. Tests are colocated with implementation files as `*_test.go`.

## Build, Test, and Development Commands
- `go mod download` - fetch dependencies.
- `go build -o bin/service-workflow ./cmd/main.go` - build the main binary.
- `go run ./cmd/main.go` - run the service directly.
- `cd fake-server && go run ./cmd/fake-server -h` - run standalone fake servers and view options.
- `go test ./...` - run all unit tests across the module.
- `docker build -t service-workflow .` - build the production image from `Dockerfile`.

## Coding Style & Naming Conventions
Target Go `1.22.5` and keep code `gofmt`-clean (`go fmt ./...` before opening a PR). Use standard Go conventions: tabs via `gofmt`, lowercase package names, `CamelCase` exported identifiers, and descriptive receiver names. Keep file names lowercase with underscores when needed (for example, `websocket_service.go`). Prefer small interfaces in `internal/interfaces` and keep workflow/service implementations inside their domain folders.

## Testing Guidelines
Use Go’s `testing` package with `testify` (`assert`/`require`) as seen in existing tests. Name tests `TestXxx`, keep them in the same package directory, and favor table-driven tests for branching logic. Add or update tests for each behavior change or bug fix, then run `go test ./...` locally before submitting.

## Commit & Pull Request Guidelines
Recent history mixes styles, but `feat:` and `fix` prefixes are common. Prefer clear, imperative commit messages like `feat: add websocket reconnection backoff` and avoid vague messages like `update`. PRs should include:
- What changed and why.
- Linked issue/task (if available).
- Test evidence (for example, `go test ./...` output summary).
- Notes on config or `deploy/k8s/` changes when relevant.

## Security & Configuration Tips
Do not commit real secrets or tokens. Keep environment-specific values in environment variables (for example, `MATTERMOST_API_TOKEN`, `CONFLUENCE_API_ENDPOINT`) and treat `config/config.yaml` as baseline configuration for local/dev.
