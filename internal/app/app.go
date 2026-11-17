package app

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
	"github.com/elvinyao/go-service-platform/internal/workflow"
	"github.com/elvinyao/go-service-platform/pkg/config"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
	appruntime "github.com/elvinyao/go-service-platform/pkg/runtime"
)

const (
	appVersion             = "1.0.0"
	appStartOperationName  = "startup"
	serviceShutdownTimeout = 10 * time.Second
)

// LifecycleState represents the explicit lifecycle state of App.
type LifecycleState string

const (
	// AppStateStopped indicates the application is idle with no active subsystems.
	AppStateStopped LifecycleState = "stopped"
	// AppStateStarting indicates staged startup is in progress.
	AppStateStarting LifecycleState = "starting"
	// AppStateRunning indicates all configured subsystems are active.
	AppStateRunning LifecycleState = "running"
	// AppStateStopping indicates graceful shutdown is in progress.
	AppStateStopping LifecycleState = "stopping"
	// AppStatePendingCleanup indicates a previous Stop or rollback left subsystems that failed to stop.
	AppStatePendingCleanup LifecycleState = "pending_cleanup"
)

type App struct {
	name           string
	config         config.RuntimeConfig
	ruleConfigPath string
	rulePath       string
	lifecycleMu    sync.Mutex
	state          LifecycleState

	serviceManager *manager.ServiceManager
	workflowEngine *workflow.WorkflowEngine
	healthManager  *health.HealthManager
	adminServer    *appruntime.AdminServer

	monitorCancel context.CancelFunc
	monitorWg     sync.WaitGroup
}

func New(name string, cfg config.RuntimeConfig, ruleConfigPath, rulePath string) *App {
	return &App{
		name:           name,
		config:         cfg,
		ruleConfigPath: ruleConfigPath,
		rulePath:       rulePath,
		state:          AppStateStopped,
	}
}

// State returns the current explicit lifecycle state of the application.
func (a *App) State() LifecycleState {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.state == "" {
		return AppStateStopped
	}
	return a.state
}

func (a *App) AdminAddress() string {
	return a.config.Admin.Address
}

func (a *App) Start(ctx context.Context) error {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()

	switch a.state {
	case AppStateRunning:
		return errors.New(errors.TypeInvalidInput, "application is already started", nil)
	case AppStatePendingCleanup:
		return errors.New(errors.TypeServiceUnavailable, "application has subsystems pending cleanup", nil)
	case AppStateStarting, AppStateStopping:
		return errors.New(errors.TypeInvalidInput, "application lifecycle transition already in progress", nil)
	}

	if err := a.config.Validate(); err != nil {
		return errors.Wrap(err, "invalid runtime config", errors.TypeInvalidInput)
	}

	a.state = AppStateStarting
	rootCtx := appctx.NewContext(ctx)
	rootCtx = appctx.WithServiceName(rootCtx, a.name)
	startCtx := appctx.WithOperationName(rootCtx, appStartOperationName)

	a.healthManager = health.NewHealthManager(30*time.Second, appVersion)
	a.serviceManager = manager.NewServiceManager(appVersion, a.healthManager)
	registerServices(a.serviceManager, a.config, appVersion)

	if len(a.serviceManager.ListServices(startCtx)) > 0 {
		if err := a.serviceManager.StartAll(startCtx); err != nil {
			err = a.rollbackStartup(err)
			return errors.Wrap(err, "failed to start services", errors.TypeServiceUnavailable)
		}
	} else {
		logger.InfoWithContext(startCtx, "No runtime services enabled")
	}

	monitorCtx, monitorCancel := context.WithCancel(rootCtx)
	a.monitorCancel = monitorCancel
	a.monitorWg.Add(1)
	go func() {
		defer a.monitorWg.Done()
		monitorServices(monitorCtx, a.serviceManager)
	}()

	workflowEngine, err := workflow.NewWorkflowEngine(rootCtx, a.serviceManager, a.ruleConfigPath, a.rulePath, a.config)
	if err != nil {
		err = a.rollbackStartup(err)
		return errors.Wrap(err, "failed to initialize workflow engine", errors.TypeInternal)
	}
	a.workflowEngine = workflowEngine

	if a.config.Inputs.WebSocket.Enabled {
		if err := setupMessageListeners(rootCtx, a.serviceManager, a.workflowEngine); err != nil {
			err = a.rollbackStartup(err)
			return errors.Wrap(err, "failed to set up message listeners", errors.TypeInternal)
		}
	}

	mux := newAdminMux(a.serviceManager, a.workflowEngine, a.healthManager)
	a.adminServer = appruntime.NewAdminServer(a.config.Admin.Address, mux, appruntime.AdminServerOptions{
		ReadHeaderTimeout: a.config.Admin.ReadHeaderTimeout,
		ReadTimeout:       a.config.Admin.ReadTimeout,
		WriteTimeout:      a.config.Admin.WriteTimeout,
		IdleTimeout:       a.config.Admin.IdleTimeout,
	})
	if err := a.adminServer.Start(rootCtx); err != nil {
		err = a.rollbackStartup(err)
		return errors.Wrap(err, "failed to start admin server", errors.TypeServiceUnavailable)
	}

	logger.InfofWithContext(rootCtx, "Admin server started on %s", a.adminServer.Addr())
	a.state = AppStateRunning
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()

	if a.state == "" || a.state == AppStateStopped {
		a.state = AppStateStopped
		return nil
	}

	a.state = AppStateStopping
	if err := a.stop(ctx); err != nil {
		a.state = AppStatePendingCleanup
		return err
	}
	a.state = AppStateStopped
	return nil
}

func (a *App) rollbackStartup(startErr error) error {
	a.state = AppStateStopping
	if cleanupErr := a.stop(context.Background()); cleanupErr != nil {
		a.state = AppStatePendingCleanup
		return stderrors.Join(startErr, fmt.Errorf("clean up failed startup: %w", cleanupErr))
	}
	a.state = AppStateStopped
	return startErr
}

func (a *App) stop(ctx context.Context) error {
	var stopErrors []error
	if a.healthManager != nil {
		a.healthManager.SetShuttingDown()
	}
	if a.monitorCancel != nil {
		a.monitorCancel()
		a.monitorWg.Wait()
		a.monitorCancel = nil
	}
	if a.adminServer != nil {
		if err := a.adminServer.Stop(ctx); err != nil {
			logger.WithContextError(ctx, err).Warn("Admin server shutdown error")
			stopErrors = append(stopErrors, fmt.Errorf("stop admin server: %w", err))
		} else {
			a.adminServer = nil
		}
	}
	if a.workflowEngine != nil {
		if err := a.workflowEngine.Stop(ctx); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("stop workflows: %w", err))
		} else {
			a.workflowEngine = nil
		}
	}
	if a.serviceManager != nil {
		if err := shutdownServices(ctx, a.serviceManager); err != nil {
			stopErrors = append(stopErrors, err)
		} else {
			a.serviceManager = nil
		}
	}
	if len(stopErrors) == 0 {
		a.healthManager = nil
	}
	return stderrors.Join(stopErrors...)
}

func registerServices(sm *manager.ServiceManager, cfg config.RuntimeConfig, version string) {
	if cfg.Inputs.WebSocket.Enabled {
		_ = sm.RegisterService(service.NewWebSocketService(
			service.WebSocketInputServiceName,
			ruleengine.DefaultWorkflowName,
			service.WebSocketConfig{
				ServerURL:         cfg.Inputs.WebSocket.ServerURL,
				Path:              cfg.Inputs.WebSocket.Path,
				ReconnectInterval: cfg.Inputs.WebSocket.ReconnectInterval,
			},
			version,
		))
	}

	if cfg.Adapters.BadgeDB.Enabled {
		_ = sm.RegisterService(service.NewBadgeDBService(
			service.BadgeDBServiceName,
			ruleengine.DefaultWorkflowName,
			cfg.Adapters.BadgeDB.Path,
			version,
		))
	}

	if cfg.Adapters.Mattermost.Enabled {
		_ = sm.RegisterService(service.NewMattermostService(
			service.MattermostServiceName,
			ruleengine.DefaultWorkflowName,
			service.MattermostConfig{
				ServerURL:    cfg.Adapters.Mattermost.ServerURL,
				APIToken:     cfg.Adapters.Mattermost.APIToken,
				Channel:      cfg.Adapters.Mattermost.Channel,
				Username:     os.Getenv("MATTERMOST_USERNAME"),
				Password:     os.Getenv("MATTERMOST_PASSWORD"),
				WebsocketURL: cfg.Adapters.Mattermost.WebsocketURL,
			},
			version,
		))
	}

	if cfg.Adapters.Confluence.Enabled {
		_ = sm.RegisterService(service.NewConfluenceSettingsService(
			service.ConfluenceSettingsServiceName,
			ruleengine.DefaultWorkflowName,
			service.ConfluenceSettingsConfig{
				PageID:          cfg.Adapters.Confluence.SettingsPageID,
				RefreshInterval: cfg.Adapters.Confluence.RefreshInterval,
				APIEndpoint:     cfg.Adapters.Confluence.APIEndpoint,
			},
			version,
		))
	}
}

func shutdownServices(ctx context.Context, serviceManager *manager.ServiceManager) error {
	ctx = appctx.WithOperationName(ctx, "services_shutdown")
	shutdownCtx, cancel := context.WithTimeout(ctx, serviceShutdownTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		err := serviceManager.StopAll(shutdownCtx)
		if err != nil {
			logger.WithContextError(ctx, err).Error("Error during service shutdown")
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("stop services: %w", err)
		}
		logger.InfoWithContext(ctx, "All services stopped successfully")
		return nil
	case <-shutdownCtx.Done():
		if shutdownCtx.Err() == context.DeadlineExceeded {
			logger.WarnWithContext(ctx, "Service shutdown timed out, some services may not have stopped gracefully")
		}
		return fmt.Errorf("stop services: %w", shutdownCtx.Err())
	}
}

func monitorServices(ctx context.Context, serviceManager *manager.ServiceManager) {
	logger.InfoWithContext(ctx, "Starting service monitoring")
	serviceManager.MonitorServices(ctx)
	logger.InfoWithContext(ctx, "Service monitoring stopped")
}

func setupMessageListeners(ctx context.Context, serviceManager *manager.ServiceManager, workflowEngine *workflow.WorkflowEngine) error {
	return logger.LogOperation(ctx, "setup_message_listeners", func(ctx context.Context) error {
		logger.InfoWithContext(ctx, "Setting up message listeners")

		svc, ok := serviceManager.GetServiceByName(ctx, service.WebSocketInputServiceName)
		if !ok {
			return errors.New(errors.TypeNotFound, "Service not found: "+service.WebSocketInputServiceName, nil)
		}

		websocketServiceA, ok := svc.(*service.WebSocketService)
		if !ok {
			return errors.New(errors.TypeInternal, "Service is not of type *service.WebSocketService", nil).
				WithField("actual_type", fmt.Sprintf("%T", svc))
		}

		websocketServiceA.OnMessage(func(msg ruleengine.Message) {
			msgCtx := appctx.NewContext(ctx)
			if err := workflowEngine.Submit(msgCtx, msg); err != nil {
				logger.WithContextError(msgCtx, err).Warnf("Message type=%s id=%s was not queued", msg.Type, msg.ID)
			}
		})

		logger.InfoWithContext(ctx, "Message listeners set up successfully")
		return nil
	})
}
