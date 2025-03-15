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
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	// 其他必要的导入
)

const (
	// 应用版本
	appVersion = "1.0.0"
	// 优雅关闭超时 - 总体超时
	shutdownTimeout = 30 * time.Second
	// 服务停止超时 - 每个服务的超时
	serviceShutdownTimeout = 10 * time.Second
)

func main() {
	// Initialize logger with environment variables
	logger.InitFromEnv()

	// Log application start
	logger.WithFields(logrus.Fields{
		"version": appVersion,
		"pid":     os.Getpid(),
	}).Info("Application starting")

	// Create our application context with request ID
	rootCtx := appctx.NewContext(context.Background())

	// Setup cancellable context for shutdown
	cancelCtx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	appName := "service-workflow"
	rootCtx = appctx.WithServiceName(rootCtx, appName)

	logger.InfoWithContext(rootCtx, "Initializing application")

	// Set up signal handling for graceful shutdown
	signalChan := setupSignalHandling(rootCtx, cancel)

	// 创建依赖注入容器
	container := di.NewContainer(appVersion)
	startCtx := appctx.WithOperationName(rootCtx, "startup")

	// 初始化依赖
	serviceManager := container.GetServiceManager(startCtx)

	// 注册所有服务
	if err := container.RegisterServices(startCtx); err != nil {
		logger.WithContextError(rootCtx, err).Fatal("Failed to register services")
	}

	// 启动所有服务
	if err := serviceManager.StartAll(startCtx); err != nil {
		logger.WithContextError(rootCtx, err).Fatal("Failed to start services")
	}

	// Start service monitoring
	monitorCtx, monitorCancel := context.WithCancel(cancelCtx)
	defer monitorCancel()

	// Track when monitoring is done
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		monitorServices(monitorCtx, serviceManager)
	}()

	// Initialize and setup workflow manager
	workflowManager, err := container.GetWorkflowManager(rootCtx)
	if err != nil {
		logger.WithContextError(rootCtx, err).Fatal("Failed to initialize workflow manager")
	}

	// Set up message listeners
	if err := setupMessageListeners(rootCtx, serviceManager, workflowManager); err != nil {
		logger.WithContextError(rootCtx, err).Fatal("Failed to set up message listeners")
	}

	// 创建健康检查管理器
	healthManager := container.GetHealthManager(rootCtx)

	// 创建HTTP管理服务器
	adminServer := createAdminServer(rootCtx, serviceManager, workflowManager, healthManager)

	// Track when HTTP server is done
	var adminServerWg sync.WaitGroup
	adminServerWg.Add(1)
	go func() {
		defer adminServerWg.Done()
		logger.InfoWithContext(rootCtx, "Starting admin server on", adminServer.Addr)

		if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithContextError(rootCtx, err).Error("Admin server failed")
		}
	}()

	logger.InfoWithContext(rootCtx, "Application started successfully")

	// Wait for shutdown signal
	sig := <-signalChan
	logger.InfofWithContext(rootCtx, "Received signal: %v, initiating graceful shutdown", sig)

	// Create a context with timeout for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(rootCtx, shutdownTimeout)
	defer shutdownCancel()

	// Handle graceful shutdown
	shutdownSequence(shutdownCtx, serviceManager, adminServer, monitorCancel, &adminServerWg, &monitorWg)

	logger.InfoWithContext(rootCtx, "Application shutdown complete")
}

// setupSignalHandling configures the application to handle OS signals
func setupSignalHandling(ctx context.Context, cancel context.CancelFunc) chan os.Signal {
	ctx = appctx.WithOperationName(ctx, "signal_handling")

	// Create buffered channel to avoid signal loss
	sigChan := make(chan os.Signal, 3)

	// Register for SIGINT (Ctrl+C), SIGTERM (Docker stop/kill), and SIGQUIT
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	logger.DebugWithContext(ctx, "Signal handlers set up")

	return sigChan
}

// shutdownSequence performs a coordinated shutdown of all components
func shutdownSequence(ctx context.Context, serviceManager *manager.ServiceManager,
	adminServer *http.Server, monitorCancel context.CancelFunc,
	adminWg *sync.WaitGroup, monitorWg *sync.WaitGroup) {

	ctx = appctx.WithOperationName(ctx, "graceful_shutdown")
	logger.InfoWithContext(ctx, "Starting graceful shutdown sequence")

	// Step 1: Stop the monitoring first to avoid log spam during shutdown
	logger.InfoWithContext(ctx, "Stopping service monitoring")
	monitorCancel()

	// Wait for monitoring to complete
	monitorDone := make(chan struct{})
	go func() {
		monitorWg.Wait()
		close(monitorDone)
	}()

	// Wait with timeout
	select {
	case <-monitorDone:
		logger.InfoWithContext(ctx, "Service monitoring stopped successfully")
	case <-time.After(5 * time.Second):
		logger.WarnWithContext(ctx, "Timeout waiting for service monitoring to stop")
	}

	// Step 2: Shutdown the admin HTTP server
	logger.InfoWithContext(ctx, "Shutting down admin server")
	httpShutdownCtx, httpCancel := context.WithTimeout(ctx, 5*time.Second)
	defer httpCancel()

	if err := adminServer.Shutdown(httpShutdownCtx); err != nil {
		logger.WithContextError(ctx, err).Warn("Admin server shutdown error")
	}

	// Wait for HTTP server to complete
	httpDone := make(chan struct{})
	go func() {
		adminWg.Wait()
		close(httpDone)
	}()

	// Wait with timeout
	select {
	case <-httpDone:
		logger.InfoWithContext(ctx, "Admin server stopped successfully")
	case <-time.After(7 * time.Second):
		logger.WarnWithContext(ctx, "Timeout waiting for admin server to stop")
	}

	// Step 3: Shutdown all services
	logger.InfoWithContext(ctx, "Shutting down all services")
	shutdownServices(ctx, serviceManager)
}

// shutdown performs a graceful shutdown of all services
func shutdownServices(ctx context.Context, serviceManager *manager.ServiceManager) {
	ctx = appctx.WithOperationName(ctx, "services_shutdown")

	// Allow specified timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, serviceShutdownTimeout)
	defer cancel()

	// Create a channel to signal when services are stopped
	done := make(chan struct{})

	// Stop services in a goroutine to handle timeout
	go func() {
		if err := serviceManager.StopAll(shutdownCtx); err != nil {
			logger.WithContextError(ctx, err).Error("Error during service shutdown")
		}
		close(done)
	}()

	// Wait for either completion or timeout
	select {
	case <-done:
		logger.InfoWithContext(ctx, "All services stopped successfully")
	case <-shutdownCtx.Done():
		if shutdownCtx.Err() == context.DeadlineExceeded {
			logger.WarnWithContext(ctx, "Service shutdown timed out, some services may not have stopped gracefully")
		}
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

	// 配置HTTP服务器 with timeout settings
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
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
	logger.InfoWithContext(ctx, "Handling services list request")

	// 创建服务信息列表
	type ServiceInfo struct {
		Name     string                 `json:"name"`
		Running  bool                   `json:"running"`
		Workflow string                 `json:"workflow"`
		Type     string                 `json:"type"`
		Metrics  map[string]interface{} `json:"metrics,omitempty"`
	}

	services := serviceManager.ListServices(ctx)
	serviceInfos := make([]ServiceInfo, 0, len(services))

	for _, svc := range services {
		info := ServiceInfo{
			Name:    svc.GetName(),
			Running: svc.IsRunning(ctx),
			Type:    fmt.Sprintf("%T", svc),
		}

		// 获取服务指标
		if metricProvider, ok := svc.(interface {
			GetMetrics(ctx context.Context) map[string]interface{}
		}); ok {
			info.Metrics = metricProvider.GetMetrics(ctx)
		}

		// 获取关联工作流
		if workflowProvider, ok := svc.(interface {
			GetWorkflowName() string
		}); ok {
			info.Workflow = workflowProvider.GetWorkflowName()
		}

		serviceInfos = append(serviceInfos, info)
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")

	// Encode with error handling
	if err := json.NewEncoder(w).Encode(serviceInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode services list")
	}
}

// workflowsList 处理工作流列表请求
func workflowsList(w http.ResponseWriter, r *http.Request, workflowManager *manager.WorkflowManager) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling workflows list request")

	// 创建工作流信息列表
	type WorkflowInfo struct {
		Name string `json:"name"`
	}

	// 获取所有注册的工作流
	workflowNames := workflowManager.ListWorkflows(ctx)
	workflowInfos := make([]WorkflowInfo, 0, len(workflowNames))

	for _, name := range workflowNames {
		workflowInfos = append(workflowInfos, WorkflowInfo{Name: name})
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")

	// Encode with error handling
	if err := json.NewEncoder(w).Encode(workflowInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode workflows list")
	}
}
