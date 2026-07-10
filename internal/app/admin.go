package app

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/elvinyao/go-service-platform/internal/manager"
	workflowimpl "github.com/elvinyao/go-service-platform/internal/workflow"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func newAdminMux(serviceManager *manager.ServiceManager, workflowManager *manager.WorkflowManager, healthManager *health.HealthManager) *http.ServeMux {
	mux := http.NewServeMux()

	healthHandler := health.NewHealthHandler(healthManager)
	healthHandler.RegisterHTTPHandlers(mux)

	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		servicesList(w, r, serviceManager)
	})
	mux.HandleFunc("GET /workflows", func(w http.ResponseWriter, r *http.Request) {
		workflowsList(w, r, workflowManager)
	})
	mux.HandleFunc("GET /rule-engine", func(w http.ResponseWriter, r *http.Request) {
		ruleEngineInfo(w, r, workflowManager)
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
			Name:    svc.GetName(),
			Running: svc.IsRunning(ctx),
			Type:    svc.GetType(),
		}

		if metricProvider, ok := svc.(interface {
			GetMetrics(ctx context.Context) map[string]interface{}
		}); ok {
			info.Metrics = metricProvider.GetMetrics(ctx)
		}

		info.Workflow = svc.GetWorkflow()

		serviceInfos = append(serviceInfos, info)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(serviceInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode services list")
	}
}

func workflowsList(w http.ResponseWriter, r *http.Request, workflowManager *manager.WorkflowManager) {
	ctx := appctx.FromRequest(r)
	logger.InfoWithContext(ctx, "Handling workflows list request")

	type WorkflowInfo struct {
		Name string `json:"name"`
	}

	workflowNames := workflowManager.ListWorkflows(ctx)
	workflowInfos := make([]WorkflowInfo, 0, len(workflowNames))

	for _, name := range workflowNames {
		workflowInfos = append(workflowInfos, WorkflowInfo{Name: name})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(workflowInfos); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode workflows list")
	}
}

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
