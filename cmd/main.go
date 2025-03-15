package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	appctx "project/pkg/context"
	"project/pkg/di"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	"syscall"
	"time"
	// 其他必要的导入
)

const (
	// 应用版本
	appVersion = "1.0.0"
)

func main() {
	// Initialize logger
	logger.Init()

	// Create our application context with request ID
	rootCtx := appctx.NewContext(context.Background())
	cancelCtx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	appName := "service-workflow"
	rootCtx = appctx.WithServiceName(rootCtx, appName)

	logger.InfoWithContext(rootCtx, "Starting application")

	// Set up signal handling for graceful shutdown
	setupSignalHandling(rootCtx, cancel)

	// 创建依赖注入容器
	container := di.NewContainer(appVersion)

	// 初始化依赖
	startCtx := appctx.WithOperationName(rootCtx, "startup")
	serviceManager := container.GetServiceManager(startCtx)

	// 注册服务
	if err := container.RegisterServices(startCtx); err != nil {
		logger.WithContextError(rootCtx, err).Error("Failed to register services")
		os.Exit(1)
	}

	// 启动所有服务
	if err := serviceManager.StartAll(startCtx); err != nil {
		logger.WithContextError(rootCtx, err).Error("Failed to start services")
		os.Exit(1)
	}

	// Start service monitoring
	monitorCtx := appctx.WithOperationName(cancelCtx, "monitoring")
	go monitorServices(monitorCtx, serviceManager)

	// Initialize and setup workflow manager
	workflowManager, err := container.GetWorkflowManager(rootCtx)
	if err != nil {
		logger.WithContextError(rootCtx, err).Error("Failed to initialize workflow manager")
		shutdown(rootCtx, serviceManager)
		os.Exit(1)
	}

	// Set up message listeners
	if err := setupMessageListeners(rootCtx, serviceManager, workflowManager); err != nil {
		logger.WithContextError(rootCtx, err).Error("Failed to set up message listeners")
		shutdown(rootCtx, serviceManager)
		os.Exit(1)
	}

	// 创建健康检查管理器
	healthManager := container.GetHealthManager(rootCtx)

	// 创建HTTP管理服务器
	adminServer := createAdminServer(rootCtx, serviceManager, workflowManager, healthManager)
	go func() {
		if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithContextError(rootCtx, err).Fatal("Admin server failed")
		}
	}()

	logger.InfoWithContext(rootCtx, "Application started successfully")

	// Block until context is cancelled
	<-cancelCtx.Done()

	// Handle graceful shutdown
	shutdownCtx := appctx.WithOperationName(rootCtx, "shutdown")
	logger.InfoWithContext(shutdownCtx, "Shutting down application")
	shutdown(shutdownCtx, serviceManager)
	logger.InfoWithContext(shutdownCtx, "Application shutdown complete")
}

// setupSignalHandling configures the application to handle OS signals
func setupSignalHandling(ctx context.Context, cancel context.CancelFunc) {
	ctx = appctx.WithOperationName(ctx, "signal_handling")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.InfofWithContext(ctx, "Received signal: %v", sig)
		cancel()
	}()

	logger.DebugWithContext(ctx, "Signal handlers set up")
}

// shutdown performs a graceful shutdown of all services
func shutdown(ctx context.Context, serviceManager *manager.ServiceManager) {
	ctx = appctx.WithOperationName(ctx, "graceful_shutdown")

	// Allow up to 10 seconds for graceful shutdown
	shutdownCtx, cancel := appctx.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := serviceManager.StopAll(shutdownCtx); err != nil {
		logger.WithContextError(ctx, err).Error("Error during service shutdown")
	}

	// Wait for context to be done, either by timeout or successful shutdown
	<-shutdownCtx.Done()

	if shutdownCtx.Err() == context.DeadlineExceeded {
		logger.WarnWithContext(ctx, "Shutdown timed out, forcing exit")
	}
}

// monitorServices starts the service monitoring in a separate goroutine
func monitorServices(ctx context.Context, serviceManager *manager.ServiceManager) {
	logger.InfoWithContext(ctx, "Starting service monitoring")
	serviceManager.MonitorServices(ctx)
	logger.InfoWithContext(ctx, "Service monitoring stopped")
}

// Set up message listeners
func setupMessageListeners(ctx context.Context, serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager) error {
	return logger.LogOperation(ctx, "setup_message_listeners", func(ctx context.Context) error {
		logger.InfoWithContext(ctx, "Setting up message listeners")

		svc, ok := serviceManager.GetServiceByName(ctx, "WebSocketServiceA")
		if !ok {
			return errors.New(errors.TypeNotFound, "Service not found: WebSocketServiceA", nil)
		}

		websocketServiceA, ok := svc.(*service.WebSocketService)
		if !ok {
			return errors.New(errors.TypeInternal,
				"Service is not of type *service.WebSocketService", nil).
				WithField("actual_type", fmt.Sprintf("%T", svc))
		}

		websocketServiceA.OnMessage(func(msg model.Message) {
			// Create a new request context for each message
			msgCtx := appctx.NewContext(ctx)

			if err := workflowManager.DispatchMessage(msgCtx, msg); err != nil {
				logger.WithContextError(msgCtx, err).Errorf("Error dispatching message: %+v", msg)
			}
		})

		logger.InfoWithContext(ctx, "Message listeners set up successfully")
		return nil
	})
}

// 创建HTTP管理服务器
func createAdminServer(ctx context.Context, serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager, healthManager *health.HealthManager) *http.Server {
	ctx = appctx.WithOperationName(ctx, "create_admin_server")
	logger.InfoWithContext(ctx, "Creating admin server")

	// 创建HTTP路由
	mux := http.NewServeMux()

	// 添加健康检查端点
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		healthCheck(w, r, serviceManager, healthManager)
	})

	// 添加服务管理端点
	mux.HandleFunc("/services", func(w http.ResponseWriter, r *http.Request) {
		servicesList(w, r, serviceManager)
	})

	// 添加工作流端点
	mux.HandleFunc("/workflows", func(w http.ResponseWriter, r *http.Request) {
		workflowsList(w, r, workflowManager)
	})

	// 配置HTTP服务器
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.InfoWithContext(ctx, "Admin server created successfully")
	return server
}

// healthCheck 处理健康检查请求
func healthCheck(w http.ResponseWriter, r *http.Request, serviceManager *manager.ServiceManager, healthManager *health.HealthManager) {
	ctx := appctx.FromRequest(r)

	// 创建系统健康报告
	report := health.Report{
		ServiceName:  "system",
		Status:       health.StatusUp,
		CheckResults: []health.CheckResult{},
		StartTime:    time.Now(),
		Version:      appVersion,
		RefreshedAt:  time.Now(),
	}

	// 检查服务状态
	allServices := serviceManager.ListServices(ctx)

	// 添加服务状态到报告
	for _, service := range allServices {
		result := health.CheckResult{
			Name:      "service." + service.GetName(),
			Status:    health.StatusUnknown,
			Category:  health.CategoryDependency,
			Level:     health.LevelCritical,
			Timestamp: time.Now(),
		}

		if service.IsRunning(ctx) {
			result.Status = health.StatusUp
			result.Description = "Service is running"
		} else {
			result.Status = health.StatusDown
			result.Description = "Service is not running"
		}

		report.CheckResults = append(report.CheckResults, result)
	}

	// 根据服务状态确定总体状态
	for _, result := range report.CheckResults {
		if result.Status == health.StatusDown {
			report.Status = health.StatusDegraded
			break
		}
	}

	// 设置适当的状态码
	if report.Status != health.StatusUp {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// 设置内容类型
	w.Header().Set("Content-Type", "application/json")

	// 将健康报告编码为JSON
	if err := json.NewEncoder(w).Encode(report); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode health report")
	}
}

// servicesList 处理服务列表请求
func servicesList(w http.ResponseWriter, r *http.Request, serviceManager *manager.ServiceManager) {
	ctx := appctx.FromRequest(r)

	// 获取所有服务
	services := serviceManager.ListServices(ctx)

	// 创建简化的服务信息列表
	type ServiceInfo struct {
		Name     string                 `json:"name"`
		Running  bool                   `json:"running"`
		Workflow string                 `json:"workflow"`
		Type     string                 `json:"type"`
		Metrics  map[string]interface{} `json:"metrics,omitempty"`
	}

	serviceInfos := make([]ServiceInfo, 0, len(services))

	for _, svc := range services {
		info := ServiceInfo{
			Name:     svc.GetName(),
			Running:  svc.IsRunning(ctx),
			Workflow: svc.GetWorkflow(),
			Type:     svc.GetType(),
			Metrics:  svc.GetMetrics(ctx),
		}

		serviceInfos = append(serviceInfos, info)
	}

	// 设置内容类型
	w.Header().Set("Content-Type", "application/json")

	// 将服务列表编码为JSON
	if err := json.NewEncoder(w).Encode(serviceInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode services list")
	}
}

// workflowsList 处理工作流列表请求
func workflowsList(w http.ResponseWriter, r *http.Request, workflowManager *manager.WorkflowManager) {
	// 设置内容类型
	w.Header().Set("Content-Type", "application/json")

	// 暂时只返回简单消息，因为WorkflowManager尚未提供获取所有工作流的方法
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		ctx := appctx.FromRequest(r)
		logger.WithContextError(ctx, err).Error("Failed to encode workflows response")
	}
}
