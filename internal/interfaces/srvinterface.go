// internal/interfaces/srvinterface.go
package interfaces

import (
	"context"
	"project/internal/service"
	"project/pkg/health"
)

type ServiceManager interface {
	GetServiceByName(ctx context.Context, name string) (service.Service, bool)
	GetServicesByWorkflowAndType(ctx context.Context, workflow string, serviceType string) []service.Service
	StartAll(ctx context.Context) error
	StopAll(ctx context.Context) error
	MonitorServices(ctx context.Context)

	// 获取所有服务列表
	ListServices(ctx context.Context) []service.Service

	// Health check methods
	GetServiceHealth(ctx context.Context, serviceName string) (health.Report, bool)
	GetAllServicesHealth(ctx context.Context) map[string]health.Report
	GetSystemHealth(ctx context.Context) health.Report
	// 其他方法
}
