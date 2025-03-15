// internal/interfaces/service_manager.go
package interfaces

import "project/internal/service"

type ServiceManager interface {
	GetServiceByName(name string) (service.Service, bool)
	GetServicesByWorkflowAndType(workflow string, serviceType string) []service.Service
	// 其他方法
}
