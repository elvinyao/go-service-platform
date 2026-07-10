package service

import (
	"context"
	"github.com/elvinyao/go-service-platform/pkg/health"
)

const (
	WebSocketInputServiceName     = "WebSocketInputService"
	BadgeDBServiceName            = "BadgeDBService"
	MattermostServiceName         = "MattermostService"
	ConfluenceSettingsServiceName = "ConfluenceSettingsService"
)

type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	GetName() string
	GetWorkflow() string
	GetType() string
	IsRunning(ctx context.Context) bool
	GetMetrics(ctx context.Context) map[string]interface{}
	Configure(ctx context.Context, config interface{}) error

	// Health check methods
	RegisterHealthChecks() []health.Checker
	ReportHealth(ctx context.Context, report *health.Report)
}
