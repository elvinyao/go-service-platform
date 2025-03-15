// internal/interfaces/srvinterface.go
package interfaces

import (
	"context"
	"project/internal/service"
)

type ServiceManager interface {
	GetServiceByName(ctx context.Context, name string) (service.Service, bool)
	GetServicesByWorkflowAndType(ctx context.Context, workflow string, serviceType string) []service.Service
	StartAll(ctx context.Context) error
	StopAll(ctx context.Context) error
	MonitorServices(ctx context.Context)
	// 其他方法
}
