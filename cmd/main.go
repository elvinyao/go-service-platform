package main

import (
	"context"
	"project/internal/dataaccess"
	"project/internal/interfaces"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/internal/workflow"
	"project/pkg/logger"
	// 其他必要的导入
)

func main() {
	logger.Init()

	// 创建一个根 context 和一个取消函数
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化服务管理器和数据访问器
	serviceManager := initServiceManager()
	dataAccessor := initDataAccessor()

	// 注册和启动服务
	registerAndStartServices(ctx, serviceManager, dataAccessor)

	// 启动服务监控
	go serviceManager.MonitorServices(ctx)

	workflowManager := initWorkflowManager(serviceManager)

	// 启动消息监听
	setupMessageListeners(serviceManager, workflowManager)

	// 阻塞主线程直到接收到取消信号
	<-ctx.Done()
}

// 初始化服务管理器
func initServiceManager() *manager.ServiceManager {
	return manager.NewServiceManager()
}

// 初始化数据访问器
func initDataAccessor() *dataaccess.CacheDataAccessor {
	return dataaccess.NewCacheDataAccessor()
}

// 注册和启动服务
func registerAndStartServices(ctx context.Context, serviceManager *manager.ServiceManager, dataAccessor dataaccess.DataAccessor) {
	services := []service.Service{
		service.NewConfluenceService("ConfluenceServiceA", "A", dataAccessor),
		service.NewWebSocketService("WebSocketServiceA", "A"),
		service.NewBadgeDBService("BadgeDBService", "Global"),
	}

	for _, svc := range services {
		serviceManager.RegisterService(svc)
	}
	serviceManager.StartAll(ctx)
}

// 初始化工作流管理器
func initWorkflowManager(serviceManager *manager.ServiceManager) *manager.WorkflowManager {
	workflowManager := manager.NewWorkflowManager()
	workflows := []interfaces.Workflow{
		workflow.NewWorkflowA(serviceManager),
	}

	for _, wf := range workflows {
		workflowManager.RegisterWorkflow(wf)
	}

	return workflowManager
}

// 设置消息监听
func setupMessageListeners(serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager) {
	svc, ok := serviceManager.GetServiceByName("WebSocketServiceA")
	if !ok {
		logger.Errorf("Service not found: WebSocketServiceA")
		return
	}

	websocketServiceA, ok := svc.(*service.WebSocketService)
	if !ok {
		logger.Errorf("Service is not of type *service.WebSocketService: %T", svc)
		return
	}

	websocketServiceA.OnMessage(func(msg model.Message) {
		workflowManager.DispatchMessage(msg)
	})
}
