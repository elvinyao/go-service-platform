# Go Service Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the project as a reusable, configurable, rule-driven Go service framework with a runnable reference application.

**Architecture:** Move framework-level rule engine and executor APIs into `pkg/`, keep business integrations in `internal/adapters`, and make `cmd/service-workflow` a thin reference runtime. Add validated runtime configuration, local development automation, Compose support, and documentation that describes the framework instead of the old business workflows.

**Tech Stack:** Go 1.26 modules, standard `net/http`, `gopkg.in/yaml.v3`, Gorilla WebSocket for the demo input adapter, existing logger/health/error packages, Make, Docker Compose.

---

## File Structure Map

Create or reshape these areas:

- `pkg/ruleengine/`: public rule types, YAML provider, matcher, composer, config loader, snapshots, tests.
- `pkg/executor/`: public executor interface, registry, template renderer, built-in log/http executors, tests.
- `pkg/config/`: runtime config structs, YAML loader, environment overrides, validation tests.
- `pkg/runtime/`: lifecycle orchestration, admin server startup, fail-fast listener behavior, tests.
- `internal/adapters/websocket/`: WebSocket input adapter using existing `WebSocketService` behavior.
- `internal/adapters/confluence/`: Confluence-backed rule provider adapter.
- `internal/adapters/mattermost/`: Mattermost executor adapter.
- `internal/adapters/badgedb/`: demo store and database executor adapter.
- `internal/app/`: reference application wiring for configured inputs, providers, and executors.
- `cmd/service-workflow/main.go`: small entry point that delegates to `internal/app`.
- `config/runtime.yaml`: default local runtime configuration.
- `config/rule-engine.yaml`: composition policy retained and validated.
- `config/workflow-rules.yaml`: local demo rules.
- `Makefile`: local build, test, dev, and cleanup commands.
- `docker-compose.yml`: local containerized runtime plus fake-server.
- `README.md` and `docs/*.md`: English framework documentation.

Keep these packages stable and reuse them:

- `pkg/logger`
- `pkg/health`
- `pkg/errors`
- `pkg/context`
- `pkg/concurrency`
- `pkg/resilience`

---

## Task 1: Extract `pkg/ruleengine`

**Files:**
- Create: `pkg/ruleengine/types.go`
- Create: `pkg/ruleengine/config.go`
- Create: `pkg/ruleengine/provider.go`
- Create: `pkg/ruleengine/matcher.go`
- Create: `pkg/ruleengine/yaml_provider.go`
- Create: `pkg/ruleengine/composer.go`
- Create: `pkg/ruleengine/ruleengine_test.go`
- Modify: `internal/ruleengine/*`

- [ ] **Step 1: Write public rule engine tests**

Create `pkg/ruleengine/ruleengine_test.go` with tests that describe the public SDK behavior:

```go
package ruleengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestYAMLProviderLoadsEnabledRulesWithDefaultWorkflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	data := []byte(`version: "1"
rules:
  - id: log_aaa
    enabled: true
    priority: 10
    conditions:
      - field: type
        op: eq
        value: AAA
    actions:
      - id: log
        executor: log
        priority: 10
        params:
          level: info
          template: "type={{.Type}} id={{.ID}}"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write rules file: %v", err)
	}

	provider := NewYAMLProvider("yaml", path, "WorkflowEngine")
	if err := provider.Start(context.Background()); err != nil {
		t.Fatalf("start provider: %v", err)
	}

	snapshot := provider.Snapshot(context.Background())
	if len(snapshot.Rules) != 1 {
		t.Fatalf("rules length = %d, want 1", len(snapshot.Rules))
	}
	if snapshot.Rules[0].Workflow != "WorkflowEngine" {
		t.Fatalf("workflow = %q, want WorkflowEngine", snapshot.Rules[0].Workflow)
	}
}

func TestComposerBuildsPriorityOrderedDeduplicatedPlan(t *testing.T) {
	now := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	provider := NewStaticProvider("static", RuleSet{
		Source:   "static",
		Version:  "1",
		LoadedAt: now,
		Rules: []Rule{
			{
				ID:       "rule_b",
				Workflow: "WorkflowEngine",
				Enabled:  true,
				Priority: 20,
				Conditions: []Condition{{Field: "type", Op: OpEq, Value: "AAA"}},
				Actions: []Action{{ID: "same", Executor: "log", Priority: 20}},
			},
			{
				ID:       "rule_a",
				Workflow: "WorkflowEngine",
				Enabled:  true,
				Priority: 10,
				Conditions: []Condition{{Field: "content", Op: OpContains, Value: "hello"}},
				Actions: []Action{{ID: "same", Executor: "log", Priority: 10}},
			},
		},
	})

	cfg := EngineConfig{
		Workflows: []WorkflowPolicy{{
			Name:      "WorkflowEngine",
			Providers: []string{"static"},
			Mode:      CompositionOr,
			ActionMerge: ActionMergeConfig{
				Dedup: true,
				Order: ActionOrderPriority,
			},
		}},
	}

	composer := NewComposer(cfg, map[string]RuleProvider{"static": provider})
	plan, err := composer.BuildExecutionPlan(context.Background(), "WorkflowEngine", Message{
		ID: "m1", Type: "AAA", Content: "hello", Timestamp: now,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(plan.Rules) != 2 {
		t.Fatalf("rules length = %d, want 2", len(plan.Rules))
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(plan.Actions))
	}
	if plan.Actions[0].ID != "same" {
		t.Fatalf("action id = %q, want same", plan.Actions[0].ID)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail because the package does not exist**

Run:

```bash
go test ./pkg/ruleengine
```

Expected: FAIL with package or symbol errors for `pkg/ruleengine`.

- [ ] **Step 3: Move rule engine implementation into `pkg/ruleengine`**

Use `git mv` for these files:

```bash
mkdir -p pkg/ruleengine
git mv internal/ruleengine/types.go pkg/ruleengine/types.go
git mv internal/ruleengine/config.go pkg/ruleengine/config.go
git mv internal/ruleengine/provider.go pkg/ruleengine/provider.go
git mv internal/ruleengine/matcher.go pkg/ruleengine/matcher.go
git mv internal/ruleengine/yaml_provider.go pkg/ruleengine/yaml_provider.go
git mv internal/ruleengine/composer.go pkg/ruleengine/composer.go
```

Change each moved file from:

```go
package ruleengine
```

to the same package name:

```go
package ruleengine
```

Keep the package name unchanged; only the import path changes from `project/internal/ruleengine` to `project/pkg/ruleengine`.

- [ ] **Step 4: Add `Message` and `StaticProvider` to `pkg/ruleengine`**

Add this to `pkg/ruleengine/types.go`:

```go
type Message struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	UserID    string                 `json:"user_id"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}
```

Create `pkg/ruleengine/static_provider.go`:

```go
package ruleengine

import "context"

type StaticProvider struct {
	name string
	set  RuleSet
}

func NewStaticProvider(name string, set RuleSet) *StaticProvider {
	set.Source = name
	return &StaticProvider{name: name, set: cloneRuleSet(set)}
}

func (p *StaticProvider) Name() string {
	return p.name
}

func (p *StaticProvider) Start(ctx context.Context) error {
	return nil
}

func (p *StaticProvider) Snapshot(ctx context.Context) RuleSet {
	return cloneRuleSet(p.set)
}
```

- [ ] **Step 5: Update `pkg/ruleengine` to use `Message`**

Change matcher and composer method signatures from `internal/model.Message` to `ruleengine.Message` inside the package:

```go
func (c *Composer) BuildExecutionPlan(ctx context.Context, workflow string, msg Message) (ExecutionPlan, error)
func MatchRules(rules []Rule, msg Message) []Rule
```

Remove the `project/internal/model` import from moved files.

- [ ] **Step 6: Run public rule engine tests**

Run:

```bash
go test ./pkg/ruleengine
```

Expected: PASS.

- [ ] **Step 7: Update imports in current app code**

Replace imports of:

```go
"project/internal/ruleengine"
```

with:

```go
"project/pkg/ruleengine"
```

Files expected to change:

- `cmd/main.go`
- `pkg/di/container.go`
- `internal/workflow/workflow_engine.go`
- `internal/executor/*.go`

- [ ] **Step 8: Run existing root tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add pkg/ruleengine internal/ruleengine cmd/main.go pkg/di/container.go internal/workflow internal/executor
git commit -m "refactor: expose rule engine sdk"
```

---

## Task 2: Extract `pkg/executor`

**Files:**
- Create: `pkg/executor/executor.go`
- Create: `pkg/executor/registry.go`
- Create: `pkg/executor/template.go`
- Create: `pkg/executor/log_executor.go`
- Create: `pkg/executor/http_executor.go`
- Create: `pkg/executor/executor_test.go`
- Modify: `internal/executor/*`
- Modify: `internal/workflow/workflow_engine.go`

- [ ] **Step 1: Write public executor tests**

Create `pkg/executor/executor_test.go`:

```go
package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"project/pkg/ruleengine"
)

type recordingExecutor struct {
	executed bool
}

func (e *recordingExecutor) Type() string {
	return "record"
}

func (e *recordingExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	e.executed = true
	return nil
}

func TestRegistryRegistersAndRetrievesExecutor(t *testing.T) {
	registry := NewRegistry()
	exe := &recordingExecutor{}

	if err := registry.Register(exe); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	got, ok := registry.Get("record")
	if !ok {
		t.Fatalf("executor not found")
	}
	if got.Type() != "record" {
		t.Fatalf("executor type = %q, want record", got.Type())
	}
}

func TestRenderTemplateUsesMessageFields(t *testing.T) {
	msg := ruleengine.Message{ID: "m1", Type: "AAA", Content: "hello"}
	rendered, err := RenderTemplate("{{.Type}} {{.ID}} {{.Content}}", msg)
	if err != nil {
		t.Fatalf("render template: %v", err)
	}
	if rendered != "AAA m1 hello" {
		t.Fatalf("rendered = %q, want AAA m1 hello", rendered)
	}
}

func TestHTTPExecutorPostsRenderedBody(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	exe := NewHTTPExecutor()
	err := exe.Execute(context.Background(), ruleengine.Message{
		ID: "m1", Type: "AAA", Content: "hello", Timestamp: time.Now(),
	}, ruleengine.Action{
		ID:       "post",
		Executor: "http",
		Params: map[string]interface{}{
			"url":           server.URL,
			"body_template": `{"type":"{{.Type}}","content":"{{.Content}}"}`,
		},
	})
	if err != nil {
		t.Fatalf("execute http action: %v", err)
	}
	if gotBody != `{"type":"AAA","content":"hello"}` {
		t.Fatalf("body = %q", gotBody)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail before extraction**

Run:

```bash
go test ./pkg/executor
```

Expected: FAIL with package or symbol errors for `pkg/executor`.

- [ ] **Step 3: Move generic executor implementation**

Use `git mv`:

```bash
mkdir -p pkg/executor
git mv internal/executor/executor.go pkg/executor/executor.go
git mv internal/executor/registry.go pkg/executor/registry.go
git mv internal/executor/template.go pkg/executor/template.go
git mv internal/executor/log_executor.go pkg/executor/log_executor.go
git mv internal/executor/http_executor.go pkg/executor/http_executor.go
```

Change imports in moved files from:

```go
"project/internal/model"
"project/internal/ruleengine"
```

to:

```go
"project/pkg/ruleengine"
```

Change signatures to use `ruleengine.Message`.

- [ ] **Step 4: Export template rendering**

In `pkg/executor/template.go`, rename:

```go
func renderTemplate(tmpl string, msg ruleengine.Message) (string, error)
```

to:

```go
func RenderTemplate(tmpl string, msg ruleengine.Message) (string, error)
```

Update `LogExecutor` and `HTTPExecutor` to call `RenderTemplate`.

- [ ] **Step 5: Keep business executors internal**

Leave Mattermost and BadgeDB executor code outside `pkg/executor`. Move them later in Task 5.

- [ ] **Step 6: Update workflow engine imports**

In `internal/workflow/workflow_engine.go`, replace:

```go
"project/internal/executor"
```

with:

```go
"project/pkg/executor"
```

- [ ] **Step 7: Run executor tests**

Run:

```bash
go test ./pkg/executor
```

Expected: PASS.

- [ ] **Step 8: Run root tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add pkg/executor internal/executor internal/workflow/workflow_engine.go
git commit -m "refactor: expose executor sdk"
```

---

## Task 3: Add Validated Runtime Configuration

**Files:**
- Create: `pkg/config/runtime.go`
- Create: `pkg/config/runtime_test.go`
- Create: `config/runtime.yaml`
- Modify: `pkg/di/container.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Write runtime configuration tests**

Create `pkg/config/runtime_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRuntimeConfigAppliesDefaultsAndYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.yaml")
	data := []byte(`admin:
  address: ":18080"
inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
    reconnect_interval: "2s"
adapters:
  confluence:
    enabled: true
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
    refresh_interval: "30s"
  mattermost:
    enabled: true
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
  badgedb:
    enabled: true
    path: "/tmp/service-workflow-badges.db"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if cfg.Admin.Address != ":18080" {
		t.Fatalf("admin address = %q, want :18080", cfg.Admin.Address)
	}
	if cfg.Inputs.WebSocket.ReconnectInterval != 2*time.Second {
		t.Fatalf("reconnect interval = %v, want 2s", cfg.Inputs.WebSocket.ReconnectInterval)
	}
}

func TestLoadRuntimeConfigRejectsEmptyAdminAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.yaml")
	data := []byte(`admin:
  address: ""
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadRuntimeConfig(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}
```

- [ ] **Step 2: Run config tests and verify they fail**

Run:

```bash
go test ./pkg/config
```

Expected: FAIL with missing `LoadRuntimeConfig` or missing types.

- [ ] **Step 3: Implement runtime config types and loader**

Create `pkg/config/runtime.go`:

```go
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type RuntimeConfig struct {
	Admin    AdminConfig    `yaml:"admin" json:"admin"`
	Inputs   InputsConfig   `yaml:"inputs" json:"inputs"`
	Adapters AdaptersConfig `yaml:"adapters" json:"adapters"`
}

type AdminConfig struct {
	Address string `yaml:"address" json:"address"`
}

type InputsConfig struct {
	WebSocket WebSocketInputConfig `yaml:"websocket" json:"websocket"`
}

type WebSocketInputConfig struct {
	Enabled           bool          `yaml:"enabled" json:"enabled"`
	ServerURL         string        `yaml:"server_url" json:"server_url"`
	Path              string        `yaml:"path" json:"path"`
	ReconnectInterval time.Duration `yaml:"-" json:"reconnect_interval"`
	ReconnectRaw      string        `yaml:"reconnect_interval" json:"-"`
}

type AdaptersConfig struct {
	Confluence ConfluenceAdapterConfig `yaml:"confluence" json:"confluence"`
	Mattermost MattermostAdapterConfig `yaml:"mattermost" json:"mattermost"`
	BadgeDB    BadgeDBAdapterConfig    `yaml:"badgedb" json:"badgedb"`
}

type ConfluenceAdapterConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	APIEndpoint     string        `yaml:"api_endpoint" json:"api_endpoint"`
	SettingsPageID  string        `yaml:"settings_page_id" json:"settings_page_id"`
	RefreshInterval time.Duration `yaml:"-" json:"refresh_interval"`
	RefreshRaw      string        `yaml:"refresh_interval" json:"-"`
}

type MattermostAdapterConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	ServerURL    string `yaml:"server_url" json:"server_url"`
	WebsocketURL string `yaml:"websocket_url" json:"websocket_url"`
	APIToken     string `yaml:"api_token" json:"api_token"`
	Channel      string `yaml:"channel" json:"channel"`
}

type BadgeDBAdapterConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path" json:"path"`
}

func LoadRuntimeConfig(path string) (RuntimeConfig, error) {
	cfg := DefaultRuntimeConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("read runtime config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return RuntimeConfig{}, fmt.Errorf("parse runtime config: %w", err)
	}
	cfg.ApplyEnv()
	if err := cfg.Normalize(); err != nil {
		return RuntimeConfig{}, err
	}
	return cfg, nil
}

func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		Admin: AdminConfig{Address: ":8080"},
		Inputs: InputsConfig{WebSocket: WebSocketInputConfig{
			Enabled:           true,
			ServerURL:         "ws://localhost:8093",
			Path:              "/ws",
			ReconnectRaw:      "5s",
			ReconnectInterval: 5 * time.Second,
		}},
		Adapters: AdaptersConfig{
			Confluence: ConfluenceAdapterConfig{
				Enabled:         true,
				APIEndpoint:     "http://localhost:8090",
				SettingsPageID:  "settings-page-1",
				RefreshRaw:      "5m",
				RefreshInterval: 5 * time.Minute,
			},
			Mattermost: MattermostAdapterConfig{
				Enabled:      true,
				ServerURL:    "http://localhost:8091",
				WebsocketURL: "ws://localhost:8092",
				APIToken:     "test-token-123",
				Channel:      "test-channel-1",
			},
			BadgeDB: BadgeDBAdapterConfig{
				Enabled: true,
				Path:    "/tmp/service-workflow-badges.db",
			},
		},
	}
}

func (c *RuntimeConfig) ApplyEnv() {
	if v := os.Getenv("ADMIN_ADDR"); v != "" {
		c.Admin.Address = v
	}
	if v := os.Getenv("WEBSOCKET_SERVER_URL"); v != "" {
		c.Inputs.WebSocket.ServerURL = v
	}
	if v := os.Getenv("CONFLUENCE_API_ENDPOINT"); v != "" {
		c.Adapters.Confluence.APIEndpoint = v
	}
	if v := os.Getenv("MATTERMOST_SERVER_URL"); v != "" {
		c.Adapters.Mattermost.ServerURL = v
	}
	if v := os.Getenv("MATTERMOST_WS_URL"); v != "" {
		c.Adapters.Mattermost.WebsocketURL = v
	}
}

func (c *RuntimeConfig) Normalize() error {
	if c.Admin.Address == "" {
		return fmt.Errorf("admin.address is required")
	}
	if c.Inputs.WebSocket.ReconnectRaw != "" {
		d, err := time.ParseDuration(c.Inputs.WebSocket.ReconnectRaw)
		if err != nil {
			return fmt.Errorf("inputs.websocket.reconnect_interval is invalid: %w", err)
		}
		c.Inputs.WebSocket.ReconnectInterval = d
	}
	if c.Adapters.Confluence.RefreshRaw != "" {
		d, err := time.ParseDuration(c.Adapters.Confluence.RefreshRaw)
		if err != nil {
			return fmt.Errorf("adapters.confluence.refresh_interval is invalid: %w", err)
		}
		c.Adapters.Confluence.RefreshInterval = d
	}
	return nil
}
```

- [ ] **Step 4: Add default runtime configuration**

Create `config/runtime.yaml`:

```yaml
admin:
  address: ":8080"

inputs:
  websocket:
    enabled: true
    server_url: "ws://localhost:8093"
    path: "/ws"
    reconnect_interval: "5s"

adapters:
  confluence:
    enabled: true
    api_endpoint: "http://localhost:8090"
    settings_page_id: "settings-page-1"
    refresh_interval: "5m"
  mattermost:
    enabled: true
    server_url: "http://localhost:8091"
    websocket_url: "ws://localhost:8092"
    api_token: "test-token-123"
    channel: "test-channel-1"
  badgedb:
    enabled: true
    path: "/tmp/service-workflow-badges.db"
```

- [ ] **Step 5: Run config tests**

Run:

```bash
go test ./pkg/config
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/config config/runtime.yaml
git commit -m "feat: add validated runtime configuration"
```

---

## Task 4: Add Runtime Lifecycle and Fail-Fast Admin Server

**Files:**
- Create: `pkg/runtime/admin_server.go`
- Create: `pkg/runtime/admin_server_test.go`
- Create: `pkg/runtime/app.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Write admin server bind failure test**

Create `pkg/runtime/admin_server_test.go`:

```go
package runtime

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestAdminServerStartFailsWhenAddressIsInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	server := NewAdminServer(listener.Addr().String(), http.NewServeMux())
	err = server.Start(context.Background())
	if err == nil {
		t.Fatalf("expected address in use error")
	}
}

func TestAdminServerStartsAndStops(t *testing.T) {
	server := NewAdminServer("127.0.0.1:0", http.NewServeMux())
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	if server.Addr() == "" {
		t.Fatalf("server address is empty")
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("stop server: %v", err)
	}
}
```

- [ ] **Step 2: Run runtime tests and verify they fail**

Run:

```bash
go test ./pkg/runtime
```

Expected: FAIL with missing `NewAdminServer`.

- [ ] **Step 3: Implement listener-first admin server**

Create `pkg/runtime/admin_server.go`:

```go
package runtime

import (
	"context"
	"net"
	"net/http"
	"time"
)

type AdminServer struct {
	address  string
	handler  http.Handler
	server   *http.Server
	listener net.Listener
	done     chan error
}

func NewAdminServer(address string, handler http.Handler) *AdminServer {
	return &AdminServer{address: address, handler: handler}
}

func (s *AdminServer) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	s.listener = listener
	s.server = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.done = make(chan error, 1)
	go func() {
		err := s.server.Serve(listener)
		if err == http.ErrServerClosed {
			err = nil
		}
		s.done <- err
	}()
	return nil
}

func (s *AdminServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	err := s.server.Shutdown(ctx)
	if s.done != nil {
		select {
		case serveErr := <-s.done:
			if err == nil {
				err = serveErr
			}
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
		}
	}
	return err
}

func (s *AdminServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}
```

- [ ] **Step 4: Run runtime tests**

Run:

```bash
go test ./pkg/runtime
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/runtime
git commit -m "feat: add fail-fast runtime admin server"
```

---

## Task 5: Convert Business Integrations into Demo Adapters

**Files:**
- Create: `internal/adapters/badgedb/executor.go`
- Create: `internal/adapters/mattermost/executor.go`
- Create: `internal/adapters/confluence/provider.go`
- Create: `internal/adapters/websocket/input.go`
- Modify: `internal/service/*`
- Modify: `internal/workflow/workflow_engine.go`
- Delete: `internal/workflow/workflow_a.go`
- Delete: `internal/workflow/workflow_c.go`

- [ ] **Step 1: Write adapter compile tests**

Create `internal/adapters/badgedb/executor_test.go`:

```go
package badgedb

import (
	"testing"
)

func TestExecutorType(t *testing.T) {
	exe := NewExecutor(nil)
	if exe.Type() != "db" {
		t.Fatalf("type = %q, want db", exe.Type())
	}
}
```

Create `internal/adapters/mattermost/executor_test.go`:

```go
package mattermost

import "testing"

func TestExecutorType(t *testing.T) {
	exe := NewExecutor(nil)
	if exe.Type() != "mattermost" {
		t.Fatalf("type = %q, want mattermost", exe.Type())
	}
}
```

- [ ] **Step 2: Run adapter tests and verify they fail**

Run:

```bash
go test ./internal/adapters/...
```

Expected: FAIL with missing packages or symbols.

- [ ] **Step 3: Move BadgeDB executor into adapter package**

Create `internal/adapters/badgedb/executor.go` using the current logic from `internal/executor/db_executor.go`, with this public constructor:

```go
package badgedb

import (
	"context"
	"fmt"
	"time"

	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/ruleengine"
)

type Executor struct {
	serviceManager *manager.ServiceManager
}

func NewExecutor(serviceManager *manager.ServiceManager) *Executor {
	return &Executor{serviceManager: serviceManager}
}

func (e *Executor) Type() string {
	return "db"
}

func (e *Executor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	if e.serviceManager == nil {
		return nil
	}
	svc, ok := e.serviceManager.GetServiceByName(ctx, "BadgeDBService")
	if !ok {
		return fmt.Errorf("BadgeDBService not found")
	}
	badgeDBService, ok := svc.(*service.BadgeDBService)
	if !ok {
		return fmt.Errorf("BadgeDBService type mismatch: %T", svc)
	}
	name, _ := action.Params["name"].(string)
	if name == "" {
		name = "workflow-badge"
	}
	badge := model.Badge{
		ID:          fmt.Sprintf("wf_%s_%d", msg.ID, time.Now().UnixNano()),
		Name:        name,
		Description: "created by workflow executor",
		UserID:      msg.UserID,
		AwardedAt:   time.Now(),
		Type:        msg.Type,
		Attributes: map[string]interface{}{"source_message_id": msg.ID, "action_id": action.ID},
	}
	return badgeDBService.SaveBadge(ctx, badge)
}
```

- [ ] **Step 4: Move Mattermost executor into adapter package**

Create `internal/adapters/mattermost/executor.go` using the current logic from `internal/executor/mattermost_executor.go`, replacing template rendering with:

```go
message, err := executor.RenderTemplate(templateVal, msg)
```

Import:

```go
"project/pkg/executor"
"project/pkg/ruleengine"
```

- [ ] **Step 5: Move Confluence provider into adapter package**

Move `internal/ruleengine/confluence_provider.go` to `internal/adapters/confluence/provider.go`, change imports to `project/pkg/ruleengine`, and keep the provider constructor:

```go
func NewProvider(name, workflow string, serviceManager *manager.ServiceManager) *Provider
```

- [ ] **Step 6: Remove legacy workflow files**

Delete:

```bash
git rm internal/workflow/workflow_a.go
git rm internal/workflow/workflow_c.go
```

- [ ] **Step 7: Update workflow engine executor registration**

In `internal/workflow/workflow_engine.go`, register:

```go
executor.NewLogExecutor()
executor.NewHTTPExecutor()
badgedb.NewExecutor(sm)
mattermost.NewExecutor(sm)
```

and use:

```go
confluence.NewProvider("confluence", ruleengine.DefaultWorkflowName, sm)
```

- [ ] **Step 8: Run adapter and root tests**

Run:

```bash
go test ./internal/adapters/... ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/adapters internal/workflow internal/executor
git commit -m "refactor: move business integrations to adapters"
```

---

## Task 6: Rewire Reference Runtime

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/admin.go`
- Modify: `cmd/main.go`
- Modify: `pkg/di/container.go`
- Modify: `internal/workflow/factories.go`

- [ ] **Step 1: Write app wiring test for config-driven admin address**

Create `internal/app/app_test.go`:

```go
package app

import (
	"testing"

	"project/pkg/config"
)

func TestNewUsesRuntimeConfigAdminAddress(t *testing.T) {
	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"

	application := New("test", cfg, "config/rule-engine.yaml", "config/workflow-rules.yaml")
	if application.AdminAddress() != "127.0.0.1:0" {
		t.Fatalf("admin address = %q, want 127.0.0.1:0", application.AdminAddress())
	}
}
```

- [ ] **Step 2: Run app test and verify it fails**

Run:

```bash
go test ./internal/app
```

Expected: FAIL with missing `New`.

- [ ] **Step 3: Create application struct**

Create `internal/app/app.go`:

```go
package app

import (
	"context"

	"project/internal/manager"
	"project/pkg/config"
	"project/pkg/health"
	"project/pkg/runtime"
)

type App struct {
	name        string
	config      config.RuntimeConfig
	ruleConfig  string
	rulePath    string
	adminServer *runtime.AdminServer
	serviceMgr  *manager.ServiceManager
	healthMgr   *health.HealthManager
}

func New(name string, cfg config.RuntimeConfig, ruleConfigPath, rulePath string) *App {
	return &App{name: name, config: cfg, ruleConfig: ruleConfigPath, rulePath: rulePath}
}

func (a *App) AdminAddress() string {
	return a.config.Admin.Address
}

func (a *App) Start(ctx context.Context) error {
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	if a.adminServer != nil {
		return a.adminServer.Stop(ctx)
	}
	return nil
}
```

- [ ] **Step 4: Move admin handlers from `cmd/main.go` into `internal/app/admin.go`**

Create `internal/app/admin.go` with functions equivalent to existing:

- `newAdminMux`
- `servicesList`
- `workflowsList`
- `ruleEngineInfo`

Keep response schemas compatible with current `/services`, `/workflows`, and `/rule-engine`.

- [ ] **Step 5: Replace `cmd/main.go` with a thin entry point**

Replace `cmd/main.go` content with:

```go
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"project/internal/app"
	"project/pkg/config"
	"project/pkg/logger"
)

const (
	appName    = "service-workflow"
	appVersion = "1.0.0"
)

func main() {
	logger.InitFromEnv()

	runtimeConfigPath := getenv("RUNTIME_CONFIG", "config/runtime.yaml")
	ruleConfigPath := getenv("RULE_ENGINE_CONFIG", "config/rule-engine.yaml")
	rulePath := getenv("WORKFLOW_RULES", "config/workflow-rules.yaml")

	cfg, err := config.LoadRuntimeConfig(runtimeConfigPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load runtime config")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	application := app.New(appName, cfg, ruleConfigPath, rulePath)
	if err := application.Start(ctx); err != nil {
		logger.WithError(err).Fatal("Application startup failed")
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := application.Stop(shutdownCtx); err != nil {
		logger.WithError(err).Error("Application shutdown failed")
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
```

- [ ] **Step 6: Fill `App.Start` with existing wiring**

Move current startup wiring from `cmd/main.go` into `internal/app.App.Start`:

- initialize service manager
- register services from runtime config
- start services
- create workflow manager
- register message listeners
- create health manager
- create admin mux
- start `runtime.AdminServer`

Return an error from `Start` if any critical component fails. If admin server startup fails, call `Stop` before returning the error.

- [ ] **Step 7: Run app and root tests**

Run:

```bash
go test ./internal/app ./...
```

Expected: PASS.

- [ ] **Step 8: Build reference app**

Run:

```bash
go build ./cmd/service-workflow
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add cmd internal/app pkg/di internal/workflow
git commit -m "refactor: rewire reference runtime"
```

---

## Task 7: Add Local Development Automation

**Files:**
- Create: `Makefile`
- Create: `docker-compose.yml`
- Modify: `fake-server/cmd/fake-server/main.go`
- Modify: `config/workflow-rules.yaml`

- [ ] **Step 1: Add Makefile**

Create `Makefile`:

```makefile
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
	@echo "Start fake-server in one terminal with: make fake-server"
	@echo "Start the runtime in another terminal with: make run"
	@echo "Verify health with: curl -s http://localhost:8080/health"
	@echo "Send a sample message with: curl -s -X POST http://localhost:8093/api/send -H 'Content-Type: application/json' -d '{\"id\":\"m1\",\"type\":\"AAA\",\"content\":\"hello from make dev\",\"user_id\":\"u1\",\"timestamp\":\"2026-06-20T00:00:00Z\"}'"

clean:
	rm -rf bin
```

This `make dev` prints deterministic commands instead of starting long-running processes in the same shell. This avoids orphaned processes and keeps local logs readable.

- [ ] **Step 2: Add Docker Compose**

Create `docker-compose.yml`:

```yaml
services:
  fake-server:
    build:
      context: ./fake-server
    ports:
      - "8090:8090"
      - "8091:8091"
      - "8092:8092"
      - "8093:8093"

  service-workflow:
    build:
      context: .
    environment:
      ADMIN_ADDR: ":8080"
      WEBSOCKET_SERVER_URL: "ws://fake-server:8093"
      CONFLUENCE_API_ENDPOINT: "http://fake-server:8090"
      MATTERMOST_SERVER_URL: "http://fake-server:8091"
      MATTERMOST_WS_URL: "ws://fake-server:8092"
    ports:
      - "8080:8080"
    depends_on:
      - fake-server
```

- [ ] **Step 3: Add Dockerfiles if missing**

Create root `Dockerfile`:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/service-workflow ./cmd/service-workflow

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/service-workflow /app/service-workflow
COPY config /app/config
EXPOSE 8080
ENTRYPOINT ["/app/service-workflow"]
```

Create `fake-server/Dockerfile`:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/fake-server ./cmd/fake-server

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/fake-server /fake-server
EXPOSE 8090 8091 8092 8093
ENTRYPOINT ["/fake-server"]
```

- [ ] **Step 4: Run local test commands**

Run:

```bash
make test
make test-fake
make build
```

Expected: all commands PASS.

- [ ] **Step 5: Commit**

```bash
git add Makefile docker-compose.yml Dockerfile fake-server/Dockerfile config/workflow-rules.yaml
git commit -m "feat: add local development automation"
```

---

## Task 8: Rewrite Documentation and Remove Stale Content

**Files:**
- Modify: `README.md`
- Create: `docs/configuration.md`
- Create: `docs/rule-engine.md`
- Create: `docs/executors.md`
- Create: `docs/adapters.md`
- Create: `docs/development.md`
- Modify: `config/config.yaml`

- [ ] **Step 1: Replace README with framework-focused content**

Rewrite `README.md` with these sections:

```markdown
# Go Service Framework

A configurable, rule-driven Go service framework for building event processing runtimes. The framework provides a reusable rule engine, executor SDK, runtime lifecycle helpers, structured logging, health checks, and a runnable reference application.

## Quick Start

```bash
make test
make test-fake
make build
make fake-server
```

In another terminal:

```bash
make run
```

Verify:

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/rule-engine
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m1","type":"AAA","content":"hello from README","user_id":"u1","timestamp":"2026-06-20T00:00:00Z"}'
```

## What Is Core

- `pkg/ruleengine`
- `pkg/executor`
- `pkg/runtime`
- `pkg/config`
- `pkg/logger`
- `pkg/health`

## What Is Demo

- WebSocket input adapter
- Confluence rule provider adapter
- Mattermost executor adapter
- BadgeDB demo executor

## Configuration

- `config/runtime.yaml`
- `config/rule-engine.yaml`
- `config/workflow-rules.yaml`

## Documentation

- `docs/configuration.md`
- `docs/rule-engine.md`
- `docs/executors.md`
- `docs/adapters.md`
- `docs/development.md`
```

- [ ] **Step 2: Add focused docs**

Create `docs/configuration.md` describing every field in `config/runtime.yaml`, `config/rule-engine.yaml`, and `config/workflow-rules.yaml`.

Create `docs/rule-engine.md` with examples for `eq`, `contains`, `regex`, `single`, `or`, `and`, and `pipeline`.

Create `docs/executors.md` with examples for `log` and `http`, plus the `Executor` interface.

Create `docs/adapters.md` explaining WebSocket, Confluence, Mattermost, and BadgeDB as demo adapters.

Create `docs/development.md` with `make` commands, Docker Compose commands, verification curls, and shutdown instructions.

- [ ] **Step 3: Remove stale config file or make it clearly legacy**

If `config/config.yaml` is no longer loaded, replace its content with:

```yaml
# This file is retained only for migration notes.
# The reference runtime loads config/runtime.yaml, config/rule-engine.yaml, and config/workflow-rules.yaml.
```

- [ ] **Step 4: Search for stale references**

Run:

```bash
rg -n "WorkflowA|WorkflowC|logrus|Go 1\\.22|其他|Other necessary imports|In actual implementation|In a real implementation|For now|simulate" README.md docs config internal pkg
```

Expected: no stale README/config references. Remaining code comments must be rewritten in English with concrete descriptions or removed.

- [ ] **Step 5: Run tests and build**

Run:

```bash
go test ./...
cd fake-server && go test ./...
cd ..
go build ./cmd/service-workflow
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add README.md docs config internal pkg
git commit -m "docs: document rule-driven service framework"
```

---

## Task 9: Final Verification and Cleanup

**Files:**
- Modify files only where verification exposes stale references, broken commands, or failing tests.

- [ ] **Step 1: Run full test suite**

Run:

```bash
go test ./...
cd fake-server && go test ./...
cd ..
```

Expected: PASS.

- [ ] **Step 2: Build binaries**

Run:

```bash
go build ./cmd/service-workflow
cd fake-server && go build ./cmd/fake-server
cd ..
```

Expected: PASS.

- [ ] **Step 3: Verify local runtime manually**

Terminal 1:

```bash
make fake-server
```

Terminal 2:

```bash
ADMIN_ADDR=:18080 make run
```

Terminal 3:

```bash
curl -s http://localhost:18080/health
curl -s http://localhost:18080/rule-engine
curl -s -X POST http://localhost:8093/api/send \
  -H 'Content-Type: application/json' \
  -d '{"id":"m1","type":"AAA","content":"hello final verification","user_id":"u1","timestamp":"2026-06-20T00:00:00Z"}'
```

Expected:

- health endpoint returns JSON
- rule engine endpoint returns configured providers and executors
- service logs show a matched YAML rule or executed log action

- [ ] **Step 4: Verify admin bind failure**

Terminal 1:

```bash
ADMIN_ADDR=:18081 make run
```

Terminal 2:

```bash
ADMIN_ADDR=:18081 make run
```

Expected: the second process exits with an address-in-use startup error and does not keep services running.

- [ ] **Step 5: Verify Docker Compose**

Run:

```bash
docker compose up --build
```

In another terminal:

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/rule-engine
```

Expected: both endpoints return JSON.

- [ ] **Step 6: Final stale reference scan**

Run:

```bash
rg -n "WorkflowA|WorkflowC|Go 1\\.22|其他|Other necessary imports|T[O]DO|T[B]D|F[I]XME" .
```

Expected: no stale references in active source, config, or docs. Historical spec and plan files may reference old workflow names as migration context.

- [ ] **Step 7: Commit final cleanup**

```bash
git add .
git commit -m "chore: finalize service framework migration"
```

---

## Self-Review Notes

- Spec coverage: SDK extraction is covered by Tasks 1 and 2. Runtime configuration is covered by Task 3. Fail-fast admin startup is covered by Task 4. Adapter separation and legacy workflow migration are covered by Task 5. Runtime wiring is covered by Task 6. Make and Compose support are covered by Task 7. README and docs are covered by Task 8. Verification is covered by Task 9.
- Placeholder scan: the plan avoids unfinished markers and gives concrete paths, commands, expected outcomes, and code snippets for the important new APIs.
- Type consistency: public message type is `ruleengine.Message`; public action type is `ruleengine.Action`; executor interface is `pkg/executor.Executor`; runtime config type is `config.RuntimeConfig`.
