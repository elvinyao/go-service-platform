package manager

import (
	"context"
	"project/internal/interfaces"
	"project/internal/service"
	"project/pkg/logger"
	"sync"
	"time"
)

type ServiceManager struct {
	services map[string]service.Service
	mu       sync.Mutex
}

// 确保 ServiceManager 实现了 interfaces.ServiceManager 接口
var _ interfaces.ServiceManager = (*ServiceManager)(nil)

func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		services: make(map[string]service.Service),
	}
}

func (sm *ServiceManager) RegisterService(s service.Service) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.services[s.GetName()] = s
}

func (sm *ServiceManager) StartAll(ctx context.Context) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, svc := range sm.services {
		go sm.startService(ctx, svc)
	}
}

func (sm *ServiceManager) startService(ctx context.Context, s service.Service) {
	select {
	case <-ctx.Done():
		return
	default:
		if err := s.Start(ctx); err != nil {
			logger.Errorf("Failed to start service %s: %v", s.GetName(), err)
		}
	}
}

// 新增方法：根据服务名称获取服务
func (sm *ServiceManager) GetServiceByName(name string) (service.Service, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s, ok := sm.services[name]
	return s, ok
}

// 新增方法：根据工作流和服务类型获取服务列表
func (sm *ServiceManager) GetServicesByWorkflowAndType(workflow string, serviceType string) []service.Service {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var result []service.Service
	for _, s := range sm.services {
		if s.GetWorkflow() == workflow && s.GetType() == serviceType {
			result = append(result, s)
		}
	}
	return result
}
func (sm *ServiceManager) MonitorServices(ctx context.Context) {
	for {
		sm.mu.Lock()
		for _, service := range sm.services {
			if !service.IsRunning() {
				logger.Warnf("Service %s is not running, restarting...", service.GetName())
				if err := service.Restart(ctx); err != nil {
					logger.Errorf("Failed to restart service %s: %v", service.GetName(), err)
				}
			}
		}
		sm.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// 其他方法，例如根据工作流和类型获取服务
