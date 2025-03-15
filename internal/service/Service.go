package service

import "context"

type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	GetName() string
	GetWorkflow() string
	GetType() string
	IsRunning() bool
	GetMetrics(ctx context.Context) map[string]interface{}
	Configure(ctx context.Context, config interface{}) error
}
