# Runtime Config Wiring Implementation Plan

> Historical implementation record. Package paths in this plan predate the move from `pkg/di` to `internal/di`. Use `README.md`, `docs/configuration.md`, and `docs/development.md` for current behavior.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the reference runtime honor `RuntimeConfig` for service wiring and make the default rule engine YAML-first.

**Architecture:** Pass `pkg/config.RuntimeConfig` from `cmd/service-workflow` into `internal/app`, `pkg/di`, and `internal/workflow`. The DI container wires only enabled runtime services, while the workflow engine registers only enabled demo providers/executors plus core executors.

**Tech Stack:** Go, `testing`, YAML config, existing `pkg/ruleengine`, `pkg/executor`, `pkg/di`, and `internal/app` packages.

---

### Task 1: Make Defaults YAML-First

**Files:**
- Modify: `pkg/ruleengine/config.go`
- Modify: `config/rule-engine.yaml`
- Test: `pkg/ruleengine/config_test.go`

- [x] **Step 1: Write the failing test**

Add or update a test asserting `DefaultEngineConfig()` uses only the `yaml` provider with `single` mode.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/ruleengine`

- [x] **Step 3: Write minimal implementation**

Update `DefaultEngineConfig()` and `config/rule-engine.yaml` to use `providers: [yaml]`, `mode: single`, and `pipeline_order: [yaml]`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/ruleengine`

### Task 2: Honor RuntimeConfig in DI Service Wiring

**Files:**
- Modify: `pkg/di/container.go`
- Modify: `pkg/di/container_test.go`
- Modify: `internal/app/app.go`

- [x] **Step 1: Write failing tests**

Add tests that disabled WebSocket, Mattermost, Confluence, and BadgeDB config prevents those services from being registered, and that configured URL/path/interval/db path values are used.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/di ./internal/app`

- [x] **Step 3: Write minimal implementation**

Store `pkg/config.RuntimeConfig` in `Container`, pass it from `App.Start()`, and conditionally register services from the config.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/di ./internal/app`

### Task 3: Honor RuntimeConfig in WorkflowEngine Providers and Executors

**Files:**
- Modify: `internal/workflow/workflow_engine.go`
- Modify: `internal/workflow/workflow_engine_test.go`
- Modify: `internal/app/app.go`

- [x] **Step 1: Write failing tests**

Add tests asserting disabled Confluence removes the `confluence` provider from the admin snapshot, disabled Mattermost removes the `mattermost` executor, disabled BadgeDB removes the `db` executor, and `log`/`http` remain registered.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/workflow ./internal/app`

- [x] **Step 3: Write minimal implementation**

Pass runtime config into `NewWorkflowEngine`, build providers/executors conditionally, and keep core `log` and `http` executors always enabled.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/workflow ./internal/app`

### Task 4: Update Docs and Verify

**Files:**
- Modify: `README.md`
- Modify: `docs/configuration.md`
- Modify: `docs/development.md`

- [x] **Step 1: Update docs**

Document YAML-first defaults and demo profile behavior in English.

- [x] **Step 2: Run full verification**

Run: `go test ./...` and `cd fake-server && go test ./...`.
