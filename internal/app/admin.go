package app

import (
	"encoding/json"
	"net/http"

	"github.com/elvinyao/go-service-platform/internal/manager"
	workflowimpl "github.com/elvinyao/go-service-platform/internal/workflow"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
)

func newAdminMux(serviceManager *manager.ServiceManager, workflowEngine *workflowimpl.WorkflowEngine, healthManager *health.HealthManager) *http.ServeMux {
	mux := http.NewServeMux()

	healthHandler := health.NewHealthHandler(healthManager)
	healthHandler.RegisterHTTPHandlers(mux)

	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		servicesList(w, r, serviceManager)
	})
	mux.HandleFunc("GET /workflows", func(w http.ResponseWriter, r *http.Request) {
		workflowsList(w, r, workflowEngine)
	})
	mux.HandleFunc("GET /rule-engine", func(w http.ResponseWriter, r *http.Request) {
		ruleEngineInfo(w, r, workflowEngine)
	})

	return mux
}

func servicesList(w http.ResponseWriter, r *http.Request, serviceManager *manager.ServiceManager) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling services list request")

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
			Name:     svc.GetName(),
			Running:  svc.IsRunning(ctx),
			Workflow: svc.GetWorkflow(),
			Type:     svc.GetType(),
			Metrics:  svc.GetMetrics(ctx),
		}
		serviceInfos = append(serviceInfos, info)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(serviceInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode services list")
	}
}

func workflowsList(w http.ResponseWriter, r *http.Request, workflowEngine *workflowimpl.WorkflowEngine) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling workflows list request")

	type WorkflowInfo struct {
		Name string `json:"name"`
	}

	workflowInfos := make([]WorkflowInfo, 0, 1)
	if workflowEngine != nil {
		workflowInfos = append(workflowInfos, WorkflowInfo{Name: workflowEngine.GetName()})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(workflowInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode workflows list")
	}
}

func ruleEngineInfo(w http.ResponseWriter, r *http.Request, workflowEngine *workflowimpl.WorkflowEngine) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling rule engine info request")

	if workflowEngine == nil {
		http.Error(w, "rule engine workflow not found", http.StatusNotFound)
		return
	}

	snapshot := workflowEngine.AdminSnapshot(ctx)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode rule engine info")
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}
