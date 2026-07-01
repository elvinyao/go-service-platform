package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGetenvReturnsFallbackWhenUnset(t *testing.T) {
	t.Setenv("SERVICE_WORKFLOW_TEST_ENV", "")

	got := getenv("SERVICE_WORKFLOW_TEST_ENV", "fallback")
	if got != "fallback" {
		t.Fatalf("getenv = %q, want fallback", got)
	}
}

func TestGetenvReturnsEnvironmentValue(t *testing.T) {
	t.Setenv("SERVICE_WORKFLOW_TEST_ENV", "configured")

	got := getenv("SERVICE_WORKFLOW_TEST_ENV", "fallback")
	if got != "configured" {
		t.Fatalf("getenv = %q, want configured", got)
	}
}

func TestFatalOnErrorIgnoresNil(t *testing.T) {
	fatalOnError(nil)
}

func TestFatalOnErrorCallsFatal(t *testing.T) {
	called := false
	oldFatal := fatal
	fatal = func(err error) {
		called = true
		if err == nil {
			t.Fatalf("fatal err = nil")
		}
	}
	defer func() { fatal = oldFatal }()

	fatalOnError(errors.New("boom"))
	if !called {
		t.Fatalf("fatal was not called")
	}
}

func TestMainHandlesInterrupt(t *testing.T) {
	dir := t.TempDir()
	runtimeConfig := filepath.Join(dir, "runtime.yaml")
	ruleConfig := filepath.Join(dir, "rule-engine.yaml")
	rules := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(runtimeConfig, []byte(`admin:
  address: "127.0.0.1:0"
`), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	if err := os.WriteFile(ruleConfig, []byte(`workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
`), 0o600); err != nil {
		t.Fatalf("write rule config: %v", err)
	}
	if err := os.WriteFile(rules, []byte(`version: "1"
rules: []
`), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG", runtimeConfig)
	t.Setenv("RULE_ENGINE_CONFIG", ruleConfig)
	t.Setenv("WORKFLOW_RULES", rules)

	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()

	time.Sleep(250 * time.Millisecond)
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal interrupt: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("main did not return after interrupt")
	}
}

func TestServiceWorkflowBinaryBuilds(t *testing.T) {
	output := filepath.Join(t.TempDir(), "service-workflow")
	cmd := exec.Command("go", "build", "-o", output, ".")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}
}

func TestRunReturnsConfigLoadError(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))

	err := run(context.Background())
	if err == nil {
		t.Fatalf("expected config load error")
	}
}

func TestRunReturnsApplicationStartError(t *testing.T) {
	dir := t.TempDir()
	runtimeConfig := filepath.Join(dir, "runtime.yaml")
	ruleConfig := filepath.Join(dir, "bad-rule-engine.yaml")
	rules := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(runtimeConfig, []byte(`admin:
  address: "127.0.0.1:0"
`), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	if err := os.WriteFile(ruleConfig, []byte(":"), 0o600); err != nil {
		t.Fatalf("write rule config: %v", err)
	}
	if err := os.WriteFile(rules, []byte(`version: "1"
rules: []
`), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG", runtimeConfig)
	t.Setenv("RULE_ENGINE_CONFIG", ruleConfig)
	t.Setenv("WORKFLOW_RULES", rules)

	err := run(context.Background())
	if err == nil {
		t.Fatalf("expected application start error")
	}
}

func TestRunStartsAndStopsWithRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	runtimeConfig := filepath.Join(dir, "runtime.yaml")
	ruleConfig := filepath.Join(dir, "rule-engine.yaml")
	rules := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(runtimeConfig, []byte(`admin:
  address: "127.0.0.1:0"
inputs:
  websocket:
    enabled: true
    server_url: "ws://127.0.0.1:1"
    path: "/ws"
    reconnect_interval: "10ms"
adapters:
  confluence:
    enabled: true
    api_endpoint: "http://127.0.0.1:1"
    settings_page_id: "settings-page-1"
    refresh_interval: "1h"
  mattermost:
    enabled: true
    server_url: "http://127.0.0.1:1"
    websocket_url: "ws://127.0.0.1:1"
    api_token: "test-token"
    channel: "test-channel"
  badgedb:
    enabled: true
    path: ""
`), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	if err := os.WriteFile(ruleConfig, []byte(`workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
`), 0o600); err != nil {
		t.Fatalf("write rule config: %v", err)
	}
	if err := os.WriteFile(rules, []byte(`version: "1"
rules: []
`), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG", runtimeConfig)
	t.Setenv("RULE_ENGINE_CONFIG", ruleConfig)
	t.Setenv("WORKFLOW_RULES", rules)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()

	if err := run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
}
