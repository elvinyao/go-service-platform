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
	appName         = "service-workflow"
	shutdownTimeout = 30 * time.Second
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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
