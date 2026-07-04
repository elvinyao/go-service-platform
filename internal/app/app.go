package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"project/internal/interfaces"
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/internal/workflow"
	"project/pkg/config"
	appctx "project/pkg/context"
	"project/pkg/di"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	appruntime "project/pkg/runtime"
)

const (
	appVersion             = "1.0.0"
	appStartOperationName  = "startup"
	serviceShutdownTimeout = 10 * time.Second
)

type App struct {
	name           string
	config         config.RuntimeConfig
	ruleConfigPath string
	rulePath       string

	container       *di.Container
	serviceManager  *manager.ServiceManager
	workflowManager *manager.WorkflowManager
	healthManager   *health.HealthManager
	adminServer     *appruntime.AdminServer

	monitorCancel context.CancelFunc
	monitorWg     sync.WaitGroup
}

func New(name string, cfg config.RuntimeConfig, ruleConfigPath, rulePath string) *App {
	return &App{
		name:           name,
		config:         cfg,
		ruleConfigPath: ruleConfigPath,
		rulePath:       rulePath,
	}
}

func (a *App) AdminAddress() string {
	return a.config.Admin.Address
}

func (a *App) Start(ctx context.Context) error {
	rootCtx := appctx.NewContext(ctx)
	rootCtx = appctx.WithServiceName(rootCtx, a.name)
	startCtx := appctx.WithOperationName(rootCtx, appStartOperationName)

	a.container = di.NewContainer(appVersion, a.config)
	a.serviceManager = a.container.GetServiceManager(startCtx)

	if err := a.container.RegisterServices(startCtx); err != nil {
		return errors.Wrap(err, "failed to register services", errors.TypeInternal)
	}

	if len(a.serviceManager.ListServices(startCtx)) > 0 {
		if err := a.serviceManager.StartAll(startCtx); err != nil {
			return errors.Wrap(err, "failed to start services", errors.TypeServiceUnavailable)
		}
	} else {
		logger.InfoWithContext(startCtx, "No runtime services enabled")
	}

	monitorCtx, monitorCancel := context.WithCancel(ctx)
	a.monitorCancel = monitorCancel
	a.monitorWg.Add(1)
	go func() {
		defer a.monitorWg.Done()
		monitorServices(monitorCtx, a.serviceManager)
	}()

	workflowManager, err := manager.NewWorkflowManagerWithDI(rootCtx, a.serviceManager, func(sm *manager.ServiceManager) (interfaces.Workflow, error) {
		return workflow.NewWorkflowEngine(rootCtx, sm, a.ruleConfigPath, a.rulePath, a.config)
	})
	if err != nil {
		_ = a.Stop(context.Background())
		return errors.Wrap(err, "failed to initialize workflow manager", errors.TypeInternal)
	}
	a.workflowManager = workflowManager

	if a.config.Inputs.WebSocket.Enabled {
		if err := setupMessageListeners(rootCtx, a.serviceManager, a.workflowManager); err != nil {
			_ = a.Stop(context.Background())
			return errors.Wrap(err, "failed to set up message listeners", errors.TypeInternal)
		}
	}

	a.healthManager = a.container.GetHealthManager(rootCtx)
	mux := newAdminMux(a.serviceManager, a.workflowManager, a.healthManager)
	a.adminServer = appruntime.NewAdminServer(a.config.Admin.Address, mux)
	if err := a.adminServer.Start(rootCtx); err != nil {
		_ = a.Stop(context.Background())
		return errors.Wrap(err, "failed to start admin server", errors.TypeServiceUnavailable)
	}

	logger.InfofWithContext(rootCtx, "Admin server started on %s", a.adminServer.Addr())
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	if a.healthManager != nil {
		a.healthManager.SetShuttingDown()
	}
	if a.monitorCancel != nil {
		a.monitorCancel()
		a.monitorWg.Wait()
	}
	if a.adminServer != nil {
		if err := a.adminServer.Stop(ctx); err != nil {
			logger.WithContextError(ctx, err).Warn("Admin server shutdown error")
		}
	}
	if a.serviceManager != nil {
		shutdownServices(ctx, a.serviceManager)
	}
	return nil
}

func shutdownServices(ctx context.Context, serviceManager *manager.ServiceManager) {
	ctx = appctx.WithOperationName(ctx, "services_shutdown")
	shutdownCtx, cancel := context.WithTimeout(ctx, serviceShutdownTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		if err := serviceManager.StopAll(shutdownCtx); err != nil {
			logger.WithContextError(ctx, err).Error("Error during service shutdown")
		}
		close(done)
	}()

	select {
	case <-done:
		logger.InfoWithContext(ctx, "All services stopped successfully")
	case <-shutdownCtx.Done():
		if shutdownCtx.Err() == context.DeadlineExceeded {
			logger.WarnWithContext(ctx, "Service shutdown timed out, some services may not have stopped gracefully")
		}
	}
}

func monitorServices(ctx context.Context, serviceManager *manager.ServiceManager) {
	logger.InfoWithContext(ctx, "Starting service monitoring")
	serviceManager.MonitorServices(ctx)
	logger.InfoWithContext(ctx, "Service monitoring stopped")
}

func setupMessageListeners(ctx context.Context, serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager) error {
	return logger.LogOperation(ctx, "setup_message_listeners", func(ctx context.Context) error {
		logger.InfoWithContext(ctx, "Setting up message listeners")

		svc, ok := serviceManager.GetServiceByName(ctx, "WebSocketServiceA")
		if !ok {
			return errors.New(errors.TypeNotFound, "Service not found: WebSocketServiceA", nil)
		}

		websocketServiceA, ok := svc.(*service.WebSocketService)
		if !ok {
			return errors.New(errors.TypeInternal, "Service is not of type *service.WebSocketService", nil).
				WithField("actual_type", fmt.Sprintf("%T", svc))
		}

		websocketServiceA.OnMessage(func(msg model.Message) {
			msgCtx := appctx.NewContext(ctx)
			if err := workflowManager.DispatchMessage(msgCtx, msg); err != nil {
				logger.WithContextError(msgCtx, err).Errorf("Error dispatching message: %+v", msg)
			}
		})

		logger.InfoWithContext(ctx, "Message listeners set up successfully")
		return nil
	})
}
