package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elvinyao/go-service-platform/internal/app"
	"github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/logger"
)

const (
	appName         = "service-workflow"
	shutdownTimeout = 30 * time.Second
)

var fatal = func(err error) {
	logger.WithError(err).Fatal("Application failed")
}

type lifecycleApplication interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

var newApplication = func(name string, cfg config.RuntimeConfig, ruleConfigPath, rulePath string) lifecycleApplication {
	return app.New(name, cfg, ruleConfigPath, rulePath)
}

func main() {
	fatalOnError(run(context.Background()))
}

func fatalOnError(err error) {
	if err != nil {
		fatal(err)
	}
}

func run(parent context.Context) error {
	logger.InitFromEnv()

	runtimeConfigPath := getenv("RUNTIME_CONFIG", "config/runtime.yaml")
	ruleConfigPath := getenv("RULE_ENGINE_CONFIG", "config/rule-engine.yaml")
	rulePath := getenv("WORKFLOW_RULES", "config/workflow-rules.yaml")

	cfg, err := config.LoadRuntimeConfig(runtimeConfigPath)
	if err != nil {
		return fmt.Errorf("load runtime config: %w", err)
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	application := newApplication(appName, cfg, ruleConfigPath, rulePath)
	if err := application.Start(ctx); err != nil {
		return fmt.Errorf("start application: %w", err)
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := application.Stop(shutdownCtx); err != nil {
		return fmt.Errorf("stop application: %w", err)
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
