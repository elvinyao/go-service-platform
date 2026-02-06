# Go Service Platform - Project Overview

## Purpose
A microservice workflow platform built with Go for handling and coordinating message passing and business processes between different services (Mattermost, Confluence, WebSocket, BadgeDB).

## Tech Stack
- **Language**: Go 1.22.5
- **Module path**: `project`
- **Logging**: logrus v1.9.3 (structured JSON logging)
- **Caching**: allegro/bigcache v3, patrickmn/go-cache
- **Testing**: stretchr/testify v1.9.0
- **Mattermost SDK**: mattermost-server/v6 v6.7.2
- **Dependency management**: Go Modules

## Architecture
The platform follows a service-manager + workflow-manager pattern:
- **Service Manager**: Manages lifecycle (start/stop/monitor) of all services
- **Workflow Manager**: Dispatches messages to registered workflows
- **DI Container** (`pkg/di`): Wires everything together

### Core services
- `WebSocketService` – receives external messages
- `MattermostService` – integrates with Mattermost via WebSocket
- `ConfluenceService` / `ConfluenceSettingsService` – Confluence integration
- `BadgeDBService` – badge/method query (global, shared)
- `BaseService` – common service base struct (health, metrics, lifecycle)

### Workflows
- `WorkflowA` – main workflow (Confluence + BadgeDB)
- `WorkflowC` – event-driven notifications (Confluence settings + Mattermost)
- Workflow factories in `internal/workflow/factories.go`

### Key packages
- `pkg/logger` – structured context-aware logging
- `pkg/context` – extended Go context with request/trace IDs
- `pkg/errors` – typed error handling
- `pkg/health` – health check framework
- `pkg/di` – dependency injection container
- `pkg/resilience` – circuit breaker, rate limiter
- `pkg/concurrency` – worker pool
- `pkg/metrics` – metrics collector

## Entry Points
- `cmd/main.go` – main application (admin server on :8080)
- `cmd/fakeapi/main.go` – fake Mattermost/Confluence API servers for testing

## Admin HTTP Endpoints
- `GET /health` – health check
- `GET /services` – list services
- `GET /workflows` – list workflows
