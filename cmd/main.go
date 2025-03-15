package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"project/internal/dataaccess"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/internal/workflow"
	appctx "project/pkg/context"
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

	// Initialize dependencies
	serviceManager := initServiceManager(rootCtx)
	dataAccessor := initDataAccessor(rootCtx)

	// Register and start services
	startCtx := appctx.WithOperationName(rootCtx, "startup")
	if err := registerAndStartServices(startCtx, serviceManager, dataAccessor); err != nil {
		logger.WithContextError(rootCtx, err).Error("Failed to register and start services")
		os.Exit(1)
	}

	// Start service monitoring
	monitorCtx := appctx.WithOperationName(cancelCtx, "monitoring")
	go monitorServices(monitorCtx, serviceManager)

	// Initialize and setup workflow manager
	workflowManager, err := initWorkflowManager(rootCtx, serviceManager)
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

	// 创建HTTP管理服务器
	adminServer := createAdminServer(rootCtx, serviceManager, workflowManager)
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

// Initialize service manager
func initServiceManager(ctx context.Context) *manager.ServiceManager {
	ctx = appctx.WithOperationName(ctx, "init_service_manager")
	logger.InfoWithContext(ctx, "Initializing service manager")
	return manager.NewServiceManager(appVersion)
}

// Initialize data accessor
func initDataAccessor(ctx context.Context) *dataaccess.CacheDataAccessor {
	ctx = appctx.WithOperationName(ctx, "init_data_accessor")
	logger.InfoWithContext(ctx, "Initializing data accessor")
	return dataaccess.NewCacheDataAccessor()
}

// Register and start services
func registerAndStartServices(ctx context.Context, serviceManager *manager.ServiceManager, dataAccessor dataaccess.DataAccessor) error {
	return logger.LogOperation(ctx, "register_and_start_services", func(ctx context.Context) error {
		logger.InfoWithContext(ctx, "Registering services")

		services := []service.Service{
			service.NewConfluenceService("ConfluenceServiceA", "A", dataAccessor, appVersion),
			service.NewWebSocketService("WebSocketServiceA", "A", appVersion),
			service.NewBadgeDBService("BadgeDBService", "Global", "/tmp/badges.db", appVersion),
		}

		for _, svc := range services {
			serviceManager.RegisterService(svc)
			logger.InfofWithContext(ctx, "Registered service: %s", svc.GetName())
		}

		logger.InfoWithContext(ctx, "Starting all services")
		if err := serviceManager.StartAll(ctx); err != nil {
			return errors.Wrap(err, "Failed to start services", errors.TypeServiceUnavailable)
		}

		logger.InfoWithContext(ctx, "All services started successfully")
		return nil
	})
}

// Initialize workflow manager
func initWorkflowManager(ctx context.Context, serviceManager *manager.ServiceManager) (*manager.WorkflowManager, error) {
	var wfManager *manager.WorkflowManager
	var wfError error

	err := logger.LogOperation(ctx, "init_workflow_manager", func(ctx context.Context) error {
		logger.InfoWithContext(ctx, "Initializing workflow manager")
		workflowManager := manager.NewWorkflowManager()

		workflowA := workflow.NewWorkflowA(serviceManager)

		if err := workflowManager.RegisterWorkflow(workflowA); err != nil {
			wfError = errors.Wrap(err, "Failed to register workflow", errors.TypeInternal).
				WithField("workflow", workflowA.GetName())
			return wfError
		}

		logger.InfofWithContext(ctx, "Registered workflow: %s", workflowA.GetName())

		wfManager = workflowManager
		return nil
	})

	if err != nil {
		return nil, err
	}

	if wfError != nil {
		return nil, wfError
	}

	return wfManager, nil
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
func createAdminServer(ctx context.Context, serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager) *http.Server {
	ctx = appctx.WithOperationName(ctx, "create_admin_server")
	logger.InfoWithContext(ctx, "Creating admin server")

	// 创建HTTP路由
	mux := http.NewServeMux()

	// 创建健康检查处理器
	healthManager := health.NewHealthManager(30*time.Second, appVersion)
	healthHandler := health.NewHealthHandler(healthManager)
	healthHandler.RegisterHTTPHandlers(mux)

	// 返回配置好的服务器
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	logger.InfoWithContext(ctx, "Admin server created at :8080")
	return server
}
