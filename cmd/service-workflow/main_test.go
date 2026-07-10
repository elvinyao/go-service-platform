package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/config"
)

type fakeLifecycleApplication struct {
	startErr error
	stopErr  error
	starts   int
	stops    int
}

func (a *fakeLifecycleApplication) Start(context.Context) error {
	a.starts++
	return a.startErr
}

func (a *fakeLifecycleApplication) Stop(context.Context) error {
	a.stops++
	return a.stopErr
}

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

func TestMainReportsRunErrorThroughFatal(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))

	oldFatal := fatal
	var fatalErr error
	fatal = func(err error) {
		fatalErr = err
	}
	defer func() { fatal = oldFatal }()

	main()
	if fatalErr == nil {
		t.Fatalf("fatal error = nil")
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
	t.Setenv("RUNTIME_CONFIG", writeRuntimeConfig(t))
	application := &fakeLifecycleApplication{startErr: errors.New("start failed")}
	replaceApplicationFactory(t, func(string, config.RuntimeConfig, string, string) lifecycleApplication {
		return application
	})

	err := run(context.Background())
	if err == nil || application.starts != 1 || application.stops != 0 {
		t.Fatalf("run error/start/stop = %v/%d/%d", err, application.starts, application.stops)
	}
}

func TestRunStartsAndStopsWithRuntimeConfig(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG", writeRuntimeConfig(t))
	t.Setenv("RULE_ENGINE_CONFIG", "custom-engine.yaml")
	t.Setenv("WORKFLOW_RULES", "custom-rules.yaml")
	application := &fakeLifecycleApplication{}
	var gotName, gotEnginePath, gotRulesPath string
	replaceApplicationFactory(t, func(name string, cfg config.RuntimeConfig, enginePath, rulesPath string) lifecycleApplication {
		gotName = name
		gotEnginePath = enginePath
		gotRulesPath = rulesPath
		if cfg.Admin.Address != "127.0.0.1:18080" {
			t.Fatalf("admin address = %q", cfg.Admin.Address)
		}
		return application
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if application.starts != 1 || application.stops != 1 {
		t.Fatalf("start/stop calls = %d/%d", application.starts, application.stops)
	}
	if gotName != appName || gotEnginePath != "custom-engine.yaml" || gotRulesPath != "custom-rules.yaml" {
		t.Fatalf("factory args = %q/%q/%q", gotName, gotEnginePath, gotRulesPath)
	}
}

func TestRunReturnsApplicationStopError(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG", writeRuntimeConfig(t))
	application := &fakeLifecycleApplication{stopErr: errors.New("stop failed")}
	replaceApplicationFactory(t, func(string, config.RuntimeConfig, string, string) lifecycleApplication {
		return application
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := run(ctx)

	if err == nil || application.starts != 1 || application.stops != 1 {
		t.Fatalf("run error/start/stop = %v/%d/%d", err, application.starts, application.stops)
	}
}

func replaceApplicationFactory(
	t *testing.T,
	factory func(string, config.RuntimeConfig, string, string) lifecycleApplication,
) {
	t.Helper()
	previous := newApplication
	newApplication = factory
	t.Cleanup(func() { newApplication = previous })
}

func writeRuntimeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	if err := os.WriteFile(path, []byte("admin:\n  address: \"127.0.0.1:18080\"\n"), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	return path
}
