# GEMINI.md - Project Context

This file provides context for Gemini to understand the architecture, development conventions, and operational procedures of the `go-service-platform`.

## Project Overview
The `go-service-platform` is a Go-based microservice workflow platform designed to coordinate message passing and business processes between various services (WebSocket, Mattermost, Confluence, etc.).

### Key Technologies
- **Language:** Go 1.22.5+
- **Logging:** Logrus (structured JSON logging)
- **Caching:** BigCache, go-cache
- **Communication:** Gorilla WebSocket, Mattermost Server SDK
- **Architecture:** Interface-driven, Dependency Injection (DI), Service-Oriented

### Core Architecture
- **Service Manager (`internal/manager/service_manager.go`):** Manages the lifecycle (start, stop, monitor) of all registered services.
- **Workflow Manager (`internal/manager/workflow_manager.go`):** Dispatches messages to registered workflows which coordinate service interactions.
- **DI Container (`pkg/di/container.go`):** Centralized registry for singletons and service initialization.
- **Context Package (`pkg/context/`):** Extends standard context with request/trace IDs and operation names.
- **Graceful Shutdown:** Implements a robust 5-step shutdown sequence (Mark Not Ready -> Drain Traffic -> Stop Monitoring -> Shutdown Admin Server -> Shutdown Services).

## Building and Running
### Build
```bash
go build -o bin/service-workflow cmd/main.go
```

### Run
The application relies on environment variables for configuration.
```bash
LOG_LEVEL=debug LOG_FORMAT=json ./bin/service-workflow
```
Key Environment Variables:
- `MATTERMOST_SERVER_URL`, `MATTERMOST_API_TOKEN`
- `WEBSOCKET_SERVER_URL`, `WEBSOCKET_PATH`
- `CONFLUENCE_API_ENDPOINT`, `CONFLUENCE_SETTINGS_PAGE_ID`

### Test
```bash
go test ./...
```

## Development Conventions
### 1. Service Implementation
New services must implement the `interfaces.Service` interface:
```go
type Service interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    GetName() string
    IsRunning(ctx context.Context) bool
    // ... other standard methods
}
```
Register new services in `pkg/di/container.go` within the `RegisterServices` method.

### 2. Workflow Implementation
Workflows should coordinate services through the `ServiceManager`. New workflows must be registered via a factory in `internal/workflow/factories.go`.

### 3. Context & Logging
- **Always** use `pkg/context` to create or propagate context.
- Use `logger.InfoWithContext(ctx, ...)` and similar methods to ensure trace IDs are included in logs.
- Use `logger.LogOperation` or `logger.LogTimingOperation` to wrap business logic for automatic performance metrics.

### 4. Error Handling
Use `pkg/errors` for creating and wrapping errors. It supports error types (e.g., `TypeNotFound`, `TypeServiceUnavailable`) and structured fields.
```go
errors.New(errors.TypeNotFound, "description", nil).WithField("key", "value")
```

### 5. Configuration
Configuration is managed via service-specific structs (e.g., `MattermostConfig`) and populated from environment variables in the DI container.

## Directory Structure
- `cmd/`: Application entry point.
- `internal/`: Private application code (services, managers, workflows).
- `pkg/`: Public utility packages (di, logger, errors, health, context).
- `config/`: Configuration files and providers.
- `deploy/`: Kubernetes manifests.
- `fake-server/`: Mock servers for local development and testing.
