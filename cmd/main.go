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
	"project/internal/ruleengine"
	"project/internal/service"
	workflowimpl "project/internal/workflow"
	appctx "project/pkg/context"
	"project/pkg/di"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"syscall"
	"time"
)

const (
	// Application version
	appVersion = "1.0.0"
	// Application name
	appName = "service-workflow"
	// Graceful shutdown timeout - overall timeout
	shutdownTimeout = 30 * time.Second
	// Service stop timeout - per service timeout
	serviceShutdownTimeout = 10 * time.Second
	// Drain period - time to wait after marking as not-ready before shutting down.
	// This gives K8s time to remove the pod from endpoints after readiness fails.
	drainPeriod = 5 * time.Second

	appStartOperationName = "startup"
)

func main() {
	// Initialize logger with environment variables
	logger.InitFromEnv()

	// Log application start
	logger.WithFields(logger.Fields{
		"name":    appName,
		"version": appVersion,
		"pid":     os.Getpid(),
	}).Info("Application starting")

	// Create our application context with request ID
	rootCtx := appctx.NewContext(context.Background())

	// Setup cancellable context for shutdown
	cancelCtx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	rootCtx = appctx.WithServiceName(rootCtx, appName)

	logger.InfoWithContext(rootCtx, "Initializing application")

	// Set up signal handling for graceful shutdown
	signalChan := setupSignalHandling(rootCtx, cancel)

	// Create dependency injection container
	container := di.NewContainer(appVersion)
	startCtx := appctx.WithOperationName(rootCtx, appStartOperationName)

	// Initialize dependencies
	serviceManager := container.GetServiceManager(startCtx)

	// Register all services
	if err := container.RegisterServices(startCtx); err != nil {
		logger.WithContextError(rootCtx, err).Fatal("Failed to register services")
	}

	// Start all services
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

	// Create health check manager
	healthManager := container.GetHealthManager(rootCtx)

	// Create HTTP management server
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
	shutdownSequence(shutdownCtx, serviceManager, healthManager, adminServer, monitorCancel, &adminServerWg, &monitorWg)

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

// shutdownSequence performs a coordinated shutdown of all components.
// The 5-step flow ensures zero-downtime during K8s rolling updates:
//  1. Mark as shutting down (readiness returns 503)
//  2. Drain period (wait for K8s to de-register pod from endpoints)
//  3. Stop service monitoring
//  4. Shutdown admin HTTP server (complete in-flight requests)
//  5. Stop all services
func shutdownSequence(ctx context.Context, serviceManager *manager.ServiceManager,
	healthManager *health.HealthManager, adminServer *http.Server,
	monitorCancel context.CancelFunc,
	adminWg *sync.WaitGroup, monitorWg *sync.WaitGroup) {

	ctx = appctx.WithOperationName(ctx, "graceful_shutdown")
	logger.InfoWithContext(ctx, "Starting graceful shutdown sequence")

	// Step 1: Mark as shutting down so readiness probe returns 503
	logger.InfoWithContext(ctx, "Step 1/5: Marking as not ready (readiness will return 503)")
	healthManager.SetShuttingDown()

	// Step 2: Wait for drain period to let K8s remove pod from endpoints
	logger.InfofWithContext(ctx, "Step 2/5: Waiting %v for traffic drain", drainPeriod)
	select {
	case <-time.After(drainPeriod):
		logger.InfoWithContext(ctx, "Drain period complete")
	case <-ctx.Done():
		logger.WarnWithContext(ctx, "Context cancelled during drain period")
	}

	// Step 3: Stop service monitoring to avoid log spam during shutdown
	logger.InfoWithContext(ctx, "Step 3/5: Stopping service monitoring")
	monitorCancel()

	monitorDone := make(chan struct{})
	go func() {
		monitorWg.Wait()
		close(monitorDone)
	}()

	select {
	case <-monitorDone:
		logger.InfoWithContext(ctx, "Service monitoring stopped successfully")
	case <-time.After(5 * time.Second):
		logger.WarnWithContext(ctx, "Timeout waiting for service monitoring to stop")
	}

	// Step 4: Shutdown the admin HTTP server (completes in-flight requests)
	logger.InfoWithContext(ctx, "Step 4/5: Shutting down admin server")
	httpShutdownCtx, httpCancel := context.WithTimeout(ctx, 5*time.Second)
	defer httpCancel()

	if err := adminServer.Shutdown(httpShutdownCtx); err != nil {
		logger.WithContextError(ctx, err).Warn("Admin server shutdown error")
	}

	httpDone := make(chan struct{})
	go func() {
		adminWg.Wait()
		close(httpDone)
	}()

	select {
	case <-httpDone:
		logger.InfoWithContext(ctx, "Admin server stopped successfully")
	case <-time.After(7 * time.Second):
		logger.WarnWithContext(ctx, "Timeout waiting for admin server to stop")
	}

	// Step 5: Shutdown all services
	logger.InfoWithContext(ctx, "Step 5/5: Shutting down all services")
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

// Create HTTP management server
func createAdminServer(ctx context.Context, serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager, healthManager *health.HealthManager) *http.Server {
	ctx = appctx.WithOperationName(ctx, "create_admin_server")
	logger.InfoWithContext(ctx, "Creating admin server")

	// Create HTTP router
	mux := http.NewServeMux()

	// Register health check endpoints: /health, /health/service, /health/readiness, /health/liveness
	healthHandler := health.NewHealthHandler(healthManager)
	healthHandler.RegisterHTTPHandlers(mux)

	// Add service management endpoint
	mux.HandleFunc("/services", func(w http.ResponseWriter, r *http.Request) {
		servicesList(w, r, serviceManager)
	})

	// Add workflow endpoint
	mux.HandleFunc("/workflows", func(w http.ResponseWriter, r *http.Request) {
		workflowsList(w, r, workflowManager)
	})
	mux.HandleFunc("/rule-engine", func(w http.ResponseWriter, r *http.Request) {
		ruleEngineInfo(w, r, workflowManager)
	})

	// Configure HTTP server with timeout settings
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

// servicesList handles service list requests
func servicesList(w http.ResponseWriter, r *http.Request, serviceManager *manager.ServiceManager) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling services list request")

	// Create service info list
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

		// Get service metrics
		if metricProvider, ok := svc.(interface {
			GetMetrics(ctx context.Context) map[string]interface{}
		}); ok {
			info.Metrics = metricProvider.GetMetrics(ctx)
		}

		// Get associated workflow
		if workflowProvider, ok := svc.(interface {
			GetWorkflowName() string
		}); ok {
			info.Workflow = workflowProvider.GetWorkflowName()
		}

		serviceInfos = append(serviceInfos, info)
	}

	// Set response header
	w.Header().Set("Content-Type", "application/json")

	// Encode with error handling
	if err := json.NewEncoder(w).Encode(serviceInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode services list")
	}
}

// workflowsList handles workflow list requests
func workflowsList(w http.ResponseWriter, r *http.Request, workflowManager *manager.WorkflowManager) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling workflows list request")

	// Create workflow info list
	type WorkflowInfo struct {
		Name string `json:"name"`
	}

	// Get all registered workflows
	workflowNames := workflowManager.ListWorkflows(ctx)
	workflowInfos := make([]WorkflowInfo, 0, len(workflowNames))

	for _, name := range workflowNames {
		workflowInfos = append(workflowInfos, WorkflowInfo{Name: name})
	}

	// Set response header
	w.Header().Set("Content-Type", "application/json")

	// Encode with error handling
	if err := json.NewEncoder(w).Encode(workflowInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode workflows list")
	}
}

// ruleEngineInfo handles rule engine detail requests.
func ruleEngineInfo(w http.ResponseWriter, r *http.Request, workflowManager *manager.WorkflowManager) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling rule engine info request")

	wf, err := workflowManager.GetWorkflowByName(ctx, ruleengine.DefaultWorkflowName)
	if err != nil {
		http.Error(w, "rule engine workflow not found", http.StatusNotFound)
		return
	}

	engine, ok := wf.(*workflowimpl.WorkflowEngine)
	if !ok {
		http.Error(w, "registered workflow is not WorkflowEngine", http.StatusInternalServerError)
		return
	}

	snapshot := engine.AdminSnapshot(ctx)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode rule engine info")
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}
